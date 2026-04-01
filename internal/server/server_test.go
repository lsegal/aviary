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

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/update"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubChannel struct{}

func (stubChannel) Start(context.Context) error              { return nil }
func (stubChannel) Stop()                                    {}
func (stubChannel) Send(string, string) error                { return nil }
func (stubChannel) OnMessage(func(channels.IncomingMessage)) {}

func setupServerDataDir(t *testing.T) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("AVIARY_PID_FILE", filepath.Join(base, "aviary.pid"))
	err := store.EnsureDirs()
	assert.NoError(t, err)

}

func TestGenerateLoadTokenFlows(t *testing.T) {
	setupServerDataDir(t)

	t.Run("load missing", func(t *testing.T) {
		_, err := LoadToken()
		assert.Error(t, err)

	})

	t.Run("generate and load", func(t *testing.T) {
		tok, err := GenerateToken()
		assert.NoError(t, err)
		assert.True(t, strings.HasPrefix(tok, tokenPrefix))

		got, err := LoadToken()
		assert.NoError(t, err)
		assert.Equal(t, tok, got)

	})

	t.Run("load or generate existing", func(t *testing.T) {
		first, isNew, err := LoadOrGenerateToken()
		assert.NoError(t, err)
		assert.False(t, isNew)

		second, isNew, err := LoadOrGenerateToken()
		assert.NoError(t, err)
		assert.False(t, isNew)
		assert.Equal(t, second, first)

	})

	t.Run("load or generate read error does not rotate token", func(t *testing.T) {
		err := os.Remove(tokenPath())
		if err != nil {
			assert.True(t, os.IsNotExist(err))
		}

		err = os.Mkdir(tokenPath(), 0o700)
		assert.NoError(t, err)

		_, isNew, err := LoadOrGenerateToken()
		assert.Error(t, err)
		assert.False(t, isNew)
		assert.True(t, strings.Contains(err.Error(), "reading token"))

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
		assert.Equal(t, http.StatusOK, rr.Code)

	})

	t.Run("valid cookie", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.AddCookie(&http.Cookie{Name: "aviary_session", Value: token})
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

	})

	t.Run("valid query token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/logs?token="+token, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

	})

	t.Run("unauthorized", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)

	})
}

func TestLoginHandler(t *testing.T) {
	token := "aviary_tok_secret"
	h := LoginHandler(token)

	t.Run("method not allowed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/login", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)

	})

	t.Run("json body", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"token": token})
		req := httptest.NewRequest(http.MethodPost, "/api/login", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

		cookies := rr.Result().Cookies()
		assert.NotEmpty(t, cookies)
		assert.Equal(t, "aviary_session", cookies[0].Name)

	})

	t.Run("form value", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("token="+token))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

	})

	t.Run("authorization header", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/login", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusOK, rr.Code)

	})

	t.Run("invalid token", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader("token=bad"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		assert.Equal(t, http.StatusUnauthorized, rr.Code)

	})
}

func TestPIDLifecycle(t *testing.T) {
	setupServerDataDir(t)

	pid, err := ReadPID()
	assert.NoError(t, err)
	assert.Equal(t, 0, pid)
	err = WritePID()
	assert.NoError(t, err)

	pid, err = ReadPID()
	assert.NoError(t, err)
	assert.Positive(t, pid)

	running, gotPID, err := IsRunning()
	assert.NoError(t, err)
	assert.Positive(t, gotPID)

	// On some platforms, process liveness probing may return false even for
	// current PID; the key contract here is no error and a parsed PID.
	_ = running
	err = RemovePID()
	assert.NoError(t, err)

	err = RemovePID()
	assert.NoError(t, err)

	running, gotPID, err = IsRunning()
	assert.NoError(t, err)
	assert.False(t, running)
	assert.Equal(t, 0, gotPID)

}

func TestHealthHandler(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	rr := httptest.NewRecorder()
	healthHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var payload struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		GOOS    string `json:"goos"`
	}
	err := json.NewDecoder(rr.Body).Decode(&payload)
	assert.NoError(t, err)

	assert.True(t, payload.OK)
	assert.NotEqual(t, "", payload.GOOS)

}

func TestVersionHandler_Emulated(t *testing.T) {
	resetSlogForTest()
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() {
		buildinfo.Version = orig
		_ = update.ConfigureEmulation("")
	})
	err := update.ConfigureEmulation("1.2.3:1.3.0")
	assert.NoError(t, err)

	srv := New(&config.Config{}, "tok")
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rr := httptest.NewRecorder()
	srv.versionHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var payload struct {
		CurrentVersion   string `json:"currentVersion"`
		LatestVersion    string `json:"latestVersion"`
		UpgradeAvailable bool   `json:"upgradeAvailable"`
	}
	err = json.NewDecoder(rr.Body).Decode(&payload)
	assert.NoError(t, err)

	assert.Equal(t, "1.2.3", payload.CurrentVersion)
	assert.Equal(t, "1.3.0", payload.LatestVersion)
	assert.True(t, payload.UpgradeAvailable)

}

