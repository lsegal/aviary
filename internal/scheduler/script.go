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
	var logs jobLogBuilder
	scriptJob := *job
	scriptJob.Script = script
	output, err := executeScriptJobOutput(ctx, &scriptJob, logs.Addf)
	if output != "" {
		if job.ReplyAgentID != "" && job.ReplySessionID != "" {
			if replyErr := agent.AppendReplyToSession(job.ReplyAgentID, job.ReplySessionID, output); replyErr != nil {
				return scriptExecutionResult{Output: output, Logs: logs.String()}, replyErr
			}
		}
		if deliverErr := recordTaskOutput(job, output, deliver); deliverErr != nil {
			return scriptExecutionResult{Output: output, Logs: logs.String()}, deliverErr
		}
	}
	if err != nil {
		return scriptExecutionResult{Output: output, Logs: logs.String()}, fmt.Errorf("running lua script task: %w", err)
	}
	return scriptExecutionResult{Output: output, Logs: logs.String()}, nil
}

// executeScriptJobOutput runs job.Script as job's agent, with the tools of the
// agent ctx names, and returns what it printed.
func executeScriptJobOutput(ctx context.Context, job *domain.Job, logf func(format string, args ...any)) (string, error) {
	baseToolClient, err := agent.NewToolClient(ctx)
	if err != nil {
		return "", err
	}
	toolClient := &scriptToolClient{base: baseToolClient, ctx: ctx}
	defer toolClient.Close() //nolint:errcheck
	return scriptruntime.RunLua(ctx, job.Script, scriptruntime.Options{
		ToolClient: toolClient,
		Environment: scriptruntime.Environment{
			AgentID:   job.AgentID,
			SessionID: job.SessionID,
			TaskID:    job.TaskID,
			JobID:     job.ID,
		},
		Logf: logf,
	})
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

// recordTaskOutput shows a run's output in the task's own session and then
// forwards it to the task's delivery target, if any. Prompt runs write their
// reply to the task session as they stream, so only runs that produce output
// outside a conversation (scripts and project tasks) use this.
func recordTaskOutput(job *domain.Job, output string, deliver func(agentName, route, text string) error) error {
	if job.SessionID != "" {
		if err := agent.AppendMessageToSession(job.AgentID, job.SessionID, domain.MessageRoleAssistant, output); err != nil {
			return fmt.Errorf("recording task output in session: %w", err)
		}
	}
	if !agent.ShouldDeliverReply(output) || job.OutputChannel == "" || deliver == nil {
		return nil
	}
	return deliver(job.AgentID, job.OutputChannel, output)
}
