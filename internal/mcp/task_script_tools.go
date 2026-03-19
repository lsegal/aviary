package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
)

type sessionSendArgs struct {
	Agent     string `json:"agent,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	Content   string `json:"content"`
}

type taskCompileArgs struct {
	Agent        string `json:"agent"`
	Prompt       string `json:"prompt"`
	RunDiscovery bool   `json:"run_discovery,omitempty"`
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

var tryCompileTaskPromptFunc func(ctx context.Context, agentName, prompt string, runDiscovery bool) (*compiledTaskResult, error)

func resolveTryCompileTaskPrompt() func(ctx context.Context, agentName, prompt string, runDiscovery bool) (*compiledTaskResult, error) {
	if tryCompileTaskPromptFunc != nil {
		return tryCompileTaskPromptFunc
	}
	return tryCompileTaskPrompt
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

func registerTaskCompilerTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "task_compile_script",
		Description: "Try to compile a natural-language task prompt into an embedded Lua script task. Returns the resulting task definition, analysis steps, and any generated script.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args taskCompileArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if strings.TrimSpace(args.Agent) == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}
		if strings.TrimSpace(args.Prompt) == "" {
			return nil, struct{}{}, fmt.Errorf("prompt is required")
		}
		plan, err := resolveTryCompileTaskPrompt()(ctx, args.Agent, args.Prompt, args.RunDiscovery)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(plan)
	})
}

func tryCompileTaskPrompt(ctx context.Context, agentName, prompt string, runDiscovery bool) (*compiledTaskResult, error) {
	cfg, agentCfg, err := loadCompileAgentConfig(agentName)
	if err != nil {
		return nil, err
	}
	model := config.EffectiveAgentModel(*agentCfg, cfg.Models)
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("agent %q has no model configured", agentName)
	}
	toolList, err := compileToolCatalog(ctx, agentName)
	if err != nil {
		return nil, err
	}
	provider, err := compileTaskProvider(model)
	if err != nil {
		return nil, err
	}

	analysisSystem := "You are Aviary's task determinism analyzer. Break the task into concrete execution steps. For each step, decide whether it is deterministic from the prompt plus the available tool catalog. A step is deterministic only when it can be executed with bounded, stable tool calls without fuzzy judgment, hidden selectors, or open-ended reasoning. If the task needs discovery that is not explicitly allowed, set needs_discovery=true. Only allow script compilation when the task contains more than one deterministic step and the overall workflow can be fully expressed in Aviary's Lua runtime with available tools. Reply with JSON only using this schema: {\"should_compile\":bool,\"needs_discovery\":bool,\"reason\":string,\"deterministic_step_count\":number,\"steps\":[{\"kind\":\"deterministic|agent_run|notify|unknown\",\"deterministic\":bool,\"tool\":\"optional tool name\",\"description\":\"short description\"}]}. Do not invent tools."
	analysisUser := fmt.Sprintf("Task prompt:\n%s\n\nAvailable tools:\n%s\n\nrun_discovery=%t", prompt, toolList, runDiscovery)
	analysisText, err := completeLLMText(ctx, provider, model, analysisSystem, analysisUser)
	if err != nil {
		return nil, err
	}
	var analysis compileTaskAnalysis
	if err := json.Unmarshal([]byte(extractJSONObject(analysisText)), &analysis); err != nil {
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
	if analysis.DeterministicStepCount <= 1 || !analysis.ShouldCompile || analysis.NeedsDiscovery {
		if result.Reason == "" {
			result.Reason = "task could not be validated as deterministic enough for script compilation"
		}
		return result, nil
	}

	generationSystem := "Generate a single-file Lua script for Aviary's embedded runtime. Output only the Lua source code, with no markdown fences. The runtime exposes a sandboxed global table `tool` where each tool is called as tool.name({ ... }) and a global `environment` table containing agent_id, session_id, task_id, and job_id. There is no filesystem, network, package loader, or shell access except through the exposed tools. Use tool calls for all external actions. When the script should emit a user-visible notification, call print(...). Do not invent tools that are not in the available tools list."
	generationUser := fmt.Sprintf("Task prompt:\n%s\n\nAvailable tools:\n%s\n\nCompiler analysis JSON:\n%s", prompt, toolList, mustJSON(analysis))
	script, err := completeLLMText(ctx, provider, model, generationSystem, generationUser)
	if err != nil {
		return nil, err
	}
	candidateScript := strings.TrimSpace(extractCodeFence(script))
	if candidateScript == "" {
		if result.Reason == "" {
			result.Reason = "compiler did not produce a script"
		}
		return result, nil
	}

	validationSystem := "You are Aviary's task compiler validator. Decide whether the generated Lua script faithfully performs the original task prompt using the available tools and no hidden assumptions. Reply with JSON only using this schema: {\"valid\":bool,\"reason\":\"short explanation\"}. Return valid=false if the script skips required behavior, changes the task meaning, relies on unavailable tools, or is otherwise unsafe to auto-promote."
	validationUser := fmt.Sprintf("Task prompt:\n%s\n\nAvailable tools:\n%s\n\nCompiler analysis JSON:\n%s\n\nGenerated Lua script:\n%s", prompt, toolList, mustJSON(analysis), candidateScript)
	validationText, err := completeLLMText(ctx, provider, model, validationSystem, validationUser)
	if err != nil {
		return nil, err
	}
	var validation compileTaskValidation
	if err := json.Unmarshal([]byte(extractJSONObject(validationText)), &validation); err != nil {
		return nil, fmt.Errorf("parsing compiler validation: %w", err)
	}
	if !validation.Valid {
		if strings.TrimSpace(validation.Reason) != "" {
			result.Reason = strings.TrimSpace(validation.Reason)
		} else if result.Reason == "" {
			result.Reason = "generated script did not validate against the original prompt"
		}
		return result, nil
	}

	result.Type = "script"
	result.Script = candidateScript
	result.Validated = true
	if strings.TrimSpace(validation.Reason) != "" {
		result.Reason = strings.TrimSpace(validation.Reason)
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

func compileToolCatalog(ctx context.Context, agentName string) (string, error) {
	toolCtx := agent.WithSessionAgentID(agent.WithSessionID(ctx, "compile-preview"), agentName)
	client, err := NewAgentToolClient(toolCtx)
	if err != nil {
		return "", err
	}
	defer client.Close() //nolint:errcheck
	tools, err := client.ListTools(toolCtx)
	if err != nil {
		return "", err
	}
	lines := make([]string, 0, len(tools))
	for _, tool := range tools {
		lines = append(lines, fmt.Sprintf("- %s: %s", tool.Name, strings.TrimSpace(tool.Description)))
	}
	return strings.Join(lines, "\n"), nil
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

func completeLLMText(ctx context.Context, provider llm.Provider, model, system, user string) (string, error) {
	ch, err := provider.Stream(ctx, llm.Request{
		Model:    model,
		System:   system,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: user}},
		Stream:   true,
	})
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for ev := range ch {
		switch ev.Type {
		case llm.EventTypeText:
			b.WriteString(ev.Text)
		case llm.EventTypeError:
			return "", ev.Error
		}
	}
	return strings.TrimSpace(b.String()), nil
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
