package agent

import "context"

// ToolInfo is a provider-agnostic capability descriptor exposed to the model.
type ToolInfo struct {
	Name        string
	Description string
}

// ToolClient executes tool calls and enumerates available tools.
type ToolClient interface {
	ListTools(ctx context.Context) ([]ToolInfo, error)
	CallToolText(ctx context.Context, name string, args map[string]any) (string, error)
	Close() error
}

var newToolClientFactory = func(context.Context) (ToolClient, error) {
	return nil, nil
}

// SetToolClientFactory injects the runtime tool client factory used by AgentRunner.
func SetToolClientFactory(fn func(context.Context) (ToolClient, error)) {
	if fn == nil {
		newToolClientFactory = func(context.Context) (ToolClient, error) { return nil, nil }
		return
	}
	newToolClientFactory = fn
}
