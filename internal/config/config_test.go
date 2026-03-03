package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Server.Port != 16677 {
		t.Fatalf("default server port = %d", cfg.Server.Port)
	}
	if cfg.Browser.CDPPort != 9222 {
		t.Fatalf("default cdp port = %d", cfg.Browser.CDPPort)
	}
	if cfg.Scheduler.Concurrency != "auto" {
		t.Fatalf("default scheduler concurrency = %v", cfg.Scheduler.Concurrency)
	}
}

func TestDefaultPath(t *testing.T) {
	t.Run("xdg", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join("C:\\", "tmp", "cfg"))
		got := DefaultPath()
		if !strings.HasSuffix(got, filepath.Join("tmp", "cfg", "aviary", "aviary.yaml")) {
			t.Fatalf("unexpected default path: %s", got)
		}
	})

	t.Run("fallback", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		got := DefaultPath()
		if got == "" || !strings.HasSuffix(got, filepath.Join(".config", "aviary", "aviary.yaml")) {
			t.Fatalf("unexpected fallback path: %s", got)
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("missing returns defaults", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("load missing: %v", err)
		}
		if cfg.Server.Port != 16677 {
			t.Fatalf("expected default port, got %d", cfg.Server.Port)
		}
	})

	t.Run("valid yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")
		yml := "server:\n  port: 9090\nagents:\n  - name: bot\n"
		if err := os.WriteFile(path, []byte(yml), 0o600); err != nil {
			t.Fatal(err)
		}

		cfg, err := Load(path)
		if err != nil {
			t.Fatalf("load valid: %v", err)
		}
		if cfg.Server.Port != 9090 {
			t.Fatalf("expected loaded port 9090, got %d", cfg.Server.Port)
		}
		if len(cfg.Agents) != 1 || cfg.Agents[0].Name != "bot" {
			t.Fatalf("unexpected agents: %+v", cfg.Agents)
		}
	})

	t.Run("bad yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")
		if err := os.WriteFile(path, []byte(": bad: yaml"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := Load(path)
		if err == nil || !strings.Contains(err.Error(), "parsing config") {
			t.Fatalf("expected parsing error, got: %v", err)
		}
	})
}

func TestValidate(t *testing.T) {
	t.Run("empty config has no errors", func(t *testing.T) {
		cfg := &Config{}
		issues := Validate(cfg, nil)
		for _, iss := range issues {
			if iss.Level == LevelError {
				t.Fatalf("validate empty: unexpected error: %+v", iss)
			}
		}
	})

	t.Run("invalid agent name", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: ""}}}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "name is required") {
			t.Fatalf("expected 'name is required' issue, got: %v", issues)
		}
	})

	t.Run("invalid task prompt", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "t1"}}}}}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "prompt is empty") {
			t.Fatalf("expected 'prompt is empty' issue, got: %v", issues)
		}
	})

	t.Run("invalid channel type", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Channels: []ChannelConfig{{Type: ""}}}}}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "type is required") {
			t.Fatalf("expected 'type is required' issue, got: %v", issues)
		}
	})

	t.Run("invalid start_at timestamp", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "t1", Schedule: "*/1 * * * * *", StartAt: "tomorrow", Prompt: "p"}}}}}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "invalid RFC3339 timestamp") {
			t.Fatalf("expected invalid start_at issue, got: %v", issues)
		}
	})

	t.Run("run_once with watch task is invalid", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "t1", Watch: "*.md", RunOnce: true, Prompt: "p"}}}}}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "run_once is only supported for scheduled tasks") {
			t.Fatalf("expected run_once/watch incompatibility issue, got: %v", issues)
		}
	})

	t.Run("run_once requires schedule or start_at", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "t1", RunOnce: true, Prompt: "p"}}}}}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "run_once requires either schedule or start_at") {
			t.Fatalf("expected run_once requirement issue, got: %v", issues)
		}
	})

	t.Run("start_at with watch task is invalid", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "t1", Watch: "*.md", StartAt: time.Now().UTC().Format(time.RFC3339), Prompt: "p"}}}}}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "start_at is only supported for scheduled tasks") {
			t.Fatalf("expected start_at/watch incompatibility issue, got: %v", issues)
		}
	})

	t.Run("openai-codex model requires openai oauth credential", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Model: "openai-codex/gpt-5.2"}}}
		issues := Validate(cfg, func(key string) (string, error) {
			if key == "openai:oauth" {
				return "", os.ErrNotExist
			}
			return "ok", nil
		})
		if !hasIssue(issues, `credential "openai:oauth" not found`) {
			t.Fatalf("expected openai oauth credential issue, got: %v", issues)
		}
	})
}

// hasIssue reports whether any issue's message contains the given substring.
func hasIssue(issues []Issue, msg string) bool {
	for _, iss := range issues {
		if strings.Contains(iss.Message, msg) {
			return true
		}
	}
	return false
}


func TestHelpers(t *testing.T) {
	if got := lastSep("a/b/c"); got != 3 {
		t.Fatalf("lastSep unix = %d", got)
	}
	if got := lastSep("a\\b\\c"); got != 3 {
		t.Fatalf("lastSep win = %d", got)
	}
	if got := lastSep("abc"); got != -1 {
		t.Fatalf("lastSep none = %d", got)
	}
	if got := max(1, 2); got != 2 {
		t.Fatalf("max = %d", got)
	}
	if got := max(4, 2); got != 4 {
		t.Fatalf("max = %d", got)
	}
}

func TestWatcher(t *testing.T) {
	t.Run("new watcher path", func(t *testing.T) {
		w := NewWatcher("custom.yaml")
		if w.path != "custom.yaml" {
			t.Fatalf("watcher path = %s", w.path)
		}
	})

	t.Run("onchange and reload", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")
		if err := os.WriteFile(path, []byte("server:\n  port: 7000\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		w := NewWatcher(path)
		got := make(chan int, 1)
		w.OnChange(func(cfg *Config) { got <- cfg.Server.Port })
		w.reload()

		select {
		case port := <-got:
			if port != 7000 {
				t.Fatalf("reloaded port = %d", port)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("watcher reload did not call handler")
		}
	})

	t.Run("start stop", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")
		if err := os.WriteFile(path, []byte("server:\n  port: 8000\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		w := NewWatcher(path)
		done := make(chan error, 1)
		go func() { done <- w.Start() }()
		time.Sleep(50 * time.Millisecond)
		w.Stop()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("start returned error: %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("watcher start did not stop")
		}
	})
}
