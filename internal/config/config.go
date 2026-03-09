// Package config handles loading, validation, and watching of aviary.yaml.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config is the top-level configuration for Aviary.
type Config struct {
	Server    ServerConfig    `yaml:"server"              json:"server"`
	Agents    []AgentConfig   `yaml:"agents,omitempty"    json:"agents,omitempty"`
	Models    ModelsConfig    `yaml:"models,omitempty"    json:"models,omitempty"`
	Browser   BrowserConfig   `yaml:"browser,omitempty"   json:"browser,omitempty"`
	Scheduler SchedulerConfig `yaml:"scheduler,omitempty" json:"scheduler,omitempty"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port           int        `yaml:"port,omitempty"            json:"port,omitempty"`
	TLS            *TLSConfig `yaml:"tls,omitempty"             json:"tls,omitempty"`
	ExternalAccess bool       `yaml:"external_access,omitempty" json:"external_access,omitempty"` // bind to 0.0.0.0 instead of 127.0.0.1
	NoTLS          bool       `yaml:"no_tls,omitempty"          json:"no_tls,omitempty"`          // disable TLS (plain HTTP)
}

// TLSConfig holds paths to TLS certificate and key.
type TLSConfig struct {
	Cert string `yaml:"cert,omitempty" json:"cert,omitempty"`
	Key  string `yaml:"key,omitempty"  json:"key,omitempty"`
}

// PermissionsConfig restricts which MCP tools an agent may use.
// When Tools is non-empty it acts as an allow-list: only the named tools are
// offered to the agent.  An empty or absent Permissions block means all tools
// are available (no restriction).
type PermissionsConfig struct {
	Tools []string `yaml:"tools,omitempty" json:"tools,omitempty"`
}

// AgentConfig describes a single agent.
type AgentConfig struct {
	Name         string   `yaml:"name"                    json:"name"`
	Model        string   `yaml:"model"                   json:"model"`
	Fallbacks    []string `yaml:"fallbacks,omitempty"     json:"fallbacks,omitempty"`
	Memory       string   `yaml:"memory,omitempty"        json:"memory,omitempty"`
	MemoryTokens int      `yaml:"memory_tokens,omitempty" json:"memory_tokens,omitempty"`
	CompactKeep  int      `yaml:"compact_keep,omitempty"  json:"compact_keep,omitempty"`
	// Rules is an optional set of operating rules injected at the top of every
	// system prompt for this agent.  It may be inline markdown text or a path
	// to a file (e.g. "./RULES.md"); file paths are resolved relative to the
	// process working directory at prompt time.
	Rules       string             `yaml:"rules,omitempty"       json:"rules,omitempty"`
	Permissions *PermissionsConfig `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	Channels    []ChannelConfig    `yaml:"channels,omitempty"    json:"channels,omitempty"`
	Tasks       []TaskConfig       `yaml:"tasks,omitempty"       json:"tasks,omitempty"`
}

// AllowFromEntry defines a set of allowed senders/groups and optional group-chat
// filtering settings that apply when a message matches this entry.
//
// The From field is a comma-separated list of IDs, each of which may be:
//   - A phone number (Signal) or user ID (Slack/Discord), e.g. "+15551234567"
//   - "*" to match any direct-message sender
//   - "group:*" to match any group/channel message
//   - "group:<id>" to match a specific group ID or channel ID
//
// MentionPrefixes and RespondToMentions only apply when the matched ID is a
// group qualifier (starts with "group:").  For direct-message IDs all messages
// from the matched sender are forwarded without further filtering.
//
// For YAML backward compatibility a plain string entry is equivalent to
// AllowFromEntry{From: "<string>"}.
type AllowFromEntry struct {
	// From is a comma-separated list of sender IDs or group qualifiers.
	From string `yaml:"from" json:"from"`
	// MentionPrefixes is a list of glob patterns matched against the message
	// text in group chats.  At least one must match for the message to be
	// forwarded (unless RespondToMentions is true and the bot is mentioned).
	MentionPrefixes []string `yaml:"mentionPrefixes,omitempty" json:"mentionPrefixes,omitempty"`
	// RespondToMentions, when true, also forwards group messages that directly
	// mention the bot.  On Slack and Discord this checks for platform @mention
	// syntax (e.g. <@BOTID>).  On Signal this uses the envelope's wasMentioned
	// field provided by signal-cli.
	RespondToMentions bool `yaml:"respondToMentions,omitempty" json:"respondToMentions,omitempty"`
	// RestrictTools overrides the agent's tool allow-list for messages that
	// match this entry.  When non-empty only the listed tools are available;
	// an absent or empty slice falls back to the agent-level permissions.
	RestrictTools []string `yaml:"restrictTools,omitempty" json:"restrictTools,omitempty"`
}

// UnmarshalYAML lets a plain YAML string act as AllowFromEntry{From: "<string>"}
// for backward compatibility with the old []string allowFrom format.
func (e *AllowFromEntry) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.ScalarNode {
		e.From = value.Value
		return nil
	}
	type plain AllowFromEntry
	return value.Decode((*plain)(e))
}

// UnmarshalJSON lets a plain JSON string act as AllowFromEntry{From: "<string>"}
// for backward compatibility with the old []string allowFrom format.
func (e *AllowFromEntry) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		e.From = s
		return nil
	}
	type plain AllowFromEntry
	return json.Unmarshal(b, (*plain)(e))
}

