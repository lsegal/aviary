package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProjectsDefaults(t *testing.T) {
	var p ProjectsConfig
	if p.EffectiveConcurrency() != DefaultProjectsConcurrency || p.EffectiveInterval() != DefaultProjectsInterval {
		t.Fatalf("defaults = %d, %s", p.EffectiveConcurrency(), p.EffectiveInterval())
	}
	if p.EffectiveDelivery() != "poll" || p.EffectiveReadyLabel() != "ready" || p.EffectiveFilter() != DefaultProjectsFilter {
		t.Fatalf("defaults = %q, %q, %q", p.EffectiveDelivery(), p.EffectiveReadyLabel(), p.EffectiveFilter())
	}
	if got := p.EffectiveAgents(); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("default agents = %v", got)
	}
	empty := ""
	p = ProjectsConfig{Concurrency: 5, Interval: "2m", Delivery: "Webhook", ReadyLabel: &empty, Filter: []string{"label:bug", "author:@me"}, Agents: []string{" claude ", ""}}
	if p.EffectiveConcurrency() != 5 || p.EffectiveInterval() != 2*time.Minute || p.EffectiveDelivery() != "webhook" {
		t.Fatalf("overrides = %d, %s, %s", p.EffectiveConcurrency(), p.EffectiveInterval(), p.EffectiveDelivery())
	}
	if p.EffectiveReadyLabel() != "" || p.EffectiveFilter() != "label:bug author:@me" {
		t.Fatalf("overrides = %q, %q", p.EffectiveReadyLabel(), p.EffectiveFilter())
	}
	if got := p.EffectiveAgents(); len(got) != 1 || got[0] != "claude" {
		t.Fatalf("agents = %v", got)
	}
}

func TestProjectsTargetsDropDuplicates(t *testing.T) {
	p := ProjectsConfig{List: []ProjectConfig{{Target: "o/r"}, {Target: " o/r "}, {Target: ""}, {Target: "o/s"}}}
	if got := strings.Join(p.Targets(), ","); got != "o/r,o/s" {
		t.Fatalf("targets = %s", got)
	}
}

// TestProjectsSectionOnlyHoldsChangedValues checks a config with no project
// settings writes no projects: section, and one changed value writes only
// that value.
func TestProjectsSectionOnlyHoldsChangedValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	cfg := Default()
	if err := Save(path, &cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if strings.Contains(string(data), "projects") {
		t.Fatalf("default config wrote a projects section:\n%s", data)
	}
	cfg.Projects.NoMerge = true
	if err := Save(path, &cfg); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "projects:\n  no_merge: true\n") {
		t.Fatalf("config = \n%s", data)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.Projects.NoMerge || loaded.Projects.IsZero() {
		t.Fatalf("loaded projects = %+v", loaded.Projects)
	}
}

func TestProjectReportsEnabledAndEvents(t *testing.T) {
	var unset *ProjectReportsConfig
	if unset.Enabled() || unset.Reports(ProjectReportFailed) {
		t.Fatal("unset reports are on")
	}
	all := &ProjectReportsConfig{Agent: "bot", Targets: []string{"session:projects"}}
	for _, event := range ProjectReportEvents {
		if !all.Reports(event) {
			t.Errorf("reports without events skip %s", event)
		}
	}
	some := &ProjectReportsConfig{Agent: "bot", Targets: []string{"session:projects"}, Events: []string{" Failed "}}
	if !some.Reports(ProjectReportFailed) || some.Reports(ProjectReportMerged) {
		t.Fatalf("event filter not applied: %+v", some)
	}
}

func TestValidateProjectReports(t *testing.T) {
	cfg := &Config{Agents: []AgentConfig{{Name: "bot", Model: "m"}}}
	if err := cfg.ValidateProjectReports(nil); err != nil {
		t.Fatalf("nil reports: %v", err)
	}
	if err := cfg.ValidateProjectReports(&ProjectReportsConfig{Agent: "bot", Targets: []string{"session:x"}, Events: []string{"merged", "waiting"}}); err != nil {
		t.Fatalf("valid reports: %v", err)
	}
	for name, reports := range map[string]*ProjectReportsConfig{
		"no agent":      {Targets: []string{"session:x"}},
		"unknown agent": {Agent: "ghost", Targets: []string{"session:x"}},
		"no targets":    {Agent: "bot"},
		"bad event":     {Agent: "bot", Targets: []string{"session:x"}, Events: []string{"started"}},
	} {
		if err := cfg.ValidateProjectReports(reports); err == nil {
			t.Errorf("%s: accepted %+v", name, reports)
		}
	}
}

// TestProjectReportsRoundTrip checks reports are written only for a project
// that opted in and load back unchanged.
func TestProjectReportsRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	cfg := Default()
	cfg.Projects.List = []ProjectConfig{
		{Target: "acme/quiet"},
		{Target: "acme/app", Reports: &ProjectReportsConfig{Agent: "bot", Targets: []string{"slack:main:C1"}, Events: []string{"failed"}}},
	}
	if err := Save(path, &cfg); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if got := strings.Count(string(data), "reports:"); got != 1 {
		t.Fatalf("config has %d reports sections:\n%s", got, data)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Projects.List[0].Reports != nil {
		t.Fatalf("quiet project loaded reports: %+v", loaded.Projects.List[0].Reports)
	}
	reports := loaded.Projects.List[1].Reports
	if !reports.Enabled() || reports.Agent != "bot" || reports.Targets[0] != "slack:main:C1" || !reports.Reports(ProjectReportFailed) || reports.Reports(ProjectReportMerged) {
		t.Fatalf("loaded reports = %+v", reports)
	}
}

func TestProjectToolsByPreset(t *testing.T) {
	if ToolGroup("project_job_stop") != "project" {
		t.Fatalf("group = %q", ToolGroup("project_job_stop"))
	}
	if !IsToolAllowedByPreset(PermissionsPresetStandard, "project_status") || !IsToolAllowedByPreset(PermissionsPresetFull, "project_job_stop") {
		t.Fatal("standard and full presets should allow project tools")
	}
	if IsToolAllowedByPreset(PermissionsPresetMinimal, "project_job_stop") {
		t.Fatal("the minimal preset should block project tools")
	}
}
