package mcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

// tccModel is a CLI-backed model, so the compiler's provider runs through
// completeCLIText, which the tests script.
const tccModel = "claude/opus"

// tccStageOf names the compiler stage a CLI completion belongs to from its
// system prompt.
func tccStageOf(system string) string {
	switch {
	case system == taskDeterminismSystemPrompt:
		return "analysis"
	case strings.HasPrefix(system, "Generate a single-file Lua script"):
		return "generation"
	case strings.HasPrefix(system, "You are Aviary's task compiler validator"):
		return "validation"
	}
	return "unknown"
}

// tccReply is a scripted answer for one compiler stage.
type tccReply struct {
	text string
	err  error
}

// tccSetup configures the config and data dirs with cfg and replaces the
// CLI completion with one answering each stage from replies. It returns the
// stages called, in order.
func tccSetup(t *testing.T, cfg *config.Config, replies map[string]tccReply) *[]string {
	t.Helper()
	setupSchedulerDeps(t, cfg)
	prev := completeCLIText
	t.Cleanup(func() { completeCLIText = prev })
	var mu sync.Mutex
	var stages []string
	completeCLIText = func(_ context.Context, model, system, _ string) (string, error) {
		stage := tccStageOf(system)
		mu.Lock()
		stages = append(stages, stage)
		mu.Unlock()
		assert.Equal(t, tccModel, model)
		reply, ok := replies[stage]
		if !ok {
			return "", errors.New("unexpected stage " + stage)
		}
		return reply.text, reply.err
	}
	return &stages
}

func tccBotConfig(model string) *config.Config {
	return &config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: model}}}
}

const (
	tccDeterministicAnalysis = `{"reason":"fixed URL check","steps":[{"kind":"deterministic","deterministic":true,"tool":"web_get","description":"fetch"},{"kind":"notify","deterministic":true,"description":"print"}]}`
	tccNoReasonAnalysis      = `{"steps":[{"kind":"notify","deterministic":true,"description":"print"}]}`
)

// tccCompile runs the real compiler for bot with a tracker and returns both.
func tccCompile(t *testing.T, prompt string) (*compiledTaskResult, *taskCompileTracker, error) {
	t.Helper()
	tracker := newTaskCompileTracker("bot", "check", "prompt", prompt, "", "test", false)
	ctx := withTaskCompileTracker(context.Background(), tracker)
	require.Same(t, tracker, taskCompileTrackerFromContext(ctx))
	result, err := tryCompileTaskPrompt(ctx, "bot", prompt, "", false)
	return result, tracker, err
}

func tccStageNames(tracker *taskCompileTracker) []string {
	names := make([]string, 0, len(tracker.record.Stages))
	for _, stage := range tracker.record.Stages {
		names = append(names, stage.Name+":"+stage.Status)
	}
	return names
}

func TestTaskCompileCovCompilesValidatedScriptAndPersistsRecord(t *testing.T) {
	stages := tccSetup(t, tccBotConfig(tccModel), map[string]tccReply{
		"analysis":   {text: "Here you go:\n" + tccDeterministicAnalysis},
		"generation": {text: "```lua\nprint(\"up\")\n```"},
		"validation": {text: `{"valid":true,"reason":"  performs the check  "}`},
	})

	result, tracker, err := tccCompile(t, "  check https://example.com and print its status  ")
	require.NoError(t, err)
	assert.Equal(t, "script", result.Type)
	assert.Equal(t, `print("up")`, result.Script)
	assert.True(t, result.Validated)
	assert.Equal(t, "performs the check", result.Reason)
	assert.Equal(t, "check https://example.com and print its status", result.Prompt)
	require.Len(t, result.Steps, 2)
	assert.Equal(t, []string{"analysis", "generation", "validation"}, *stages)

	rec := tracker.record
	assert.Equal(t, domain.TaskCompileStatusSucceeded, rec.Status)
	assert.Equal(t, "script", rec.ResultTaskType)
	assert.True(t, rec.Validated)
	assert.Equal(t, `print("up")`, rec.Script)
	assert.Equal(t, 2, rec.DeterministicSteps)
	require.Len(t, rec.Steps, 2)
	assert.Equal(t, "web_get", rec.Steps[0].Tool)
	assert.Equal(t, []string{"analysis:succeeded", "generation:succeeded", "validation:succeeded"}, tccStageNames(tracker))
	for _, stage := range rec.Stages {
		assert.NotNil(t, stage.FinishedAt)
		assert.Empty(t, stage.Error)
	}
	assert.Contains(t, rec.Stages[1].UserPrompt, "Compiler analysis JSON:")
	assert.Contains(t, rec.Stages[2].UserPrompt, "Generated Lua script:\nprint(\"up\")")

	require.NoError(t, tracker.persist())
	saved, err := store.ReadJSON[domain.TaskCompile](store.TaskCompilePath("bot", rec.ID))
	require.NoError(t, err)
	assert.Equal(t, domain.TaskCompileStatusSucceeded, saved.Status)
	assert.Equal(t, `print("up")`, saved.Script)
	assert.Len(t, saved.Stages, 3)
}

