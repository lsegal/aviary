package mcp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strconv"
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

// Rough token figures for the trigger context a workflow run's prompt gets:
// a short header, the matched message, and up to workflowContextMessages
// messages before it (see scheduler.WorkflowPrompt).
const (
	workflowContextHeaderTokens = 40
	workflowContextMessages     = 3
	workflowMinMessageTokens    = 15
	workflowMaxMessageTokens    = 300
)

type taskAnalyzeArgs struct {
	Agent      string `json:"agent"`
	Content    string `json:"content"`
	Type       string `json:"type,omitempty" schema:"enum=prompt|script|project"`
	Target     string `json:"target,omitempty"`
	Model      string `json:"model,omitempty"`
	Project    string `json:"project,omitempty"`
	Precompile bool   `json:"precompile,omitempty"`
	Editor     string `json:"editor,omitempty"`
	// Trigger is what starts the task's runs: "schedule" (default) for a
	// scheduled task, or "message" for a workflow, whose runs also carry
	// the triggering message and the messages before it.
	Trigger string `json:"trigger,omitempty" schema:"enum=schedule|message"`
	// CachedOnly returns the cached analysis, or a miss, without analyzing.
	CachedOnly bool `json:"cached_only,omitempty"`
	// Force re-analyzes even when a cached analysis exists, and replaces it.
	Force bool `json:"force,omitempty"`
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
	// MinContextTokens and MaxContextTokens are the part of each round's
	// input that is a workflow's trigger context, included in the totals.
	MinContextTokens int `json:"min_context_tokens,omitempty"`
	MaxContextTokens int `json:"max_context_tokens,omitempty"`
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
	// WithoutPrecompile is what each run would cost on the model, reported
	// beside Estimate when the task will be precompiled.
	WithoutPrecompile *taskRunEstimate `json:"without_precompile,omitempty"`
	Cached            bool             `json:"cached,omitempty"`
	// AnalyzedAt is when the analysis shown was run on the model.
	AnalyzedAt *time.Time `json:"analyzed_at,omitempty"`
	// Miss reports that a cached_only call found no cached analysis; the
	// rest of the result is empty.
	Miss bool `json:"miss,omitempty"`
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

// taskAnalysisCache keeps analyses by a hash of their input, persisted under
// the data directory so reopening an unchanged editor, after a reload, a
// server restart, or in another browser, shows its last analysis without
// sending it to the model again. It also cancels an editor's in-flight
// analysis when it asks for another.
type taskAnalysisCache struct {
	mu sync.Mutex
	// path resolves where entries are persisted, empty to keep them in
	// memory only. It is resolved on each use, so a data directory change
	// switches to that directory's entries.
	path     func() string
	loaded   *string
	entries  map[string]*taskAnalysisEntry
	inflight map[string]*taskAnalysisRun
}

// taskAnalysisRun is an editor's in-flight analysis.
type taskAnalysisRun struct{ cancel context.CancelFunc }

// taskAnalysisEntry is a cached analysis: what the determinism gate found,
// and what buildTaskAnalysisResult needs to turn it into a verdict and an
// estimate. The estimate is rebuilt from it on every lookup, so the model
// override, the precompile flag, and pricing changes apply to cached entries.
type taskAnalysisEntry struct {
	Analysis   *compileTaskAnalysis `json:"analysis"`
	Model      string               `json:"model,omitempty"`
	ToolTokens int                  `json:"tool_tokens"`
	AnalyzedAt time.Time            `json:"analyzed_at"`
}

const (
	// taskAnalysisCacheTTL bounds how long an analysis is trusted while the
	// agent's tools or model can change underneath it.
	taskAnalysisCacheTTL = 30 * 24 * time.Hour
	taskAnalysisCacheMax = 512
)

// newTaskAnalysisCache returns a cache persisted where path says.
func newTaskAnalysisCache(path func() string) *taskAnalysisCache {
	return &taskAnalysisCache{
		path:     path,
		entries:  map[string]*taskAnalysisEntry{},
		inflight: map[string]*taskAnalysisRun{},
	}
}

var taskAnalyses = newTaskAnalysisCache(taskAnalysisCachePath)

// taskAnalysisCachePath is where analyses are persisted.
func taskAnalysisCachePath() string {
	return filepath.Join(store.DataDir(), "task-analyses.json")
}

// taskAnalysisKey hashes every input the analysis depends on: the compiler
// version, so a compiler change re-analyzes every task, and the agent,
// target, task type, project, and trigger, which the analyzed prompt is
// built from. The model override and the precompile flag only change how the
// cached analysis is presented, which buildTaskAnalysisResult redoes on
// every lookup.
func taskAnalysisKey(agentName, prompt, target, taskType, project, trigger string) string {
	sum := sha256.New()
	for _, part := range []string{strconv.Itoa(taskCompilerVersion), agentName, target, taskType, project, trigger, prompt} {
		sum.Write([]byte(part))
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// load reads the persisted entries, once per path, and returns the path.
// The caller holds c.mu.
func (c *taskAnalysisCache) load() string {
	path := c.path()
	if c.loaded != nil && *c.loaded == path {
		return path
	}
	c.loaded = &path
	c.entries = map[string]*taskAnalysisEntry{}
	if path == "" {
		return path
	}
	entries, err := store.ReadJSON[map[string]*taskAnalysisEntry](path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("task_analyze: failed to read analysis cache", "component", "task_compile", "path", path, "err", err)
		}
		return path
	}
	for k, e := range entries {
		if e != nil && e.Analysis != nil {
			c.entries[k] = e
		}
	}
	return path
}

func (c *taskAnalysisCache) get(key string) (*taskAnalysisEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.load()
	entry, ok := c.entries[key]
	if !ok || time.Since(entry.AnalyzedAt) > taskAnalysisCacheTTL {
		return nil, false
	}
	return entry, true
}

func (c *taskAnalysisCache) put(key string, entry *taskAnalysisEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	path := c.load()
	c.entries[key] = entry
	for k, e := range c.entries {
		if time.Since(e.AnalyzedAt) > taskAnalysisCacheTTL {
			delete(c.entries, k)
		}
	}
	if over := len(c.entries) - taskAnalysisCacheMax; over > 0 {
		keys := make([]string, 0, len(c.entries))
		for k := range c.entries {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b string) int { return c.entries[a].AnalyzedAt.Compare(c.entries[b].AnalyzedAt) })
		for _, k := range keys[:over] {
			delete(c.entries, k)
		}
	}
	if path == "" {
		return
	}
	if err := store.WriteJSON(path, c.entries); err != nil {
		slog.Warn("task_analyze: failed to write analysis cache", "component", "task_compile", "path", path, "err", err)
	}
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
		Description: "Preview, without compiling, saving, or scheduling anything, whether a task's prompt passes the same determinism gate precompilation uses, and estimate the tokens and cost of each run. type=script tasks report 0 tokens without analysis; type=project tasks need project=<watched project> and are analyzed as their script would run, in a clone of it. trigger=message analyzes a workflow, as its script would run with the triggering message, and adds the message context each run's prompt carries to the estimate. precompile=true reports the cost when the task is precompiled on save. editor identifies the calling editor: its analysis token use is recorded under it, and a new call cancels its previous one. Analyses are cached by a hash of every input that affects them, including the compiler version, and persist across restarts: a cached result has cached=true and analyzed_at, and makes no model call. cached_only=true returns the cached analysis, or miss=true, without analyzing; force=true re-analyzes and replaces the cached entry.",
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
	workflow := strings.EqualFold(strings.TrimSpace(args.Trigger), config.TriggerMessage)
	// A project task is analyzed as it is compiled: with its clone described,
	// and a workflow with its triggering message.
	analyzedPrompt := prompt
	if workflow {
		if strings.TrimSpace(args.Type) == "project" {
			return nil, fmt.Errorf("workflows run prompt or script content")
		}
		analyzedPrompt = workflowTaskCompilePrompt(prompt)
	}
	if strings.TrimSpace(args.Type) == "project" {
		if strings.TrimSpace(args.Project) == "" {
			return nil, fmt.Errorf("project is required for project tasks")
		}
		analyzedPrompt = projectTaskCompilePrompt(prompt, args.Project)
		// It sees the tools its script would run with.
		ctx = agent.WithProjectTaskScript(ctx)
	}

	trigger := "schedule"
	if workflow {
		trigger = config.TriggerMessage
	}
	key := taskAnalysisKey(agentName, analyzedPrompt, target, strings.TrimSpace(args.Type), strings.TrimSpace(args.Project), trigger)
	var entry *taskAnalysisEntry
	cached := false
	if !args.Force {
		entry, cached = taskAnalyses.get(key)
	}
	if !cached {
		if args.CachedOnly {
			return &taskAnalysisResult{Miss: true}, nil
		}
		runCtx, release := taskAnalyses.start(ctx, strings.TrimSpace(args.Editor))
		defer release()
		analyzed, err := resolveTaskAnalyzer()(runCtx, agentName, analyzedPrompt, target)
		recordTaskEditorUsage(agentName, analyzed, err)
		if err != nil {
			if runCtx.Err() != nil && ctx.Err() == nil {
				return nil, errTaskAnalysisSuperseded
			}
			return nil, err
		}
		entry = &taskAnalysisEntry{
			Analysis:   analyzed.Analysis,
			Model:      analyzed.Model,
			ToolTokens: llm.EstimateTokens(renderCompileToolCatalog(analyzed.Tools)),
			AnalyzedAt: time.Now().UTC(),
		}
		taskAnalyses.put(key, entry)
	}
	result := buildTaskAnalysisResult(entry, prompt, strings.TrimSpace(args.Model), args.Precompile, workflow)
	result.Cached = cached
	analyzedAt := entry.AnalyzedAt
	result.AnalyzedAt = &analyzedAt
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
// per-run estimate. model overrides the analyzer's model for pricing, and
// workflow adds a workflow run's trigger context to the estimate.
func buildTaskAnalysisResult(analyzed *taskAnalysisEntry, prompt, model string, precompile, workflow bool) *taskAnalysisResult {
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
	modelEstimate := taskRunEstimate{Reason: "depends on the size of fetched data"}
	if !analysis.UnboundedInput {
		modelEstimate = estimateTaskRun(prompt, analyzed.ToolTokens, analysis.Steps, model, workflow)
	}
	if result.WillPrecompile {
		result.Estimate = taskRunEstimate{Available: true}
		result.WithoutPrecompile = &modelEstimate
		return result
	}
	result.Estimate = modelEstimate
	return result
}

// workflowContextTokens is the range of tokens a workflow run's trigger
// context adds to its prompt: the matched message alone, short, up to it
// and workflowContextMessages long messages before it.
func workflowContextTokens() (minTokens, maxTokens int) {
	return workflowContextHeaderTokens + workflowMinMessageTokens,
		workflowContextHeaderTokens + (workflowContextMessages+1)*workflowMaxMessageTokens
}

// estimateTaskRun estimates the token use of running prompt through model,
// whose tool catalog is toolTokens long, one model round per tool-using step
// plus a final answer. A workflow's
// rounds also carry its trigger context.
func estimateTaskRun(prompt string, toolTokens int, steps []compiledTaskStep, model string, workflow bool) taskRunEstimate {
	base := taskRunBaseTokens + llm.EstimateTokens(prompt) + toolTokens
	minContext, maxContext := 0, 0
	if workflow {
		minContext, maxContext = workflowContextTokens()
	}
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
	roundsInput := func(rounds, context, toolOutput int) int {
		// Round k carries the output of the k tool calls before it.
		return rounds*(base+context) + toolOutput*rounds*(rounds-1)/2
	}
	est := taskRunEstimate{
		Available:        true,
		MinInputTokens:   roundsInput(minRounds, minContext, taskRunMinToolOutputTokens),
		MaxInputTokens:   roundsInput(maxRounds, maxContext, taskRunMaxToolOutputTokens),
		MinOutputTokens:  minRounds * taskRunMinOutputTokens,
		MaxOutputTokens:  maxRounds * taskRunMaxOutputTokens,
		MinContextTokens: minContext,
		MaxContextTokens: maxContext,
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
