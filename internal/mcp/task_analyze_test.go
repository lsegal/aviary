package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

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
	taskAnalyses = &taskAnalysisCache{entries: map[string]taskAnalysisCacheEntry{}, inflight: map[string]*taskAnalysisRun{}}
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
		ShouldCompile:          true,
		DeterministicStepCount: 2,
		Reason:                 "fixed URL check and fixed message",
		Steps: []compiledTaskStep{
			{Kind: "deterministic", Deterministic: true, Tool: "web_get", Description: "fetch the URL"},
			{Kind: "notify", Deterministic: true, Description: "print the status"},
		},
	},
	"fetch the release notes and summarize them": {
		ShouldCompile:          false,
		DeterministicStepCount: 1,
		Reason:                 "summarizing needs judgment",
		Steps: []compiledTaskStep{
			{Kind: "deterministic", Deterministic: true, Tool: "web_get", Description: "fetch the release notes"},
			{Kind: "agent_run", Deterministic: false, Description: "summarizing the results"},
		},
	},
	"find something interesting to tell me": {
		ShouldCompile:  false,
		NeedsDiscovery: true,
		Reason:         "open-ended search",
		Steps: []compiledTaskStep{
			{Kind: "agent_run", Deterministic: false, Description: "find something interesting"},
		},
	},
	"summarize every new email": {
		ShouldCompile:  false,
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

func TestTaskAnalyzeScriptAndProjectSkipAnalyzer(t *testing.T) {
	calls := stubTaskAnalyzer(t, sampleTaskAnalyses)
	script, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "print('hi')", Type: "script"})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictScript, script.Verdict)
	assert.True(t, script.Estimate.Available)
	assert.Zero(t, script.Estimate.MaxTokens)

	project, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "triage", Type: "project"})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictProject, project.Verdict)
	assert.False(t, project.Estimate.Available)
	assert.Zero(t, *calls)
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

func TestAnalyzeTaskDeterminismParsesAndCountsUsage(t *testing.T) {
	provider := &recordingProvider{events: []llm.Event{
		{Type: llm.EventTypeText, Text: "```json\n{\"should_compile\":true,\"unbounded_input\":false,\"steps\":[{\"kind\":\"deterministic\",\"deterministic\":true},{\"kind\":\"notify\",\"deterministic\":true}]}\n```"},
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
	est := estimateTaskRun("fetch the release notes and summarize them", nil, steps, "anthropic/claude-opus-5-5")
	require.NotNil(t, est.MinCostUSD)
	require.NotNil(t, est.MaxCostUSD)
	// Claude Opus 5.5 costs $4 per million input tokens and $20 per million output tokens.
	assert.InDelta(t, (float64(est.MinInputTokens)*4+float64(est.MinOutputTokens)*20)/1e6, *est.MinCostUSD, 1e-12)
	assert.InDelta(t, (float64(est.MaxInputTokens)*4+float64(est.MaxOutputTokens)*20)/1e6, *est.MaxCostUSD, 1e-12)
	assert.Greater(t, *est.MaxCostUSD, *est.MinCostUSD)

	unpriced := estimateTaskRun("fetch the release notes and summarize them", nil, steps, "openai-codex/gpt-6-sol")
	assert.Nil(t, unpriced.MinCostUSD, "subscription-billed models show tokens only")
}

func TestCompileTaskProviderRunsCLIModelsThroughTheirCLI(t *testing.T) {
	prev := completeCLIText
	t.Cleanup(func() { completeCLIText = prev })
	var gotModel, gotSystem, gotPrompt string
	completeCLIText = func(_ context.Context, model, system, prompt string) (string, error) {
		gotModel, gotSystem, gotPrompt = model, system, prompt
		return `{"should_compile":true,"steps":[{"kind":"deterministic","deterministic":true},{"kind":"notify","deterministic":true}]}`, nil
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
