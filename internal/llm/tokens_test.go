package llm

import (
	"context"
	"strings"
	"testing"
	"time"
)

// fakeProvider implements Provider for testing SummarizeMessages.
type fakeProvider struct {
	chunks []string
}

func (f *fakeProvider) Stream(ctx context.Context, _ Request) (<-chan Event, error) {
	ch := make(chan Event, 8)
	go func() {
		defer close(ch)
		for _, c := range f.chunks {
			select {
			case <-ctx.Done():
				ch <- Event{Type: EventTypeError, Error: ctx.Err()}
				return
			default:
			}
			ch <- Event{Type: EventTypeText, Text: c}
			time.Sleep(5 * time.Millisecond)
		}
		ch <- Event{Type: EventTypeDone}
	}()
	return ch, nil
}

func TestEstimateTokensBasic(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Fatalf("expected 0 tokens for empty string")
	}
	if EstimateTokens("hello world") < 1 {
		t.Fatalf("expected >=1 tokens for 'hello world'")
	}
}

func TestEstimateRequestTokensAndModelBudget(t *testing.T) {
	req := Request{System: "system prompt", Messages: []Message{{Role: RoleUser, Content: "hi"}, {Role: RoleAssistant, Content: "ok"}}}
	tok := EstimateRequestTokens(req)
	if tok <= 0 {
		t.Fatalf("expected positive token estimate; got %d", tok)
	}
	b := ModelInputBudget("openai/gpt-3.5-turbo")
	if b < 1000 {
		t.Fatalf("unexpected small budget for gpt-3.5: %d", b)
	}
	if got := ModelInputBudget("openai-codex/gpt-5.4"); got < 100000 {
		t.Fatalf("unexpected small budget for gpt-5.4: %d", got)
	}
}

func TestCompactToTokenBudget(t *testing.T) {
	// Create many short messages to exceed a small budget.
	msgs := make([]Message, 0, 50)
	for i := 0; i < 30; i++ {
		msgs = append(msgs, Message{Role: RoleUser, Content: strings.Repeat("word ", 20)})
	}
	req := Request{System: "sys", Messages: msgs}
	// very small budget to force compaction
	compacted := CompactToTokenBudget(req, 10)
	if len(compacted.Messages) < 1 {
		t.Fatalf("expected at least 1 message after compacting")
	}
	if EstimateRequestTokens(compacted) > 10 {
		t.Fatalf("compacted request still exceeds budget: %d > 10", EstimateRequestTokens(compacted))
	}
}

func TestCompactToTokenBudget_SingleLargeMessage(t *testing.T) {
	req := Request{
		System:   "sys",
		Messages: []Message{{Role: RoleUser, Content: strings.Repeat("word ", 2000)}},
	}
	compacted := CompactToTokenBudget(req, 32)
	if len(compacted.Messages) != 1 {
		t.Fatalf("expected single message after compacting, got %d", len(compacted.Messages))
	}
	if EstimateRequestTokens(compacted) > 32 {
		t.Fatalf("compacted request still exceeds budget: %d > 32", EstimateRequestTokens(compacted))
	}
}

func TestSummarizeMessages(t *testing.T) {
	provider := &fakeProvider{chunks: []string{"First line.\n", "Second line."}}
	msgs := []Message{{Role: RoleUser, Content: "Hello"}, {Role: RoleAssistant, Content: "Answer"}}
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := SummarizeMessages(ctx, provider, "openai/gpt-4o", msgs)
	if err != nil {
		t.Fatalf("SummarizeMessages error: %v", err)
	}
	if !strings.Contains(out, "First line") || !strings.Contains(out, "Second line") {
		t.Fatalf("unexpected summary output: %q", out)
	}
}
