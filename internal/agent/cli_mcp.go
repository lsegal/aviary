package agent

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/domain"
)

// CLI-backed agents reach Aviary's tools through a scoped MCP endpoint. Each
// CLI session gets its own token, and that token's MCP server only registers
// the tools the agent is permitted to use, so a call to anything else is
// refused at the MCP boundary rather than merely hidden from the CLI. Every
// call still goes through the agent's ToolClient, which re-checks permissions
// and applies the agent's command and filesystem policies.
//
// The endpoint listens on loopback over plain HTTP. The main server's /mcp
// uses TLS with a self-signed certificate the CLIs do not trust, and its
// bearer token grants every tool.

// cliToolScope is the set of tools one CLI session may call.
type cliToolScope struct {
	token   string
	handler http.Handler

	callMu sync.Mutex // tool calls run one at a time, like the native loop

	turnMu sync.Mutex
	turn   *cliTurnState
}

// cliTurnState is the Aviary prompt a scope's tool calls currently belong to.
type cliTurnState struct {
	ctx       context.Context
	runner    *AgentRunner
	emit      func(StreamEvent)
	client    ToolClient
	sessionID string
	usage     *domain.UsageRecord
}

var cliMCP = struct {
	once   sync.Once
	url    string
	err    error
	mu     sync.Mutex
	scopes map[string]*cliToolScope
}{scopes: map[string]*cliToolScope{}}

// cliMCPURL returns the scoped MCP endpoint URL, starting its listener on
// first use.
func cliMCPURL() (string, error) {
	cliMCP.once.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			cliMCP.err = fmt.Errorf("starting CLI MCP listener: %w", err)
			return
		}
		mux := http.NewServeMux()
		mux.Handle("/mcp", http.HandlerFunc(serveCLIMCP))
		srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
		go func() {
			if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Warn("agent: CLI MCP listener stopped", "err", err)
			}
		}()
		cliMCP.url = "http://" + ln.Addr().String() + "/mcp"
	})
	return cliMCP.url, cliMCP.err
}

func serveCLIMCP(w http.ResponseWriter, r *http.Request) {
	token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || token == "" {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	cliMCP.mu.Lock()
	scope := cliMCP.scopes[token]
	cliMCP.mu.Unlock()
	if scope == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	scope.handler.ServeHTTP(w, r)
}

// newCLIToolScope registers a scope exposing exactly tools.
func newCLIToolScope(tools []ToolInfo) (*cliToolScope, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	scope := &cliToolScope{token: "aviary_cli_" + hex.EncodeToString(buf)}

	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "aviary", Version: buildinfo.Version}, nil)
	for _, t := range tools {
		schema := t.InputSchema
		if schema == nil {
			schema = map[string]any{"type": "object"}
		}
		name := t.Name
		server.AddTool(&sdkmcp.Tool{Name: name, Description: t.Description, InputSchema: schema},
			func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
				return scope.call(name, req)
			})
	}
	scope.handler = sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server },
		&sdkmcp.StreamableHTTPOptions{Stateless: true})

	cliMCP.mu.Lock()
	cliMCP.scopes[scope.token] = scope
	cliMCP.mu.Unlock()
	return scope, nil
}

// release revokes the scope's token.
func (s *cliToolScope) release() {
	cliMCP.mu.Lock()
	delete(cliMCP.scopes, s.token)
	cliMCP.mu.Unlock()
}

func (s *cliToolScope) setTurn(t *cliTurnState) {
	s.turnMu.Lock()
	s.turn = t
	s.turnMu.Unlock()
}

func (s *cliToolScope) currentTurn() *cliTurnState {
	s.turnMu.Lock()
	defer s.turnMu.Unlock()
	return s.turn
}

// call runs a tool on behalf of the CLI through the runner's normal tool path,
// which emits the chat tool events and records the call in session history.
func (s *cliToolScope) call(name string, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
	turn := s.currentTurn()
	if turn == nil {
		return nil, errors.New("no active Aviary turn for this CLI session")
	}
	args := map[string]any{}
	if req != nil && req.Params != nil && len(req.Params.Arguments) > 0 {
		if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
			return nil, fmt.Errorf("invalid arguments for %s: %w", name, err)
		}
	}

	s.callMu.Lock()
	defer s.callMu.Unlock()
	text, canceled := turn.runner.executeToolCall(turn.ctx, turn.emit, turn.client, turn.sessionID, turn.usage,
		toolEventRecord{Name: name, Args: args}, name, args)
	if canceled {
		return nil, context.Canceled
	}
	return &sdkmcp.CallToolResult{
		Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: text}},
		IsError: strings.HasPrefix(text, "error: "),
	}, nil
}
