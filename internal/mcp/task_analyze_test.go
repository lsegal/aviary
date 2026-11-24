package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

// stubTaskAnalyzer replaces the analyzer with one that answers each prompt
// from analyses and counts its calls.
func stubTaskAnalyzer(t *testing.T, analyses map[string]*compileTaskAnalysis) *int {
	t.Helper()
	base := t.TempDir()
	store.SetDataDir(filepath.Join(base, "aviary"))
	t.Cleanup(func() { store.SetDataDir("") })
	require.NoError(t, store.EnsureDirs())

	prev := taskAnalyzerFunc
	prevCache := taskAnalyses
	t.Cleanup(func() {
		taskAnalyzerFunc = prev
		taskAnalyses = prevCache
	})
	taskAnalyses = newTaskAnalysisCache(taskAnalysisCachePath)
	calls := 0
	taskAnalyzerFunc = func(_ context.Context, agentName, prompt, _ string) (*taskAnalyzerResult, error) {
		calls++
		assert.Equal(t, "bot", agentName)
		analysis, ok := analyses[prompt]
		require.True(t, ok, "unexpected prompt %q", prompt)
		return &taskAnalyzerResult{
			Analysis: analysis,
			Model:    "test/model",
			Usage:    llm.Usage{InputTokens: 120, OutputTokens: 30},
		}, nil
	}
	return &calls
}

var sampleTaskAnalyses = map[string]*compileTaskAnalysis{
	"check https://example.com and print its status": {
		Reason: "fixed URL check and fixed message",
		Steps: []compiledTaskStep{
			{Kind: "deterministic", Deterministic: true, Tool: "web_get", Description: "fetch the URL"},
			{Kind: "notify", Deterministic: true, Description: "print the status"},
		},
	},
	"fetch the release notes and summarize them": {
		Reason: "summarizing needs judgment",
		Steps: []compiledTaskStep{
			{Kind: "deterministic", Deterministic: true, Tool: "web_get", Description: "fetch the release notes"},
			{Kind: "agent_run", Deterministic: false, Description: "summarizing the results"},
		},
	},
	"find something interesting to tell me": {
		NeedsDiscovery: true,
		Reason:         "open-ended search",
		Steps: []compiledTaskStep{
			{Kind: "agent_run", Deterministic: false, Description: "find something interesting"},
		},
	},
	"Say hello": {
		Reason: "a single fixed notify step that can be implemented as print(\"Hello\")",
		Steps: []compiledTaskStep{
			{Kind: "notify", Deterministic: true, Description: "print a fixed greeting"},
		},
	},
	"look busy": {
		NeedsDiscovery: true,
		Reason:         "needs discovery",
		Steps: []compiledTaskStep{
			{Kind: "deterministic", Deterministic: true, Description: "check something"},
		},
	},
	"summarize every new email": {
		UnboundedInput: true,
		Reason:         "summaries need judgment",
		Steps: []compiledTaskStep{
			{Kind: "deterministic", Deterministic: true, Tool: "gmail_search", Description: "read new email"},
			{Kind: "agent_run", Deterministic: false, Description: "summarize each email"},
		},
	},
}

func TestTaskAnalyzeVerdicts(t *testing.T) {
	stubTaskAnalyzer(t, sampleTaskAnalyses)

	det, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "check https://example.com and print its status", Precompile: true})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictDeterministic, det.Verdict)
	assert.True(t, det.WillPrecompile)
	assert.True(t, det.Estimate.Available)
	assert.Zero(t, det.Estimate.MaxTokens)

	partial, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "fetch the release notes and summarize them", Precompile: true})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictPartial, partial.Verdict)
	assert.False(t, partial.WillPrecompile, "a partially deterministic task is not precompiled")
	assert.Equal(t, []string{"summarizing the results"}, partial.ModelSteps)
	assert.True(t, partial.Estimate.Available)
	assert.Positive(t, partial.Estimate.MinTokens)
	assert.Greater(t, partial.Estimate.MaxTokens, partial.Estimate.MinTokens)
	assert.Nil(t, partial.Estimate.MinCostUSD, "test/model has no catalog pricing")

	non, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "find something interesting to tell me"})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictNonDeterministic, non.Verdict)
	assert.Equal(t, "open-ended search", non.Reason)
	assert.Positive(t, non.Estimate.MinTokens)

	unbounded, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "summarize every new email"})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictPartial, unbounded.Verdict)
	assert.False(t, unbounded.Estimate.Available)
	assert.Equal(t, "depends on the size of fetched data", unbounded.Estimate.Reason)
}

