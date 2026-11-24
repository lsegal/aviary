package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// The Code Loop agent is focused on the Code Loop, not restricted to it: its
// rules carry no blanket refusal, and it can read the watched repositories
// and their sites while keeping its write tools.
func TestProjectsAgentRulesFocusRatherThanRefuse(t *testing.T) {
	for _, want := range []string{"focused on the Code Loop", "project_github_read", "project_site_fetch", "never refuse a Code Loop question"} {
		assert.Contains(t, ProjectsAgentRules, want)
	}
	for _, gone := range []string{"Refuse anything outside this instance's Code Loop", "never offer to use gh or any other tool outside your own"} {
		assert.NotContains(t, ProjectsAgentRules, gone)
	}
	for _, tool := range []string{"project_github_read", "project_site_fetch", "project_issue_create", "project_issue_comment", "project_job_retry", "project_job_stop", "project_task_create", "project_task_delete"} {
		assert.Contains(t, ProjectsAgentTools, tool)
	}
	// Adding or removing projects and clearing clones stay with the user.
	for _, tool := range []string{"project_add", "project_remove", "project_clone_clear"} {
		assert.NotContains(t, ProjectsAgentTools, tool)
	}
}
