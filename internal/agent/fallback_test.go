package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"

	"github.com/stretchr/testify/assert"
)

type testProviderFactory struct {
	mu        sync.Mutex
	providers map[string]llm.Provider
	errors    map[string]error
	calls     []string
}

func (f *testProviderFactory) ForModel(model string) (llm.Provider, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, model)
	if err := f.errors[model]; err != nil {
		return nil, err
	}
	provider, ok := f.providers[model]
	if !ok {
		return nil, errors.New("provider unavailable")
	}
	return provider, nil
}

func (f *testProviderFactory) ForModelForceRefresh(model string) (llm.Provider, error) {
	return f.ForModel(model)
}

func (f *testProviderFactory) calledModels() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func TestAgentRunnerSkipsUnavailableFallbackProvider(t *testing.T) {
	setTestDataDir(t)
	primary := &mockProvider{err: errors.New("429 rate limit")}
	workingFallback := &mockProvider{events: []llm.Event{{Type: llm.EventTypeText, Text: "fallback answer"}}}
	factory := &testProviderFactory{
		providers: map[string]llm.Provider{"openai/working": workingFallback},
		errors:    map[string]error{"openai-codex/missing": errors.New("missing OAuth token")},
	}
	runner := NewAgentRunner(
		&domain.Agent{ID: "fallback-agent", Name: "bot", Model: "anthropic/primary"},
		&config.AgentConfig{Name: "bot"},
		primary,
		nil,
	)
	runner.factory = factory

	done := make(chan struct{}, 1)
	var output string
	runner.PromptWithOverrides(context.Background(), "hello", RunOverrides{
		Bare:      true,
		Fallbacks: []string{"openai-codex/missing", "openai/working"},
	}, func(event StreamEvent) {
		if event.Type == StreamEventText {
			output += event.Text
		}
		if event.Type == StreamEventDone {
			done <- struct{}{}
		}
	})

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		assert.FailNow(t, "timeout waiting for fallback response")
	}
	assert.Equal(t, "fallback answer", output)
	assert.Equal(t, []string{
		"anthropic/primary",
		"openai-codex/missing",
		"openai/working",
	}, factory.calledModels())
}
