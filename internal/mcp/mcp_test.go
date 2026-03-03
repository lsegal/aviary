package mcp

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/config"
)

func TestSetGetDeps(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })

	d := &Deps{}
	SetDeps(d)
	if GetDeps() != d {
		t.Fatal("expected GetDeps to return the deps set by SetDeps")
	}
}

func TestHelpers_TextJSONStubAndExtract(t *testing.T) {
	res, _, err := text("hello")
	if err != nil {
		t.Fatalf("text: %v", err)
	}
	if got := extractText(res); got != "hello" {
		t.Fatalf("unexpected extractText result: %q", got)
	}

	res, _, err = jsonResult(map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("jsonResult: %v", err)
	}
	if got := extractText(res); !strings.Contains(got, "\"ok\": true") {
		t.Fatalf("expected JSON output, got %q", got)
	}

	res, _, err = stub("x")
	if err != nil {
		t.Fatalf("stub: %v", err)
	}
	if got := extractText(res); !strings.Contains(got, "not yet implemented") {
		t.Fatalf("unexpected stub text: %q", got)
	}

	combined := extractText(&sdkmcp.CallToolResult{Content: []sdkmcp.Content{
		&sdkmcp.TextContent{Text: "a"},
		&sdkmcp.TextContent{Text: "b"},
	}})
	if combined != "ab" {
		t.Fatalf("expected concatenated text, got %q", combined)
	}

	if got := extractText(nil); got != "" {
		t.Fatalf("nil result should extract empty string, got %q", got)
	}
}

func TestDispatcherResolve_InProcess(t *testing.T) {
	d := NewDispatcher("https://localhost:16677", "")

	prevChecker := checkServerRunning
	prevLoader := loadStoredToken
	t.Cleanup(func() {
		checkServerRunning = prevChecker
		loadStoredToken = prevLoader
	})

	SetServerChecker(func() bool { return false })
	SetTokenLoader(func() (string, error) { return "", nil })

	c, err := d.Resolve(context.Background())
	if err != nil {
		t.Fatalf("resolve in-process: %v", err)
	}
	defer c.Close() //nolint:errcheck

	if _, ok := c.(*InProcessClient); !ok {
		t.Fatalf("expected in-process client, got %T", c)
	}
}

func TestDispatcherResolve_ServerRunningTokenLoadError(t *testing.T) {
	d := NewDispatcher("https://localhost:16677", "")

	prevChecker := checkServerRunning
	prevLoader := loadStoredToken
	t.Cleanup(func() {
		checkServerRunning = prevChecker
		loadStoredToken = prevLoader
	})

	SetServerChecker(func() bool { return true })
	SetTokenLoader(func() (string, error) { return "", errors.New("no token") })

	_, err := d.Resolve(context.Background())
	if err == nil || !strings.Contains(err.Error(), "loading token") {
		t.Fatalf("expected token loading error, got %v", err)
	}
}

func TestDispatcherCallTool_PingAndToolError(t *testing.T) {
	d := NewDispatcher("https://localhost:16677", "")
	oldDeps := GetDeps()
	SetDeps(&Deps{})
	t.Cleanup(func() { SetDeps(oldDeps) })

	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	out, err := d.CallTool(context.Background(), "ping", map[string]any{})
	if err != nil {
		t.Fatalf("call ping: %v", err)
	}
	if out != "pong" {
		t.Fatalf("expected pong, got %q", out)
	}

	out, err = d.CallTool(context.Background(), "agent_list", map[string]any{})
	if err != nil {
		t.Fatalf("agent_list should succeed with in-process deps init, got %v", err)
	}
	if strings.TrimSpace(out) == "" {
		t.Fatal("expected non-empty agent_list result")
	}
}

func TestInProcessClientWithRegisteredTools(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })

	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "alpha", Model: "x"}}})
	SetDeps(&Deps{Agents: mgr})

	c, err := NewInProcessClient(context.Background(), NewServer())
	if err != nil {
		t.Fatalf("new in-process client: %v", err)
	}
	defer c.Close() //nolint:errcheck

	res, err := c.CallTool(context.Background(), "agent_list", map[string]any{})
	if err != nil {
		t.Fatalf("agent_list: %v", err)
	}
	if txt := extractText(res); !strings.Contains(txt, "\"alpha\"") {
		t.Fatalf("expected agent list response to include alpha, got %q", txt)
	}
}

func TestHTTPHandlerServesRequest(t *testing.T) {
	h := HTTPHandler(NewServer())
	r := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)
	if w.Code == 0 {
		t.Fatal("expected a valid HTTP status code")
	}
}

