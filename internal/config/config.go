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
	Server    ServerConfig    `yaml:"server"    json:"server"`
	Agents    []AgentConfig   `yaml:"agents"    json:"agents"`
	Models    ModelsConfig    `yaml:"models"    json:"models"`
	Browser   BrowserConfig   `yaml:"browser"   json:"browser"`
	Scheduler SchedulerConfig `yaml:"scheduler" json:"scheduler"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port int       `yaml:"port" json:"port"`
	TLS  TLSConfig `yaml:"tls"  json:"tls"`
}

// TLSConfig holds paths to TLS certificate and key.
type TLSConfig struct {
	Cert string `yaml:"cert" json:"cert"`
	Key  string `yaml:"key"  json:"key"`
}

// AgentConfig describes a single agent.
type AgentConfig struct {
	Name         string          `yaml:"name"                    json:"name"`
	Model        string          `yaml:"model"                   json:"model"`
	Fallbacks    []string        `yaml:"fallbacks,omitempty"     json:"fallbacks,omitempty"`
	Memory       string          `yaml:"memory,omitempty"        json:"memory,omitempty"`
	MemoryTokens int             `yaml:"memory_tokens,omitempty" json:"memory_tokens,omitempty"`
	CompactKeep  int             `yaml:"compact_keep,omitempty"  json:"compact_keep,omitempty"`
	// Rules is an optional set of operating rules injected at the top of every
	// system prompt for this agent.  It may be inline markdown text or a path
	// to a file (e.g. "./RULES.md"); file paths are resolved relative to the
	// process working directory at prompt time.
	Rules    string          `yaml:"rules,omitempty"         json:"rules,omitempty"`
	Channels []ChannelConfig `yaml:"channels"                json:"channels"`
	Tasks    []TaskConfig    `yaml:"tasks"                   json:"tasks"`
}

// ChannelConfig describes a communication channel for an agent.
type ChannelConfig struct {
	Type      string   `yaml:"type"      json:"type"`
	Token     string   `yaml:"token"     json:"token"`
	Channel   string   `yaml:"channel"   json:"channel"`
	Phone     string   `yaml:"phone"     json:"phone"`
	AllowFrom []string `yaml:"allowFrom" json:"allowFrom"`
}

// TaskConfig describes a scheduled or file-watch task.
type TaskConfig struct {
	Name     string `yaml:"name"     json:"name"`
	Schedule string `yaml:"schedule" json:"schedule"`
	StartAt  string `yaml:"start_at" json:"start_at"`
	RunOnce  bool   `yaml:"run_once" json:"run_once"`
	Watch    string `yaml:"watch"    json:"watch"`
	Prompt   string `yaml:"prompt"   json:"prompt"`
	Channel  string `yaml:"channel"  json:"channel"`
}

// ModelsConfig holds model provider configuration and defaults.
type ModelsConfig struct {
	Providers map[string]ProviderConfig `yaml:"providers" json:"providers"`
	Defaults  ModelDefaults             `yaml:"defaults"  json:"defaults"`
}

// ProviderConfig holds auth for a model provider.
type ProviderConfig struct {
	Auth string `yaml:"auth" json:"auth"`
}

// ModelDefaults holds default model settings.
type ModelDefaults struct {
	Model     string   `yaml:"model"     json:"model"`
	Fallbacks []string `yaml:"fallbacks" json:"fallbacks"`
}

// BrowserConfig holds browser control settings.
type BrowserConfig struct {
	Binary     string `yaml:"binary"            json:"binary"`
	CDPPort    int    `yaml:"cdp_port"          json:"cdp_port"`
	// ProfileDir is the Chrome profile folder name in the browser's default
	// user data directory (e.g. "Default", "Profile 1", "work").
	// Defaults to "Aviary" if unset.
	ProfileDir string `yaml:"profile_directory" json:"profile_directory"`
	Headless   bool   `yaml:"headless"          json:"headless"`
}

// SchedulerConfig holds scheduler settings.
type SchedulerConfig struct {
	Concurrency any `yaml:"concurrency" json:"concurrency"` // "auto" or a number
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

// Save writes cfg to path as YAML (creating parent directories as needed).
// If path is empty, DefaultPath() is used.
func Save(path string, cfg *Config) error {
	if path == "" {
		path = DefaultPath()
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
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
