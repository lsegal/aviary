package core

import (
	"errors"
	"fmt"
	"time"
)

// ErrNotReady is returned by the settings and job-action handlers when the
// run loop has not yet reached its dispatch select statement. Before that
// point the handlers would otherwise block indefinitely on an unbuffered
// channel nothing is reading from yet, leaving the dashboard's "loading
// settings..." spinner stuck with no error and no timeout (issue #579). The
// dashboard treats this as a signal to retry rather than a hard failure.
var ErrNotReady = errors.New("run is still starting")

// JobSnapshot is one agent run as the dashboards draw it.
type JobSnapshot struct {
	Number int
	Target string
	// Repository is the OWNER/REPO hosting the job's issue, which differs
	// from Target when Target is a project, so the dashboards can link the
	// issue number to its page (issue #767).
	Repository        string
	Title             string
	Status            string
	CheckoutDirectory string
	SessionID         string
	Agent             string
	Model             string
	Effort            string
	Started           time.Time
	Log               string
	// WaitReason and WakeAt describe a job whose agent scheduled a wakeup and
	// is waiting for it, with Status "waiting" (issue #671). WaitReason also
	// says what a job with Status "paused" waits on (issue #732).
	WaitReason string
	WakeAt     time.Time
	// PullRequest is the number of the job's current pull request, or 0 while
	// it has none. PullRequestRepository is the OWNER/REPO hosting it, set
	// only when that differs from the issue's repository, such as a pull
	// request opened from a fork (issue #753).
	PullRequest           int
	PullRequestRepository string
	// TasksDone and TasksTotal count the checked and all items in the plan
	// checklist of the job's current pull request, and Progress is the
	// percentage done the dashboards draw as a progress bar (issue #755). A
	// job whose pull request has no checklist, or that has no pull request,
	// has a TasksTotal of zero and shows no bar. Progress is computed here
	// only, so the two dashboards can never disagree about it.
	TasksDone  int
	TasksTotal int
	Progress   int
	// Kind is JobKindIssue, JobKindDiscussion, or JobKindSchedule, and
	// ScheduleID names the schedule a scheduled run fired from (issue #848).
	Kind       string
	ScheduleID string
}

// Job kinds.
const (
	JobKindIssue      = "issue"
	JobKindDiscussion = "discussion"
	JobKindSchedule   = "schedule"
)

// HasProgress reports whether the job has a plan checklist to measure its
// progress against (issue #755).
func (j JobSnapshot) HasProgress() bool {
	return j.TasksTotal > 0
}

// setProgress records a plan checklist's counts and the percentage they make.
// A finished job reads 100% whatever its checklist still says.
func (j *JobSnapshot) setProgress(done, total int, finished bool) bool {
	percent, ok := ProgressPercent(done, total)
	if !ok {
		done, total = 0, 0
	} else if finished {
		percent = 100
	}
	if j.TasksDone == done && j.TasksTotal == total && j.Progress == percent {
		return false
	}
	j.TasksDone, j.TasksTotal, j.Progress = done, total, percent
	return true
}

// Finish marks the job's plan done, so a completed job reads 100% (issue
// #755). A job with no checklist still shows no bar.
func (j *JobSnapshot) Finish() {
	j.setProgress(j.TasksDone, j.TasksTotal, true)
}

// PullRequestLabel is how the dashboards name a job's pull request next to its
// issue number: "PR #750", or "PR fork/repo#750" when it lives in another
// repository. A job without a pull request has no label.
func (j JobSnapshot) PullRequestLabel() string {
	if j.PullRequest <= 0 {
		return ""
	}
	return fmt.Sprintf("PR %s#%d", j.PullRequestRepository, j.PullRequest)
}

// issueRepository is the OWNER/REPO hosting the job's issue: its Repository,
// or its Target when that is a bare repository.
func (j JobSnapshot) issueRepository() string {
	if j.Repository != "" {
		return j.Repository
	}
	if ValidRepo(j.Target) {
		return j.Target
	}
	return ""
}

