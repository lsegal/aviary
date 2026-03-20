package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"gopkg.in/yaml.v3"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	assert.Equal(t, 16677, cfg.Server.Port)
	assert.Equal(t, // CDPPort and Concurrency are intentionally absent from Default() so unset
		// fields don't appear in aviary.yaml; consuming code applies its own fallbacks.
		0, cfg.Browser.CDPPort)
	assert.Nil(t, cfg.Scheduler.Concurrency)
	assert.True(t, EffectivePrecomputeTasks(cfg.Scheduler))

}

func TestDefaultPath(t *testing.T) {
	t.Run("xdg", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", filepath.Join("C:\\", "tmp", "cfg"))
		got := DefaultPath()
		assert.True(t, strings.HasSuffix(got, filepath.Join("tmp", "cfg", "aviary", "aviary.yaml")))

	})

	t.Run("fallback", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", "")
		got := DefaultPath()
		assert.NotEqual(t, "", got)
		assert.True(t, strings.Contains(got, filepath.Join("aviary", "aviary.yaml")))
		home, err := os.UserHomeDir()
		if err == nil {
			assert.False(t, strings.HasPrefix(got, filepath.Join(home, ".config", "aviary")))
		}

	})
}

func TestBaseDir(t *testing.T) {
	t.Run("env override", func(t *testing.T) {
		t.Setenv("AVIARY_CONFIG_BASE_DIR", "/tmp/aviary-base")
		got := BaseDir()
		assert.Equal(t, "/tmp/aviary-base", got)

	})

	t.Run("default from config path", func(t *testing.T) {
		t.Setenv("AVIARY_CONFIG_BASE_DIR", "")
		t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-config")
		want := filepath.Join("/tmp/xdg-config", "aviary")
		got := BaseDir()
		assert.Equal(t, want, got)

	})
}

func TestLoad(t *testing.T) {
	t.Run("missing returns empty config", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		cfg, err := Load("")
		assert.NoError(t, err)
		assert.Equal(t, // Load returns zero values for missing files; consuming code applies runtime defaults.
			0, cfg.Server.Port)

	})

	t.Run("valid yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")
		yml := "server:\n  port: 9090\nagents:\n  - name: bot\n"
		err := os.WriteFile(path, []byte(yml), 0o600)
		assert.NoError(t, err)

		cfg, err := Load(path)
		assert.NoError(t, err)
		assert.Equal(t, 9090, cfg.Server.Port)
		assert.Len(t, cfg.Agents, 1)
		assert.Equal(t, "bot", cfg.Agents[0].Name)

	})

	t.Run("bad yaml", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")
		err := os.WriteFile(path, []byte(": bad: yaml"), 0o600)
		assert.NoError(t, err)

		_, err = Load(path)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "parsing config")

	})
}

