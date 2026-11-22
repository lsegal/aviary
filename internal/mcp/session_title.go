package mcp

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
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

// needsAutoTitle reports whether a run's prompt is for a chat created by
// session_create that nobody has named yet. A chat whose first prompt did not
// name it (an image-only prompt, or a failed save) is named by a later one.
// The default "main" session and channel, task, and named sessions keep their
// names.
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
	return store.FindSessionPath(sess.AgentID, sess.ID) != ""
}

// titlingSessions holds the chats with a title being generated, so prompts
// sent while one is in flight do not generate another.
var titlingSessions sync.Map

// autoTitleSession names the chat with the agent's model's title for its
// first prompt. When the model gives no usable title the chat stays unnamed,
// and needsAutoTitle offers it again on a later prompt.
func autoTitleSession(agentName string, sess *domain.Session, prompt string) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return
	}
	key := sess.AgentID + "/" + sess.ID
	if _, busy := titlingSessions.LoadOrStore(key, struct{}{}); busy {
		return
	}
	defer titlingSessions.Delete(key)
	ctx, cancel := context.WithTimeout(context.Background(), sessionTitleTimeout)
	defer cancel()
	title, err := sessionTitleGenerator(ctx, agentName, truncateRunes(prompt, sessionTitlePromptCap))
	if err != nil {
		slog.Warn("mcp: generating session title failed", "agent", agentName, "session", sess.ID, "err", err)
	}
	title = cleanSessionTitle(title)
	if title == "" || agent.SessionTitle(sess.AgentID, sess.ID) != "" {
		return
	}
	if err := agent.NewSessionManager().Rename(sess.AgentID, sess.ID, title); err != nil {
		slog.Warn("mcp: saving session title failed", "agent", agentName, "session", sess.ID, "err", err)
	}
}

// generateSessionTitle asks the agent's own model for a title, through its
// CLI when the model is CLI-backed.
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