func TestTaskCompileCovDeclinedCompilesStayPromptTasks(t *testing.T) {
	tests := []struct {
		name       string
		replies    map[string]tccReply
		wantReason string
		wantStatus domain.TaskCompileStatus
		wantScript string
		wantStages []string
	}{
		{
			name:       "nondeterministic analysis",
			replies:    map[string]tccReply{"analysis": {text: `{"reason":"summarizing needs judgment","steps":[{"kind":"agent_run","deterministic":false}]}`}},
			wantReason: "summarizing needs judgment",
			wantStatus: domain.TaskCompileStatusSkipped,
			wantStages: []string{"analysis"},
		},
		{
			name:       "discovery without a reason",
			replies:    map[string]tccReply{"analysis": {text: `{"needs_discovery":true,"steps":[{"kind":"notify","deterministic":true}]}`}},
			wantReason: "task could not be validated as deterministic enough for script compilation",
			wantStatus: domain.TaskCompileStatusSkipped,
			wantStages: []string{"analysis"},
		},
		{
			name: "empty generation",
			replies: map[string]tccReply{
				"analysis":   {text: tccNoReasonAnalysis},
				"generation": {text: "   "},
			},
			wantReason: "compiler did not produce a script",
			wantStatus: domain.TaskCompileStatusSkipped,
			wantStages: []string{"analysis", "generation"},
		},
		{
			name: "invalid lua",
			replies: map[string]tccReply{
				"analysis":   {text: tccDeterministicAnalysis},
				"generation": {text: "print((("},
			},
			wantReason: "compiler produced invalid Lua",
			wantStatus: domain.TaskCompileStatusFailed,
			wantScript: "print(((",
			wantStages: []string{"analysis", "generation"},
		},
		{
			name: "script that always errors",
			replies: map[string]tccReply{
				"analysis":   {text: tccDeterministicAnalysis},
				"generation": {text: `error("cannot do this")`},
			},
			wantReason: "only raises an error",
			wantStatus: domain.TaskCompileStatusSkipped,
			wantScript: `error("cannot do this")`,
			wantStages: []string{"analysis", "generation"},
		},
		{
			name: "validator rejects with a reason",
			replies: map[string]tccReply{
				"analysis":   {text: tccDeterministicAnalysis},
				"generation": {text: `print("up")`},
				"validation": {text: `{"valid":false,"reason":"skips the status check"}`},
			},
			wantReason: "skips the status check",
			wantStatus: domain.TaskCompileStatusSkipped,
			wantScript: `print("up")`,
			wantStages: []string{"analysis", "generation", "validation"},
		},
		{
			name: "validator accepts but its reason declines",
			replies: map[string]tccReply{
				"analysis":   {text: tccDeterministicAnalysis},
				"generation": {text: `print("up")`},
				"validation": {text: `{"valid":true,"reason":"This should stay a prompt task."}`},
			},
			wantReason: "This should stay a prompt task.",
			wantStatus: domain.TaskCompileStatusSkipped,
			wantScript: `print("up")`,
			wantStages: []string{"analysis", "generation", "validation"},
		},
		{
			name: "validator rejects without any reason",
			replies: map[string]tccReply{
				"analysis":   {text: tccNoReasonAnalysis},
				"generation": {text: `print("hi")`},
				"validation": {text: `{"valid":false}`},
			},
			wantReason: "generated script did not validate against the original prompt",
			wantStatus: domain.TaskCompileStatusSkipped,
			wantScript: `print("hi")`,
			wantStages: []string{"analysis", "generation", "validation"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stages := tccSetup(t, tccBotConfig(tccModel), tt.replies)
			result, tracker, err := tccCompile(t, "say hi")
			require.NoError(t, err)
			assert.Equal(t, "prompt", result.Type)
			assert.Empty(t, result.Script)
			assert.False(t, result.Validated)
			assert.Contains(t, result.Reason, tt.wantReason)
			assert.Equal(t, tt.wantStages, *stages)
			assert.Equal(t, tt.wantStatus, tracker.record.Status)
			assert.Equal(t, "prompt", tracker.record.ResultTaskType)
			assert.Equal(t, result.Reason, tracker.record.Reason)
			assert.Equal(t, tt.wantScript, tracker.record.Script)
		})
	}
}