func TestValidate(t *testing.T) {
	t.Run("empty config has no errors", func(t *testing.T) {
		cfg := &Config{}
		issues := Validate(cfg, nil)
		for _, iss := range issues {
			assert.NotEqual(t, LevelError, iss.Level)

		}
	})

	t.Run("invalid agent name", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: ""}}}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "name is required"))

	})

	t.Run("invalid task prompt", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "t1"}}}}}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "prompt is empty"))

	})

	t.Run("empty task target is valid", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{
			Name:     "t1",
			Schedule: "*/1 * * * * *",
			Prompt:   "p",
		}}}}}
		issues := Validate(cfg, nil)
		assert.False(t, hasIssue(issues, "target"))

	})

	t.Run("session task target is valid", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{
			Name:     "t1",
			Schedule: "*/1 * * * * *",
			Prompt:   "p",
			Target:   "session:main",
		}}}}}
		issues := Validate(cfg, nil)
		assert.False(t, hasIssue(issues, "target"))
	})

	t.Run("task names may contain slash", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "folder/subtask", Prompt: "p", Schedule: "*/1 * * * * *"}}}}}
		issues := Validate(cfg, nil)
		assert.False(t, hasIssue(issues, "must not contain '/'"))

	})

	t.Run("invalid channel type", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Channels: []ChannelConfig{{Type: ""}}}}}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "type is required"))

	})

	t.Run("invalid start_at timestamp", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "t1", Schedule: "*/1 * * * * *", StartAt: "tomorrow", Prompt: "p"}}}}}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "invalid RFC3339 timestamp"))

	})

	t.Run("run_once with watch task is invalid", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "t1", Watch: "*.md", RunOnce: true, Prompt: "p"}}}}}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "run_once is only supported for scheduled tasks"))

	})

	t.Run("run_once requires schedule or start_at", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "t1", RunOnce: true, Prompt: "p"}}}}}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "run_once requires either schedule or start_at"))

	})

	t.Run("start_at with watch task is invalid", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Tasks: []TaskConfig{{Name: "t1", Watch: "*.md", StartAt: time.Now().UTC().Format(time.RFC3339), Prompt: "p"}}}}}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "start_at is only supported for scheduled tasks"))

	})

	t.Run("openai-codex model requires openai oauth credential", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Model: "openai-codex/gpt-5.2"}}}
		issues := Validate(cfg, func(key string) (string, error) {
			if key == "openai:oauth" {
				return "", os.ErrNotExist
			}
			return "ok", nil
		})
		assert.True(t, hasIssue(issues, `credential "openai:oauth" not found`))

	})

	t.Run("gemini oauth token satisfies credential check", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Model: "google-gemini/gemini-2.0-flash"}}}
		issues := Validate(cfg, func(key string) (string, error) {
			if key == "gemini:oauth" {
				return `{"access_token":"tok"}`, nil
			}
			return "", os.ErrNotExist
		})
		assert.False(t, hasIssue(issues, "gemini"))

	})

	t.Run("google-gemini warns when oauth is not set", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Model: "google-gemini/gemini-2.0-flash"}}}
		issues := Validate(cfg, func(string) (string, error) { return "", os.ErrNotExist })
		assert.True(t, hasIssue(issues, `credential "gemini:oauth" not found`))

	})

	t.Run("anthropic oauth token satisfies credential check", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{Name: "bot", Model: "anthropic/claude-sonnet-4-5"}}}
		issues := Validate(cfg, func(key string) (string, error) {
			if key == "anthropic:oauth" {
				return `{"access_token":"tok"}`, nil
			}
			return "", os.ErrNotExist
		})
		assert.False(t, hasIssue(issues, "anthropic"))

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
	got := lastSep("a/b/c")
	assert.Equal(t, 3, got)

	got = lastSep("a\\b\\c")
	assert.Equal(t, 3, got)

	got = lastSep("abc")
	assert.Equal(t, -1, got)

	got = max(1, 2)
	assert.Equal(t, 2, got)

	got = max(4, 2)
	assert.Equal(t, 4, got)

}