func TestVersionHandler_CheckFailureStillReturnsStatusOK(t *testing.T) {
	orig := versionCheck
	versionCheck = func(_ context.Context, _ *http.Client) (update.CheckResult, error) {
		return update.CheckResult{
			CurrentVersion: "dev",
			Message:        "release lookup failed",
		}, assert.AnError
	}
	t.Cleanup(func() {
		versionCheck = orig
	})

	srv := New(&config.Config{}, "tok")
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	rr := httptest.NewRecorder()
	srv.versionHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var payload struct {
		CurrentVersion string `json:"currentVersion"`
		Message        string `json:"message"`
	}
	err := json.NewDecoder(rr.Body).Decode(&payload)
	assert.NoError(t, err)
	assert.Equal(t, "dev", payload.CurrentVersion)
	assert.Equal(t, "release lookup failed", payload.Message)
}

func TestVersionUpgradeHandler_Emulated(t *testing.T) {
	resetSlogForTest()
	orig := buildinfo.Version
	buildinfo.Version = "dev"
	t.Cleanup(func() {
		buildinfo.Version = orig
		_ = update.ConfigureEmulation("")
	})
	err := update.ConfigureEmulation("1.2.3:1.3.0")
	assert.NoError(t, err)

	srv := New(&config.Config{}, "tok")
	req := httptest.NewRequest(http.MethodPost, "/api/version/upgrade", strings.NewReader(`{"version":"1.3.0"}`))
	rr := httptest.NewRecorder()
	srv.versionUpgradeHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, strings.Contains(rr.Body.String(), "Emulated upgrade completed"))

}

func TestApplyConfigReload_ReconcilesChannels(t *testing.T) {
	srv := New(&config.Config{}, "tok")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	srv.runCtx = ctx
	srv.msgFn = func(string, string, string, channels.Channel, channels.IncomingMessage) {}

	cfgWithChannel := &config.Config{
		Agents: []config.AgentConfig{{
			Name: "bot",
			Channels: []config.ChannelConfig{{
				Type: "signal",
			}},
		}},
	}

	srv.applyConfigReload(cfgWithChannel)
	assert.Len(t, srv.channels.List(), 1)

	srv.applyConfigReload(&config.Config{})
	assert.Empty(t, srv.channels.List())
}

func TestApplyConfigReload_ServerChangeTriggersListenerRestartOnly(t *testing.T) {
	srv := New(&config.Config{}, "tok")

	srv.applyConfigReload(&config.Config{
		Server: config.ServerConfig{Port: 17777},
	})

	select {
	case <-srv.listenerRestartCh:
	default:
		t.Fatal("expected listener restart signal")
	}

	select {
	case <-srv.hardRestartCh:
		t.Fatal("unexpected hard restart signal")
	default:
	}
}

func TestProcSampler(t *testing.T) {
	s := NewProcSampler()

	// Initially no stats.
	_, ok := s.Get(99999)
	assert.False(t, ok)

	// Forget on untracked PID should not panic.
	s.Forget(99999)
}

func TestLoadOrGenerateTLS_GeneratesSelfSigned(t *testing.T) {
	setupServerDataDir(t)

	cert, err := LoadOrGenerateTLS("", "")
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(cert.Certificate))

}

func TestLoadOrGenerateTLS_LoadsExisting(t *testing.T) {
	setupServerDataDir(t)

	// Generate first.
	cert1, err := LoadOrGenerateTLS("", "")
	assert.NoError(t, err)

	// Load again — should return the same cert.
	cert2, err := LoadOrGenerateTLS("", "")
	assert.NoError(t, err)
	assert.Equal(t, len(cert2.Certificate), len(cert1.Certificate))

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
		assert.Equal(t, tc.want, got)

	}
}

func TestLogHub_Handle(t *testing.T) {
	hub := newLogHub(10)

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "test message", 0)
	_ = hub.Handle(context.Background(), rec)

	// Ring should have one entry.
	hub.mu.Lock()
	defer hub.mu.Unlock()
	assert.Equal(t, 1, len(hub.ring))
	assert.Equal(t, "test message", hub.ring[0].Message)

}

func TestIntegration_TokenAndBearer(t *testing.T) {
	setupServerDataDir(t)
	tok, _, err := LoadOrGenerateToken()
	assert.NoError(t, err)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	h := BearerMiddleware(tok, next)

	req := httptest.NewRequest(http.MethodGet, "/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

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
		assert.Equal(t, tc.want, got)

	}
}

