package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/browser"
	internalmcp "github.com/lsegal/aviary/internal/mcp"
)

var (
	browserBinary     string
	browserCDPPort    int
	browserProfileDir string
	browserTabID      string
)

var browserCmd = &cobra.Command{
	Use:   "browser",
	Short: "Control a Chromium browser via CDP",
	PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
		if internalmcp.GetDeps().Browser == nil {
			internalmcp.GetDeps().Browser = browser.NewManager(browserBinary, browserCDPPort, browserProfileDir, false, true)
		}
		return nil
	},
}

var (
	browserSelector string
	browserText     string
	browserCount    int
	browserMaxLen   int
	browserMaxText  int
	browserTimeout  int
	browserHTML     bool
)

// browserToolSpec describes a browser subcommand that forwards to a browser_*
// MCP tool and prints its result.
type browserToolSpec struct {
	use             string
	short           string
	args            cobra.PositionalArgs
	tool            string
	requireTab      bool
	requireSelector bool
	// params builds the tool arguments after the tab/selector checks pass.
	params func(args []string) (any, error)
}

// newBrowserToolCmd builds a cobra command from a browserToolSpec.
func newBrowserToolCmd(spec browserToolSpec) *cobra.Command {
	return &cobra.Command{
		Use:   spec.use,
		Short: spec.short,
		Args:  spec.args,
		RunE: func(cmd *cobra.Command, args []string) error {
			if spec.requireTab && browserTabID == "" {
				return fmt.Errorf("--tab is required")
			}
			if spec.requireSelector && browserSelector == "" {
				return fmt.Errorf("--selector is required")
			}
			params, err := spec.params(args)
			if err != nil {
				return err
			}
			return printToolResult(cmd, spec.tool, params)
		},
	}
}

// browserTextParams builds tab/selector/text arguments, preferring a
// positional text argument over --text.
func browserTextParams(args []string) (any, error) {
	text := browserText
	if len(args) > 0 {
		text = args[0]
	}
	if text == "" {
		return nil, fmt.Errorf("text is required: pass as argument or use --text")
	}
	return map[string]any{
		"tab_id":   browserTabID,
		"selector": browserSelector,
		"text":     text,
	}, nil
}

var browserOpenCmd = newBrowserToolCmd(browserToolSpec{
	use:   "open <url>",
	short: "Open a URL in a new browser tab and print its tab ID",
	args:  cobra.ExactArgs(1),
	tool:  "browser_open",
	params: func(args []string) (any, error) {
		return map[string]any{"url": args[0]}, nil
	},
})

var browserTabsCmd = newBrowserToolCmd(browserToolSpec{
	use:   "tabs",
	short: "List all open browser tabs",
	tool:  "browser_tabs",
	params: func([]string) (any, error) {
		return nil, nil
	},
})

var browserClickCmd = newBrowserToolCmd(browserToolSpec{
	use:             "click",
	short:           "Click an element by CSS selector",
	tool:            "browser_click",
	requireTab:      true,
	requireSelector: true,
	params: func([]string) (any, error) {
		return map[string]any{
			"tab_id":   browserTabID,
			"selector": browserSelector,
		}, nil
	},
})

var browserNavigateCmd = newBrowserToolCmd(browserToolSpec{
	use:        "navigate <url>",
	short:      "Navigate an existing tab to a new URL",
	args:       cobra.ExactArgs(1),
	tool:       "browser_navigate",
	requireTab: true,
	params: func(args []string) (any, error) {
		return map[string]any{
			"tab_id": browserTabID,
			"url":    args[0],
		}, nil
	},
})

var browserTypeCmd = newBrowserToolCmd(browserToolSpec{
	use:             "type [text]",
	short:           "Send keystrokes into an element",
	args:            cobra.MaximumNArgs(1),
	tool:            "browser_keystroke",
	requireTab:      true,
	requireSelector: true,
	params:          browserTextParams,
})

var browserFillCmd = newBrowserToolCmd(browserToolSpec{
	use:             "fill [text]",
	short:           "Fill text into an element (default typing)",
	args:            cobra.MaximumNArgs(1),
	tool:            "browser_fill",
	requireTab:      true,
	requireSelector: true,
	params:          browserTextParams,
})

