package agent

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lsegal/aviary/internal/llm"
)

// BuildLLMToolDefinitions converts tool metadata into provider-native tool definitions.
func BuildLLMToolDefinitions(tools []ToolInfo) []llm.ToolDefinition {
	if len(tools) == 0 {
		return nil
	}
	defs := make([]llm.ToolDefinition, 0, len(tools))
	for _, tool := range tools {
		defs = append(defs, llm.ToolDefinition{
			Name:        tool.Name,
			Description: tool.Description,
			InputSchema: tool.InputSchema,
			Examples:    synthesizeToolExamples(tool),
		})
	}
	return defs
}

func buildLLMToolDefinitions(tools []ToolInfo) []llm.ToolDefinition {
	return BuildLLMToolDefinitions(tools)
}

func synthesizeToolExamples(tool ToolInfo) []map[string]any {
	schema := schemaMap(tool.InputSchema)
	props, _ := schema["properties"].(map[string]any)
	if len(props) == 0 {
		return nil
	}
	requiredSet := map[string]struct{}{}
	switch req := schema["required"].(type) {
	case []any:
		for _, item := range req {
			if s, ok := item.(string); ok && s != "" {
				requiredSet[s] = struct{}{}
			}
		}
	case []string:
		for _, item := range req {
			if item != "" {
				requiredSet[item] = struct{}{}
			}
		}
	}

	example := map[string]any{}
	for name, raw := range props {
		prop, _ := raw.(map[string]any)
		if len(requiredSet) > 0 {
			if _, ok := requiredSet[name]; !ok {
				continue
			}
		}
		example[name] = exampleValue(tool.Name, name, prop)
		if len(example) >= 3 {
			break
		}
	}
	if len(example) == 0 {
		for name, raw := range props {
			prop, _ := raw.(map[string]any)
			example[name] = exampleValue(tool.Name, name, prop)
			if len(example) >= 2 {
				break
			}
		}
	}
	if len(example) == 0 {
		return nil
	}
	return []map[string]any{example}
}

func schemaMap(schema any) map[string]any {
	if schema == nil {
		return nil
	}
	if m, ok := schema.(map[string]any); ok {
		return m
	}
	data, err := json.Marshal(schema)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return m
}

func exampleValue(toolName, fieldName string, prop map[string]any) any {
	field := strings.ToLower(fieldName)
	typeName, _ := prop["type"].(string)
	if enumValues, ok := prop["enum"].([]any); ok && len(enumValues) > 0 {
		return enumValues[0]
	}
	switch field {
	case "query", "q", "search":
		return fmt.Sprintf("example %s query", toolName)
	case "url", "uri":
		return "https://example.com"
	case "path", "file", "directory", "dir":
		return "notes/example.md"
	case "session_id":
		return "current"
	case "order":
		return "desc"
	case "limit":
		return 10
	case "agent":
		return "assistant"
	case "content", "text", "message", "prompt":
		return fmt.Sprintf("Example input for %s", toolName)
	}
	switch typeName {
	case "boolean":
		return true
	case "integer", "number":
		return 1
	case "array":
		return []any{}
	case "object":
		return map[string]any{}
	default:
		return fieldName
	}
}
