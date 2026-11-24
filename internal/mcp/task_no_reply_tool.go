package mcp

import (
	"context"
	"errors"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
)

// TaskNoReplyTool ends a task or workflow run without delivering its output.
const TaskNoReplyTool = "task_no_reply"

type taskNoReplyArgs struct {
	Reason string `json:"reason"`
}

func registerTaskNoReplyTool(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name: TaskNoReplyTool,
		Description: "Only in a scheduled task or workflow run: end the run without replying, so nothing is sent to its output target. " +
			"Call it instead of writing a message that explains you are not replying; that message would itself be sent as the reply. " +
			"reason (required) is a short note on why, recorded in the run history, such as \"message was negative\".",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args taskNoReplyArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		jobID, ok := agent.JobIDFromContext(ctx)
		if !ok {
			return nil, struct{}{}, errors.New("task_no_reply only works in a scheduled task or workflow run")
		}
		agent.SkipReply(jobID, args.Reason)
		return text("No reply will be sent for this run. End the run now without writing a reply.")
	})
}
