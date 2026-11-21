// Package cliagent runs agent turns through a coding-agent CLI (Claude Code or
// Codex) instead of a model API. The CLI owns the agent loop; Aviary exposes
// its tools to the CLI over MCP and receives the CLI's JSON event stream as
// backend-neutral events.
package cliagent

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Backend identifies a supported agent CLI.
type Backend string

// Supported backends.
const (
	Claude Backend = "claude"
	Codex  Backend = "codex"
)

// Backends lists every supported backend.
var Backends = []Backend{Claude, Codex}

// ModelPrefix is the provider prefix that selects a CLI backend in a model
// string such as "cli/claude/opus". A backend's own name selects it too, as
// in "claude/opus": each CLI is a provider in its own right.
const ModelPrefix = "cli/"

// MCPServerName is the name Aviary's MCP server is registered under in the
// CLI's MCP configuration. Tool calls against it are reported by Aviary's MCP
// boundary rather than mapped from the CLI's event stream.
const MCPServerName = "aviary"

// MCPToolTimeout is how long a CLI waits on one call to Aviary's MCP server.
// The CLIs give up on a tool call after 60 seconds by default, while tools
// such as project_issue_create run a whole gh-create (issue #710), so it
// covers the longest tool rather than the CLI's default.
const MCPToolTimeout = 30 * time.Minute

// IsCLIModel reports whether model selects a CLI backend, either as
// "cli/<backend>/<model>" or as "<backend>/<model>".
func IsCLIModel(model string) bool {
	model = strings.TrimSpace(model)
	if strings.HasPrefix(model, ModelPrefix) {
		return true
	}
	provider, _, ok := strings.Cut(model, "/")
	return ok && Backend(provider).Valid()
}

// ParseModel splits "cli/<backend>/<model>" or "<backend>/<model>" into its
// backend and the model name passed to that CLI.
func ParseModel(model string) (Backend, string, error) {
	model = strings.TrimSpace(model)
	rest, prefixed := strings.CutPrefix(model, ModelPrefix)
	backend, name, ok := strings.Cut(rest, "/")
	if !prefixed && !Backend(backend).Valid() {
		return "", "", fmt.Errorf("model %q is not a CLI model", model)
	}
	if !ok || backend == "" || strings.TrimSpace(name) == "" {
		return "", "", fmt.Errorf("invalid CLI model %q: expected <claude|codex>/<model> or cli/<claude|codex>/<model>", model)
	}
	b := Backend(backend)
	if !b.Valid() {
		return "", "", fmt.Errorf("unknown CLI backend %q in model %q; must be claude or codex", backend, model)
	}
	return b, name, nil
}

// Valid reports whether b is a supported backend.
func (b Backend) Valid() bool {
	return b == Claude || b == Codex
}

// Binary returns the executable name for the backend.
func (b Backend) Binary() string {
	return string(b)
}

// Options configures a CLI session. Options are fixed for the life of a
// session; changing any of them requires a new session.
type Options struct {
	Backend      Backend
	Model        string
	SystemPrompt string
	WorkDir      string
	// MCPURL and MCPToken point the CLI at Aviary's scoped MCP endpoint.
	// When MCPURL is empty the CLI gets no MCP servers at all.
	MCPURL   string
	MCPToken string
	// BuiltinTools enables the CLI's own shell, file-edit and web tools.
	// When false they are turned off and only Aviary's MCP tools remain.
	BuiltinTools bool
	// Approval are the CLI's native permission flags for the surface's
	// approval mode, as its CLI provider definition translates it. They are
	// always passed, so the user's own CLI settings never pick the mode.
	Approval []string
	// ExtraDir are the CLI's native arguments that add the agent's own
	// directory to the session's workspace scope beside WorkDir, as its CLI
	// provider definition declares them. They widen which directories are in
	// scope, never what the approval mode allows there.
	ExtraDir []string
	// Permissions are the CLI's native allow and deny arguments carrying the
	// agent's exec and filesystem rules, as its CLI provider definition
	// translates them for the approval mode.
	Permissions []string
	// RefusalNote, when set, is added to a CLI tool's error when the CLI's
	// own permission check refused the call, naming that layer.
	RefusalNote string
	// CommandHook, when set with BuiltinTools, has the CLI check each of its
	// shell tool calls against the agent's Allowed Exec Commands. Only
	// claude honors it.
	CommandHook *CommandHook
	// ResumeID is the CLI-native session ID to resume. Empty starts a new
	// session; the new ID is reported with an EventSession event.
	ResumeID string
	// NewSessionID, when set, is used as the ID of a new Claude Code session.
	NewSessionID string
	// Ephemeral runs without persisting the CLI session, so it cannot be
	// resumed later.
	Ephemeral bool
	// Binary overrides the executable path (tests).
	Binary string
	// Env is appended to the CLI's environment.
	Env []string
}

func (o Options) binary() string {
	if o.Binary != "" {
		return o.Binary
	}
	return o.Backend.Binary()
}

// Turn is one user message handed to the CLI.
type Turn struct {
	Text string
	// MediaURLs are images attached to the message, as data URLs or remote
	// URLs.
	MediaURLs []string
}

// EventType identifies a backend-neutral CLI event.
type EventType string

// EventType values.
const (
	// EventText carries a chunk of assistant text.
	EventText EventType = "text"
	// EventToolStart reports that the CLI started one of its own tools.
	EventToolStart EventType = "tool_start"
	// EventToolResult reports the result of one of the CLI's own tools.
	EventToolResult EventType = "tool_result"
	// EventSession reports the CLI-native session ID.
	EventSession EventType = "session"
	// EventUsage reports token usage and cost for the turn.
	EventUsage EventType = "usage"
)

// Event is a single event from a CLI turn.
type Event struct {
	Type      EventType
	Text      string
	SessionID string
	Tool      *ToolEvent
	Usage     *Usage
}

// ToolEvent describes a CLI built-in tool invocation.
type ToolEvent struct {
	ID     string
	Name   string
	Args   map[string]any
	Result string
	Error  string
}

// Usage holds token counts and cost reported by the CLI.
type Usage struct {
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	CostUSD          float64
}

// Result is the outcome of a completed turn.
type Result struct {
	// Text is the final assistant message.
	Text string
}

// Session runs turns against one CLI conversation.
type Session interface {
	// Send runs one turn. onEvent is called synchronously for every event.
	// Send returns once the CLI finishes the turn. Turns must not overlap.
	Send(ctx context.Context, turn Turn, onEvent func(Event)) (Result, error)
	// Close stops any process the session holds.
	Close() error
}

// NewSession creates a session for opts.Backend. No process is started until
// the first Send.
func NewSession(opts Options) (Session, error) {
	switch opts.Backend {
	case Claude:
		return newClaudeSession(opts), nil
	case Codex:
		return newCodexSession(opts), nil
	default:
		return nil, fmt.Errorf("unknown CLI backend %q", opts.Backend)
	}
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func mapField(m map[string]any, key string) map[string]any {
	v, _ := m[key].(map[string]any)
	return v
}