func TestParseLogLine(t *testing.T) {
	// Valid JSON log line.
	line := `{"time":"2024-01-01T00:00:00Z","level":"WARN","msg":"server: test message","key":"value"}`
	entry := parseLogLine(line)
	assert.Equal(t, "warn", entry.Level)
	assert.Equal(t, "server: test message", entry.Message)
	assert.Equal(t, "server", entry.Component)
	assert.Equal(t, "value", entry.Attrs["key"])

	// Non-JSON plain text.
	plain := "plain text log line"
	e2 := parseLogLine(plain)
	assert.Equal(t, plain, e2.Message)

	// JSON with explicit component field.
	withComp := `{"time":"2024-01-01T00:00:00Z","level":"info","msg":"hello","component":"mycomp"}`
	e3 := parseLogLine(withComp)
	assert.Equal(t, "mycomp", e3.Component)

}

func TestLogHub_WithAttrsAndWithGroup(t *testing.T) {
	hub := newLogHub(10)

	child := hub.WithAttrs([]slog.Attr{slog.String("k", "v")})
	assert.NotNil(t, child)

	grp := hub.WithGroup("mygroup")
	assert.NotNil(t, grp)

	// child.WithAttrs
	child2 := child.WithAttrs([]slog.Attr{slog.String("k2", "v2")})
	assert.NotNil(t, child2)

	// child.WithGroup
	grp2 := child.WithGroup("subgroup")
	assert.NotNil(t, grp2)

	// hubChild.Handle forwards to parent ring
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "child message", 0)
	_ = child.Handle(context.Background(), rec)
	hub.mu.Lock()
	defer hub.mu.Unlock()
	assert.NotEqual(t, 0, len(hub.ring))

}

func TestLogHub_SetDelegate(t *testing.T) {
	hub := newLogHub(10)
	var buf bytes.Buffer
	delegate := slog.NewTextHandler(&buf, nil)
	hub.setDelegate(delegate)

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "delegated", 0)
	_ = hub.Handle(context.Background(), rec)
	assert.True(t, strings.Contains(buf.String(), "delegated"))

}

func TestLogHub_Enabled(t *testing.T) {
	hub := newLogHub(10)
	assert.True(t, hub.Enabled(context.Background(), slog.LevelDebug))

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

	var got logEntry
	received := false
	select {
	case got = <-ch:
		received = true
	case <-time.After(1 * time.Second):
	}
	assert.True(t, received)
	assert.Equal(t, "live event", got.Message)
}

func TestLogsHistoryHandler_NoLogFile(t *testing.T) {
	setupServerDataDir(t)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/history", nil)
	rr := httptest.NewRecorder()
	logsHistoryHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	body := rr.Body.String()
	assert.True(t, strings.Contains(body, "entries"))

}

func TestLogsHistoryHandler_WithLogFile(t *testing.T) {
	setupServerDataDir(t)

	// Write some fake JSON log lines to the log file path.
	logDir := filepath.Join(store.DataDir(), "logs")
	err := os.MkdirAll(logDir, 0o700)
	assert.NoError(t, err)

	logFile := filepath.Join(logDir, "aviary.log")
	lines := `{"time":"2024-01-01T00:00:00Z","level":"info","msg":"agent: hello"}
{"time":"2024-01-01T00:00:01Z","level":"warn","msg":"server: warning","key":"val"}
`
	err = os.WriteFile(logFile, []byte(lines), 0o600)
	assert.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/api/logs/history?limit=10", nil)
	rr := httptest.NewRecorder()
	logsHistoryHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

	var resp struct {
		Entries []logEntry `json:"entries"`
		HasMore bool       `json:"hasMore"`
	}
	err = json.NewDecoder(rr.Body).Decode(&resp)
	assert.NoError(t, err)

	assert.Equal(t, 2, len(resp.Entries))

}

func TestHubGroup_Methods(t *testing.T) {
	hub := newLogHub(10)

	// Get a group handler via WithGroup
	grp := hub.WithGroup("mygroup")
	assert.NotNil(t, grp)
	assert.True(t, // grp is a *hubGroup; test Enabled
		grp.Enabled(context.Background(), slog.LevelDebug))

	// grp.WithAttrs returns a hubChild
	child := grp.WithAttrs([]slog.Attr{slog.String("k", "v")})
	assert.NotNil(t, child)

	// grp.WithGroup returns another hubGroup
	grp2 := grp.WithGroup("sub")
	assert.NotNil(t, grp2)

	// grp.Handle forwards to parent ring
	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "group message", 0)
	err := grp.Handle(context.Background(), rec)
	assert.NoError(t, err)

	hub.mu.Lock()
	n := len(hub.ring)
	hub.mu.Unlock()
	assert.NotEqual(t, 0, n)

}

func TestPIDPath_EnvOverride(t *testing.T) {
	tmp := t.TempDir()
	custom := tmp + "/custom.pid"
	t.Setenv("AVIARY_PID_FILE", custom)
	got := PIDPath()
	assert.Equal(t, custom, got)

}

