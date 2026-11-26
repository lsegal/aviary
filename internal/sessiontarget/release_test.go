package sessiontarget

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// taskWithData gives bot's task taskName a session holding two messages, one
// attaching a saved image, and one run record; it returns the image's path
// and the run record's path.
func taskWithData(t *testing.T, taskName string) (string, string) {
	t.Helper()
	sess, err := agent.EnsureTaskSession("bot", taskName)
	require.NoError(t, err)
	media := filepath.Join(store.IncomingMediaDir("slack"), taskName+".png")
	require.NoError(t, os.MkdirAll(filepath.Dir(media), 0o700))
	require.NoError(t, os.WriteFile(media, []byte("png"), 0o600))
	require.NoError(t, agent.AppendMediaMessageToSession("bot", sess.ID, domain.MessageRoleUser, "look", []string{media, "data:image/png;base64,AA=="}))
	require.NoError(t, agent.AppendMessageToSession("bot", sess.ID, domain.MessageRoleAssistant, "nice"))
	run := store.JobPath("bot", "job_"+taskName)
	require.NoError(t, store.WriteJSON(run, domain.Job{ID: "job_" + taskName, TaskID: "bot/" + taskName, AgentID: "bot"}))
	other := store.JobPath("bot", "job_other")
	require.NoError(t, store.WriteJSON(other, domain.Job{ID: "job_other", TaskID: "bot/other", AgentID: "bot"}))
	return media, run
}

func sessionTitles(t *testing.T) map[string]domain.SessionType {
	t.Helper()
	all, err := agent.NewSessionManager().List("bot")
	require.NoError(t, err)
	out := map[string]domain.SessionType{}
	for _, s := range all {
		out[s.Title] = s.Type
	}
	return out
}

func TestReleaseTaskDeleteData(t *testing.T) {
	setupStore(t)
	media, run := taskWithData(t, "be-nice")

	assert.Equal(t, DataCounts{Sessions: 1, Messages: 2, Runs: 1, Files: 1}, TaskData("bot", "be-nice"))

	require.NoError(t, ReleaseTask("bot", "be-nice", true))
	assert.Empty(t, store.FindSessionPath("bot", "be-nice"))
	assert.NoFileExists(t, media)
	assert.NoFileExists(t, run)
	assert.FileExists(t, store.JobPath("bot", "job_other"), "other tasks' runs stay")
	assert.NotContains(t, sessionTitles(t), "be-nice (archived)")
	assert.Equal(t, DataCounts{}, TaskData("bot", "be-nice"))
}

func TestReleaseTaskKeepData(t *testing.T) {
	setupStore(t)
	media, run := taskWithData(t, "be-nice")

	require.NoError(t, ReleaseTask("bot", "be-nice", false))
	assert.Empty(t, store.FindSessionPath("bot", "be-nice"), "the task session moved")
	assert.Equal(t, domain.SessionTypeUser, sessionTitles(t)["be-nice (archived)"], "kept as a regular chat")
	assert.FileExists(t, media)
	assert.FileExists(t, run)
}

func TestReleaseTaskWithoutSession(t *testing.T) {
	setupStore(t)
	require.NoError(t, ReleaseTask("bot", "never-ran", true))
	require.NoError(t, ReleaseTask("bot", "never-ran", false))
}

func TestReleaseChannelDeleteRemovesMedia(t *testing.T) {
	setupStore(t)
	a := config.AgentConfig{Name: "bot", Channels: []config.ChannelConfig{{Type: "slack", ID: "ws"}}}
	conv := conversation(t, "bot", "slack:C9", store.SessionChannel{Type: "slack", ConfiguredID: "ws", ID: "C9"})
	media := filepath.Join(store.IncomingMediaDir("slack"), "c9.png")
	require.NoError(t, os.MkdirAll(filepath.Dir(media), 0o700))
	require.NoError(t, os.WriteFile(media, []byte("png"), 0o600))
	require.NoError(t, agent.AppendMediaMessageToSession("bot", conv.ID, domain.MessageRoleUser, "pic", []string{media}))

	counts, err := ChannelData(a, 0)
	require.NoError(t, err)
	assert.Equal(t, DataCounts{Sessions: 1, Messages: 2, Files: 1}, counts)

	require.NoError(t, ReleaseChannel(a, 0, true))
	assert.Empty(t, store.FindSessionPath("bot", conv.ID))
	assert.NoFileExists(t, media)
}

func TestReleaseAgent(t *testing.T) {
	setupStore(t)
	t.Setenv("AVIARY_CONFIG_BASE_DIR", store.DataDir())
	a := config.AgentConfig{Name: "bot"}
	taskWithData(t, "be-nice")
	require.NoError(t, os.WriteFile(filepath.Join(store.AgentDir("bot"), "MEMORY.md"), []byte("remember"), 0o600))

	counts := AgentData(a)
	assert.Equal(t, 1, counts.Sessions)
	assert.Equal(t, 2, counts.Messages)
	assert.Equal(t, 2, counts.Runs)
	assert.GreaterOrEqual(t, counts.Files, 1, "MEMORY.md counts as a file")

	// Unchecked only deregisters: the data directory stays intact.
	require.NoError(t, ReleaseAgent(a, false))
	assert.FileExists(t, filepath.Join(store.AgentDir("bot"), "MEMORY.md"))
	assert.NotEmpty(t, store.FindSessionPath("bot", "be-nice"))

	require.NoError(t, ReleaseAgent(a, true))
	assert.NoDirExists(t, store.AgentDir("bot"))
}

func TestFindAndReleaseOrphans(t *testing.T) {
	setupStore(t)
	t.Setenv("AVIARY_CONFIG_BASE_DIR", store.DataDir())
	cfg := &config.Config{Agents: []config.AgentConfig{{
		Name:     "bot",
		Tasks:    []config.TaskConfig{{Name: "standup"}},
		Channels: []config.ChannelConfig{{Type: "slack", ID: "ws"}},
	}}}
	// be-nice's workflow and the signal channel were deleted before deletes
	// released their sessions.
	_, err := agent.EnsureTaskSession("bot", "be-nice")
	require.NoError(t, err)
	_, err = agent.EnsureTaskSession("bot", "standup")
	require.NoError(t, err)
	conversation(t, "bot", "slack:C9", store.SessionChannel{Type: "slack", ConfiguredID: "ws", ID: "C9"})
	gone, err := agent.NewSessionManager().GetOrCreateNamedTyped("bot", "channel:signal:+1", domain.SessionTypeChannel)
	require.NoError(t, err)
	_, err = agent.NewSessionManager().Create("bot")
	require.NoError(t, err)

	orphans, err := FindOrphans(cfg)
	require.NoError(t, err)
	var ids []string
	for _, o := range orphans {
		ids = append(ids, o.Session)
	}
	assert.ElementsMatch(t, []string{"be-nice", gone.ID}, ids)

	require.NoError(t, ReleaseOrphans(orphans, false))
	titles := sessionTitles(t)
	assert.Equal(t, domain.SessionTypeUser, titles["be-nice (archived)"])
	assert.Empty(t, store.FindSessionPath("bot", "be-nice"))
	assert.NotEmpty(t, store.FindSessionPath("bot", "standup"), "an owned task session stays")
	left, err := FindOrphans(cfg)
	require.NoError(t, err)
	assert.Empty(t, left)
}
