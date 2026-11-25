package llm

import (
	"fmt"
	"strings"
	"testing"
)

func TestCompactToTokenBudgetStaysWithinBudget(t *testing.T) {
	texts := map[string]string{
		"short words": strings.Repeat("a ", 40000),
		"mixed words": strings.Repeat("the quick brown fox jumps over a lazy dog ", 6000),
		"long words":  strings.Repeat("internationalization ", 20000),
		"unicode":     strings.Repeat("héllo wörld 日本語 ", 15000),
		"newlines":    strings.Repeat("line one\nline two\n\n", 15000),
	}
	systems := map[string]string{
		"no system":   "",
		"with system": "You are a helpful assistant. Answer concisely and cite sources.",
	}
	budgets := []int{20, 33, 100, 999, 1000, 4097, 8192, 16000, 31999}

	for textName, text := range texts {
		for sysName, system := range systems {
			for _, budget := range budgets {
				for _, history := range []int{0, 3} {
					name := fmt.Sprintf("%s/%s/budget=%d/history=%d", textName, sysName, budget, history)
					t.Run(name, func(t *testing.T) {
						msgs := make([]Message, 0, history+1)
						for i := 0; i < history; i++ {
							msgs = append(msgs, Message{Role: RoleAssistant, Content: strings.Repeat("older ", 50)})
						}
						msgs = append(msgs, Message{Role: RoleUser, Content: text})
						req := Request{System: system, Messages: msgs}

						got := CompactToTokenBudget(req, budget)

						if est := EstimateRequestTokens(got); est > budget {
							t.Fatalf("estimate %d exceeds budget %d", est, budget)
						}
						if got.System != system {
							t.Fatalf("system prompt changed")
						}
						if len(got.Messages) != 1 {
							t.Fatalf("got %d messages; want only the last", len(got.Messages))
						}
						content := got.Messages[0].Content
						if content == "" || !strings.HasSuffix(text, content) {
							t.Fatalf("kept content is not a non-empty suffix of the last message")
						}
					})
				}
			}
		}
	}
}

func TestCompactToTokenBudgetSystemOnly(t *testing.T) {
	req := Request{System: strings.Repeat("a ", 5000)}
	for _, budget := range []int{1, 10, 16000 / 4} {
		got := CompactToTokenBudget(req, budget)
		if est := EstimateRequestTokens(got); est > budget {
			t.Fatalf("budget %d: estimate %d exceeds budget", budget, est)
		}
		if !strings.HasSuffix(req.System, got.System) {
			t.Fatalf("budget %d: kept system prompt is not a suffix", budget)
		}
	}
}
