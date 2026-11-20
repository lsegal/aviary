// Package aviary embeds the files that ship from the repository root.
package aviary

import "embed"

// CodingSkillsRoot is where the skills Aviary ships sit in this repository and
// inside CodingSkills. skills.sh installs them from the same path.
const CodingSkillsRoot = ".agents/skills"

// CodingSkillNames are the skills Aviary ships for providers, in display
// order.
var CodingSkillNames = []string{"gh-fix", "gh-discuss", "gh-create", "gh-safe-merge"}

// CodingSkills is the text of the skills Aviary ships that this binary was built with.
// Installed copies are compared against it to find outdated ones.
//
//go:embed .agents/skills/gh-fix .agents/skills/gh-discuss .agents/skills/gh-create .agents/skills/gh-safe-merge
var CodingSkills embed.FS
