package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/scheduler"
	"github.com/lsegal/aviary/internal/store"
)

// forEachCheckpointFile calls fn with the path of every JSON checkpoint file
// stored for agentName. A missing or unreadable directory is skipped.
func forEachCheckpointFile(agentName string, fn func(path string)) {
	dir := store.CheckpointDir(agentName)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		fn(filepath.Join(dir, e.Name()))
	}
}

// deleteSessionCheckpoints deletes agentName's checkpoints for session sid,
// plus any checkpoint that cannot be read, to avoid leaving corrupt files.
func deleteSessionCheckpoints(agentName, sid string) {
	forEachCheckpointFile(agentName, func(p string) {
		v, rerr := store.ReadJSON[agent.RunCheckpoint](p)
		if rerr != nil {
			if derr := store.DeleteJSON(p); derr != nil {
				slog.Warn("mcp: failed to delete unreadable checkpoint", "agent", agentName, "path", p, "err", derr)
			}
			return
		}
		if v.SessionID != sid {
			return
		}
		if derr := store.DeleteJSON(p); derr != nil {
			slog.Warn("mcp: failed to delete checkpoint", "agent", agentName, "path", p, "err", derr)
		}
	})
}

// runSchedulerJob is the shared body of the tools that start a job through
// the scheduler (task_run, job_run_now) and return it as JSON.
func runSchedulerJob(tool, logKey, value string, run func(*scheduler.Scheduler, string) (*domain.Job, error)) (*sdkmcp.CallToolResult, struct{}, error) {
	slog.Info("mcp: tool call", "component", "scheduler", "tool", tool, logKey, value)
	d := GetDeps()
	if d.Scheduler == nil {
		return nil, struct{}{}, fmt.Errorf("scheduler not initialized")
	}
	job, err := run(d.Scheduler, value)
	if err != nil {
		return nil, struct{}{}, err
	}
	return jsonResult(job)
}

// jobSessionLog renders the message log of job's session, or "" when the job
// has no readable session messages.
func jobSessionLog(job domain.Job) string {
	if job.SessionID == "" {
		return ""
	}
	sessPath := store.FindSessionPath(job.AgentID, job.SessionID)
	if sessPath == "" {
		return ""
	}
	lines, err := store.ReadJSONL[domain.Message](sessPath)
	if err != nil {
		return ""
	}
	var sb strings.Builder
	for _, msg := range lines {
		if msg.Role == "" {
			// response marker or metadata — skip
			continue
		}
		ts := ""
		if !msg.Timestamp.IsZero() {
			ts = msg.Timestamp.Format(time.RFC3339Nano)
		}
		fmt.Fprintf(&sb, "[%s] <%s>: %s\n", ts, string(msg.Role), sessionLogContent(msg))
	}
	return sb.String()
}

// sessionLogContent is msg's content with media markers appended and tool
// JSON payloads pretty-printed when possible.
func sessionLogContent(msg domain.Message) string {
	content := strings.TrimSpace(msg.Content)
	for _, mediaURL := range msg.MediaURLs {
		if content != "" {
			content += " "
		}
		content += "[media: " + mediaURL + "]"
	}
	if msg.Role != domain.MessageRoleTool || content == "" {
		return content
	}
	var pretty any
	if err := json.Unmarshal([]byte(content), &pretty); err != nil {
		return content
	}
	if p, err := json.MarshalIndent(pretty, "", "  "); err == nil {
		return string(p)
	}
	return content
}

// browserTypeAction describes a browser tool that sends text to an element.
type browserTypeAction struct {
	tool    string
	run     func(m *browser.Manager, ctx context.Context, tabID, selector, text string) error
	logMsg  string
	doneFmt string // formatted with the selector and tab ID
}

// browserTypeText is the shared body of browser_keystroke and browser_fill.
func browserTypeText(ctx context.Context, args browserTypeArgs, action browserTypeAction) (*sdkmcp.CallToolResult, struct{}, error) {
	slog.Info("mcp: tool call", "component", "browser", "tool", action.tool, "tab_id", args.TabID, "selector", args.Selector)
	d := GetDeps()
	if d.Browser == nil {
		return nil, struct{}{}, fmt.Errorf("browser manager not initialized")
	}
	if args.TabID == "" {
		return nil, struct{}{}, fmt.Errorf("tab_id is required")
	}
	if err := action.run(d.Browser, ctx, args.TabID, args.Selector, args.Text); err != nil {
		slog.Error("mcp: tool failed", "component", "browser", "tool", action.tool, "tab_id", args.TabID, "selector", args.Selector, "err", err)
		return nil, struct{}{}, err
	}
	slog.Info(action.logMsg, "component", "browser", "tab_id", args.TabID, "selector", args.Selector)
	return text(fmt.Sprintf(action.doneFmt, args.Selector, args.TabID))
}

