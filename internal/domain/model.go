package domain

// Provider identifies a model provider.
type Provider string

const (
	ProviderAnthropic Provider = "anthropic"
	ProviderOpenAI    Provider = "openai"
	ProviderGemini    Provider = "gemini"
	ProviderStdio     Provider = "stdio" // subprocess: claude CLI, codex, etc.
)

// Model represents a language model configuration.
type Model struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`     // e.g. "anthropic/claude-sonnet-4.5"
	Provider Provider `json:"provider"` // derived from Name prefix
	Auth     string   `json:"auth"`     // auth reference
}
