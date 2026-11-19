package mcp

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

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

// progressDrainTimeout bounds how long a call waits after its result for the
// progress message onProgress reports as the last one. The SDK hands
// notifications over on a goroutine of its own, in order but not in step with
// the response, so the last ones can land after the result.
const progressDrainTimeout = 2 * time.Second

// callToolWithProgress calls a tool with a progress token, handing every
// progress message the server sends for it to onProgress, which reports
// whether the message is the call's last. The call returns once that message
// has been handled, or progressDrainTimeout after the result.
func callToolWithProgress(ctx context.Context, session *sdkmcp.ClientSession, name string, args any, onProgress func(string) bool) (*sdkmcp.CallToolResult, error) {
	token := fmt.Sprintf("aviary-cli-%d", progressTokens.Add(1))
	last := make(chan struct{})
	var once sync.Once
	progressHandlers.Store(token, func(message string) {
		if onProgress(message) {
			once.Do(func() { close(last) })
		}
	})
	defer progressHandlers.Delete(token)
	params := &sdkmcp.CallToolParams{Name: name, Arguments: args}
	params.SetProgressToken(token)
	result, err := session.CallTool(ctx, params)
	if err == nil {
		select {
		case <-last:
		case <-time.After(progressDrainTimeout):
		case <-ctx.Done():
		}
	}
	return result, err
}
