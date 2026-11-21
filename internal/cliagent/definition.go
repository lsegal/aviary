package cliagent

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/process"
	"github.com/lsegal/aviary/internal/transcript"
)

// renderDefinition renders the argv for one mode of backend's CLI provider
// definition, or nothing when the registry in force has no such definition.
func renderDefinition(backend Backend, mode clidefs.Mode, values clidefs.Values) []string {
	definition, ok := backend.Definition()
	if !ok {
		return nil
	}
	return definition.Render(mode, values)
}

// definitionSession runs each turn as one process rendered from a CLI
// provider definition: its run template for the first turn and its resume
// template, with the stored session ID, after that, so later turns send only
// the new message. The session ID is assigned by Aviary or captured from the
// CLI's output, as the definition's session.assign says. Its output is read
// with the definition's transcript mapper, the one Code Loop jobs use.
//
// The definition's mcp block, when declared, says how each turn hands the CLI
// Aviary's MCP server, and its systemPrompt block how it hands the CLI the
// system prompt. Without an mcp block Aviary's own tools are not offered, and
// without a systemPrompt block the prompt opens the session's first turn.
type definitionSession struct {
	opts       Options
	definition clidefs.Definition

	mu        sync.Mutex
	sessionID string
}

func newDefinitionSession(opts Options, definition clidefs.Definition) *definitionSession {
	opts.Env = append(definition.EnvList(), opts.Env...)
	return &definitionSession{opts: opts, definition: definition, sessionID: opts.ResumeID}
}

// definitionArgs renders the command line for one turn. The chat's approval,
// extra-directory and permission arguments, followed by extra (its MCP and
// system prompt arguments), take the place of the template's approval
// arguments.
func definitionArgs(opts Options, definition clidefs.Definition, mode clidefs.Mode, session, prompt string, extra ...string) []string {
	approval := []string{}
	if opts.BuiltinTools {
		approval = append(approval, opts.Approval...)
		approval = append(approval, opts.ExtraDir...)
		approval = append(approval, opts.Permissions...)
	} else {
		// The CLI's own tools cannot be switched off through a definition;
		// its read-only mode is the closest it comes.
		approval = append(approval, definition.ApprovalArgs(clidefs.ApprovalReadOnly)...)
	}
	approval = append(approval, extra...)
	return definition.Render(mode, clidefs.Values{
		Prompt:       prompt,
		Session:      session,
		Model:        opts.Model,
		ApprovalArgs: approval,
		PromptStdin:  definition.ReadsPromptFromStdinIn(mode),
		NoCheckout:   true, // a chat's working directory need not be a checkout
	})
}

