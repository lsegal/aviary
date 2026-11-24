package mcp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

func workflowCompileConfig() *config.Config {
	return &config.Config{Agents: []config.AgentConfig{{
		Name: "bot", Model: "test/x",
		Channels: []config.ChannelConfig{{Type: "slack", ID: "team", Name: "Team"}},
	}}}
}

func TestWorkflowPrecompileSavesAndCompiles(t *testing.T) {
	// A workflow script may use text a project task's would be rejected for.
	const script = `print("{repo}: " .. trigger.message.text)`
	prompts, _ := setupProjectTaskCompile(t, workflowCompileConfig(), compiledScript(script))
	d := NewDispatcher("https://localhost:16677", "")
	ctx := context.Background()

	// Precompile defaults on, as scheduler.precompute_tasks does, and saving
	// compiles the workflow with its triggering message described.
	out, err := d.CallTool(ctx, "task_schedule", map[string]any{
		"agent": "bot", "name": "echo", "content": "Echo the message back",
		"trigger": "message", "channel": "team", "target": "reply",
	})
	require.NoError(t, err)
	assert.Contains(t, out, "Precompiling")
	backgroundCompiles.Wait()
	task := loadedTask(t, "echo")
	require.NotNil(t, task.Precompile)
	assert.True(t, *task.Precompile)
	assert.Equal(t, "prompt", task.Type, "a precompiled workflow keeps its prompt to fall back to")
	assert.Equal(t, "Echo the message back", task.Prompt)
	assert.Equal(t, script, task.Compiled)
	assert.Equal(t, []string{workflowTaskCompilePrompt("Echo the message back")}, *prompts)
	assert.Contains(t, (*prompts)[0], "trigger.message")

	// Turning it off drops the script; turning it back on compiles again.
	_, err = d.CallTool(ctx, "task_update", map[string]any{"name": "bot/echo", "precompile": false})
	require.NoError(t, err)
	off := loadedTask(t, "echo")
	assert.False(t, *off.Precompile)
	assert.Empty(t, off.Compiled)
	_, err = d.CallTool(ctx, "task_update", map[string]any{"name": "bot/echo", "precompile": true})
	require.NoError(t, err)
	backgroundCompiles.Wait()
	assert.Equal(t, script, loadedTask(t, "echo").Compiled)
	assert.Len(t, *prompts, 2)

	// precompile=false at creation stays a prompt workflow.
	_, err = d.CallTool(ctx, "task_schedule", map[string]any{
		"agent": "bot", "name": "chat", "content": "Answer the question",
		"trigger": "message", "channel": "team", "precompile": false,
	})
	require.NoError(t, err)
	backgroundCompiles.Wait()
	assert.False(t, *loadedTask(t, "chat").Precompile)
	assert.Len(t, *prompts, 2)

	// A script workflow is already a script.
	_, err = d.CallTool(ctx, "task_schedule", map[string]any{
		"agent": "bot", "name": "lua", "type": "script", "content": `print("hi")`,
		"trigger": "message", "channel": "team",
	})
	require.NoError(t, err)
	assert.Nil(t, loadedTask(t, "lua").Precompile)
	out, err = d.CallTool(ctx, "task_update", map[string]any{"name": "bot/lua", "precompile": true})
	require.NoError(t, err)
	assert.Contains(t, out, "script tasks have no prompt to compile")
}

func TestWorkflowScriptFailureIsFlagged(t *testing.T) {
	on := true
	cfg := workflowCompileConfig()
	cfg.Agents[0].Tasks = []config.TaskConfig{{
		Name: "echo", Type: "prompt", Trigger: config.TriggerMessage, Channel: "Team", Prompt: "Echo the message back", Precompile: &on,
	}}
	prompts, _ := setupProjectTaskCompile(t, cfg, compiledScript(`print(trigger.message.text)`))

	got, err := EnsureTaskCompiled(context.Background(), "bot", "echo", "run")
	require.NoError(t, err)
	assert.Equal(t, `print(trigger.message.text)`, got)

	// A script that fails its first run is set aside, with a note saying so,
	// and the workflow runs on the model without compiling the same prompt
	// again.
	require.NoError(t, RecordTaskScriptResult("bot", "echo", errors.New("attempt to index a nil value")))
	task := loadedTask(t, "echo")
	assert.Empty(t, task.Compiled)
	assert.Contains(t, task.PrecompileNote, "failed on its first run")
	got, err = EnsureTaskCompiled(context.Background(), "bot", "echo", "run")
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.Len(t, *prompts, 1)
}