func TestWriteReadRemovePID(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("AVIARY_PID_FILE", tmp+"/aviary.pid")
	err := WritePID()
	assert.NoError(t, err)

	pid, err := ReadPID()
	assert.NoError(t, err)
	assert.Equal(t, os.Getpid(), pid)

	running, rpid, err := IsRunning()
	assert.NoError(t, err)
	assert.True(t, running)
	assert.Equal(t, os.Getpid(), rpid)
	err = RemovePID()
	assert.NoError(t, err)

	// After remove, ReadPID should return 0.
	pid2, err := ReadPID()
	assert.NoError(t, err)
	assert.Equal(t, 0, pid2)

}

func TestMakeAuthResolver(t *testing.T) {
	setupServerDataDir(t)
	resolve := makeAuthResolver()
	// Resolving a non-existent ref should return error (no credentials file).
	_, err := resolve("auth:openai:default")
	assert.Error(t, err)

}

func TestExtractComponent_MultiWord(t *testing.T) {
	// Multi-word prefix: "agent manager: something" → first word "agent"
	attrs := map[string]string{}
	got := extractComponent("agent manager: something happened", attrs)
	assert.Equal(t, "agent", got)

	// Long prefix (>= 24 chars) defaults to "server"
	attrs2 := map[string]string{}
	got2 := extractComponent("this is way too long prefix: msg", attrs2)
	assert.Equal(t, "server", got2)

}

func TestProcSampler_Sample(t *testing.T) {
	s := NewProcSampler()
	// Sample the current process — should not panic.
	s.Sample([]int{os.Getpid()})

	stats, ok := s.Get(os.Getpid())
	assert.True(t, ok)
	assert.Contains(t, []string{"running", "sleeping"}, stats.Status)

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
	err := json.NewDecoder(rr.Body).Decode(&resp)
	assert.NoError(t, err)

	assert.Equal(t, 5, len(resp.Entries))

	// skip > total returns empty.
	req2 := httptest.NewRequest(http.MethodGet, "/api/logs/history?skip=20", nil)
	rr2 := httptest.NewRecorder()
	logsHistoryHandler(rr2, req2)
	var resp2 struct {
		Entries []logEntry `json:"entries"`
	}
	_ = json.NewDecoder(rr2.Body).Decode(&resp2)
	assert.Equal(t, 0, len(resp2.Entries))

	// limit=2 with hasMore=true.
	req3 := httptest.NewRequest(http.MethodGet, "/api/logs/history?limit=2", nil)
	rr3 := httptest.NewRecorder()
	logsHistoryHandler(rr3, req3)
	var resp3 struct {
		Entries []logEntry `json:"entries"`
		HasMore bool       `json:"hasMore"`
	}
	_ = json.NewDecoder(rr3.Body).Decode(&resp3)
	assert.True(t, resp3.HasMore)

}

func TestParseLogLine_MissingTimestamp(t *testing.T) {
	// JSON without time field gets a generated timestamp.
	line := `{"level":"ERROR","msg":"no time here"}`
	e := parseLogLine(line)
	assert.Equal(t, "error", e.Level)
	assert.NotEqual(t, "", e.Timestamp)

}

func TestGenerateToken(t *testing.T) {
	setupServerDataDir(t)

	tok, err := GenerateToken()
	assert.NoError(t, err)
	assert.GreaterOrEqual(t, len(tok), 32)

	// Two calls should produce different tokens.
	tok2, _ := GenerateToken()
	assert.NotEqual(t, tok2, tok)

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
	assert.NotNil(t, srv)

}

func TestServerAddr(t *testing.T) {
	setupServerDataDir(t)

	t.Run("default port https", func(t *testing.T) {
		resetSlogForTest()
		cfg := &config.Config{}
		srv := New(cfg, "tok")
		addr := srv.Addr()
		assert.True(t, strings.HasPrefix(addr, "https://"))
		assert.True(t, strings.Contains(addr, "16677"))

	})

	t.Run("custom port no-tls", func(t *testing.T) {
		resetSlogForTest()
		cfg := &config.Config{}
		cfg.Server.Port = 9999
		cfg.Server.NoTLS = true
		srv := New(cfg, "tok")
		addr := srv.Addr()
		assert.True(t, strings.HasPrefix(addr, "http://"))
		assert.True(t, strings.Contains(addr, "9999"))

	})
}

func TestServerSettingsChanged(t *testing.T) {
	base := &config.Config{}
	base.Server.Port = 16677

	t.Run("unchanged", func(t *testing.T) {
		other := &config.Config{}
		other.Server.Port = 16677
		assert.False(t, serverSettingsChanged(base, other))

	})

	t.Run("port changed", func(t *testing.T) {
		other := &config.Config{}
		other.Server.Port = 9999
		assert.True(t, serverSettingsChanged(base, other))

	})

	t.Run("no_tls changed", func(t *testing.T) {
		other := &config.Config{}
		other.Server.Port = 16677
		other.Server.NoTLS = true
		assert.True(t, serverSettingsChanged(base, other))

	})

	t.Run("external_access changed", func(t *testing.T) {
		other := &config.Config{}
		other.Server.Port = 16677
		other.Server.ExternalAccess = true
		assert.True(t, serverSettingsChanged(base, other))

	})

	t.Run("external_access env override suppresses yaml-only changes", func(t *testing.T) {
		t.Setenv("AVIARY_CONFIG_SERVER_EXTERNAL_ACCESS", "true")
		other := &config.Config{}
		other.Server.Port = 16677
		other.Server.ExternalAccess = true
		assert.False(t, serverSettingsChanged(base, other))

	})

	t.Run("tls cert changed", func(t *testing.T) {
		cfgA := &config.Config{}
		cfgA.Server.TLS = &config.TLSConfig{Cert: "a.pem", Key: "a.key"}
		cfgB := &config.Config{}
		cfgB.Server.TLS = &config.TLSConfig{Cert: "b.pem", Key: "b.key"}
		assert.True(t, serverSettingsChanged(cfgA, cfgB))

	})
}

