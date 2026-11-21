package cliagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/process"
	"github.com/lsegal/aviary/internal/transcript"
)

// claudeMCPToolPrefix prefixes Claude Code's names for tools served by
// Aviary's MCP server.
const claudeMCPToolPrefix = "mcp__" + MCPServerName + "__"

// claudeSession keeps one long-lived `claude -p` process per session, fed
// turns through stream-json input so later turns skip startup cost.
type claudeSession struct {
	opts Options

	mu        sync.Mutex
	proc      *claudeProc
	sessionID string // set once the CLI reports it
}

type claudeProc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	lines  chan []byte
	stderr *tailBuffer
	done   chan struct{}
	err    error // set before done closes
}

func newClaudeSession(opts Options) *claudeSession {
	return &claudeSession{opts: opts, sessionID: opts.ResumeID}
}

// claudeArgs builds the command line for a Claude Code process. The system
// prompt is read from systemPromptFile rather than passed inline: with a skill
// expanded into it, it outgrows Windows' 32,767-character command line.
func claudeArgs(opts Options, resumeID, systemPromptFile string) []string {
	args := []string{
		"-p",
		"--input-format", "stream-json",
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--model", opts.Model,
		"--strict-mcp-config",
		"--mcp-config", claudeMCPConfig(opts),
	}
	if systemPromptFile != "" {
		args = append(args, "--system-prompt-file", systemPromptFile)
	}
	if !opts.BuiltinTools {
		args = append(args, "--tools", "")
	}
	if opts.MCPURL != "" {
		// Aviary's own tools were already granted by the agent's permissions
		// and are re-checked at Aviary's MCP boundary, so the CLI never asks
		// for them separately, in any approval mode.
		args = append(args, "--allowedTools", "mcp__"+MCPServerName)
	}
	args = append(args, opts.Approval...)
	args = append(args, opts.ExtraDir...)
	args = append(args, opts.Permissions...)
	if opts.BuiltinTools && opts.CommandHook != nil {
		args = append(args, "--settings", claudeHookSettings(opts.CommandHook))
	}
	switch {
	case opts.Ephemeral:
		args = append(args, "--no-session-persistence")
	case resumeID != "":
		args = append(args, "--resume", resumeID)
	case opts.NewSessionID != "":
		args = append(args, "--session-id", opts.NewSessionID)
	}
	return args
}

// claudeMCPConfig returns the --mcp-config JSON. The bearer token is passed
// through the environment so it never appears in the process list.
func claudeMCPConfig(opts Options) string {
	servers := map[string]any{}
	if opts.MCPURL != "" {
		servers[MCPServerName] = map[string]any{
			"type":    "http",
			"url":     opts.MCPURL,
			"headers": map[string]string{"Authorization": "Bearer ${" + mcpTokenEnv + "}"},
			"timeout": MCPToolTimeout.Milliseconds(),
		}
	}
	data, _ := json.Marshal(map[string]any{"mcpServers": servers})
	return string(data)
}

// mcpTokenEnv is the environment variable that carries the scoped MCP token.
const mcpTokenEnv = "AVIARY_MCP_TOKEN"

func cliEnv(opts Options) []string {
	env := append(os.Environ(), opts.Env...)
	if opts.MCPToken != "" {
		env = append(env, mcpTokenEnv+"="+opts.MCPToken)
	}
	if opts.CommandHook != nil {
		env = append(env, commandHookEnvValue(opts.CommandHook))
	}
	return env
}

// writeSystemPromptFile writes the system prompt to a temporary file for
// --system-prompt-file and returns its path and a cleanup that removes it. An
// empty prompt writes nothing.
func writeSystemPromptFile(prompt string) (string, func(), error) {
	if prompt == "" {
		return "", func() {}, nil
	}
	file, err := os.CreateTemp("", "aviary-system-prompt-*.md")
	if err != nil {
		return "", nil, fmt.Errorf("writing the system prompt file: %w", err)
	}
	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }
	_, err = file.WriteString(prompt)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("writing the system prompt file: %w", err)
	}
	return path, cleanup, nil
}

