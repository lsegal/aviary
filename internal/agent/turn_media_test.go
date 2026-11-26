package agent

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
)

// The tool client a prompt opens sees that prompt's media, so tools such as
// project_issue_create can attach it.
func TestPromptMedia_ToolsSeeTheTurnMedia(t *testing.T) {
	setTestDataDir(t)
	var seen []string
	SetToolClientFactory(func(ctx context.Context) (ToolClient, error) {
		seen = TurnMediaFromContext(ctx)
		return &recordingToolClient{}, nil
	})
	t.Cleanup(func() { SetToolClientFactory(nil) })
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_turn_media", Name: "turn-media"},
		&config.AgentConfig{Name: "turn-media", Model: "test/model"},
		&captureProvider{}, nil,
	)
	media := []string{"data:image/png;base64,Zmlyc3Q=", llm.FileURL("/tmp/clip.mp4")}
	promptAndWait(context.Background(), t, runner, "file this", media, RunOverrides{})
	assert.Equal(t, media, seen)
}

func TestMediaFile(t *testing.T) {
	setTestDataDir(t)

	path, err := MediaFile(llm.FileURL("/tmp/shot.png"))
	require.NoError(t, err)
	assert.Equal(t, filepath.FromSlash("/tmp/shot.png"), path)

	path, err = MediaFile("/tmp/plain.png")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/plain.png", path)

	// A data URL is saved under the turn media directory, named by its
	// content with an extension for its type, once.
	path, err = MediaFile("data:image/jpeg;base64,c2Vjb25k")
	require.NoError(t, err)
	assert.Equal(t, TurnMediaDir(), filepath.Dir(path))
	assert.True(t, strings.HasSuffix(path, "_attachment.jpg"), path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "second", string(data))
	again, err := MediaFile("data:image/jpeg;base64,c2Vjb25k")
	require.NoError(t, err)
	assert.Equal(t, path, again)

	named, err := MediaFile("data:image/png;name=login%20error.png;base64,Zmlyc3Q=")
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(named, "_login error.png"), named)

	for _, bad := range []string{"https://example.com/a.png", "data:image/png,raw", "data:image/png;base64,!!!", " "} {
		_, err := MediaFile(bad)
		assert.Error(t, err, bad)
	}
}

func TestWithTurnMediaKeepsACopy(t *testing.T) {
	media := []string{"a"}
	ctx := WithTurnMedia(context.Background(), media)
	media[0] = "b"
	assert.Equal(t, []string{"a"}, TurnMediaFromContext(ctx))
	assert.Nil(t, TurnMediaFromContext(WithTurnMedia(context.Background(), nil)))
}
