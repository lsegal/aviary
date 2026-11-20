package mcp

import (
	"context"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/codingskills"
)

// codingSkills is the manager the coding_skills_* tools use; tests replace it.
var codingSkills = codingskills.Default

func registerCodingSkillTools(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "coding_skills_list",
		Description: "List the skills Aviary ships for coding CLIs (gh-fix, gh-discuss, gh-create, gh-safe-merge) and the version installed in each coding CLI on PATH",
	}, func(_ context.Context, _ *sdkmcp.CallToolRequest, _ struct{}) (*sdkmcp.CallToolResult, struct{}, error) {
		return jsonResult(codingSkills().Status())
	})

	type codingSkillsInstallArgs struct {
		Skill      string `json:"skill,omitempty" jsonschema:"Skill to install: gh-fix, gh-discuss, gh-create or gh-safe-merge. Omit for every skill."`
		CLI        string `json:"cli,omitempty" jsonschema:"Coding CLI id from coding_skills_list, such as claude or codex. Omit for every coding CLI on PATH."`
		UpdateOnly bool   `json:"update_only,omitempty" jsonschema:"Only update outdated copies; skip CLIs that do not have the skill yet."`
	}

	addTool(s, &sdkmcp.Tool{
		Name:        "coding_skills_install",
		Description: "Install or update the coding-CLI skills through skills.sh (npx skills add). Copies already current are skipped.",
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args codingSkillsInstallArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		result, err := codingSkills().Install(ctx, codingskills.Request{
			Skill:    args.Skill,
			CLI:      args.CLI,
			Missing:  !args.UpdateOnly,
			Outdated: true,
		})
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(result)
	})
}