func TestTaskAnalyzeDeterministicWithoutPrecompileEstimatesModelRun(t *testing.T) {
	stubTaskAnalyzer(t, sampleTaskAnalyses)
	res, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "check https://example.com and print its status"})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictDeterministic, res.Verdict)
	assert.False(t, res.WillPrecompile)
	assert.Positive(t, res.Estimate.MinTokens)
}

func TestTaskAnalyzeCachesByInputAndRecordsEditorUsage(t *testing.T) {
	calls := stubTaskAnalyzer(t, sampleTaskAnalyses)
	args := taskAnalyzeArgs{Agent: "bot", Content: "find something interesting to tell me", Editor: "ed-1"}
	first, err := analyzeTaskForEditor(context.Background(), args)
	require.NoError(t, err)
	assert.False(t, first.Cached)
	second, err := analyzeTaskForEditor(context.Background(), args)
	require.NoError(t, err)
	assert.True(t, second.Cached)
	assert.Equal(t, 1, *calls)

	records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, taskEditorUsageSessionID, records[0].SessionID)
	assert.Equal(t, domain.UsageSourceTaskEditor, records[0].Source)
	assert.Equal(t, 120, records[0].InputTokens)
	assert.Equal(t, 30, records[0].OutputTokens)
}

func TestTaskAnalyzeCachePersistsAcrossRestarts(t *testing.T) {
	calls := stubTaskAnalyzer(t, sampleTaskAnalyses)
	args := taskAnalyzeArgs{Agent: "bot", Content: "fetch the release notes and summarize them", Editor: "ed-1"}
	first, err := analyzeTaskForEditor(context.Background(), args)
	require.NoError(t, err)
	require.NotNil(t, first.AnalyzedAt)
	assert.FileExists(t, taskAnalysisCachePath())

	// A restarted server loads the persisted entry instead of re-analyzing.
	taskAnalyses = newTaskAnalysisCache(taskAnalysisCachePath)
	again, err := analyzeTaskForEditor(context.Background(), args)
	require.NoError(t, err)
	assert.True(t, again.Cached)
	assert.Equal(t, 1, *calls)
	require.NotNil(t, again.AnalyzedAt)
	assert.True(t, first.AnalyzedAt.Equal(*again.AnalyzedAt), "the cached result reports when it was analyzed")
	assert.Equal(t, first.Verdict, again.Verdict)
	assert.Equal(t, first.Estimate, again.Estimate)

	records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
	require.NoError(t, err)
	assert.Len(t, records, 1, "a cache hit records no editor usage")
}

func TestTaskAnalyzeCacheKeysEveryInput(t *testing.T) {
	calls := stubTaskAnalyzer(t, nil)
	taskAnalyzerFunc = func(context.Context, string, string, string) (*taskAnalyzerResult, error) {
		*calls++
		return &taskAnalyzerResult{Analysis: sampleTaskAnalyses["Say hello"], Model: "test/model"}, nil
	}
	base := taskAnalyzeArgs{Agent: "bot", Content: "Say hello"}
	_, err := analyzeTaskForEditor(context.Background(), base)
	require.NoError(t, err)
	require.Equal(t, 1, *calls)

	// Inputs the analysis depends on are each analyzed again.
	for name, args := range map[string]taskAnalyzeArgs{
		"prompt":  {Agent: "bot", Content: "Say hello!"},
		"agent":   {Agent: "other", Content: "Say hello"},
		"target":  {Agent: "bot", Content: "Say hello", Target: "slack:C1"},
		"trigger": {Agent: "bot", Content: "Say hello", Trigger: "message"},
		"project": {Agent: "bot", Content: "Say hello", Type: "project", Project: "acme/app"},
	} {
		before := *calls
		res, err := analyzeTaskForEditor(context.Background(), args)
		require.NoError(t, err, name)
		assert.False(t, res.Cached, name)
		assert.Equal(t, before+1, *calls, name)
	}

	// The model override and the precompile flag reuse the analysis, but
	// the result shown follows them.
	before := *calls
	res, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "Say hello", Model: "other/model", Precompile: true})
	require.NoError(t, err)
	assert.True(t, res.Cached)
	assert.Equal(t, before, *calls)
	assert.Equal(t, "other/model", res.Model)
	assert.True(t, res.WillPrecompile)

	// Reverting to the original input is still cached.
	res, err = analyzeTaskForEditor(context.Background(), base)
	require.NoError(t, err)
	assert.True(t, res.Cached)
	assert.Equal(t, before, *calls)
}

