package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Server.Port != 16677 {
		t.Fatalf("default server port = %d", cfg.Server.Port)
	}
	// CDPPort and Concurrency are intentionally absent from Default() so unset
	// fields don't appear in aviary.yaml; consuming code applies its own fallbacks.
	if cfg.Browser.CDPPort != 0 {
		t.Fatalf("default cdp port should be unset (0), got %d", cfg.Browser.CDPPort)
	}
	if cfg.Scheduler.Concurrency != nil {
		t.Fatalf("default scheduler concurrency should be unset (nil), got %v", cfg.Scheduler.Concurrency)
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

func TestBaseDir(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		t.Setenv("AVIARY_CONFIG_BASE_DIR", "/tmp/aviary-base")
		if got := BaseDir(); got != "/tmp/aviary-base" {
			t.Fatalf("BaseDir() = %q, want %q", got, "/tmp/aviary-base")
		}
	})

	t.Run("default from config path", func(t *testing.T) {
		t.Setenv("AVIARY_CONFIG_BASE_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
		want := filepath.Join("/tmp/xdg-config", "aviary")
		if got := BaseDir(); got != want {
			t.Fatalf("BaseDir() = %q, want %q", got, want)
		}
	})
}

func TestLoad(t *testing.T) {
	t.Run("missing returns empty config", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		cfg, err := Load("")
		if err != nil {
			t.Fatalf("load missing: %v", err)
		}
		// Load returns zero values for missing files; consuming code applies runtime defaults.
		if cfg.Server.Port != 0 {
			t.Fatalf("expected port 0 (unset), got %d", cfg.Server.Port)
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

	t.Run("task names may contain slash", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "folder/subtask", Prompt: "p", Schedule: "*/1 * * * * *"}}}}}
		issues := Validate(cfg, nil)
		if hasIssue(issues, "must not contain '/'") {
			t.Fatalf("expected slash in task name to be allowed, got: %v", issues)
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

	t.Run("gemini oauth token satisfies credential check", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Model: "gemini/gemini-2.0-flash"}}}
		issues := Validate(cfg, func(key string) (string, error) {
			if key == "gemini:oauth" {
				return `{"access_token":"tok"}`, nil
			}
			return "", os.ErrNotExist
		})
		if hasIssue(issues, "gemini") {
			t.Fatalf("expected no gemini credential issue when oauth is set, got: %v", issues)
		}
	})

	t.Run("gemini warns when neither api key nor oauth is set", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Model: "gemini/gemini-2.0-flash"}}}
		issues := Validate(cfg, func(string) (string, error) { return "", os.ErrNotExist })
		if !hasIssue(issues, `credential "gemini:default" not found`) {
			t.Fatalf("expected gemini credential warning, got: %v", issues)
		}
	})

	t.Run("anthropic oauth token satisfies credential check", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Model: "anthropic/claude-sonnet-4-5"}}}
		issues := Validate(cfg, func(key string) (string, error) {
			if key == "anthropic:oauth" {
				return `{"access_token":"tok"}`, nil
			}
			return "", os.ErrNotExist
		})
		if hasIssue(issues, "anthropic") {
			t.Fatalf("expected no anthropic credential issue when oauth is set, got: %v", issues)
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

func TestSave(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")

		cfg := Default()
		cfg.Agents = []AgentConfig{{Name: "mybot", Model: "anthropic/claude-sonnet-4-5"}}
		if err := Save(path, &cfg); err != nil {
			t.Fatalf("Save: %v", err)
		}

		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("Load after Save: %v", err)
		}
		if loaded.Server.Port != cfg.Server.Port {
			t.Errorf("port mismatch: got %d want %d", loaded.Server.Port, cfg.Server.Port)
		}
		if len(loaded.Agents) != 1 || loaded.Agents[0].Name != "mybot" {
			t.Errorf("agents mismatch: %+v", loaded.Agents)
		}
	})

	t.Run("creates parent dir", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nested", "subdir", "aviary.yaml")

		cfg := Default()
		if err := Save(path, &cfg); err != nil {
			t.Fatalf("Save with nested dir: %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("file not created: %v", err)
		}
	})

	t.Run("writes yaml with 2-space indentation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")

		cfg := Default()
		cfg.Agents = []AgentConfig{{
			Name:  "bot",
			Model: "anthropic/claude-sonnet-4-5",
			Channels: []ChannelConfig{{
				Type: "signal",
				AllowFrom: []AllowFromEntry{{
					From: "*",
				}},
			}},
		}}
		if err := Save(path, &cfg); err != nil {
			t.Fatalf("Save: %v", err)
		}

		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile: %v", err)
		}
		text := string(data)
		if !strings.Contains(text, "\n  - name: bot\n") {
			t.Fatalf("expected 2-space list indentation, got:\n%s", text)
		}
		if !strings.Contains(text, "\n          - from: ") {
			t.Fatalf("expected nested 2-space indentation, got:\n%s", text)
		}
	})

	t.Run("normalize empty agents", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")

		cfg := Default()
		cfg.Agents = []AgentConfig{} // empty
		if err := Save(path, &cfg); err != nil {
			t.Fatalf("Save: %v", err)
		}

		loaded, err := Load(path)
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if len(loaded.Agents) != 0 {
			t.Errorf("expected no agents in loaded config, got %d", len(loaded.Agents))
		}
	})

	t.Run("rotates backups up to five", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")

		for i := 0; i < 7; i++ {
			cfg := Default()
			cfg.Server.Port = 16677 + i
			if err := Save(path, &cfg); err != nil {
				t.Fatalf("Save %d: %v", i, err)
			}
		}

		backupDir := filepath.Join(dir, "backups")
		entries, err := os.ReadDir(backupDir)
		if err != nil {
			t.Fatalf("ReadDir backups: %v", err)
		}
		if len(entries) != 5 {
			t.Fatalf("expected 5 backups, got %d", len(entries))
		}

		latestBackup, err := Load(filepath.Join(backupDir, "aviary.yml.bak.1"))
		if err != nil {
			t.Fatalf("Load latest backup: %v", err)
		}
		if latestBackup.Server.Port != 16682 {
			t.Fatalf("expected latest backup port 16682, got %d", latestBackup.Server.Port)
		}

		oldestBackup, err := Load(filepath.Join(backupDir, "aviary.yml.bak.5"))
		if err != nil {
			t.Fatalf("Load oldest backup: %v", err)
		}
		if oldestBackup.Server.Port != 16678 {
			t.Fatalf("expected oldest retained backup port 16678, got %d", oldestBackup.Server.Port)
		}
	})
}