func TestSave(t *testing.T) {
	t.Run("round-trip", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")

		cfg := Default()
		cfg.Agents = []AgentConfig{{Name: "mybot", Model: "anthropic/claude-sonnet-4-5"}}
		err := Save(path, &cfg)
		assert.NoError(t, err)

		loaded, err := Load(path)
		assert.NoError(t, err)
		assert.Equal(t, cfg.Server.Port, loaded.Server.Port)
		assert.Len(t, loaded.Agents, 1)
		assert.Equal(t, "mybot", loaded.Agents[0].Name)

	})

	t.Run("creates parent dir", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "nested", "subdir", "aviary.yaml")

		cfg := Default()
		err := Save(path, &cfg)
		assert.NoError(t, err)

		_, err = os.Stat(path)
		assert.NoError(t, err)

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
		err := Save(path, &cfg)
		assert.NoError(t, err)

		data, err := os.ReadFile(path)
		assert.NoError(t, err)

		text := string(data)
		assert.True(t, strings.Contains(text, "\n  - name: bot\n"))
		assert.True(t, strings.Contains(text, "\n          - from: "))

	})

	t.Run("writes long strings using folded style", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")

		cfg := Default()
		cfg.Agents = []AgentConfig{{
			Name:  "bot",
			Model: "anthropic/claude-sonnet-4-5",
			Rules: "This is a deliberately long rules string that should be emitted with folded YAML style once it exceeds eighty characters.",
		}}
		err := Save(path, &cfg)
		assert.NoError(t, err)

		data, err := os.ReadFile(path)
		assert.NoError(t, err)

		text := string(data)
		assert.Contains(t, text, "rules: >")

		loaded, err := Load(path)
		assert.NoError(t, err)
		assert.Equal(t, cfg.Agents[0].Rules, loaded.Agents[0].Rules)
	})

	t.Run("writes multiline task prompts using literal style", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")

		cfg := Default()
		cfg.Agents = []AgentConfig{{
			Name:  "bot",
			Model: "anthropic/claude-sonnet-4-5",
			Tasks: []TaskConfig{{
				Name:     "daily-briefing",
				Schedule: "0 9 * * *",
				Prompt:   "line 1\nline 2\n\nline 4\n",
			}},
		}}
		err := Save(path, &cfg)
		assert.NoError(t, err)

		data, err := os.ReadFile(path)
		assert.NoError(t, err)

		text := string(data)
		assert.Contains(t, text, "prompt: |")

		loaded, err := Load(path)
		assert.NoError(t, err)
		assert.Equal(t, cfg.Agents[0].Tasks[0].Prompt, loaded.Agents[0].Tasks[0].Prompt)
	})

	t.Run("normalize empty agents", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")

		cfg := Default()
		cfg.Agents = []AgentConfig{}
		// empty
		err := Save(path, &cfg)
		assert.NoError(t, err)

		loaded, err := Load(path)
		assert.NoError(t, err)
		assert.Equal(t, 0, len(loaded.Agents))

	})

	t.Run("rotates backups up to five", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")

		for i := 0; i < 7; i++ {
			cfg := Default()
			cfg.Server.Port = 16677 + i
			err := Save(path, &cfg)
			assert.NoError(t, err)

		}

		backupDir := filepath.Join(dir, "backups")
		entries, err := os.ReadDir(backupDir)
		assert.NoError(t, err)
		assert.Equal(t, 5, len(entries))

		latestBackup, err := Load(filepath.Join(backupDir, "aviary.yml.bak.1"))
		assert.NoError(t, err)
		assert.Equal(t, 16682, latestBackup.Server.Port)

		oldestBackup, err := Load(filepath.Join(backupDir, "aviary.yml.bak.5"))
		assert.NoError(t, err)
		assert.Equal(t, 16678, oldestBackup.Server.Port)

	})
}

