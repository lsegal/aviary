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

func executeScriptJob(ctx context.Context, job *domain.Job, cfg *config.AgentConfig, deliver func(agentName, route, text string) error) (string, error) {
	_ = cfg
	script := strings.TrimSpace(job.Script)
	if script == "" {
		return "", fmt.Errorf("script task %q has no script content", job.TaskID)
	}
	baseToolClient, err := agent.NewToolClient(ctx)
	if err != nil {
		return "", err
	}
	toolClient := &scriptToolClient{base: baseToolClient, ctx: ctx}
	defer toolClient.Close() //nolint:errcheck

	output, err := scriptruntime.RunLua(ctx, script, scriptruntime.Options{
		ToolClient: toolClient,
		Environment: scriptruntime.Environment{
			AgentID:   job.AgentID,
			SessionID: job.SessionID,
			TaskID:    job.TaskID,
			JobID:     job.ID,
		},
	})
	if output != "" {
		if job.ReplyAgentID != "" && job.ReplySessionID != "" {
			if replyErr := agent.AppendReplyToSession(job.ReplyAgentID, job.ReplySessionID, output); replyErr != nil {
				return output, replyErr
			}
		}
		if agent.ShouldDeliverReply(output) && job.OutputChannel != "" && deliver != nil {
			if deliverErr := deliver(job.AgentID, job.OutputChannel, output); deliverErr != nil {
				return output, deliverErr
			}
		}
	}
	if err != nil {
		return output, fmt.Errorf("running lua script task: %w", err)
	}
	return output, nil
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
