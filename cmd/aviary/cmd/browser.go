package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

var browserCmd = &cobra.Command{
	Use:   "browser",
	Short: "Control a Chromium browser via CDP",
}

var browserOpenCmd = &cobra.Command{
	Use:   "open <url>",
	Short: "Navigate to a URL",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Opening %q (not yet implemented)\n", args[0])
		return nil
	},
}

var (
	browserSelector string
	browserText     string
)

var browserClickCmd = &cobra.Command{
	Use:   "click",
	Short: "Click an element by CSS selector",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("Clicking %q (not yet implemented)\n", browserSelector)
		return nil
	},
}

var browserTypeCmd = &cobra.Command{
	Use:   "type [text]",
	Short: "Type text into an element",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		text := browserText
		if len(args) > 0 {
			text = args[0]
		}
		fmt.Printf("Typing into %q: %q (not yet implemented)\n", browserSelector, text)
		return nil
	},
}

var browserScreenshotCmd = &cobra.Command{
	Use:   "screenshot",
	Short: "Capture a screenshot",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Screenshot captured (not yet implemented)")
		return nil
	},
}

var browserCloseCmd = &cobra.Command{
	Use:   "close",
	Short: "Close the browser session",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Browser closed (not yet implemented)")
		return nil
	},
}

func init() {
	browserClickCmd.Flags().StringVar(&browserSelector, "selector", "", "CSS selector")
	browserTypeCmd.Flags().StringVar(&browserSelector, "selector", "", "CSS selector")
	browserTypeCmd.Flags().StringVar(&browserText, "text", "", "text to type")
	browserCmd.AddCommand(browserOpenCmd, browserClickCmd, browserTypeCmd, browserScreenshotCmd, browserCloseCmd)
	rootCmd.AddCommand(browserCmd)
}
