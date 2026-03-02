package llm

import (
	"errors"
	"testing"
)

func TestConstants(t *testing.T) {
	for _, role := range []Role{RoleUser, RoleAssistant, RoleSystem} {
		if role == "" {
			t.Fatal("role constant should not be empty")
		}
	}
	for _, typ := range []EventType{EventTypeText, EventTypeError, EventTypeDone} {
		if typ == "" {
			t.Fatal("event type constant should not be empty")
		}
	}
}

func TestRequestAndMessageZeroValues(t *testing.T) {
	r := Request{}
	if r.Model != "" || r.MaxToks != 0 || r.Stream {
		t.Fatalf("unexpected zero request: %+v", r)
	}
	m := Message{Role: RoleUser, Content: "hello"}
	if m.Role != RoleUser || m.Content != "hello" {
		t.Fatalf("unexpected message: %+v", m)
	}
}

func TestFactoryForModel(t *testing.T) {
	f := NewFactory(func(_ string) (string, error) { return "test-key", nil })

	cases := []struct {
		model   string
		wantErr bool
	}{
		{model: "anthropic/claude-sonnet-4.5", wantErr: false},
		{model: "openai/gpt-4o", wantErr: false},
		{model: "gemini/gemini-2.0-flash", wantErr: false},
		{model: "stdio/claude", wantErr: false},
		{model: "invalid", wantErr: true},
		{model: "unknown/model", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.model, func(t *testing.T) {
			p, err := f.ForModel(tc.model)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for model %s", tc.model)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %s: %v", tc.model, err)
			}
			if p == nil {
				t.Fatalf("provider should not be nil for %s", tc.model)
			}
		})
	}
}

func TestFactoryResolverError(t *testing.T) {
	f := NewFactory(func(ref string) (string, error) {
		if ref == "auth:openai:default" {
			return "", errors.New("boom")
		}
		return "key", nil
	})

	if _, err := f.ForModel("openai/gpt-4o"); err == nil {
		t.Fatal("expected auth resolver error")
	}

	if _, err := f.ForModel("stdio/codex"); err != nil {
		t.Fatalf("stdio should not require auth resolver, got %v", err)
	}
}

func TestFactoryNilResolver(t *testing.T) {
	f := NewFactory(nil)
	if _, err := f.ForModel("anthropic/claude-3-5-sonnet"); err != nil {
		t.Fatalf("expected nil resolver to still construct provider, got %v", err)
	}
}

func TestIntegration_AllProviderKinds(t *testing.T) {
	f := NewFactory(func(_ string) (string, error) { return "integration-key", nil })
	models := []string{
		"anthropic/claude-sonnet-4.5",
		"openai/gpt-4o-mini",
		"gemini/gemini-pro",
		"stdio/claude",
	}
	for _, model := range models {
		t.Run(model, func(t *testing.T) {
			p, err := f.ForModel(model)
			if err != nil {
				t.Fatalf("for model %s: %v", model, err)
			}
			if p == nil {
				t.Fatalf("provider is nil for %s", model)
			}
		})
	}
}
