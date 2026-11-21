package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/codingskills"
)

// installSkillsManager is the coding skills manager install-skills uses;
// tests replace it.
var installSkillsManager = codingskills.Default

var installSkillsCmd = &cobra.Command{
	Use:   "install-skills",
	Short: "Install the gh-* skills globally with npx skills",
	Long: `Install the skills Aviary ships (gh-create, gh-discuss, gh-fix and
gh-safe-merge) globally with npx skills (skills.sh): into ~/.agents/skills and
the skills directory of every CLI provider on PATH, so they work for Aviary's
agents and in the CLIs directly.

A release installs the skills from its own tag; a development build installs
the copies bundled with it. Skills already installed are refreshed, so running
it again is safe. Aviary runs the same install on its first launch.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, _ []string) error {
		result, err := installSkillsManager().InstallGlobal(cmd.Context())
		for _, skill := range result.Installed {
			fmt.Fprintf(cmd.OutOrStdout(), "installed %s\n", skill)
		}
		return err
	},
}

func init() {
	rootCmd.AddCommand(installSkillsCmd)
}