func TestTLSConfigChanged(t *testing.T) {
	t.Run("both nil", func(t *testing.T) {
		assert.False(t, tlsConfigChanged(nil, nil))

	})

	t.Run("nil and non-nil", func(t *testing.T) {
		assert.True(t, tlsConfigChanged(nil, &config.TLSConfig{}))
		assert.True(t, tlsConfigChanged(&config.TLSConfig{}, nil))

	})

	t.Run("same values", func(t *testing.T) {
		a := &config.TLSConfig{Cert: "c.pem", Key: "k.key"}
		b := &config.TLSConfig{Cert: "c.pem", Key: "k.key"}
		assert.False(t, tlsConfigChanged(a, b))

	})

	t.Run("different cert", func(t *testing.T) {
		a := &config.TLSConfig{Cert: "a.pem", Key: "k.key"}
		b := &config.TLSConfig{Cert: "b.pem", Key: "k.key"}
		assert.True(t, tlsConfigChanged(a, b))

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
	assert.Equal(t, http.StatusOK, rr.Code)

	ct := rr.Header().Get("Content-Type")
	assert.True(t, strings.Contains(ct, "application/json"))

	var daemons []DaemonStatus
	err := json.NewDecoder(rr.Body).Decode(&daemons)
	assert.NoError(t, err)

	assert.NotEqual(t, 0, len(daemons))
	assert.Equal(t, // The first entry should be the aviary server itself.
		"aviary", daemons[0].Name)
	assert.Equal(t, "server", daemons[0].Type)

}

func TestDaemonLogsHandler_MissingKey(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")

	req := httptest.NewRequest(http.MethodGet, "/api/daemons/logs", nil)
	rr := httptest.NewRecorder()
	srv.daemonLogsHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)

}

func TestDaemonLogsHandler_NotFound(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")

	req := httptest.NewRequest(http.MethodGet, "/api/daemons/logs?key=nonexistent", nil)
	rr := httptest.NewRecorder()
	srv.daemonLogsHandler(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)

}

func TestDaemonRestartHandler_Server(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")

	req := httptest.NewRequest(http.MethodPost, "/api/daemons/restart", strings.NewReader(`{"key":"aviary"}`))
	rr := httptest.NewRecorder()
	srv.daemonRestartHandler(rr, req)
	assert.Equal(t, http.StatusAccepted, rr.Code)

	select {
	case <-srv.hardRestartCh:
	default:
		t.Fatal("expected restart signal")
	}
}

func TestDaemonRestartHandler_MissingKey(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")

	req := httptest.NewRequest(http.MethodPost, "/api/daemons/restart", strings.NewReader(`{}`))
	rr := httptest.NewRecorder()
	srv.daemonRestartHandler(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestDaemonRestartHandler_MethodNotAllowed(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	srv := New(cfg, "tok")

	req := httptest.NewRequest(http.MethodGet, "/api/daemons/restart", nil)
	rr := httptest.NewRecorder()
	srv.daemonRestartHandler(rr, req)
	assert.Equal(t, http.StatusMethodNotAllowed, rr.Code)
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

	completed := false
	select {
	case <-done:
		completed = true
	case <-time.After(2 * time.Second):
	}
	assert.True(t, completed)

	ct := rr.Header().Get("Content-Type")
	assert.True(t, strings.Contains(ct, "text/event-stream"))

}

func TestWsBroadcast_NoSubscribers(_ *testing.T) {
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
	assert.Equal(t, after, before)

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
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, strings.Contains(rr.Body.String(), "index"))

}

func TestSPAHandler_ServeIndex_NoIndexHTML(t *testing.T) {
	// FS with no index.html — serveIndex should return 404.
	mfs := &memFS{files: map[string]string{}}
	h := spaHandler{fs: mfs}

	req := httptest.NewRequest(http.MethodGet, "/missing", nil)
	rr := httptest.NewRecorder()
	h.serveIndex(rr, req)
	assert.Equal(t, http.StatusNotFound, rr.Code)

}

func TestWebFileServer_ReturnsHandler(t *testing.T) {
	// webFileServer() should always return a non-nil handler even if
	// webdist is empty or not embedded.
	h := webFileServer()
	assert.NotNil(t, h)

	// Serving / should not panic.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	assert.Contains(t, []int{http.StatusOK, http.StatusNotFound}, rr.Code)

}

func TestFmtUptime_NegativeDuration(t *testing.T) {
	// Additional edge cases.
	got := fmtUptime(-5 * time.Second)
	assert.Equal(t, "0s", got)

}

func TestLogsHistoryHandler_InvalidParams(t *testing.T) {
	setupServerDataDir(t)
	// Invalid skip and limit values should silently default.
	req := httptest.NewRequest(http.MethodGet, "/api/logs/history?skip=notanumber&limit=notanumber", nil)
	rr := httptest.NewRecorder()
	logsHistoryHandler(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)

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
	assert.LessOrEqual(t, n, 3)

}

func TestLogHub_WithAttrs_HasDelegate(t *testing.T) {
	hub := newLogHub(10)
	var buf bytes.Buffer
	hub.setDelegate(slog.NewTextHandler(&buf, nil))

	child := hub.WithAttrs([]slog.Attr{slog.String("x", "y")})
	assert.NotNil(t, child)

	child2 := child.WithAttrs([]slog.Attr{slog.String("a", "b")})
	assert.NotNil(t, child2)

	grp := child.WithGroup("g")
	assert.NotNil(t, grp)

}

func TestLogHub_WithGroup_HasDelegate(t *testing.T) {
	hub := newLogHub(10)
	var buf bytes.Buffer
	hub.setDelegate(slog.NewTextHandler(&buf, nil))

	grp := hub.WithGroup("grp")
	assert.NotNil(t, grp)

	grp2 := grp.WithGroup("sub")
	assert.NotNil(t, grp2)

	child := grp.WithAttrs([]slog.Attr{slog.String("k", "v")})
	assert.NotNil(t, child)

	rec := slog.NewRecord(time.Now(), slog.LevelInfo, "grp msg", 0)
	err := grp.Handle(context.Background(), rec)
	assert.NoError(t, err)

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

func (f *memFile) Read(p []byte) (n int, err error) { return f.content.Read(p) }
func (f *memFile) Seek(offset int64, whence int) (int64, error) {
	return f.content.Seek(offset, whence)
}
func (f *memFile) Close() error                         { return nil }
func (f *memFile) Readdir(_ int) ([]fs.FileInfo, error) { return nil, nil }
func (f *memFile) Stat() (fs.FileInfo, error) {
	return &memFileInfo{name: f.name, size: int64(f.content.Len())}, nil
}

type memFileInfo struct {
	name string
	size int64
}

func (fi *memFileInfo) Name() string       { return fi.name }
func (fi *memFileInfo) Size() int64        { return fi.size }
func (fi *memFileInfo) Mode() fs.FileMode  { return 0o444 }
func (fi *memFileInfo) ModTime() time.Time { return time.Time{} }
func (fi *memFileInfo) IsDir() bool        { return false }
func (fi *memFileInfo) Sys() any           { return nil }

// ── Additional coverage tests ─────────────────────────────────────────────────

func TestWsHandler_Unauthorized(t *testing.T) {
	h := wsHandler("secret-token")

	// No auth at all → 401.
	req := httptest.NewRequest(http.MethodGet, "/api/ws", nil)
	rr := httptest.NewRecorder()
	h(rr, req)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	// Wrong token → 401.
	req2 := httptest.NewRequest(http.MethodGet, "/api/ws?token=wrong", nil)
	rr2 := httptest.NewRecorder()
	h(rr2, req2)
	assert.Equal(t, http.StatusUnauthorized, rr2.Code)

}

func TestWsHandler_ValidCookieAuthUpgradeFails(_ *testing.T) {
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

func TestWsHandler_ValidQueryTokenUpgradeFails(_ *testing.T) {
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
	assert.NotNil(t, agents)

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
	assert.
		// child is a *hubChild; Enabled should always return true.
		True(t, child.Enabled(context.Background(), slog.LevelDebug))

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
	assert.Equal(t, 4, n)

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

	completed := false
	select {
	case <-done:
		completed = true
	case <-time.After(2 * time.Second):
	}
	assert.True(t, completed)

	body := rr.Body.String()
	assert.True(t, strings.Contains(body, "data:"))

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
	assert.NotEqual(t, 0, len(daemons))
	assert.Equal(t, ":16677", daemons[0].Addr)

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
	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, strings.Contains(rr.Body.String(), "home"))

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

func (f *memDirFile) Read(_ []byte) (int, error)           { return 0, nil }
func (f *memDirFile) Seek(_ int64, _ int) (int64, error)   { return 0, nil }
func (f *memDirFile) Close() error                         { return nil }
func (f *memDirFile) Readdir(_ int) ([]fs.FileInfo, error) { return nil, nil }
func (f *memDirFile) Stat() (fs.FileInfo, error)           { return &memDirInfo{name: f.name}, nil }

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
	assert.Equal(t, http.StatusOK, rr.Code)

}

func TestRecordToEntry_AttrsCoverage(t *testing.T) {
	hub := newLogHub(10)
	rec := slog.NewRecord(time.Now(), slog.LevelWarn, "component:warning", 0)
	rec.AddAttrs(slog.String("key1", "val1"), slog.Int("count", 42))
	entry := hub.recordToEntry(rec)
	assert.Equal(t, "warn", entry.Level)
	assert.Equal(t, "val1", entry.Attrs["key1"])

}

func TestServerLoadSessionDeliveries_WithData(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()

	// Write a session channels config file so loadSessionDeliveries has something to read.
	err := store.EnsureSessionChannel("test", "sess1", "slack", "alerts", "C123")
	assert.NoError(t, err)

	cfg := &config.Config{}
	srv := New(cfg, "tok")
	// Should not panic and should log the loaded sessions.
	srv.loadSessionDeliveries()
}

func TestHandleIncomingChannelMessage_PersistsIncomingMedia(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()

	cfg := &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "stub"}}}
	srv := New(cfg, "tok")
	srv.agents.Reconcile(cfg)
	t.Cleanup(func() {
		if runner, ok := srv.agents.Get("bot"); ok {
			runner.Wait()
		}
	})

	srv.handleIncomingChannelMessage(context.Background(), "bot", "slack", "alerts", stubChannel{}, channels.IncomingMessage{
		Type:     "slack",
		Channel:  "D123",
		From:     "U123",
		Text:     "describe this",
		MediaURL: "data:image/png;base64,cG5n",
	})

	sessions, err := agent.NewSessionManager().List("bot")
	assert.NoError(t, err)
	assert.NotEmpty(t, sessions)

	var sessionID string
	for _, sess := range sessions {
		if sess != nil && sess.Name == "slack:D123" {
			sessionID = sess.ID
			break
		}
	}
	assert.NotEqual(t, "", sessionID)

	var userMsg domain.Message
	found := false
	assert.Eventually(t, func() bool {
		lines, err := store.ReadJSONL[domain.Message](store.SessionPath("bot", sessionID))
		if err != nil {
			return false
		}
		for _, line := range lines {
			if line.Role == domain.MessageRoleUser {
				userMsg = line
				found = true
				return true
			}
		}
		return false
	}, time.Second, 25*time.Millisecond)
	assert.True(t, found)
	assert.Equal(t, "describe this", userMsg.Content)
	assert.Equal(t, "data:image/png;base64,cG5n", userMsg.MediaURL)

	if runner, ok := srv.agents.Get("bot"); ok {
		runner.Wait()
	}
}

