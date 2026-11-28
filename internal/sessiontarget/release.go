package sessiontarget

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// DataCounts is what deleting an object's data removes: its sessions, their
// messages, its run records, and the files it owns.
type DataCounts struct {
	Sessions int `json:"sessions"`
	Messages int `json:"messages"`
	Runs     int `json:"runs"`
	Files    int `json:"files"`
}

// sessionContent counts a session's messages and lists the saved media files
// they attach.
func sessionContent(agentID, sessionID string) (int, []string) {
	p := store.FindSessionPath(agentID, sessionID)
	if p == "" {
		return 0, nil
	}
	lines, err := store.ReadJSONL[domain.Message](p)
	if err != nil {
		return 0, nil
	}
	messages := 0
	var files []string
	for _, line := range lines {
		if line.Role == "" {
			continue
		}
		messages++
		for _, media := range line.MediaURLs {
			if path := mediaFile(media); path != "" {
				files = append(files, path)
			}
		}
	}
	return messages, files
}

// mediaFile is the saved file under the media directory that a message's
// media URL names, or "" for inline data and anything outside it.
func mediaFile(media string) string {
	media = strings.TrimSpace(media)
	if u, err := url.Parse(media); err == nil && u.Scheme == "file" {
		media = u.Path
	}
	if !filepath.IsAbs(media) {
		return ""
	}
	root := filepath.Clean(store.MediaDir())
	rel, err := filepath.Rel(root, filepath.Clean(media))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return ""
	}
	if info, err := os.Stat(media); err != nil || !info.Mode().IsRegular() {
		return ""
	}
	return filepath.Clean(media)
}

// countSessions adds sessions, their messages and their media files to c.
func countSessions(c *DataCounts, agentID string, sessions []*domain.Session) {
	for _, sess := range sessions {
		messages, files := sessionContent(agentID, sess.ID)
		c.Sessions++
		c.Messages += messages
		c.Files += len(files)
	}
}

// deleteSession removes a session, its messages and its saved media files.
func deleteSession(agentID string, sess *domain.Session) error {
	_, files := sessionContent(agentID, sess.ID)
	if err := agent.NewSessionManager().Delete(agentID, sess.ID); err != nil {
		return err
	}
	agent.ClearSessionDelivery(agentID, sess.ID)
	for _, f := range files {
		if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("deleting %s: %w", f, err)
		}
	}
	return nil
}

// archiveSession keeps a session as a regular chat titled title.
func archiveSession(agentID string, sess *domain.Session, title string) error {
	_, err := agent.NewSessionManager().Archive(agentID, sess.ID, title+" (archived)")
	return err
}

// taskSession is the session agentID's task taskName shares across its runs,
// or nil when it has none.
func taskSession(agentID, taskName string) *domain.Session {
	if strings.TrimSpace(taskName) == "" || store.FindSessionPath(agentID, taskName) == "" {
		return nil
	}
	sess, err := agent.NewSessionManager().Load(agentID, taskName)
	if err != nil || sess.Type != domain.SessionTypeTask {
		return nil
	}
	return sess
}

// taskRuns lists the job record files of agentID's task taskName.
func taskRuns(agentID, taskName string) []string {
	dir := filepath.Join(store.AgentDir(agentID), "jobs")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	id := agentID + "/" + taskName
	var out []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		p := filepath.Join(dir, e.Name())
		job, err := store.ReadJSON[domain.Job](p)
		if err == nil && job.TaskID == id {
			out = append(out, p)
		}
	}
	return out
}

// TaskData counts the data deleting agentID's task taskName with its data
// removes: its session and its runs.
func TaskData(agentID, taskName string) DataCounts {
	var c DataCounts
	if sess := taskSession(agentID, taskName); sess != nil {
		countSessions(&c, agentID, []*domain.Session{sess})
	}
	c.Runs = len(taskRuns(agentID, taskName))
	return c
}

// ReleaseTask disposes of the data of agentID's task taskName, which is being
// deleted. deleteData removes its session, with its messages and media, and
// its run records; otherwise its session is kept as a regular chat titled
// after the task and its run records stay on disk.
func ReleaseTask(agentID, taskName string, deleteData bool) error {
	if sess := taskSession(agentID, taskName); sess != nil {
		var err error
		if deleteData {
			err = deleteSession(agentID, sess)
		} else {
			err = archiveSession(agentID, sess, taskName)
		}
		if err != nil {
			return err
		}
	}
	if !deleteData {
		return nil
	}
	for _, p := range taskRuns(agentID, taskName) {
		if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("deleting run %s: %w", p, err)
		}
	}
	return nil
}