func (s *definitionSession) Send(ctx context.Context, turn Turn, onEvent func(Event)) (Result, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if onEvent == nil {
		onEvent = func(Event) {}
	}
	name := string(s.opts.Backend)

	mode, session := clidefs.ModeResume, s.sessionID
	assigned := ""
	if session == "" {
		mode = clidefs.ModeRun
		if s.definition.AssignsSessionID() && !s.opts.Ephemeral {
			assigned = s.opts.NewSessionID
			if assigned == "" {
				assigned = randomUUID()
			}
			session = assigned
		}
	}

	text := turn.Text
	var images []string
	for _, mediaURL := range turn.MediaURLs {
		path, err := writeTempImage(ctx, mediaURL)
		if err != nil {
			return Result{}, err
		}
		defer os.Remove(path) //nolint:errcheck
		images = append(images, path)
	}
	if len(images) > 0 {
		text = strings.TrimSpace(text + "\n\nAttached images: " + strings.Join(images, ", "))
	}
	if mode == clidefs.ModeRun && s.opts.SystemPrompt != "" && !s.definition.TakesSystemPrompt() {
		text = "<system_instructions>\n" + s.opts.SystemPrompt + "\n</system_instructions>\n\n" + text
	}
	if strings.TrimSpace(text) == "" {
		text = " "
	}

	extra, env, cleanup, err := s.wiring()
	if err != nil {
		return Result{}, err
	}
	defer cleanup()
	args := definitionArgs(s.opts, s.definition, mode, session, text, extra...)
	if len(args) == 0 {
		return Result{}, fmt.Errorf("the %s CLI definition renders no %s arguments", name, mode)
	}
	cmd := process.Command(ctx, s.opts.binary(), args...)
	cmd.Dir = s.opts.WorkDir
	cmd.Env = append(cliEnv(s.opts), env...)
	if s.definition.ReadsPromptFromStdinIn(mode) {
		cmd.Stdin = strings.NewReader(text)
	}
	capture := &sessionCapture{pattern: s.definition.SessionPattern()}
	stderr := &tailBuffer{max: 4096}
	cmd.Stderr = io.MultiWriter(stderr, capture)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return Result{}, err
	}
	if err := cmd.Start(); err != nil {
		return Result{}, fmt.Errorf("starting %s: %w", name, err)
	}

	m := newDefinitionMapper(s.definition, s.aviaryToolPrefix(), onEvent)
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64*1024), 64*1024*1024)
	var output tailBuffer
	output.max = 4096
	for sc.Scan() {
		line := sc.Bytes()
		capture.line(line)
		output.Write(line)         //nolint:errcheck
		output.Write([]byte{'\n'}) //nolint:errcheck
		m.handle(line)
	}
	_, _ = io.Copy(io.Discard, stdout)
	m.flush()
	waitErr := cmd.Wait()
	if ctx.Err() != nil {
		return Result{}, ctx.Err()
	}
	if waitErr != nil || (mode == clidefs.ModeResume && m.messages == 0 && s.missingSession(stderr.String()+output.String())) {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" && m.messages == 0 {
			msg = strings.TrimSpace(output.String())
		}
		switch {
		case msg != "":
			return Result{}, fmt.Errorf("%s exited: %s", name, msg)
		case waitErr != nil:
			return Result{}, fmt.Errorf("%s exited: %w", name, waitErr)
		default:
			return Result{}, fmt.Errorf("%s exited without resuming its session", name)
		}
	}
	id := capture.captured()
	if id == "" {
		id = assigned
	}
	if id != "" && id != s.sessionID {
		s.sessionID = id
		onEvent(Event{Type: EventSession, SessionID: id})
	}
	return Result{Text: m.lastMessage}, nil
}

// servesMCP reports whether this session hands the CLI Aviary's MCP server.
func (s *definitionSession) servesMCP() bool {
	return s.opts.MCPURL != "" && s.definition.TakesMCP()
}

// aviaryToolPrefix is how the CLI's output names Aviary's MCP tools, or empty
// when the session does not serve them.
func (s *definitionSession) aviaryToolPrefix() string {
	if !s.servesMCP() {
		return ""
	}
	return s.definition.MCPToolPrefix(MCPServerName)
}

// wiring renders one turn's MCP and system prompt arguments and environment
// from the definition, writing the files they name. cleanup removes them.
func (s *definitionSession) wiring() (args, env []string, cleanup func(), err error) {
	var cleanups []func()
	cleanup = func() {
		for _, c := range cleanups {
			c()
		}
	}
	defer func() {
		if err != nil {
			cleanup()
		}
	}()
	if s.servesMCP() {
		values := clidefs.MCPValues{
			Name:     MCPServerName,
			URL:      s.opts.MCPURL,
			TokenEnv: mcpTokenEnv,
			AuthEnv:  mcpAuthEnv,
			Timeout:  MCPToolTimeout,
		}
		if s.definition.MCPWritesFile() {
			data, err := s.definition.MCPFile(values)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("rendering the MCP settings file: %w", err)
			}
			path, remove, err := writeTempFile("aviary-mcp-*.json", data)
			if err != nil {
				return nil, nil, nil, fmt.Errorf("writing the MCP settings file: %w", err)
			}
			cleanups = append(cleanups, remove)
			values.File = path
		}
		args = append(args, s.definition.MCPArgs(values)...)
		env = append(env, s.definition.MCPEnv(values)...)
	}
	if s.opts.SystemPrompt != "" && s.definition.TakesSystemPrompt() {
		values := clidefs.SystemPromptValues{Prompt: s.opts.SystemPrompt}
		if s.definition.SystemPromptWritesFile() {
			path, remove, err := writeSystemPromptFile(s.opts.SystemPrompt)
			if err != nil {
				return nil, nil, nil, err
			}
			cleanups = append(cleanups, remove)
			values.File = path
		}
		args = append(args, s.definition.SystemPromptArgs(values)...)
		env = append(env, s.definition.SystemPromptEnv(values)...)
	}
	return args, env, cleanup, nil
}