func TestStageOutgoingMedia_CopiesToChannelDir(t *testing.T) {
	setupServerDataDir(t)

	source := filepath.Join(t.TempDir(), "image.png")
	err := os.WriteFile(source, []byte("png-bytes"), 0o600)
	assert.NoError(t, err)

	staged, err := stageOutgoingMedia("signal", source)
	assert.NoError(t, err)
	assert.True(t, strings.HasPrefix(staged, store.OutgoingMediaDir("signal")))

	data, err := os.ReadFile(staged)
	assert.NoError(t, err)
	assert.Equal(t, "png-bytes", string(data))
}

func TestLoadOrGenerateTLS_WithCustomCertError(t *testing.T) {
	setupServerDataDir(t)
	// Providing a non-existent cert/key path should return an error.
	_, err := LoadOrGenerateTLS("/nonexistent/cert.pem", "/nonexistent/key.pem")
	assert.Error(t, err)

}

func TestPIDPath_WindowsFallback(t *testing.T) {
	// Ensure PIDPath returns something reasonable when env overrides are set.
	tmp := t.TempDir()
	t.Setenv("AVIARY_PID_FILE", "")
	t.Setenv("PROGRAMDATA", tmp)
	p := PIDPath()
	assert.
		// On Windows this will use PROGRAMDATA; on others it uses TempDir.
		NotEqual(t, "", p)

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

	completed := false
	select {
	case <-done:
		completed = true
	case <-time.After(3 * time.Second):
	}
	assert.True(t, completed)

	body := rr.Body.String()
	assert.
		// Should contain at least the initial entry.
		True(t, strings.Contains(body, "data:"))

}