// IssueURL is the GitHub page of the job's issue, or "" when its repository
// is unknown (issue #767).
func (j JobSnapshot) IssueURL() string {
	repository := j.issueRepository()
	// A scheduled run's Number is its fire time, not an issue.
	if repository == "" || j.Number <= 0 || j.Kind == JobKindSchedule {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/issues/%d", repository, j.Number)
}

// PullRequestURL is the GitHub page of the job's pull request, in its own
// repository when that differs from the issue's, or "" when the job has no
// pull request (issue #767).
func (j JobSnapshot) PullRequestURL() string {
	repository := j.PullRequestRepository
	if repository == "" {
		repository = j.issueRepository()
	}
	if repository == "" || j.PullRequest <= 0 {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/pull/%d", repository, j.PullRequest)
}

// SetPullRequest records the current pull request in state as the job's, and
// reports whether that changed what the dashboards show (issue #753), along
// with the progress its plan checklist makes (issue #755).
func (j *JobSnapshot) SetPullRequest(state OriginatingWorkState, issueRepository string) bool {
	number, repository := 0, ""
	pullRequest, ok := CurrentPullRequest(state)
	if ok {
		number = pullRequest.Number
		if !pullRequest.InRepo(issueRepository) {
			repository = pullRequest.Repository
		}
	}
	// A merged pull request reads 100% (issue #755).
	changed := j.setProgress(pullRequest.TasksDone, pullRequest.TasksTotal, pullRequest.Merged)
	if j.PullRequest == number && j.PullRequestRepository == repository {
		return changed
	}
	j.PullRequest, j.PullRequestRepository = number, repository
	return true
}

// Snapshot is the whole run as the dashboards draw it. The terminal dashboard
// in the root package and the browser dashboard in package webui both render
// it, so it lives here rather than in either of them.
type Snapshot struct {
	// Identity is this dispatcher instance's own handoff ID (issue #624). The
	// dashboards show it as the leftmost status segment so an operator
	// watching several running instances can tell which one they are looking
	// at without cross-referencing a terminal title or log line.
	Identity    string
	Targets     []string
	IssueCounts map[string]int
	Running     int
	Queued      int
	Completed   int
	Failed      int
	// Paused counts work out of the queue waiting on a GitHub merge or close,
	// holding no agent slot (issue #732).
	Paused      int
	Concurrency int
	NextPoll    time.Time
	// LastPoll is when the most recent poll of GitHub finished. The poll loop
	// only logs what it found when that changes (issue #413), so this is the
	// run's standing evidence that it is still checking (issue #447).
	LastPoll      time.Time
	Interval      time.Duration
	UseWebhooks   bool
	WebhookURL    string
	WebhookOnline bool
	// WebUIURL is the local browser dashboard address when it is enabled.
	// The terminal dashboard shows it as a persistent bottom line so operators
	// can open the companion interface without searching the startup logs.
	WebUIURL   string
	TokensUsed int
	TokenLimit int
	Quota      string
	Quotas     map[string]string
	// Jobs holds every in-flight job. The terminal dashboard draws just the
	// first VisibleJobs (issue #714); the web dashboard pages through all of
	// them VisibleJobs at a time, and /api/state publishes the Log only for
	// the jobs on the requested page (issue #739).
	Jobs        []JobSnapshot
	VisibleJobs int
	// Upcoming lists the next runs of the enabled schedules, soonest first
	// (issue #848).
	Upcoming []UpcomingRun
}

// UIReporter receives the published state of a run.
type UIReporter interface {
	Snapshot(Snapshot)
	Log(string)
}

// JobAction is a retry, stop, or pause a dashboard asks the run to perform on
// one job.
type JobAction struct {
	Action string `json:"action"`
	Target string `json:"target"`
	Number int    `json:"number"`
}

// JobActions says which job actions the run accepts for a job in one state,
// so a dashboard never enables a button whose request is guaranteed to fail
// (issue #763). RetryReason says why retry is refused, when it is.
type JobActions struct {
	Retry       bool
	RetryReason string
	Stop        bool
	Pause       bool
}

// JobActionAvailability reports the job actions the run accepts for a job in
// status. The web dashboard's jobActionAvailability mirrors it.
func JobActionAvailability(status string) JobActions {
	actions := JobActions{Retry: true}
	switch status {
	case "queued":
		actions.Retry, actions.RetryReason = false, "Already queued for a gh-fix run"
	case "stopping":
		actions.Retry, actions.RetryReason = false, "Waiting for the stopped run to exit"
	}
	// A job waiting on a scheduled wakeup still holds its agent session, so
	// it can be stopped just like a running one (issue #671). A queued job
	// has nothing to stop yet, but it can be held back (issue #771).
	running := status == "active" || status == "waiting"
	actions.Pause = running || status == "queued"
	// Stopping a paused job clears it, so a manual pause Retry cannot
	// requeue still has a way out (issue #777).
	actions.Stop = running || status == "paused"
	return actions
}

// SettingsUpdate carries a partial update to glorp's live-editable runtime
// settings (issue #341). Nil fields are left unchanged.
type SettingsUpdate struct {
	Concurrency       *int      `json:"concurrency,omitempty"`
	ReadyState        *string   `json:"readyState,omitempty"`
	AllowedCommenters *[]string `json:"allowedCommenters,omitempty"`
	NoMerge           *bool     `json:"noMerge,omitempty"`
	NoChangelog       *bool     `json:"noChangelog,omitempty"`
	// AutoUpdate chooses whether the updater restarts onto a new release
	// while agents are busy (issue #824); off waits until none are active.
	AutoUpdate *bool `json:"autoUpdate,omitempty"`
	// TempDirs replaces the ordered temp directories new clones are placed in
	// (issue #830). An empty list means the system temp directory.
	TempDirs *[]string `json:"tempDirs,omitempty"`
	// ActiveAgents replaces the live set of agent specs new dispatches
	// round-robin across (issue #572), overriding what --agent configured at
	// startup. When given it must name at least one valid agent spec; the
	// result is reported back as SettingsSnapshot.ConfiguredAgents.
	ActiveAgents *[]string `json:"activeAgents,omitempty"`
	// UnblockInterval is how often the stuck-job watchdog runs, as a
	// duration such as "5m", or "0" to turn it off (issue #779).
	UnblockInterval *string `json:"unblockInterval,omitempty"`
	// UnblockAgent and UnblockModel override the agent and model of the
	// watchdog's unblock call. Empty means the coding agent and its model.
	UnblockAgent *string `json:"unblockAgent,omitempty"`
	UnblockModel *string `json:"unblockModel,omitempty"`
	// PromptAgent and PromptModel override the agent and model of the
	// dashboards' prompt agent (issue #801). Empty means the coding agent and
	// its model.
	PromptAgent *string `json:"promptAgent,omitempty"`
	PromptModel *string `json:"promptModel,omitempty"`
}

// AgentOption describes one agent the registry knows, so the dashboard's
// agent selector can offer it -- and the models and levels it accepts -- from
// what the run actually registered rather than from a hardcoded pair.
type AgentOption struct {
	Name string `json:"name"`
	// Models and Levels are the agent's allow-lists. Empty means the agent
	// accepts any value, so the UI offers no closed list for it.
	Models []string `json:"models,omitempty"`
	Levels []string `json:"levels,omitempty"`
}

// SettingsSnapshot reports the settings a running glorp instance can change
// on the fly, and their current values.
type SettingsSnapshot struct {
	Concurrency       int      `json:"concurrency"`
	ReadyState        string   `json:"readyState"`
	ReadyStateDefault string   `json:"readyStateDefault"`
	AllowedCommenters []string `json:"allowedCommenters"`
	NoMerge           bool     `json:"noMerge"`
	NoChangelog       bool     `json:"noChangelog"`
	// AutoUpdate reports whether updates restart glorp even while agents
	// are busy (issue #824).
	AutoUpdate bool `json:"autoUpdate"`
	// TempDirs is the configured, ordered temp directories, empty for the
	// system temp directory, and TempDirDefault is that system directory
	// (issue #830).
	TempDirs       []string `json:"tempDirs"`
	TempDirDefault string   `json:"tempDirDefault"`
	// Agents lists every agent the run's registry defines, built-in and
	// config-defined alike, which is the set ActiveAgents may be set from.
	Agents []string `json:"agents"`
	// AgentOptions carries the same agents with their model and level
	// allow-lists, for a selector that offers more than a bare name.
	AgentOptions []AgentOption `json:"agentOptions,omitempty"`
	// ConfiguredAgents lists the agent specs this run dispatches with, which
	// is a subset of Agents chosen by --agent or the live ActiveAgents
	// override (issue #572).
	ConfiguredAgents []string `json:"configuredAgents"`
	// UnblockInterval, UnblockAgent, and UnblockModel are the stuck-job
	// watchdog's settings (issue #779), and UnblockDefault is the agent spec
	// its unblock call uses when no agent or model override is set.
	UnblockInterval string `json:"unblockInterval"`
	UnblockAgent    string `json:"unblockAgent"`
	UnblockModel    string `json:"unblockModel"`
	UnblockDefault  string `json:"unblockDefault"`
	// PromptAgent and PromptModel are the prompt agent's override (issue
	// #801), and PromptDefault is the agent spec it runs when neither is set.
	PromptAgent   string `json:"promptAgent"`
	PromptModel   string `json:"promptModel"`
	PromptDefault string `json:"promptDefault"`
}

// AgentStatus reports one registered agent's install, sign-in, and quota
// state, mirroring what `glorp agents` prints, for the settings dashboard's
// agents tab (issue #572).
type AgentStatus struct {
	Name        string `json:"name"`
	Installed   bool   `json:"installed"`
	Binary      string `json:"binary"`
	Version     string `json:"version,omitempty"`
	VersionNote string `json:"versionNote,omitempty"`
	Auth        string `json:"auth"`
	Quota       string `json:"quota"`
	// TracksQuota says the definition names a quota source at all, telling an
	// agent that reports no quota by design apart from one whose reader
	// could not answer -- the same distinction describeQuota draws for the
	// terminal report.
	TracksQuota bool   `json:"tracksQuota"`
	QuotaError  string `json:"quotaError,omitempty"`
	// Models are the fully qualified agent/model names --agent accepts, and
	// ModelNote explains a missing or partial list.
	Models    []string `json:"models,omitempty"`
	ModelNote string   `json:"modelNote,omitempty"`
	// DefaultModel is the model glorp runs this agent with when --agent names
	// none, empty for an agent whose definition leaves the choice to its own
	// CLI (issue #612).
	DefaultModel string `json:"defaultModel,omitempty"`
	// SkillsNote says which of glorp's installed skills are stale for the
	// running release, empty when they are current (issue #797).
	SkillsNote string `json:"skillsNote,omitempty"`
	// Status is one of "ok", "warn", or "missing", matching the marker
	// `glorp agents` prefixes each block with.
	Status string `json:"status"`
}

// CheckpointResult is one active run's outcome in the shutdown checkpoint
// glorp takes before it restarts or exits (issue #741): Outcome is "pushed",
// "nothing to push", "failed", or "timed out", and Detail says why.
type CheckpointResult struct {
	Target  string
	Number  int
	Branch  string `json:",omitempty"`
	Outcome string
	Detail  string `json:",omitempty"`
}

// PromptEntry is one request made in the dashboards' prompt area and the
// prompt agent's answer to it (issue #801). Status is "pending" while the
// agent call runs, then "done" or "failed". Actions lists the outcome of each
// job action or unblock round the answer asked glorp to perform.
type PromptEntry struct {
	ID       int       `json:"id"`
	Request  string    `json:"request"`
	Status   string    `json:"status"`
	Agent    string    `json:"agent,omitempty"`
	Response string    `json:"response,omitempty"`
	Actions  []string  `json:"actions,omitempty"`
	Started  time.Time `json:"started"`
	Finished time.Time `json:"finished,omitzero"`
}

// PromptReporter receives the prompt area's conversation whenever an entry is
// added or answered. A UIReporter that also implements it shows the prompt
// area (issue #801).
type PromptReporter interface {
	Prompts([]PromptEntry)
}

// IssueUpdate is an update to post as a comment on Repository's issue or pull
// request Number, mentioning its author so the owning job hears it through
// glorp's mention path (issue #806).
type IssueUpdate struct {
	Repository string `json:"repository"`
	Number     int    `json:"number"`
	Text       string `json:"text"`
}

// IssueUpdateResult is the comment an IssueUpdate posted.
type IssueUpdateResult struct {
	URL    string `json:"url"`
	Author string `json:"author"`
}

// IssueCreate asks for an issue to be filed on Repository through the
// gh-create skill (issue #807), described by Text. Model, when set, overrides
// the configured prompt-agent model for this request.
type IssueCreate struct {
	Repository string `json:"repository"`
	Text       string `json:"text"`
	Model      string `json:"model,omitempty"`
}

// IssueCreateResult is the issue an IssueCreate filed and the agent that
// filed it.
type IssueCreateResult struct {
	URL   string `json:"url"`
	Agent string `json:"agent"`
}
