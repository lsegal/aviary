package mcp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// progressHandlers routes progress notifications to the call that asked for
// them, keyed by the progress token the call sent.
var (
	progressHandlers sync.Map
	progressTokens   atomic.Int64
)

// cliClientOptions are the options every CLI-side client connects with, so a
// call made through any of them can receive progress.
func cliClientOptions() *sdkmcp.ClientOptions {
	return &sdkmcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *sdkmcp.ProgressNotificationClientRequest) {
			if req == nil || req.Params == nil {
				return
			}
			if handler, ok := progressHandlers.Load(fmt.Sprint(req.Params.ProgressToken)); ok {
				handler.(func(string))(req.Params.Message)
			}
		},
	}
}

// callToolWithProgress calls a tool with a progress token, handing every
// progress message the server sends for it to onProgress.
func callToolWithProgress(ctx context.Context, session *sdkmcp.ClientSession, name string, args any, onProgress func(string)) (*sdkmcp.CallToolResult, error) {
	token := fmt.Sprintf("aviary-cli-%d", progressTokens.Add(1))
	progressHandlers.Store(token, onProgress)
	defer progressHandlers.Delete(token)
	params := &sdkmcp.CallToolParams{Name: name, Arguments: args}
	params.SetProgressToken(token)
	return session.CallTool(ctx, params)
}