func TestTaskAnalyzeWorkflowAddsTriggerContext(t *testing.T) {
	const prompt = "fetch the release notes and summarize them"
	analyses := map[string]*compileTaskAnalysis{
		prompt:                                 sampleTaskAnalyses[prompt],
		workflowTaskCompilePrompt(prompt):      sampleTaskAnalyses[prompt],
		workflowTaskCompilePrompt("Say hello"): sampleTaskAnalyses["Say hello"],
	}
	calls := stubTaskAnalyzer(t, analyses)
	ctx := context.Background()

	task, err := analyzeTaskForEditor(ctx, taskAnalyzeArgs{Agent: "bot", Content: prompt, Model: "anthropic/claude-opus-5-5"})
	require.NoError(t, err)
	workflow, err := analyzeTaskForEditor(ctx, taskAnalyzeArgs{Agent: "bot", Content: prompt, Model: "anthropic/claude-opus-5-5", Trigger: "message"})
	require.NoError(t, err)
	assert.Equal(t, 2, *calls, "a workflow is analyzed with its triggering message described")
	assert.Equal(t, task.Verdict, workflow.Verdict)

	// Each run's prompt carries the matched message and the ones before it.
	minContext, maxContext := workflowContextTokens()
	assert.Equal(t, minContext, workflow.Estimate.MinContextTokens)
	assert.Equal(t, maxContext, workflow.Estimate.MaxContextTokens)
	assert.Zero(t, task.Estimate.MaxContextTokens)
	assert.Greater(t, workflow.Estimate.MinTokens, task.Estimate.MinTokens)
	assert.Greater(t, workflow.Estimate.MaxTokens, task.Estimate.MaxTokens)
	require.NotNil(t, workflow.Estimate.MaxCostUSD)
	assert.Greater(t, *workflow.Estimate.MaxCostUSD, *task.Estimate.MaxCostUSD)

	// A precompiled workflow runs no model, and reports what it would cost
	// without precompile beside that.
	det, err := analyzeTaskForEditor(ctx, taskAnalyzeArgs{Agent: "bot", Content: "Say hello", Trigger: "message", Precompile: true})
	require.NoError(t, err)
	assert.True(t, det.WillPrecompile)
	assert.Zero(t, det.Estimate.MaxTokens)
	require.NotNil(t, det.WithoutPrecompile)
	assert.Equal(t, minContext, det.WithoutPrecompile.MinContextTokens)
	assert.Positive(t, det.WithoutPrecompile.MaxTokens)

	_, err = analyzeTaskForEditor(ctx, taskAnalyzeArgs{Agent: "bot", Content: prompt, Type: "project", Project: "acme/app", Trigger: "message"})
	require.ErrorContains(t, err, "workflows run prompt or script content")
}

func TestScheduledPromptTaskPrecompilesBesidePrompt(t *testing.T) {
	cfg := workflowCompileConfig()
	cfg.Agents[0].Tasks = []config.TaskConfig{{Name: "digest", Type: "prompt", Every: "1h", Prompt: "Post the digest"}}
	prompts, _ := setupProjectTaskCompile(t, cfg, compiledScript(`print("digest")`))
	d := NewDispatcher("https://localhost:16677", "")
	ctx := context.Background()

	// task_update accepts precompile on a scheduled prompt task, which keeps
	// its prompt and compiles it as is, without workflow or project context.
	_, err := d.CallTool(ctx, "task_update", map[string]any{"name": "bot/digest", "precompile": true})
	require.NoError(t, err)
	backgroundCompiles.Wait()
	task := loadedTask(t, "digest")
	assert.Equal(t, "prompt", task.Type)
	assert.Equal(t, "Post the digest", task.Prompt)
	assert.Equal(t, `print("digest")`, task.Compiled)
	assert.Equal(t, []string{"Post the digest"}, *prompts)

	// Converting it recompiles beside the prompt rather than replacing it.
	_, err = d.CallTool(ctx, "config_task_convert_to_script", map[string]any{"agent": "bot", "task": "digest"})
	require.NoError(t, err)
	backgroundCompiles.Wait()
	task = loadedTask(t, "digest")
	assert.Equal(t, "prompt", task.Type)
	assert.Equal(t, `print("digest")`, task.Compiled)
	assert.Len(t, *prompts, 2)
}

func TestConfigSaveAcceptsPrecompiledPromptTasks(t *testing.T) {
	setupProjectTaskCompile(t, workflowCompileConfig(), compiledScript(`print("x")`))
	d := NewDispatcher("https://localhost:16677", "")
	agentJSON := `{"name":"bot","model":"anthropic/claude-sonnet-4-5","channels":[{"type":"slack","id":"team","name":"Team"}],"tasks":[%s]}`
	save := func(task string) (string, error) {
		return d.CallTool(context.Background(), "config_save", map[string]any{"config": `{"agents":[` + fmt.Sprintf(agentJSON, task) + `]}`})
	}

	for _, task := range []string{
		`{"name":"be-nice","type":"prompt","trigger":"message","channel":"Team","match":"test *","target":"reply","prompt":"Be nice","precompile":true}`,
		`{"name":"digest","type":"prompt","every":"1h","prompt":"Post the digest","precompile":true}`,
		`{"name":"triage","type":"project","every":"1h","project":"o/r","prompt":"Triage","precompile":true}`,
	} {
		out, err := save(task)
		require.NoError(t, err, task)
		assert.Contains(t, out, "saved")
	}

	// The dispatcher returns a tool's error as its text.
	out, err := save(`{"name":"lua","type":"script","every":"1h","prompt":"print(1)","precompile":true}`)
	require.NoError(t, err)
	assert.Contains(t, out, "script tasks have no prompt to compile")
	assert.NotContains(t, out, "saved")
}