func TestNormalize(t *testing.T) {
	t.Run("empty TLS block removed", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{TLS: &TLSConfig{}}}
		normalize(cfg)
		if cfg.Server.TLS != nil {
			t.Error("expected TLS to be nil after normalization")
		}
	})

	t.Run("TLS with cert preserved", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{TLS: &TLSConfig{Cert: "cert.pem", Key: "key.pem"}}}
		normalize(cfg)
		if cfg.Server.TLS == nil {
			t.Error("expected TLS to be preserved when cert/key are set")
		}
	})

	t.Run("empty agents set to nil", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{}}
		normalize(cfg)
		if cfg.Agents != nil {
			t.Error("expected Agents to be nil after normalization")
		}
	})

	t.Run("empty permissions set to nil", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{
			Name:        "bot",
			Permissions: &PermissionsConfig{Tools: []string{}},
		}}}
		normalize(cfg)
		if cfg.Agents[0].Permissions != nil {
			t.Error("expected empty Permissions to be nil after normalization")
		}
	})

	t.Run("empty disabled tools normalized", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{
			Name: "bot",
			Permissions: &PermissionsConfig{
				Tools:         []string{"tool_a"},
				DisabledTools: []string{},
			},
			Channels: []ChannelConfig{{
				Type:          "slack",
				DisabledTools: []string{},
			}},
		}}}
		normalize(cfg)
		if cfg.Agents[0].Permissions == nil || cfg.Agents[0].Permissions.DisabledTools != nil {
			t.Error("expected empty Permissions.DisabledTools to be nil after normalization")
		}
		if cfg.Agents[0].Channels[0].DisabledTools != nil {
			t.Error("expected empty Channel.DisabledTools to be nil after normalization")
		}
	})

	t.Run("auto concurrency removed", func(t *testing.T) {
		cfg := &Config{Scheduler: SchedulerConfig{Concurrency: "auto"}}
		normalize(cfg)
		if cfg.Scheduler.Concurrency != nil {
			t.Errorf("expected Concurrency=nil after normalize(auto), got %v", cfg.Scheduler.Concurrency)
		}
	})

	t.Run("numeric concurrency preserved", func(t *testing.T) {
		cfg := &Config{Scheduler: SchedulerConfig{Concurrency: 4}}
		normalize(cfg)
		if cfg.Scheduler.Concurrency != 4 {
			t.Errorf("expected Concurrency=4, got %v", cfg.Scheduler.Concurrency)
		}
	})
}

