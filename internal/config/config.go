// Package config handles loading, validation, and watching of aviary.yaml.
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/lsegal/aviary/internal/testenv"
)

// Config is the top-level configuration for Aviary.
type Config struct {
	Server    ServerConfig           `yaml:"server"              json:"server"`
	Agents    []AgentConfig          `yaml:"agents,omitempty"    json:"agents,omitempty"`
	Models    ModelsConfig           `yaml:"models,omitempty"    json:"models,omitempty"`
	Browser   BrowserConfig          `yaml:"browser,omitempty"   json:"browser,omitempty"`
	Search    SearchConfig           `yaml:"search,omitempty"    json:"search,omitempty"`
	Scheduler SchedulerConfig        `yaml:"scheduler,omitempty" json:"scheduler,omitempty"`
	Skills    map[string]SkillConfig `yaml:"skills,omitempty" json:"skills,omitempty"`
}

// SkillConfig configures an installed skill runtime.
type SkillConfig struct {
	Enabled  bool           `yaml:"enabled,omitempty"  json:"enabled,omitempty"`
	Settings map[string]any `yaml:"settings,omitempty" json:"settings,omitempty"`
}

// ServerConfig holds HTTP server settings.
type ServerConfig struct {
	Port           int        `yaml:"port,omitempty"            json:"port,omitempty"`
	TLS            *TLSConfig `yaml:"tls,omitempty"             json:"tls,omitempty"`
	ExternalAccess bool       `yaml:"external_access,omitempty" json:"external_access,omitempty"` // bind to 0.0.0.0 instead of 127.0.0.1
	NoTLS          bool       `yaml:"no_tls,omitempty"          json:"no_tls,omitempty"`          // disable TLS (plain HTTP)
	// FailedTaskTimeout is the maximum age of a pending run checkpoint before
	// the agent gives up and notifies the session instead of resuming.
	// Accepts Go duration strings like "6h", "30m". Defaults to 6h if unset.
	FailedTaskTimeout string `yaml:"failed_task_timeout,omitempty" json:"failed_task_timeout,omitempty"`
}

// TLSConfig holds paths to TLS certificate and key.
type TLSConfig struct {
	Cert string `yaml:"cert,omitempty" json:"cert,omitempty"`
	Key  string `yaml:"key,omitempty"  json:"key,omitempty"`
}

// FilesystemPermissionsConfig restricts file tool access to ordered allow/deny
// path rules. Rules use gitignore-style globbing and are processed in order.
type FilesystemPermissionsConfig struct {
	AllowedPaths []string `yaml:"allowedPaths,omitempty" json:"allowedPaths,omitempty"`
}

// ExecPermissionsConfig restricts host command execution for an agent.
// Rules are ordered glob patterns matched against the raw command string.
// A leading "!" negates a match. Rules are processed in order.
type ExecPermissionsConfig struct {
	AllowedCommands  []string `yaml:"allowedCommands,omitempty"  json:"allowedCommands,omitempty"`
	ShellInterpolate bool     `yaml:"shellInterpolate,omitempty" json:"shellInterpolate,omitempty"`
	Shell            string   `yaml:"shell,omitempty"            json:"shell,omitempty"`
}

// PermissionsConfig restricts which MCP tools an agent may use.
// When Tools is non-empty it acts as an allow-list: only the named tools are
// offered to the agent.  An empty or absent Permissions block means all tools
// are available (no restriction).
type PermissionsConfig struct {
	Preset        PermissionsPreset            `yaml:"preset,omitempty"        json:"preset,omitempty"`
	Tools         []string                     `yaml:"tools,omitempty"         json:"tools,omitempty"`
	DisabledTools []string                     `yaml:"disabledTools,omitempty" json:"disabledTools,omitempty"`
	Filesystem    *FilesystemPermissionsConfig `yaml:"filesystem,omitempty"    json:"filesystem,omitempty"`
	Exec          *ExecPermissionsConfig       `yaml:"exec,omitempty"          json:"exec,omitempty"`
}

