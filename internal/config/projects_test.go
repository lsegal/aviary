package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func boolPtr(b bool) *bool { return &b }

func TestSaveProjectsWritesOnlyDifferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	cfg := Default()
	cfg.Projects = []ProjectConfig{
		{
			// Every value equals its default, so the entry is dropped.
			Target:     "lsegal/zvidlib",
			Jobs:       DefaultProjectJobs,
			ReadyLabel: DefaultProjectReadyLabel,
			Delivery:   DefaultProjectDelivery,
			Filters:    DefaultProjectFilters,
			Merge:      boolPtr(true),
			Changelog:  boolPtr(true),
			Stack:      boolPtr(true),
			Commenters: []string{" "},
		},
		{
			Target:     "lsegal/aviary",
			Jobs:       DefaultProjectJobs,
			ReadyLabel: "agent-ready",
			Commenters: []string{"lsegal", "teammate"},
			Merge:      boolPtr(false),
			Changelog:  boolPtr(true),
			CLIs:       []string{"claude/opus", "codex/gpt-5.5"},
			ReportTo:   "atlas",
			ReportOn:   DefaultProjectReportOn,
		},
	}
	assert.NoError(t, Save(path, &cfg))

	data, err := os.ReadFile(path)
	assert.NoError(t, err)
	text := string(data)
	assert.NotContains(t, text, "zvidlib")
	for _, absent := range []string{"jobs:", "changelog:", "report_on:", "delivery:", "stack:"} {
		assert.NotContains(t, text, absent)
	}
	for _, present := range []string{"target: lsegal/aviary", "ready_label: agent-ready", "merge: false", "report_to: atlas", "- claude/opus"} {
		assert.Contains(t, text, present)
	}

	loaded, err := Load(path)
	assert.NoError(t, err)
	assert.Len(t, loaded.Projects, 1)
	eff := EffectiveProject(loaded.Projects[0])
	assert.Equal(t, DefaultProjectJobs, eff.Jobs)
	assert.Equal(t, "agent-ready", eff.ReadyLabel)
	assert.False(t, *eff.Merge)
	assert.True(t, *eff.Changelog)
	assert.Equal(t, DefaultProjectReportOn, eff.ReportOn)
}

func TestSaveWithoutProjectsOmitsSection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	cfg := Default()
	cfg.Projects = []ProjectConfig{{Target: "lsegal/aviary"}}
	assert.NoError(t, Save(path, &cfg))
	data, err := os.ReadFile(path)
	assert.NoError(t, err)
	assert.NotContains(t, string(data), "projects")
}

func TestEffectiveProjectDefaults(t *testing.T) {
	eff := EffectiveProject(ProjectConfig{Target: "o/r"})
	assert.Equal(t, DefaultProjectJobs, eff.Jobs)
	assert.Equal(t, DefaultProjectReadyLabel, eff.ReadyLabel)
	assert.Equal(t, ProjectDeliveryBrowser, eff.Delivery)
	assert.Equal(t, DefaultProjectFilters, eff.Filters)
	assert.True(t, *eff.Merge && *eff.Changelog && *eff.Stack)
	assert.Empty(t, eff.ReportOn, "no report events without report_to")

	all := EffectiveProject(ProjectConfig{Target: "o/r", AllIssues: true})
	assert.Empty(t, all.Filters)
}

func TestFindProject(t *testing.T) {
	cfg := &Config{Projects: []ProjectConfig{{Target: "LSegal/Aviary", Jobs: 2}}}
	p, ok := FindProject(cfg, "lsegal/aviary")
	assert.True(t, ok)
	assert.Equal(t, 2, p.Jobs)
	p, ok = FindProject(cfg, "lsegal/other")
	assert.False(t, ok)
	assert.Equal(t, "lsegal/other", p.Target)
}

func TestValidateProjects(t *testing.T) {
	cfg := &Config{Projects: []ProjectConfig{
		{Target: "o/r", Jobs: -1, Delivery: "carrier-pigeon", Interval: "soon", ReportOn: []string{"lunch"}},
		{Target: "O/R"},
		{Target: ""},
		{Target: "o/s", Schedules: []ProjectSchedule{{Schedule: "not cron", Prompt: ""}}},
		{Target: "o/t", ReportOn: []string{ProjectReportMerged}},
	}}
	var errs, warns []string
	for _, issue := range Validate(cfg, nil) {
		if issue.Level == LevelError {
			errs = append(errs, issue.Field)
		} else {
			warns = append(warns, issue.Field)
		}
	}
	joined := strings.Join(errs, " ")
	for _, field := range []string{
		"projects[0].jobs", "projects[0].delivery", "projects[0].interval", "projects[0].report_on",
		"projects[1].target", "projects[2].target",
		"projects[3].schedules[0].schedule", "projects[3].schedules[0].prompt",
	} {
		assert.Contains(t, joined, field)
	}
	assert.Contains(t, warns, "projects[4].report_on")

	ok := &Config{Projects: []ProjectConfig{{
		Target: "o/r", Delivery: ProjectDeliveryPoll, Interval: "45s", ReportTo: "atlas",
		Schedules: []ProjectSchedule{{Schedule: "0 2 * * 1-5", Prompt: "Triage new issues"}},
	}}}
	for _, issue := range Validate(ok, nil) {
		assert.NotEqual(t, LevelError, issue.Level, issue.Field+": "+issue.Message)
	}
}

func TestSchemaListsProjectFields(t *testing.T) {
	s := string(Schema())
	for _, key := range []string{`"projects"`, `"ready_label"`, `"commenters"`, `"temp_dirs"`, `"schedules"`} {
		assert.Contains(t, s, key)
	}
}
