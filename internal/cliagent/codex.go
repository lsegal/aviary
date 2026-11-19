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
	"os/exec"
	"strings"
	"sync"
	"time"
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

// codexArgs builds the command line for one Codex turn. The prompt is read
// from stdin.
func codexArgs(opts Options, threadID string, images []string) []string {
	args := []string{"exec"}
	if threadID != "" {
		args = append(args, "resume")
	}
	args = append(args,
		"--json",
		"--skip-git-repo-check",
		"--ignore-user-config",
		"--model", opts.Model,
		"-c", "approval_policy=" + tomlString("never"),
	)
	if opts.Ephemeral {
		args = append(args, "--ephemeral")
	}
	if opts.SystemPrompt != "" {
		args = append(args, "-c", "developer_instructions="+tomlString(opts.SystemPrompt))
	}
	if opts.BuiltinTools {
		args = append(args, "-c", "sandbox_mode="+tomlString("workspace-write"))
	} else {
		args = append(args,
			"-c", "sandbox_mode="+tomlString("read-only"),
			"-c", "web_search="+tomlString("disabled"),
		)
		for _, f := range codexBuiltinFeatures {
			args = append(args, "--disable", f)
		}
	}
	if opts.MCPURL != "" {
		prefix := "mcp_servers." + MCPServerName + "."
		args = append(args,
			"-c", prefix+"url="+tomlString(opts.MCPURL),
			"-c", prefix+"bearer_token_env_var="+tomlString(mcpTokenEnv),
			"-c", prefix+"default_tools_approval_mode="+tomlString("approve"),
		)
	}
	for _, img := range images {
		args = append(args, "--image", img)
	}
	if threadID != "" {
		args = append(args, threadID)
	}
	return append(args, "-")
}

// tomlString quotes s as a TOML basic string. JSON string escapes are a
// subset of TOML's.
func tomlString(s string) string {
	data, _ := json.Marshal(s)
	return string(data)
}

func (s *codexSession) Send(ctx context.Context, turn Turn, onEvent func(Event)) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	var images []string
	if turn.MediaURL != "" {
		path, err := writeTempImage(ctx, turn.MediaURL)
		if err != nil {
			return Result{}, err
		}
		defer os.Remove(path) //nolint:errcheck
		images = append(images, path)
	}

	cmd := exec.CommandContext(ctx, s.opts.binary(), codexArgs(s.opts, s.threadID, images)...) //nolint:gosec // binary is a fixed backend name
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

// codexMapper turns `codex exec --json` lines into Events.
type codexMapper struct {
	emit        func(Event)
	threadID    string
	lastMessage string
	messages    int
	started     map[string]bool
}

func newCodexMapper(emit func(Event)) *codexMapper {
	if emit == nil {
		emit = func(Event) {}
	}
	return &codexMapper{emit: emit, started: map[string]bool{}}
}

// handle processes one line. It returns done once the turn completes.
func (m *codexMapper) handle(line []byte) (bool, error) {
	var ev map[string]any
	if err := json.Unmarshal(line, &ev); err != nil {
		return false, nil // ignore log noise
	}
	switch stringField(ev, "type") {
	case "thread.started":
		if id := stringField(ev, "thread_id"); id != "" {
			m.threadID = id
			m.emit(Event{Type: EventSession, SessionID: id})
		}
	case "item.started":
		if tool := codexTool(mapField(ev, "item")); tool != nil {
			m.started[tool.ID] = true
			m.emit(Event{Type: EventToolStart, Tool: tool})
		}
	case "item.completed":
		item := mapField(ev, "item")
		if stringField(item, "type") == "agent_message" {
			text := stringField(item, "text")
			if text == "" {
				break
			}
			if m.messages > 0 {
				m.emit(Event{Type: EventText, Text: "\n\n"})
			}
			m.messages++
			m.lastMessage = text
			m.emit(Event{Type: EventText, Text: text})
			break
		}
		tool := codexTool(item)
		if tool == nil {
			break
		}
		if !m.started[tool.ID] {
			m.emit(Event{Type: EventToolStart, Tool: &ToolEvent{ID: tool.ID, Name: tool.Name, Args: tool.Args}})
		}
		delete(m.started, tool.ID)
		codexToolOutcome(item, tool)
		m.emit(Event{Type: EventToolResult, Tool: tool})
	case "turn.completed":
		if usage := mapField(ev, "usage"); usage != nil {
			cached := intField(usage, "cached_input_tokens")
			m.emit(Event{Type: EventUsage, Usage: &Usage{
				InputTokens:      max(intField(usage, "input_tokens")-cached, 0),
				OutputTokens:     intField(usage, "output_tokens"),
				CacheReadTokens:  cached,
				CacheWriteTokens: intField(usage, "cache_write_input_tokens"),
			}})
		}
		return true, nil
	case "turn.failed":
		msg := stringField(mapField(ev, "error"), "message")
		if msg == "" {
			msg = "turn failed"
		}
		return true, fmt.Errorf("codex: %s", msg)
	case "error":
		if msg := stringField(ev, "message"); msg != "" {
			return false, fmt.Errorf("codex: %s", msg)
		}
	}
	return false, nil
}

// codexTool describes a Codex item that represents a tool call, or returns nil
// for items that are not tools or that Aviary's MCP boundary already reports.
func codexTool(item map[string]any) *ToolEvent {
	id := stringField(item, "id")
	switch stringField(item, "type") {
	case "command_execution":
		return &ToolEvent{ID: id, Name: "shell", Args: map[string]any{"command": item["command"]}}
	case "file_change":
		return &ToolEvent{ID: id, Name: "file_change", Args: map[string]any{"changes": item["changes"]}}
	case "web_search":
		return &ToolEvent{ID: id, Name: "web_search", Args: map[string]any{"query": item["query"]}}
	case "mcp_tool_call":
		server := stringField(item, "server")
		if server == MCPServerName {
			return nil
		}
		args, _ := item["arguments"].(map[string]any)
		return &ToolEvent{ID: id, Name: server + "." + stringField(item, "tool"), Args: args}
	}
	return nil
}

// codexToolOutcome fills in the result or error of a completed tool item.
func codexToolOutcome(item map[string]any, tool *ToolEvent) {
	failed := stringField(item, "status") == "failed"
	switch stringField(item, "type") {
	case "command_execution":
		tool.Result = stringField(item, "aggregated_output")
		if code, ok := item["exit_code"].(float64); ok && code != 0 {
			failed = true
		}
	case "mcp_tool_call":
		if e := mapField(item, "error"); e != nil {
			tool.Error = stringField(e, "message")
		}
		if r := mapField(item, "result"); r != nil {
			tool.Result = toolResultText(r["content"])
		}
	default:
		tool.Result = stringField(item, "status")
	}
	if failed && tool.Error == "" {
		tool.Error = tool.Result
		if tool.Error == "" {
			tool.Error = "failed"
		}
		tool.Result = ""
	}
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
