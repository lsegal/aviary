package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/projects"
	"github.com/lsegal/aviary/internal/store"
)

// recordingModel is an OpenAI-compatible chat endpoint that records each
// request's raw body and answers with a fixed reply. While block is set it
// holds every request until the client goes away; a nonzero status fails
// every request with it.
type recordingModel struct {
	mu       sync.Mutex
	bodies   []string
	block    bool
	status   int
	received chan struct{}
}

func (m *recordingModel) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	m.mu.Lock()
	m.bodies = append(m.bodies, string(body))
	block, status := m.block, m.status
	m.mu.Unlock()
	if m.received != nil {
		select {
		case m.received <- struct{}{}:
		default:
		}
	}
	if block {
		<-r.Context().Done()
		return
	}
	if status != 0 {
		http.Error(w, `{"error":{"message":"model rejected the request"}}`, status)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	for _, delta := range []struct {
		delta  map[string]any
		finish any
	}{
		{map[string]any{"role": "assistant", "content": "on it"}, nil},
		{map[string]any{}, "stop"},
	} {
		data, _ := json.Marshal(map[string]any{
			"id": "c", "object": "chat.completion.chunk", "created": 0, "model": "m",
			"choices": []any{map[string]any{"index": 0, "delta": delta.delta, "finish_reason": delta.finish}},
		})
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}

func (m *recordingModel) requests() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.bodies...)
}

// newProjectsAgentServer returns a server whose agents, "bot" and the
// built-in Code Loop agent, talk to a recording model.
func newProjectsAgentServer(t *testing.T) (*Server, *recordingModel) {
	t.Helper()
	setupServerDataDir(t)
	resetSlogForTest()
	model := &recordingModel{received: make(chan struct{}, 1)}
	llmSrv := httptest.NewServer(model)
	t.Cleanup(llmSrv.Close)

	cfg := &config.Config{
		Agents:   []config.AgentConfig{{Name: "bot", Model: "vllm/test"}},
		Projects: config.ProjectsConfig{ChatModel: "vllm/test"},
	}
	srv := New(cfg, "tok")
	srv.agents.Stop()
	srv.agents = agent.NewManager(llm.NewFactory(func(string) (string, error) { return "", nil }).
		WithProviderOptionsResolver(func(string) (llm.ProviderOptions, bool) {
			return llm.ProviderOptions{BaseURI: llmSrv.URL + "/v1"}, true
		}))
	srv.agents.Reconcile(cfg)
	t.Cleanup(func() {
		for _, name := range []string{"bot", config.ProjectsAgentName} {
			if runner, ok := srv.agents.Get(name); ok {
				runner.Wait()
			}
		}
		srv.agents.Stop()
		if srv.sched != nil {
			srv.sched.Stop()
		}
	})
	return srv, model
}

func sessionMessages(t *testing.T, agentID, name string) (domain.Session, []domain.Message) {
	t.Helper()
	sess, err := agent.NewSessionManager().GetOrCreateNamed(agentID, name)
	require.NoError(t, err)
	msgs, err := store.ReadJSONL[domain.Message](store.SessionPath(agentID, sess.ID))
	require.NoError(t, err)
	return *sess, msgs
}

// blockSessions makes agentID's sessions directory unusable by putting a
// file where it belongs.
func blockSessions(t *testing.T, agentID string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(store.AgentDir(agentID), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(store.AgentDir(agentID), "sessions"), nil, 0o644))
}

func TestMediaList(t *testing.T) {
	assert.Nil(t, mediaList(""))
	assert.Equal(t, []string{"https://example.com/a.png"}, mediaList("https://example.com/a.png"))
}

func TestPromptProjectsAgent_SendsPromptToHiddenChatSession(t *testing.T) {
	srv, model := newProjectsAgentServer(t)

	require.NoError(t, srv.promptProjectsAgent(context.Background(), "why did job 7 stop?"))
	runner, ok := srv.agents.Get(config.ProjectsAgentName)
	require.True(t, ok)
	runner.Wait()

	sess, msgs := sessionMessages(t, config.ProjectsAgentName, config.ProjectsChatSession)
	assert.Equal(t, domain.SessionTypeHidden, sess.Type)
	user := lastUserMessage(msgs)
	assert.Equal(t, "why did job 7 stop?", user.Content)
	assert.True(t, hasAssistant(msgs), "the agent answers in the same session")

	reqs := model.requests()
	require.Len(t, reqs, 1)
	assert.Contains(t, reqs[0], "why did job 7 stop?")
	assert.Contains(t, reqs[0], "request_context", "the Code Loop chat context rides along")
}

