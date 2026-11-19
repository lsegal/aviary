package agents

import (
	"strings"
	"testing"
)

// TestModelsFromJSONReadsACatalogDocument checks a CLI that prints its whole
// catalog as one JSON object is read through the path the definition names,
// which is how codex answers `codex debug models`.
func TestModelsFromJSONReadsACatalogDocument(t *testing.T) {
	doctor := Doctor{Models: []string{"codex", "debug", "models"}, ModelsJSON: "models[].slug"}
	output := `{"models":[{"slug":"gpt-5.6-sol"},{"slug":"gpt-5.6-terra"}]}`
	if got := strings.Join(doctor.ModelsFrom(output), ","); got != "gpt-5.6-sol,gpt-5.6-terra" {
		t.Errorf("ModelsFrom() = %q, want both slugs in the order printed", got)
	}
}

// TestModelsFromJSONFiltersHiddenCatalogRows checks the key=value selector
// keeps the rows a catalog marks as listed, so a model the CLI hides from its
// own picker is not reported as one to write after --agent.
func TestModelsFromJSONFiltersHiddenCatalogRows(t *testing.T) {
	doctor := Doctor{Models: []string{"codex", "debug", "models"}, ModelsJSON: "models[visibility=list].slug"}
	output := `{"models":[{"slug":"hidden","visibility":"hide"},{"slug":"shown","visibility":"list"}]}`
	if got := strings.Join(doctor.ModelsFrom(output), ","); got != "shown" {
		t.Errorf("ModelsFrom() = %q, want only the listed row", got)
	}
}

// TestModelsFromJSONReadsOneResponseAmongMany checks a probe that talks a
// stdio protocol is read line by line: the answer arrives as one JSON-RPC
// response among a handshake and whatever else the agent prints.
func TestModelsFromJSONReadsOneResponseAmongMany(t *testing.T) {
	doctor := Doctor{
		Models:      []string{"cline", "--acp"},
		ModelsStdin: []string{`{"jsonrpc":"2.0","id":0,"method":"initialize"}`},
		ModelsJSON:  "result.models.availableModels[].modelId",
	}
	output := strings.Join([]string{
		"[acp] starting ACP mode over stdio",
		`{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":1}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"models":{"availableModels":[{"modelId":"anthropic/claude-opus-5"},{"modelId":"openai/gpt-5.6-sol"}]}}}`,
	}, "\n")
	if got := strings.Join(doctor.ModelsFrom(output), ","); got != "anthropic/claude-opus-5,openai/gpt-5.6-sol" {
		t.Errorf("ModelsFrom() = %q, want the ids out of the one response that carries them", got)
	}
}

// TestModelsFromJSONIgnoresOutputThatIsNotTheAnswer checks noise, errors, and
// a document shaped differently produce no models rather than garbage: the
// report tells "listed nothing" from "listed something" and nothing else.
func TestModelsFromJSONIgnoresOutputThatIsNotTheAnswer(t *testing.T) {
	doctor := Doctor{Models: []string{"gemini", "--acp"}, ModelsJSON: "result.models.availableModels[].modelId"}
	for _, output := range []string{
		"",
		"not json at all",
		`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"API key is missing"}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"models":{"availableModels":[{"name":"no id here"}]}}}`,
	} {
		if models := doctor.ModelsFrom(output); len(models) != 0 {
			t.Errorf("ModelsFrom(%q) = %v, want nothing", output, models)
		}
	}
}

// TestModelsFromLinesStillReadsAPlainList checks the line reader is untouched
// by the JSON path: a CLI that prints one model per line is still read that
// way, filtered by its pattern.
func TestModelsFromLinesStillReadsAPlainList(t *testing.T) {
	doctor := Doctor{Models: []string{"opencode", "models"}, ModelPattern: `^[A-Za-z0-9._-]+/[A-Za-z0-9._/-]+$`}
	output := "Models:\nanthropic/claude-opus-5\nopenai/gpt-5.6-sol\n"
	if got := strings.Join(doctor.ModelsFrom(output), ","); got != "anthropic/claude-opus-5,openai/gpt-5.6-sol" {
		t.Errorf("ModelsFrom() = %q, want the decorated list read as before", got)
	}
}

// TestParseModelPathRejectsMalformedExpressions checks the parser names the
// segment it could not read, so a definition author is told which one is wrong
// rather than left with an empty model list.
func TestParseModelPathRejectsMalformedExpressions(t *testing.T) {
	for _, expr := range []string{"", "   ", "models[", "models[=list].slug", "models[bad].id", ".slug"} {
		if _, err := parseModelPath(expr); err == nil {
			t.Errorf("parseModelPath(%q) error = nil, want it rejected", expr)
		}
	}
}

