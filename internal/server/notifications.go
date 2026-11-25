package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/notify"
	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/store"
)

// The live channel's notification events: a new notification to show, and
// a change to the history (read, cleared, or a repeat merged) for the bell
// to reload.
const (
	wsNotification        = "notification"
	wsNotificationsChange = "notifications_changed"
)

// newNotifier creates the server's notification service (#1215), pushing to
// the web UI over /api/ws and, on macOS, through Notification Center when
// no browser is connected.
func (s *Server) newNotifier() *notify.Service {
	n := notify.New()
	n.Publish = func(note notify.Notification) {
		wsBroadcast(wsEvent{Type: wsNotification, Notification: &note})
	}
	n.Changed = func() { wsBroadcast(wsEvent{Type: wsNotificationsChange}) }
	n.Connected = func() bool { return wsClientCount() > 0 }
	n.System = notify.SystemNotifier(func() string {
		if s.cfg == nil {
			return ""
		}
		return config.ServerURL(s.cfg.Server)
	})
	return n
}

// emit sends e through the notification service, when there is one.
func (s *Server) emit(e notify.Event) (notify.Notification, notify.Outcome) {
	if s.notifier == nil {
		return notify.Notification{}, notify.Suppressed
	}
	return s.notifier.Emit(e)
}

// Notifier returns the server's notification service.
func (s *Server) Notifier() *notify.Service { return s.notifier }

// notifyTaskRun is the Notify target's delivery of a successful run's
// output: a notification with the run's summary.
func (s *Server) notifyTaskRun(source domain.OutputSource, target config.TaskTarget, text string, delivery *domain.JobDelivery) {
	if target.FailuresOnly {
		delivery.Status = domain.DeliverySkipped
		delivery.Message = "the run succeeded; this target notifies only on failure"
		return
	}
	title := "Aviary"
	if source.Task != "" {
		title = capitalize(source.Label()) + " finished"
	}
	_, outcome := s.emit(notify.Event{
		Type:     notify.TypeTaskRun,
		Severity: notify.SeveritySuccess,
		Title:    title,
		Body:     notify.Clip(text, notify.MaxBodyBytes),
		Link:     taskRunPath(source),
	})
	delivery.Message = notifyOutcomeMessage(outcome)
}