func TestPromptProjectsAgent_Errors(t *testing.T) {
	t.Run("no agent manager", func(t *testing.T) {
		err := (&Server{}).promptProjectsAgent(context.Background(), "hi")
		assert.EqualError(t, err, "the agent manager is not running")
	})

	t.Run("agent not running", func(t *testing.T) {
		srv, model := newProjectsAgentServer(t)
		srv.agents.Stop()
		srv.agents = agent.NewManager(llm.NewFactory(func(string) (string, error) { return "", nil }))
		err := srv.promptProjectsAgent(context.Background(), "hi")
		assert.EqualError(t, err, `agent "projects" is not running`)
		assert.Empty(t, model.requests())
	})

	t.Run("session unavailable", func(t *testing.T) {
		srv, model := newProjectsAgentServer(t)
		blockSessions(t, config.ProjectsAgentName)
		err := srv.promptProjectsAgent(context.Background(), "hi")
		assert.ErrorContains(t, err, "opening the Code Loop agent's session")
		assert.Empty(t, model.requests())
	})
}

func TestRunProjectsJobTurn_RunsInMainSessionWithMedia(t *testing.T) {
	srv, model := newProjectsAgentServer(t)
	workdir := t.TempDir()
	var mu sync.Mutex
	var events []agent.StreamEventType

	err := srv.runProjectsJobTurn(context.Background(), projects.AviaryAgentRun{
		Agent:    "bot",
		Prompt:   "fix issue 12",
		MediaURL: "https://example.com/shot.png",
		Workdir:  workdir,
		Event: func(e agent.StreamEvent) {
			mu.Lock()
			events = append(events, e.Type)
			mu.Unlock()
		},
	})
	require.NoError(t, err)

	mu.Lock()
	assert.Contains(t, events, agent.StreamEventDone, "the job sees the turn's events")
	mu.Unlock()

	sess, msgs := sessionMessages(t, "bot", "main")
	assert.Equal(t, "main", sess.ID, "the turn runs in the agent's main session")
	user := lastUserMessage(msgs)
	assert.Equal(t, "fix issue 12", user.Content)
	assert.Equal(t, []string{"https://example.com/shot.png"}, user.MediaURLs)

	reqs := model.requests()
	require.Len(t, reqs, 1)
	assert.Contains(t, reqs[0], "fix issue 12")
	assert.Contains(t, reqs[0], "https://example.com/shot.png")
}

func TestRunProjectsJobTurn_Errors(t *testing.T) {
	t.Run("no agent manager", func(t *testing.T) {
		err := (&Server{}).runProjectsJobTurn(context.Background(), projects.AviaryAgentRun{Agent: "bot"})
		assert.EqualError(t, err, "the agent manager is not running")
	})

	t.Run("agent not found", func(t *testing.T) {
		srv, model := newProjectsAgentServer(t)
		err := srv.runProjectsJobTurn(context.Background(), projects.AviaryAgentRun{Agent: "ghost", Prompt: "hi"})
		assert.EqualError(t, err, `agent "ghost" not found`)
		assert.Empty(t, model.requests())
	})

	t.Run("session unavailable", func(t *testing.T) {
		srv, model := newProjectsAgentServer(t)
		blockSessions(t, "bot")
		err := srv.runProjectsJobTurn(context.Background(), projects.AviaryAgentRun{Agent: "bot", Prompt: "hi"})
		assert.ErrorContains(t, err, `opening agent "bot"'s session`)
		assert.Empty(t, model.requests())
	})

	t.Run("run fails", func(t *testing.T) {
		srv, model := newProjectsAgentServer(t)
		model.status = http.StatusBadRequest
		err := srv.runProjectsJobTurn(context.Background(), projects.AviaryAgentRun{Agent: "bot", Prompt: "hi"})
		assert.ErrorContains(t, err, "model rejected the request")
		assert.NotEmpty(t, model.requests())
	})

	t.Run("cancelled", func(t *testing.T) {
		srv, model := newProjectsAgentServer(t)
		model.block = true
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			select {
			case <-model.received:
			case <-time.After(10 * time.Second):
			}
			cancel()
		}()
		err := srv.runProjectsJobTurn(ctx, projects.AviaryAgentRun{Agent: "bot", Prompt: "hi"})
		assert.ErrorIs(t, err, context.Canceled)
	})
}
