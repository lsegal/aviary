package domain

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestConstants_NonEmpty(t *testing.T) {
	for _, state := range []AgentState{AgentStateIdle, AgentStateRunning, AgentStateStopped} {
		assert.NotEqual(t, "", state)

	}
	for _, status := range []JobStatus{JobStatusPending, JobStatusInProgress, JobStatusCompleted, JobStatusFailed, JobStatusCanceled} {
		assert.NotEqual(t, "", status)

	}
	for _, status := range []RunStatus{RunStatusPending, RunStatusInProgress, RunStatusCompleted, RunStatusFailed} {
		assert.NotEqual(t, "", status)

	}
	for _, tr := range []TriggerType{TriggerTypeCron, TriggerTypeWatch} {
		assert.NotEqual(t, "", tr)

	}
	for _, typ := range []ChannelType{ChannelTypeSlack, ChannelTypeDiscord, ChannelTypeSignal} {
		assert.NotEqual(t, "", typ)

	}
	for _, provider := range []Provider{ProviderAnthropic, ProviderOpenAI, ProviderGoogle, ProviderStdio} {
		assert.NotEqual(t, "", provider)

	}
	for _, role := range []MessageRole{MessageRoleUser, MessageRoleAssistant, MessageRoleSystem} {
		assert.NotEqual(t, "", role)

	}
}

func TestStructs_Construct(t *testing.T) {
	now := time.Now()

	a := Agent{ID: "a1", Name: "agent", State: AgentStateIdle, CreatedAt: now, UpdatedAt: now}
	assert.Equal(t, "a1", a.ID)
	assert.Equal(t, "agent", a.Name)
	assert.Equal(t, AgentStateIdle, a.State)

	ch := Channel{ID: "c1", AgentID: "a1", Type: ChannelTypeSlack, ChannelID: "general"}
	assert.Equal(t, ChannelTypeSlack, ch.Type)
	assert.Equal(t, "a1", ch.AgentID)

	mp := MemoryPool{ID: "m1", Name: "shared"}
	assert.Equal(t, "shared", mp.Name)

	me := MemoryEntry{ID: "e1", PoolID: "m1", Role: "user", Content: "hello", Tokens: 1, Timestamp: now}
	assert.Equal(t, "hello", me.Content)
	assert.Equal(t, 1, me.Tokens)

	model := Model{ID: "mod1", Name: "anthropic/claude", Provider: ProviderAnthropic, Auth: "auth:anthropic:default"}
	assert.Equal(t, ProviderAnthropic, model.Provider)

	task := ScheduledTask{ID: "t1", AgentID: "a1", Name: "heartbeat", TriggerType: TriggerTypeCron, Schedule: "@hourly", Prompt: "ping", CreatedAt: now}
	assert.Equal(t, TriggerTypeCron, task.TriggerType)
	assert.Equal(t, "heartbeat", task.Name)

	job := Job{ID: "j1", TaskID: "t1", AgentID: "a1", Status: JobStatusPending, Attempts: 0, MaxRetries: 3, CreatedAt: now, UpdatedAt: now}
	assert.Equal(t, JobStatusPending, job.Status)
	assert.Equal(t, 3, job.MaxRetries)

	run := Run{ID: "r1", JobID: "j1", Status: RunStatusInProgress, StartedAt: now}
	assert.Equal(t, "j1", run.JobID)
	assert.Equal(t, RunStatusInProgress, run.Status)

	s := Session{ID: "s1", AgentID: "a1", CreatedAt: now, UpdatedAt: now}
	assert.Equal(t, "a1", s.AgentID)

	msg := Message{ID: "msg1", SessionID: "s1", Role: MessageRoleUser, Content: "hi", Timestamp: now}
	assert.Equal(t, MessageRoleUser, msg.Role)
	assert.Equal(t, "hi", msg.Content)

}