func TestTaskCompileCovStageFailuresReturnErrorsAndMarkRecordFailed(t *testing.T) {
	cliErr := errors.New("cli exited 1")
	tests := []struct {
		name       string
		replies    map[string]tccReply
		wantErr    string
		wantScript string
		wantStages []string
	}{
		{
			name:       "analysis error",
			replies:    map[string]tccReply{"analysis": {err: cliErr}},
			wantErr:    "cli exited 1",
			wantStages: []string{"analysis:failed"},
		},
		{
			name:       "analysis not JSON",
			replies:    map[string]tccReply{"analysis": {text: "I cannot answer"}},
			wantErr:    "parsing compiler analysis",
			wantStages: []string{"analysis:succeeded"},
		},
		{
			name: "generation error",
			replies: map[string]tccReply{
				"analysis":   {text: tccDeterministicAnalysis},
				"generation": {err: cliErr},
			},
			wantErr:    "cli exited 1",
			wantStages: []string{"analysis:succeeded", "generation:failed"},
		},
		{
			name: "validation error",
			replies: map[string]tccReply{
				"analysis":   {text: tccDeterministicAnalysis},
				"generation": {text: `print("up")`},
				"validation": {err: cliErr},
			},
			wantErr:    "cli exited 1",
			wantScript: `print("up")`,
			wantStages: []string{"analysis:succeeded", "generation:succeeded", "validation:failed"},
		},
		{
			name: "validation not JSON",
			replies: map[string]tccReply{
				"analysis":   {text: tccDeterministicAnalysis},
				"generation": {text: `print("up")`},
				"validation": {text: "looks fine to me"},
			},
			wantErr:    "parsing compiler validation",
			wantScript: `print("up")`,
			wantStages: []string{"analysis:succeeded", "generation:succeeded", "validation:succeeded"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tccSetup(t, tccBotConfig(tccModel), tt.replies)
			result, tracker, err := tccCompile(t, "check the site")
			require.Error(t, err)
			assert.Nil(t, result)
			assert.Contains(t, err.Error(), tt.wantErr)
			assert.Equal(t, domain.TaskCompileStatusFailed, tracker.record.Status)
			assert.Contains(t, tracker.record.Reason, tt.wantErr)
			assert.Equal(t, tt.wantScript, tracker.record.Script)
			assert.Equal(t, tt.wantStages, tccStageNames(tracker))
			last := tracker.record.Stages[len(tracker.record.Stages)-1]
			if strings.HasSuffix(tt.wantStages[len(tt.wantStages)-1], ":failed") {
				assert.Equal(t, "cli exited 1", last.Error)
			}
		})
	}
}

