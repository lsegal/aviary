package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

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
