package llm

import (
	"context"
	"strings"
)

// EstimateTokens gives a rough token count (~1.3 tokens per word).
func EstimateTokens(text string) int {
	words := len(strings.Fields(text))
	tokens := int(float64(words) * 1.3)
	if tokens < 1 && len(text) > 0 {
		return 1
	}
	return tokens
}

// EstimateRequestTokens returns the estimated token count for the system prompt
// plus all messages in the request.
func EstimateRequestTokens(req Request) int {
	total := 0
	if strings.TrimSpace(req.System) != "" {
		total += EstimateTokens(req.System)
	}
	for _, m := range req.Messages {
		// include role labels as light overhead
		total += EstimateTokens(m.Content) + 2
	}
	return total
}

// CompactToTokenBudget trims oldest messages (preserving system prompt and
// the most recent messages) until the estimated request token count is
// within the provided budget. It never removes the last user message.
func CompactToTokenBudget(req Request, budget int) Request {
	if budget <= 0 {
		return req
	}
	if EstimateRequestTokens(req) <= budget {
		return req
	}

	// Keep system prompt always; drop oldest messages until under budget.
	// Always retain the most recent message (user turn) to avoid empty prompts.
	msgs := make([]Message, len(req.Messages))
	copy(msgs, req.Messages)

	// Remove from the front while over budget and more than 1 message remains.
	for len(msgs) > 1 && EstimateTokens(req.System)+EstimateRequestTokens(Request{Messages: msgs}) > budget {
		// drop the oldest message
		msgs = msgs[1:]
	}

	// If still over budget (e.g., a single message is too large), truncate
	// message content from the front to keep the most recent text.
	if EstimateRequestTokens(Request{System: req.System, Messages: msgs}) > budget {
		// Compute allowed characters based on remaining token budget (conservative).
		sysToks := EstimateTokens(req.System)
		allowedTokens := budget - sysToks
		if allowedTokens < 1 {
			allowedTokens = 1
		}
		allowedWords := int(float64(allowedTokens) / 1.3)
		if allowedWords < 1 {
			allowedWords = 1
		}
		maxChars := allowedWords * 4
		if len(msgs) > 0 {
			// Keep only the last message and truncate its content.
			last := msgs[len(msgs)-1]
			s := last.Content
			if len(s) > maxChars {
				s = s[len(s)-maxChars:]
			}
			last.Content = s
			msgs = []Message{last}
		} else {
			// Fallback: truncate system prompt.
			s := req.System
			if len(s) > maxChars {
				s = s[len(s)-maxChars:]
			}
			req.System = s
		}
	}

	req.Messages = msgs
	return req
}

// ModelInputBudget returns a conservative input token budget for a given model.
// This can be extended to read configurable values later.
func ModelInputBudget(model string) int {
	m := strings.ToLower(model)
	switch {
	case strings.Contains(m, "gpt-5"):
		return 128000
	case strings.Contains(m, "gpt-4o") || strings.Contains(m, "gpt-4"):
		return 32768
	case strings.Contains(m, "gpt-3.5") || strings.Contains(m, "gpt-3"):
		return 4096
	case strings.Contains(m, "claude"):
		return 131072
	case strings.Contains(m, "gemini"):
		return 65536
	default:
		return 16000
	}
}

// SummarizeMessages uses the provided LLM provider to summarize the given
// messages into a concise single-string summary. The returned string may be
// empty on error and an error will be returned.
func SummarizeMessages(ctx context.Context, provider Provider, model string, msgs []Message) (string, error) {
	if provider == nil || len(msgs) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for _, m := range msgs {
		sb.WriteString(string(m.Role))
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}

	prompt := "Summarize the following conversation concisely, preserving key facts and entities. Produce a short bullet list of facts and important context:\n\n" + sb.String()

	req := Request{
		Model:    model,
		Messages: []Message{{Role: RoleUser, Content: prompt}},
		Stream:   true,
	}

	ch, err := provider.Stream(ctx, req)
	if err != nil {
		return "", err
	}

	var out strings.Builder
	for ev := range ch {
		switch ev.Type {
		case EventTypeText:
			out.WriteString(ev.Text)
		case EventTypeError:
			return "", ev.Error
		case EventTypeDone:
			return strings.TrimSpace(out.String()), nil
		}
	}
	return strings.TrimSpace(out.String()), nil
}
