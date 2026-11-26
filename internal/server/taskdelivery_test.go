package server

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/store"
)

// fakeJobMessenger records the Code Loop message it is handed and answers
// with outcome and err.
type fakeJobMessenger struct {
	outcome string
	err     error

	calls   int
	target  string
	number  int
	message projects.InboxMessage
	options projects.JobDeliveryOptions
}

func (f *fakeJobMessenger) SendJobMessage(_ context.Context, target string, number int, message projects.InboxMessage, options projects.JobDeliveryOptions) (string, core.JobSnapshot, error) {
	f.calls++
	f.target, f.number, f.message, f.options = target, number, message, options
	return f.outcome, core.JobSnapshot{}, f.err
}

func codeLoopServer(t *testing.T, messenger jobMessenger) *Server {
	t.Helper()
	setupServerDataDir(t)
	resetSlogForTest()
	return &Server{
		cfg:          &config.Config{Agents: []config.AgentConfig{{Name: "bot", Projects: []config.ProjectConfig{{Target: " Acme/App "}}}}},
		jobMessenger: messenger,
	}
}

func readSessionFile(t *testing.T, agentName, id string) string {
	t.Helper()
	data, err := os.ReadFile(store.SessionPath(agentName, id))
	require.NoError(t, err)
	return string(data)
}

func TestDeliverToSession_CreatesNamedSessionOnFirstDelivery(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	require.Empty(t, store.FindSessionPath("bot", "reports"))

	require.NoError(t, deliverToSession("bot", "reports", "first report"))
	require.NotEmpty(t, store.FindSessionPath("bot", "reports"))
	assert.Contains(t, readSessionFile(t, "bot", "reports"), "first report")

	// A second delivery appends to the same session.
	require.NoError(t, deliverToSession("bot", "reports", "second report"))
	data := readSessionFile(t, "bot", "reports")
	assert.Contains(t, data, "first report")
	assert.Contains(t, data, "second report")
}

func TestDeliverToSession_ExistingSessionByID(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	sess, err := agent.NewSessionManager().GetOrCreateNamed("bot", "main")
	require.NoError(t, err)

	require.NoError(t, deliverToSession("bot", sess.ID, "status update"))
	assert.Contains(t, readSessionFile(t, "bot", sess.ID), "status update")
}

func TestDeliverToSession_CreateFailure(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	// A file where the sessions directory belongs makes creating the session
	// fail on every OS.
	sessionsDir := filepath.Dir(store.SessionPath("bot", "reports"))
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionsDir), 0o700))
	require.NoError(t, os.WriteFile(sessionsDir, []byte("x"), 0o600))

	err := deliverToSession("bot", "reports", "text")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `resolving task target session "reports"`)
}

func TestDeliverOutput_SessionTargetRecordsDelivery(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	srv := &Server{cfg: &config.Config{}}

	source := domain.OutputSource{Task: "bot/digest", JobID: "job_7"}
	delivery, err := srv.deliverOutput(source, "bot", " session:digest ", "today's digest")
	require.NoError(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, domain.DeliveryDelivered, delivery.Status)
	assert.Equal(t, "delivered to session digest", delivery.Message)
	assert.Equal(t, "session:digest", delivery.Target)
	assert.Contains(t, readSessionFile(t, "bot", "digest"), "today's digest")

	records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "bot", records[0].AgentID)
	assert.Equal(t, UsageSourceTaskDelivery, records[0].Source)
	assert.Equal(t, "bot/digest", records[0].Task)
	assert.False(t, records[0].HasError)
	require.NotNil(t, records[0].Delivery)
	assert.Equal(t, domain.DeliveryDelivered, records[0].Delivery.Status)
}

func TestDeliverToCodeLoop_NoEngine(t *testing.T) {
	srv := codeLoopServer(t, nil)
	delivery := &domain.JobDelivery{}

	err := srv.deliverToCodeLoop(domain.OutputSource{Task: "bot/x"}, config.TaskTarget{Kind: config.TargetCodeLoop, Project: "acme/app", Number: 9}, "text", delivery)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "acme/app#9")
	assert.Contains(t, err.Error(), "not running")
	assert.Equal(t, "acme/app", delivery.Project)
	assert.Equal(t, 9, delivery.Number)
	assert.Equal(t, "bot", delivery.Owner)
}

func TestDeliverToCodeLoop_Errors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"job gone", projects.ErrJobGone, "the Code Loop job acme/app#3 no longer exists"},
		{"wrapped job gone", errors.Join(errors.New("lookup"), projects.ErrJobGone), "the Code Loop job acme/app#3 no longer exists"},
		{"generic", errors.New("engine exploded"), "engine exploded"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeJobMessenger{err: tc.err}
			srv := codeLoopServer(t, fake)
			delivery := &domain.JobDelivery{}

			err := srv.deliverToCodeLoop(domain.OutputSource{Task: "bot/x"}, config.TaskTarget{Project: "acme/app", Number: 3}, "text", delivery)
			require.EqualError(t, err, tc.want)
			assert.Equal(t, 1, fake.calls)
			assert.Empty(t, delivery.Status)
			assert.Equal(t, "bot", delivery.Owner)
		})
	}
}

