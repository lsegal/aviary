package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/scriptruntime"
	"github.com/lsegal/aviary/internal/sessiontarget"
)

// workflowPreviewArgs selects the recent messages workflow_preview tests a
// text match against.
type workflowPreviewArgs struct {
	Agent   string `json:"agent"`
	Channel string `json:"channel"`
	Entry   string `json:"entry,omitempty"`
	Match   string `json:"match,omitempty"`
	Limit   int    `json:"limit,omitempty"`
}

// workflowPreviewMessage is one recent message and whether the match takes it.
type workflowPreviewMessage struct {
	Entry     string `json:"entry"`
	Sender    string `json:"sender,omitempty"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
	Matches   bool   `json:"matches"`
}

// workflowEntry is a conversation a channel has received, which a workflow
// can watch.
type workflowEntry struct {
	Entry string `json:"entry"`
	Title string `json:"title,omitempty"`
}

func registerWorkflowTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "workflow_preview",
		Description: "Test a workflow's text match against a channel's recent messages: lists the latest messages on channel (optionally only those on entry, a conversation ID) and whether match, a case-insensitive wildcard pattern (* and ?; empty matches everything), would trigger the workflow for each. Messages sent by the agent are left out because they never trigger workflows.",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args workflowPreviewArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		_, a, idx, err := loadChannel(args.Agent, args.Channel)
		if err != nil {
			return nil, struct{}{}, err
		}
		limit := args.Limit
		if limit <= 0 || limit > 100 {
			limit = 20
		}
		msgs, err := recentChannelMessages(a, idx, strings.TrimSpace(args.Entry), limit)
		if err != nil {
			return nil, struct{}{}, err
		}
		for i := range msgs {
			msgs[i].Matches = scheduler.WildcardMatch(args.Match, msgs[i].Text)
		}
		return jsonResult(msgs)
	})

	addTool(s, &sdkmcp.Tool{
		Name:        "workflow_entries",
		Description: "List the conversations (entries) a channel has received messages on, such as Slack channels or Signal contacts and groups, for picking a workflow's source entry",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, args channelArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		_, a, idx, err := loadChannel(args.Agent, args.Channel)
		if err != nil {
			return nil, struct{}{}, err
		}
		sessions, err := sessiontarget.ChannelSessions(a, idx)
		if err != nil {
			return nil, struct{}{}, err
		}
		seen := map[string]bool{}
		out := []workflowEntry{}
		for _, sess := range sessions {
			entry := sessionEntry(a.Channels[idx], sess.Name)
			if entry == "" || seen[entry] {
				continue
			}
			seen[entry] = true
			out = append(out, workflowEntry{Entry: entry, Title: sess.Title})
		}
		return jsonResult(out)
	})
}

// sessionEntry returns the conversation a channel session named name holds
// ("<type>:<entry>" or "<type>:<entry>:<thread>"), or "" for the channel's
// own session and sessions of other channels.
func sessionEntry(cc config.ChannelConfig, name string) string {
	if name == cc.SessionName() {
		return ""
	}
	rest, ok := strings.CutPrefix(name, cc.Type+":")
	if !ok {
		return ""
	}
	entry, _, _ := strings.Cut(rest, ":")
	return entry
}

// recentChannelMessages returns the latest limit messages others sent on the
// channel's conversations, or only on entry when it is set, oldest first.
func recentChannelMessages(a config.AgentConfig, idx int, entry string, limit int) ([]workflowPreviewMessage, error) {
	h, err := readChannelHistory(a, idx, entry)
	if err != nil {
		return nil, err
	}
	msgs := h.Messages
	if len(msgs) > limit {
		msgs = msgs[len(msgs)-limit:]
	}
	out := make([]workflowPreviewMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, workflowPreviewMessage{Entry: m.Entry, Sender: m.Sender, Text: m.Message.Content, Timestamp: m.Message.Timestamp.Format(time.RFC3339Nano)})
	}
	return out, nil
}

// createWorkflow saves a workflow from task_schedule's arguments: a task
// with the message trigger, which runs when a matching message arrives on
// the channel instead of on a schedule.
func createWorkflow(args taskScheduleArgs) (*sdkmcp.CallToolResult, struct{}, error) {
	if strings.TrimSpace(args.In) != "" || strings.TrimSpace(args.Schedule) != "" || args.Every != "" || args.At != "" || args.Days != "" || args.StartAt != "" || args.RunOnce || args.Missed != "" {
		return nil, struct{}{}, fmt.Errorf("workflows are triggered by messages and take no schedule, in, every, at, days, start_at, run_once or missed")
	}
	taskType := strings.ToLower(strings.TrimSpace(args.Type))
	if taskType == "" {
		taskType = "prompt"
	}
	if taskType != "prompt" && taskType != "script" {
		return nil, struct{}{}, fmt.Errorf("workflows run prompt or script content")
	}
	if args.Project != "" {
		return nil, struct{}{}, fmt.Errorf("project is only supported for project tasks")
	}
	content := strings.TrimSpace(args.Content)
	if content == "" {
		return nil, struct{}{}, fmt.Errorf("content is required")
	}
	if taskType == "script" {
		if err := scriptruntime.ValidateLua(content); err != nil {
			return nil, struct{}{}, fmt.Errorf("script tasks require valid Aviary Lua source: %w", err)
		}
	}
	cfg, a, idx, err := loadChannel(args.Agent, args.Channel)
	if err != nil {
		return nil, struct{}{}, err
	}
	target := normalizeTaskTarget(args.Target)
	if _, err := config.ParseTaskTarget(target); err != nil {
		return nil, struct{}{}, err
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		name = generatedTaskName(content)
	}
	updated := false
	for _, t := range a.Tasks {
		if t.Name == name {
			if !t.IsWorkflow() {
				return nil, struct{}{}, fmt.Errorf("agent %q already has a scheduled task named %q", a.Name, name)
			}
			updated = true
		}
	}
	task := config.TaskConfig{
		Type:    taskType,
		Name:    name,
		Prompt:  content,
		Trigger: config.TriggerMessage,
		Channel: a.Channels[idx].EffectiveName(),
		Entry:   strings.TrimSpace(args.Entry),
		Match:   strings.TrimSpace(args.Match),
		Target:  target,
		Overlap: strings.ToLower(strings.TrimSpace(args.Overlap)),
		Model:   strings.TrimSpace(args.Model),
	}
	if task.PrecompilesBesidePrompt() {
		// A prompt workflow keeps its prompt and, when precompiled, runs a
		// compiled script beside it, as a project task does. The choice
		// defaults to scheduler.precompute_tasks, as the task editor's does.
		precompile := config.EffectivePrecomputeTasks(cfg.Scheduler)
		if args.Precompile != nil {
			precompile = *args.Precompile
		}
		task.Precompile = &precompile
	}
	if _, err := config.SaveMarkdownTask(config.AgentTasksDir(a), task); err != nil {
		return nil, struct{}{}, err
	}
	if err := reloadTaskConfig(); err != nil {
		return nil, struct{}{}, err
	}
	action := "created"
	if updated {
		action = "updated"
	}
	note := ""
	if EffectiveTaskPrecompile(task) {
		// Compiling calls the model several times, so it runs in the
		// background; a run that comes first compiles before it starts.
		compileTaskInBackground(a.Name, task.Name, "schedule")
		note = " Precompiling: when its prompt is deterministic, runs execute the compiled script and fall back to the model if it fails."
	}
	return text(fmt.Sprintf("Workflow %q %s for agent %q: runs on %s.%s", name, action, a.Name, workflowSummary(task), note))
}

// workflowSummary describes what a workflow fires on.
func workflowSummary(t config.TaskConfig) string {
	what := "every message"
	if m := strings.TrimSpace(t.Match); m != "" && m != "*" {
		what = "messages matching " + m
	}
	where := "channel " + t.Channel
	if t.Entry != "" {
		where += " (" + t.Entry + ")"
	}
	return what + " on " + where
}
