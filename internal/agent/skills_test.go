package agent

import (
	"strings"
	"testing"
)

func TestParseSkillFileFrontmatter(t *testing.T) {
	data := []byte(`---
name: gogcli
description: Run Google Workspace actions through gogcli.
---
Use gogcli for Gmail and Calendar tasks.
`)

	got, err := parseSkillFile("default-name", data)
	if err != nil {
		t.Fatalf("parseSkillFile: %v", err)
	}
	if got.Name != "gogcli" {
		t.Fatalf("expected skill name gogcli, got %q", got.Name)
	}
	if got.Description != "Run Google Workspace actions through gogcli." {
		t.Fatalf("unexpected description %q", got.Description)
	}
	if got.Content != "Use gogcli for Gmail and Calendar tasks." {
		t.Fatalf("unexpected content %q", got.Content)
	}
}

func TestBuildSystemPromptIncludesSkillDescription(t *testing.T) {
	prompt := BuildSystemPrompt("Base prompt", []Skill{{
		Name:        "gogcli",
		Description: "Run Google Workspace commands.",
		Content:     "Prefer JSON output.",
	}})

	if !strings.Contains(prompt, `<skill name="gogcli" description="Run Google Workspace commands.">`) {
		t.Fatalf("expected prompt to include skill tag with description, got %q", prompt)
	}
	if !strings.Contains(prompt, "Prefer JSON output.") {
		t.Fatalf("expected prompt to include skill content, got %q", prompt)
	}
}
