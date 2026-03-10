package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseSkillFileFrontmatter(t *testing.T) {
	data := []byte(`---
name: gogcli
description: Run Google Workspace actions through gogcli.
---
Use gogcli for Gmail and Calendar tasks.
`)

	got, err := parseSkillFile("default-name", data)
	assert.NoError(t, err)
	assert.Equal(t, "gogcli", got.Name)
	assert.Equal(t, "Run Google Workspace actions through gogcli.", got.Description)
	assert.Equal(t, "Use gogcli for Gmail and Calendar tasks.", got.Content)

}

func TestBuildSystemPromptIncludesSkillDescription(t *testing.T) {
	prompt := BuildSystemPrompt("Base prompt", []Skill{{
		Name:        "gogcli",
		Description: "Run Google Workspace commands.",
		Content:     "Prefer JSON output.",
	}})
	assert.Contains(t, prompt, `<skill name="gogcli" description="Run Google Workspace commands.">`)
	assert.Contains(t, prompt, "Prefer JSON output.")

}
