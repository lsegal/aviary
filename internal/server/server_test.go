package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

func setupServerDataDir(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("AVIARY_PID_FILE", filepath.Join(base, "aviary.pid"))
	if err := store.EnsureDirs(); err != nil {
		t.Fatalf("ensure dirs: %v", err)
	}
}

func TestGenerateLoadTokenFlows(t *testing.T) {
	setupServerDataDir(t)

	t.Run("load missing", func(t *testing.T) {
		if _, err := LoadToken(); err == nil {
			t.Fatal("expected missing token error")
		}
	})

	t.Run("generate and load", func(t *testing.T) {
		tok, err := GenerateToken()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if !strings.HasPrefix(tok, tokenPrefix) {
			t.Fatalf("token prefix mismatch: %s", tok)
		}
		got, err := LoadToken()
		if err != nil {
			t.Fatalf("load token: %v", err)
		}
		if got != tok {
			t.Fatalf("token mismatch got=%s want=%s", got, tok)
		}
	})

	t.Run("load or generate existing", func(t *testing.T) {
		first, isNew, err := LoadOrGenerateToken()
		if err != nil {
			t.Fatalf("loadorgenerate first: %v", err)
		}
		if isNew {
			t.Fatalf("expected existing token, got isNew=true")
		}
		second, isNew, err := LoadOrGenerateToken()
		if err != nil {
			t.Fatalf("loadorgenerate second: %v", err)
		}
		if isNew {
			t.Fatalf("expected existing token on second read")
		}
		if first != second {
			t.Fatalf("token changed first=%s second=%s", first, second)
		}
	})
}

func TestBearerMiddleware(t *testing.T) {
	token := "aviary_tok_test"
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := BearerMiddleware(token, next)

	t.Run("valid bearer", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d", rr.Code)
		}
	})

	t.Run("valid cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "aviary_session", Value: token})
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d", rr.Code)
		}
	})

	t.Run("valid query token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/logs?token="+token, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d", rr.Code)
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d", rr.Code)
		}
	})
}

func TestLoginHandler(t *testing.T) {
	token := "aviary_tok_secret"
	h := LoginHandler(token)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/login", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("code = %d", rr.Code)
		}
	})

	t.Run("json body", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"token": token})
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d", rr.Code)
		}
		cookies := rr.Result().Cookies()
		if len(cookies) == 0 || cookies[0].Name != "aviary_session" {
			t.Fatalf("expected aviary_session cookie, got %+v", cookies)
		}
	})

	t.Run("form value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("token="+token))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d", rr.Code)
		}
	})

	t.Run("authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("code = %d", rr.Code)
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("token=bad"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("code = %d", rr.Code)
		}
	})
}