func TestTaskAnalyzeCompilerVersionBumpInvalidatesCache(t *testing.T) {
	calls := stubTaskAnalyzer(t, sampleTaskAnalyses)
	args := taskAnalyzeArgs{Agent: "bot", Content: "Say hello"}
	_, err := analyzeTaskForEditor(context.Background(), args)
	require.NoError(t, err)

	prev := taskCompilerVersion
	t.Cleanup(func() { taskCompilerVersion = prev })
	taskCompilerVersion++
	res, err := analyzeTaskForEditor(context.Background(), args)
	require.NoError(t, err)
	assert.False(t, res.Cached)
	assert.Equal(t, 2, *calls)
}

func TestTaskAnalyzeCachedOnlyAndForce(t *testing.T) {
	calls := stubTaskAnalyzer(t, sampleTaskAnalyses)
	args := taskAnalyzeArgs{Agent: "bot", Content: "Say hello", CachedOnly: true}
	miss, err := analyzeTaskForEditor(context.Background(), args)
	require.NoError(t, err)
	assert.True(t, miss.Miss)
	assert.Zero(t, *calls, "a cached_only lookup never analyzes")

	args.CachedOnly = false
	first, err := analyzeTaskForEditor(context.Background(), args)
	require.NoError(t, err)

	args.CachedOnly = true
	hit, err := analyzeTaskForEditor(context.Background(), args)
	require.NoError(t, err)
	assert.False(t, hit.Miss)
	assert.True(t, hit.Cached)
	assert.Equal(t, taskVerdictDeterministic, hit.Verdict)

	time.Sleep(time.Millisecond)
	forced, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "Say hello", Force: true})
	require.NoError(t, err)
	assert.False(t, forced.Cached)
	assert.Equal(t, 2, *calls)
	assert.True(t, forced.AnalyzedAt.After(*first.AnalyzedAt))

	// The forced analysis replaced the cached entry.
	args.CachedOnly = true
	hit, err = analyzeTaskForEditor(context.Background(), args)
	require.NoError(t, err)
	assert.True(t, hit.AnalyzedAt.Equal(*forced.AnalyzedAt))
}

func TestTaskAnalyzeCacheExpiresAndEvicts(t *testing.T) {
	stubTaskAnalyzer(t, nil)
	cache := newTaskAnalysisCache(taskAnalysisCachePath)
	cache.put("old", &taskAnalysisEntry{Analysis: &compileTaskAnalysis{}, AnalyzedAt: time.Now().Add(-taskAnalysisCacheTTL - time.Hour)})
	_, ok := cache.get("old")
	assert.False(t, ok, "an expired analysis is not used")

	for i := range taskAnalysisCacheMax + 5 {
		cache.put(strconv.Itoa(i), &taskAnalysisEntry{Analysis: &compileTaskAnalysis{}, AnalyzedAt: time.Now().Add(time.Duration(i) * time.Second)})
	}
	reloaded := newTaskAnalysisCache(taskAnalysisCachePath)
	_, ok = reloaded.get("0")
	assert.False(t, ok, "the oldest analyses are evicted past the limit")
	_, ok = reloaded.get(strconv.Itoa(taskAnalysisCacheMax + 4))
	assert.True(t, ok)
	reloaded.mu.Lock()
	defer reloaded.mu.Unlock()
	assert.Len(t, reloaded.entries, taskAnalysisCacheMax)
}

func TestTaskAnalyzeScriptSkipsAnalyzer(t *testing.T) {
	calls := stubTaskAnalyzer(t, sampleTaskAnalyses)
	script, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "print('hi')", Type: "script"})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictScript, script.Verdict)
	assert.True(t, script.Estimate.Available)
	assert.Zero(t, script.Estimate.MaxTokens)
	assert.Zero(t, *calls)
}

