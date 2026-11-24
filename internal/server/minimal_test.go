package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lsegal/aviary/internal/config"
)

func toolCallBody(name string, args any) string {
	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/call",
		"params":  map[string]any{"name": name, "arguments": args},
	})
	return string(data)
}

func TestCheckMinimalToolCalls(t *testing.T) {
	for _, tool := range []string{"project_status", "project_job_retry", "project_job_pause", "task_list", "task_run", "job_logs"} {
		if err := checkMinimalToolCalls([]byte(toolCallBody(tool, map[string]any{}))); err != nil {
			t.Errorf("%s refused: %v", tool, err)
		}
	}
	for _, tool := range []string{"agent_add", "agent_update", "session_send", "channel_create", "auth_set", "server_restart", "server_upgrade", "file_write", "exec", "browser_open", "project_add"} {
		err := checkMinimalToolCalls([]byte(toolCallBody(tool, map[string]any{})))
		if !errors.Is(err, errMinimalBlocked) {
			t.Errorf("%s = %v, want refused", tool, err)
		}
	}
	batch := "[" + toolCallBody("task_list", map[string]any{}) + "," + toolCallBody("agent_delete", map[string]any{}) + "]"
	if err := checkMinimalToolCalls([]byte(batch)); !errors.Is(err, errMinimalBlocked) {
		t.Errorf("a batch hiding agent_delete = %v, want refused", err)
	}
	if err := checkMinimalToolCalls([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`)); err != nil {
		t.Errorf("tools/list refused: %v", err)
	}
}

func TestCheckMinimalAgentRun(t *testing.T) {
	chat := func(args map[string]any) error {
		return checkMinimalToolCalls([]byte(toolCallBody("agent_run", args)))
	}
	// The board's prompt chat names the session; the watch TUI's chat too.
	for _, args := range []map[string]any{
		{"name": config.ProjectsAgentName, "session": config.ProjectsChatSession, "message": "hi"},
		{"name": config.ProjectsAgentName, "session": config.ProjectsChatSession, "session_id": config.ProjectsChatSession, "message": "hi"},
	} {
		if err := chat(args); err != nil {
			t.Errorf("prompt chat %v refused: %v", args, err)
		}
	}
	for _, args := range []map[string]any{
		{"name": "watch", "session": config.ProjectsChatSession, "message": "hi"},
		{"name": config.ProjectsAgentName, "message": "hi"},
		{"name": config.ProjectsAgentName, "session": "main", "message": "hi"},
		{"name": config.ProjectsAgentName, "session": config.ProjectsChatSession, "session_id": "other", "message": "hi"},
	} {
		if err := chat(args); !errors.Is(err, errMinimalBlocked) {
			t.Errorf("agent_run %v = %v, want refused", args, err)
		}
	}
}

func TestCheckMinimalConfigSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	config.SetDefaultPath(path)
	t.Cleanup(func() { config.SetDefaultPath("") })
	saved := &config.Config{
		Server: config.ServerConfig{Port: 17000, DisableTokenAuth: true, NoTLS: true},
		Agents: []config.AgentConfig{{Name: "watch", Projects: config.AgentProjectsConfig{{Target: "o/r"}}}},
	}
	if err := config.Save(path, saved); err != nil {
		t.Fatal(err)
	}
	save := func(cfg config.Config) error {
		data, _ := json.Marshal(cfg)
		args, _ := json.Marshal(map[string]string{"config": string(data)})
		return checkMinimalConfigSave(args)
	}

	allowed := *saved
	allowed.Projects = config.ProjectsConfig{Concurrency: 4}
	allowed.Agents = []config.AgentConfig{{
		Name:     "watch",
		Projects: config.AgentProjectsConfig{{Target: "o/r"}},
		Tasks:    []config.TaskConfig{{Name: "nightly", Schedule: "0 3 * * *", Prompt: "check"}},
	}}
	if err := save(allowed); err != nil {
		t.Errorf("saving Code Loop settings and a task refused: %v", err)
	}

	added := *saved
	added.Agents = append([]config.AgentConfig{saved.Agents[0]}, config.AgentConfig{Name: "new"})
	if err := save(added); !errors.Is(err, errMinimalBlocked) {
		t.Errorf("adding an agent = %v, want refused", err)
	}
	channel := *saved
	channel.Agents = []config.AgentConfig{{Name: "watch", Projects: saved.Agents[0].Projects, Channels: []config.ChannelConfig{{Type: "slack"}}}}
	if err := save(channel); !errors.Is(err, errMinimalBlocked) {
		t.Errorf("adding a channel = %v, want refused", err)
	}
	server := *saved
	server.Server.DisableTokenAuth = false
	if err := save(server); !errors.Is(err, errMinimalBlocked) {
		t.Errorf("changing server settings = %v, want refused", err)
	}
}

func TestMinimalModeRoutes(t *testing.T) {
	SetMinimalMode(true)
	SetWebUIEnabled(false)
	t.Cleanup(func() {
		SetMinimalMode(false)
		SetWebUIEnabled(true)
	})
	s := &Server{cfg: &config.Config{}, token: "tok", mux: http.NewServeMux()}
	s.authDisabled.Store(true)
	s.registerRoutes()

	get := func(path string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		s.mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		return rr
	}
	for _, path := range []string{"/api/daemons", "/api/daemons/logs", "/api/transcription"} {
		if code := get(path).Code; code != http.StatusForbidden {
			t.Errorf("GET %s = %d, want 403", path, code)
		}
	}
	if code := get("/").Code; code != http.StatusNotFound {
		t.Errorf("GET / with the web UI off = %d, want 404", code)
	}
	health := get("/api/health").Body.String()
	if !strings.Contains(health, `"minimal":true`) {
		t.Errorf("health = %s, want minimal", health)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(toolCallBody("agent_add", map[string]any{"name": "x"})))
	req.Header.Set("Content-Type", "application/json")
	s.mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Errorf("POST /mcp agent_add = %d, want 403", rr.Code)
	}
}
