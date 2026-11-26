package agent

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildLLMToolDefinitions_ExamplesIncludeAllRequiredFields(t *testing.T) {
	tools := []ToolInfo{{
		Name:        "session_set_target",
		Description: "Set a session target",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"agent":      map[string]any{"type": "string"},
				"session_id": map[string]any{"type": "string"},
				"channel_type": map[string]any{
					"type": "string",
					"enum": []any{"slack", "discord"},
				},
				"channel_id": map[string]any{"type": "string"},
				"target":     map[string]any{"type": "string"},
			},
			"required": []any{"agent", "session_id", "channel_type", "channel_id", "target"},
		},
	}}

	defs := BuildLLMToolDefinitions(tools)
	require.Len(t, defs, 1)
	require.Len(t, defs[0].Examples, 1)

	example := defs[0].Examples[0]
	assert.Equal(t, "assistant", example["agent"])
	assert.Equal(t, "current", example["session_id"])
	assert.Equal(t, "slack", example["channel_type"])
	assert.Equal(t, "alerts", example["channel_id"])
	assert.Equal(t, "C12345678", example["target"])
	assert.Len(t, example, 5)
}

func TestBuildLLMToolDefinitions_Empty(t *testing.T) {
	assert.Nil(t, BuildLLMToolDefinitions(nil))
	assert.Nil(t, buildLLMToolDefinitions([]ToolInfo{}))
}

func TestBuildLLMToolDefinitions_CopiesToolMetadata(t *testing.T) {
	schema := map[string]any{"type": "object"}
	defs := buildLLMToolDefinitions([]ToolInfo{{Name: "noop", Description: "Does nothing", InputSchema: schema}})
	require.Len(t, defs, 1)
	assert.Equal(t, "noop", defs[0].Name)
	assert.Equal(t, "Does nothing", defs[0].Description)
	assert.Equal(t, schema, defs[0].InputSchema)
	assert.Nil(t, defs[0].Examples)
}

type toolDefsTestSchema struct {
	Type       string                    `json:"type"`
	Properties map[string]map[string]any `json:"properties"`
	Required   []string                  `json:"required"`
}

func TestSchemaMap(t *testing.T) {
	direct := map[string]any{"type": "object"}
	tests := []struct {
		name   string
		schema any
		want   map[string]any
	}{
		{name: "nil", schema: nil, want: nil},
		{name: "map", schema: direct, want: direct},
		{
			name: "struct",
			schema: toolDefsTestSchema{
				Type:       "object",
				Properties: map[string]map[string]any{"limit": {"type": "integer"}},
				Required:   []string{"limit"},
			},
			want: map[string]any{
				"type":       "object",
				"properties": map[string]any{"limit": map[string]any{"type": "integer"}},
				"required":   []any{"limit"},
			},
		},
		{name: "raw JSON object", schema: json.RawMessage(`{"type":"object"}`), want: map[string]any{"type": "object"}},
		{name: "unmarshalable", schema: make(chan int), want: nil},
		{name: "non-object JSON", schema: []string{"a"}, want: nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, schemaMap(tt.schema))
		})
	}
}

func TestSynthesizeToolExamples(t *testing.T) {
	tests := []struct {
		name   string
		schema any
		want   []map[string]any
	}{
		{name: "nil schema", schema: nil, want: nil},
		{name: "no properties", schema: map[string]any{"type": "object"}, want: nil},
		{
			name: "required as []any skips blanks, non-strings, and unknown names",
			schema: map[string]any{
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer"},
					"query": map[string]any{"type": "string"},
				},
				"required": []any{"limit", "", 42, "missing"},
			},
			want: []map[string]any{{"limit": 10}},
		},
		{
			name: "required as []string",
			schema: map[string]any{
				"properties": map[string]any{
					"url":   map[string]any{"type": "string"},
					"order": map[string]any{"type": "string"},
				},
				"required": []string{"", "url", "order"},
			},
			want: []map[string]any{{"url": "https://example.com", "order": "desc"}},
		},
		{
			name: "struct schema required list",
			schema: toolDefsTestSchema{
				Type:       "object",
				Properties: map[string]map[string]any{"path": {"type": "string"}},
				Required:   []string{"path"},
			},
			want: []map[string]any{{"path": "notes/example.md"}},
		},
		{
			name: "no required fields uses first two sorted properties",
			schema: map[string]any{
				"properties": map[string]any{
					"zeta":    map[string]any{"type": "boolean"},
					"beta":    map[string]any{"type": "array"},
					"alpha":   map[string]any{"type": "number"},
					"verbose": "not a map",
				},
			},
			want: []map[string]any{{"alpha": 1, "beta": []any{}}},
		},
		{
			name: "required names all missing falls back to properties",
			schema: map[string]any{
				"properties": map[string]any{"agent": map[string]any{"type": "string"}},
				"required":   []any{"ghost"},
			},
			want: []map[string]any{{"agent": "assistant"}},
		},
		{
			name: "non-map property values still get a fallback example",
			schema: map[string]any{
				"properties": map[string]any{"name": true},
				"required":   []any{"name"},
			},
			want: []map[string]any{{"name": "name"}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, synthesizeToolExamples(ToolInfo{Name: "demo", InputSchema: tt.schema}))
		})
	}
}

func TestExampleValue(t *testing.T) {
	tests := []struct {
		field string
		prop  map[string]any
		want  any
	}{
		{field: "query", want: "example web_search query"},
		{field: "Q", want: "example web_search query"},
		{field: "search", want: "example web_search query"},
		{field: "url", want: "https://example.com"},
		{field: "uri", want: "https://example.com"},
		{field: "path", want: "notes/example.md"},
		{field: "file", want: "notes/example.md"},
		{field: "directory", want: "notes/example.md"},
		{field: "dir", want: "notes/example.md"},
		{field: "session_id", want: "current"},
		{field: "channel_id", want: "alerts"},
		{field: "channel_type", want: "slack"},
		{field: "target", want: "C12345678"},
		{field: "order", want: "desc"},
		{field: "limit", prop: map[string]any{"type": "integer"}, want: 10},
		{field: "agent", want: "assistant"},
		{field: "content", want: "Example input for web_search"},
		{field: "text", want: "Example input for web_search"},
		{field: "message", want: "Example input for web_search"},
		{field: "prompt", want: "Example input for web_search"},
		{field: "query", prop: map[string]any{"type": "string", "enum": []any{"first", "second"}}, want: "first"},
		{field: "mode", prop: map[string]any{"type": "string", "enum": []any{}}, want: "mode"},
		{field: "enabled", prop: map[string]any{"type": "boolean"}, want: true},
		{field: "count", prop: map[string]any{"type": "integer"}, want: 1},
		{field: "ratio", prop: map[string]any{"type": "number"}, want: 1},
		{field: "tags", prop: map[string]any{"type": "array"}, want: []any{}},
		{field: "options", prop: map[string]any{"type": "object"}, want: map[string]any{}},
		{field: "Title", prop: map[string]any{"type": "string"}, want: "Title"},
		{field: "untyped", prop: nil, want: "untyped"},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			assert.Equal(t, tt.want, exampleValue("web_search", tt.field, tt.prop))
		})
	}
}
