package agent

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
)

type guardRecordingToolClient struct {
	recordingToolClient
	guarded bool
}

func (g *guardRecordingToolClient) CallToolText(ctx context.Context, name string, args map[string]any) (string, error) {
	g.guarded = WriteGuardFromContext(ctx)
	return g.recordingToolClient.CallToolText(ctx, name, args)
}

func TestAgentRunner_EmitsFileChangeEventsForEdits(t *testing.T) {
	setTestDataDir(t)

	toolClient := &guardRecordingToolClient{recordingToolClient: recordingToolClient{
		tools:   []ToolInfo{{Name: "file_edit", Description: "Edit a file"}},
		results: map[string]string{"file_edit": `{"path": "/repo/main.go", "replacements": 1, "diff": "--- a/repo/main.go\n+++ b/repo/main.go\n@@ -1 +1 @@\n-a\n+b\n"}`},
	}}
	SetToolClientFactory(func(_ context.Context) (ToolClient, error) { return toolClient, nil })
	t.Cleanup(func() { SetToolClientFactory(nil) })

	provider := &sequenceProvider{responses: [][]llm.Event{
		{{Type: llm.EventTypeToolCall, ToolCall: &llm.ToolCall{Name: "file_edit", Arguments: map[string]any{"path": "main.go"}}}, {Type: llm.EventTypeDone}},
		{{Type: llm.EventTypeText, Text: "done"}, {Type: llm.EventTypeDone}},
	}}
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_file_change", Name: "file-change", Model: "test/model"},
		&config.AgentConfig{Name: "file-change", Model: "test/model", Permissions: &config.PermissionsConfig{Preset: config.PermissionsPresetFull}},
		provider,
		nil,
	)

	sess, err := NewSessionManager().GetOrCreateNamed("agent_file_change", "main")
	require.NoError(t, err)

	var changes []FileChangeEvent
	done := make(chan struct{}, 1)
	runner.Prompt(WithSessionID(context.Background(), sess.ID), "edit", func(e StreamEvent) {
		switch e.Type {
		case StreamEventFile:
			changes = append(changes, *e.File)
		case StreamEventDone, StreamEventError, StreamEventStop:
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		require.FailNow(t, "timeout")
	}
	runner.Wait()

	require.Len(t, toolClient.calls, 1)
	require.Len(t, changes, 1)
	assert.Equal(t, "file_edit", changes[0].Tool)
	assert.Equal(t, "/repo/main.go", changes[0].Path)
	assert.Contains(t, changes[0].Diff, "+b")
	assert.True(t, toolClient.guarded, "model-driven tool calls carry the write guard")
}

func TestFileChangeFromToolResult(t *testing.T) {
	change, ok := FileChangeFromToolResult("file_multi_edit", `{"path":"/x","diff":"d"}`)
	require.True(t, ok)
	assert.Equal(t, FileChangeEvent{Tool: "file_multi_edit", Path: "/x", Diff: "d"}, *change)

	_, ok = FileChangeFromToolResult("file_read", `{"path":"/x"}`)
	assert.False(t, ok)
	_, ok = FileChangeFromToolResult("file_write", "not json")
	assert.False(t, ok)
}

func TestFilesystemGrantContext(t *testing.T) {
	_, ok := FilesystemGrantFromContext(context.Background())
	assert.False(t, ok)
	assert.Equal(t, context.Background(), WithFilesystemGrant(context.Background(), ""))
	root, ok := FilesystemGrantFromContext(WithFilesystemGrant(context.Background(), "/clone"))
	assert.True(t, ok)
	assert.Equal(t, "/clone", root)
	assert.False(t, WriteGuardFromContext(context.Background()))
}
