package projects

import (
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/core"
)

type sentReport struct{ agent, target, text string }

func testReporter(cfg config.ProjectsConfig) (*reporter, *[]sentReport) {
	var sent []sentReport
	r := newReporter()
	r.send = func(f func()) { f() }
	r.setDelivery(func(agent, target, text string) error {
		sent = append(sent, sentReport{agent, target, text})
		return nil
	})
	r.configure(cfg)
	return r, &sent
}

func reportJob(status string) core.JobSnapshot {
	return core.JobSnapshot{Number: 7, Target: "acme/app", Repository: "acme/app", Title: "Fix login", Status: status, Kind: core.JobKindIssue}
}

func reportsConfig(reports *config.ProjectReportsConfig) config.ProjectsConfig {
	return config.ProjectsConfig{List: []config.ProjectConfig{
		{Target: "acme/app", Reports: reports},
		{Target: "acme/quiet"},
	}}
}

func TestReporterSendsLifecycleEventsThroughEveryTarget(t *testing.T) {
	r, sent := testReporter(reportsConfig(&config.ProjectReportsConfig{Agent: "bot", Targets: []string{"slack:main:C1", "session:projects"}}))

	r.observe(core.Snapshot{Jobs: []core.JobSnapshot{reportJob("active")}})
	if len(*sent) != 0 {
		t.Fatalf("an active job sent reports: %+v", *sent)
	}
	done := reportJob("complete")
	done.PullRequest = 12
	r.observe(core.Snapshot{Jobs: []core.JobSnapshot{done}})
	r.observe(core.Snapshot{Jobs: []core.JobSnapshot{done}})
	if len(*sent) != 2 {
		t.Fatalf("sent %d reports, want one per target: %+v", len(*sent), *sent)
	}
	for i, target := range []string{"slack:main:C1", "session:projects"} {
		got := (*sent)[i]
		if got.agent != "bot" || got.target != target {
			t.Errorf("report %d went to %s via %s", i, got.target, got.agent)
		}
		want := "acme/app#7 \"Fix login\" is done: pull request #12 merged.\nhttps://github.com/acme/app/pull/12"
		if got.text != want {
			t.Errorf("report %d = %q, want %q", i, got.text, want)
		}
	}
}

func TestReporterReportsFailedAndWaitingJobs(t *testing.T) {
	r, sent := testReporter(reportsConfig(&config.ProjectReportsConfig{Agent: "bot", Targets: []string{"session:projects"}}))
	r.observe(core.Snapshot{Jobs: []core.JobSnapshot{reportJob("active")}})

	waiting := reportJob("waiting")
	waiting.WaitReason = "CI is running"
	r.observe(core.Snapshot{Jobs: []core.JobSnapshot{waiting}})
	// Waiting and paused are the same event, so moving between them is
	// not reported again.
	paused := reportJob("paused")
	r.observe(core.Snapshot{Jobs: []core.JobSnapshot{paused}})
	failed := reportJob("failed")
	failed.Log = "output\nagent exited with status 1\n"
	r.observe(core.Snapshot{Jobs: []core.JobSnapshot{failed}})

	want := []string{
		"acme/app#7 \"Fix login\" is waiting: CI is running.\nhttps://github.com/acme/app/issues/7",
		"acme/app#7 \"Fix login\" failed. agent exited with status 1\nhttps://github.com/acme/app/issues/7",
	}
	if len(*sent) != len(want) {
		t.Fatalf("sent %+v, want %d reports", *sent, len(want))
	}
	for i := range want {
		if (*sent)[i].text != want[i] {
			t.Errorf("report %d = %q, want %q", i, (*sent)[i].text, want[i])
		}
	}
}

func TestReporterSendsNothingWhenReportsAreOff(t *testing.T) {
	for name, reports := range map[string]*config.ProjectReportsConfig{
		"unset":      nil,
		"no agent":   {Targets: []string{"session:projects"}},
		"no targets": {Agent: "bot", Targets: []string{" "}},
	} {
		t.Run(name, func(t *testing.T) {
			r, sent := testReporter(reportsConfig(reports))
			quiet := reportJob("active")
			quiet.Target, quiet.Repository = "acme/quiet", "acme/quiet"
			r.observe(core.Snapshot{Jobs: []core.JobSnapshot{reportJob("active"), quiet}})
			quiet.Status = "failed"
			r.observe(core.Snapshot{Jobs: []core.JobSnapshot{reportJob("complete"), quiet}})
			if len(*sent) != 0 {
				t.Fatalf("sent reports while off: %+v", *sent)
			}
		})
	}
}

func TestReporterHonorsEventFilter(t *testing.T) {
	r, sent := testReporter(reportsConfig(&config.ProjectReportsConfig{Agent: "bot", Targets: []string{"session:projects"}, Events: []string{"Failed"}}))
	other := reportJob("active")
	other.Number = 8
	r.observe(core.Snapshot{Jobs: []core.JobSnapshot{reportJob("active"), other}})
	other.Status = "failed"
	r.observe(core.Snapshot{Jobs: []core.JobSnapshot{reportJob("complete"), other}})
	if len(*sent) != 1 || !strings.Contains((*sent)[0].text, "acme/app#8") || !strings.Contains((*sent)[0].text, "failed") {
		t.Fatalf("sent %+v, want only the failed job", *sent)
	}
}

func TestReporterDoesNotRepeatStateSeenFirst(t *testing.T) {
	r, sent := testReporter(reportsConfig(&config.ProjectReportsConfig{Agent: "bot", Targets: []string{"session:projects"}}))
	// A restarted engine republishes paused and finished jobs; those are not
	// new events.
	r.observe(core.Snapshot{Jobs: []core.JobSnapshot{reportJob("paused")}})
	if len(*sent) != 0 {
		t.Fatalf("first sighting sent reports: %+v", *sent)
	}
}

func TestReportTextForScheduledRun(t *testing.T) {
	job := core.JobSnapshot{Number: 1763540000, Target: "acme/app", Kind: core.JobKindSchedule, ScheduleID: "nightly", Status: "complete"}
	if got, want := reportText(job, config.ProjectReportMerged), `Scheduled prompt "nightly" on acme/app is done.`; got != want {
		t.Fatalf("reportText = %q, want %q", got, want)
	}
}

func TestManagerDeliversReportsFromSnapshots(t *testing.T) {
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m.SetEngineEnabled(false)
	defer m.Shutdown()
	sent := make(chan sentReport, 1)
	m.SetReportDelivery(func(agent, target, text string) error {
		sent <- sentReport{agent, target, text}
		return nil
	})
	m.Reconcile(&config.Config{Agents: []config.AgentConfig{{Name: "bot", Model: "cli/codex", Projects: reportsConfig(&config.ProjectReportsConfig{Agent: "bot", Targets: []string{"telegram:main:42"}})}}})

	m.Snapshot(core.Snapshot{Jobs: []core.JobSnapshot{reportJob("active")}})
	m.Snapshot(core.Snapshot{Jobs: []core.JobSnapshot{reportJob("failed")}})
	select {
	case got := <-sent:
		if got.agent != "bot" || got.target != "telegram:main:42" || !strings.Contains(got.text, "failed") {
			t.Fatalf("delivered %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no report delivered")
	}
}