func TestNormalize(t *testing.T) {
	t.Run("empty TLS block removed", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{TLS: &TLSConfig{}}}
		normalize(cfg)
		assert.Nil(t, cfg.Server.TLS)

	})

	t.Run("TLS with cert preserved", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{TLS: &TLSConfig{Cert: "cert.pem", Key: "key.pem"}}}
		normalize(cfg)
		assert.NotNil(t, cfg.Server.TLS)

	})

	t.Run("empty agents set to nil", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{}}
		normalize(cfg)
		assert.Nil(t, cfg.Agents)

	})

	t.Run("empty permissions set to nil", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{
			Name:        "bot",
			Permissions: &PermissionsConfig{Tools: []string{}},
		}}}
		normalize(cfg)
		assert.Nil(t, cfg.Agents[0].Permissions)

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
		assert.NotNil(t, cfg.Agents[0].Permissions)
		assert.Nil(t, cfg.Agents[0].Permissions.DisabledTools)
		assert.Nil(t, cfg.Agents[0].Channels[0].DisabledTools)

	})

	t.Run("empty filesystem permissions normalized", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{
			Name: "bot",
			Permissions: &PermissionsConfig{
				Filesystem: &FilesystemPermissionsConfig{AllowedPaths: []string{}},
			},
		}}}
		normalize(cfg)
		assert.Nil(t, cfg.Agents[0].Permissions)
	})

	t.Run("empty exec allowed commands normalized", func(t *testing.T) {
		cfg := &Config{Agents: []AgentConfig{{
			Name: "bot",
			Permissions: &PermissionsConfig{
				Exec: &ExecPermissionsConfig{AllowedCommands: []string{}},
			},
		}}}
		normalize(cfg)
		assert.Nil(t, cfg.Agents[0].Permissions)
	})

	t.Run("auto concurrency removed", func(t *testing.T) {
		cfg := &Config{Scheduler: SchedulerConfig{Concurrency: "auto"}}
		normalize(cfg)
		assert.Nil(t, cfg.Scheduler.Concurrency)

	})

	t.Run("numeric concurrency preserved", func(t *testing.T) {
		cfg := &Config{Scheduler: SchedulerConfig{Concurrency: 4}}
		normalize(cfg)
		assert.Equal(t, 4, cfg.Scheduler.Concurrency)

	})

	t.Run("default precompute_tasks omitted when enabled", func(t *testing.T) {
		enabled := true
		cfg := &Config{Scheduler: SchedulerConfig{PrecomputeTasks: &enabled}}
		normalize(cfg)
		assert.Nil(t, cfg.Scheduler.PrecomputeTasks)
	})

	t.Run("disabled precompute_tasks preserved", func(t *testing.T) {
		disabled := false
		cfg := &Config{Scheduler: SchedulerConfig{PrecomputeTasks: &disabled}}
		normalize(cfg)
		assert.NotNil(t, cfg.Scheduler.PrecomputeTasks)
		assert.False(t, *cfg.Scheduler.PrecomputeTasks)
	})

	t.Run("enabled task omitted when true and preserved when false", func(t *testing.T) {
		disabled := false
		cfg := &Config{Agents: []AgentConfig{{
			Name: "bot",
			Tasks: []TaskConfig{
				{Name: "enabled-task", Prompt: "p"},
				{Name: "disabled-task", Prompt: "p", Enabled: &disabled},
			},
		}}}
		normalize(cfg)
		assert.Nil(t, cfg.Agents[0].Tasks[0].Enabled)
		assert.NotNil(t, cfg.Agents[0].Tasks[1].Enabled)
		assert.False(t, *cfg.Agents[0].Tasks[1].Enabled)
	})
}

func TestEffectivePrecomputeTasks(t *testing.T) {
	t.Run("defaults to true", func(t *testing.T) {
		assert.True(t, EffectivePrecomputeTasks(SchedulerConfig{}))
	})

	t.Run("respects disabled setting", func(t *testing.T) {
		disabled := false
		assert.False(t, EffectivePrecomputeTasks(SchedulerConfig{PrecomputeTasks: &disabled}))
	})
}

func TestBoolOr(t *testing.T) {
	t.Run("nil returns default", func(t *testing.T) {
		got := BoolOr(nil, true)
		assert.Equal(t, true, got)

		got = BoolOr(nil, false)
		assert.Equal(t, false, got)

	})

	t.Run("non-nil returns value", func(t *testing.T) {
		b := true
		got := BoolOr(&b, false)
		assert.Equal(t, true, got)

		b = false
		got = BoolOr(&b, true)
		assert.Equal(t, false, got)

	})
}

func TestAllowFromEntry_UnmarshalYAML(t *testing.T) {
	t.Run("plain string", func(t *testing.T) {
		src := `"+15551234567"`
		var entry AllowFromEntry
		err := yaml.Unmarshal([]byte(src), &entry)
		assert.NoError(t, err)

		assert.Equal(t, "+15551234567", entry.From)

	})

	t.Run("full struct", func(t *testing.T) {
		src := "from: \"+1\"\nallowedGroups: \"group1\"\nrespondToMentions: true\n"
		var entry AllowFromEntry
		err := yaml.Unmarshal([]byte(src), &entry)
		assert.NoError(t, err)

		assert.Equal(t, "+1", entry.From)
		assert.Equal(t, "group1", entry.AllowedGroups)
		assert.True(t, entry.RespondToMentions)

	})
}

