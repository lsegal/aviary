package domain

import (
	"testing"
	"time"
)

func TestConstants_NonEmpty(t *testing.T) {
	for _, state := range []AgentState{AgentStateIdle, AgentStateRunning, AgentStateStopped} {
		if state == "" {
			t.Fatal("agent state should not be empty")
		}
	}
	for _, status := range []JobStatus{JobStatusPending, JobStatusInProgress, JobStatusCompleted, JobStatusFailed} {
		if status == "" {
			t.Fatal("job status should not be empty")
		}
	}
	for _, status := range []RunStatus{RunStatusPending, RunStatusInProgress, RunStatusCompleted, RunStatusFailed} {
		if status == "" {
			t.Fatal("run status should not be empty")
		}
	}
	for _, tr := range []TriggerType{TriggerTypeCron, TriggerTypeWatch} {
		if tr == "" {
			t.Fatal("trigger type should not be empty")
		}
	}
	for _, typ := range []ChannelType{ChannelTypeSlack, ChannelTypeDiscord, ChannelTypeSignal} {
		if typ == "" {
			t.Fatal("channel type should not be empty")
		}
	}
	for _, provider := range []Provider{ProviderAnthropic, ProviderOpenAI, ProviderGemini, ProviderStdio} {
		if provider == "" {
			t.Fatal("provider should not be empty")
		}
	}
	for _, role := range []MessageRole{MessageRoleUser, MessageRoleAssistant, MessageRoleSystem} {
		if role == "" {
			t.Fatal("message role should not be empty")
		}
	}
}

func TestStructs_Construct(t *testing.T) {
	now := time.Now()

	a := Agent{ID: "a1", Name: "agent", State: AgentStateIdle, CreatedAt: now, UpdatedAt: now}
	if a.ID != "a1" || a.Name != "agent" || a.State != AgentStateIdle {
		t.Fatalf("unexpected agent: %+v", a)
	}

	ch := Channel{ID: "c1", AgentID: "a1", Type: ChannelTypeSlack, ChannelID: "general"}
	if ch.Type != ChannelTypeSlack || ch.AgentID != "a1" {
		t.Fatalf("unexpected channel: %+v", ch)
	}

	mp := MemoryPool{ID: "m1", Name: "shared"}
	if mp.Name != "shared" {
		t.Fatalf("unexpected pool: %+v", mp)
	}

	me := MemoryEntry{ID: "e1", PoolID: "m1", Role: "user", Content: "hello", Tokens: 1, Timestamp: now}
	if me.Content != "hello" || me.Tokens != 1 {
		t.Fatalf("unexpected memory entry: %+v", me)
	}

	model := Model{ID: "mod1", Name: "anthropic/claude", Provider: ProviderAnthropic, Auth: "auth:anthropic:default"}
	if model.Provider != ProviderAnthropic {
		t.Fatalf("unexpected model: %+v", model)
	}

	task := ScheduledTask{ID: "t1", AgentID: "a1", Name: "heartbeat", TriggerType: TriggerTypeCron, Schedule: "@hourly", Prompt: "ping", CreatedAt: now}
	if task.TriggerType != TriggerTypeCron || task.Name != "heartbeat" {
		t.Fatalf("unexpected task: %+v", task)
	}

	job := Job{ID: "j1", TaskID: "t1", AgentID: "a1", Status: JobStatusPending, Attempts: 0, MaxRetries: 3, CreatedAt: now, UpdatedAt: now}
	if job.Status != JobStatusPending || job.MaxRetries != 3 {
		t.Fatalf("unexpected job: %+v", job)
	}

	run := Run{ID: "r1", JobID: "j1", Status: RunStatusInProgress, StartedAt: now}
	if run.JobID != "j1" || run.Status != RunStatusInProgress {
		t.Fatalf("unexpected run: %+v", run)
	}

	s := Session{ID: "s1", AgentID: "a1", CreatedAt: now, UpdatedAt: now}
	if s.AgentID != "a1" {
		t.Fatalf("unexpected session: %+v", s)
	}

	msg := Message{ID: "msg1", SessionID: "s1", Role: MessageRoleUser, Content: "hi", Timestamp: now}
	if msg.Role != MessageRoleUser || msg.Content != "hi" {
		t.Fatalf("unexpected message: %+v", msg)
	}
}
