package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// UsageSession is the name, kind and owner a usage record snapshots for the
// session that spent its tokens. Kind is one of the domain.UsageKind values.
type UsageSession struct {
	Name  string
	Kind  string
	Owner string
}

type usageSessionKey struct{}

// WithUsageSession names the session a prompt's usage is recorded under, for
// a caller that knows better than the session itself what the run is, such
// as a Code Loop job naming the issue it works on.
func WithUsageSession(ctx context.Context, s UsageSession) context.Context {
	if s == (UsageSession{}) {
		return ctx
	}
	return context.WithValue(ctx, usageSessionKey{}, s)
}

// usageSessionFromContext returns the session set by WithUsageSession.
func usageSessionFromContext(ctx context.Context) (UsageSession, bool) {
	s, ok := ctx.Value(usageSessionKey{}).(UsageSession)
	return s, ok
}

// SessionDisplayName is the name a session shows in Chats: its title, "Main
// Chat", a task session's task name, or its name unless that is just its ID.
// It matches the web UI's chatDisplayName.
func SessionDisplayName(sess *domain.Session, title string) string {
	switch {
	case strings.TrimSpace(title) != "":
		return strings.TrimSpace(title)
	case sess.Type == domain.SessionTypeMain:
		return "Main Chat"
	case sess.Type == domain.SessionTypeTask && sess.Name != "":
		return sess.Name
	case sess.Name != "" && sess.Name != sess.ID:
		return sess.Name
	}
	return "New chat"
}

// LiveSessionName is the display name of agentID's session sessionID, or ""
// when the session does not exist.
func LiveSessionName(agentID, sessionID string) string {
	if title := SessionTitle(agentID, sessionID); title != "" {
		return title
	}
	p := store.FindSessionPath(agentID, sessionID)
	if p == "" {
		return ""
	}
	sess, _, err := sessionAt(agentID, sessionID, p)
	if err != nil {
		return ""
	}
	return SessionDisplayName(sess, "")
}

// snapshotUsageSession fills u's session snapshot: from the context when the
// caller named the run, from its task or workflow, or else from the chat's
// current display name.
func snapshotUsageSession(ctx context.Context, u *domain.UsageRecord) {
	if s, ok := usageSessionFromContext(ctx); ok {
		u.SessionName, u.SessionKind, u.SessionOwner = s.Name, s.Kind, s.Owner
		return
	}
	if u.Task != "" {
		u.SessionName = u.Task[strings.Index(u.Task, "/")+1:]
		u.SessionKind, u.SessionOwner = domain.UsageKindTask, u.Task
		if u.Channel != "" {
			u.SessionKind, u.SessionOwner = domain.UsageKindWorkflow, u.Channel
		}
		return
	}
	u.SessionKind = domain.UsageKindChat
	u.SessionName = LiveSessionName(u.AgentID, u.SessionID)
	if u.Channel != "" {
		u.SessionOwner = u.Channel
	}
}

// recordSessionDeleted writes the tombstone of agentID's session sessionID,
// which was called name, so Usage can mark its records deleted.
func recordSessionDeleted(agentID, sessionID, name string) {
	rec := domain.DeletedSession{AgentID: agentID, SessionID: sessionID, Name: name, DeletedAt: time.Now()}
	if err := store.AppendJSONL(store.DeletedSessionsPath(), rec); err != nil {
		slog.Warn("agent: failed to record deleted session", "agent", agentID, "session", sessionID, "err", err)
	}
}

// RecordSessionsDeleted writes tombstones for sessions removed without
// SessionManager.Delete, such as when an agent's whole data directory goes.
// Call it before the sessions are removed, so their names can still be read.
func RecordSessionsDeleted(agentID string, sessions []*domain.Session) {
	for _, sess := range sessions {
		recordSessionDeleted(agentID, sess.ID, SessionDisplayName(sess, SessionTitle(agentID, sess.ID)))
	}
}