func TestBoolOr(t *testing.T) {
	t.Run("nil returns default", func(t *testing.T) {
		if got := BoolOr(nil, true); got != true {
			t.Fatalf("BoolOr(nil, true) = %v", got)
		}
		if got := BoolOr(nil, false); got != false {
			t.Fatalf("BoolOr(nil, false) = %v", got)
		}
	})

	t.Run("non-nil returns value", func(t *testing.T) {
		b := true
		if got := BoolOr(&b, false); got != true {
			t.Fatalf("BoolOr(&true, false) = %v", got)
		}
		b = false
		if got := BoolOr(&b, true); got != false {
			t.Fatalf("BoolOr(&false, true) = %v", got)
		}
	})
}

func TestAllowFromEntry_UnmarshalYAML(t *testing.T) {
	t.Run("plain string", func(t *testing.T) {
		src := `"+15551234567"`
		var entry AllowFromEntry
		if err := yaml.Unmarshal([]byte(src), &entry); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if entry.From != "+15551234567" {
			t.Errorf("From = %q; want %q", entry.From, "+15551234567")
		}
	})

	t.Run("full struct", func(t *testing.T) {
		src := "from: \"+1\"\nallowedGroups: \"group1\"\nrespondToMentions: true\n"
		var entry AllowFromEntry
		if err := yaml.Unmarshal([]byte(src), &entry); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if entry.From != "+1" {
			t.Errorf("From = %q", entry.From)
		}
		if entry.AllowedGroups != "group1" {
			t.Errorf("AllowedGroups = %q", entry.AllowedGroups)
		}
		if !entry.RespondToMentions {
			t.Error("expected RespondToMentions=true")
		}
	})
}

func TestAllowFromEntry_UnmarshalJSON(t *testing.T) {
	t.Run("plain string", func(t *testing.T) {
		data := []byte(`"+15551234567"`)
		var entry AllowFromEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if entry.From != "+15551234567" {
			t.Errorf("From = %q; want %q", entry.From, "+15551234567")
		}
	})

	t.Run("full struct", func(t *testing.T) {
		data := []byte(`{"from":"+2","allowedGroups":"ch1","respondToMentions":true}`)
		var entry AllowFromEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if entry.From != "+2" || entry.AllowedGroups != "ch1" || !entry.RespondToMentions {
			t.Errorf("unexpected entry: %+v", entry)
		}
	})
}

