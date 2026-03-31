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

var browserOpenCmd = &cobra.Command{
	Use:   "open <url>",
	Short: "Open a URL in a new browser tab and print its tab ID",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "browser_open", map[string]any{"url": args[0]})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var browserTabsCmd = &cobra.Command{
	Use:   "tabs",
	Short: "List all open browser tabs",
	RunE: func(cmd *cobra.Command, _ []string) error {
		out, err := dispatcher.CallTool(cmd.Context(), "browser_tabs", nil)
		if err != nil {
			return err
		}
		fmt.Println(out)
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

var browserClickCmd = &cobra.Command{
	Use:   "click",
	Short: "Click an element by CSS selector",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if browserTabID == "" {
			return fmt.Errorf("--tab is required")
		}
		if browserSelector == "" {
			return fmt.Errorf("--selector is required")
		}
		out, err := dispatcher.CallTool(cmd.Context(), "browser_click", map[string]any{
			"tab_id":   browserTabID,
			"selector": browserSelector,
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var browserNavigateCmd = &cobra.Command{
	Use:   "navigate <url>",
	Short: "Navigate an existing tab to a new URL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if browserTabID == "" {
			return fmt.Errorf("--tab is required")
		}
		out, err := dispatcher.CallTool(cmd.Context(), "browser_navigate", map[string]any{
			"tab_id": browserTabID,
			"url":    args[0],
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var browserTypeCmd = &cobra.Command{
	Use:   "type [text]",
	Short: "Send keystrokes into an element",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if browserTabID == "" {
			return fmt.Errorf("--tab is required")
		}
		if browserSelector == "" {
			return fmt.Errorf("--selector is required")
		}
		text := browserText
		if len(args) > 0 {
			text = args[0]
		}
		if text == "" {
			return fmt.Errorf("text is required: pass as argument or use --text")
		}
		out, err := dispatcher.CallTool(cmd.Context(), "browser_keystroke", map[string]any{
			"tab_id":   browserTabID,
			"selector": browserSelector,
			"text":     text,
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var browserFillCmd = &cobra.Command{
	Use:   "fill [text]",
	Short: "Fill text into an element (default typing)",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if browserTabID == "" {
			return fmt.Errorf("--tab is required")
		}
		if browserSelector == "" {
			return fmt.Errorf("--selector is required")
		}
		text := browserText
		if len(args) > 0 {
			text = args[0]
		}
		if text == "" {
			return fmt.Errorf("text is required: pass as argument or use --text")
		}
		out, err := dispatcher.CallTool(cmd.Context(), "browser_fill", map[string]any{
			"tab_id":   browserTabID,
			"selector": browserSelector,
			"text":     text,
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var browserScreenshotCmd = &cobra.Command{
	Use:   "screenshot",
	Short: "Capture a screenshot of a tab",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if browserTabID == "" {
			return fmt.Errorf("--tab is required")
		}
		out, err := dispatcher.CallTool(cmd.Context(), "browser_screenshot", map[string]any{
			"tab_id": browserTabID,
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var browserWaitCmd = &cobra.Command{
	Use:   "wait",
	Short: "Wait for an element to become visible",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if browserTabID == "" {
			return fmt.Errorf("--tab is required")
		}
		if browserSelector == "" {
			return fmt.Errorf("--selector is required")
		}
		out, err := dispatcher.CallTool(cmd.Context(), "browser_wait", map[string]any{
			"tab_id":     browserTabID,
			"selector":   browserSelector,
			"timeout_ms": browserTimeout,
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var browserTextCmd = &cobra.Command{
	Use:   "text",
	Short: "Extract normalized page text",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if browserTabID == "" {
			return fmt.Errorf("--tab is required")
		}
		out, err := dispatcher.CallTool(cmd.Context(), "browser_text", map[string]any{
			"tab_id":     browserTabID,
			"selector":   browserSelector,
			"max_length": browserMaxLen,
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var browserQueryCmd = &cobra.Command{
	Use:   "query",
	Short: "Extract structured element data by selector",
	RunE: func(cmd *cobra.Command, _ []string) error {
		if browserTabID == "" {
			return fmt.Errorf("--tab is required")
		}
		if browserSelector == "" {
			return fmt.Errorf("--selector is required")
		}
		out, err := dispatcher.CallTool(cmd.Context(), "browser_query", map[string]any{
			"tab_id":          browserTabID,
			"selector":        browserSelector,
			"count":           browserCount,
			"max_text_length": browserMaxText,
			"include_html":    browserHTML,
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

var browserEvalCmd = &cobra.Command{
	Use:   "eval <expr>",
	Short: "Evaluate JavaScript in a tab",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if browserTabID == "" {
			return fmt.Errorf("--tab is required")
		}
		out, err := dispatcher.CallTool(cmd.Context(), "browser_eval", map[string]any{
			"tab_id":     browserTabID,
			"javascript": args[0],
		})
		if err != nil {
			return err
		}
		fmt.Println(out)
		return nil
	},
}

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
