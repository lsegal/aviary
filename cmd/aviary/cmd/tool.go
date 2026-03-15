package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	internalmcp "github.com/lsegal/aviary/internal/mcp"
	"github.com/lsegal/aviary/internal/store"
)

var toolCmd = &cobra.Command{
	Use:                "tool <name> [--field value ...] [--args '{...}']",
	Short:              "Run any MCP tool by name",
	Long:               "Run any MCP tool by name using schema-driven flags. Use --args for unsupported or complex payloads.",
	DisableFlagParsing: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) == 0 || isHelpArg(args[0]) {
			return writeToolHelp(cmd.Context(), cmd.OutOrStdout())
		}
		if args[0] == "list" {
			return writeToolCatalog(cmd.Context(), cmd.OutOrStdout())
		}

		tool, err := lookupTool(cmd.Context(), args[0])
		if err != nil {
			return err
		}
		if len(args) > 1 && containsHelpArg(args[1:]) {
			return writeSingleToolHelp(cmd.OutOrStdout(), tool)
		}

		payload, err := parseToolInvocationArgs(tool, args[1:])
		if err != nil {
			return err
		}
		out, err := activeDispatcher().CallTool(cmd.Context(), tool.Name, payload)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), out)
		return err
	},
}

func init() {
	toolCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if len(args) > 0 && !isHelpArg(args[0]) {
			tool, err := lookupTool(cmd.Context(), args[0])
			if err == nil {
				_ = writeSingleToolHelp(cmd.OutOrStdout(), tool)
				return
			}
		}
		_ = writeToolHelp(cmd.Context(), cmd.OutOrStdout())
	})
	rootCmd.AddCommand(toolCmd)
}

func activeDispatcher() *internalmcp.Dispatcher {
	if dispatcher != nil {
		return dispatcher
	}
	if dataDir != "" {
		store.SetDataDir(dataDir)
	}
	return internalmcp.NewDispatcher(serverURL, token)
}

func parseToolArgs(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var payload any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, fmt.Errorf("parsing --args as JSON: %w", err)
	}
	if payload == nil {
		return map[string]any{}, nil
	}
	args, ok := payload.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("--args must be a JSON object")
	}
	return args, nil
}

func parseToolInvocationArgs(tool internalmcp.ToolInfo, rawArgs []string) (map[string]any, error) {
	fields := toolInputFields(tool)
	payload := map[string]any{}

	for i := 0; i < len(rawArgs); i++ {
		arg := rawArgs[i]
		if isHelpArg(arg) {
			continue
		}
		if !strings.HasPrefix(arg, "--") {
			return nil, fmt.Errorf("unexpected argument %q; use --<field> <value>", arg)
		}

		name, inlineValue := splitFlagToken(arg)
		if name == "args" {
			if inlineValue == nil {
				i++
				if i >= len(rawArgs) {
					return nil, fmt.Errorf("--args requires a JSON object value")
				}
				inlineValue = &rawArgs[i]
			}
			argsPayload, err := parseToolArgs(*inlineValue)
			if err != nil {
				return nil, err
			}
			for key, value := range argsPayload {
				payload[key] = value
			}
			continue
		}

		field, ok := fields[name]
		if !ok {
			return nil, fmt.Errorf("unknown flag --%s for tool %q", name, tool.Name)
		}

		value, consumedNext, err := parseToolFieldArg(field, inlineValue, rawArgs, i)
		if err != nil {
			return nil, err
		}
		if consumedNext {
			i++
		}
		payload[name] = value
	}

	for _, field := range sortedToolFields(tool) {
		if field.Required && payload[field.Name] == nil {
			return nil, fmt.Errorf("--%s is required", field.Name)
		}
	}

	return payload, nil
}

type toolField struct {
	Name        string
	Required    bool
	Schema      map[string]any
	Type        string
	ItemType    string
	Description string
}

