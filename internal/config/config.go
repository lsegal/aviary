// Package config handles loading, validation, and watching of aviary.yaml.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for Aviary.
type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Agents    []AgentConfig   `yaml:"agents"`
	Models    ModelsConfig    `yaml:"models"`
	Browser   BrowserConfig   `yaml:"browser"`
	Scheduler SchedulerConfig `yaml:"scheduler"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int       `yaml:"port"`
	TLS  TLSConfig `yaml:"tls"`
}

// TLSConfig holds paths to TLS certificate and key.
type TLSConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}

// AgentConfig describes a single agent.
type AgentConfig struct {
	Name        string          `yaml:"name"`
	Description string          `yaml:"description"`
	Model       string          `yaml:"model"`
	Memory      string          `yaml:"memory"` // "shared", "private", or pool name
	Channels    []ChannelConfig `yaml:"channels"`
	Tasks       []TaskConfig    `yaml:"tasks"`
}

// ChannelConfig describes a communication channel for an agent.
type ChannelConfig struct {
	Type      string   `yaml:"type"`  // "slack", "discord", "signal"
	Token     string   `yaml:"token"` // auth reference, e.g. "auth:slack:workspace"
	Channel   string   `yaml:"channel"`
	Phone     string   `yaml:"phone"`     // Signal phone number
	AllowFrom []string `yaml:"allowFrom"` // User/group allowlist; "*" for all
}

// TaskConfig describes a scheduled or file-watch task.
type TaskConfig struct {
	Name     string `yaml:"name"`
	Schedule string `yaml:"schedule"` // Cron expression
	Watch    string `yaml:"watch"`    // Glob pattern for file watch
	Prompt   string `yaml:"prompt"`
	Channel  string `yaml:"channel"` // "slack", "discord", "last", or omit for silent
}

// ModelsConfig holds model provider configuration and defaults.
type ModelsConfig struct {
	Providers map[string]ProviderConfig `yaml:"providers"`
	Defaults  ModelDefaults             `yaml:"defaults"`
}

// ProviderConfig holds auth for a model provider.
type ProviderConfig struct {
	Auth string `yaml:"auth"` // auth reference, e.g. "auth:anthropic:default"
}

// ModelDefaults holds default model settings.
type ModelDefaults struct {
	Model     string   `yaml:"model"`
	Fallbacks []string `yaml:"fallbacks"`
}

// BrowserConfig holds browser control settings.
type BrowserConfig struct {
	Binary  string `yaml:"binary"`
	CDPPort int    `yaml:"cdp_port"`
}

// SchedulerConfig holds scheduler settings.
type SchedulerConfig struct {
	Concurrency any `yaml:"concurrency"` // "auto" or a number
}

// Default returns a Config populated with sensible defaults.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Port: 16677,
		},
		Browser: BrowserConfig{
			CDPPort: 9222,
		},
		Scheduler: SchedulerConfig{
			Concurrency: "auto",
		},
	}
}

// DefaultPath returns the default path to aviary.yaml.
func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "aviary", "aviary.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "aviary", "aviary.yaml")
}

// Load reads and parses the config file at path.
// If path is empty, DefaultPath() is used.
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := Default()
			return &cfg, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	cfg := Default()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	return &cfg, nil
}