func TestTaskCompileCovSetupFailuresAreRecordedWithoutModelCalls(t *testing.T) {
	t.Run("agent not in config", func(t *testing.T) {
		stages := tccSetup(t, &config.Config{Agents: []config.AgentConfig{{Name: "other", Model: tccModel}}}, nil)
		_, tracker, err := tccCompile(t, "say hi")
		require.ErrorContains(t, err, `agent "bot" not found in config`)
		assert.Equal(t, domain.TaskCompileStatusFailed, tracker.record.Status)
		assert.Equal(t, err.Error(), tracker.record.Reason)
		assert.Empty(t, *stages)
	})
	t.Run("agent without a model", func(t *testing.T) {
		stages := tccSetup(t, tccBotConfig(""), nil)
		_, tracker, err := tccCompile(t, "say hi")
		require.ErrorContains(t, err, `agent "bot" has no model configured`)
		assert.Equal(t, domain.TaskCompileStatusFailed, tracker.record.Status)
		assert.Equal(t, err.Error(), tracker.record.Reason)
		assert.Empty(t, *stages)
	})
	t.Run("unreadable config", func(t *testing.T) {
		tccSetup(t, tccBotConfig(tccModel), nil)
		require.NoError(t, os.WriteFile(config.DefaultPath(), []byte("agents: [\n  - name: bot\n    model: : :"), 0o600))
		_, tracker, err := tccCompile(t, "say hi")
		require.Error(t, err)
		assert.Equal(t, domain.TaskCompileStatusFailed, tracker.record.Status)
		assert.Equal(t, err.Error(), tracker.record.Reason)
	})
	t.Run("model with no provider", func(t *testing.T) {
		stages := tccSetup(t, tccBotConfig("nomodelslash"), nil)
		_, tracker, err := tccCompile(t, "say hi")
		require.ErrorContains(t, err, "invalid model")
		assert.Equal(t, domain.TaskCompileStatusFailed, tracker.record.Status)
		assert.Equal(t, err.Error(), tracker.record.Reason)
		assert.Empty(t, *stages)
	})
	t.Run("tool catalog unavailable", func(t *testing.T) {
		stages := tccSetup(t, tccBotConfig(tccModel), nil)
		tracker := newTaskCompileTracker("bot", "check", "prompt", "say hi", "", "test", false)
		ctx, cancel := context.WithCancel(withTaskCompileTracker(context.Background(), tracker))
		cancel()
		_, err := tryCompileTaskPrompt(ctx, "bot", "say hi", "", false)
		require.Error(t, err)
		assert.Equal(t, domain.TaskCompileStatusFailed, tracker.record.Status)
		assert.Equal(t, err.Error(), tracker.record.Reason)
		assert.Empty(t, *stages)
	})
}

func TestTaskCompileCovRunTaskAnalyzerUsesAgentModelAndRecordsEstimatedUsage(t *testing.T) {
	var gotUser string
	tccSetup(t, tccBotConfig(tccModel), map[string]tccReply{"analysis": {text: tccDeterministicAnalysis}})
	prevCLI := completeCLIText
	completeCLIText = func(ctx context.Context, model, system, user string) (string, error) {
		gotUser = user
		return prevCLI(ctx, model, system, user)
	}
	stubCache := taskAnalyses
	t.Cleanup(func() { taskAnalyses = stubCache })
	taskAnalyses = newTaskAnalysisCache(taskAnalysisCachePath)

	prevAnalyzer := taskAnalyzerFunc
	t.Cleanup(func() { taskAnalyzerFunc = prevAnalyzer })
	taskAnalyzerFunc = nil

	analyzed, err := resolveTaskAnalyzer()(context.Background(), "bot", "check https://example.com", "")
	require.NoError(t, err)
	assert.Equal(t, tccModel, analyzed.Model)
	assert.True(t, analyzed.Analysis.allowsCompile())
	assert.Equal(t, 2, analyzed.Analysis.DeterministicStepCount)
	assert.NotEmpty(t, analyzed.Tools, "the analyzer sees the agent's tool catalog")
	assert.Contains(t, gotUser, "run_discovery=false")
	// The CLI reports no usage, so the analyzer estimates it.
	assert.Positive(t, analyzed.Usage.InputTokens)
	assert.Positive(t, analyzed.Usage.OutputTokens)

	// task_analyze runs it, records the estimate under the task editor, and
	// caches the result.
	result, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "check https://example.com"})
	require.NoError(t, err)
	assert.Equal(t, taskVerdictDeterministic, result.Verdict)
	assert.Equal(t, tccModel, result.Model)
	usage, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
	require.NoError(t, err)
	require.Len(t, usage, 1)
	assert.Equal(t, taskEditorUsageSessionID, usage[0].SessionID)
	assert.Equal(t, domain.UsageSourceTaskEditor, usage[0].Source)
	assert.Equal(t, tccModel, usage[0].Model)
	assert.False(t, usage[0].HasError)
}