var browserScreenshotCmd = newBrowserToolCmd(browserToolSpec{
	use:        "screenshot",
	short:      "Capture a screenshot of a tab",
	tool:       "browser_screenshot",
	requireTab: true,
	params: func([]string) (any, error) {
		return map[string]any{"tab_id": browserTabID}, nil
	},
})

var browserWaitCmd = newBrowserToolCmd(browserToolSpec{
	use:             "wait",
	short:           "Wait for an element to become visible",
	tool:            "browser_wait",
	requireTab:      true,
	requireSelector: true,
	params: func([]string) (any, error) {
		return map[string]any{
			"tab_id":     browserTabID,
			"selector":   browserSelector,
			"timeout_ms": browserTimeout,
		}, nil
	},
})

var browserTextCmd = newBrowserToolCmd(browserToolSpec{
	use:        "text",
	short:      "Extract normalized page text",
	tool:       "browser_text",
	requireTab: true,
	params: func([]string) (any, error) {
		return map[string]any{
			"tab_id":     browserTabID,
			"selector":   browserSelector,
			"max_length": browserMaxLen,
		}, nil
	},
})

var browserQueryCmd = newBrowserToolCmd(browserToolSpec{
	use:             "query",
	short:           "Extract structured element data by selector",
	tool:            "browser_query",
	requireTab:      true,
	requireSelector: true,
	params: func([]string) (any, error) {
		return map[string]any{
			"tab_id":          browserTabID,
			"selector":        browserSelector,
			"count":           browserCount,
			"max_text_length": browserMaxText,
			"include_html":    browserHTML,
		}, nil
	},
})

var browserEvalCmd = newBrowserToolCmd(browserToolSpec{
	use:        "eval <expr>",
	short:      "Evaluate JavaScript in a tab",
	args:       cobra.ExactArgs(1),
	tool:       "browser_eval",
	requireTab: true,
	params: func(args []string) (any, error) {
		return map[string]any{
			"tab_id":     browserTabID,
			"javascript": args[0],
		}, nil
	},
})

func init() {
	browserCmd.PersistentFlags().StringVar(&browserBinary, "browser-binary", "", "path to Chromium/Chrome binary (auto-detected if empty)")
	browserCmd.PersistentFlags().IntVar(&browserCDPPort, "cdp-port", 0, "Chrome DevTools Protocol debugging port (default: 9222)")
	browserCmd.PersistentFlags().StringVar(&browserProfileDir, "profile-directory", "", "Chrome profile directory name (e.g. Default, Work)")
	browserCmd.PersistentFlags().StringVar(&browserTabID, "tab", "", "CDP tab ID returned by 'browser open'")

	browserClickCmd.Flags().StringVar(&browserSelector, "selector", "", "CSS selector")
	browserTypeCmd.Flags().StringVar(&browserSelector, "selector", "", "CSS selector")
	browserTypeCmd.Flags().StringVar(&browserText, "text", "", "text for keystrokes")
	browserFillCmd.Flags().StringVar(&browserSelector, "selector", "", "CSS selector")
	browserFillCmd.Flags().StringVar(&browserText, "text", "", "text to fill/type")
	browserWaitCmd.Flags().StringVar(&browserSelector, "selector", "", "CSS selector")
	browserWaitCmd.Flags().IntVar(&browserTimeout, "timeout-ms", 10000, "wait timeout in milliseconds")
	browserTextCmd.Flags().StringVar(&browserSelector, "selector", "", "optional CSS selector")
	browserTextCmd.Flags().IntVar(&browserMaxLen, "max-length", 4000, "maximum text length to return")
	browserQueryCmd.Flags().StringVar(&browserSelector, "selector", "", "CSS selector")
	browserQueryCmd.Flags().IntVar(&browserCount, "count", 20, "maximum number of elements to return")
	browserQueryCmd.Flags().IntVar(&browserMaxText, "max-length", 500, "maximum text length per element")
	browserQueryCmd.Flags().BoolVar(&browserHTML, "include-html", false, "include outer HTML for each element")

	browserCmd.AddCommand(browserOpenCmd, browserTabsCmd, browserNavigateCmd, browserWaitCmd, browserTextCmd, browserQueryCmd, browserClickCmd, browserTypeCmd, browserFillCmd, browserScreenshotCmd, browserEvalCmd)
	rootCmd.AddCommand(browserCmd)
}
