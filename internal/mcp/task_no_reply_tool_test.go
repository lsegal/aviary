package mcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/store"
)

func TestTaskNoReplyNeedsARun(t *testing.T) {
	d, _ := setupScriptedAgent(t)
	_, err := d.CallTool(context.Background(), TaskNoReplyTool, map[string]any{"reason": "nothing to say"})
	out := requireToolErrorText(t, err)
	assert.Contains(t, out, "only works in a scheduled task or workflow run")
}

// #1030: a run that calls task_no_reply delivers nothing, even though the
// model still writes a final message, and records why in its run.
func TestTaskNoReplySkipsDelivery(t *testing.T) {
	setupScriptedAgentWithTasks := func(t *testing.T) (*scheduler.Scheduler, *scriptedToolModel) {
		_, model := setupScriptedAgent(t)
		mgr := GetDeps().Agents
		cfg := &config.Config{Agents: []config.AgentConfig{{
			Name:        "bot",
			Model:       "vllm/test",
			Permissions: &config.PermissionsConfig{Preset: config.PermissionsPresetFull},
			Tasks: []config.TaskConfig{
				{Name: "be-nice", Trigger: config.TriggerMessage, Channel: "team", Prompt: "Reply only to positive messages.", Target: config.ReplyTarget},
				{Name: "report", Schedule: "0 9 * * *", Prompt: "Report anything new.", Target: "session:ops"},
			},
		}}}
		mgr.Reconcile(cfg)
		s, err := scheduler.New(mgr, 1)
		require.NoError(t, err)
		t.Cleanup(s.Stop)
		s.Reconcile(cfg)
		return s, model
	}
	s, model := setupScriptedAgentWithTasks(t)
	var mu sync.Mutex
	var replies, delivered []string
	s.SetWorkflowHooks(func(_ string, _ domain.TriggerContext, text string) error {
		mu.Lock()
		defer mu.Unlock()
		replies = append(replies, text)
		return nil
	}, nil)
	s.SetTaskOutputDelivery(func(_ domain.OutputSource, _, route, text string) (*domain.JobDelivery, error) {
		mu.Lock()
		defer mu.Unlock()
		delivered = append(delivered, route+": "+text)
		return nil, nil
	})
	model.set(TaskNoReplyTool, map[string]any{"reason": "message was negative"})

	waitDone := func(jobID string) domain.Job {
		t.Helper()
		var job domain.Job
		require.Eventually(t, func() bool {
			got, err := store.ReadJSON[domain.Job](store.FindJobPath(jobID))
			if err != nil {
				return false
			}
			job = got
			return job.Status == domain.JobStatusCompleted || job.Status == domain.JobStatusFailed
		}, 10*time.Second, 20*time.Millisecond)
		return job
	}

	cc := config.ChannelConfig{Type: "signal", ID: "+15550000000", Name: "team"}
	fires := s.FireWorkflows("bot", cc, domain.TriggerContext{
		ChannelType: "signal", Channel: "team", Entry: "C1",
		Message: domain.TriggerMessage{Sender: "Ana", Text: "test you're mean"},
	})
	require.Len(t, fires, 1)
	_, err := s.RunJobNow(fires[0].JobID)
	require.NoError(t, err)
	job := waitDone(fires[0].JobID)
	assert.Equal(t, domain.JobStatusCompleted, job.Status, job.Error)
	require.NotNil(t, job.Delivery, "the skip is recorded on the run")
	assert.Equal(t, domain.DeliverySkipped, job.Delivery.Status)
	assert.Equal(t, "message was negative", job.Delivery.Message)
	assert.Contains(t, job.Prompt, "test you're mean")

	triggered, err := s.Trigger("bot/report")
	require.NoError(t, err)
	job = waitDone(triggered.ID)
	assert.Equal(t, domain.JobStatusCompleted, job.Status, job.Error)
	require.NotNil(t, job.Delivery)
	assert.Equal(t, domain.DeliverySkipped, job.Delivery.Status)

	mu.Lock()
	defer mu.Unlock()
	assert.Empty(t, replies, "a workflow that chose not to reply sends nothing to its conversation")
	assert.Empty(t, delivered, "a task that chose not to reply sends nothing to its target")
	_, pending := agent.TakeSkippedReply(fires[0].JobID)
	assert.False(t, pending, "the choice ends with the run")
}
