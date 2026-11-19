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
	if p.EffectiveConcurrency() != DefaultProjectsConcurrency || p.EffectiveInterval() != DefaultProjectsBrowserInterval {
		t.Fatalf("defaults = %d, %s", p.EffectiveConcurrency(), p.EffectiveInterval())
	}
	if p.EffectiveDelivery() != "browser" || p.EffectiveReadyLabel() != "ready" || p.EffectiveFilter() != DefaultProjectsFilter {
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