func TestValidate_ServerPort(t *testing.T) {
	t.Run("valid port", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{Port: 8080}}
		issues := Validate(cfg, nil)
		for _, iss := range issues {
			if iss.Field == "server.port" && iss.Level == LevelError {
				t.Fatalf("unexpected port error: %s", iss.Message)
			}
		}
	})

	t.Run("port out of range high", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{Port: 99999}}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "out of range") {
			t.Fatalf("expected port range error, got: %v", issues)
		}
	})

	t.Run("port out of range low", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{Port: -1}}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "out of range") {
			t.Fatalf("expected port range error, got: %v", issues)
		}
	})

	t.Run("TLS cert without key", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{TLS: &TLSConfig{Cert: "cert.pem"}}}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "tls.cert and tls.key must both be set") {
			t.Fatalf("expected TLS error, got: %v", issues)
		}
	})
}

func TestValidate_UnknownChannelType(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name:     "bot",
			Channels: []ChannelConfig{{Type: "telegram"}},
		}},
	}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "unknown channel type") {
		t.Fatalf("expected unknown channel type issue, got: %v", issues)
	}
}

func TestValidate_ChannelEmptyAllowFrom(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name:     "bot",
			Channels: []ChannelConfig{{Type: "signal"}},
		}},
	}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "empty allowFrom list") {
		t.Fatalf("expected empty allowFrom warning, got: %v", issues)
	}
}

func TestValidate_StdioModelMissingCommand(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name:  "bot",
			Model: "stdio/nonexistent-cmd-xyz-12345",
		}},
	}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "not found in PATH") {
		t.Fatalf("expected stdio not-found issue, got: %v", issues)
	}
}

func TestValidate_UnknownProvider(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name:  "bot",
			Model: "unknown-provider/model-x",
		}},
	}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "unknown provider") {
		t.Fatalf("expected unknown provider issue, got: %v", issues)
	}
}

func TestValidate_InvalidModel(t *testing.T) {
	// Model without slash is invalid.
	cfg := &Config{
		Agents: []AgentConfig{{
			Name:  "bot",
			Model: "noSlashModel",
		}},
	}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "invalid model") {
		t.Fatalf("expected invalid model issue, got: %v", issues)
	}
}

func TestValidate_DuplicateAgentName(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{
			{Name: "bot", Model: "anthropic/claude-sonnet-4-5"},
			{Name: "bot", Model: "anthropic/claude-sonnet-4-5"},
		},
	}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "duplicate agent name") {
		t.Fatalf("expected duplicate agent name issue, got: %v", issues)
	}
}

func TestValidate_BrowserInvalidCDPPort(t *testing.T) {
	cfg := &Config{Browser: BrowserConfig{CDPPort: 99999}}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "CDP port") {
		t.Fatalf("expected CDP port issue, got: %v", issues)
	}
}

func TestValidate_SchedulerInvalidConcurrency(t *testing.T) {
	cfg := &Config{Scheduler: SchedulerConfig{Concurrency: "invalid-str"}}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "invalid string value") {
		t.Fatalf("expected invalid concurrency issue, got: %v", issues)
	}
}

func TestValidate_SchedulerNegativeConcurrency(t *testing.T) {
	cfg := &Config{Scheduler: SchedulerConfig{Concurrency: -1}}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "not positive") {
		t.Fatalf("expected negative concurrency warning, got: %v", issues)
	}
}

func TestValidate_ModelsProviderBadAuth(t *testing.T) {
	cfg := &Config{
		Models: ModelsConfig{
			Providers: map[string]ProviderConfig{
				"myprovider": {Auth: "auth:"},
			},
		},
	}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "malformed auth reference") {
		t.Fatalf("expected malformed auth reference issue, got: %v", issues)
	}
}

func TestValidate_InvalidTaskChannel(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name: "bot",
			Tasks: []TaskConfig{{
				Name:     "t1",
				Schedule: "* * * * * *",
				Prompt:   "do it",
				Channel:  "telegram",
			}},
		}},
	}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "invalid value") {
		t.Fatalf("expected invalid task channel issue, got: %v", issues)
	}
}

func TestValidate_DuplicateTaskName(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name: "bot",
			Tasks: []TaskConfig{
				{Name: "t1", Schedule: "* * * * * *", Prompt: "do it"},
				{Name: "t1", Schedule: "* * * * * *", Prompt: "do it again"},
			},
		}},
	}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "duplicate task name") {
		t.Fatalf("expected duplicate task name issue, got: %v", issues)
	}
}

