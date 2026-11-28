package llm

const defaultOllamaBaseURI = "http://127.0.0.1:11434"

// OllamaProvider speaks to an Ollama server via its OpenAI-compatible API.
type OllamaProvider struct {
	openAICompatLocalProvider
}

// NewOllamaProvider creates a provider backed by an Ollama OpenAI-compatible endpoint.
// If baseURI omits the API path, "/v1" is added automatically.
func NewOllamaProvider(apiKey, model, baseURI string) *OllamaProvider {
	return &OllamaProvider{newOpenAICompatLocalProvider("ollama", apiKey, model, normalizeOllamaBaseURI(baseURI))}
}

func normalizeOllamaBaseURI(baseURI string) string {
	return normalizeLocalOpenAIBaseURI(baseURI, defaultOllamaBaseURI)
}
