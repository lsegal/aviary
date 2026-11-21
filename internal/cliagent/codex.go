package cliagent

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/process"
	"github.com/lsegal/aviary/internal/transcript"
)

// codexSession runs each turn as a `codex exec --json` process and resumes
// the Codex thread on later turns with `codex exec resume`.
//
// `codex app-server` would keep one process alive per session, but its
// JSON-RPC protocol is still marked experimental, so per-turn exec is used.
type codexSession struct {
	opts Options

	mu       sync.Mutex
	threadID string
}

func newCodexSession(opts Options) *codexSession {
	return &codexSession{opts: opts, threadID: opts.ResumeID}
}

// codexBuiltinFeatures are the Codex features that give the model its own
// shell, file and web access.
var codexBuiltinFeatures = []string{"shell_tool", "unified_exec", "view_image", "browser_use", "computer_use"}

// codexArgs builds the command line for one Codex turn. The subcommand,
// model, thread and prompt placement come from codex's CLI provider
// definition, as every other launch renders them; the prompt is read from
// stdin. What an interactive chat adds (its sandbox, Aviary's MCP server, the
// developer instructions and images) takes the place of the template's
// approval arguments, which every codex template puts before its positional
// thread and prompt. With a profile, codex reads its settings, the developer
// instructions among them, from aviary's own CODEX_HOME (see
// writeCodexProfile); otherwise it ignores the user's config and the
// instructions go on the command line.
func codexArgs(opts Options, threadID, profile string, images []string) []string {
	chat := []string{"-c", "approval_policy=" + tomlString("never")}
	if profile == "" {
		chat = append(chat, "--ignore-user-config")
	}
	if opts.Ephemeral {
		chat = append(chat, "--ephemeral")
	}
	if opts.SystemPrompt != "" && profile == "" {
		chat = append(chat, "-c", "developer_instructions="+tomlString(opts.SystemPrompt))
	}
	if opts.BuiltinTools {
		chat = append(chat, opts.Approval...)
		chat = append(chat, opts.ExtraDir...)
	} else {
		chat = append(chat,
			"-c", "sandbox_mode="+tomlString("read-only"),
			"-c", "web_search="+tomlString("disabled"),
		)
		for _, f := range codexBuiltinFeatures {
			chat = append(chat, "--disable", f)
		}
	}
	if opts.MCPURL != "" {
		prefix := "mcp_servers." + MCPServerName + "."
		chat = append(chat,
			"-c", prefix+"url="+tomlString(opts.MCPURL),
			"-c", prefix+"bearer_token_env_var="+tomlString(mcpTokenEnv),
			"-c", prefix+"default_tools_approval_mode="+tomlString("approve"),
			"-c", prefix+"tool_timeout_sec="+strconv.Itoa(int(MCPToolTimeout.Seconds())),
		)
	}
	for _, img := range images {
		chat = append(chat, "--image", img)
	}
	mode := clidefs.ModeRun
	if threadID != "" {
		mode = clidefs.ModeResume
	}
	args := renderDefinition(Codex, mode, clidefs.Values{
		Model:        opts.Model,
		Session:      threadID,
		ApprovalArgs: chat,
		PromptStdin:  true,
		NoCheckout:   true, // a chat's working directory need not be a git repository
	})
	if profile != "" && len(args) > 0 {
		// `exec resume` takes no --profile of its own, so it goes right after
		// `exec`, ahead of any subcommand.
		args = slices.Insert(args, 1, "--profile", profile)
	}
	return args
}

// tomlString quotes s as a TOML basic string. JSON string escapes are a
// subset of TOML's.
func tomlString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

// writeCodexProfile writes the developer instructions into a codex profile in
// the aviary-owned CODEX_HOME that opts.Env names, and returns the profile's
// name and a cleanup that removes it. A long system prompt passed with -c can
// exceed the Windows command-line limit (#573). Profiles are only read
// without --ignore-user-config, which that home's empty config.toml makes
// equivalent. Without such a home or a system prompt, no profile is written.
func writeCodexProfile(opts Options) (string, func(), error) {
	home := ""
	for _, entry := range opts.Env {
		if value, ok := strings.CutPrefix(entry, "CODEX_HOME="); ok {
			home = value
		}
	}
	if home == "" || opts.SystemPrompt == "" {
		return "", func() {}, nil
	}
	file, err := os.CreateTemp(home, clidefs.LaunchFilePrefix+"*"+codexProfileSuffix)
	if err != nil {
		return "", nil, fmt.Errorf("writing the codex profile: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	_, err = file.WriteString("developer_instructions = " + tomlString(opts.SystemPrompt) + "\n")
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("writing the codex profile: %w", err)
	}
	return strings.TrimSuffix(filepath.Base(path), codexProfileSuffix), cleanup, nil
}

// codexProfileSuffix ends the name of the file codex reads `--profile NAME`
// from: $CODEX_HOME/NAME.config.toml.
const codexProfileSuffix = ".config.toml"

func (s *codexSession) Send(ctx context.Context, turn Turn, onEvent func(Event)) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var images []string
	for _, mediaURL := range turn.MediaURLs {
		path, err := writeTempImage(ctx, mediaURL)
		if err != nil {
			return Result{}, err
		}
		defer os.Remove(path) //nolint:errcheck
		images = append(images, path)
	}

	profile, removeProfile, err := writeCodexProfile(s.opts)
	if err != nil {
		return Result{}, err
	}
	defer removeProfile()

	cmd := process.Command(ctx, s.opts.binary(), codexArgs(s.opts, s.threadID, profile, images)...)
	cmd.Dir = s.opts.WorkDir
	cmd.Env = cliEnv(s.opts)
	text := turn.Text
	if strings.TrimSpace(text) == "" {
		text = "(see attached image)"
	}
	cmd.Stdin = strings.NewReader(text)
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("starting codex: %w", err)
	}

	m := newCodexMapper(onEvent)
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	var turnErr error
	finished := false
	for sc.Scan() {
		done, err := m.handle(sc.Bytes())
		if m.threadID != "" {
			s.threadID = m.threadID
		}
		if err != nil && turnErr == nil {
			turnErr = err
		}
		if done {
			finished = true
		}
	}
	_, _ = io.Copy(io.Discard, stdout)
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if turnErr != nil {
		return Result{}, turnErr
	}
	if !finished {
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return Result{}, fmt.Errorf("codex exited: %s", msg)
		}
		if waitErr != nil {
			return Result{}, fmt.Errorf("codex exited: %w", waitErr)
		}
		return Result{}, errors.New("codex exited before finishing the turn")
	}
	return Result{Text: m.lastMessage}, nil
}

