package mcp

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func addTool[Args any](
	s *sdkmcp.Server,
	tool *sdkmcp.Tool,
	handler func(context.Context, *sdkmcp.CallToolRequest, Args) (*sdkmcp.CallToolResult, struct{}, error),
) {
	if tool != nil && tool.InputSchema == nil {
		tool.InputSchema = inferredInputSchema[Args]()
	}
	sdkmcp.AddTool(s, tool, handler)
}

func inferredInputSchema[T any]() any {
	t := reflect.TypeFor[T]()
	s, err := jsonschema.ForType(t, nil)
	if err != nil {
		return nil
	}
	applySchemaTags(derefType(t), s)
	return s
}

func applySchemaTags(t reflect.Type, root *jsonschema.Schema) {
	if t.Kind() != reflect.Struct || root == nil {
		return
	}
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		tag := strings.TrimSpace(field.Tag.Get("schema"))
		if tag == "" {
			continue
		}
		directives := parseSchemaTag(tag)
		if len(directives) == 0 {
			continue
		}

		jsonName := jsonFieldName(field)
		for key, value := range directives {
			switch key {
			case "enum":
				if jsonName == "" || root.Properties == nil || root.Properties[jsonName] == nil {
					continue
				}
				root.Properties[jsonName].Enum = splitSchemaNames(value)
			case "atmostone":
				names := splitSchemaKeys(value)
				for i := 0; i < len(names); i++ {
					for j := i + 1; j < len(names); j++ {
						root.AllOf = append(root.AllOf, &jsonschema.Schema{
							Not: &jsonschema.Schema{Required: []string{names[i], names[j]}},
						})
					}
				}
			case "atleastone":
				names := splitSchemaKeys(value)
				if len(names) == 0 {
					continue
				}
				anyOf := make([]*jsonschema.Schema, 0, len(names))
				for _, name := range names {
					anyOf = append(anyOf, &jsonschema.Schema{Required: []string{name}})
				}
				root.AllOf = append(root.AllOf, &jsonschema.Schema{AnyOf: anyOf})
			}
		}
	}
}

func parseSchemaTag(tag string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(tag, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.ToLower(strings.TrimSpace(key))] = strings.TrimSpace(value)
	}
	return out
}

func splitSchemaKeys(raw string) []string {
	parts := strings.Split(raw, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" && !slices.Contains(out, part) {
			out = append(out, part)
		}
	}
	return out
}

func splitSchemaNames(raw string) []any {
	keys := splitSchemaKeys(raw)
	out := make([]any, 0, len(keys))
	for _, key := range keys {
		out = append(out, key)
	}
	return out
}

func jsonFieldName(field reflect.StructField) string {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return ""
	}
	if name, _, ok := strings.Cut(tag, ","); ok {
		name = strings.TrimSpace(name)
		if name != "" {
			return name
		}
	}
	return field.Name
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
