package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
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

type compiledTaskPlan struct {
	Compilable     bool               `json:"compilable"`
	NeedsDiscovery bool               `json:"needs_discovery,omitempty"`
	Reason         string             `json:"reason,omitempty"`
	Steps          []compiledTaskStep `json:"steps,omitempty"`
	Script         string             `json:"script,omitempty"`
}

type compiledTaskStep struct {
	Kind          string `json:"kind"`
	Deterministic bool   `json:"deterministic"`
	Tool          string `json:"tool,omitempty"`
	Description   string `json:"description"`
}

var taskPromptCompiler func(ctx context.Context, agentName, prompt string, runDiscovery bool) (*compiledTaskPlan, error)

func resolveTaskPromptCompiler() func(ctx context.Context, agentName, prompt string, runDiscovery bool) (*compiledTaskPlan, error) {
	if taskPromptCompiler != nil {
		return taskPromptCompiler
	}
	return compileTaskPrompt
}

func registerSessionSendTool(s *sdkmcp.Server) {
	sdkmcp.AddTool(s, &sdkmcp.Tool{
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
	sdkmcp.AddTool(s, &sdkmcp.Tool{
		Name:        "task_compile_script",
		Description: "Analyze a natural-language task prompt and, when feasible, generate a Python script task. Returns JSON with steps, confidence, and generated script.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args taskCompileArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if strings.TrimSpace(args.Agent) == "" {
			return nil, struct{}{}, fmt.Errorf("agent is required")
		}
		if strings.TrimSpace(args.Prompt) == "" {
			return nil, struct{}{}, fmt.Errorf("prompt is required")
		}
		plan, err := resolveTaskPromptCompiler()(ctx, args.Agent, args.Prompt, args.RunDiscovery)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(plan)
	})
}

func compileTaskPrompt(ctx context.Context, agentName, prompt string, runDiscovery bool) (*compiledTaskPlan, error) {
	if builtIn := compileBuiltInTaskPrompt(prompt); builtIn != nil {
		return builtIn, nil
	}
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
	analysisSystem := "You are a task compiler for Aviary. Break the task into steps. Mark each step deterministic when it can be executed by a fixed tool call with stable arguments. Mark semantic classification or fuzzy reasoning steps as nondeterministic. If the task has at least one deterministic tool step and the rest can be handled by bounded agent_run --bare calls, it is compilable. Reply with JSON only matching this schema: {\"compilable\":bool,\"needs_discovery\":bool,\"reason\":string,\"steps\":[{\"kind\":\"deterministic|agent_run|notify|unknown\",\"deterministic\":bool,\"tool\":\"optional tool name\",\"description\":\"short description\"}]}. If a browser selector cannot be grounded from the prompt alone, set needs_discovery=true instead of guessing a selector."
	analysisUser := fmt.Sprintf("Task prompt:\n%s\n\nAvailable tools:\n%s\n\nrun_discovery=%t", prompt, toolList, runDiscovery)
	analysisText, err := completeLLMText(ctx, provider, model, analysisSystem, analysisUser)
	if err != nil {
		return nil, err
	}
	var plan compiledTaskPlan
	if err := json.Unmarshal([]byte(extractJSONObject(analysisText)), &plan); err != nil {
		return nil, fmt.Errorf("parsing compiler analysis: %w", err)
	}
	if !plan.Compilable || plan.NeedsDiscovery {
		if strings.TrimSpace(plan.Reason) == "" {
			plan.Reason = "task is not safely compilable into a script from the prompt alone"
		}
		return &plan, nil
	}
	generationSystem := "Generate a single-file Python 3 script for Aviary. Output only the script text, starting with #!/usr/bin/env python3. Use only the Python standard library. Use os.environ.get('AVIARY_BIN', 'aviary') to invoke Aviary tools when needed. For deterministic steps, call subprocess against [AVIARY_BIN, 'tool', <tool>, ...]. For nondeterministic semantic checks, call agent_run with --bare semantics through the MCP tool interface. If the script needs to notify, print the notification text to stdout and let Aviary's task scheduler deliver that output to the task's configured target. Do not invent tools that are not in the available tools list."
	generationUser := fmt.Sprintf("Task prompt:\n%s\n\nAvailable tools:\n%s\n\nCompiler plan JSON:\n%s", prompt, toolList, mustJSON(plan))
	script, err := completeLLMText(ctx, provider, model, generationSystem, generationUser)
	if err != nil {
		return nil, err
	}
	plan.Script = strings.TrimSpace(extractCodeFence(script))
	return &plan, nil
}

var (
	taskPromptURLPattern     = regexp.MustCompile(`https?://[^\s"'<>]+`)
	taskPromptQuotedMsg      = regexp.MustCompile(`(?is)(?:exact message|message)\s*:\s*"([^"]+)"`)
	taskPromptNon200Patterns = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bnot returning (?:a )?200\b`),
		regexp.MustCompile(`(?i)\bstatus(?: code|)\s+is\s+not\s+200\b`),
		regexp.MustCompile(`(?i)\bresponse status(?: code|)\s+is\s+not\s+200\b`),
		regexp.MustCompile(`(?i)\bif .*?\bstatus\b.*?\b!=\s*200\b`),
		regexp.MustCompile(`(?i)\bif .*?\bstatus\b.*?\bnot\s+200\b`),
	}
)

func compileBuiltInTaskPrompt(prompt string) *compiledTaskPlan {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return nil
	}
	url := taskPromptURLPattern.FindString(trimmed)
	if url == "" {
		return nil
	}
	if !mentionsNon200Condition(trimmed) {
		return nil
	}
	message := defaultStatusAlertMessage(url)
	if match := taskPromptQuotedMsg.FindStringSubmatch(trimmed); len(match) == 2 {
		message = strings.TrimSpace(match[1])
	}
	return &compiledTaskPlan{
		Compilable: true,
		Reason:     "deterministic HTTP health-check task",
		Steps: []compiledTaskStep{
			{Kind: "deterministic", Deterministic: true, Description: fmt.Sprintf("Perform an HTTP GET to %s", url)},
			{Kind: "deterministic", Deterministic: true, Description: "Compare the HTTP status code to 200"},
			{Kind: "notify", Deterministic: true, Description: "Emit a notification only when the status code is not 200"},
		},
		Script: buildHTTPStatusScript(url, message),
	}
}

func mentionsNon200Condition(prompt string) bool {
	for _, pattern := range taskPromptNon200Patterns {
		if pattern.MatchString(prompt) {
			return true
		}
	}
	return false
}

func defaultStatusAlertMessage(url string) string {
	host := strings.TrimPrefix(url, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")
	if host == "" {
		host = url
	}
	return fmt.Sprintf("%s is not returning 200 — status: [STATUS]", host)
}

func buildHTTPStatusScript(url, message string) string {
	quotedURL, _ := json.Marshal(url)
	quotedMessage, _ := json.Marshal(message)
	return strings.TrimSpace(fmt.Sprintf(`#!/usr/bin/env python3
import urllib.error
import urllib.request

URL = %s
MESSAGE_TEMPLATE = %s


def fetch_status(url: str) -> int:
    request = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(request, timeout=20) as response:
            return int(response.getcode() or 0)
    except urllib.error.HTTPError as exc:
        return int(exc.code or 0)


status = fetch_status(URL)
if status != 200:
    print(MESSAGE_TEMPLATE.replace("[STATUS]", str(status)))
`, quotedURL, quotedMessage))
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