func TestTaskCompileCovRunTaskAnalyzerFailures(t *testing.T) {
	prevAnalyzer := taskAnalyzerFunc
	t.Cleanup(func() { taskAnalyzerFunc = prevAnalyzer })
	taskAnalyzerFunc = nil

	t.Run("unknown agent", func(t *testing.T) {
		tccSetup(t, tccBotConfig(tccModel), nil)
		_, err := runTaskAnalyzer(context.Background(), "ghost", "say hi", "")
		require.ErrorContains(t, err, `agent "ghost" not found in config`)
	})
	t.Run("no model", func(t *testing.T) {
		tccSetup(t, tccBotConfig(""), nil)
		_, err := runTaskAnalyzer(context.Background(), "bot", "say hi", "")
		require.ErrorContains(t, err, "has no model configured")
	})
	t.Run("tool catalog unavailable", func(t *testing.T) {
		stages := tccSetup(t, tccBotConfig(tccModel), nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := runTaskAnalyzer(ctx, "bot", "say hi", "")
		require.Error(t, err)
		assert.Empty(t, *stages)
	})
	t.Run("invalid model", func(t *testing.T) {
		tccSetup(t, tccBotConfig("nomodelslash"), nil)
		_, err := runTaskAnalyzer(context.Background(), "bot", "say hi", "")
		require.ErrorContains(t, err, "invalid model")
	})
	t.Run("analysis error is recorded as a failed editor usage", func(t *testing.T) {
		tccSetup(t, tccBotConfig(tccModel), map[string]tccReply{"analysis": {err: errors.New("cli exited 2")}})
		stubCache := taskAnalyses
		t.Cleanup(func() { taskAnalyses = stubCache })
		taskAnalyses = newTaskAnalysisCache(taskAnalysisCachePath)

		_, err := analyzeTaskForEditor(context.Background(), taskAnalyzeArgs{Agent: "bot", Content: "say hi"})
		require.ErrorContains(t, err, "cli exited 2")
		usage, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
		require.NoError(t, err)
		require.Len(t, usage, 1)
		assert.True(t, usage[0].HasError)
		assert.Equal(t, "bot", usage[0].AgentID)
		assert.Empty(t, usage[0].Model)
	})
}

func TestTaskCompileCovCompileTaskProviderResolvesAPICredentials(t *testing.T) {
	setupSchedulerDeps(t, botConfig())
	deps := GetDeps()

	creds, err := auth.NewFileStore(filepath.Join(t.TempDir(), "credentials.json"))
	require.NoError(t, err)
	SetDeps(&Deps{Agents: deps.Agents, Scheduler: deps.Scheduler, Auth: creds})

	_, err = compileTaskProvider("openai/gpt-test")
	require.ErrorContains(t, err, "openai auth")

	require.NoError(t, creds.Set("openai:default", "sk-test"))
	provider, err := compileTaskProvider("openai/gpt-test")
	require.NoError(t, err)
	assert.NotNil(t, provider)
	_, isCLI := provider.(cliTextProvider)
	assert.False(t, isCLI)
}

func TestTaskCompileCovCompileTaskProviderFailsOnCorruptCredentials(t *testing.T) {
	setupSchedulerDeps(t, botConfig())
	path := filepath.Join(store.SubDir(store.DirAuth), "credentials.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("{not json"), 0o600))

	provider, err := compileTaskProvider("openai/gpt-test")
	require.Error(t, err)
	assert.Nil(t, provider)
}

// tccStreamErrProvider fails to start a stream.
type tccStreamErrProvider struct{ err error }

func (p tccStreamErrProvider) Stream(context.Context, llm.Request) (<-chan llm.Event, error) {
	return nil, p.err
}

func TestTaskCompileCovCompleteLLMTextTracksStagesAndUsage(t *testing.T) {
	t.Run("usage is accumulated", func(t *testing.T) {
		provider := &recordingProvider{events: []llm.Event{
			{Type: llm.EventTypeText, Text: " part one"},
			{Type: llm.EventTypeUsage, Usage: &llm.Usage{InputTokens: 10, OutputTokens: 3, CacheReadTokens: 2, CacheWriteTokens: 1}},
			{Type: llm.EventTypeText, Text: " part two "},
			{Type: llm.EventTypeUsage, Usage: &llm.Usage{InputTokens: 5, OutputTokens: 4}},
		}}
		usage := &llm.Usage{}
		out, err := completeLLMText(withLLMUsage(context.Background(), usage), provider, "test/m", "analysis", "sys", "user", nil)
		require.NoError(t, err)
		assert.Equal(t, "part one part two", out)
		assert.Equal(t, llm.Usage{InputTokens: 15, OutputTokens: 7, CacheReadTokens: 2, CacheWriteTokens: 1}, *usage)
		require.Len(t, provider.requests, 1)
		assert.Equal(t, "sys", provider.requests[0].System)
		assert.True(t, provider.requests[0].Stream)
	})
	t.Run("stream start failure", func(t *testing.T) {
		tracker := newTaskCompileTracker("bot", "", "", "", "", "", false)
		ctx := withTaskCompileTracker(context.Background(), tracker)
		_, err := completeLLMText(ctx, tccStreamErrProvider{err: errors.New("dial failed")}, "test/m", "generation", "sys", "user", nil)
		require.ErrorContains(t, err, "dial failed")
		require.Len(t, tracker.record.Stages, 1)
		assert.Equal(t, "failed", tracker.record.Stages[0].Status)
		assert.Equal(t, "dial failed", tracker.record.Stages[0].Error)
		assert.Equal(t, "sys", tracker.record.Stages[0].SystemPrompt)
	})
	t.Run("error event keeps partial text", func(t *testing.T) {
		tracker := newTaskCompileTracker("bot", "", "", "", "", "", false)
		ctx := withTaskCompileTracker(context.Background(), tracker)
		provider := &recordingProvider{events: []llm.Event{
			{Type: llm.EventTypeText, Text: "partial"},
			{Type: llm.EventTypeError, Error: errors.New("overloaded")},
		}}
		_, err := completeLLMText(ctx, provider, "test/m", "validation", "sys", "user", nil)
		require.ErrorContains(t, err, "overloaded")
		require.Len(t, tracker.record.Stages, 1)
		assert.Equal(t, "failed", tracker.record.Stages[0].Status)
		assert.Equal(t, "partial", tracker.record.Stages[0].Response)
		assert.Equal(t, "overloaded", tracker.record.Stages[0].Error)
	})
	t.Run("native tool call is rejected", func(t *testing.T) {
		tracker := newTaskCompileTracker("bot", "", "", "", "", "", false)
		ctx := withTaskCompileTracker(context.Background(), tracker)
		provider := &recordingProvider{events: []llm.Event{{Type: llm.EventTypeToolCall}}}
		_, err := completeLLMText(ctx, provider, "test/m", "generation", "sys", "user", nil)
		require.EqualError(t, err, "unexpected native tool call during generation stage")
		require.Len(t, tracker.record.Stages, 1)
		assert.Equal(t, "failed", tracker.record.Stages[0].Status)

		// An unnamed stage is not tracked, and is called the compiler stage.
		_, err = completeLLMText(ctx, provider, "test/m", "  ", "sys", "user", nil)
		require.EqualError(t, err, "unexpected native tool call during compiler stage")
		assert.Len(t, tracker.record.Stages, 1)
	})
}

func TestTaskCompileCovTrackerHelpers(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, ctx, withTaskCompileTracker(ctx, nil), "a nil tracker leaves the context alone")
	assert.Nil(t, taskCompileTrackerFromContext(ctx))

	tracker := newTaskCompileTracker("bot", " name ", " prompt ", " do it ", " session:x ", " manual ", true)
	assert.Equal(t, "name", tracker.record.TaskName)
	assert.Equal(t, "do it", tracker.record.Prompt)
	assert.Equal(t, "session:x", tracker.record.Target)
	assert.Equal(t, domain.TaskCompileStatusSkipped, tracker.record.Status)

	idx := tracker.addStage("analysis", "sys", "user")
	before := tracker.record.Stages[idx]
	tracker.finishStage(-1, "succeeded", "x", nil)
	tracker.finishStage(5, "succeeded", "x", nil)
	assert.Equal(t, before, tracker.record.Stages[idx], "out-of-range stages are ignored")

	time.Sleep(time.Millisecond)
	tracker.finishStage(idx, " succeeded ", " answer ", nil)
	stage := tracker.record.Stages[idx]
	assert.Equal(t, "succeeded", stage.Status)
	assert.Equal(t, "answer", stage.Response)
	require.NotNil(t, stage.FinishedAt)
	assert.False(t, stage.FinishedAt.Before(stage.StartedAt))
}

func TestTaskCompileCovExtractCodeFenceAndMustJSON(t *testing.T) {
	assert.Equal(t, "print(1)\nprint(2)", extractCodeFence("```lua\nprint(1)\nprint(2)\n```"))
	assert.Equal(t, "```print(1)```", extractCodeFence("  ```print(1)```  "), "a fence too short to hold code is kept")
	assert.Equal(t, "print(1)", extractCodeFence("\n print(1) \n"))

	assert.Equal(t, "{}", mustJSON(make(chan int)), "unencodable values render as an empty object")
	assert.Equal(t, "{\n  \"a\": 1\n}", mustJSON(map[string]int{"a": 1}))
}

func TestTaskCompileCovSessionSendUsesSessionContext(t *testing.T) {
	setupSchedulerDeps(t, botConfig())
	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "main")
	require.NoError(t, err)
	var delivered []string
	var mu sync.Mutex
	agent.RegisterSessionDelivery("bot", sess.ID, "signal", "+15550001111", func(text string) {
		mu.Lock()
		defer mu.Unlock()
		delivered = append(delivered, text)
	})

	d := NewDispatcher("https://localhost:16677", "")
	inSession := agent.WithSessionAgentID(agent.WithSessionID(context.Background(), sess.ID), "bot")

	out, err := d.CallTool(inSession, "session_send", map[string]any{"content": "from context"})
	require.NoError(t, err)
	assert.Contains(t, out, `sent message to session "`+sess.ID+`" for agent "bot"`)
	mu.Lock()
	assert.Equal(t, []string{"from context"}, delivered)
	mu.Unlock()

	_, err = d.CallTool(inSession, "session_send", map[string]any{"content": "   "})
	out = requireToolErrorText(t, err)
	assert.Contains(t, out, "content is required")

	agentOnly := agent.WithSessionAgentID(context.Background(), "bot")
	_, err = d.CallTool(agentOnly, "session_send", map[string]any{"content": "hi"})
	out = requireToolErrorText(t, err)
	assert.Contains(t, out, "session_id is required")

	_, err = d.CallTool(context.Background(), "session_send", map[string]any{"agent": "bot", "session_id": "missing-session", "content": "hi"})
	out = requireToolErrorText(t, err)
	assert.NotContains(t, out, "sent message")
	mu.Lock()
	assert.Len(t, delivered, 1, "nothing is delivered for an unknown session")
	mu.Unlock()
}