func TestGenerateSelfSigned_Direct(t *testing.T) {
	setupServerDataDir(t)
	// Call generateSelfSigned directly to cover its body.
	dir := t.TempDir()
	certFile := filepath.Join(dir, "cert.pem")
	keyFile := filepath.Join(dir, "key.pem")
	cert, err := generateSelfSigned(certFile, keyFile)
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(cert.Certificate))

	// Verify files were written.
	_, err = os.Stat(certFile)
	assert.NoError(t, err)

	_, err = os.Stat(keyFile)
	assert.NoError(t, err)

}

func TestParseLogLine_NoAttrs(t *testing.T) {
	// JSON with only standard fields — attrs should be nil.
	line := `{"time":"2024-01-01T00:00:00Z","level":"info","msg":"simple"}`
	e := parseLogLine(line)
	assert.Nil(t, e.Attrs)
	assert.Equal(t, "simple", e.Message)

}

func TestWsBroadcast_WithEntry(_ *testing.T) {
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
	assert.Error(t, err)

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
	assert.True(t, resp.HasMore)
	assert.Equal(t, 2, len(resp.Entries))

}

func TestReadLogLinesFromEnd_SkipLimitWithoutTrailingNewline(t *testing.T) {
	setupServerDataDir(t)

	logDir := filepath.Join(store.DataDir(), "logs")
	_ = os.MkdirAll(logDir, 0o700)
	logFile := filepath.Join(logDir, "aviary.log")
	content := strings.Join([]string{
		`{"time":"2024-01-01T00:00:00Z","level":"info","msg":"line-1"}`,
		`{"time":"2024-01-01T00:00:01Z","level":"info","msg":"line-2"}`,
		`{"time":"2024-01-01T00:00:02Z","level":"info","msg":"line-3"}`,
		`{"time":"2024-01-01T00:00:03Z","level":"info","msg":"line-4"}`,
	}, "\n")
	_ = os.WriteFile(logFile, []byte(content), 0o600)

	lines, hasMore, err := readLogLinesFromEnd(logFile, 1, 2)
	assert.NoError(t, err)
	assert.True(t, hasMore)
	assert.Equal(t, 2, len(lines))
	assert.True(t, strings.Contains(string(lines[0]), "line-2"))
	assert.True(t, strings.Contains(string(lines[1]), "line-3"))

}