func (s *claudeSession) start() (proc *claudeProc, err error) {
	promptFile, cleanup, err := writeSystemPromptFile(s.opts.SystemPrompt)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	cmd := process.Command(context.Background(), s.opts.binary(), claudeArgs(s.opts, s.sessionID, promptFile)...)
	cmd.Dir = s.opts.WorkDir
	cmd.Env = cliEnv(s.opts)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	p := &claudeProc{
		cmd:    cmd,
		stdin:  stdin,
		lines:  make(chan []byte, 64),
		stderr: &tailBuffer{max: 4096},
		done:   make(chan struct{}),
	}
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting claude: %w", err)
	}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
		for sc.Scan() {
			line := append([]byte(nil), sc.Bytes()...)
			p.lines <- line
		}
		close(p.lines)
		p.err = cmd.Wait()
		cleanup()
		close(p.done)
	}()
	return p, nil
}

func (p *claudeProc) kill() {
	_ = p.stdin.Close()
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	for range p.lines { //nolint:revive // drain so the reader goroutine can exit
	}
	<-p.done
}

// exitError describes why the process ended.
func (p *claudeProc) exitError() error {
	<-p.done
	msg := strings.TrimSpace(p.stderr.String())
	switch {
	case msg != "":
		return fmt.Errorf("claude exited: %s", msg)
	case p.err != nil:
		return fmt.Errorf("claude exited: %w", p.err)
	default:
		return errors.New("claude exited before finishing the turn")
	}
}

// exited reports whether the process has already ended.
func (p *claudeProc) exited() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// discard kills the current process; the next turn starts a new one that
// resumes the session.
func (s *claudeSession) discard() {
	p := s.proc
	s.proc = nil
	if p != nil {
		p.kill()
	}
}

// deliver writes msg to the session's process, starting one first if there
// is none. When the write fails the process is discarded and the error says
// the process stopped, not how the pipe write failed.
func (s *claudeSession) deliver(msg []byte) (*claudeProc, error) {
	if s.proc == nil {
		p, err := s.start()
		if err != nil {
			return nil, err
		}
		s.proc = p
	}
	p := s.proc
	if _, err := p.stdin.Write(msg); err != nil {
		s.discard()
		if out := strings.TrimSpace(p.stderr.String()); out != "" {
			return nil, fmt.Errorf("claude stopped before it could take the turn: %s", out)
		}
		return nil, errors.New("claude stopped before it could take the turn")
	}
	return p, nil
}

func (s *claudeSession) Send(ctx context.Context, turn Turn, onEvent func(Event)) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msg, err := claudeUserMessage(turn)
	if err != nil {
		return Result{}, err
	}
	msg = append(msg, '\n')

	// A process kept between turns can die while idle (a crash, the system
	// reaping it). That is only discovered here, so heal it within the turn:
	// start a new process resuming the session and deliver the turn to it.
	reused := s.proc != nil
	if reused && s.proc.exited() {
		s.discard()
		reused = false
	}
	p, err := s.deliver(msg)
	if err != nil && reused {
		p, err = s.deliver(msg)
		reused = false
	}
	if err != nil {
		return Result{}, err
	}

	m := newClaudeMapper(onEvent)
	m.tools.refusalNote = s.opts.RefusalNote
	gotLine := false
	for {
		select {
		case <-ctx.Done():
			// Interrupting mid-turn leaves the process in an unknown state;
			// restart it (resuming the session) on the next turn.
			s.proc = nil
			p.kill()
			return Result{}, ctx.Err()
		case line, ok := <-p.lines:
			if !ok {
				s.proc = nil
				if reused && !gotLine {
					// The idle process died just as the turn reached it,
					// before answering; deliver the turn to a new one.
					p.kill()
					if p, err = s.deliver(msg); err != nil {
						return Result{}, err
					}
					reused = false
					continue
				}
				return Result{}, p.exitError()
			}
			gotLine = true
			res, done, err := m.handle(line)
			if m.sessionID != "" {
				s.sessionID = m.sessionID
			}
			if done || err != nil {
				return res, err
			}
		}
	}
}

func (s *claudeSession) Close() error {
	s.mu.Lock()
	p := s.proc
	s.proc = nil
	s.mu.Unlock()
	if p != nil {
		p.kill()
	}
	return nil
}

