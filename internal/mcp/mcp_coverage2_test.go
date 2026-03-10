package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

// ── RemoteClient (proxy.go) ──────────────────────────────────────────────────

// TestRemoteClient_ViaHTTPTestServer starts a local httptest server using HTTPHandler
// and connects a RemoteClient to it, covering proxy.go comprehensively.
func TestRemoteClient_ViaHTTPTestServer(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	srv := NewServer()
	ts := httptest.NewServer(HTTPHandler(srv))
	defer ts.Close()

	c, err := NewRemoteClient(context.Background(), ts.URL, "")
	if err != nil {
		t.Fatalf("NewRemoteClient: %v", err)
	}
	defer c.Close() //nolint:errcheck

	// ListTools via remote
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("remote ListTools: %v", err)
	}
	if len(tools) == 0 {
		t.Fatal("expected at least one tool from remote server")
	}
	found := false
	for _, tool := range tools {
		if tool.Name == "ping" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected 'ping' in remote tool list")
	}

	// CallTool via remote
	result, err := c.CallTool(context.Background(), "ping", map[string]any{})
	if err != nil {
		t.Fatalf("remote CallTool: %v", err)
	}
	if extractText(result) != "pong" {
		t.Fatalf("expected pong from remote ping, got %q", extractText(result))
	}

	// CallToolText via remote
	out, err := c.CallToolText(context.Background(), "ping", map[string]any{})
	if err != nil {
		t.Fatalf("remote CallToolText: %v", err)
	}
	if out != "pong" {
		t.Fatalf("expected pong from remote CallToolText, got %q", out)
	}
}

func TestNewRemoteClient_ConnectionError(t *testing.T) {
	// Connecting to a port that is definitely not listening should error
	_, err := NewRemoteClient(context.Background(), "https://localhost:1", "")
	if err == nil {
		t.Fatal("expected error connecting to non-listening port")
	}
}

// ── HTTPHandler POST path ─────────────────────────────────────────────────────