// ChannelConfig describes a communication channel for an agent.
type ChannelConfig struct {
	Type      string           `yaml:"type"               json:"type"`
	Token     string           `yaml:"token,omitempty"    json:"token,omitempty"`
	Channel   string           `yaml:"channel,omitempty"  json:"channel,omitempty"`
	Phone     string           `yaml:"phone,omitempty"    json:"phone,omitempty"`
	URL       string           `yaml:"url,omitempty"      json:"url,omitempty"`
	AllowFrom []AllowFromEntry `yaml:"allowFrom,omitempty" json:"allowFrom,omitempty"`
	// ShowTyping controls whether a typing indicator is shown while the agent
	// processes a message. Defaults to true for channels that support it.
	ShowTyping *bool `yaml:"showTyping,omitempty"     json:"showTyping,omitempty"`
	// ReactToEmoji controls whether the agent mirrors emoji reactions placed on
	// its own messages. Defaults to true for channels that support it.
	ReactToEmoji *bool `yaml:"reactToEmoji,omitempty"   json:"reactToEmoji,omitempty"`
	// ReplyToReplies controls whether the agent responds when someone replies
	// to one of its own messages (bypassing normal allowFrom filtering).
	// Defaults to true for channels that support it.
	ReplyToReplies *bool `yaml:"replyToReplies,omitempty" json:"replyToReplies,omitempty"`
}

// BoolOr returns the value of b if non-nil, otherwise def.
func BoolOr(b *bool, def bool) bool {
	if b == nil {
		return def
	}
	return *b
}

// TaskConfig describes a scheduled or file-watch task.
type TaskConfig struct {
	Name     string `yaml:"name"              json:"name"`
	Schedule string `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	StartAt  string `yaml:"start_at,omitempty" json:"start_at,omitempty"`
	RunOnce  bool   `yaml:"run_once,omitempty" json:"run_once,omitempty"`
	Watch    string `yaml:"watch,omitempty"    json:"watch,omitempty"`
	Prompt   string `yaml:"prompt,omitempty"   json:"prompt,omitempty"`
	Channel  string `yaml:"channel,omitempty"  json:"channel,omitempty"`
}

// ModelsConfig holds model provider configuration and defaults.
type ModelsConfig struct {
	Providers map[string]ProviderConfig `yaml:"providers,omitempty" json:"providers,omitempty"`
	Defaults  *ModelDefaults            `yaml:"defaults,omitempty"  json:"defaults,omitempty"`
}

// ProviderConfig holds auth for a model provider.
type ProviderConfig struct {
	Auth string `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// ModelDefaults holds default model settings.
type ModelDefaults struct {
	Model     string   `yaml:"model,omitempty"     json:"model,omitempty"`
	Fallbacks []string `yaml:"fallbacks,omitempty" json:"fallbacks,omitempty"`
}

// BrowserConfig holds browser control settings.
type BrowserConfig struct {
	Binary  string `yaml:"binary,omitempty"            json:"binary,omitempty"`
	CDPPort int    `yaml:"cdp_port,omitempty"          json:"cdp_port,omitempty"`
	// ProfileDir is the Chrome profile folder name in the browser's default
	// user data directory (e.g. "Default", "Profile 1", "work").
	// Defaults to "Aviary" if unset.
	ProfileDir string `yaml:"profile_directory,omitempty" json:"profile_directory,omitempty"`
	Headless   bool   `yaml:"headless,omitempty"          json:"headless,omitempty"`
}

// SchedulerConfig holds scheduler settings.
type SchedulerConfig struct {
	Concurrency any `yaml:"concurrency,omitempty" json:"concurrency,omitempty"` // "auto" or a number
}

// DefaultCDPPort is the default Chrome DevTools Protocol port used when not set in config.
const DefaultCDPPort = 9222

// Default returns a Config populated with sensible defaults.
// Only fields that must be explicitly written to YAML are set here.
// Other defaults (CDPPort, Concurrency) live in the consuming code so that
// unset fields remain absent from the YAML file.
func Default() Config {
	return Config{
		Server: ServerConfig{
			Port: 16677,
		},
	}
}

// normalize strips zero/empty fields that would produce noisy YAML output.
// It is called automatically by Save.
func normalize(cfg *Config) {
	// Nil out TLS block if no cert/key are configured.
	if cfg.Server.TLS != nil && cfg.Server.TLS.Cert == "" && cfg.Server.TLS.Key == "" {
		cfg.Server.TLS = nil
	}
	// Nil out empty slices/maps so they are omitted from YAML.
	if len(cfg.Agents) == 0 {
		cfg.Agents = nil
	}
	if len(cfg.Models.Providers) == 0 {
		cfg.Models.Providers = nil
	}
	if cfg.Models.Defaults != nil && cfg.Models.Defaults.Model == "" && len(cfg.Models.Defaults.Fallbacks) == 0 {
		cfg.Models.Defaults = nil
	}
	for i := range cfg.Agents {
		if len(cfg.Agents[i].Channels) == 0 {
			cfg.Agents[i].Channels = nil
		}
		if len(cfg.Agents[i].Tasks) == 0 {
			cfg.Agents[i].Tasks = nil
		}
		if len(cfg.Agents[i].Fallbacks) == 0 {
			cfg.Agents[i].Fallbacks = nil
		}
		if cfg.Agents[i].Permissions != nil && len(cfg.Agents[i].Permissions.Tools) == 0 {
			cfg.Agents[i].Permissions = nil
		}
	}
	// Strip concurrency if it's the implicit default so it doesn't clutter the YAML.
	if s, ok := cfg.Scheduler.Concurrency.(string); ok && (s == "" || s == "auto") {
		cfg.Scheduler.Concurrency = nil
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
	normalize(cfg)
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
// Only fields present in the file are populated; unset fields remain zero so
// they are omitted from YAML on the next save. Consuming code applies its own
// runtime defaults (e.g. port 16677, CDP port 9222).
func Load(path string) (*Config, error) {
	if path == "" {
		path = DefaultPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{}, nil
		}
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config %s: %w", path, err)
	}

	return &cfg, nil
}
