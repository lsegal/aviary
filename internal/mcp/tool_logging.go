package mcp

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
)

const (
	maxLoggedStringLen = 1024
)

func logToolCall(source, name string, args any) {
	if strings.TrimSpace(name) == "" {
		return
	}
	slog.Info(
		"mcp: tool call",
		"component", "mcp",
		"source", source,
		"tool", name,
		"arguments", redactedJSON(args),
	)
}

func redactedJSON(v any) string {
	redacted := redactValue("", v)
	b, err := json.Marshal(redacted)
	if err != nil {
		return fmt.Sprintf("%v", redacted)
	}
	return string(b)
}

func redactValue(key string, v any) any {
	if isSensitiveKey(key) {
		return "[REDACTED]"
	}

	switch vv := v.(type) {
	case nil:
		return nil
	case string:
		return truncateForLog(vv)
	case bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return vv
	case map[string]any:
		out := make(map[string]any, len(vv))
		for k, child := range vv {
			out[k] = redactValue(k, child)
		}
		return out
	case map[string]string:
		out := make(map[string]any, len(vv))
		for k, child := range vv {
			out[k] = redactValue(k, child)
		}
		return out
	case []any:
		out := make([]any, 0, len(vv))
		for _, child := range vv {
			out = append(out, redactValue(key, child))
		}
		return out
	case []string:
		out := make([]any, 0, len(vv))
		for _, child := range vv {
			out = append(out, redactValue(key, child))
		}
		return out
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		var generic any
		if err := json.Unmarshal(b, &generic); err != nil {
			return truncateForLog(string(b))
		}
		return redactValue(key, generic)
	}
}

func truncateForLog(s string) string {
	if len(s) <= maxLoggedStringLen {
		return s
	}
	return s[:maxLoggedStringLen] + fmt.Sprintf(" …+%d chars", len(s)-maxLoggedStringLen)
}

func isSensitiveKey(key string) bool {
	k := strings.ToLower(strings.TrimSpace(key))
	if k == "" {
		return false
	}

	if strings.Contains(k, "token") || strings.Contains(k, "secret") || strings.Contains(k, "password") {
		return true
	}

	switch k {
	case "authorization", "api_key", "apikey", "key", "client_key", "client_secret", "access_key", "private_key", "value", "code":
		return true
	default:
		return false
	}
}

func extractToolCallFromPayload(payload []byte) (name string, args any, ok bool) {
	var req struct {
		Method string `json:"method"`
		Params struct {
			Name      string `json:"name"`
			Arguments any    `json:"arguments"`
		} `json:"params"`
	}
	if err := json.Unmarshal(payload, &req); err != nil {
		return "", nil, false
	}
	if req.Method != "tools/call" || strings.TrimSpace(req.Params.Name) == "" {
		return "", nil, false
	}
	return req.Params.Name, req.Params.Arguments, true
}
