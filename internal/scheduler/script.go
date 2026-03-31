package scheduler

import (
	"context"
	"fmt"
	"strings"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/scriptruntime"
)

type scriptExecutionResult struct {
	Output string
	Logs   string
}

func executeScriptJob(ctx context.Context, job *domain.Job, cfg *config.AgentConfig, deliver func(agentName, route, text string) error) (scriptExecutionResult, error) {
	_ = cfg
	// Prefer explicit script body on the job; fall back to Prompt for
	// backwards compatibility.
	script := strings.TrimSpace(job.Script)
	if script == "" {
		script = strings.TrimSpace(job.Prompt)
	}
	if script == "" {
		return scriptExecutionResult{}, fmt.Errorf("script task %q has no script content", job.TaskID)
	}
	baseToolClient, err := agent.NewToolClient(ctx)
	if err != nil {
		return scriptExecutionResult{}, err
	}
	toolClient := &scriptToolClient{base: baseToolClient, ctx: ctx}
	defer toolClient.Close() //nolint:errcheck

	var logs jobLogBuilder
	output, err := scriptruntime.RunLua(ctx, script, scriptruntime.Options{
		ToolClient: toolClient,
		Environment: scriptruntime.Environment{
			AgentID:   job.AgentID,
			SessionID: job.SessionID,
			TaskID:    job.TaskID,
			JobID:     job.ID,
		},
		Logf: logs.Addf,
	})
	if output != "" {
		if job.ReplyAgentID != "" && job.ReplySessionID != "" {
			if replyErr := agent.AppendReplyToSession(job.ReplyAgentID, job.ReplySessionID, output); replyErr != nil {
				return scriptExecutionResult{Output: output, Logs: logs.String()}, replyErr
			}
		}
		if agent.ShouldDeliverReply(output) && job.OutputChannel != "" && deliver != nil {
			if deliverErr := deliver(job.AgentID, job.OutputChannel, output); deliverErr != nil {
				return scriptExecutionResult{Output: output, Logs: logs.String()}, deliverErr
			}
		}
	}
	if err != nil {
		return scriptExecutionResult{Output: output, Logs: logs.String()}, fmt.Errorf("running lua script task: %w", err)
	}
	return scriptExecutionResult{Output: output, Logs: logs.String()}, nil
}

type scriptToolClient struct {
	base agent.ToolClient
	ctx  context.Context
}

func (c *scriptToolClient) ListTools(ctx context.Context) ([]agent.ToolInfo, error) {
	if c.base == nil {
		return nil, nil
	}
	return c.base.ListTools(ctx)
}

func (c *scriptToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, error) {
	if c.base == nil {
		return "", fmt.Errorf("tool client not initialized")
	}
	return c.base.CallToolText(ctx, name, agent.NormalizeScriptToolArguments(c.ctx, name, args))
}

func (c *scriptToolClient) Close() error {
	if c.base == nil {
		return nil
	}
	return c.base.Close()
}