func (s *codexSession) Close() error { return nil }

// codexMapper turns `codex exec --json` lines into Events. The shared stream
// parser decodes messages, tools and usage; codexMapper reads only the thread
// ID and the turn's outcome.
type codexMapper struct {
	emit        func(Event)
	threadID    string
	lastMessage string
	messages    int
	stream      transcript.Mapper
	tools       *toolTranslator
}

func newCodexMapper(emit func(Event)) *codexMapper {
	if emit == nil {
		emit = func(Event) {}
	}
	return &codexMapper{
		emit:   emit,
		stream: transcript.NewMapper(transcript.FormatCodexJSON),
		tools: newToolTranslator(emit, func(name string) bool {
			return strings.HasPrefix(name, MCPServerName+".") // reported by Aviary's MCP boundary
		}),
	}
}

// handle processes one line. It returns done once the turn completes.
func (m *codexMapper) handle(line []byte) (bool, error) {
	var ev map[string]any
	if err := json.Unmarshal(line, &ev); err != nil {
		return false, nil // ignore log noise
	}
	events := m.stream.Map(line)
	for _, event := range events {
		if event.Kind != transcript.KindMessage || event.Text == "" {
			continue
		}
		if m.messages > 0 {
			m.emit(Event{Type: EventText, Text: "\n\n"})
		}
		m.messages++
		m.lastMessage = event.Text
		m.emit(Event{Type: EventText, Text: event.Text})
	}
	m.tools.translate(events)
	switch stringField(ev, "type") {
	case "thread.started":
		if id := stringField(ev, "thread_id"); id != "" {
			m.threadID = id
			m.emit(Event{Type: EventSession, SessionID: id})
		}
	case "turn.completed":
		return true, nil
	case "turn.failed":
		msg := codexErrorMessage(stringField(mapField(ev, "error"), "message"))
		if msg == "" {
			msg = "turn failed"
		}
		return true, fmt.Errorf("codex: %s", msg)
	case "error":
		if msg := codexErrorMessage(stringField(ev, "message")); msg != "" {
			return false, fmt.Errorf("codex: %s", msg)
		}
	}
	return false, nil
}

// codexErrorMessage unwraps API errors that Codex reports as a JSON string,
// such as {"type":"error","status":400,"error":{"message":"..."}}.
func codexErrorMessage(msg string) string {
	var body struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if strings.HasPrefix(strings.TrimSpace(msg), "{") && json.Unmarshal([]byte(msg), &body) == nil && body.Error.Message != "" {
		return body.Error.Message
	}
	return msg
}

// writeTempImage stores an image given as a data URL or remote URL in a temp
// file so it can be passed to `codex exec --image`.
func writeTempImage(ctx context.Context, mediaURL string) (string, error) {
	var (
		data      []byte
		mediaType string
	)
	if strings.HasPrefix(mediaURL, "data:") {
		mt, payload, err := splitDataURL(mediaURL)
		if err != nil {
			return "", err
		}
		data, err = base64.StdEncoding.DecodeString(payload)
		if err != nil {
			return "", fmt.Errorf("decoding image: %w", err)
		}
		mediaType = mt
	} else {
		reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, mediaURL, nil)
		if err != nil {
			return "", err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("fetching image: %w", err)
		}
		defer resp.Body.Close() //nolint:errcheck
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("fetching image: HTTP %d", resp.StatusCode)
		}
		data, err = io.ReadAll(io.LimitReader(resp.Body, 32<<20))
		if err != nil {
			return "", fmt.Errorf("fetching image: %w", err)
		}
		mediaType = resp.Header.Get("Content-Type")
	}
	ext := ".png"
	if exts, _ := mime.ExtensionsByType(strings.TrimSpace(strings.Split(mediaType, ";")[0])); len(exts) > 0 {
		ext = exts[0]
	}
	f, err := os.CreateTemp("", "aviary-image-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(f.Name())
		return "", err
	}
	return f.Name(), f.Close()
}