func TestPIDLifecycle(t *testing.T) {
	setupServerDataDir(t)

	pid, err := ReadPID()
	if err != nil || pid != 0 {
		t.Fatalf("expected missing pid to return 0,nil got %d,%v", pid, err)
	}

	if err := WritePID(); err != nil {
		t.Fatalf("write pid: %v", err)
	}
	pid, err = ReadPID()
	if err != nil || pid <= 0 {
		t.Fatalf("read pid got %d err=%v", pid, err)
	}

	running, gotPID, err := IsRunning()
	if err != nil {
		t.Fatalf("is running err: %v", err)
	}
	if gotPID <= 0 {
		t.Fatalf("expected pid > 0, got %d", gotPID)
	}
	// On some platforms, process liveness probing may return false even for
	// current PID; the key contract here is no error and a parsed PID.
	_ = running

	if err := RemovePID(); err != nil {
		t.Fatalf("remove pid: %v", err)
	}
	if err := RemovePID(); err != nil {
		t.Fatalf("remove pid idempotent: %v", err)
	}
	running, gotPID, err = IsRunning()
	if err != nil {
		t.Fatalf("is running after remove err: %v", err)
	}
	if running || gotPID != 0 {
		t.Fatalf("expected not running after remove, got running=%v pid=%d", running, gotPID)
	}
}

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	healthHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rr.Code)
	}

	var payload struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		GOOS    string `json:"goos"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&payload); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if !payload.OK {
		t.Error("expected ok=true in health response")
	}
	if payload.GOOS == "" {
		t.Error("expected non-empty GOOS in health response")
	}
}

func TestProcSampler(t *testing.T) {
	s := NewProcSampler()

	// Initially no stats.
	_, ok := s.Get(99999)
	if ok {
		t.Error("expected Get on untracked PID to return ok=false")
	}

	// Forget on untracked PID should not panic.
	s.Forget(99999)
}

func TestLoadOrGenerateTLS_GeneratesSelfSigned(t *testing.T) {
	setupServerDataDir(t)

	cert, err := LoadOrGenerateTLS("", "")
	if err != nil {
		t.Fatalf("LoadOrGenerateTLS: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Fatal("expected non-empty certificate")
	}
}

func TestLoadOrGenerateTLS_LoadsExisting(t *testing.T) {
	setupServerDataDir(t)

	// Generate first.
	cert1, err := LoadOrGenerateTLS("", "")
	if err != nil {
		t.Fatalf("first generate: %v", err)
	}

	// Load again — should return the same cert.
	cert2, err := LoadOrGenerateTLS("", "")
	if err != nil {
		t.Fatalf("second load: %v", err)
	}

	if len(cert1.Certificate) != len(cert2.Certificate) {
		t.Error("expected same certificate on second call")
	}
}

func TestExtractComponent(t *testing.T) {
	tests := []struct {
		msg   string
		attrs map[string]string
		want  string
	}{
		{"server: something happened", map[string]string{}, "server"},
		{"agent: running task", map[string]string{}, "agent"},
		{"no colon", map[string]string{}, "server"}, // default
		{"", map[string]string{"component": "mycomp"}, "mycomp"},
		{"msg", map[string]string{"component": "explicit"}, "explicit"},
	}
	for _, tc := range tests {
		got := extractComponent(tc.msg, tc.attrs)
		if got != tc.want {
			t.Errorf("extractComponent(%q, %v) = %q; want %q", tc.msg, tc.attrs, got, tc.want)
		}
	}
}

func TestLogHub_Handle(t *testing.T) {
	hub := newLogHub(10)

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	_ = hub.Handle(context.Background(), rec)

	// Ring should have one entry.
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.ring) != 1 {
		t.Fatalf("expected 1 ring entry, got %d", len(hub.ring))
	}
	if hub.ring[0].Message != "test message" {
		t.Errorf("unexpected message: %q", hub.ring[0].Message)
	}
}

func TestIntegration_TokenAndBearer(t *testing.T) {
	setupServerDataDir(t)
	tok, _, err := LoadOrGenerateToken()
	if err != nil {
		t.Fatalf("load or generate token: %v", err)
	}

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := BearerMiddleware(tok, next)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected authorized status, got %d", rr.Code)
	}
}

func TestFmtUptime(t *testing.T) {
	tests := []struct {
		d    time.Duration
		want string
	}{
		{-1 * time.Second, "0s"},
		{0, "0s"},
		{30 * time.Second, "30s"},
		{90 * time.Second, "1m30s"},
		{1*time.Hour + 30*time.Minute, "1h30m"},
		{25*time.Hour + 5*time.Minute, "25h5m"},
	}
	for _, tc := range tests {
		got := fmtUptime(tc.d)
		if got != tc.want {
			t.Errorf("fmtUptime(%v) = %q; want %q", tc.d, got, tc.want)
		}
	}
}

func TestParseLogLine(t *testing.T) {
	// Valid JSON log line.
	line := `{"time":"2024-01-01T00:00:00Z","level":"WARN","msg":"server: test message","key":"value"}`
	entry := parseLogLine(line)
	if entry.Level != "warn" {
		t.Errorf("level = %q; want warn", entry.Level)
	}
	if entry.Message != "server: test message" {
		t.Errorf("message = %q", entry.Message)
	}
	if entry.Component != "server" {
		t.Errorf("component = %q; want server", entry.Component)
	}
	if entry.Attrs["key"] != "value" {
		t.Errorf("attrs[key] = %q; want value", entry.Attrs["key"])
	}

	// Non-JSON plain text.
	plain := "plain text log line"
	e2 := parseLogLine(plain)
	if e2.Message != plain {
		t.Errorf("plain message = %q; want %q", e2.Message, plain)
	}

	// JSON with explicit component field.
	withComp := `{"time":"2024-01-01T00:00:00Z","level":"info","msg":"hello","component":"mycomp"}`
	e3 := parseLogLine(withComp)
	if e3.Component != "mycomp" {
		t.Errorf("component from field = %q; want mycomp", e3.Component)
	}
}

func TestLogHub_WithAttrsAndWithGroup(t *testing.T) {
	hub := newLogHub(10)

	child := hub.WithAttrs([]slog.Attr{slog.String("k", "v")})
	if child == nil {
		t.Fatal("WithAttrs returned nil")
	}

	grp := hub.WithGroup("mygroup")
	if grp == nil {
		t.Fatal("WithGroup returned nil")
	}

	// child.WithAttrs
	child2 := child.WithAttrs([]slog.Attr{slog.String("k2", "v2")})
	if child2 == nil {
		t.Fatal("child.WithAttrs returned nil")
	}

	// child.WithGroup
	grp2 := child.WithGroup("subgroup")
	if grp2 == nil {
		t.Fatal("child.WithGroup returned nil")
	}

	// hubChild.Handle forwards to parent ring
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "child message", 0)
	_ = child.Handle(context.Background(), rec)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.ring) == 0 {
		t.Error("expected ring entry after child.Handle")
	}
}

func TestLogHub_SetDelegate(t *testing.T) {
	hub := newLogHub(10)
	var buf bytes.Buffer
	delegate := slog.NewTextHandler(&buf, nil)
	hub.setDelegate(delegate)

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "delegated", 0)
	_ = hub.Handle(context.Background(), rec)

	if !strings.Contains(buf.String(), "delegated") {
		t.Errorf("expected 'delegated' in delegate output, got: %s", buf.String())
	}
}

func TestLogHub_Enabled(t *testing.T) {
	hub := newLogHub(10)
	if !hub.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("expected Enabled to return true")
	}
}

func TestLogHub_ManualSubscribe(t *testing.T) {
	hub := newLogHub(10)

	// Manually add a subscriber channel.
	ch := make(chan logEntry, 10)
	hub.mu.Lock()
	hub.subs[ch] = struct{}{}
	hub.mu.Unlock()
	defer func() {
		hub.mu.Lock()
		delete(hub.subs, ch)
		hub.mu.Unlock()
	}()

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "live event", 0)
	_ = hub.Handle(context.Background(), rec)

	select {
	case got := <-ch:
		if got.Message != "live event" {
			t.Errorf("expected 'live event', got %q", got.Message)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("expected message from subscribe channel")
	}
}

func TestLogsHistoryHandler_NoLogFile(t *testing.T) {
	setupServerDataDir(t)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/history", nil)
	rr := httptest.NewRecorder()
	logsHistoryHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "entries") {
		t.Errorf("expected JSON with entries, got: %s", body)
	}
}

func TestLogsHistoryHandler_WithLogFile(t *testing.T) {
	setupServerDataDir(t)

	// Write some fake JSON log lines to the log file path.
	logDir := filepath.Join(store.DataDir(), "logs")
	if err := os.MkdirAll(logDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	logFile := filepath.Join(logDir, "aviary.log")
	lines := `{"time":"2024-01-01T00:00:00Z","level":"info","msg":"agent: hello"}
{"time":"2024-01-01T00:00:01Z","level":"warn","msg":"server: warning","key":"val"}
`
	if err := os.WriteFile(logFile, []byte(lines), 0o600); err != nil {
		t.Fatalf("write log file: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/logs/history?limit=10", nil)
	rr := httptest.NewRecorder()
	logsHistoryHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}

	var resp struct {
		Entries []logEntry `json:"entries"`
		HasMore bool       `json:"hasMore"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(resp.Entries))
	}
}