func TestAllowFromEntry_UnmarshalJSON(t *testing.T) {
	t.Run("plain string", func(t *testing.T) {
		data := []byte(`"+15551234567"`)
		var entry AllowFromEntry
		err := json.Unmarshal(data, &entry)
		assert.NoError(t, err)

		assert.Equal(t, "+15551234567", entry.From)

	})

	t.Run("full struct", func(t *testing.T) {
		data := []byte(`{"from":"+2","allowedGroups":"ch1","respondToMentions":true}`)
		var entry AllowFromEntry
		err := json.Unmarshal(data, &entry)
		assert.NoError(t, err)

		assert.Equal(t, "+2", entry.From)
		assert.Equal(t, "ch1", entry.AllowedGroups)
		assert.True(t, entry.RespondToMentions)

	})
}

func TestValidate_ServerPort(t *testing.T) {
	t.Run("valid port", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{Port: 8080}}
		issues := Validate(cfg, nil)
		for _, iss := range issues {
			assert.NotEqual(t, "server.port", iss.Field)

		}
	})

	t.Run("port out of range high", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{Port: 99999}}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "out of range"))

	})

	t.Run("port out of range low", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{Port: -1}}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "out of range"))

	})

	t.Run("TLS cert without key", func(t *testing.T) {
		cfg := &Config{Server: ServerConfig{TLS: &TLSConfig{Cert: "cert.pem"}}}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "tls.cert and tls.key must both be set"))

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
	assert.True(t, hasIssue(issues, "unknown channel type"))

}

func TestValidate_ChannelEmptyAllowFrom(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name:     "bot",
			Channels: []ChannelConfig{{Type: "signal"}},
		}},
	}
	issues := Validate(cfg, nil)
	assert.True(t, hasIssue(issues, "no enabled allowFrom entries"))

}

func TestValidate_DisabledChannelSkipsAllowFromWarning(t *testing.T) {
	disabled := false
	cfg := &Config{
		Agents: []AgentConfig{{
			Name: "bot",
			Channels: []ChannelConfig{{
				Type:    "signal",
				Enabled: &disabled,
			}},
		}},
	}
	issues := Validate(cfg, nil)
	assert.False(t, hasIssue(issues, "allowFrom"))

}

func TestValidate_StdioModelMissingCommand(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name:  "bot",
			Model: "stdio/nonexistent-cmd-xyz-12345",
		}},
	}
	issues := Validate(cfg, nil)
	assert.True(t, hasIssue(issues, "not found in PATH"))

}

func TestValidate_UnknownProvider(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name:  "bot",
			Model: "unknown-provider/model-x",
		}},
	}
	issues := Validate(cfg, nil)
	assert.True(t, hasIssue(issues, "unknown provider"))

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
	assert.True(t, hasIssue(issues, "invalid model"))

}

func TestValidate_DuplicateAgentName(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{
			{Name: "bot", Model: "anthropic/claude-sonnet-4-5"},
			{Name: "bot", Model: "anthropic/claude-sonnet-4-5"},
		},
	}
	issues := Validate(cfg, nil)
	assert.True(t, hasIssue(issues, "duplicate agent name"))

}

func TestValidate_BrowserInvalidCDPPort(t *testing.T) {
	cfg := &Config{Browser: BrowserConfig{CDPPort: 99999}}
	issues := Validate(cfg, nil)
	assert.True(t, hasIssue(issues, "CDP port"))

}

func TestValidate_SchedulerInvalidConcurrency(t *testing.T) {
	cfg := &Config{Scheduler: SchedulerConfig{Concurrency: "invalid-str"}}
	issues := Validate(cfg, nil)
	assert.True(t, hasIssue(issues, "invalid string value"))

}

func TestValidate_SchedulerNegativeConcurrency(t *testing.T) {
	cfg := &Config{Scheduler: SchedulerConfig{Concurrency: -1}}
	issues := Validate(cfg, nil)
	assert.True(t, hasIssue(issues, "not positive"))

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
	assert.True(t, hasIssue(issues, "malformed auth reference"))

}