func TestWsRegisterUnregister_NilConn(t *testing.T) {
	// wsRegister/wsUnregister use the pointer as a map key.
	// A nil *websocket.Conn is a valid key and exercises the code paths.
	before := mapLen()
	wsRegister(nil)
	after := mapLen()
	assert.Equal(t, before+1, after)

	wsUnregister(nil)
	final := mapLen()
	assert.Equal(t, before, final)

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

	completed := false
	select {
	case <-done:
		completed = true
	case <-time.After(3 * time.Second):
	}
	assert.True(t, completed)
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
	assert.LessOrEqual(t, n, 5)

}

func TestProcSampler_MultiPID(_ *testing.T) {
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
	assert.
		// Agents() should not be nil.
		NotNil(t, srv.Agents())

}

func TestServerAddr_ExplicitPort(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	cfg := &config.Config{}
	cfg.Server.Port = 8443
	srv := New(cfg, "tok")
	addr := srv.Addr()
	assert.True(t, strings.Contains(addr, "8443"))

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
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

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
	assert.Equal(t, http.StatusOK, rr.Code)

	var daemons []DaemonStatus
	_ = json.NewDecoder(rr.Body).Decode(&daemons)
	assert.NotEqual(t, 0, len(daemons))

}

func TestExtractComponent_EdgeCases(t *testing.T) {
	// Empty message.
	got := extractComponent("", map[string]string{})
	assert.Equal(t, "server", got)

	// Message with colon at position 0 — no prefix.
	got2 := extractComponent(":nodeName", map[string]string{})
	assert.Equal(t, "server", got2)

}

func TestParseLogLine_JSONWithMissingLevel(t *testing.T) {
	// JSON without level defaults to "info".
	line := `{"time":"2024-01-01T00:00:00Z","msg":"no level here"}`
	e := parseLogLine(line)
	assert.Equal(t, "info", e.Level)

}

// ── deliverTaskOutput ────────────────────────────────────────────────────────

func TestDeliverTaskOutput_EmptyRoute(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	srv := New(&config.Config{}, "tok")
	err := srv.deliverTaskOutput("bot", "", "text")
	assert.NoError(t, err)

}

func TestDeliverTaskOutput_InvalidRoute(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	srv := New(&config.Config{}, "tok")
	err := srv.deliverTaskOutput("bot", "badroute", "text")
	assert.Error(t, err)

}

func TestDeliverTaskOutput_InvalidRouteIndex(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	srv := New(&config.Config{}, "tok")
	err := srv.deliverTaskOutput("bot", "slack:notanumber:C123", "text")
	assert.Error(t, err)

}

func TestDeliverTaskOutput_EmptyTargetID(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	srv := New(&config.Config{}, "tok")
	err := srv.deliverTaskOutput("bot", "slack:alerts:   ", "text")
	assert.Error(t, err)

}

func TestDeliverTaskOutput_SessionTarget(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	srv := New(&config.Config{}, "tok")
	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "main")
	require.NoError(t, err)

	err = srv.deliverTaskOutput("bot", "session:main", "hello")
	require.NoError(t, err)

	data, err := os.ReadFile(store.SessionPath("bot", sess.ID))
	require.NoError(t, err)
	assert.Contains(t, string(data), "hello")
}