func TestValidate_InvalidCronExpression(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name: "bot",
			Tasks: []TaskConfig{{
				Name:     "t1",
				Schedule: "not-a-cron",
				Prompt:   "do it",
			}},
		}},
	}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "invalid cron expression") {
		t.Fatalf("expected invalid cron expression issue, got: %v", issues)
	}
}

func TestSchema(t *testing.T) {
	s := Schema()
	if len(s) == 0 {
		t.Fatal("Schema() returned empty bytes")
	}
	// Should be valid JSON.
	var v any
	if err := json.Unmarshal(s, &v); err != nil {
		t.Fatalf("Schema() is not valid JSON: %v", err)
	}
}

func TestUniqueProviderModels(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{
			{Name: "a1", Model: "anthropic/claude-sonnet-4-5"},
			{Name: "a2", Model: "openai/gpt-4"},
			{Name: "a3", Model: "anthropic/claude-opus-4-5"}, // duplicate provider
			{Name: "a4", Model: "stdio/mycommand"},           // excluded
		},
	}
	got := UniqueProviderModels(cfg)
	if len(got) != 2 {
		t.Fatalf("expected 2 unique providers, got %d: %v", len(got), got)
	}
	if _, ok := got["anthropic"]; !ok {
		t.Error("expected 'anthropic' in result")
	}
	if _, ok := got["openai"]; !ok {
		t.Error("expected 'openai' in result")
	}
	if _, ok := got["stdio"]; ok {
		t.Error("expected 'stdio' to be excluded")
	}
}

func TestUniqueProviderModels_WithDefaults(t *testing.T) {
	cfg := &Config{
		Models: ModelsConfig{
			Defaults: &ModelDefaults{
				Model:     "gemini/gemini-2.0-flash",
				Fallbacks: []string{"openai/gpt-4"},
			},
		},
	}
	got := UniqueProviderModels(cfg)
	if len(got) != 2 {
		t.Fatalf("expected 2 providers from defaults, got %d: %v", len(got), got)
	}
}

func TestValidate_ChannelAuthRef(t *testing.T) {
	t.Run("malformed auth ref in token", func(t *testing.T) {
		cfg := &Config{
			Agents: []AgentConfig{{
				Name: "bot",
				Channels: []ChannelConfig{{
					Type:      "slack",
					Token:     "auth:", // malformed
					AllowFrom: []AllowFromEntry{{From: "*"}},
				}},
			}},
		}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "malformed auth reference") {
			t.Fatalf("expected malformed auth ref issue, got: %v", issues)
		}
	})

	t.Run("valid auth ref not found", func(t *testing.T) {
		cfg := &Config{
			Agents: []AgentConfig{{
				Name: "bot",
				Channels: []ChannelConfig{{
					Type:      "slack",
					Token:     "auth:slack:default",
					AllowFrom: []AllowFromEntry{{From: "*"}},
				}},
			}},
		}
		issues := Validate(cfg, func(string) (string, error) { return "", os.ErrNotExist })
		if !hasIssue(issues, "not found in credential store") {
			t.Fatalf("expected auth ref not-found warning, got: %v", issues)
		}
	})

	t.Run("signal phone without + prefix", func(t *testing.T) {
		cfg := &Config{
			Agents: []AgentConfig{{
				Name: "bot",
				Channels: []ChannelConfig{{
					Type:      "signal",
					Phone:     "15551234567", // missing +
					AllowFrom: []AllowFromEntry{{From: "*"}},
				}},
			}},
		}
		issues := Validate(cfg, nil)
		if !hasIssue(issues, "E.164") {
			t.Fatalf("expected E.164 warning, got: %v", issues)
		}
	})
}

