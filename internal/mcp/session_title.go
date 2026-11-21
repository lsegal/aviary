package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

const (
	sessionTitleMaxRunes  = 60
	sessionTitlePromptCap = 2000
	sessionTitleTimeout   = 30 * time.Second
	sessionTitleSystem    = "You name chat conversations. Reply with only a short title of two to six words that says what the user's first message is about. No quotes, no trailing punctuation, no preamble."
)

// sessionTitleGenerator asks a model for a chat title from the chat's first
// prompt. Tests replace it.
var sessionTitleGenerator = generateSessionTitle

// needsAutoTitle reports whether a run's prompt is the first one in a chat
// created by session_create that nobody has named yet. The default "main"
// session and channel, task, and named sessions keep their names.
func needsAutoTitle(sess *domain.Session) bool {
	if sess == nil || !strings.HasPrefix(sess.ID, "sess_") {
		return false
	}
	if sess.Name != "" && sess.Name != sess.ID {
		return false
	}
	if agent.SessionTitle(sess.AgentID, sess.ID) != "" {
		return false
	}
	path := store.FindSessionPath(sess.AgentID, sess.ID)
	if path == "" {
		return false
	}
	records, err := store.ReadJSONL[domain.Message](path)
	if err != nil {
		return false
	}
	for _, rec := range records {
		if rec.Role == domain.MessageRoleUser {
			return false
		}
	}
	return true
}

// autoTitleSession names the chat from its first prompt, falling back to the
// prompt's own opening words when the model gives no usable title.
func autoTitleSession(agentName string, sess *domain.Session, prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), sessionTitleTimeout)
	defer cancel()
	title, err := sessionTitleGenerator(ctx, agentName, truncateRunes(prompt, sessionTitlePromptCap))
	if err != nil {
		slog.Warn("mcp: generating session title failed", "agent", agentName, "session", sess.ID, "err", err)
	}
	title = cleanSessionTitle(title)
	if title == "" {
		title = fallbackSessionTitle(prompt)
	}
	if title == "" || agent.SessionTitle(sess.AgentID, sess.ID) != "" {
		return
	}
	if err := agent.NewSessionManager().Rename(sess.AgentID, sess.ID, title); err != nil {
		slog.Warn("mcp: saving session title failed", "agent", agentName, "session", sess.ID, "err", err)
	}
}

// generateSessionTitle asks the agent's own model for a title.
func generateSessionTitle(ctx context.Context, agentName, prompt string) (string, error) {
	cfg, agentCfg, err := loadCompileAgentConfig(agentName)
	if err != nil {
		return "", err
	}
	model := config.EffectiveAgentModel(*agentCfg, cfg.Models)
	if strings.TrimSpace(model) == "" {
		return "", fmt.Errorf("agent %q has no model configured", agentName)
	}
	provider, err := compileTaskProvider(model)
	if err != nil {
		return "", err
	}
	return completeLLMText(ctx, provider, model, "", sessionTitleSystem, prompt, nil)
}

// cleanSessionTitle reduces a model reply to a one-line title.
func cleanSessionTitle(reply string) string {
	line := ""
	for _, l := range strings.Split(reply, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			line = l
			break
		}
	}
	if i := strings.Index(line, ":"); i >= 0 && strings.EqualFold(strings.TrimSpace(line[:i]), "title") {
		line = line[i+1:]
	}
	line = strings.TrimFunc(line, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("\"'`*#_“”‘’", r)
	})
	line = strings.TrimRight(line, ".!?,;: ")
	return truncateTitle(line)
}

// fallbackSessionTitle titles a chat with its prompt's first line.
func fallbackSessionTitle(prompt string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(prompt), "\n")
	return truncateTitle(strings.Join(strings.Fields(line), " "))
}

// truncateTitle shortens a title at a word boundary, marking the cut.
func truncateTitle(title string) string {
	if len([]rune(title)) <= sessionTitleMaxRunes {
		return title
	}
	cut := string([]rune(title)[:sessionTitleMaxRunes-1])
	if i := strings.LastIndex(cut, " "); i > sessionTitleMaxRunes/2 {
		cut = cut[:i]
	}
	return strings.TrimSpace(cut) + "…"
}

func truncateRunes(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}
