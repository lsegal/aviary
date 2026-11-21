package config

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestProjectsDefaults(t *testing.T) {
	var p ProjectsConfig
	if p.EffectiveConcurrency() != DefaultProjectsConcurrency || p.EffectiveInterval() != DefaultProjectsBrowserInterval {
		t.Fatalf("defaults = %d, %s", p.EffectiveConcurrency(), p.EffectiveInterval())
	}
	if p.EffectiveDelivery() != "browser" || p.EffectiveReadyLabel() != "ready" || p.EffectiveFilter() != DefaultProjectsFilter {
		t.Fatalf("defaults = %q, %q, %q", p.EffectiveDelivery(), p.EffectiveReadyLabel(), p.EffectiveFilter())
	}
	empty := ""
	p = ProjectsConfig{Concurrency: 5, Interval: "2m", Delivery: "Webhook", ReadyLabel: &empty, Filter: []string{"label:bug", "author:@me"}}
	if p.EffectiveConcurrency() != 5 || p.EffectiveInterval() != 2*time.Minute || p.EffectiveDelivery() != "webhook" {
		t.Fatalf("overrides = %d, %s, %s", p.EffectiveConcurrency(), p.EffectiveInterval(), p.EffectiveDelivery())
	}
	if p.EffectiveReadyLabel() != "" || p.EffectiveFilter() != "label:bug author:@me" {
		t.Fatalf("overrides = %q, %q", p.EffectiveReadyLabel(), p.EffectiveFilter())
	}
}

func TestProjectsDeliveryInterval(t *testing.T) {
	if got := (ProjectsConfig{}).EffectiveInterval(); got != 20*time.Second {
		t.Fatalf("browser delivery interval = %s, want 20s", got)
	}
	for _, mode := range []string{"poll", "webhook", "Poll"} {
		p := ProjectsConfig{Delivery: mode}
		if p.EffectiveDelivery() != strings.ToLower(mode) || p.EffectiveInterval() != DefaultProjectsInterval {
			t.Fatalf("%s = %q every %s", mode, p.EffectiveDelivery(), p.EffectiveInterval())
		}
	}
	if got := (ProjectsConfig{Delivery: "browser", Interval: "1m"}).EffectiveInterval(); got != time.Minute {
		t.Fatalf("explicit interval = %s", got)
	}
}

func TestProjectsTargetsDropDuplicates(t *testing.T) {
	p := AgentProjectsConfig{List: []ProjectConfig{{Target: "o/r"}, {Target: " o/r "}, {Target: ""}, {Target: "o/s"}}}
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
	cfg.Agents = []AgentConfig{{Name: "coder", Model: "m"}}
	if err := Save(path, &cfg); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if strings.Contains(string(data), "projects") {
		t.Fatalf("an agent without projects wrote a projects section:\n%s", data)
	}
	cfg.Projects.NoMerge = true
	if err := Save(path, &cfg); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	if !strings.Contains(string(data), "\nprojects:\n  no_merge: true\n") || strings.Contains(string(data), "    projects:") {
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
	cfg.Agents = []AgentConfig{{Name: "bot", Model: "m", Projects: AgentProjectsConfig{List: []ProjectConfig{
		{Target: "acme/quiet"},
		{Target: "acme/app", Reports: &ProjectReportsConfig{Agent: "bot", Targets: []string{"slack:main:C1"}, Events: []string{"failed"}}},
	}}}}
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
	if loaded.Agents[0].Projects.List[0].Reports != nil {
		t.Fatalf("quiet project loaded reports: %+v", loaded.Agents[0].Projects.List[0].Reports)
	}
	reports := loaded.Agents[0].Projects.List[1].Reports
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

// TestProjectsLiveUnderTheirAgent checks the engine sees every agent's
// projects with their owners, under the one shared top-level projects:
// settings; settings left in an agent's section are not read.
func TestProjectsLiveUnderTheirAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	yaml := `agents:
  - name: coder
    model: cli/claude/opus
    projects:
      concurrency: 2
      list:
        - target: acme/app
  - name: docs
    model: cli/codex/gpt-5
    projects:
      concurrency: 4
      no_merge: true
      list:
        - target: acme/docs
projects:
  concurrency: 5
  list:
    - target: acme/legacy
`
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := (AgentProjectsConfig{List: cfg.ProjectList()}).Targets(); !slices.Equal(got, []string{"acme/app", "acme/docs"}) {
		t.Fatalf("targets = %v", got)
	}
	if cfg.Projects.Concurrency != 5 || cfg.Projects.NoMerge {
		t.Fatalf("shared settings = %+v", cfg.Projects)
	}
	if cfg.ProjectOwner("acme/app") != "coder" || cfg.ProjectOwner("acme/docs") != "docs" || cfg.ProjectOwner("acme/legacy") != "" {
		t.Fatalf("owners = %v", cfg.ProjectOwners())
	}
}

func TestValidateRejectsAProjectOwnedTwice(t *testing.T) {
	cfg := &Config{Agents: []AgentConfig{
		{Name: "a", Model: "cli/claude/opus", Projects: AgentProjectsConfig{List: []ProjectConfig{{Target: "o/r"}}}},
		{Name: "b", Model: "cli/claude/opus", Projects: AgentProjectsConfig{List: []ProjectConfig{{Target: "o/r"}}}},
	}}
	for _, issue := range Validate(cfg, nil) {
		if issue.Field == "agents[1].projects.list[0]" && issue.Level == LevelError {
			return
		}
	}
	t.Fatalf("issues = %+v", Validate(cfg, nil))
}

func TestValidateWarnsOnATaskForAnotherAgentsProject(t *testing.T) {
	cfg := &Config{Agents: []AgentConfig{
		{Name: "a", Model: "cli/claude/opus", Projects: AgentProjectsConfig{List: []ProjectConfig{{Target: "o/a"}}},
			Tasks: []TaskConfig{{Name: "mine", Schedule: "0 3 * * *", Project: "o/a", Prompt: "x"}}},
		{Name: "b", Model: "cli/claude/opus", Projects: AgentProjectsConfig{List: []ProjectConfig{{Target: "o/b"}}},
			Tasks: []TaskConfig{{Name: "theirs", Schedule: "0 3 * * *", Project: "o/a", Prompt: "x"}}},
	}}
	var fields []string
	for _, issue := range Validate(cfg, nil) {
		if strings.HasSuffix(issue.Field, ".project") {
			fields = append(fields, issue.Field+" "+string(issue.Level))
		}
	}
	if !slices.Equal(fields, []string{"agents[1].tasks[0].project WARN"}) {
		t.Fatalf("project issues = %v", fields)
	}
}
