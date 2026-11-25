package notify

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// SystemNotifier returns how a server shows a notification through the OS
// when no browser is connected, or nil where there is none. On macOS it uses
// terminal-notifier when installed, so clicking opens the linked page at
// baseURL, and otherwise osascript, whose Notification Center entry opens
// Script Editor. Banner or alert style is set per app in System Settings →
// Notifications.
func SystemNotifier(baseURL func() string) func(Notification) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return func(n Notification) error {
		return run(darwinCommand(n, baseURL(), lookPath))
	}
}

var lookPath = exec.LookPath

// darwinCommand is the command that shows n on macOS.
func darwinCommand(n Notification, baseURL string, look func(string) (string, error)) []string {
	title, body := n.Title, n.Body
	if body == "" {
		body = title
		title = ""
	}
	if path, err := look("terminal-notifier"); err == nil {
		args := []string{path, "-title", "Aviary", "-message", body, "-group", "aviary-" + n.Type}
		if title != "" {
			args = append(args, "-subtitle", title)
		}
		if n.Link != "" && baseURL != "" {
			args = append(args, "-open", strings.TrimRight(baseURL, "/")+n.Link)
		}
		return args
	}
	// The text is passed as arguments, never spliced into the script.
	return []string{"osascript",
		"-e", "on run argv",
		"-e", `display notification (item 2 of argv) with title "Aviary" subtitle (item 1 of argv)`,
		"-e", "end run",
		title, body}
}

func run(args []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, args[0], args[1:]...).Run()
}