// AgentConfig describes a single agent.
type AgentConfig struct {
	Name         string   `yaml:"name"                    json:"name"`
	Model        string   `yaml:"model"                   json:"model"`
	Fallbacks    []string `yaml:"fallbacks,omitempty"     json:"fallbacks,omitempty"`
	Memory       string   `yaml:"memory,omitempty"        json:"memory,omitempty"`
	MemoryTokens int      `yaml:"memory_tokens,omitempty" json:"memory_tokens,omitempty"`
	CompactKeep  int      `yaml:"compact_keep,omitempty"  json:"compact_keep,omitempty"`
	// WorkingDir is the default working directory for this agent. When set it
	// overrides the process working directory for file-path resolution and
	// filesystem-policy base-dir expansion.  Supports ~ and environment
	// variable expansion.  Defaults to the process working directory.
	WorkingDir string `yaml:"working_dir,omitempty" json:"working_dir,omitempty"`
	// Rules is an optional set of operating rules injected at the top of every
	// system prompt for this agent.  It may be inline markdown text or a path
	// to a file (e.g. "./RULES.md"); file paths are resolved relative to the
	// agent working directory at prompt time.
	Rules       string             `yaml:"rules,omitempty"       json:"rules,omitempty"`
	Permissions *PermissionsConfig `yaml:"permissions,omitempty" json:"permissions,omitempty"`
	Channels    []ChannelConfig    `yaml:"channels,omitempty"    json:"channels,omitempty"`
	Tasks       []TaskConfig       `yaml:"tasks,omitempty"       json:"tasks,omitempty"`
	// Verbose enables progress status messages before each tool call when the
	// agent is responding via a channel (Slack, Signal, etc.). When true the
	// agent emits a brief "I am doing X..." message before executing each tool,
	// allowing channels that do not support real-time streaming to display
	// incremental updates by sending or editing a status message.
	Verbose *bool `yaml:"verbose,omitempty" json:"verbose,omitempty"`
}