func TestValidate_ModelsWithDefaults(t *testing.T) {
	cfg := &Config{
		Models: ModelsConfig{
			Defaults: &ModelDefaults{
				Model:     "anthropic/claude-sonnet-4-5",
				Fallbacks: []string{"openai/gpt-4"},
			},
		},
	}
	// No auth store, so no credential warnings about keys.
	issues := Validate(cfg, nil)
	for _, iss := range issues {
		if iss.Level == LevelError {
			t.Fatalf("unexpected error in model defaults validation: %+v", iss)
		}
	}
}

func TestValidate_ModelsProviderFoundAuth(t *testing.T) {
	cfg := &Config{
		Models: ModelsConfig{
			Providers: map[string]ProviderConfig{
				"myprovider": {Auth: "auth:myprovider:default"},
			},
		},
	}
	issues := Validate(cfg, func(key string) (string, error) {
		if key == "myprovider:default" {
			return "tok", nil
		}
		return "", os.ErrNotExist
	})
	for _, iss := range issues {
		if strings.Contains(iss.Message, "myprovider") && iss.Level == LevelError {
			t.Fatalf("unexpected error: %v", iss)
		}
	}
}

func TestNormalize_Skills(t *testing.T) {
	t.Run("empty skills map set to nil", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{}}
		normalize(cfg)
		if cfg.Skills != nil {
			t.Error("expected Skills to be nil after normalization")
		}
	})

	t.Run("empty skill entry deleted", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{
			"mygog": {},
		}}
		normalize(cfg)
		if cfg.Skills != nil {
			t.Errorf("expected Skills to be nil after deleting empty entry, got %v", cfg.Skills)
		}
	})

	t.Run("enabled skill preserved", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{
			"mygog": {Enabled: true},
		}}
		normalize(cfg)
		if cfg.Skills == nil || !cfg.Skills["mygog"].Enabled {
			t.Error("expected enabled skill to be preserved")
		}
	})

	t.Run("skill with binary preserved", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{
			"mygog": {Binary: "/usr/local/bin/gog"},
		}}
		normalize(cfg)
		if cfg.Skills == nil || cfg.Skills["mygog"].Binary == "" {
			t.Error("expected skill with binary to be preserved")
		}
	})

	t.Run("skill allowed commands normalized", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{
			"mygog": {Enabled: true, AllowedCommands: []string{}},
		}}
		normalize(cfg)
		if cfg.Skills["mygog"].AllowedCommands != nil {
			t.Error("expected empty AllowedCommands to be nil")
		}
	})

	t.Run("skill env normalized", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{
			"mygog": {Enabled: true, Env: map[string]string{}},
		}}
		normalize(cfg)
		if cfg.Skills["mygog"].Env != nil {
			t.Error("expected empty Env to be nil")
		}
	})
}

func TestValidate_BrowserBinaryNotFound(t *testing.T) {
	cfg := &Config{Browser: BrowserConfig{Binary: "/nonexistent/path/to/chrome-xyz-notreal"}}
	issues := Validate(cfg, nil)
	if !hasIssue(issues, "not found") {
		t.Fatalf("expected binary not found issue, got: %v", issues)
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

	t.Run("new watcher empty path uses default", func(t *testing.T) {
		w := NewWatcher("")
		if w.path == "" {
			t.Fatal("expected non-empty path from NewWatcher(\"\")")
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

	t.Run("start detects file write and debounces", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")
		if err := os.WriteFile(path, []byte("server:\n  port: 9000\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		w := NewWatcher(path)
		got := make(chan int, 2)
		w.OnChange(func(cfg *Config) { got <- cfg.Server.Port })

		done := make(chan error, 1)
		go func() { done <- w.Start() }()
		time.Sleep(50 * time.Millisecond)

		// Write to the file — watcher should debounce and reload.
		if err := os.WriteFile(path, []byte("server:\n  port: 9001\n"), 0o600); err != nil {
			t.Fatal(err)
		}

		select {
		case port := <-got:
			if port != 9001 {
				t.Fatalf("expected port 9001, got %d", port)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("watcher did not call handler after file write")
		}

		w.Stop()
		<-done
	})
}
