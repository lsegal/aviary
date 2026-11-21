package cliagent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
)

// TestGeminiChatServesAviaryTools runs the real Gemini CLI through the
// built-in gemini definition against a local stand-in model, reached through
// GOOGLE_GEMINI_BASE_URL with gemini-api-key auth, and a stand-in for
// Aviary's scoped MCP endpoint. The model calls the endpoint's tool, so the
// test checks the CLI registered the server from the settings file with the
// token from the environment, got the system prompt as its system
// instruction rather than in the turn, and that the call is left to Aviary's
// MCP boundary to report.
//
// It needs gemini on PATH, so it runs only when AVIARY_GEMINI_E2E is set.
func TestGeminiChatServesAviaryTools(t *testing.T) {
	if os.Getenv("AVIARY_GEMINI_E2E") == "" {
		t.Skip("set AVIARY_GEMINI_E2E=1 to run against the installed gemini CLI")
	}
	binary, err := exec.LookPath("gemini")
	require.NoError(t, err)

	const token = "scoped-token-e2e"
	var mu sync.Mutex
	var calls []string
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "aviary"}, nil)
	sdkmcp.AddTool(server, &sdkmcp.Tool{Name: "echo", Description: "Echoes its text."},
		func(_ context.Context, _ *sdkmcp.CallToolRequest, in struct {
			Text string `json:"text"`
		}) (*sdkmcp.CallToolResult, struct{}, error) {
			mu.Lock()
			calls = append(calls, in.Text)
			mu.Unlock()
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "echoed " + in.Text}}}, struct{}{}, nil
		})
	mcpHandler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return server },
		&sdkmcp.StreamableHTTPOptions{Stateless: true})
	mcpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		mcpHandler.ServeHTTP(w, r)
	}))
	defer mcpServer.Close()

	var systemInstructions []string
	model := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Contents []struct {
				Parts []map[string]any `json:"parts"`
			} `json:"contents"`
			SystemInstruction struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"systemInstruction"`
			GenerationConfig struct {
				ResponseMimeType string `json:"responseMimeType"`
			} `json:"generationConfig"`
			Tools []struct {
				FunctionDeclarations []struct {
					Name string `json:"name"`
				} `json:"functionDeclarations"`
			} `json:"tools"`
		}
		_ = json.Unmarshal(body, &req)
		if !strings.Contains(r.URL.Path, ":streamGenerateContent") {
			// countTokens and other side calls.
			_, _ = w.Write([]byte(`{"totalTokens":1}`))
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		respond := func(parts ...map[string]any) {
			chunk, _ := json.Marshal(map[string]any{
				"candidates":    []any{map[string]any{"content": map[string]any{"role": "model", "parts": parts}, "finishReason": "STOP"}},
				"usageMetadata": map[string]any{"promptTokenCount": 3, "candidatesTokenCount": 2, "totalTokenCount": 5},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
		}
		if req.GenerationConfig.ResponseMimeType == "application/json" {
			// Routing and classifier calls want any JSON.
			respond(map[string]any{"text": `{"reasoning":"simple","model_choice":"flash","complexity_score":1}`})
			return
		}
		var system []string
		for _, part := range req.SystemInstruction.Parts {
			system = append(system, part.Text)
		}
		mu.Lock()
		systemInstructions = append(systemInstructions, strings.Join(system, ""))
		mu.Unlock()
		answered := false
		for _, content := range req.Contents {
			for _, part := range content.Parts {
				if _, ok := part["functionResponse"]; ok {
					answered = true
				}
			}
		}
		if answered {
			respond(map[string]any{"text": "all done"})
			return
		}
		respond(map[string]any{"functionCall": map[string]any{"name": "mcp_aviary_echo", "args": map[string]any{"text": "hi"}}})
	}))
	defer model.Close()

	home := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".gemini"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(home, ".gemini", "settings.json"),
		[]byte(`{"security":{"auth":{"selectedType":"gemini-api-key"}}}`), 0o644))

	llm.SetCLIProviderRegistry(clidefs.MustBuiltin())
	t.Cleanup(func() { llm.SetCLIProviderRegistry(nil) })
	definition, ok := Backend("gemini").Definition()
	require.True(t, ok)
	approval := definition.ApprovalArgs(clidefs.ApprovalAuto)
	s, err := NewSession(Options{
		Backend:      "gemini",
		Model:        "gemini-2.5-flash",
		SystemPrompt: "You are the e2e stand-in agent.",
		WorkDir:      t.TempDir(),
		MCPURL:       mcpServer.URL,
		MCPToken:     token,
		BuiltinTools: true,
		Approval:     approval,
		NewSessionID: randomUUID(),
		Binary:       binary,
		Env: []string{
			"HOME=" + home,
			"GEMINI_API_KEY=fake",
			"GOOGLE_GEMINI_BASE_URL=" + model.URL,
		},
	})
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	var events []Event
	result, err := s.Send(ctx, Turn{Text: "call the echo tool"}, collect(&events))
	require.NoError(t, err)
	assert.Equal(t, "all done", result.Text)

	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, []string{"hi"}, calls, "gemini called Aviary's tool through the registered server with the token")
	require.NotEmpty(t, systemInstructions)
	assert.Equal(t, "You are the e2e stand-in agent.", strings.TrimSpace(systemInstructions[0]), "the system prompt replaces gemini's own")
	for _, event := range events {
		if event.Type == EventToolStart {
			assert.NotEqual(t, "mcp_aviary_echo", event.Tool.Name, "Aviary's MCP boundary reports its own tools")
		}
	}
}
