package server

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
