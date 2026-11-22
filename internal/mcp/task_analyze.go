package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/models"
	"github.com/lsegal/aviary/internal/store"
)

// The verdicts task_analyze reports for a task being edited.
const (
	taskVerdictDeterministic    = "deterministic"
	taskVerdictPartial          = "partial"
	taskVerdictNonDeterministic = "nondeterministic"
	taskVerdictScript           = "script"
	taskVerdictProject          = "project"
)

// taskEditorUsageSessionID is the session task_analyze records its own token
// use under, so the analysis is attributed to the task editor, not a task.
const taskEditorUsageSessionID = "task-editor"

// Rough per-run token figures for a prompt task run through the model. The
// agent's system prompt, memory, and tool definitions are sent each round;
// every tool call adds its output to the following rounds' input.
const (
	taskRunBaseTokens          = 2000
	taskRunMinToolOutputTokens = 200
	taskRunMaxToolOutputTokens = 2000
	taskRunMinOutputTokens     = 100
	taskRunMaxOutputTokens     = 400
)

type taskAnalyzeArgs struct {
	Agent      string `json:"agent"`
	Content    string `json:"content"`
	Type       string `json:"type,omitempty" schema:"enum=prompt|script|project"`
	Target     string `json:"target,omitempty"`
	Model      string `json:"model,omitempty"`
	Precompile bool   `json:"precompile,omitempty"`
	Editor     string `json:"editor,omitempty"`
}

// taskRunEstimate is the estimated token use, and cost when the model's
// pricing is known, of one run of a task.
type taskRunEstimate struct {
	Available       bool     `json:"available"`
	Reason          string   `json:"reason,omitempty"`
	MinInputTokens  int      `json:"min_input_tokens"`
	MaxInputTokens  int      `json:"max_input_tokens"`
	MinOutputTokens int      `json:"min_output_tokens"`
	MaxOutputTokens int      `json:"max_output_tokens"`
	MinTokens       int      `json:"min_tokens"`
	MaxTokens       int      `json:"max_tokens"`
	MinCostUSD      *float64 `json:"min_cost_usd,omitempty"`
	MaxCostUSD      *float64 `json:"max_cost_usd,omitempty"`
}

// taskAnalysisResult is task_analyze's preview of what precompiling a task
// would do, and what each of its runs would cost.
type taskAnalysisResult struct {
	Verdict        string             `json:"verdict"`
	WillPrecompile bool               `json:"will_precompile"`
	Reason         string             `json:"reason,omitempty"`
	ModelSteps     []string           `json:"model_steps,omitempty"`
	Steps          []compiledTaskStep `json:"steps,omitempty"`
	Model          string             `json:"model,omitempty"`
	Estimate       taskRunEstimate    `json:"estimate"`
	Cached         bool               `json:"cached,omitempty"`
}

// taskAnalyzerResult is the outcome of running the precompile gate's
// determinism analysis for a prompt.
type taskAnalyzerResult struct {
	Analysis *compileTaskAnalysis
	Model    string
	Tools    []agent.ToolInfo
	Usage    llm.Usage
}

var taskAnalyzerFunc func(ctx context.Context, agentName, prompt, target string) (*taskAnalyzerResult, error)

func resolveTaskAnalyzer() func(ctx context.Context, agentName, prompt, target string) (*taskAnalyzerResult, error) {
	if taskAnalyzerFunc != nil {
		return taskAnalyzerFunc
	}
	return runTaskAnalyzer
}