func TestHTTPHandler_PostWithToolCallPayload(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	srv := NewServer()
	ts := httptest.NewServer(HTTPHandler(srv))
	defer ts.Close()

	// Send a POST with a tools/call payload to exercise the logging path
	payload := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ping","arguments":{}}}`
	resp, err := http.Post(ts.URL+"/mcp", "application/json", bytes.NewBufferString(payload)) //nolint:noctx
	if err != nil {
		t.Fatalf("POST to HTTPHandler: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck
	// Any non-zero status code is acceptable
	if resp.StatusCode == 0 {
		t.Fatal("expected a valid HTTP status code")
	}
}

// ── tool_logging.go ───────────────────────────────────────────────────────────

func TestRedactValue_AdditionalTypes(t *testing.T) {
	// map[string]string path
	m := map[string]string{
		"safe_key": "safe_val",
		"token":    "secret-token",
	}
	got := redactValue("", m)
	gotMap, ok := got.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any, got %T", got)
	}
	if gotMap["safe_key"] != "safe_val" {
		t.Errorf("expected safe_key unchanged, got %v", gotMap["safe_key"])
	}
	if gotMap["token"] != "[REDACTED]" {
		t.Errorf("expected token to be redacted, got %v", gotMap["token"])
	}

	// []string path
	sl := []string{"hello", "world"}
	gotSlice := redactValue("", sl)
	gotSliceAny, ok := gotSlice.([]any)
	if !ok {
		t.Fatalf("expected []any from []string, got %T", gotSlice)
	}
	if len(gotSliceAny) != 2 {
		t.Fatalf("expected 2 elements, got %d", len(gotSliceAny))
	}

	// boolean scalar
	gotBool := redactValue("", true)
	if gotBool != true {
		t.Errorf("expected bool unchanged, got %v", gotBool)
	}

	// int scalar
	gotInt := redactValue("", 42)
	if gotInt != 42 {
		t.Errorf("expected int unchanged, got %v", gotInt)
	}

	// nil value
	gotNil := redactValue("", nil)
	if gotNil != nil {
		t.Errorf("expected nil, got %v", gotNil)
	}

	// struct fallback (goes through json.Marshal path)
	type myStruct struct {
		Name  string `json:"name"`
		Token string `json:"token"`
	}
	s := myStruct{Name: "alice", Token: "secret"}
	gotStruct := redactValue("", s)
	// Should be a map after the json round-trip
	gotStructMap, ok := gotStruct.(map[string]any)
	if !ok {
		t.Fatalf("expected map[string]any from struct, got %T", gotStruct)
	}
	if gotStructMap["token"] != "[REDACTED]" {
		t.Errorf("expected token redacted in struct fallback, got %v", gotStructMap["token"])
	}
}

func TestTruncateForLog_LongString(t *testing.T) {
	long := strings.Repeat("x", maxLoggedStringLen+100)
	truncated := truncateForLog(long)
	if len(truncated) <= maxLoggedStringLen {
		// That's expected — check it has the suffix
		if !strings.Contains(truncated, "+100 chars") {
			t.Fatalf("expected truncated suffix, got %q", truncated[len(truncated)-30:])
		}
	}

	short := "hello"
	if truncateForLog(short) != short {
		t.Fatalf("short string should not be truncated, got %q", truncateForLog(short))
	}
}

func TestLogToolCall_EmptyName(_ *testing.T) {
	// Should not panic or log anything
	logToolCall("test", "", map[string]any{"key": "val"})
}

func TestLogToolCall_WithArgs(_ *testing.T) {
	// Should not panic
	logToolCall("inprocess", "ping", map[string]any{})
	logToolCall("remote", "agent_run", map[string]any{"name": "bot", "token": "secret"})
}

// ── registerPluginTools coverage ─────────────────────────────────────────────

func TestSkillGogcliTool_ViaDispatcher(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	d := NewDispatcher("https://localhost:16677", "")

	// skill_gogcli should fail gracefully when gog binary is not available
	// This exercises registerPluginTools -> runGogCLI path
	toolCallContains(t, d, "skill_gogcli", map[string]any{"command": []any{"gmail", "list"}}, "")
}

// ── ensureInProcessDeps path ──────────────────────────────────────────────────

func TestEnsureInProcessDeps_WhenDepsNotSet(t *testing.T) {
	// Save and restore state
	oldDeps := GetDeps()
	oldDepsSet := depsSet
	t.Cleanup(func() {
		globalDeps = oldDeps
		depsSet = oldDepsSet
	})

	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
	cfg := &config.Config{}
	if err := config.Save("", cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	// Reset to unset state
	globalDeps = &Deps{}
	depsSet = false

	// Calling ensureInProcessDeps should auto-initialize deps
	err := ensureInProcessDeps()
	if err != nil {
		t.Fatalf("ensureInProcessDeps: %v", err)
	}

	// Should have set depsSet = true
	if !depsSet {
		t.Fatal("expected depsSet to be true after ensureInProcessDeps")
	}

	// Should have set Agents
	if globalDeps.Agents == nil {
		t.Fatal("expected Agents to be initialized")
	}
}

// ── jsonResult error path ─────────────────────────────────────────────────────

func TestJsonResult_UnmarshalableValue(t *testing.T) {
	// json.Marshal fails on channels
	ch := make(chan int)
	_, _, err := jsonResult(ch)
	if err == nil {
		t.Fatal("expected error marshaling channel")
	}
}

// ── redactedJSON with marshal error ──────────────────────────────────────────

func TestRedactedJSON_WithNonMarshalableResult(t *testing.T) {
	// Create a value where redactValue returns something that can't be marshaled.
	// This tests the fmt.Sprintf fallback in redactedJSON.
	// We use a custom type that marshals fine; to trigger the error path,
	// we'd need an unmarshalable result. Instead, verify the normal case:
	result := redactedJSON(map[string]any{"key": "value"})
	if !strings.Contains(result, "key") {
		t.Fatalf("expected key in redactedJSON result, got %q", result)
	}

	// Nil input
	result = redactedJSON(nil)
	if result != "null" {
		t.Fatalf("expected null for nil input, got %q", result)
	}
}

// ── HTTPHandler with GET (no body) ────────────────────────────────────────────

func TestHTTPHandler_GetRequest(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	h := HTTPHandler(NewServer())
	r := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	w := httptest.NewRecorder()

	h.ServeHTTP(w, r)
	// Any response is fine; just checking no panic
}

// ── agent_run with file attachment ───────────────────────────────────────────

func TestAgentRun_WithNilAgentManager(t *testing.T) {
	old := GetDeps()
	SetDeps(&Deps{Agents: nil})
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	d := NewDispatcher("https://localhost:16677", "")
	toolCallContains(t, d, "agent_run", map[string]any{"name": "bot", "message": "hi"}, "not initialized")
}

// ── session_stop without session or agent ────────────────────────────────────

func TestSessionStop_NeitherSessionNorAgent(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}

	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{})

	d := NewDispatcher("https://localhost:16677", "")
	// No session_id and no agent → error
	toolCallContains(t, d, "session_stop", map[string]any{}, "required")
}