// TestModelsFromReadsTheRealGeminiSessionResponse pins the answer
// `gemini --acp` actually gives to the handshake `gemini.json` writes, captured
// from gemini-cli 0.58.0. The response carries `modes` beside `models`, and the
// ids are one level under `availableModels`, so this holds the definition's
// `result.models.availableModels[].modelId` to the shape the CLI really sends
// rather than to the one it was inferred to send.
func TestModelsFromReadsTheRealGeminiSessionResponse(t *testing.T) {
	doctor := Doctor{Models: []string{"gemini", "--acp"}, ModelsJSON: "result.models.availableModels[].modelId"}
	output := strings.Join([]string{
		`{"jsonrpc":"2.0","id":0,"result":{"protocolVersion":1,"agentInfo":{"name":"gemini-cli","title":"Gemini CLI","version":"0.58.0"}}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"sessionId":"cd61e326-d31c-44e8-8b35-59875bca8903","modes":{"availableModes":[{"id":"default","name":"Default","description":"Prompts for approval"},{"id":"yolo","name":"YOLO","description":"Auto-approves all tools"}],"currentModeId":"default"},"models":{"availableModels":[{"modelId":"auto","name":"Auto","description":"Let Gemini CLI decide the best model for the task: gemini-3.1-pro-preview, gemini-3.5-flash"},{"modelId":"gemini-3.1-pro-preview","name":"gemini-3.1-pro-preview"},{"modelId":"gemini-3-flash-preview","name":"gemini-3-flash-preview"},{"modelId":"gemini-2.5-pro","name":"gemini-2.5-pro"},{"modelId":"gemini-3.5-flash","name":"gemini-3.5-flash"},{"modelId":"gemini-3.1-flash-lite","name":"gemini-3.1-flash-lite"}],"currentModelId":"auto"}}}`,
	}, "\n")
	want := "auto,gemini-3.1-pro-preview,gemini-3-flash-preview,gemini-2.5-pro,gemini-3.5-flash,gemini-3.1-flash-lite"
	if got := strings.Join(doctor.ModelsFrom(output), ","); got != want {
		t.Errorf("ModelsFrom() = %q, want %q", got, want)
	}
}

// TestModelsFromReadsTheRealMuseCatalogResponse pins the answer `muse serve`
// gives to `model/list`, captured from Muse Code 1.0.2. The ids sit directly on
// the rows rather than under a nested catalog object, and the result carries
// `providerId`, `profileId`, and `source` beside them, so this holds the
// definition's `result.models[].modelId` to the wire shape muse publishes in
// the MSP schema its own binary exports.
func TestModelsFromReadsTheRealMuseCatalogResponse(t *testing.T) {
	doctor := Doctor{Models: []string{"muse", "serve"}, ModelsJSON: "result.models[].modelId"}
	output := strings.Join([]string{
		`{"jsonrpc":"2.0","id":0,"result":{"serverInfo":{"name":"muse","version":"1.0.2"},"sessionDurability":"durable"}}`,
		`{"jsonrpc":"2.0","id":1,"result":{"providerId":"meta","profileId":"tbh","source":"providerCatalog","models":[{"modelId":"muse-1","displayLabel":"muse-1","providerId":"meta","profileId":"tbh","isActive":true,"isDefault":false,"releaseDate":null,"contextLimit":null,"outputLimit":null,"description":null,"cost":null},{"modelId":"muse-1-mini","displayLabel":"muse-1-mini","providerId":"meta","profileId":"tbh","isActive":false,"isDefault":true,"releaseDate":null,"contextLimit":null,"outputLimit":null,"description":null,"cost":null}]}}`,
	}, "\n")
	if got := strings.Join(doctor.ModelsFrom(output), ","); got != "muse-1,muse-1-mini" {
		t.Errorf("ModelsFrom() = %q, want both catalog ids in the order muse lists them", got)
	}
}

// TestModelsFromReadsAnEmptyMuseCatalogAsNothing pins the other answer muse
// really gives, which is the one a machine signed out of the provider gets: the
// call succeeds and the catalog is empty, because muse composes a logged-out
// fallback rather than failing. That has to read as "listed nothing" so the
// report names the command that could not list it, and it is not a shape error
// -- muse's schema documents an empty `models` as a supported answer, and a row
// the catalog marks hidden never reaches the wire either.
func TestModelsFromReadsAnEmptyMuseCatalogAsNothing(t *testing.T) {
	doctor := Doctor{Models: []string{"muse", "serve"}, ModelsJSON: "result.models[].modelId"}
	output := `{"jsonrpc":"2.0","id":1,"result":{"providerId":"meta","profileId":"tbh","source":"bundledCatalog","models":[]}}`
	if models := doctor.ModelsFrom(output); len(models) != 0 {
		t.Errorf("ModelsFrom() = %v, want nothing from an empty catalog", models)
	}
}