// ChannelData counts the data removing a.Channels[idx] with its data
// removes: its own session and the conversations it received.
func ChannelData(a config.AgentConfig, idx int) (DataCounts, error) {
	var c DataCounts
	sessions, err := ChannelSessions(a, idx)
	if err != nil {
		return c, err
	}
	countSessions(&c, a.Name, sessions)
	return c, nil
}

// agentDataDirs are the directories that hold a's data: its data directory
// and, when it lives elsewhere, its task files directory.
func agentDataDirs(a config.AgentConfig) []string {
	dirs := []string{filepath.Clean(store.AgentDir(a.Name))}
	if tasks := filepath.Dir(filepath.Clean(config.AgentTasksDir(a))); tasks != dirs[0] {
		dirs = append(dirs, tasks)
	}
	return dirs
}

// AgentData counts the data deleting agent a with its data removes: every
// session, message and run in its data directory, and its other files, such
// as AGENTS.md, MEMORY.md, memory/, notes/ and task files.
func AgentData(a config.AgentConfig) DataCounts {
	var c DataCounts
	if sessions, err := agent.NewSessionManager().List(a.Name); err == nil {
		countSessions(&c, a.Name, sessions)
	}
	for _, dir := range agentDataDirs(a) {
		sessionsDir := filepath.Join(dir, "sessions")
		jobsDir := filepath.Join(dir, "jobs")
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			switch filepath.Dir(p) {
			case sessionsDir:
			case jobsDir:
				c.Runs++
			default:
				c.Files++
			}
			return nil
		})
	}
	return c
}

// ReleaseAgent disposes of the data of agent a, which is being deregistered.
// deleteData removes its data directory and task files with everything in
// them; otherwise they stay on disk so the agent can be added again.
func ReleaseAgent(a config.AgentConfig, deleteData bool) error {
	if !deleteData {
		return nil
	}
	if sessions, err := agent.NewSessionManager().List(a.Name); err == nil {
		agent.RecordSessionsDeleted(a.Name, sessions)
		for _, sess := range sessions {
			_, files := sessionContent(a.Name, sess.ID)
			for _, f := range files {
				if err := os.Remove(f); err != nil && !errors.Is(err, fs.ErrNotExist) {
					return fmt.Errorf("deleting %s: %w", f, err)
				}
			}
			agent.ClearSessionDelivery(a.Name, sess.ID)
		}
	}
	for _, dir := range agentDataDirs(a) {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("deleting agent data %s: %w", dir, err)
		}
	}
	return nil
}

// Orphan is a task or channel session whose owner no longer exists, so it
// would show in Chats as if its task or channel did.
type Orphan struct {
	Agent   string `json:"agent"`
	Session string `json:"session"`
	Title   string `json:"title"`
}

// FindOrphans lists every configured agent's task and channel sessions whose
// task or channel is gone. An agent whose task files fail to load is skipped,
// since its tasks' sessions would look orphaned.
func FindOrphans(cfg *config.Config) ([]Orphan, error) {
	if cfg == nil {
		return nil, nil
	}
	var out []Orphan
	for _, a := range cfg.Agents {
		if _, err := config.LoadAgentTaskFiles(a); err != nil {
			continue
		}
		sessions, err := agent.NewSessionManager().List(a.Name)
		if err != nil {
			return nil, fmt.Errorf("agent %q: %w", a.Name, err)
		}
		for _, sess := range sessions {
			if !orphaned(a, sess) {
				continue
			}
			title := sess.Title
			if title == "" {
				title = sess.Name
			}
			out = append(out, Orphan{Agent: a.Name, Session: sess.ID, Title: title})
		}
	}
	return out, nil
}

func orphaned(a config.AgentConfig, sess *domain.Session) bool {
	switch sess.Type {
	case domain.SessionTypeTask:
		return agent.TaskSessionOwner(a, sess) < 0
	case domain.SessionTypeChannel:
		return ChannelOwner(a, sess) < 0
	}
	return false
}

// ReleaseOrphans archives each orphan as a regular chat titled after its old
// owner, or deletes it with its messages and media when deleteData is set.
func ReleaseOrphans(orphans []Orphan, deleteData bool) error {
	sm := agent.NewSessionManager()
	for _, o := range orphans {
		sess, err := sm.Load(o.Agent, o.Session)
		if err != nil {
			return err
		}
		if deleteData {
			err = deleteSession(o.Agent, sess)
		} else {
			err = archiveSession(o.Agent, sess, o.Title)
		}
		if err != nil {
			return fmt.Errorf("agent %q session %q: %w", o.Agent, o.Session, err)
		}
	}
	return nil
}