func toolInputFields(tool internalmcp.ToolInfo) map[string]toolField {
	required := map[string]struct{}{}
	schema := toolInputSchema(tool)
	if rawRequired, ok := schema["required"].([]any); ok {
		for _, item := range rawRequired {
			if name, ok := item.(string); ok {
				required[name] = struct{}{}
			}
		}
	} else if rawRequired, ok := schema["required"].([]string); ok {
		for _, name := range rawRequired {
			required[name] = struct{}{}
		}
	}

	properties, _ := schema["properties"].(map[string]any)
	fields := make(map[string]toolField, len(properties))
	for name, raw := range properties {
		prop, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		_, isRequired := required[name]
		fields[name] = toolField{
			Name:        name,
			Required:    isRequired,
			Schema:      prop,
			Type:        schemaType(prop),
			ItemType:    arrayItemType(prop),
			Description: strings.TrimSpace(stringValue(prop["description"])),
		}
	}
	return fields
}

func sortedToolFields(tool internalmcp.ToolInfo) []toolField {
	fields := toolInputFields(tool)
	out := make([]toolField, 0, len(fields))
	for _, field := range fields {
		out = append(out, field)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func parseToolFieldArg(field toolField, inlineValue *string, rawArgs []string, index int) (any, bool, error) {
	if field.Type == "boolean" {
		if inlineValue == nil && (index+1 >= len(rawArgs) || strings.HasPrefix(rawArgs[index+1], "--")) {
			return true, false, nil
		}
	}

	valueText := ""
	consumedNext := false
	if inlineValue != nil {
		valueText = *inlineValue
	} else {
		if index+1 >= len(rawArgs) {
			return nil, false, fmt.Errorf("--%s requires a value", field.Name)
		}
		valueText = rawArgs[index+1]
		consumedNext = true
	}

	value, err := parseToolFieldValue(field, valueText)
	if err != nil {
		return nil, false, err
	}
	return value, consumedNext, nil
}

func parseToolFieldValue(field toolField, value string) (any, error) {
	switch field.Type {
	case "string", "":
		return value, nil
	case "integer":
		parsed, err := strconv.Atoi(value)
		if err != nil {
			return nil, fmt.Errorf("--%s must be an integer", field.Name)
		}
		return parsed, nil
	case "number":
		parsed, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return nil, fmt.Errorf("--%s must be a number", field.Name)
		}
		return parsed, nil
	case "boolean":
		parsed, err := strconv.ParseBool(value)
		if err != nil {
			return nil, fmt.Errorf("--%s must be true or false", field.Name)
		}
		return parsed, nil
	case "array":
		parts := splitCommaSeparated(value)
		items := make([]any, 0, len(parts))
		itemField := toolField{Name: field.Name, Type: field.ItemType}
		for _, part := range parts {
			item, err := parseToolFieldValue(itemField, part)
			if err != nil {
				return nil, err
			}
			items = append(items, item)
		}
		return items, nil
	case "object":
		return nil, fmt.Errorf("--%s is not supported as a flat flag; use --args", field.Name)
	default:
		return nil, fmt.Errorf("--%s uses unsupported type %q; use --args", field.Name, field.Type)
	}
}

func splitCommaSeparated(value string) []string {
	if strings.TrimSpace(value) == "" {
		return []string{}
	}
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitFlagToken(token string) (string, *string) {
	name := strings.TrimPrefix(token, "--")
	if key, value, ok := strings.Cut(name, "="); ok {
		return key, &value
	}
	return name, nil
}

func toolInputSchema(tool internalmcp.ToolInfo) map[string]any {
	schema, ok := tool.InputSchema.(map[string]any)
	if !ok || schema == nil {
		return map[string]any{}
	}
	return schema
}

func schemaType(schema map[string]any) string {
	raw := schema["type"]
	switch value := raw.(type) {
	case string:
		if value != "null" {
			return value
		}
	case []any:
		for _, item := range value {
			if text, ok := item.(string); ok && text != "null" {
				return text
			}
		}
	case []string:
		for _, item := range value {
			if item != "null" {
				return item
			}
		}
	}
	if _, ok := schema["items"]; ok {
		return "array"
	}
	return ""
}

func arrayItemType(schema map[string]any) string {
	items, ok := schema["items"].(map[string]any)
	if !ok {
		return "string"
	}
	itemType := schemaType(items)
	if itemType == "" {
		return "string"
	}
	return itemType
}

func lookupTool(ctx context.Context, name string) (internalmcp.ToolInfo, error) {
	tools, err := activeDispatcher().ListTools(ctx)
	if err != nil {
		return internalmcp.ToolInfo{}, err
	}
	for _, tool := range tools {
		if tool.Name == name {
			return tool, nil
		}
	}
	return internalmcp.ToolInfo{}, fmt.Errorf("tool %q is not available", name)
}

func writeToolHelp(ctx context.Context, out io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	_, err := fmt.Fprintln(out, "Run any MCP tool by name using schema-driven flags. Use --args for unsupported or complex payloads.")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "Usage:")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "  aviary tool <name> --field value [--field value ...]")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "  aviary tool <name> --args '{\"key\":\"value\"}'")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "  aviary tool list")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "Notes:")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "  Arrays use comma-separated values, for example --command gmail,list.")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "  Boolean flags can be passed as --flag or --flag=false.")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, "Available Tools:")
	if err != nil {
		return err
	}
	if err := writeToolCatalog(ctx, out); err != nil {
		_, writeErr := fmt.Fprintf(out, "  unavailable: %v\n", err)
		if writeErr != nil {
			return writeErr
		}
	}
	return nil
}