// runTaskAnalyzer runs the same determinism analysis, on the same model and
// tool catalog, that tryCompileTaskPrompt gates precompilation on, without
// generating, validating, or saving anything.
func runTaskAnalyzer(ctx context.Context, agentName, prompt, target string) (*taskAnalyzerResult, error) {
	cfg, agentCfg, err := loadCompileAgentConfig(agentName)
	if err != nil {
		return nil, err
	}
	model := config.EffectiveAgentModel(*agentCfg, cfg.Models)
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("agent %q has no model configured", agentName)
	}
	tools, err := compileTools(ctx, agentName)
	if err != nil {
		return nil, err
	}
	provider, err := compileTaskProvider(model)
	if err != nil {
		return nil, err
	}
	usage := &llm.Usage{}
	analysis, err := analyzeTaskDeterminism(withLLMUsage(ctx, usage), provider, model, prompt, target, tools, false)
	if err != nil {
		return nil, err
	}
	if usage.InputTokens == 0 && usage.OutputTokens == 0 {
		// Providers that report no usage still spent tokens; record an estimate.
		usage.InputTokens = llm.EstimateTokens(taskDeterminismSystemPrompt) + llm.EstimateTokens(buildCompileStageUserPrompt(prompt, target, tools))
		usage.OutputTokens = llm.EstimateTokens(mustJSON(analysis))
	}
	return &taskAnalyzerResult{Analysis: analysis, Model: model, Tools: tools, Usage: *usage}, nil
}

type llmUsageContextKey struct{}

// withLLMUsage makes completeLLMText add the token use it sees to usage.
func withLLMUsage(ctx context.Context, usage *llm.Usage) context.Context {
	return context.WithValue(ctx, llmUsageContextKey{}, usage)
}

func llmUsageFromContext(ctx context.Context) *llm.Usage {
	usage, _ := ctx.Value(llmUsageContextKey{}).(*llm.Usage)
	return usage
}

// taskAnalysisCache keeps analyses by a hash of their input, so re-entering
// text that was already analyzed is not sent to the model again, and cancels
// an editor's in-flight analysis when it asks for another.
type taskAnalysisCache struct {
	mu       sync.Mutex
	entries  map[string]taskAnalysisCacheEntry
	inflight map[string]*taskAnalysisRun
}

// taskAnalysisRun is an editor's in-flight analysis.
type taskAnalysisRun struct{ cancel context.CancelFunc }

type taskAnalysisCacheEntry struct {
	result  *taskAnalyzerResult
	expires time.Time
}

const (
	taskAnalysisCacheTTL = 30 * time.Minute
	taskAnalysisCacheMax = 256
)

var taskAnalyses = &taskAnalysisCache{
	entries:  map[string]taskAnalysisCacheEntry{},
	inflight: map[string]*taskAnalysisRun{},
}

func taskAnalysisKey(agentName, prompt, target string) string {
	sum := sha256.Sum256([]byte(agentName + "\x00" + target + "\x00" + prompt))
	return hex.EncodeToString(sum[:])
}

func (c *taskAnalysisCache) get(key string) (*taskAnalyzerResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expires) {
		delete(c.entries, key)
		return nil, false
	}
	return entry.result, true
}

func (c *taskAnalysisCache) put(key string, result *taskAnalyzerResult) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.entries) >= taskAnalysisCacheMax {
		now := time.Now()
		for k, e := range c.entries {
			if now.After(e.expires) || len(c.entries) >= taskAnalysisCacheMax {
				delete(c.entries, k)
			}
		}
	}
	c.entries[key] = taskAnalysisCacheEntry{result: result, expires: time.Now().Add(taskAnalysisCacheTTL)}
}

// start cancels editor's previous analysis, if one is still running, and
// returns the context and release function of its new one.
func (c *taskAnalysisCache) start(ctx context.Context, editor string) (context.Context, func()) {
	ctx, cancel := context.WithCancel(ctx)
	if editor == "" {
		return ctx, cancel
	}
	run := &taskAnalysisRun{cancel: cancel}
	c.mu.Lock()
	if prev := c.inflight[editor]; prev != nil {
		prev.cancel()
	}
	c.inflight[editor] = run
	c.mu.Unlock()
	return ctx, func() {
		cancel()
		c.mu.Lock()
		// Only the latest analysis owns the editor's slot.
		if c.inflight[editor] == run {
			delete(c.inflight, editor)
		}
		c.mu.Unlock()
	}
}

