package cmd

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/auth"
)

// openURL is swapped out in tests.
var openURL = auth.OpenBrowser

var uiCmd = &cobra.Command{
	Use:   "ui [project]",
	Short: "Open the Projects dashboard in your browser",
	Long: `Open the Projects board in the web UI of the Aviary server.

With a project argument (for example owner/repo), open that project's board.`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project := ""
		if len(args) == 1 {
			project = args[0]
		}
		target := projectsURL(serverURL, project)
		_, _ = fmt.Fprintln(cmd.OutOrStdout(), target)
		if err := openURL(target); err != nil {
			return fmt.Errorf("opening browser: %w", err)
		}
		return nil
	},
}

// projectsURL is the web UI address of the Projects board, or of one
// project's board when project is set.
func projectsURL(server, project string) string {
	u := strings.TrimRight(server, "/") + "/projects"
	if project != "" {
		u += "/" + url.PathEscape(project)
	}
	return u
}

func init() {
	rootCmd.AddCommand(uiCmd)
}
