package llm

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestBedrockMessages_Basic(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "Hello"},
		{Role: RoleAssistant, Content: "Hi there"},
		{Role: RoleUser, Content: "How are you?"},
	}
	out, err := bedrockMessages(msgs)
	assert.NoError(t, err)
	assert.Len(t, out, 3)
	assert.Equal(t, "user", string(out[0].Role))
	assert.Equal(t, "assistant", string(out[1].Role))
	assert.Equal(t, "user", string(out[2].Role))
}

func TestBedrockMessages_ConsecutiveMerge(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "First"},
		{Role: RoleUser, Content: "Second"},
		{Role: RoleAssistant, Content: "Response"},
	}
	out, err := bedrockMessages(msgs)
	assert.NoError(t, err)
	assert.Len(t, out, 2, "consecutive user messages should be merged")
	assert.Equal(t, "user", string(out[0].Role))
	assert.Len(t, out[0].Content, 2, "merged user message should have 2 content blocks")
}

func TestBedrockMessages_SystemAsUser(t *testing.T) {
	msgs := []Message{
		{Role: RoleSystem, Content: "You are helpful"},
		{Role: RoleUser, Content: "Hello"},
	}
	out, err := bedrockMessages(msgs)
	assert.NoError(t, err)
	assert.Len(t, out, 1, "system + user should merge into one user message")
	assert.Equal(t, "user", string(out[0].Role))
}

func TestBedrockMessages_ToolCall(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Content: "Search for dogs"},
		{Role: RoleAssistant, ToolCall: &ToolCall{
			ID:        "call-1",
			Name:      "search",
			Arguments: map[string]any{"query": "dogs"},
		}},
		{Role: RoleUser, Result: &ToolResult{
			ToolCallID: "call-1",
			Name:       "search",
			Content:    `{"results": ["dog1"]}`,
		}},
	}
	out, err := bedrockMessages(msgs)
	assert.NoError(t, err)
	assert.Len(t, out, 3)
}

func TestBedrockMessages_ToolResultError(t *testing.T) {
	msgs := []Message{
		{Role: RoleUser, Result: &ToolResult{
			ToolCallID: "call-1",
			Name:       "search",
			Content:    "something went wrong",
			IsError:    true,
		}},
	}
	out, err := bedrockMessages(msgs)
	assert.NoError(t, err)
	assert.Len(t, out, 1)
}

func TestBedrockToolSchema(t *testing.T) {
	schema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string"},
		},
	}
	result := bedrockToolSchema(schema)
	assert.NotNil(t, result, "should return a non-nil document")
}

func TestBedrockToolSchema_Nil(t *testing.T) {
	result := bedrockToolSchema(nil)
	assert.NotNil(t, result, "nil schema should still produce a fallback document")
}

func TestBedrockToolStatus(t *testing.T) {
	assert.Equal(t, "error", string(bedrockToolStatus(true)))
	assert.Equal(t, "success", string(bedrockToolStatus(false)))
}

func TestFactoryForModel_Bedrock(t *testing.T) {
	f := NewFactory(func(_ string) (string, error) { return "test-key", nil })
	p, err := f.ForModel("bedrock/us.anthropic.claude-sonnet-4-6")
	assert.NoError(t, err)
	_, ok := p.(*BedrockProvider)
	assert.True(t, ok, "expected BedrockProvider for bedrock/* model")
}

func TestFactoryForModel_BedrockWithRegion(t *testing.T) {
	f := NewFactory(func(_ string) (string, error) { return "", nil }).
		WithProviderOptionsResolver(func(provider string) (ProviderOptions, bool) {
			if provider != "bedrock" {
				return ProviderOptions{}, false
			}
			return ProviderOptions{Region: "eu-west-1"}, true
		})
	p, err := f.ForModel("bedrock/us.anthropic.claude-sonnet-4-6")
	assert.NoError(t, err)
	bp, ok := p.(*BedrockProvider)
	assert.True(t, ok)
	assert.Equal(t, "eu-west-1", bp.region)
}

func TestFactoryForModel_BedrockDefaultRegion(t *testing.T) {
	f := NewFactory(func(_ string) (string, error) { return "", nil })
	p, err := f.ForModel("bedrock/us.anthropic.claude-sonnet-4-6")
	assert.NoError(t, err)
	bp, ok := p.(*BedrockProvider)
	assert.True(t, ok)
	assert.Equal(t, defaultBedrockRegion, bp.region)
}

func TestNeedsInferenceProfileARN(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"us.anthropic.claude-sonnet-4-6", true},
		{"eu.meta.llama3-1-8b-instruct-v1:0", true},
		{"ap.anthropic.claude-haiku-4-5-20251001-v1:0", true},
		{"global.anthropic.claude-opus-4-6-v1", true},
		{"anthropic.claude-v2", false},
		{"amazon.nova-pro-v1:0", false},
		{"mistral.mistral-large-2407-v1:0", false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			assert.Equal(t, tt.want, needsInferenceProfileARN(tt.model))
		})
	}
}

func TestResolveModelID(t *testing.T) {
	p := &BedrockProvider{
		model:     "us.anthropic.claude-sonnet-4-6",
		region:    "us-west-2",
		accountID: "302010998300",
		resolved:  true,
	}
	modelID, err := p.resolveModelID(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "arn:aws:bedrock:us-west-2:302010998300:inference-profile/us.anthropic.claude-sonnet-4-6", modelID)
}

func TestResolveModelID_FoundationModel(t *testing.T) {
	p := &BedrockProvider{
		model:  "anthropic.claude-v2",
		region: "us-east-1",
	}
	modelID, err := p.resolveModelID(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "anthropic.claude-v2", modelID)
}