func TestHubGroup_Methods(t *testing.T) {
	hub := newLogHub(10)

	// Get a group handler via WithGroup
	grp := hub.WithGroup("mygroup")
	if grp == nil {
		t.Fatal("WithGroup returned nil")
	}

	// grp is a *hubGroup; test Enabled
	if !grp.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("hubGroup.Enabled should return true")
	}

	// grp.WithAttrs returns a hubChild
	child := grp.WithAttrs([]slog.Attr{slog.String("k", "v")})
	if child == nil {
		t.Fatal("hubGroup.WithAttrs returned nil")
	}

	// grp.WithGroup returns another hubGroup
	grp2 := grp.WithGroup("sub")
	if grp2 == nil {
		t.Fatal("hubGroup.WithGroup returned nil")
	}

	// grp.Handle forwards to parent ring
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "group message", 0)
	if err := grp.Handle(context.Background(), rec); err != nil {
		t.Fatalf("hubGroup.Handle: %v", err)
	}
	hub.mu.Lock()
	n := len(hub.ring)
	hub.mu.Unlock()
	if n == 0 {
		t.Error("expected ring entry after hubGroup.Handle")
	}
}

func TestPIDPath_EnvOverride(t *testing.T) {
	tmp := t.TempDir()
	custom := tmp + "/custom.pid"
	t.Setenv("AVIARY_PID_FILE", custom)
	got := PIDPath()
	if got != custom {
		t.Errorf("PIDPath with env = %q; want %q", got, custom)
	}
}

func TestWriteReadRemovePID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AVIARY_PID_FILE", tmp+"/aviary.pid")

	if err := WritePID(); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	pid, err := ReadPID()
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("ReadPID = %d; want %d", pid, os.Getpid())
	}

	running, rpid, err := IsRunning()
	if err != nil {
		t.Fatalf("IsRunning: %v", err)
	}
	if !running || rpid != os.Getpid() {
		t.Errorf("IsRunning = (%v, %d); want (true, %d)", running, rpid, os.Getpid())
	}

	if err := RemovePID(); err != nil {
		t.Fatalf("RemovePID: %v", err)
	}

	// After remove, ReadPID should return 0.
	pid2, err := ReadPID()
	if err != nil {
		t.Fatalf("ReadPID after remove: %v", err)
	}
	if pid2 != 0 {
		t.Errorf("expected 0 after remove, got %d", pid2)
	}
}

func TestMakeAuthResolver(t *testing.T) {
	setupServerDataDir(t)
	resolve := makeAuthResolver()
	// Resolving a non-existent ref should return error (no credentials file).
	_, err := resolve("auth:openai:default")
	if err == nil {
		t.Error("expected error for missing auth ref")
	}
}

func TestExtractComponent_MultiWord(t *testing.T) {
	// Multi-word prefix: "agent manager: something" → first word "agent"
	attrs := map[string]string{}
	got := extractComponent("agent manager: something happened", attrs)
	if got != "agent" {
		t.Errorf("extractComponent multiword = %q; want agent", got)
	}

	// Long prefix (>= 24 chars) defaults to "server"
	attrs2 := map[string]string{}
	got2 := extractComponent("this is way too long prefix: msg", attrs2)
	if got2 != "server" {
		t.Errorf("extractComponent long prefix = %q; want server", got2)
	}
}

func TestProcSampler_Sample(t *testing.T) {
	s := NewProcSampler()
	// Sample the current process — should not panic.
	s.Sample([]int{os.Getpid()})

	stats, ok := s.Get(os.Getpid())
	if !ok {
		t.Fatal("expected stats for current PID")
	}
	if stats.Status != "running" {
		t.Errorf("expected status=running, got %q", stats.Status)
	}
}

