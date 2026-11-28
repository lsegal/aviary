package mcp

import (
	"context"
	"reflect"
	"slices"
	"strings"

	"github.com/google/jsonschema-go/jsonschema"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/startup"
)

func addTool[Args any](
	s *sdkmcp.Server,
	tool *sdkmcp.Tool,
	handler func(context.Context, *sdkmcp.CallToolRequest, Args) (*sdkmcp.CallToolResult, struct{}, error),
) {
	if tool != nil && tool.InputSchema == nil {
		tool.InputSchema = inferredInputSchema[Args]()
	}
	if tool == nil || !restartUngatedTools[tool.Name] {
		handler = restartGated(handler)
	}
	if tool != nil {
		if subsystem := toolSubsystem(tool.Name); subsystem != "" {
			handler = startupGated(subsystem, handler)
		}
	}
	sdkmcp.AddTool(s, tool, handler)
}

// toolSubsystem names the background-initialized subsystem a tool needs, or
// "" when it needs none. Channel tools wait on channel connections, which
// hold the channel manager while they connect. Code Loop tools are not
// listed: they serve the Code Loop's saved state while discovery runs.
func toolSubsystem(name string) string {
	if strings.HasPrefix(name, "channel_") {
		return "channels"
	}
	return ""
}

// startupGated answers a call with an "initializing, retry in Ns" error
// while subsystem is still initializing after a start, instead of letting
// the call hang until it finishes.
func startupGated[Args any](
	subsystem string,
	handler func(context.Context, *sdkmcp.CallToolRequest, Args) (*sdkmcp.CallToolResult, struct{}, error),
) func(context.Context, *sdkmcp.CallToolRequest, Args) (*sdkmcp.CallToolResult, struct{}, error) {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest, args Args) (*sdkmcp.CallToolResult, struct{}, error) {
		if err := startup.CheckReady(subsystem); err != nil {
			return nil, struct{}{}, err
		}
		return handler(ctx, req, args)
	}
}

// AddTool registers a tool on s the way Aviary's own tools are registered:
// with an input schema inferred from Args when tool has none, and holding the
// restart gate for the length of each call.
func AddTool[Args any](
	s *sdkmcp.Server,
	tool *sdkmcp.Tool,
	handler func(context.Context, *sdkmcp.CallToolRequest, Args) (*sdkmcp.CallToolResult, struct{}, error),
) {
	addTool(s, tool, handler)
}

// restartUngatedTools run agent turns rather than a single operation: they can
// last as long as a job, which a restart checkpoints instead of waiting for,
// and the tool calls their turns make are gated on their own.
var restartUngatedTools = map[string]bool{
	"agent_run":        true,
	"agent_run_script": true,
}

// restartGated holds the restart gate for the length of each call, so an
// automatic restart waits for the call to finish and a call does not start
// while a restart is under way. The hold is released however the call ends,
// including on error, panic and cancellation.
func restartGated[Args any](
	handler func(context.Context, *sdkmcp.CallToolRequest, Args) (*sdkmcp.CallToolResult, struct{}, error),
) func(context.Context, *sdkmcp.CallToolRequest, Args) (*sdkmcp.CallToolResult, struct{}, error) {
	return func(ctx context.Context, req *sdkmcp.CallToolRequest, args Args) (*sdkmcp.CallToolResult, struct{}, error) {
		release, err := saferestart.MCPCalls.Acquire(ctx)
		if err != nil {
			return nil, struct{}{}, err
		}
		defer release()
		return handler(ctx, req, args)
	}
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
			applySchemaDirective(root, jsonName, key, value)
		}
	}
}

// applySchemaDirective applies one `schema:"key=value"` directive of the
// field named jsonName to root.
func applySchemaDirective(root *jsonschema.Schema, jsonName, key, value string) {
	switch key {
	case "enum":
		if jsonName == "" || root.Properties == nil || root.Properties[jsonName] == nil {
			return
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
			return
		}
		anyOf := make([]*jsonschema.Schema, 0, len(names))
		for _, name := range names {
			anyOf = append(anyOf, &jsonschema.Schema{Required: []string{name}})
		}
		root.AllOf = append(root.AllOf, &jsonschema.Schema{AnyOf: anyOf})
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
	name, _, _ := strings.Cut(tag, ",")
	if name = strings.TrimSpace(name); name != "" {
		return name
	}
	return field.Name
}

func derefType(t reflect.Type) reflect.Type {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t
}
