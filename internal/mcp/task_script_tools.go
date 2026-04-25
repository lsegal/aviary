package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/scriptruntime"
	"github.com/lsegal/aviary/internal/store"
)

type sessionSendArgs struct {
	Agent     string `json:"agent,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Content   string `json:"content"`
}

type compiledTaskResult struct {
	Type           string             `json:"type"`
	Prompt         string             `json:"prompt,omitempty"`
	Script         string             `json:"script,omitempty"`
	NeedsDiscovery bool               `json:"needs_discovery,omitempty"`
	Reason         string             `json:"reason,omitempty"`
	Steps          []compiledTaskStep `json:"steps,omitempty"`
	Validated      bool               `json:"validated,omitempty"`
}

type compiledTaskStep struct {
	Kind          string `json:"kind"`
	Deterministic bool   `json:"deterministic"`
	Tool          string `json:"tool,omitempty"`
	Description   string `json:"description"`
}

type compileTaskAnalysis struct {
	ShouldCompile          bool               `json:"should_compile"`
	NeedsDiscovery         bool               `json:"needs_discovery,omitempty"`
	Reason                 string             `json:"reason,omitempty"`
	DeterministicStepCount int                `json:"deterministic_step_count,omitempty"`
	Steps                  []compiledTaskStep `json:"steps,omitempty"`
}

type compileTaskValidation struct {
	Valid  bool   `json:"valid"`
	Reason string `json:"reason,omitempty"`
}

var tryCompileTaskPromptFunc func(ctx context.Context, agentName, prompt, target string, runDiscovery bool) (*compiledTaskResult, error)

type taskCompileTrackerContextKey struct{}

func resolveTryCompileTaskPrompt() func(ctx context.Context, agentName, prompt, target string, runDiscovery bool) (*compiledTaskResult, error) {
	if tryCompileTaskPromptFunc != nil {
		return tryCompileTaskPromptFunc
	}
	return tryCompileTaskPrompt
}

func withTaskCompileTracker(ctx context.Context, tracker *taskCompileTracker) context.Context {
	if tracker == nil {
		return ctx
	}
	return context.WithValue(ctx, taskCompileTrackerContextKey{}, tracker)
}

func taskCompileTrackerFromContext(ctx context.Context) *taskCompileTracker {
	tracker, _ := ctx.Value(taskCompileTrackerContextKey{}).(*taskCompileTracker)
	return tracker
}

type taskCompileTracker struct {
	record *domain.TaskCompile
}

func newTaskCompileTracker(agentName, taskName, requestedTaskType, prompt, target, trigger string, runDiscovery bool) *taskCompileTracker {
	now := time.Now().UTC()
	return &taskCompileTracker{
		record: &domain.TaskCompile{
			ID:                fmt.Sprintf("task_compile_%s", now.Format("20060102_150405_000000000")),
			AgentID:           agentName,
			TaskName:          strings.TrimSpace(taskName),
			RequestedTaskType: strings.TrimSpace(requestedTaskType),
			Prompt:            strings.TrimSpace(prompt),
			Target:            strings.TrimSpace(target),
			Trigger:           strings.TrimSpace(trigger),
			RunDiscovery:      runDiscovery,
			Status:            domain.TaskCompileStatusSkipped,
			ResultTaskType:    "prompt",
			CreatedAt:         now,
			UpdatedAt:         now,
		},
	}
}

func (t *taskCompileTracker) addStage(name, systemPrompt, userPrompt string) int {
	stage := domain.TaskCompileStage{
		Name:         name,
		Status:       "started",
		SystemPrompt: systemPrompt,
		UserPrompt:   userPrompt,
		StartedAt:    time.Now().UTC(),
	}
	t.record.Stages = append(t.record.Stages, stage)
	t.touch()
	return len(t.record.Stages) - 1
}

func (t *taskCompileTracker) finishStage(index int, status, response string, err error) {
	if index < 0 || index >= len(t.record.Stages) {
		return
	}
	stage := t.record.Stages[index]
	stage.Status = strings.TrimSpace(status)
	stage.Response = strings.TrimSpace(response)
	if err != nil {
		stage.Error = err.Error()
	}
	finishedAt := time.Now().UTC()
	stage.FinishedAt = &finishedAt
	t.record.Stages[index] = stage
	t.touch()
}

func (t *taskCompileTracker) persist() error {
	t.touch()
	return store.WriteJSON(store.TaskCompilePath(t.record.AgentID, t.record.ID), t.record)
}

func (t *taskCompileTracker) touch() {
	t.record.UpdatedAt = time.Now().UTC()
}

func toDomainCompileSteps(steps []compiledTaskStep) []domain.TaskCompileStep {
	out := make([]domain.TaskCompileStep, 0, len(steps))
	for _, step := range steps {
		out = append(out, domain.TaskCompileStep{
			Kind:          step.Kind,
			Deterministic: step.Deterministic,
			Tool:          step.Tool,
			Description:   step.Description,
		})
	}
	return out
}

func registerSessionSendTool(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "session_send",
		Description: "Send a plain-text assistant message to a session and any connected channel deliveries. Arguments: session_id(optional in-session), content(required).",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args sessionSendArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		agentName := strings.TrimSpace(args.Agent)
		sessionID := strings.TrimSpace(args.SessionID)
		if sessionID == "" {
			sessionID, _ = agent.SessionIDFromContext(ctx)
		}
		if agentName == "" {
			if agentID, ok := agent.SessionAgentIDFromContext(ctx); ok {
				agentName = strings.TrimSpace(agentID)
			}
		}
		if agentName == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}
		if sessionID == "" {
			return nil, struct{}{}, fmt.Errorf("session_id is required")
		}
		if strings.TrimSpace(args.Content) == "" {
			return nil, struct{}{}, fmt.Errorf("content is required")
		}
		sess, err := loadSessionByID(agentName, sessionID)
		if err != nil {
			return nil, struct{}{}, err
		}
		if err := agent.AppendReplyToSession(sess.AgentID, sess.ID, args.Content); err != nil {
			return nil, struct{}{}, err
		}
		return text(fmt.Sprintf("sent message to session %q for agent %q", sess.ID, agentName))
	})
}

func tryCompileTaskPrompt(ctx context.Context, agentName, prompt, target string, runDiscovery bool) (*compiledTaskResult, error) {
	tracker := taskCompileTrackerFromContext(ctx)
	cfg, agentCfg, err := loadCompileAgentConfig(agentName)
	if err != nil {
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusFailed
			tracker.record.Reason = err.Error()
		}
		return nil, err
	}
	model := config.EffectiveAgentModel(*agentCfg, cfg.Models)
	if strings.TrimSpace(model) == "" {
		err := fmt.Errorf("agent %q has no model configured", agentName)
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusFailed
			tracker.record.Reason = err.Error()
		}
		return nil, err
	}
	tools, err := compileTools(ctx, agentName)
	if err != nil {
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusFailed
			tracker.record.Reason = err.Error()
		}
		return nil, err
	}
	provider, err := compileTaskProvider(model)
	if err != nil {
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusFailed
			tracker.record.Reason = err.Error()
		}
		return nil, err
	}

	analysisSystem := "You are Aviary's task determinism analyzer. Break the task into concrete execution steps. For each step, decide whether it is deterministic from the prompt plus the available tool catalog. A step is deterministic when it can be executed with fixed arguments and bounded tool calls, even if the observed external result changes from run to run. For example, checking a URL with a fixed tool call, reading a fixed inbox query, downloading a fixed file, or visiting a fixed page with a grounded selector are deterministic observation steps. Do not confuse changing external data with nondeterminism. A step is nondeterministic only when it requires fuzzy judgment, semantic interpretation, hidden selectors, open-ended search, or discovery of missing parameters/capabilities. A notify step is deterministic whenever it can be implemented as print(...) or a fixed message template derived only from deterministic step outputs. When a non-empty task output target is provided, any script output emitted via print(...) will be delivered to that target by Aviary automatically. Treat that routed notification delivery as deterministic and preferred; do not require session_send for that case. If the task needs discovery that is not explicitly allowed, set needs_discovery=true. Only allow script compilation when the task contains more than one deterministic step and the overall workflow can be fully expressed in Aviary's Lua runtime with available tools. Prefer using the named tools in the catalog as evidence of capability; do not claim capabilities are missing when a listed tool clearly covers the step. Reply with JSON only using this schema: {\"should_compile\":bool,\"needs_discovery\":bool,\"reason\":string,\"deterministic_step_count\":number,\"steps\":[{\"kind\":\"deterministic|agent_run|notify|unknown\",\"deterministic\":bool,\"tool\":\"optional tool name\",\"description\":\"short description\"}]}. Do not invent tools."
	analysisUser := buildCompileStageUserPrompt(prompt, target, tools, fmt.Sprintf("run_discovery=%t", runDiscovery))
	analysisText, err := completeLLMText(ctx, provider, model, "analysis", analysisSystem, analysisUser, nil)
	if err != nil {
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusFailed
			tracker.record.Reason = err.Error()
		}
		return nil, err
	}
	var analysis compileTaskAnalysis
	if err := json.Unmarshal([]byte(extractJSONObject(analysisText)), &analysis); err != nil {
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusFailed
			tracker.record.Reason = fmt.Sprintf("parsing compiler analysis: %v", err)
		}
		return nil, fmt.Errorf("parsing compiler analysis: %w", err)
	}
	if analysis.DeterministicStepCount == 0 {
		for _, step := range analysis.Steps {
			if step.Deterministic {
				analysis.DeterministicStepCount++
			}
		}
	}

	result := &compiledTaskResult{
		Type:           "prompt",
		Prompt:         strings.TrimSpace(prompt),
		NeedsDiscovery: analysis.NeedsDiscovery,
		Reason:         strings.TrimSpace(analysis.Reason),
		Steps:          analysis.Steps,
	}
	if tracker != nil {
		tracker.record.NeedsDiscovery = analysis.NeedsDiscovery
		tracker.record.DeterministicSteps = analysis.DeterministicStepCount
		tracker.record.Steps = toDomainCompileSteps(analysis.Steps)
		tracker.record.Reason = strings.TrimSpace(analysis.Reason)
	}
	slog.Info(
		"task_compile: analysis completed",
		"component", "task_compile",
		"agent", agentName,
		"should_compile", analysis.ShouldCompile,
		"needs_discovery", analysis.NeedsDiscovery,
		"deterministic_step_count", analysis.DeterministicStepCount,
		"reason", strings.TrimSpace(analysis.Reason),
	)
	if analysis.DeterministicStepCount <= 1 || !analysis.ShouldCompile || analysis.NeedsDiscovery {
		if result.Reason == "" {
			result.Reason = "task could not be validated as deterministic enough for script compilation"
		}
		slog.Info(
			"task_compile: compilation declined",
			"component", "task_compile",
			"agent", agentName,
			"should_compile", analysis.ShouldCompile,
			"needs_discovery", analysis.NeedsDiscovery,
			"deterministic_step_count", analysis.DeterministicStepCount,
			"reason", result.Reason,
		)
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusSkipped
			tracker.record.ResultTaskType = "prompt"
			tracker.record.Reason = result.Reason
		}
		return result, nil
	}
	slog.Info(
		"task_compile: generation starting",
		"component", "task_compile",
		"agent", agentName,
		"deterministic_step_count", analysis.DeterministicStepCount,
	)

	generationSystem := "Generate a single-file Lua script for Aviary's embedded runtime. Output only the Lua source code, with no markdown fences. The runtime exposes a sandboxed global table `tool` where each tool is called as tool.name({ ... }) and a global `environment` table containing agent_id, session_id, task_id, and job_id. There is no filesystem, network, package loader, or shell access except through the exposed tools. Use tool calls for all external actions. When the script should emit a user-visible notification, call print(...). If a non-empty task output target is provided, Aviary will automatically deliver any print(...) output to that target. In that case, treat print(...) as the delivery mechanism and do not call session_send just to notify the user. Only use session_send when the task explicitly needs to reply to a live session and no task output target is available. Do not invent tools that are not in the available tools list."
	generationUser := buildCompileStageUserPrompt(prompt, target, tools, "Compiler analysis JSON:\n"+mustJSON(analysis))
	script, err := completeLLMText(ctx, provider, model, "generation", generationSystem, generationUser, nil)
	if err != nil {
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusFailed
			tracker.record.Reason = err.Error()
		}
		return nil, err
	}
	candidateScript := strings.TrimSpace(extractCodeFence(script))
	if candidateScript == "" {
		if result.Reason == "" {
			result.Reason = "compiler did not produce a script"
		}
		slog.Warn("task_compile: generation produced empty script", "component", "task_compile", "agent", agentName, "reason", result.Reason)
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusSkipped
			tracker.record.ResultTaskType = "prompt"
			tracker.record.Reason = result.Reason
		}
		return result, nil
	}
	if err := scriptruntime.ValidateLua(candidateScript); err != nil {
		result.Reason = fmt.Sprintf("compiler produced invalid Lua: %v", err)
		slog.Warn("task_compile: lua validation failed", "component", "task_compile", "agent", agentName, "reason", result.Reason)
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusFailed
			tracker.record.ResultTaskType = "prompt"
			tracker.record.Reason = result.Reason
			tracker.record.Script = candidateScript
		}
		return result, nil
	}

	validationSystem := "You are Aviary's task compiler validator. Decide whether the generated Lua script faithfully performs the original task prompt using the available tools and no hidden assumptions. Reply with JSON only using this schema: {\"valid\":bool,\"reason\":\"short explanation\"}. Return valid=false if the script skips required behavior, changes the task meaning, relies on unavailable tools, or is otherwise unsafe to auto-promote. When a non-empty task output target is provided, Aviary will automatically deliver print(...) output to that target, so prefer scripts that notify via print(...) over scripts that call session_send for the same routed output."
	validationUser := buildCompileStageUserPrompt(prompt, target, tools, "Compiler analysis JSON:\n"+mustJSON(analysis), "Generated Lua script:\n"+candidateScript)
	validationText, err := completeLLMText(ctx, provider, model, "validation", validationSystem, validationUser, nil)
	if err != nil {
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusFailed
			tracker.record.Reason = err.Error()
			tracker.record.Script = candidateScript
		}
		return nil, err
	}
	var validation compileTaskValidation
	if err := json.Unmarshal([]byte(extractJSONObject(validationText)), &validation); err != nil {
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusFailed
			tracker.record.Reason = fmt.Sprintf("parsing compiler validation: %v", err)
			tracker.record.Script = candidateScript
		}
		return nil, fmt.Errorf("parsing compiler validation: %w", err)
	}
	if !validation.Valid {
		if strings.TrimSpace(validation.Reason) != "" {
			result.Reason = strings.TrimSpace(validation.Reason)
		} else if result.Reason == "" {
			result.Reason = "generated script did not validate against the original prompt"
		}
		slog.Warn("task_compile: validator rejected script", "component", "task_compile", "agent", agentName, "reason", result.Reason)
		if tracker != nil {
			tracker.record.Status = domain.TaskCompileStatusSkipped
			tracker.record.ResultTaskType = "prompt"
			tracker.record.Reason = result.Reason
			tracker.record.Script = candidateScript
		}
		return result, nil
	}

	result.Type = "script"
	result.Script = candidateScript
	result.Validated = true
	if strings.TrimSpace(validation.Reason) != "" {
		result.Reason = strings.TrimSpace(validation.Reason)
	}
	slog.Info("task_compile: compilation succeeded", "component", "task_compile", "agent", agentName, "validated", result.Validated, "reason", result.Reason)
	if tracker != nil {
		tracker.record.Status = domain.TaskCompileStatusSucceeded
		tracker.record.ResultTaskType = "script"
		tracker.record.Validated = result.Validated
		tracker.record.Reason = result.Reason
		tracker.record.Script = candidateScript
	}
	return result, nil
}

func loadCompileAgentConfig(agentName string) (*config.Config, *config.AgentConfig, error) {
	cfg, err := config.Load("")
	if err != nil {
		return nil, nil, err
	}
	for i := range cfg.Agents {
		if cfg.Agents[i].Name == agentName {
			return cfg, &cfg.Agents[i], nil
		}
	}
	return nil, nil, fmt.Errorf("agent %q not found in config", agentName)
}

func compileTools(ctx context.Context, agentName string) ([]agent.ToolInfo, error) {
	toolCtx := agent.WithSessionAgentID(agent.WithSessionID(ctx, "compile-preview"), agentName)
	client, err := NewAgentToolClient(toolCtx)
	if err != nil {
		return nil, err
	}
	defer client.Close() //nolint:errcheck
	tools, err := client.ListTools(toolCtx)
	if err != nil {
		return nil, err
	}
	return tools, nil
}

func compileTaskProvider(model string) (llm.Provider, error) {
	store, err := authStore()
	if err != nil {
		return nil, err
	}
	factory := llm.NewFactory(func(ref string) (string, error) {
		if store == nil {
			return "", nil
		}
		return store.Get(strings.TrimPrefix(ref, "auth:"))
	})
	return factory.ForModel(model)
}

func completeLLMText(
	ctx context.Context,
	provider llm.Provider,
	model, stageName, system, user string,
	tools []llm.ToolDefinition,
) (string, error) {
	tracker := taskCompileTrackerFromContext(ctx)
	stageIndex := -1
	if tracker != nil && strings.TrimSpace(stageName) != "" {
		stageIndex = tracker.addStage(stageName, system, user)
	}
	ch, err := provider.Stream(ctx, llm.Request{
		Model:        model,
		System:       system,
		Messages:     []llm.Message{{Role: llm.RoleUser, Content: user}},
		Stream:       true,
		CacheControl: llm.DefaultPromptCacheControl(),
		Tools:        tools,
	})
	if err != nil {
		if tracker != nil && stageIndex >= 0 {
			tracker.finishStage(stageIndex, "failed", "", err)
		}
		return "", err
	}
	var b strings.Builder
	for ev := range ch {
		switch ev.Type {
		case llm.EventTypeText:
			b.WriteString(ev.Text)
		case llm.EventTypeError:
			if tracker != nil && stageIndex >= 0 {
				tracker.finishStage(stageIndex, "failed", b.String(), ev.Error)
			}
			return "", ev.Error
		case llm.EventTypeToolCall:
			err := fmt.Errorf("unexpected native tool call during %s stage", firstNonEmpty(strings.TrimSpace(stageName), "compiler"))
			if tracker != nil && stageIndex >= 0 {
				tracker.finishStage(stageIndex, "failed", b.String(), err)
			}
			return "", err
		}
	}
	out := strings.TrimSpace(b.String())
	if tracker != nil && stageIndex >= 0 {
		tracker.finishStage(stageIndex, "succeeded", out, nil)
	}
	return out, nil
}

func buildCompileStageUserPrompt(prompt, target string, tools []agent.ToolInfo, sections ...string) string {
	parts := []string{
		"Task prompt:\n" + prompt,
		"Task output target:\n" + firstNonEmpty(strings.TrimSpace(target), "(silent)"),
		"Available tools:\n" + renderCompileToolCatalog(tools),
	}
	for _, section := range sections {
		section = strings.TrimSpace(section)
		if section != "" {
			parts = append(parts, section)
		}
	}
	return strings.Join(parts, "\n\n")
}

func renderCompileToolCatalog(tools []agent.ToolInfo) string {
	if len(tools) == 0 {
		return "(none)"
	}
	defs := agent.BuildLLMToolDefinitions(tools)
	lines := make([]string, 0, len(defs)*4)
	for _, tool := range defs {
		lines = append(lines, "- "+tool.Name)
		if desc := strings.TrimSpace(tool.Description); desc != "" {
			lines = append(lines, "  description: "+desc)
		}
		if schema := strings.TrimSpace(mustJSON(tool.InputSchema)); schema != "" && schema != "null" {
			lines = append(lines, "  input_schema: "+schema)
		}
		if len(tool.Examples) > 0 {
			lines = append(lines, "  examples: "+mustJSON(tool.Examples))
		}
	}
	return strings.Join(lines, "\n")
}

func extractJSONObject(text string) string {
	start := strings.Index(text, "{")
	end := strings.LastIndex(text, "}")
	if start >= 0 && end > start {
		return text[start : end+1]
	}
	return text
}

func extractCodeFence(text string) string {
	trimmed := strings.TrimSpace(text)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 3 {
		return trimmed
	}
	return strings.TrimSpace(strings.Join(lines[1:len(lines)-1], "\n"))
}

func mustJSON(v any) string {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(data)
}