func TestDeliverToCodeLoop_Outcomes(t *testing.T) {
	cases := []struct {
		outcome    string
		wantStatus string
		wantMsg    string
	}{
		{projects.InboxQueued, domain.DeliveryQueued, "queued for Code Loop acme/app#5 until the job runs again"},
		{projects.InboxResumed, domain.DeliveryDelivered, "resumed Code Loop acme/app#5 with the output"},
		{projects.InboxReopened, domain.DeliveryDelivered, "reopened Code Loop acme/app#5 with the output"},
		{"delivered", domain.DeliveryDelivered, "delivered to Code Loop acme/app#5"},
	}
	for _, tc := range cases {
		t.Run(tc.outcome, func(t *testing.T) {
			fake := &fakeJobMessenger{outcome: tc.outcome}
			srv := codeLoopServer(t, fake)
			delivery := &domain.JobDelivery{}
			target := config.TaskTarget{Kind: config.TargetCodeLoop, Project: "acme/app", Number: 5, Resume: true, Reopen: true}

			err := srv.deliverToCodeLoop(domain.OutputSource{Task: "bot/triage", JobID: "job_1"}, target, "build is red", delivery)
			require.NoError(t, err)
			assert.Equal(t, tc.wantStatus, delivery.Status)
			assert.Equal(t, tc.wantMsg, delivery.Message)
			assert.Equal(t, "bot", delivery.Owner)
			assert.Equal(t, "acme/app", delivery.Project)
			assert.Equal(t, 5, delivery.Number)

			assert.Equal(t, "acme/app", fake.target)
			assert.Equal(t, 5, fake.number)
			assert.Equal(t, "build is red", fake.message.Text)
			assert.Equal(t, "/agents/bot/tasks/triage/runs/job_1", fake.message.SourceURL)
			assert.NotEqual(t, "Aviary", fake.message.Source)
			assert.Equal(t, projects.JobDeliveryOptions{Resume: true, Reopen: true}, fake.options)
		})
	}
}

func TestDeliverToCodeLoop_UntaskedSourceIsLabelledAviary(t *testing.T) {
	fake := &fakeJobMessenger{outcome: "delivered"}
	srv := codeLoopServer(t, fake)

	err := srv.deliverToCodeLoop(domain.OutputSource{}, config.TaskTarget{Project: "other/repo", Number: 1}, "hi", &domain.JobDelivery{})
	require.NoError(t, err)
	assert.Equal(t, "Aviary", fake.message.Source)
	assert.Empty(t, fake.message.SourceURL)
	assert.Equal(t, projects.JobDeliveryOptions{}, fake.options)
}

func TestDeliverOutput_CodeLoopQueuedIsRecorded(t *testing.T) {
	srv := codeLoopServer(t, &fakeJobMessenger{outcome: projects.InboxQueued})

	delivery, err := srv.deliverOutput(domain.OutputSource{Task: "bot/triage", JobID: "job_2"}, "bot", "codeloop:acme/app#4", "ping")
	require.NoError(t, err)
	require.NotNil(t, delivery)
	assert.Equal(t, domain.DeliveryQueued, delivery.Status)
	assert.Equal(t, "bot", delivery.Owner)

	records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.False(t, records[0].HasError)
	require.NotNil(t, records[0].Delivery)
	assert.Equal(t, domain.DeliveryQueued, records[0].Delivery.Status)
}

func TestCodeLoopMessenger(t *testing.T) {
	assert.Nil(t, (&Server{}).codeLoopMessenger())

	fake := &fakeJobMessenger{}
	assert.Same(t, fake, (&Server{jobMessenger: fake}).codeLoopMessenger())

	mgr := &projects.Manager{}
	assert.Same(t, mgr, (&Server{projects: mgr}).codeLoopMessenger())
}

func TestProjectOwner(t *testing.T) {
	assert.Empty(t, (&Server{}).projectOwner("acme/app"), "nil config")

	srv := &Server{cfg: &config.Config{Agents: []config.AgentConfig{
		{Name: "idle"},
		{Name: "bot", Projects: []config.ProjectConfig{{Target: "other/repo"}, {Target: "  ACME/app "}}},
	}}}
	assert.Equal(t, "bot", srv.projectOwner("acme/app"))
	assert.Empty(t, srv.projectOwner("unknown/repo"), "runtime watch")
}

func TestRecordDelivery_AppendsUsageRecord(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	srv := &Server{}
	at := time.Date(2026, 11, 26, 12, 0, 0, 0, time.UTC)

	srv.recordDelivery(domain.OutputSource{Task: "bot/a"}, "bot", &domain.JobDelivery{Target: "session:x", Status: domain.DeliveryDelivered, At: at})
	srv.recordDelivery(domain.OutputSource{Task: "bot/b"}, "bot", &domain.JobDelivery{Target: "channel:y", Status: domain.DeliveryFailed, Message: "boom", At: at})

	records, err := store.ReadJSONL[domain.UsageRecord](store.UsagePath())
	require.NoError(t, err)
	require.Len(t, records, 2)
	assert.True(t, records[0].Timestamp.Equal(at))
	assert.Equal(t, "bot/a", records[0].Task)
	assert.False(t, records[0].HasError)
	assert.Equal(t, "bot/b", records[1].Task)
	assert.True(t, records[1].HasError)
	require.NotNil(t, records[1].Delivery)
	assert.Equal(t, "boom", records[1].Delivery.Message)
}

func TestRecordDelivery_WriteFailureIsTolerated(t *testing.T) {
	setupServerDataDir(t)
	resetSlogForTest()
	// A directory at the usage log path makes the append fail on every OS.
	require.NoError(t, os.MkdirAll(store.UsagePath(), 0o700))

	assert.NotPanics(t, func() {
		(&Server{}).recordDelivery(domain.OutputSource{Task: "bot/a"}, "bot", &domain.JobDelivery{Target: "session:x", Status: domain.DeliveryDelivered})
	})
	info, err := os.Stat(store.UsagePath())
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}