// claudeUserMessage encodes a turn as a stream-json user message.
func claudeUserMessage(turn Turn) ([]byte, error) {
	content := []map[string]any{}
	for _, mediaURL := range turn.MediaURLs {
		block, err := claudeImageBlock(mediaURL)
		if err != nil {
			return nil, err
		}
		content = append(content, block)
	}
	text := turn.Text
	if text == "" && len(content) == 0 {
		text = " "
	}
	if text != "" {
		content = append(content, map[string]any{"type": "text", "text": text})
	}
	return json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": content},
	})
}

func claudeImageBlock(mediaURL string) (map[string]any, error) {
	if strings.HasPrefix(mediaURL, "data:") {
		mediaType, data, err := splitDataURL(mediaURL)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"type":   "image",
			"source": map[string]any{"type": "base64", "media_type": mediaType, "data": data},
		}, nil
	}
	return map[string]any{
		"type":   "image",
		"source": map[string]any{"type": "url", "url": mediaURL},
	}, nil
}

// splitDataURL returns the media type and base64 payload of a data URL.
func splitDataURL(u string) (string, string, error) {
	header, data, ok := strings.Cut(strings.TrimPrefix(u, "data:"), ",")
	if !ok || !strings.HasSuffix(header, ";base64") {
		return "", "", errors.New("image must be a base64 data URL")
	}
	return strings.TrimSuffix(header, ";base64"), data, nil
}

// claudeMapper turns Claude Code stream-json lines into Events. The shared
// stream parser decodes messages, tools and usage; claudeMapper reads only
// the session ID, streamed text deltas and the turn's outcome.
type claudeMapper struct {
	emit      func(Event)
	sessionID string
	streamed  bool // text deltas seen for the current message
	stream    transcript.Mapper
	tools     *toolTranslator
}

func newClaudeMapper(emit func(Event)) *claudeMapper {
	if emit == nil {
		emit = func(Event) {}
	}
	return &claudeMapper{
		emit:   emit,
		stream: transcript.NewMapper(transcript.FormatClaudeStreamJSON),
		tools: newToolTranslator(emit, func(name string) bool {
			return strings.HasPrefix(name, claudeMCPToolPrefix) // reported by Aviary's MCP boundary
		}),
	}
}

// handle processes one line. It returns done once the turn's result arrives.
func (m *claudeMapper) handle(line []byte) (Result, bool, error) {
	var ev map[string]any
	if err := json.Unmarshal(line, &ev); err != nil {
		return Result{}, false, nil // ignore non-JSON noise
	}
	if id := stringField(ev, "session_id"); id != "" && id != m.sessionID {
		m.sessionID = id
		m.emit(Event{Type: EventSession, SessionID: id})
	}
	if stringField(ev, "type") == "stream_event" {
		inner := mapField(ev, "event")
		switch stringField(inner, "type") {
		case "message_start":
			m.streamed = false
		case "content_block_delta":
			delta := mapField(inner, "delta")
			if stringField(delta, "type") == "text_delta" {
				if text := stringField(delta, "text"); text != "" {
					m.streamed = true
					m.emit(Event{Type: EventText, Text: text})
				}
			}
		}
		return Result{}, false, nil
	}
	events := m.stream.Map(line)
	for _, event := range events {
		if event.Kind == transcript.KindMessage && !m.streamed {
			m.emit(Event{Type: EventText, Text: event.Text})
		}
	}
	m.tools.translate(events)
	if stringField(ev, "type") == "result" {
		text := stringField(ev, "result")
		if isErr, _ := ev["is_error"].(bool); isErr || (stringField(ev, "subtype") != "" && stringField(ev, "subtype") != "success") {
			return Result{}, true, claudeResultError(ev, text)
		}
		return Result{Text: text}, true, nil
	}
	return Result{}, false, nil
}

func claudeResultError(ev map[string]any, text string) error {
	if text != "" {
		return fmt.Errorf("claude: %s", text)
	}
	if errs, ok := ev["errors"].([]any); ok && len(errs) > 0 {
		parts := make([]string, 0, len(errs))
		for _, e := range errs {
			parts = append(parts, fmt.Sprint(e))
		}
		return fmt.Errorf("claude: %s", strings.Join(parts, "; "))
	}
	return fmt.Errorf("claude: turn failed (%s)", stringField(ev, "subtype"))
}

// tailBuffer keeps the last max bytes written to it.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