// AllowFromEntry defines a set of allowed senders and optional group-chat
// filtering settings that apply when a message matches this entry.
//
// The From field is a comma-separated list of sender IDs:
//   - A phone number (Signal) or user ID (Slack/Discord), e.g. "+15551234567"
//   - "*" to match any sender (DMs or groups, combined with AllowedGroups for groups)
//
// To allow group/channel messages, AllowedGroups must be set.  When AllowedGroups
// is empty the entry only applies to direct messages.
//
// MentionPrefixes and RespondToMentions apply to group messages by default.
// Set MentionPrefixGroupOnly to false to also require a matching prefix in
// direct messages; when false, DMs from allowed senders must still match a
// MentionPrefixes pattern or trigger RespondToMentions to be forwarded.
// For direct-message IDs all messages from the matched sender are forwarded
// without further filtering when MentionPrefixGroupOnly is true (the default).
//
// For YAML backward compatibility a plain string entry is equivalent to
// AllowFromEntry{From: "<string>"}.
type AllowFromEntry struct {
	// Enabled controls whether this allowFrom entry is active. Defaults to true.
	Enabled *bool `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	// From is a comma-separated list of sender IDs.
	From string `yaml:"from" json:"from"`
	// AllowedGroups is a comma-separated list of group/channel IDs that this
	// entry permits.  Use "*" to allow any group.  When empty (the default) the
	// entry only matches direct messages.
	AllowedGroups string `yaml:"allowedGroups,omitempty" json:"allowedGroups,omitempty"`
	// MentionPrefixes is a list of glob patterns matched against the message
	// text in group chats.  At least one must match for the message to be
	// forwarded (unless RespondToMentions is true and the bot is mentioned).
	MentionPrefixes []string `yaml:"mentionPrefixes,omitempty" json:"mentionPrefixes,omitempty"`
	// ExcludePrefixes is a list of glob patterns matched against the message
	// text.  If any pattern matches, the message is silently dropped regardless
	// of other rules.  Applies to both direct messages and group messages.
	ExcludePrefixes []string `yaml:"excludePrefixes,omitempty" json:"excludePrefixes,omitempty"`
	// RespondToMentions, when true, also forwards group messages that directly
	// mention the bot.  On Slack and Discord this checks for platform @mention
	// syntax (e.g. <@BOTID>).  On Signal this uses the envelope's wasMentioned
	// field provided by signal-cli.
	RespondToMentions bool `yaml:"respondToMentions,omitempty" json:"respondToMentions,omitempty"`
	// MentionPrefixGroupOnly controls whether MentionPrefixes and
	// RespondToMentions filtering is restricted to group chats only.
	// Defaults to true (current behaviour). Set to false to also require a
	// mention prefix in direct messages; DMs without a matching prefix are
	// then silently dropped even when the sender is in the allow-list.
	MentionPrefixGroupOnly *bool `yaml:"mentionPrefixGroupOnly,omitempty" json:"mentionPrefixGroupOnly,omitempty"`
	// RestrictTools overrides the agent's tool allow-list for messages that
	// match this entry.  When non-empty only the listed tools are available;
	// an absent or empty slice falls back to the agent-level permissions.
	RestrictTools []string `yaml:"restrictTools,omitempty" json:"restrictTools,omitempty"`
	// Model overrides the agent's default model for messages matching this entry.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
	// Fallbacks overrides the agent's default fallbacks for messages matching this entry.
	Fallbacks []string `yaml:"fallbacks,omitempty" json:"fallbacks,omitempty"`
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
	Enabled       *bool            `yaml:"enabled,omitempty"         json:"enabled,omitempty"`
	Type          string           `yaml:"type"                    json:"type"`
	Token         string           `yaml:"token,omitempty"         json:"token,omitempty"`
	ID            string           `yaml:"id,omitempty"            json:"id,omitempty"`
	URL           string           `yaml:"url,omitempty"           json:"url,omitempty"`
	AllowFrom     []AllowFromEntry `yaml:"allowFrom,omitempty"     json:"allowFrom,omitempty"`
	DisabledTools []string         `yaml:"disabledTools,omitempty" json:"disabledTools,omitempty"`
	// ShowTyping controls whether a typing indicator is shown while the agent
	// processes a message. Defaults to true for channels that support it.
	ShowTyping *bool `yaml:"showTyping,omitempty"     json:"showTyping,omitempty"`
	// ReactToEmoji controls whether the agent reacts to emoji reactions placed
	// on its own messages. On Signal, this treats the emoji as a prompt and
	// mirrors the same reaction back. Defaults to true for supported channels.
	ReactToEmoji *bool `yaml:"reactToEmoji,omitempty"   json:"reactToEmoji,omitempty"`
	// ReplyToReplies controls whether the agent responds when someone replies
	// to one of its own messages. Replies still have to match the entry's
	// sender/group allowFrom scope, but can continue the conversation without
	// re-satisfying mention-based group gating.
	// Defaults to true for channels that support it.
	ReplyToReplies *bool `yaml:"replyToReplies,omitempty" json:"replyToReplies,omitempty"`
	// SendReadReceipts controls whether the agent sends read receipts for
	// messages it will respond to. Read receipts are only sent for messages
	// that pass the allowFrom filter (i.e. messages the agent will act on).
	// Defaults to true for channels that support it.
	SendReadReceipts *bool `yaml:"sendReadReceipts,omitempty" json:"sendReadReceipts,omitempty"`
	// Model overrides the agent's default model for all messages on this channel.
	Model string `yaml:"model,omitempty" json:"model,omitempty"`
	// Fallbacks overrides the agent's default fallbacks for all messages on this channel.
	Fallbacks []string `yaml:"fallbacks,omitempty" json:"fallbacks,omitempty"`
	// GroupChatHistory is the number of recent group chat messages to log and
	// provide as context to the agent. 0 means use the default (50).
	// Set to -1 to disable group chat history logging entirely.
	GroupChatHistory int `yaml:"group_chat_history,omitempty" json:"group_chat_history,omitempty"`
}

// DefaultGroupChatHistory is the default number of group chat messages retained
// in the chat log and provided as context to the agent.
const DefaultGroupChatHistory = 50

// EffectiveGroupChatHistory returns the number of group chat messages to retain.
// Returns 0 if logging is disabled (GroupChatHistory == -1).
func (c ChannelConfig) EffectiveGroupChatHistory() int {
	if c.GroupChatHistory < 0 {
		return 0 // disabled
	}
	if c.GroupChatHistory == 0 {
		return DefaultGroupChatHistory
	}
	return c.GroupChatHistory
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
	Enabled  *bool  `yaml:"enabled,omitempty"  json:"enabled,omitempty"`
	Name     string `yaml:"name"               json:"name"`
	Type     string `yaml:"type,omitempty"     json:"type,omitempty"`
	Schedule string `yaml:"schedule,omitempty" json:"schedule,omitempty"`
	StartAt  string `yaml:"start_at,omitempty" json:"start_at,omitempty"`
	RunOnce  bool   `yaml:"run_once,omitempty" json:"run_once,omitempty"`
	Watch    string `yaml:"watch,omitempty"    json:"watch,omitempty"`
	Prompt   string `yaml:"prompt,omitempty"   json:"prompt,omitempty"`
	Script   string `yaml:"script,omitempty"   json:"script,omitempty"`
	Target   string `yaml:"target,omitempty"   json:"target,omitempty"`
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
	// ProfileDir is the Chrome user data directory path.
	// Defaults to <OS config dir>/aviary/browser if unset.
	ProfileDir string `yaml:"profile_directory,omitempty" json:"profile_directory,omitempty"`
	Headless   bool   `yaml:"headless,omitempty"          json:"headless,omitempty"`
}

// SearchConfig holds search backend settings.
type SearchConfig struct {
	Web WebSearchConfig `yaml:"web,omitempty" json:"web,omitempty"`
}

// WebSearchConfig holds web search provider credentials.
type WebSearchConfig struct {
	BraveAPIKey string `yaml:"brave_api_key,omitempty" json:"brave_api_key,omitempty"`
}

// SchedulerConfig holds scheduler settings.
type SchedulerConfig struct {
	Concurrency     any   `yaml:"concurrency,omitempty"      json:"concurrency,omitempty"` // "auto" or a number
	PrecomputeTasks *bool `yaml:"precompute_tasks,omitempty" json:"precompute_tasks,omitempty"`
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

// DefaultFailedTaskTimeout is used when failed_task_timeout is not set in config.
const DefaultFailedTaskTimeout = 6 * time.Hour

// EffectiveFailedTaskTimeout returns the parsed duration for FailedTaskTimeout,
// falling back to DefaultFailedTaskTimeout if unset or invalid.
func (s ServerConfig) EffectiveFailedTaskTimeout() time.Duration {
	if s.FailedTaskTimeout == "" {
		return DefaultFailedTaskTimeout
	}
	d, err := time.ParseDuration(s.FailedTaskTimeout)
	if err != nil || d <= 0 {
		return DefaultFailedTaskTimeout
	}
	return d
}

// EffectiveAgentModel returns the runtime model for an agent, preferring the
// agent-specific value and falling back to models.defaults.model.
func EffectiveAgentModel(agent AgentConfig, models ModelsConfig) string {
	model := strings.TrimSpace(agent.Model)
	if model != "" {
		return model
	}
	if models.Defaults == nil {
		return ""
	}
	return strings.TrimSpace(models.Defaults.Model)
}

// EffectiveAgentFallbacks returns the runtime fallback list for an agent,
// preferring the agent-specific list and otherwise using models.defaults.fallbacks.
func EffectiveAgentFallbacks(agent AgentConfig, models ModelsConfig) []string {
	if len(agent.Fallbacks) > 0 {
		out := make([]string, len(agent.Fallbacks))
		copy(out, agent.Fallbacks)
		return out
	}
	if models.Defaults == nil || len(models.Defaults.Fallbacks) == 0 {
		return nil
	}
	out := make([]string, len(models.Defaults.Fallbacks))
	copy(out, models.Defaults.Fallbacks)
	return out
}

// EffectivePrecomputeTasks returns whether prompt tasks should be precompiled
// before scheduling. The default is true when the setting is unset.
func EffectivePrecomputeTasks(s SchedulerConfig) bool {
	return BoolOr(s.PrecomputeTasks, true)
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
	if cfg.Search.Web.BraveAPIKey == "" {
		cfg.Search.Web = WebSearchConfig{}
	}
	if len(cfg.Skills) == 0 {
		cfg.Skills = nil
	}
	if cfg.Models.Defaults != nil && cfg.Models.Defaults.Model == "" && len(cfg.Models.Defaults.Fallbacks) == 0 {
		cfg.Models.Defaults = nil
	}
	for name, sk := range cfg.Skills {
		if len(sk.Settings) == 0 {
			sk.Settings = nil
		}
		if !sk.Enabled && len(sk.Settings) == 0 {
			delete(cfg.Skills, name)
			continue
		}
		cfg.Skills[name] = sk
	}
	if len(cfg.Skills) == 0 {
		cfg.Skills = nil
	}
	for i := range cfg.Agents {
		if cfg.Agents[i].Permissions != nil {
			preset := EffectivePermissionsPreset(cfg.Agents[i].Permissions)
			if preset == PermissionsPresetStandard {
				cfg.Agents[i].Permissions.Preset = ""
			}
			cfg.Agents[i].Permissions.Tools = ClampToolNamesForPreset(preset, cfg.Agents[i].Permissions.Tools)
			cfg.Agents[i].Permissions.DisabledTools = ClampToolNamesForPreset(preset, cfg.Agents[i].Permissions.DisabledTools)
		}
		if len(cfg.Agents[i].Channels) == 0 {
			cfg.Agents[i].Channels = nil
		}
		for j := range cfg.Agents[i].Channels {
			ch := &cfg.Agents[i].Channels[j]
			preset := EffectivePermissionsPreset(cfg.Agents[i].Permissions)
			if len(ch.Fallbacks) == 0 {
				ch.Fallbacks = nil
			}
			ch.DisabledTools = ClampToolNamesForPreset(preset, ch.DisabledTools)
			if len(ch.DisabledTools) == 0 {
				ch.DisabledTools = nil
			}
			if len(ch.AllowFrom) == 0 {
				ch.AllowFrom = nil
			}
			for k := range ch.AllowFrom {
				if len(ch.AllowFrom[k].Fallbacks) == 0 {
					ch.AllowFrom[k].Fallbacks = nil
				}
				if len(ch.AllowFrom[k].MentionPrefixes) == 0 {
					ch.AllowFrom[k].MentionPrefixes = nil
				}
				ch.AllowFrom[k].RestrictTools = ClampToolNamesForPreset(preset, ch.AllowFrom[k].RestrictTools)
				if len(ch.AllowFrom[k].RestrictTools) == 0 {
					ch.AllowFrom[k].RestrictTools = nil
				}
			}
		}
		if len(cfg.Agents[i].Tasks) == 0 {
			cfg.Agents[i].Tasks = nil
		}
		if len(cfg.Agents[i].Fallbacks) == 0 {
			cfg.Agents[i].Fallbacks = nil
		}
		if cfg.Agents[i].Permissions != nil && len(cfg.Agents[i].Permissions.Tools) == 0 {
			cfg.Agents[i].Permissions.Tools = nil
		}
		if cfg.Agents[i].Permissions != nil && len(cfg.Agents[i].Permissions.DisabledTools) == 0 {
			cfg.Agents[i].Permissions.DisabledTools = nil
		}
		if cfg.Agents[i].Permissions != nil &&
			cfg.Agents[i].Permissions.Filesystem != nil &&
			len(cfg.Agents[i].Permissions.Filesystem.AllowedPaths) == 0 {
			cfg.Agents[i].Permissions.Filesystem = nil
		}
		if cfg.Agents[i].Permissions != nil &&
			cfg.Agents[i].Permissions.Exec != nil &&
			len(cfg.Agents[i].Permissions.Exec.AllowedCommands) == 0 {
			cfg.Agents[i].Permissions.Exec.AllowedCommands = nil
		}
		if cfg.Agents[i].Permissions != nil &&
			cfg.Agents[i].Permissions.Exec != nil &&
			len(cfg.Agents[i].Permissions.Exec.AllowedCommands) == 0 &&
			!cfg.Agents[i].Permissions.Exec.ShellInterpolate &&
			cfg.Agents[i].Permissions.Exec.Shell == "" {
			cfg.Agents[i].Permissions.Exec = nil
		}
		if cfg.Agents[i].Permissions != nil &&
			len(cfg.Agents[i].Permissions.Tools) == 0 &&
			len(cfg.Agents[i].Permissions.DisabledTools) == 0 &&
			cfg.Agents[i].Permissions.Filesystem == nil &&
			cfg.Agents[i].Permissions.Exec == nil {
			cfg.Agents[i].Permissions = nil
		}
	}
	// Strip concurrency if it's the implicit default so it doesn't clutter the YAML.
	if s, ok := cfg.Scheduler.Concurrency.(string); ok && (s == "" || s == "auto") {
		cfg.Scheduler.Concurrency = nil
	}
	if EffectivePrecomputeTasks(cfg.Scheduler) {
		cfg.Scheduler.PrecomputeTasks = nil
	}
}

// DefaultPath returns the default path to aviary.yaml.
func DefaultPath() string {
	if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
		return filepath.Join(xdg, "aviary", "aviary.yaml")
	}
	if testHome := testenv.GoTestConfigHome(); testHome != "" {
		return filepath.Join(testHome, "aviary", "aviary.yaml")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "aviary", "aviary.yaml")
}

// BaseDir returns the Aviary config base directory.
// If AVIARY_CONFIG_BASE_DIR is set, it takes precedence. Otherwise it is the
// parent directory containing aviary.yaml.
func BaseDir() string {
	if base := os.Getenv("AVIARY_CONFIG_BASE_DIR"); base != "" {
		return base
	}
	return filepath.Dir(DefaultPath())
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
	if err := backupConfigFile(path); err != nil {
		return err
	}
	var node yaml.Node
	if err := node.Encode(cfg); err != nil {
		return fmt.Errorf("building config yaml node: %w", err)
	}
	applyFoldedStyleToLongStrings(&node)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&node); err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("finalizing config yaml: %w", err)
	}
	data := buf.Bytes()
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return fmt.Errorf("writing config %s: %w", path, err)
	}
	return nil
}

func applyFoldedStyleToLongStrings(node *yaml.Node) {
	applyFoldedStyle(node, false)
}

func applyFoldedStyle(node *yaml.Node, mappingKey bool) {
	if node == nil {
		return
	}
	if !mappingKey && node.Kind == yaml.ScalarNode && node.Tag == "!!str" {
		if strings.Contains(node.Value, "\n") {
			node.Style = yaml.LiteralStyle
		} else if len(node.Value) > 80 {
			node.Style = yaml.FoldedStyle
		}
	}
	if node.Kind == yaml.MappingNode {
		for i, child := range node.Content {
			applyFoldedStyle(child, i%2 == 0)
		}
		return
	}
	for _, child := range node.Content {
		applyFoldedStyle(child, false)
	}
}

func backupConfigFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("reading existing config for backup: %w", err)
	}
	backupDir := filepath.Join(filepath.Dir(path), "backups")
	if err := os.MkdirAll(backupDir, 0o750); err != nil {
		return fmt.Errorf("creating backup dir: %w", err)
	}
	oldest := filepath.Join(backupDir, "aviary.yml.bak.5")
	if err := os.Remove(oldest); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("removing oldest backup: %w", err)
	}
	for i := 4; i >= 1; i-- {
		src := filepath.Join(backupDir, fmt.Sprintf("aviary.yml.bak.%d", i))
		dst := filepath.Join(backupDir, fmt.Sprintf("aviary.yml.bak.%d", i+1))
		if err := os.Rename(src, dst); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("rotating backup %d: %w", i, err)
		}
	}
	if err := os.WriteFile(filepath.Join(backupDir, "aviary.yml.bak.1"), data, 0o640); err != nil {
		return fmt.Errorf("writing config backup: %w", err)
	}
	return nil
}

// RestoreLatestBackup copies the newest rotating backup (aviary.yml.bak.1)
// back to the live config path.
func RestoreLatestBackup(path string) error {
	if path == "" {
		path = DefaultPath()
	}
	backupPath := filepath.Join(filepath.Dir(path), "backups", "aviary.yml.bak.1")
	data, err := os.ReadFile(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("latest config backup not found")
		}
		return fmt.Errorf("reading latest config backup: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return fmt.Errorf("restoring config from backup: %w", err)
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