// startBrowserLogin is the shared body of the tools that start a browser
// OAuth login and return its authorization and callback URLs.
func startBrowserLogin(label string, start func() (*auth.BrowserLoginStart, error)) (*sdkmcp.CallToolResult, struct{}, error) {
	login, err := start()
	if err != nil {
		return nil, struct{}{}, fmt.Errorf("%s login: %w", label, err)
	}
	return jsonResult(map[string]any{
		"url":                login.AuthorizeURL,
		"callback_url":       login.CallbackURL,
		"browser_opened":     login.BrowserOpened,
		"browser_open_error": login.BrowserOpenErr,
		"expires_at":         login.ExpiresAt.UTC().Format(time.RFC3339),
		"timeout_seconds":    int(auth.BrowserLoginTimeout / time.Second),
	})
}

// browserLogin describes how to finish a browser OAuth login.
type browserLogin struct {
	label    string // prefix of error messages
	display  string // provider name in the success message
	storeKey string // auth store key for the token
	complete func(context.Context) (*auth.OAuthToken, error)
}

// completeBrowserLogin waits for a browser OAuth callback, stores the token
// and reconciles agents.
func completeBrowserLogin(ctx context.Context, login browserLogin) (*sdkmcp.CallToolResult, struct{}, error) {
	loginCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	token, err := login.complete(loginCtx)
	if err != nil {
		return nil, struct{}{}, fmt.Errorf("%s login: %w", login.label, err)
	}
	tokenJSON, _ := json.Marshal(token)
	st, err := authStore()
	if err != nil {
		return nil, struct{}{}, err
	}
	if err := st.Set(login.storeKey, string(tokenJSON)); err != nil {
		return nil, struct{}{}, err
	}
	reconcileAgents()
	return text(fmt.Sprintf("%s OAuth login successful. Access token stored (expires %s).", login.display, time.UnixMilli(token.ExpiresAt).UTC().Format(time.RFC3339)))
}

// syncFileBackedTasks deletes the markdown files of file-backed tasks that
// cfg no longer defines, then writes cfg's file-backed tasks to markdown
// files and removes them from the inline config so aviary.yaml does not
// duplicate them.
func syncFileBackedTasks(prevCfg, cfg *config.Config) error {
	for i := range cfg.Agents {
		var prevTasks []config.TaskConfig
		if i < len(prevCfg.Agents) {
			prevTasks = prevCfg.Agents[i].Tasks
		}
		if err := removeDeletedTaskFiles(cfg.Agents[i], prevTasks); err != nil {
			return err
		}
		if err := writeFileBackedTasks(&cfg.Agents[i]); err != nil {
			return err
		}
	}
	return nil
}

// removeDeletedTaskFiles deletes the markdown file of every file-backed task
// in prevTasks that agentCfg no longer defines.
func removeDeletedTaskFiles(agentCfg config.AgentConfig, prevTasks []config.TaskConfig) error {
	prevFileTasks := map[string]struct{}{}
	for _, t := range prevTasks {
		if t.FromFile {
			prevFileTasks[t.Name] = struct{}{}
		}
	}
	if len(prevFileTasks) == 0 {
		return nil
	}
	currTasks := map[string]struct{}{}
	for _, t := range agentCfg.Tasks {
		currTasks[t.Name] = struct{}{}
	}
	files, _ := filepath.Glob(filepath.Join(config.AgentTasksDir(agentCfg), "*.md"))
	for name := range prevFileTasks {
		if _, ok := currTasks[name]; ok {
			continue
		}
		if err := deleteTaskFile(agentCfg.Name, files, name); err != nil {
			return err
		}
	}
	return nil
}

// deleteTaskFile deletes the first of files whose parsed task name is name.
// Files that are already gone are skipped.
func deleteTaskFile(agentName string, files []string, name string) error {
	for _, f := range files {
		tc, err := config.LoadMarkdownTask(f)
		if err != nil || tc.Name != name {
			continue
		}
		// Delete via store helper using the path relative to the agent
		// directory (preserving subdirectories such as "tasks/").
		rel, err := filepath.Rel(store.AgentDir(agentName), f)
		if err != nil {
			return fmt.Errorf("computing relative task file path: %w", err)
		}
		err = deleteAgentFile(agentName, rel)
		if err == nil {
			return nil
		}
		if !os.IsNotExist(err) {
			return fmt.Errorf("deleting task file %s: %w", rel, err)
		}
		// Ignore missing files (race or already-removed).
	}
	return nil
}

// writeFileBackedTasks writes agentCfg's file-backed tasks to markdown files
// and removes them from its inline task list.
func writeFileBackedTasks(agentCfg *config.AgentConfig) error {
	var filtered []config.TaskConfig
	for _, t := range agentCfg.Tasks {
		if !t.FromFile {
			filtered = append(filtered, t)
			continue
		}
		if _, err := config.SaveMarkdownTask(config.AgentTasksDir(*agentCfg), t); err != nil {
			return fmt.Errorf("writing task file: %w", err)
		}
	}
	agentCfg.Tasks = filtered
	return nil
}