func TestValidate_InvalidTaskTarget(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name: "bot",
			Tasks: []TaskConfig{{
				Name:     "t1",
				Schedule: "* * * * * *",
				Prompt:   "do it",
				Target:   "telegram",
			}},
		}},
	}
	issues := Validate(cfg, nil)
	assert.True(t, hasIssue(issues, "invalid value"))

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
	assert.True(t, hasIssue(issues, "duplicate task name"))

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
	assert.True(t, hasIssue(issues, "invalid cron expression"))

}

func TestValidate_AcceptsStandardCronExpression(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name: "bot",
			Tasks: []TaskConfig{{
				Name:     "t1",
				Schedule: "*/5 * * * *",
				Prompt:   "do it",
			}},
		}},
	}
	issues := Validate(cfg, nil)
	assert.False(t, hasIssue(issues, "invalid cron expression"))
}

func TestSchema(t *testing.T) {
	s := Schema()
	assert.NotEqual(t, 0, len(s))

	// Should be valid JSON.
	var v any
	err := json.Unmarshal(s, &v)
	assert.NoError(t, err)

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
	assert.Equal(t, 2, len(got))
	_, ok := got["anthropic"]
	assert.True(t, ok)

	_, ok = got["openai"]
	assert.True(t, ok)

	_, ok = got["stdio"]
	assert.False(t, ok)

}

func TestUniqueProviderModels_WithDefaults(t *testing.T) {
	cfg := &Config{
		Models: ModelsConfig{
			Defaults: &ModelDefaults{
				Model:     "google-gemini/gemini-2.0-flash",
				Fallbacks: []string{"openai/gpt-4"},
			},
		},
	}
	got := UniqueProviderModels(cfg)
	assert.Equal(t, 2, len(got))

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
		assert.True(t, hasIssue(issues, "malformed auth reference"))

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
		assert.True(t, hasIssue(issues, "not found in credential store"))

	})

	t.Run("signal id without + prefix", func(t *testing.T) {
		cfg := &Config{
			Agents: []AgentConfig{{
				Name: "bot",
				Channels: []ChannelConfig{{
					Type:      "signal",
					ID:        "15551234567", // missing +
					AllowFrom: []AllowFromEntry{{From: "*"}},
				}},
			}},
		}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "E.164"))

	})

	t.Run("exec permissions require allowlist", func(t *testing.T) {
		cfg := &Config{
			Agents: []AgentConfig{{
				Name:  "bot",
				Model: "anthropic/claude-sonnet-4-5",
				Permissions: &PermissionsConfig{
					Exec: &ExecPermissionsConfig{},
				},
			}},
		}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "permissions.exec requires at least one allowedCommands entry"))
	})

	t.Run("invalid permissions preset", func(t *testing.T) {
		cfg := &Config{
			Agents: []AgentConfig{{
				Name:  "bot",
				Model: "anthropic/claude-sonnet-4-5",
				Permissions: &PermissionsConfig{
					Preset: PermissionsPreset("locked-down"),
				},
			}},
		}
		issues := Validate(cfg, nil)
		assert.True(t, hasIssue(issues, "invalid permissions preset"))
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
		assert.NotEqual(t, LevelError, iss.Level)

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
		assert.False(t, strings.Contains(iss.Message, "myprovider"))

	}
}

func TestNormalize_Skills(t *testing.T) {
	t.Run("empty skills map set to nil", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{}}
		normalize(cfg)
		assert.Nil(t, cfg.Skills)

	})

	t.Run("empty skill entry deleted", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{
			"mygog": {},
		}}
		normalize(cfg)
		assert.Nil(t, cfg.Skills)

	})

	t.Run("enabled skill preserved", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{
			"mygog": {Enabled: true},
		}}
		normalize(cfg)
		assert.NotNil(t, cfg.Skills)
		assert.True(t, cfg.Skills["mygog"].Enabled)

	})

	t.Run("skill with settings preserved", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{
			"mygog": {Settings: map[string]any{"binary": "/usr/local/bin/gog"}},
		}}
		normalize(cfg)
		assert.NotNil(t, cfg.Skills)
		assert.NotEmpty(t, cfg.Skills["mygog"].Settings)

	})

	t.Run("skill empty settings normalized", func(t *testing.T) {
		cfg := &Config{Skills: map[string]SkillConfig{
			"mygog": {Enabled: true, Settings: map[string]any{}},
		}}
		normalize(cfg)
		assert.Nil(t, cfg.Skills["mygog"].Settings)

	})
}