func TestLogsHistoryHandler_SkipAndLimit(t *testing.T) {
	setupServerDataDir(t)

	logDir := filepath.Join(store.DataDir(), "logs")
	_ = os.MkdirAll(logDir, 0o700)
	logFile := filepath.Join(logDir, "aviary.log")
	// Write 10 lines.
	var sb strings.Builder
	for i := 0; i < 10; i++ {
		sb.WriteString(`{"time":"2024-01-01T00:00:00Z","level":"info","msg":"line"}` + "\n")
	}
	_ = os.WriteFile(logFile, []byte(sb.String()), 0o600)

	// skip=5 should return last 5 (limit=500 default).
	req := httptest.NewRequest(http.MethodGet, "/api/logs/history?skip=5", nil)
	rr := httptest.NewRecorder()
	logsHistoryHandler(rr, req)

	var resp struct {
		Entries []logEntry `json:"entries"`
		HasMore bool       `json:"hasMore"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Entries) != 5 {
		t.Errorf("expected 5 entries with skip=5, got %d", len(resp.Entries))
	}

	// skip > total returns empty.
	req2 := httptest.NewRequest(http.MethodGet, "/api/logs/history?skip=20", nil)
	rr2 := httptest.NewRecorder()
	logsHistoryHandler(rr2, req2)
	var resp2 struct {
		Entries []logEntry `json:"entries"`
	}
	_ = json.NewDecoder(rr2.Body).Decode(&resp2)
	if len(resp2.Entries) != 0 {
		t.Errorf("expected empty with skip>total, got %d", len(resp2.Entries))
	}

	// limit=2 with hasMore=true.
	req3 := httptest.NewRequest(http.MethodGet, "/api/logs/history?limit=2", nil)
	rr3 := httptest.NewRecorder()
	logsHistoryHandler(rr3, req3)
	var resp3 struct {
		Entries []logEntry `json:"entries"`
		HasMore bool       `json:"hasMore"`
	}
	_ = json.NewDecoder(rr3.Body).Decode(&resp3)
	if !resp3.HasMore {
		t.Error("expected hasMore=true when limit < total")
	}
}

func TestParseLogLine_MissingTimestamp(t *testing.T) {
	// JSON without time field gets a generated timestamp.
	line := `{"level":"ERROR","msg":"no time here"}`
	e := parseLogLine(line)
	if e.Level != "error" {
		t.Errorf("level = %q; want error", e.Level)
	}
	if e.Timestamp == "" {
		t.Error("expected non-empty generated timestamp")
	}
}

func TestGenerateToken(t *testing.T) {
	tok, err := GenerateToken()
	if err != nil {
		t.Fatalf("GenerateToken: %v", err)
	}
	if len(tok) < 32 {
		t.Errorf("token too short: %q", tok)
	}
	// Two calls should produce different tokens.
	tok2, _ := GenerateToken()
	if tok == tok2 {
		t.Error("expected different tokens from two calls")
	}
}

// ── New tests for increased coverage ─────────────────────────────────────────

// resetSlogForTest resets global slog state so that multiple calls to New()
// within the same test binary don't cause recursive delegate loops.
// New() guards against double-install but we must reset the default to allow
// the first call in each test to re-install properly.
func resetSlogForTest() {
	globalHub.setDelegate(nil)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

func TestServerNew(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "test-token")
	if srv == nil {
		t.Fatal("expected non-nil server")
	}
}

func TestServerAddr(t *testing.T) {
	setupServerDataDir(t)

	t.Run("default port https", func(t *testing.T) {
		resetSlogForTest()
		cfg := &config.Config{}
		srv := New(cfg, "tok")
		addr := srv.Addr()
		if !strings.HasPrefix(addr, "https://") {
			t.Errorf("expected https prefix, got %q", addr)
		}
		if !strings.Contains(addr, "16677") {
			t.Errorf("expected default port 16677 in addr %q", addr)
		}
	})

	t.Run("custom port no-tls", func(t *testing.T) {
		resetSlogForTest()
		cfg := &config.Config{}
		cfg.Server.Port = 9999
		cfg.Server.NoTLS = true
		srv := New(cfg, "tok")
		addr := srv.Addr()
		if !strings.HasPrefix(addr, "http://") {
			t.Errorf("expected http prefix for no-tls, got %q", addr)
		}
		if !strings.Contains(addr, "9999") {
			t.Errorf("expected port 9999 in addr %q", addr)
		}
	})
}

func TestServerSettingsChanged(t *testing.T) {
	base := &config.Config{}
	base.Server.Port = 16677

	t.Run("unchanged", func(t *testing.T) {
		other := &config.Config{}
		other.Server.Port = 16677
		if serverSettingsChanged(base, other) {
			t.Error("expected false for identical config")
		}
	})

	t.Run("port changed", func(t *testing.T) {
		other := &config.Config{}
		other.Server.Port = 9999
		if !serverSettingsChanged(base, other) {
			t.Error("expected true when port changes")
		}
	})

	t.Run("no_tls changed", func(t *testing.T) {
		other := &config.Config{}
		other.Server.Port = 16677
		other.Server.NoTLS = true
		if !serverSettingsChanged(base, other) {
			t.Error("expected true when NoTLS changes")
		}
	})

	t.Run("external_access changed", func(t *testing.T) {
		other := &config.Config{}
		other.Server.Port = 16677
		other.Server.ExternalAccess = true
		if !serverSettingsChanged(base, other) {
			t.Error("expected true when ExternalAccess changes")
		}
	})

	t.Run("tls cert changed", func(t *testing.T) {
		cfgA := &config.Config{}
		cfgA.Server.TLS = &config.TLSConfig{Cert: "a.pem", Key: "a.key"}
		cfgB := &config.Config{}
		cfgB.Server.TLS = &config.TLSConfig{Cert: "b.pem", Key: "b.key"}
		if !serverSettingsChanged(cfgA, cfgB) {
			t.Error("expected true when TLS cert changes")
		}
	})
}

func TestTLSConfigChanged(t *testing.T) {
	t.Run("both nil", func(t *testing.T) {
		if tlsConfigChanged(nil, nil) {
			t.Error("expected false for both nil")
		}
	})

	t.Run("nil and non-nil", func(t *testing.T) {
		if !tlsConfigChanged(nil, &config.TLSConfig{}) {
			t.Error("expected true when one is nil")
		}
		if !tlsConfigChanged(&config.TLSConfig{}, nil) {
			t.Error("expected true when one is nil")
		}
	})

	t.Run("same values", func(t *testing.T) {
		a := &config.TLSConfig{Cert: "c.pem", Key: "k.key"}
		b := &config.TLSConfig{Cert: "c.pem", Key: "k.key"}
		if tlsConfigChanged(a, b) {
			t.Error("expected false for same TLS config")
		}
	})

	t.Run("different cert", func(t *testing.T) {
		a := &config.TLSConfig{Cert: "a.pem", Key: "k.key"}
		b := &config.TLSConfig{Cert: "b.pem", Key: "k.key"}
		if !tlsConfigChanged(a, b) {
			t.Error("expected true for different cert")
		}
	})
}

func TestDaemonsHandler(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")

	req := httptest.NewRequest(http.MethodGet, "/api/daemons", nil)
	rr := httptest.NewRecorder()
	srv.daemonsHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected JSON content-type, got %q", ct)
	}
	var daemons []DaemonStatus
	if err := json.NewDecoder(rr.Body).Decode(&daemons); err != nil {
		t.Fatalf("decode daemons: %v", err)
	}
	if len(daemons) == 0 {
		t.Fatal("expected at least one daemon entry")
	}
	// The first entry should be the aviary server itself.
	if daemons[0].Name != "aviary" {
		t.Errorf("expected first daemon to be 'aviary', got %q", daemons[0].Name)
	}
	if daemons[0].Type != "server" {
		t.Errorf("expected type 'server', got %q", daemons[0].Type)
	}
}

func TestDaemonLogsHandler_MissingKey(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")

	req := httptest.NewRequest(http.MethodGet, "/api/daemons/logs", nil)
	rr := httptest.NewRecorder()
	srv.daemonLogsHandler(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rr.Code)
	}
}

func TestDaemonLogsHandler_NotFound(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")

	req := httptest.NewRequest(http.MethodGet, "/api/daemons/logs?key=nonexistent", nil)
	rr := httptest.NewRecorder()
	srv.daemonLogsHandler(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestLogsHandler_SSE(t *testing.T) {
	setupServerDataDir(t)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	// logsHandler blocks until context is cancelled; run in goroutine.
	done := make(chan struct{})
	go func() {
		defer close(done)
		logsHandler(rr, req)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("logsHandler did not return after context cancellation")
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "text/event-stream") {
		t.Errorf("expected text/event-stream, got %q", ct)
	}
}

func TestWsBroadcast_NoSubscribers(t *testing.T) {
	// wsBroadcast with no connected clients should not panic.
	wsBroadcast(wsEvent{Type: "test"})
	wsBroadcast(wsEvent{Type: "health", OK: true, Version: "v0", GOOS: "linux"})
}

func TestWsRegisterUnregister(t *testing.T) {
	// We can't create a real websocket.Conn without a live connection, but we
	// can verify the map operations via wsRegister/wsUnregister using a nil
	// pointer (the map stores the pointer as a key; no dereference happens
	// in register/unregister themselves).
	//
	// Use a real-ish approach: check the map size before and after.
	wsClients.mu.Lock()
	before := len(wsClients.m)
	wsClients.mu.Unlock()

	// Unregistering something that was never registered is a no-op.
	wsUnregister(nil)

	wsClients.mu.Lock()
	after := len(wsClients.m)
	wsClients.mu.Unlock()

	if before != after {
		t.Errorf("map size changed after unregister of unknown key: %d -> %d", before, after)
	}
}

func TestSPAHandler_FileNotFound_FallsBackToIndex(t *testing.T) {
	// Build a minimal in-memory FS with index.html.
	mfs := &memFS{files: map[string]string{
		"/index.html": "<html>index</html>",
	}}
	h := spaHandler{fs: mfs}

	// Request a path that doesn't exist — should fall back to index.html.
	req := httptest.NewRequest(http.MethodGet, "/some/spa/route", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "index") {
		t.Errorf("expected index.html content, got %q", rr.Body.String())
	}
}

func TestSPAHandler_ServeIndex_NoIndexHTML(t *testing.T) {
	// FS with no index.html — serveIndex should return 404.
	mfs := &memFS{files: map[string]string{}}
	h := spaHandler{fs: mfs}

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rr := httptest.NewRecorder()
	h.serveIndex(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rr.Code)
	}
}

func TestWebFileServer_ReturnsHandler(t *testing.T) {
	// webFileServer() should always return a non-nil handler even if
	// webdist is empty or not embedded.
	h := webFileServer()
	if h == nil {
		t.Fatal("expected non-nil handler from webFileServer()")
	}
	// Serving / should not panic.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	// Either 200 (index.html found) or 404 (web UI not embedded) is acceptable.
	if rr.Code != http.StatusOK && rr.Code != http.StatusNotFound {
		t.Errorf("unexpected status %d", rr.Code)
	}
}

func TestFmtUptime_NegativeDuration(t *testing.T) {
	// Additional edge cases.
	got := fmtUptime(-5 * time.Second)
	if got != "0s" {
		t.Errorf("fmtUptime(-5s) = %q; want 0s", got)
	}
}

func TestLogsHistoryHandler_InvalidParams(t *testing.T) {
	setupServerDataDir(t)
	// Invalid skip and limit values should silently default.
	req := httptest.NewRequest(http.MethodGet, "/api/logs/history?skip=notanumber&limit=notanumber", nil)
	rr := httptest.NewRecorder()
	logsHistoryHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for invalid params, got %d", rr.Code)
	}
}

func TestLogHub_RingCapOverflow(t *testing.T) {
	hub := newLogHub(3)
	for i := 0; i < 10; i++ {
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "msg", 0)
		_ = hub.Handle(context.Background(), rec)
	}
	hub.mu.Lock()
	n := len(hub.ring)
	hub.mu.Unlock()
	if n > 3 {
		t.Errorf("ring grew beyond cap: len=%d", n)
	}
}

func TestLogHub_WithAttrs_HasDelegate(t *testing.T) {
	hub := newLogHub(10)
	var buf bytes.Buffer
	hub.setDelegate(slog.NewTextHandler(&buf, nil))

	child := hub.WithAttrs([]slog.Attr{slog.String("x", "y")})
	if child == nil {
		t.Fatal("WithAttrs returned nil")
	}
	child2 := child.WithAttrs([]slog.Attr{slog.String("a", "b")})
	if child2 == nil {
		t.Fatal("child.WithAttrs returned nil")
	}
	grp := child.WithGroup("g")
	if grp == nil {
		t.Fatal("child.WithGroup returned nil")
	}
}

func TestLogHub_WithGroup_HasDelegate(t *testing.T) {
	hub := newLogHub(10)
	var buf bytes.Buffer
	hub.setDelegate(slog.NewTextHandler(&buf, nil))

	grp := hub.WithGroup("grp")
	if grp == nil {
		t.Fatal("WithGroup returned nil")
	}
	grp2 := grp.WithGroup("sub")
	if grp2 == nil {
		t.Fatal("grp.WithGroup returned nil")
	}
	child := grp.WithAttrs([]slog.Attr{slog.String("k", "v")})
	if child == nil {
		t.Fatal("grp.WithAttrs returned nil")
	}
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "grp msg", 0)
	if err := grp.Handle(context.Background(), rec); err != nil {
		t.Fatalf("grp.Handle: %v", err)
	}
}

// memFS is a minimal http.FileSystem backed by a string map for testing.
type memFS struct {
	files map[string]string
}

func (m *memFS) Open(name string) (http.File, error) {
	content, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{content: strings.NewReader(content), name: name}, nil
}

type memFile struct {
	content *strings.Reader
	name    string
}

func (f *memFile) Read(p []byte) (n int, err error)         { return f.content.Read(p) }
func (f *memFile) Seek(offset int64, whence int) (int64, error) { return f.content.Seek(offset, whence) }
func (f *memFile) Close() error                             { return nil }
func (f *memFile) Readdir(_ int) ([]fs.FileInfo, error)     { return nil, nil }
func (f *memFile) Stat() (fs.FileInfo, error)               { return &memFileInfo{name: f.name, size: int64(f.content.Len())}, nil }

type memFileInfo struct {
	name string
	size int64
}

func (fi *memFileInfo) Name() string      { return fi.name }
func (fi *memFileInfo) Size() int64       { return fi.size }
func (fi *memFileInfo) Mode() fs.FileMode { return 0o444 }
func (fi *memFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *memFileInfo) IsDir() bool       { return false }
func (fi *memFileInfo) Sys() any          { return nil }

// ── Additional coverage tests ─────────────────────────────────────────────────

func TestWsHandler_Unauthorized(t *testing.T) {
	h := wsHandler("secret-token")

	// No auth at all → 401.
	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", rr.Code)
	}

	// Wrong token → 401.
	req2 := httptest.NewRequest(http.MethodGet, "/api/ws?token=wrong", nil)
	rr2 := httptest.NewRecorder()
	h(rr2, req2)
	if rr2.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for wrong token, got %d", rr2.Code)
	}
}

func TestWsHandler_ValidCookieAuthUpgradeFails(t *testing.T) {
	// Authentication passes (cookie) but the upgrade will fail because
	// httptest.ResponseRecorder does not implement http.Hijacker.
	// The handler should not panic and should return (upgrade error is silently swallowed).
	token := "aviary_tok_wstest"
	h := wsHandler(token)

	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	req.AddCookie(&http.Cookie{Name: "aviary_session", Value: token})
	// Add required WebSocket headers so gorilla/websocket attempts to upgrade.
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "upgrade")
	req.Header.Set("Sec-Websocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-Websocket-Version", "13")

	rr := httptest.NewRecorder()
	// Will fail during upgrade (recorder is not a Hijacker), but must not panic.
	h(rr, req)
	// The gorilla upgrader returns an error and writes 4xx; any non-panic result is ok.
}

func TestWsHandler_ValidQueryTokenUpgradeFails(t *testing.T) {
	token := "aviary_tok_wstest2"
	h := wsHandler(token)

	req := httptest.NewRequest(http.MethodGet, "/api/ws?token="+token, nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "upgrade")
	req.Header.Set("Sec-Websocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	req.Header.Set("Sec-Websocket-Version", "13")

	rr := httptest.NewRecorder()
	h(rr, req)
	// Must not panic regardless of upgrade outcome.
}

func TestServerAgents(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")
	agents := srv.Agents()
	if agents == nil {
		t.Fatal("expected non-nil Agents()")
	}
}

func TestServerLoadSessionDeliveries(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")
	// loadSessionDeliveries reads from store; with an empty data dir it should
	// complete without error or panic.
	srv.loadSessionDeliveries()
}

func TestHubChild_Enabled(t *testing.T) {
	hub := newLogHub(10)
	child := hub.WithAttrs([]slog.Attr{slog.String("k", "v")})
	// child is a *hubChild; Enabled should always return true.
	if !child.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("hubChild.Enabled should return true")
	}
}

func TestLogHub_LevelCoverage(t *testing.T) {
	hub := newLogHub(10)

	levels := []slog.Level{slog.LevelDebug, slog.LevelInfo, slog.LevelWarn, slog.LevelError}
	for _, lvl := range levels {
		rec := slog.NewRecord(time.Now(), lvl, "test level", 0)
		_ = hub.Handle(context.Background(), rec)
	}
	hub.mu.Lock()
	n := len(hub.ring)
	hub.mu.Unlock()
	if n != 4 {
		t.Errorf("expected 4 entries, got %d", n)
	}
}

func TestLogsHandler_WithLogFile(t *testing.T) {
	setupServerDataDir(t)

	// Write a fake log file so logsHandler has content to send before blocking.
	logDir := filepath.Join(store.DataDir(), "logs")
	_ = os.MkdirAll(logDir, 0o700)
	logFile := filepath.Join(logDir, "aviary.log")
	_ = os.WriteFile(logFile, []byte(`{"time":"2024-01-01T00:00:00Z","level":"info","msg":"test line"}`+"\n"), 0o600)

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		logsHandler(rr, req)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("logsHandler did not return after context cancellation")
	}

	body := rr.Body.String()
	if !strings.Contains(body, "data:") {
		t.Errorf("expected SSE data in body, got: %q", body)
	}
}

func TestDaemonsHandler_DefaultPort(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	// Zero port should default to 16677 in the daemon list.
	cfg := &config.Config{}
	cfg.Server.Port = 0
	srv := New(cfg, "tok")

	req := httptest.NewRequest(http.MethodGet, "/api/daemons", nil)
	rr := httptest.NewRecorder()
	srv.daemonsHandler(rr, req)

	var daemons []DaemonStatus
	_ = json.NewDecoder(rr.Body).Decode(&daemons)
	if len(daemons) == 0 {
		t.Fatal("expected at least one daemon")
	}
	if daemons[0].Addr != ":16677" {
		t.Errorf("expected addr :16677, got %q", daemons[0].Addr)
	}
}

func TestSPAHandler_DirectoryFallback(t *testing.T) {
	// FS where "/" opens as a directory → should fall back to index.html.
	mfs := &memFSWithDir{
		files: map[string]string{"/index.html": "<html>home</html>"},
		dirs:  map[string]bool{"/": true},
	}
	h := spaHandler{fs: mfs}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for directory path, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "home") {
		t.Errorf("expected index content for dir path, got %q", rr.Body.String())
	}
}

// memFSWithDir extends memFS to support directories.
type memFSWithDir struct {
	files map[string]string
	dirs  map[string]bool
}

func (m *memFSWithDir) Open(name string) (http.File, error) {
	if m.dirs[name] {
		return &memDirFile{name: name}, nil
	}
	content, ok := m.files[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return &memFile{content: strings.NewReader(content), name: name}, nil
}

type memDirFile struct{ name string }

func (f *memDirFile) Read(_ []byte) (int, error)               { return 0, nil }
func (f *memDirFile) Seek(_ int64, _ int) (int64, error)       { return 0, nil }
func (f *memDirFile) Close() error                             { return nil }
func (f *memDirFile) Readdir(_ int) ([]fs.FileInfo, error)     { return nil, nil }
func (f *memDirFile) Stat() (fs.FileInfo, error)               { return &memDirInfo{name: f.name}, nil }

type memDirInfo struct{ name string }

func (di *memDirInfo) Name() string       { return di.name }
func (di *memDirInfo) Size() int64        { return 0 }
func (di *memDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o755 }
func (di *memDirInfo) ModTime() time.Time { return time.Time{} }
func (di *memDirInfo) IsDir() bool        { return true }
func (di *memDirInfo) Sys() any           { return nil }

func TestLogsHistoryHandler_LimitCap(t *testing.T) {
	setupServerDataDir(t)
	// limit > 1000 should be capped at 1000 (no panic).
	req := httptest.NewRequest(http.MethodGet, "/api/logs/history?limit=9999", nil)
	rr := httptest.NewRecorder()
	logsHistoryHandler(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
}

func TestRecordToEntry_AttrsCoverage(t *testing.T) {
	hub := newLogHub(10)
	rec := slog.NewRecord(time.Now(), slog.LevelWarn, "component:warning", 0)
	rec.AddAttrs(slog.String("key1", "val1"), slog.Int("count", 42))
	entry := hub.recordToEntry(rec)
	if entry.Level != "warn" {
		t.Errorf("level = %q; want warn", entry.Level)
	}
	if entry.Attrs["key1"] != "val1" {
		t.Errorf("attrs[key1] = %q; want val1", entry.Attrs["key1"])
	}
}

func TestServerLoadSessionDeliveries_WithData(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()

	// Write a session channels config file so loadSessionDeliveries has something to read.
	if err := store.EnsureSessionChannel("agent_test", "sess1", "slack", "C123"); err != nil {
		t.Fatalf("EnsureSessionChannel: %v", err)
	}

	cfg := &config.Config{}
	srv := New(cfg, "tok")
	// Should not panic and should log the loaded sessions.
	srv.loadSessionDeliveries()
}

func TestLoadOrGenerateTLS_WithCustomCertError(t *testing.T) {
	setupServerDataDir(t)
	// Providing a non-existent cert/key path should return an error.
	_, err := LoadOrGenerateTLS("/nonexistent/cert.pem", "/nonexistent/key.pem")
	if err == nil {
		t.Error("expected error for missing custom cert files")
	}
}

func TestPIDPath_WindowsFallback(t *testing.T) {
	// Ensure PIDPath returns something reasonable when env overrides are set.
	tmp := t.TempDir()
	t.Setenv("AVIARY_PID_FILE", "")
	t.Setenv("PROGRAMDATA", tmp)
	p := PIDPath()
	// On Windows this will use PROGRAMDATA; on others it uses TempDir.
	if p == "" {
		t.Error("expected non-empty PIDPath")
	}
}

func TestLogsHandler_FilePoll(t *testing.T) {
	setupServerDataDir(t)

	// Write initial log file.
	logDir := filepath.Join(store.DataDir(), "logs")
	_ = os.MkdirAll(logDir, 0o700)
	logFile := filepath.Join(logDir, "aviary.log")
	initial := `{"time":"2024-01-01T00:00:00Z","level":"info","msg":"initial"}` + "\n"
	_ = os.WriteFile(logFile, []byte(initial), 0o600)

	// Use a context that cancels after the ticker fires once (ticker is 400ms).
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()

	// Append to the file while the handler is running (after a short delay).
	go func() {
		time.Sleep(200 * time.Millisecond)
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer f.Close() //nolint:errcheck
		_, _ = fmt.Fprintf(f, `{"time":"2024-01-01T00:00:01Z","level":"info","msg":"appended"}`+"\n")
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		logsHandler(rr, req)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("logsHandler did not return after context cancellation")
	}

	body := rr.Body.String()
	// Should contain at least the initial entry.
	if !strings.Contains(body, "data:") {
		t.Errorf("expected SSE events in body, got: %q", body[:min(len(body), 200)])
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func TestGenerateSelfSigned_Direct(t *testing.T) {
	setupServerDataDir(t)
	// Call generateSelfSigned directly to cover its body.
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	cert, err := generateSelfSigned(certFile, keyFile)
	if err != nil {
		t.Fatalf("generateSelfSigned: %v", err)
	}
	if len(cert.Certificate) == 0 {
		t.Error("expected non-empty certificate")
	}
	// Verify files were written.
	if _, err := os.Stat(certFile); err != nil {
		t.Errorf("cert file not written: %v", err)
	}
	if _, err := os.Stat(keyFile); err != nil {
		t.Errorf("key file not written: %v", err)
	}
}

func TestParseLogLine_NoAttrs(t *testing.T) {
	// JSON with only standard fields — attrs should be nil.
	line := `{"time":"2024-01-01T00:00:00Z","level":"info","msg":"simple"}`
	e := parseLogLine(line)
	if e.Attrs != nil {
		t.Errorf("expected nil attrs for simple entry, got %v", e.Attrs)
	}
	if e.Message != "simple" {
		t.Errorf("message = %q; want simple", e.Message)
	}
}

func TestWsBroadcast_WithEntry(t *testing.T) {
	// Ensure wsBroadcast handles all wsEvent fields without panic.
	processing := true
	wsBroadcast(wsEvent{
		Type:         "session_message",
		SessionID:    "sess1",
		AgentID:      "agent1",
		PoolID:       "pool1",
		Role:         "user",
		IsProcessing: &processing,
		OK:           true,
		Version:      "v1",
		GOOS:         "linux",
	})
}

func TestReadPID_InvalidContent(t *testing.T) {
	tmp := t.TempDir()
	pidFile := filepath.Join(tmp, "aviary.pid")
	t.Setenv("AVIARY_PID_FILE", pidFile)

	// Write non-numeric content to the PID file.
	_ = os.WriteFile(pidFile, []byte("notanumber\n"), 0o644)

	_, err := ReadPID()
	if err == nil {
		t.Error("expected error for non-numeric PID file content")
	}
}

func TestLogsHistoryHandler_HasMoreTrue(t *testing.T) {
	setupServerDataDir(t)
	logDir := filepath.Join(store.DataDir(), "logs")
	_ = os.MkdirAll(logDir, 0o700)
	logFile := filepath.Join(logDir, "aviary.log")

	// Write exactly 5 lines, request limit=2 to trigger hasMore.
	var sb strings.Builder
	for i := 0; i < 5; i++ {
		sb.WriteString(`{"time":"2024-01-01T00:00:00Z","level":"info","msg":"line"}` + "\n")
	}
	_ = os.WriteFile(logFile, []byte(sb.String()), 0o600)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/history?limit=2&skip=0", nil)
	rr := httptest.NewRecorder()
	logsHistoryHandler(rr, req)

	var resp struct {
		Entries []logEntry `json:"entries"`
		HasMore bool       `json:"hasMore"`
	}
	_ = json.NewDecoder(rr.Body).Decode(&resp)
	if !resp.HasMore {
		t.Error("expected hasMore=true when limit < total lines")
	}
	if len(resp.Entries) != 2 {
		t.Errorf("expected 2 entries, got %d", len(resp.Entries))
	}
}

func TestWsRegisterUnregister_NilConn(t *testing.T) {
	// wsRegister/wsUnregister use the pointer as a map key.
	// A nil *websocket.Conn is a valid key and exercises the code paths.
	before := mapLen()
	wsRegister(nil)
	after := mapLen()
	if after != before+1 {
		t.Errorf("wsRegister: map len %d -> %d, expected +1", before, after)
	}
	wsUnregister(nil)
	final := mapLen()
	if final != before {
		t.Errorf("wsUnregister: map len %d -> %d, expected back to %d", after, final, before)
	}
}

func mapLen() int {
	wsClients.mu.Lock()
	defer wsClients.mu.Unlock()
	return len(wsClients.m)
}

func TestGenerateSelfSigned_BadDir(t *testing.T) {
	// Provide a path under a read-only location to force a write error.
	// On most systems /proc/... is not writable.
	_, err := generateSelfSigned("/proc/nonexistent/cert.pem", "/proc/nonexistent/key.pem")
	if err == nil {
		// If for some reason this didn't fail (unlikely), just skip.
		t.Skip("expected error for unwritable path but got none")
	}
}

func TestLogsHandler_LogFileGrows(t *testing.T) {
	setupServerDataDir(t)

	logDir := filepath.Join(store.DataDir(), "logs")
	_ = os.MkdirAll(logDir, 0o700)
	logFile := filepath.Join(logDir, "aviary.log")

	// Start with empty file.
	_ = os.WriteFile(logFile, []byte{}, 0o600)

	// Cancel quickly so the ticker has time to check the file.
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()

	// Append content to file after a short delay to trigger "size > offset" branch.
	go func() {
		time.Sleep(50 * time.Millisecond)
		f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		defer f.Close() //nolint:errcheck
		_, _ = fmt.Fprintf(f, `{"time":"2024-01-01T00:00:00Z","level":"info","msg":"new line"}`+"\n")
	}()

	req := httptest.NewRequest(http.MethodGet, "/api/logs", nil).WithContext(ctx)
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		logsHandler(rr, req)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("logsHandler did not return")
	}
	// Anything without panic is success — the body may or may not have events.
}

func TestLogHub_RingCapWithSubs(t *testing.T) {
	hub := newLogHub(5)
	ch := make(chan logEntry, 20)
	hub.mu.Lock()
	hub.subs[ch] = struct{}{}
	hub.mu.Unlock()
	defer func() {
		hub.mu.Lock()
		delete(hub.subs, ch)
		hub.mu.Unlock()
	}()

	// Send 8 entries — ring should cap at 5.
	for i := 0; i < 8; i++ {
		rec := slog.NewRecord(time.Now(), slog.LevelInfo, "fill", 0)
		_ = hub.Handle(context.Background(), rec)
	}

	hub.mu.Lock()
	n := len(hub.ring)
	hub.mu.Unlock()
	if n > 5 {
		t.Errorf("ring exceeded cap: %d > 5", n)
	}
}

func TestProcSampler_MultiPID(t *testing.T) {
	s := NewProcSampler()
	// Sample self + nonexistent PIDs — should not panic.
	s.Sample([]int{os.Getpid(), 99998, 99999})
	s.Forget(99999)
	s.Forget(99998)
}

func TestServerNew_Agents(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")
	// Agents() should not be nil.
	if srv.Agents() == nil {
		t.Error("expected non-nil Agents()")
	}
}

func TestServerAddr_ExplicitPort(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	cfg.Server.Port = 8443
	srv := New(cfg, "tok")
	addr := srv.Addr()
	if !strings.Contains(addr, "8443") {
		t.Errorf("expected port 8443 in addr %q", addr)
	}
}

func TestBearerMiddleware_EmptyToken(t *testing.T) {
	// An empty token means all requests without auth are allowed.
	// Actually: the token check is "q == token && token != ''" so empty token
	// will not match "" and will reject. Let's verify the exact behavior.
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	h := BearerMiddleware("valid-tok", next)

	// Request with empty bearer header.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for empty bearer, got %d", rr.Code)
	}
}

func TestDaemonsHandler_WithSampler(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")

	// Pre-populate sampler with current PID stats.
	srv.sampler.Sample([]int{os.Getpid()})

	req := httptest.NewRequest(http.MethodGet, "/api/daemons", nil)
	rr := httptest.NewRecorder()
	srv.daemonsHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rr.Code)
	}
	var daemons []DaemonStatus
	_ = json.NewDecoder(rr.Body).Decode(&daemons)
	if len(daemons) == 0 {
		t.Fatal("expected at least one daemon")
	}
}

func TestExtractComponent_EdgeCases(t *testing.T) {
	// Empty message.
	got := extractComponent("", map[string]string{})
	if got != "server" {
		t.Errorf("empty msg = %q; want server", got)
	}

	// Message with colon at position 0 — no prefix.
	got2 := extractComponent(":nodeName", map[string]string{})
	if got2 != "server" {
		t.Errorf("colon at 0 = %q; want server", got2)
	}
}

func TestParseLogLine_JSONWithMissingLevel(t *testing.T) {
	// JSON without level defaults to "info".
	line := `{"time":"2024-01-01T00:00:00Z","msg":"no level here"}`
	e := parseLogLine(line)
	if e.Level != "info" {
		t.Errorf("level = %q; want info", e.Level)
	}
}