func writeSingleToolHelp(out io.Writer, tool internalmcp.ToolInfo) error {
	_, err := fmt.Fprintf(out, "%s\n", tool.Name)
	if err != nil {
		return err
	}
	desc := strings.TrimSpace(tool.Description)
	if desc != "" {
		_, err = fmt.Fprintf(out, "  %s\n\n", desc)
		if err != nil {
			return err
		}
	} else {
		_, err = fmt.Fprintln(out)
		if err != nil {
			return err
		}
	}

	_, err = fmt.Fprintf(out, "Usage:\n  aviary tool %s", tool.Name)
	if err != nil {
		return err
	}
	for _, field := range sortedToolFields(tool) {
		placeholder := "<value>"
		switch field.Type {
		case "boolean":
			placeholder = "[=true|false]"
		case "array":
			placeholder = "<a,b,c>"
		}
		if field.Required {
			_, err = fmt.Fprintf(out, " --%s %s", field.Name, placeholder)
		} else {
			_, err = fmt.Fprintf(out, " [--%s %s]", field.Name, placeholder)
		}
		if err != nil {
			return err
		}
	}
	_, err = fmt.Fprintln(out, " [--args '{...}']")
	if err != nil {
		return err
	}

	fields := sortedToolFields(tool)
	if len(fields) == 0 {
		_, err = fmt.Fprintln(out, "\nThis tool does not declare any input flags.")
		return err
	}

	_, err = fmt.Fprintln(out, "\nFlags:")
	if err != nil {
		return err
	}
	for _, field := range fields {
		kind := field.Type
		if kind == "array" {
			kind = field.ItemType + " list"
		}
		label := fmt.Sprintf("  --%-22s %s", field.Name, kind)
		if field.Required {
			label += " required"
		}
		if field.Description != "" {
			label += "  " + field.Description
		}
		if _, err := fmt.Fprintln(out, label); err != nil {
			return err
		}
	}
	return nil
}

func writeToolCatalog(ctx context.Context, out io.Writer) error {
	if ctx == nil {
		ctx = context.Background()
	}
	tools, err := activeDispatcher().ListTools(ctx)
	if err != nil {
		return err
	}
	sort.Slice(tools, func(i, j int) bool {
		return tools[i].Name < tools[j].Name
	})
	for _, tool := range tools {
		desc := strings.TrimSpace(tool.Description)
		if desc == "" {
			desc = "No description available."
		}
		if _, err := fmt.Fprintf(out, "  %-28s %s\n", tool.Name, desc); err != nil {
			return err
		}
	}
	return nil
}

func containsHelpArg(args []string) bool {
	for _, arg := range args {
		if isHelpArg(arg) {
			return true
		}
	}
	return false
}

func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