func TestNormalize_PermissionsPresetClampsInaccessibleTools(t *testing.T) {
	cfg := &Config{
		Agents: []AgentConfig{{
			Name: "bot",
			Permissions: &PermissionsConfig{
				Preset:        PermissionsPresetMinimal,
				Tools:         []string{"task_run", "auth_set", "browser_open", "usage_query"},
				DisabledTools: []string{"job_list", "server_status"},
			},
			Channels: []ChannelConfig{{
				DisabledTools: []string{"browser_open", "task_run"},
				AllowFrom: []AllowFromEntry{{
					From:          "*",
					RestrictTools: []string{"task_run", "auth_set", "usage_query"},
				}},
			}},
		}},
	}

	normalize(cfg)

	perms := cfg.Agents[0].Permissions
	if assert.NotNil(t, perms) {
		assert.Equal(t, PermissionsPresetMinimal, perms.Preset)
		assert.Equal(t, []string{"task_run"}, perms.Tools)
		assert.Equal(t, []string{"job_list"}, perms.DisabledTools)
	}
	assert.Equal(t, []string{"task_run"}, cfg.Agents[0].Channels[0].DisabledTools)
	assert.Equal(t, []string{"task_run"}, cfg.Agents[0].Channels[0].AllowFrom[0].RestrictTools)
}

func TestValidate_BrowserBinaryNotFound(t *testing.T) {
	cfg := &Config{Browser: BrowserConfig{Binary: "/nonexistent/path/to/chrome-xyz-notreal"}}
	issues := Validate(cfg, nil)
	assert.True(t, hasIssue(issues, "not found"))

}

func TestWatcher(t *testing.T) {
	t.Run("new watcher path", func(t *testing.T) {
		w := NewWatcher("custom.yaml")
		assert.Equal(t, "custom.yaml", w.path)

	})

	t.Run("onchange and reload", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")
		err := os.WriteFile(path, []byte("server:\n  port: 7000\n"), 0o600)
		assert.NoError(t, err)

		w := NewWatcher(path)
		got := make(chan int, 1)
		w.OnChange(func(cfg *Config) { got <- cfg.Server.Port })
		w.reload()

		select {
		case port := <-got:
			assert.Equal(t, 7000, port)
		case <-time.After(1 * time.Second):
			assert.FailNow(t, "timeout")
		}
	})

	t.Run("new watcher empty path uses default", func(t *testing.T) {
		w := NewWatcher("")
		assert.NotEqual(t, "", w.path)

	})

	t.Run("start stop", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")
		err := os.WriteFile(path, []byte("server:\n  port: 8000\n"), 0o600)
		assert.NoError(t, err)

		w := NewWatcher(path)
		done := make(chan error, 1)
		go func() { done <- w.Start() }()
		time.Sleep(50 * time.Millisecond)
		w.Stop()

		select {
		case err := <-done:
			assert.NoError(t, err)
		case <-time.After(2 * time.Second):
			assert.FailNow(t, "timeout")
		}
	})

	t.Run("start detects file write and debounces", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "aviary.yaml")
		err := os.WriteFile(path, []byte("server:\n  port: 9000\n"), 0o600)
		assert.NoError(t, err)

		w := NewWatcher(path)
		got := make(chan int, 2)
		w.OnChange(func(cfg *Config) { got <- cfg.Server.Port })

		done := make(chan error, 1)
		go func() { done <- w.Start() }()
		time.Sleep(50 * time.Millisecond)

		// Write to the file — watcher should debounce and reload.
		err = os.WriteFile(path, []byte("server:\n  port: 9001\n"), 0o600)
		assert.NoError(t, err)

		select {
		case port := <-got:
			assert.Equal(t, 9001, port)
		case <-time.After(2 * time.Second):
			assert.FailNow(t, "timeout")
		}

		w.Stop()
		<-done
	})
}