func registerTaskAnalyzeTool(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "task_analyze",
		Description: "Preview, without compiling, saving, or scheduling anything, whether a task's prompt passes the same determinism gate precompilation uses, and estimate the tokens and cost of each run. type=script tasks report 0 tokens without analysis; type=project tasks are never precomputed. precompile=true reports the cost when the task is precompiled on save. editor identifies the calling editor: its analysis token use is recorded under it, and a new call cancels its previous one.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args taskAnalyzeArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		result, err := analyzeTaskForEditor(ctx, args)
		if err != nil {
			logTaskAnalyzeFailure(args, err)
			return nil, struct{}{}, err
		}
		return jsonResult(result)
	})
}

// errTaskAnalysisSuperseded is returned to an analysis cancelled because the
// same editor asked for a newer one.
var errTaskAnalysisSuperseded = errors.New("analysis superseded by a newer request")

// logTaskAnalyzeFailure logs a failed task_analyze call. A superseded
// analysis is expected while typing, so it is logged at debug level only.
func logTaskAnalyzeFailure(args taskAnalyzeArgs, err error) {
	level := slog.LevelWarn
	if errors.Is(err, errTaskAnalysisSuperseded) {
		level = slog.LevelDebug
	}
	slog.Log(context.Background(), level, "task_analyze failed", "component", "task_compile",
		"agent", strings.TrimSpace(args.Agent), "editor", strings.TrimSpace(args.Editor),
		"type", strings.TrimSpace(args.Type), "err", err)
}

// analyzeTaskForEditor builds task_analyze's result for args.
func analyzeTaskForEditor(ctx context.Context, args taskAnalyzeArgs) (*taskAnalysisResult, error) {
	switch strings.TrimSpace(args.Type) {
	case "script":
		return &taskAnalysisResult{
			Verdict:  taskVerdictScript,
			Reason:   "Runs a script",
			Estimate: taskRunEstimate{Available: true},
		}, nil
	case "project":
		return &taskAnalysisResult{
			Verdict:  taskVerdictProject,
			Reason:   "Project tasks run as Code Loop jobs, so they are never precomputed",
			Model:    strings.TrimSpace(args.Model),
			Estimate: taskRunEstimate{Reason: "depends on the work the Code Loop job does"},
		}, nil
	}
	agentName := strings.TrimSpace(args.Agent)
	prompt := strings.TrimSpace(args.Content)
	if agentName == "" {
		return nil, fmt.Errorf("agent is required")
	}
	if prompt == "" {
		return nil, fmt.Errorf("content is required")
	}
	target := strings.TrimSpace(args.Target)

	key := taskAnalysisKey(agentName, prompt, target)
	analyzed, cached := taskAnalyses.get(key)
	if !cached {
		runCtx, release := taskAnalyses.start(ctx, strings.TrimSpace(args.Editor))
		defer release()
		var err error
		analyzed, err = resolveTaskAnalyzer()(runCtx, agentName, prompt, target)
		recordTaskEditorUsage(agentName, analyzed, err)
		if err != nil {
			if runCtx.Err() != nil && ctx.Err() == nil {
				return nil, errTaskAnalysisSuperseded
			}
			return nil, err
		}
		taskAnalyses.put(key, analyzed)
	}
	result := buildTaskAnalysisResult(analyzed, prompt, strings.TrimSpace(args.Model), args.Precompile)
	result.Cached = cached
	return result, nil
}

// recordTaskEditorUsage writes the analysis's own token use to the usage log,
// under the task editor's session rather than any task's.
func recordTaskEditorUsage(agentName string, analyzed *taskAnalyzerResult, err error) {
	if analyzed == nil && err == nil {
		return
	}
	rec := domain.UsageRecord{
		Timestamp: time.Now(),
		SessionID: taskEditorUsageSessionID,
		AgentID:   agentName,
		Source:    domain.UsageSourceTaskEditor,
		HasError:  err != nil,
	}
	if analyzed != nil {
		rec.Model = analyzed.Model
		rec.Provider = models.ProviderOf(analyzed.Model)
		rec.InputTokens = analyzed.Usage.InputTokens
		rec.OutputTokens = analyzed.Usage.OutputTokens
		rec.CacheReadTokens = analyzed.Usage.CacheReadTokens
		rec.CacheWriteTokens = analyzed.Usage.CacheWriteTokens
	}
	if rec.InputTokens == 0 && rec.OutputTokens == 0 && !rec.HasError {
		return
	}
	if err := store.AppendJSONL(store.UsagePath(), rec); err != nil {
		slog.Warn("task_analyze: failed to record usage", "component", "task_compile", "err", err)
	}
}