func TestBearerTransportAddsAuthHeader(t *testing.T) {
	var seenAuth string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenAuth = r.Header.Get("Authorization")
		_, _ = io.WriteString(w, "ok")
	}))
	defer ts.Close()

	base := http.DefaultTransport
	tpt := &bearerTransport{base: base, token: "abc123"}
	client := &http.Client{Transport: tpt}

	req, err := http.NewRequest(http.MethodGet, ts.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck

	if seenAuth != "Bearer abc123" {
		t.Fatalf("expected Authorization header, got %q", seenAuth)
	}
}

func TestExtractToolCallFromPayload(t *testing.T) {
	t.Run("valid tool call", func(t *testing.T) {
		payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"agent_run","arguments":{"name":"bot","message":"hi"}}}`)
		name, args, ok := extractToolCallFromPayload(payload)
		if !ok {
			t.Fatal("expected payload to be parsed as tools/call")
		}
		if name != "agent_run" {
			t.Fatalf("expected tool name agent_run, got %q", name)
		}
		m, ok := args.(map[string]any)
		if !ok {
			t.Fatalf("expected args map, got %T", args)
		}
		if m["name"] != "bot" {
			t.Fatalf("expected name arg bot, got %v", m["name"])
		}
	})

	t.Run("non tool call", func(t *testing.T) {
		payload := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
		if _, _, ok := extractToolCallFromPayload(payload); ok {
			t.Fatal("expected non tools/call payload to be ignored")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		if _, _, ok := extractToolCallFromPayload([]byte(`{`)); ok {
			t.Fatal("expected invalid json to be ignored")
		}
	})
}

func TestRedactValue(t *testing.T) {
	input := map[string]any{
		"token":       "abc123",
		"message":     "hello",
		"nested":      map[string]any{"password": "p@ss", "safe": "ok"},
		"list":        []any{map[string]any{"client_secret": "xyz"}, "fine"},
		"authorization": "Bearer qwe",
	}

	got := redactValue("", input)
	gotMap, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T", got)
	}

	if gotMap["token"] != "[REDACTED]" {
		t.Fatalf("expected token to be redacted, got %v", gotMap["token"])
	}
	if gotMap["message"] != "hello" {
		t.Fatalf("expected safe field unchanged, got %v", gotMap["message"])
	}

	nested, ok := gotMap["nested"].(map[string]any)
	if !ok {
		t.Fatalf("expected nested map, got %T", gotMap["nested"])
	}
	if nested["password"] != "[REDACTED]" {
		t.Fatalf("expected nested password redacted, got %v", nested["password"])
	}
	if nested["safe"] != "ok" {
		t.Fatalf("expected nested safe value unchanged, got %v", nested["safe"])
	}

	list, ok := gotMap["list"].([]any)
	if !ok {
		t.Fatalf("expected list, got %T", gotMap["list"])
	}
	if len(list) != 2 {
		t.Fatalf("expected list length 2, got %d", len(list))
	}
	inner, ok := list[0].(map[string]any)
	if !ok {
		t.Fatalf("expected list[0] map, got %T", list[0])
	}
	if inner["client_secret"] != "[REDACTED]" {
		t.Fatalf("expected client_secret redacted, got %v", inner["client_secret"])
	}

	if gotMap["authorization"] != "[REDACTED]" {
		t.Fatalf("expected authorization redacted, got %v", gotMap["authorization"])
	}

	jsonText := redactedJSON(input)
	if strings.Contains(strings.ToLower(jsonText), "abc123") || strings.Contains(strings.ToLower(jsonText), "bearer qwe") || strings.Contains(strings.ToLower(jsonText), "p@ss") {
		t.Fatalf("redacted json leaked secret data: %s", jsonText)
	}

	if !reflect.DeepEqual(isSensitiveKey("token"), true) {
		t.Fatal("expected token key to be sensitive")
	}
}

// toolCallContains calls a tool and checks that the result (output or error) contains want.
func toolCallContains(t *testing.T, d *Dispatcher, tool string, args map[string]any, want string) {
	t.Helper()
	out, err := d.CallTool(context.Background(), tool, args)
	if err != nil {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("tool %s: expected %q in error, got %v", tool, want, err)
		}
		return
	}
	if !strings.Contains(out, want) {
		t.Errorf("tool %s: expected %q in output, got %q", tool, want, out)
	}
}

func TestBrowserTools_NilDeps(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{Browser: nil})
	t.Cleanup(func() { SetDeps(old) })

	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	d := NewDispatcher("https://localhost:16677", "")

	tests := []struct {
		tool string
		args map[string]any
	}{
		{"browser_open", map[string]any{"url": "https://example.com"}},
		{"browser_click", map[string]any{"tab_id": "x", "selector": "#btn"}},
		{"browser_keystroke", map[string]any{"tab_id": "x", "selector": "#inp", "text": "hi"}},
		{"browser_fill", map[string]any{"tab_id": "x", "selector": "#inp", "text": "hi"}},
		{"browser_screenshot", map[string]any{"tab_id": "x"}},
		{"browser_close", nil},
	}

	for _, tt := range tests {
		toolCallContains(t, d, tt.tool, tt.args, "browser manager not initialized")
	}
}

func TestIsStopCommand(t *testing.T) {
	positives := []string{"stop", "halt", "cancel", "abort", "STOP", "HALT", " cancel ", "/stop", "/halt", "/cancel", "/abort"}
	for _, c := range positives {
		if !isStopCommand(c) {
			t.Errorf("isStopCommand(%q) = false; want true", c)
		}
	}
	negatives := []string{"", "hello", "please stop", "stopper", "don't stop", "stopping"}
	for _, c := range negatives {
		if isStopCommand(c) {
			t.Errorf("isStopCommand(%q) = true; want false", c)
		}
	}
}

func TestAgentRun_StopCommand_NoActiveSession(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })

	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "assistant", Model: "stub"}}})
	SetDeps(&Deps{Agents: mgr})

	c, err := NewInProcessClient(context.Background(), NewServer())
	if err != nil {
		t.Fatalf("in-process client: %v", err)
	}
	defer c.Close() //nolint:errcheck

	// Sending "stop" when no session is active should return an informational message,
	// not an error.
	res, err := c.CallTool(context.Background(), "agent_run", map[string]any{
		"name":    "assistant",
		"message": "stop",
	})
	if err != nil {
		t.Fatalf("agent_run stop: unexpected error: %v", err)
	}
	out := extractText(res)
	if !strings.Contains(out, "no active") {
		t.Errorf("expected 'no active' in response, got %q", out)
	}
}

func TestSessionStop_NoActiveWork(t *testing.T) {
	c, err := NewInProcessClient(context.Background(), NewServer())
	if err != nil {
		t.Fatalf("in-process client: %v", err)
	}
	defer c.Close() //nolint:errcheck

	// Calling session_stop on an unknown session should report no active work.
	res, err := c.CallTool(context.Background(), "session_stop", map[string]any{
		"session_id": "nonexistent-session-id-xyz",
	})
	if err != nil {
		t.Fatalf("session_stop: unexpected error: %v", err)
	}
	out := extractText(res)
	if !strings.Contains(out, "no active") {
		t.Errorf("expected 'no active' message, got %q", out)
	}
}

func TestSessionList_IsProcessing(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })

	mgr := agent.NewManager(nil)
	mgr.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "assistant", Model: "stub"}}})
	SetDeps(&Deps{Agents: mgr})

	c, err := NewInProcessClient(context.Background(), NewServer())
	if err != nil {
		t.Fatalf("in-process client: %v", err)
	}
	defer c.Close() //nolint:errcheck

	// Listing sessions when no processing is happening should return is_processing=false.
	res, err := c.CallTool(context.Background(), "session_list", map[string]any{"agent": "assistant"})
	if err != nil {
		t.Fatalf("session_list: unexpected error: %v", err)
	}
	out := extractText(res)
	if !strings.Contains(out, "is_processing") {
		t.Errorf("expected is_processing in session_list output, got %q", out)
	}
	if strings.Contains(out, `"is_processing": true`) {
		t.Errorf("expected is_processing=false when idle, got %q", out)
	}
}

func TestBrowserTools_WithManager(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })

	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	// Use a real Manager with no Chrome running to verify routing through the dep.
	mgr := browser.NewManager("", 0, t.TempDir(), false)
	SetDeps(&Deps{Browser: mgr})

	d := NewDispatcher("https://localhost:16677", "")

	// Click without tab_id is rejected by the SDK schema validator before reaching
	// the handler; the error contains "tab_id" (the missing property name).
	toolCallContains(t, d, "browser_click", map[string]any{"selector": "#x"}, "tab_id")
	toolCallContains(t, d, "browser_fill", map[string]any{"selector": "#x", "text": "abc"}, "tab_id")

	// Close should succeed (no-op on a manager with no Chrome running).
	out, err := d.CallTool(context.Background(), "browser_close", nil)
	if err != nil {
		t.Fatalf("browser_close unexpected error: %v", err)
	}
	if !strings.Contains(out, "closed") {
		t.Fatalf("expected closed confirmation, got %q", out)
	}
}