func TestTaskAnalyzeProjectRunsAnalyzerWithClone(t *testing.T) {
	const prompt = "rerun failed CI jobs on open PRs"
	analyses := map[string]*compileTaskAnalysis{
		projectTaskCompilePrompt(prompt, "acme/app"):              sampleTaskAnalyses["check https://example.com and print its status"],
		projectTaskCompilePrompt("triage new issues", "acme/app"): sampleTaskAnalyses["fetch the release notes and summarize them"],
	}
	calls := stubTaskAnalyzer(t, analyses)

	det, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: prompt, Type: "project", Project: "acme/app", Precompile: true})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictDeterministic, det.Verdict)
	assert.True(t, det.WillPrecompile)
	assert.True(t, det.Estimate.Available)
	assert.Zero(t, det.Estimate.MaxTokens)

	partial, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "triage new issues", Type: "project", Project: "acme/app", Precompile: true, Model: "claude:opus"})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictPartial, partial.Verdict)
	assert.False(t, partial.WillPrecompile)
	assert.Equal(t, "claude:opus", partial.Model)
	assert.Positive(t, partial.Estimate.MaxTokens)
	assert.Equal(t, 2, *calls)

	_, err = analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: prompt, Type: "project"})
	require.ErrorContains(t, err, "project is required")
}

func TestTaskAnalyzeToolViaDispatcher(t *testing.T) {
	stubTaskAnalyzer(t, sampleTaskAnalyses)
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })

	d := NewDispatcher("https://localhost:16677", "")
	out, err := d.CallTool(context.Background(), "task_analyze", map[string]any{
		"agent":      "bot",
		"content":    "check https://example.com and print its status",
		"precompile": true,
	})
	require.NoError(t, err)
	var res taskAnalysisResult
	require.NoError(t, json.Unmarshal([]byte(out), &res))
	assert.Equal(t, taskVerdictDeterministic, res.Verdict)
	assert.True(t, res.WillPrecompile)
}

func TestTaskAnalyzePrecompilesSingleDeterministicStep(t *testing.T) {
	stubTaskAnalyzer(t, sampleTaskAnalyses)

	res, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "Say hello", Precompile: true})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictDeterministic, res.Verdict)
	assert.True(t, res.WillPrecompile)
	assert.Zero(t, res.Estimate.MaxTokens)

	unsaved, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "Say hello"})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictDeterministic, unsaved.Verdict)
	assert.False(t, unsaved.WillPrecompile)
}

func TestTaskAnalyzeAllDeterministicStepsAreNeverNondeterministic(t *testing.T) {
	stubTaskAnalyzer(t, sampleTaskAnalyses)

	for prompt, analysis := range sampleTaskAnalyses {
		allDeterministic := len(analysis.Steps) > 0
		for _, step := range analysis.Steps {
			allDeterministic = allDeterministic && step.Deterministic
		}
		if !allDeterministic || analysis.NeedsDiscovery {
			continue
		}
		res, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: prompt, Precompile: true})
		require.NoError(t, err)
		assert.Equal(t, taskVerdictDeterministic, res.Verdict, prompt)
		assert.True(t, res.WillPrecompile, prompt)
	}

	discovery, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "look busy", Precompile: true})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictNonDeterministic, discovery.Verdict, "discovery is nondeterminism even when every listed step is deterministic")
	assert.False(t, discovery.WillPrecompile)
}

func TestTaskDeterminismPromptHasNoStepThresholdOrTargetReason(t *testing.T) {
	assert.NotContains(t, taskDeterminismSystemPrompt, "more than one deterministic step")
	assert.NotContains(t, taskDeterminismSystemPrompt, "should_compile")
	assert.Contains(t, taskDeterminismSystemPrompt, "never mention the output target in the reason")
}

func TestAnalyzeTaskDeterminismCountsStepsNotTheModelsCount(t *testing.T) {
	provider := &recordingProvider{events: []llm.Event{
		{Type: llm.EventTypeText, Text: `{"deterministic_step_count":5,"steps":[{"kind":"notify","deterministic":true}]}`},
		{Type: llm.EventTypeDone},
	}}
	analysis, err := analyzeTaskDeterminism(context.Background(), provider, "test/model", "Say hello", "", nil, false)
	require.NoError(t, err)
	assert.Equal(t, 1, analysis.DeterministicStepCount)
	assert.True(t, analysis.allowsCompile())
}