// buildTaskAnalysisResult turns the gate's analysis into a verdict and a
// per-run estimate. model overrides the analyzer's model for pricing.
func buildTaskAnalysisResult(analyzed *taskAnalyzerResult, prompt, model string, precompile bool) *taskAnalysisResult {
	analysis := analyzed.Analysis
	model = firstNonEmpty(model, analyzed.Model)
	result := &taskAnalysisResult{
		Reason: strings.TrimSpace(analysis.Reason),
		Steps:  analysis.Steps,
		Model:  model,
	}
	deterministic := 0
	for _, step := range analysis.Steps {
		if step.Deterministic {
			deterministic++
		} else if desc := strings.TrimSpace(step.Description); desc != "" {
			result.ModelSteps = append(result.ModelSteps, desc)
		}
	}
	switch {
	case analysis.allowsCompile():
		result.Verdict = taskVerdictDeterministic
	case deterministic > 0 && len(result.ModelSteps) > 0:
		result.Verdict = taskVerdictPartial
	default:
		result.Verdict = taskVerdictNonDeterministic
	}
	if result.Reason == "" && result.Verdict != taskVerdictDeterministic {
		result.Reason = "task could not be validated as deterministic enough for script compilation"
	}
	// Precompilation is all-or-nothing: only a task that passes the gate is
	// compiled, and a partially deterministic one still runs entirely
	// through the model.
	result.WillPrecompile = precompile && result.Verdict == taskVerdictDeterministic
	if result.WillPrecompile {
		result.Estimate = taskRunEstimate{Available: true}
		return result
	}
	if analysis.UnboundedInput {
		result.Estimate = taskRunEstimate{Reason: "depends on the size of fetched data"}
		return result
	}
	result.Estimate = estimateTaskRun(prompt, analyzed.Tools, analysis.Steps, model)
	return result
}

// estimateTaskRun estimates the token use of running prompt through model,
// one model round per tool-using step plus a final answer.
func estimateTaskRun(prompt string, tools []agent.ToolInfo, steps []compiledTaskStep, model string) taskRunEstimate {
	base := taskRunBaseTokens + llm.EstimateTokens(prompt) + llm.EstimateTokens(renderCompileToolCatalog(tools))
	toolSteps := 0
	for _, step := range steps {
		if step.Kind != "notify" {
			toolSteps++
		}
	}
	minRounds, maxRounds := 1, toolSteps+1
	if toolSteps > 0 {
		minRounds = 2
	}
	roundsInput := func(rounds, toolOutput int) int {
		// Round k carries the output of the k tool calls before it.
		return rounds*base + toolOutput*rounds*(rounds-1)/2
	}
	est := taskRunEstimate{
		Available:       true,
		MinInputTokens:  roundsInput(minRounds, taskRunMinToolOutputTokens),
		MaxInputTokens:  roundsInput(maxRounds, taskRunMaxToolOutputTokens),
		MinOutputTokens: minRounds * taskRunMinOutputTokens,
		MaxOutputTokens: maxRounds * taskRunMaxOutputTokens,
	}
	est.MinTokens = est.MinInputTokens + est.MinOutputTokens
	est.MaxTokens = est.MaxInputTokens + est.MaxOutputTokens
	if entry, ok := models.Lookup(model); ok && (entry.InputCostPerMTok > 0 || entry.OutputCostPerMTok > 0) {
		cost := func(in, out int) *float64 {
			v := (float64(in)*entry.InputCostPerMTok + float64(out)*entry.OutputCostPerMTok) / 1e6
			return &v
		}
		est.MinCostUSD = cost(est.MinInputTokens, est.MinOutputTokens)
		est.MaxCostUSD = cost(est.MaxInputTokens, est.MaxOutputTokens)
	}
	return est
}