func notifyOutcomeMessage(outcome notify.Outcome) string {
	switch outcome {
	case notify.Suppressed:
		return "notification turned off in Settings → Notifications"
	case notify.Grouped:
		return "notification grouped with others sent at the same time"
	case notify.Merged:
		return "notification merged with a recent identical one"
	}
	return "sent as a notification"
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// observeRun notifies about a failed run: through its Notify target when it
// has one, otherwise as a failed workflow when it is a workflow run.
// Successful Notify runs are notified as their output is delivered.
func (s *Server) observeRun(job domain.Job, status domain.TaskRunStatus, summary string) {
	if status != domain.TaskRunFailed {
		return
	}
	source := domain.OutputSource{Task: job.TaskID, JobID: job.ID, Workflow: job.TriggerContext != nil}
	target, err := config.ParseTaskTarget(job.OutputChannel)
	event := notify.Event{
		Severity: notify.SeverityError,
		Title:    capitalize(source.Label()) + " failed",
		Body:     notify.Clip(summary, notify.MaxBodyBytes),
		Link:     taskRunPath(source),
	}
	switch {
	case err == nil && target.Kind == config.TargetNotify:
		event.Type = notify.TypeTaskRun
	case source.Workflow:
		event.Type = notify.TypeWorkflowFailed
	default:
		return
	}
	s.emit(event)
}

// observeCodeLoopJob notifies about a Code Loop job that failed, is waiting
// on the owner, or is done.
func (s *Server) observeCodeLoopJob(job core.JobSnapshot, event, text string) {
	e := notify.Event{Body: text, Link: s.codeLoopJobPath(job)}
	title := fmt.Sprintf("%s#%d", firstNonEmpty(job.Repository, job.Target), job.Number)
	if job.Kind == core.JobKindSchedule {
		title = "Scheduled prompt " + strconv.Quote(job.ScheduleID)
	} else if job.Title != "" {
		title += " " + job.Title
	}
	switch event {
	case config.ProjectReportFailed:
		e.Type, e.Severity, e.Title = notify.TypeCodeLoopFailed, notify.SeverityError, "Failed: "+title
	case config.ProjectReportWaiting:
		e.Type, e.Severity, e.Title = notify.TypeCodeLoopBlocked, notify.SeverityWarning, "Waiting: "+title
	case config.ProjectReportMerged:
		e.Type, e.Severity, e.Title = notify.TypeCodeLoopDone, notify.SeveritySuccess, "Done: "+title
	default:
		return
	}
	s.emit(e)
}

// codeLoopJobPath is the web UI path of job's card.
func (s *Server) codeLoopJobPath(job core.JobSnapshot) string {
	board := "/system/watches/" + url.PathEscape(job.Target)
	if owner := s.projectOwner(job.Target); owner != "" {
		board = "/agents/" + url.PathEscape(owner) + "/projects/" + url.PathEscape(job.Target)
	}
	if job.Kind == core.JobKindSchedule {
		return board
	}
	return board + "/jobs/" + strconv.Itoa(job.Number)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// chatReplies notifies when an agent replies in one of the user's chats:
// once its turn ends, or at once for a reply that arrives outside a turn,
// such as a task's output. The web UI skips it while the chat is in view.
type chatReplies struct {
	mu      sync.Mutex
	pending map[string]bool
	emit    func(notify.Event)
	// session loads a session's metadata; tests replace it.
	session func(agentID, sessionID string) (*domain.Session, error)
	// processing reports whether a session's turn is running.
	processing func(agentID, sessionID string) bool
}

func newChatReplies(emit func(notify.Event)) *chatReplies {
	return &chatReplies{
		pending:    map[string]bool{},
		emit:       emit,
		session:    agent.NewSessionManager().Load,
		processing: agent.IsSessionProcessing,
	}
}

// message observes a session message.
func (c *chatReplies) message(agentID, sessionID, role string) {
	if role != string(domain.MessageRoleAssistant) {
		return
	}
	if c.processing(agentID, sessionID) {
		c.mu.Lock()
		c.pending[agentID+"\x00"+sessionID] = true
		c.mu.Unlock()
		return
	}
	c.notify(agentID, sessionID)
}

// turnState observes a session's turn starting or ending.
func (c *chatReplies) turnState(agentID, sessionID string, processing bool) {
	if processing {
		return
	}
	key := agentID + "\x00" + sessionID
	c.mu.Lock()
	replied := c.pending[key]
	delete(c.pending, key)
	c.mu.Unlock()
	if replied {
		c.notify(agentID, sessionID)
	}
}

func (c *chatReplies) notify(agentID, sessionID string) {
	sess, err := c.session(agentID, sessionID)
	if err != nil || sess == nil {
		return
	}
	// Only the user's own chats: channel conversations are read where they
	// happen, and task and hidden sessions are no one's chat.
	if sess.Type != domain.SessionTypeMain && sess.Type != domain.SessionTypeUser {
		return
	}
	name := firstNonEmpty(sess.Title, sess.Name)
	if sess.Type == domain.SessionTypeMain {
		name = "Main Chat"
	}
	c.emit(notify.Event{
		Type:     notify.TypeChatReply,
		Severity: notify.SeverityInfo,
		Title:    agentID + " replied",
		Body:     "in " + name,
		Link:     "/agents/" + url.PathEscape(agentID) + "/chat/" + url.PathEscape(sess.ID),
	})
}

// notificationsHandler lists the history (GET), sends a notification
// (POST, the notify endpoint), or clears the history (DELETE).
func (s *Server) notificationsHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.writeNotifications(w)
	case http.MethodPost:
		var e notify.Event
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPreferencesBytes)).Decode(&e); err != nil || strings.TrimSpace(e.Title) == "" {
			http.Error(w, "a notification needs a title", http.StatusBadRequest)
			return
		}
		n, outcome := s.emit(notify.Sanitize(e))
		writeJSON(w, map[string]any{"outcome": outcome, "notification": n})
	case http.MethodDelete:
		s.notifier.Clear()
		s.writeNotifications(w)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// notificationsReadHandler marks the listed notifications read, or every
// one when none are listed.
func (s *Server) notificationsReadHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		IDs []string `json:"ids"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPreferencesBytes)).Decode(&body); err != nil {
			http.Error(w, "invalid request", http.StatusBadRequest)
			return
		}
	}
	s.notifier.MarkRead(body.IDs)
	s.writeNotifications(w)
}

// notificationsTestHandler sends the settings page's Test notification.
func (s *Server) notificationsTestHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	n, outcome := s.emit(notify.Event{
		Type:  notify.TypeTest,
		Title: "Test notification",
		Body:  "Notifications from Aviary will look like this.",
		Link:  "/system/notifications",
	})
	writeJSON(w, map[string]any{"outcome": outcome, "notification": n})
}

func (s *Server) writeNotifications(w http.ResponseWriter) {
	list, unread := s.notifier.History()
	writeJSON(w, map[string]any{"notifications": list, "unread": unread})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// notificationPreferencesHandler reads (GET), replaces (PUT) or resets
// (DELETE) the notification settings, with the event types and sounds the
// settings page offers.
func notificationPreferencesHandler(w http.ResponseWriter, r *http.Request) {
	var (
		prefs store.NotificationPreferences
		err   error
	)
	switch r.Method {
	case http.MethodGet:
		prefs, err = notify.ReadSettings()
	case http.MethodPut:
		var next store.NotificationPreferences
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxPreferencesBytes)).Decode(&next); err != nil {
			http.Error(w, "invalid notification preferences", http.StatusBadRequest)
			return
		}
		prefs, err = notify.WriteSettings(&next)
	case http.MethodDelete:
		prefs, err = notify.WriteSettings(nil)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err != nil {
		slog.Warn("server: notification preferences", "err", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, prefs)
}