func TestAnalyzeTaskDeterminismParsesAndCountsUsage(t *testing.T) {
	provider := &recordingProvider{events: []llm.Event{
		{Type: llm.EventTypeText, Text: "```json\n{\"unbounded_input\":false,\"steps\":[{\"kind\":\"deterministic\",\"deterministic\":true},{\"kind\":\"notify\",\"deterministic\":true}]}\n```"},
		{Type: llm.EventTypeUsage, Usage: &llm.Usage{InputTokens: 50, OutputTokens: 7}},
		{Type: llm.EventTypeDone},
	}}
	usage := &llm.Usage{}
	analysis, err := analyzeTaskDeterminism(withLLMUsage(context.Background(), usage), provider, "test/model", "check", "", nil, false)
	require.NoError(t, err)
	assert.Equal(t, 2, analysis.DeterministicStepCount)
	assert.True(t, analysis.allowsCompile())
	assert.Equal(t, 50, usage.InputTokens)
	assert.Equal(t, 7, usage.OutputTokens)
	require.Len(t, provider.requests, 1)
	assert.Equal(t, taskDeterminismSystemPrompt, provider.requests[0].System)
}

func TestEstimateTaskRunPricesCatalogModels(t *testing.T) {
	steps := []compiledTaskStep{
		{Kind: "deterministic", Deterministic: true, Tool: "web_get"},
		{Kind: "agent_run"},
	}
	est := estimateTaskRun("fetch the release notes and summarize them", 0, steps, "anthropic/claude-opus-5-5", false)
	require.NotNil(t, est.MinCostUSD)
	require.NotNil(t, est.MaxCostUSD)
	// Claude Opus 5.5 costs $4 per million input tokens and $20 per million output tokens.
	assert.InDelta(t, (float64(est.MinInputTokens)*4+float64(est.MinOutputTokens)*20)/1e6, *est.MinCostUSD, 1e-12)
	assert.InDelta(t, (float64(est.MaxInputTokens)*4+float64(est.MaxOutputTokens)*20)/1e6, *est.MaxCostUSD, 1e-12)
	assert.Greater(t, *est.MaxCostUSD, *est.MinCostUSD)

	unpriced := estimateTaskRun("fetch the release notes and summarize them", 0, steps, "openai-codex/gpt-6-sol", false)
	assert.Nil(t, unpriced.MinCostUSD, "subscription-billed models show tokens only")
}

func TestCompileTaskProviderRunsCLIModelsThroughTheirCLI(t *testing.T) {
	prev := completeCLIText
	t.Cleanup(func() { completeCLIText = prev })
	var gotModel, gotSystem, gotPrompt string
	completeCLIText = func(_ context.Context, model, system, prompt string) (string, error) {
		gotModel, gotSystem, gotPrompt = model, system, prompt
		return `{"steps":[{"kind":"deterministic","deterministic":true},{"kind":"notify","deterministic":true}]}`, nil
	}

	provider, err := compileTaskProvider("claude/opus")
	require.NoError(t, err)
	analysis, err := analyzeTaskDeterminism(context.Background(), provider, "claude/opus", "check the aviary.com site", "", nil, false)
	require.NoError(t, err)
	assert.True(t, analysis.allowsCompile())
	assert.Equal(t, "claude/opus", gotModel)
	assert.Equal(t, taskDeterminismSystemPrompt, gotSystem)
	assert.Contains(t, gotPrompt, "check the aviary.com site")

	_, err = provider.Stream(context.Background(), llm.Request{Tools: []llm.ToolDefinition{{Name: "x"}}})
	assert.Error(t, err)
}

func TestTaskAnalyzeLogsFailures(t *testing.T) {
	stubTaskAnalyzer(t, sampleTaskAnalyses)
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	var buf bytes.Buffer
	prevLog := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prevLog) })

	taskAnalyzerFunc = func(context.Context, string, string, string) (*taskAnalyzerResult, error) {
		return nil, errors.New("provider unavailable")
	}
	d := NewDispatcher("https://localhost:16677", "")
	text, isError, err := d.CallToolWithProgress(context.Background(), "task_analyze", map[string]any{
		"agent": "bot", "content": "hello", "editor": "task-editor-1", "precompile": true,
	}, nil)
	require.NoError(t, err)
	assert.True(t, isError)
	assert.Equal(t, "provider unavailable", text)
	logged := buf.String()
	assert.Contains(t, logged, "level=WARN")
	assert.Contains(t, logged, `msg="task_analyze failed"`)
	assert.Contains(t, logged, "agent=bot")
	assert.Contains(t, logged, "editor=task-editor-1")
	assert.Contains(t, logged, `err="provider unavailable"`)

	buf.Reset()
	logTaskAnalyzeFailure(taskAnalyzeArgs{Agent: "bot", Editor: "task-editor-1"}, errTaskAnalysisSuperseded)
	assert.NotContains(t, buf.String(), "task_analyze failed", "superseded analyses log below the default level")
}