// missingSession reports whether output says the CLI no longer holds the
// session it was asked to resume.
func (s *definitionSession) missingSession(output string) bool {
	lower := strings.ToLower(output)
	for _, phrase := range s.definition.MissingSessionPatterns() {
		if phrase != "" && strings.Contains(lower, strings.ToLower(phrase)) {
			return true
		}
	}
	return false
}

func (s *definitionSession) Close() error { return nil }

// sessionCapture reads a CLI-reported session ID out of its output with the
// definition's session.capture expression, from stdout lines and stderr
// alike. stderr is written from its own goroutine, so the ID is guarded and
// read once the process ends.
type sessionCapture struct {
	pattern *regexp.Regexp

	mu      sync.Mutex
	id      string
	partial []byte
}

func (c *sessionCapture) line(line []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.match(line)
}

// match records the ID line reports. Called with c.mu held.
func (c *sessionCapture) match(line []byte) {
	if c.pattern == nil {
		return
	}
	if match := c.pattern.FindSubmatch(line); len(match) >= 2 && len(match[1]) > 0 {
		c.id = string(match[1])
	}
}

// Write splits stderr into lines for match.
func (c *sessionCapture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partial = append(c.partial, p...)
	for {
		i := bytes.IndexByte(c.partial, '\n')
		if i < 0 {
			break
		}
		c.match(c.partial[:i])
		c.partial = c.partial[i+1:]
	}
	if len(c.partial) > 64*1024 {
		c.partial = c.partial[len(c.partial)-64*1024:]
	}
	return len(p), nil
}

// captured returns the last ID the output reported.
func (c *sessionCapture) captured() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.id
}

// definitionMapper turns a CLI's output into Events with the transcript
// mapper its definition names. Output in a format without one is read as
// plain text.
type definitionMapper struct {
	emit        func(Event)
	stream      transcript.Mapper
	tools       *toolTranslator
	messages    int
	lastMessage string
	text        strings.Builder
}

// newDefinitionMapper builds the mapper for one turn. Tools whose names
// start with aviaryPrefix are served by Aviary's MCP boundary, which reports
// them itself, so they are left out; an empty prefix leaves out none.
func newDefinitionMapper(definition clidefs.Definition, aviaryPrefix string, emit func(Event)) *definitionMapper {
	return &definitionMapper{
		emit:   emit,
		stream: transcript.NewMapper(definition.Output.TranscriptFormat()),
		tools: newToolTranslator(emit, func(name string) bool {
			return aviaryPrefix != "" && strings.HasPrefix(name, aviaryPrefix)
		}),
	}
}

func (m *definitionMapper) handle(line []byte) {
	if m.stream == nil {
		if m.text.Len() > 0 {
			m.text.WriteByte('\n')
			m.emit(Event{Type: EventText, Text: "\n"})
		}
		m.text.Write(line)
		m.emit(Event{Type: EventText, Text: string(line)})
		m.messages = 1
		m.lastMessage = m.text.String()
		return
	}
	m.events(m.stream.Map(line))
}

// flush reports what a mapper still held back when the output ended, such as
// a message streamed as deltas.
func (m *definitionMapper) flush() {
	if flusher, ok := m.stream.(transcript.Flusher); ok {
		m.events(flusher.Flush())
	}
}

func (m *definitionMapper) events(events []transcript.Event) {
	for _, event := range events {
		if event.Kind != transcript.KindMessage || strings.TrimSpace(event.Text) == "" {
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
}

// randomUUID returns a random RFC 4122 version 4 UUID.
func randomUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString(b[:])
	return h[:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:]
}
