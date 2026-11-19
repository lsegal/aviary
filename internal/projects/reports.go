package projects

import (
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

// ReportDelivery sends one report as agent through target, a scheduled-task
// style route such as "slack:main:C123" or "session:projects".
type ReportDelivery func(agent, target, text string) error

// reporter turns job status changes in the engine's snapshots into reports
// for the projects that opted in.
type reporter struct {
	mu       sync.Mutex
	deliver  ReportDelivery
	projects map[string]*config.ProjectReportsConfig
	// events holds each job's last reportable event, or "" while it has
	// none, keyed by jobKey. A job seen for the first time only records its
	// event, so an engine restart does not repeat old reports.
	events map[string]string
	// send delivers the reports; tests replace it to run synchronously.
	send func(func())
}

func newReporter() *reporter {
	return &reporter{
		projects: map[string]*config.ProjectReportsConfig{},
		events:   map[string]string{},
		send:     func(f func()) { go f() },
	}
}

// setDelivery sets how reports are sent; nil sends none.
func (r *reporter) setDelivery(deliver ReportDelivery) {
	r.mu.Lock()
	r.deliver = deliver
	r.mu.Unlock()
}

// configure applies the projects: section's per-project report settings.
func (r *reporter) configure(cfg config.ProjectsConfig) {
	projects := map[string]*config.ProjectReportsConfig{}
	for _, project := range cfg.List {
		if project.Reports.Enabled() {
			projects[strings.TrimSpace(project.Target)] = project.Reports
		}
	}
	r.mu.Lock()
	r.projects = projects
	r.mu.Unlock()
}

// observe reports every job whose status moved to a new reportable event
// since the last snapshot.
func (r *reporter) observe(snapshot core.Snapshot) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, job := range snapshot.Jobs {
		key := reportJobKey(job)
		event := reportEvent(job.Status)
		previous, seen := r.events[key]
		r.events[key] = event
		if !seen || event == "" || event == previous {
			continue
		}
		reports := r.projects[strings.TrimSpace(job.Target)]
		if r.deliver == nil || !reports.Reports(event) {
			continue
		}
		deliver, agent, text := r.deliver, strings.TrimSpace(reports.Agent), reportText(job, event)
		for _, target := range reports.Targets {
			if target = strings.TrimSpace(target); target == "" {
				continue
			}
			r.send(func() {
				if err := deliver(agent, target, text); err != nil {
					slog.Warn("projects: report delivery failed", "component", "projects", "agent", agent, "target", target, "err", err)
				}
			})
		}
	}
}

// reportJobKey identifies a job across snapshots.
func reportJobKey(job core.JobSnapshot) string {
	return job.Target + "|" + job.Repository + "#" + strconv.Itoa(job.Number) + "|" + job.Kind + "|" + job.ScheduleID
}

// reportEvent maps a job status to the report event it raises, or "" for a
// status that raises none.
func reportEvent(status string) string {
	switch status {
	case "complete":
		return config.ProjectReportMerged
	case "failed":
		return config.ProjectReportFailed
	case "waiting", "paused":
		return config.ProjectReportWaiting
	}
	return ""
}

// reportText is the message sent for a job's event.
func reportText(job core.JobSnapshot, event string) string {
	repo := job.Repository
	if repo == "" {
		repo = job.Target
	}
	subject := fmt.Sprintf("%s#%d", repo, job.Number)
	if job.Kind == core.JobKindSchedule {
		// A scheduled run's Number is its fire time, not an issue.
		subject = fmt.Sprintf("Scheduled prompt %s on %s", strconv.Quote(job.ScheduleID), job.Target)
	} else if job.Title != "" {
		subject += " " + strconv.Quote(job.Title)
	}
	var text string
	switch event {
	case config.ProjectReportMerged:
		if job.PullRequest > 0 {
			text = fmt.Sprintf("%s is done: pull request #%d merged.", subject, job.PullRequest)
		} else {
			text = subject + " is done."
		}
	case config.ProjectReportFailed:
		text = subject + " failed."
		if line := strings.TrimSpace(lastLines(job.Log, 1)); line != "" {
			text += " " + line
		}
	default:
		text = subject + " is waiting"
		if job.WaitReason != "" {
			text += ": " + job.WaitReason
		}
		text += "."
	}
	if url := job.PullRequestURL(); url != "" {
		text += "\n" + url
	} else if url := job.IssueURL(); url != "" {
		text += "\n" + url
	}
	return text
}