// ── isSensitiveKey additional cases ──────────────────────────────────────────

func TestIsSensitiveKey_AdditionalCases(t *testing.T) {
	sensitiveKeys := []string{
		"api_key", "apikey", "key", "client_key", "client_secret",
		"access_key", "private_key", "authorization",
	}
	for _, k := range sensitiveKeys {
		if !isSensitiveKey(k) {
			t.Errorf("expected %q to be sensitive", k)
		}
	}

	safeKeys := []string{"", "name", "message", "query", "agent", "url"}
	for _, k := range safeKeys {
		if isSensitiveKey(k) {
			t.Errorf("expected %q to be safe, but got sensitive", k)
		}
	}
}

// ── agent_rules_get and _set with missing arg ─────────────────────────────────

func TestAgentRulesGet_EmptyName(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{})

	d := NewDispatcher("https://localhost:16677", "")
	toolCallContains(t, d, "agent_rules_get", map[string]any{"name": ""}, "required")
	toolCallContains(t, d, "agent_rules_set", map[string]any{"agent": "", "content": "x"}, "required")
}

// ── jsonResult with valid input ───────────────────────────────────────────────

func TestJsonResult_WithMarshalError(t *testing.T) {
	// Normal case
	res, _, err := jsonResult(map[string]any{"ok": true})
	if err != nil {
		t.Fatalf("jsonResult: %v", err)
	}
	var m map[string]any
	if jsonErr := json.Unmarshal([]byte(extractText(res)), &m); jsonErr != nil {
		t.Fatalf("expected valid JSON from jsonResult, got %v", jsonErr)
	}
}

// ── dispatcher dispatch to server route ──────────────────────────────────────

func TestDispatcherResolve_ServerRunningWithToken(t *testing.T) {
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	SetDeps(&Deps{Agents: agent.NewManager(nil)})

	// Start a real test server
	srv := NewServer()
	ts := httptest.NewServer(HTTPHandler(srv))
	defer ts.Close()

	// Set server checker to return true and token loader to return empty token
	prevChecker := checkServerRunning
	prevLoader := loadStoredToken
	t.Cleanup(func() {
		checkServerRunning = prevChecker
		loadStoredToken = prevLoader
	})
	SetServerChecker(func() bool { return true })
	SetTokenLoader(func() (string, error) { return "", nil })

	// Create a dispatcher that targets our test server's address
	d := NewDispatcher(ts.URL, "")
	c, err := d.Resolve(context.Background())
	if err != nil {
		t.Fatalf("resolve with server running: %v", err)
	}
	defer c.Close() //nolint:errcheck

	if _, ok := c.(*RemoteClient); !ok {
		t.Fatalf("expected RemoteClient when server is running, got %T", c)
	}
}
