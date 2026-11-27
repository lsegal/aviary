package llm

const defaultVLLMBaseURI = "http://127.0.0.1:8000"

// VLLMProvider speaks to a vLLM server via its OpenAI-compatible API.
type VLLMProvider struct {
	openAICompatLocalProvider
}

// NewVLLMProvider creates a provider backed by a vLLM OpenAI-compatible endpoint.
// If baseURI omits the API path, "/v1" is added automatically.
func NewVLLMProvider(apiKey, model, baseURI string) *VLLMProvider {
	return &VLLMProvider{newOpenAICompatLocalProvider("vllm", apiKey, model, normalizeVLLMBaseURI(baseURI))}
}

func normalizeVLLMBaseURI(baseURI string) string {
	return normalizeLocalOpenAIBaseURI(baseURI, defaultVLLMBaseURI)
}
