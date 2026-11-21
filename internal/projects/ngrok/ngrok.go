// Package ngrok starts and supervises the ngrok tunnel that carries GitHub
// webhook deliveries to the Code Loop engine.
package ngrok

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/process"
)

// Tunnel owns the ngrok process and the public URL it assigned.
type Tunnel struct {
	cmd       *exec.Cmd
	publicURL string
}

// URL is the tunnel's public URL.
func (t *Tunnel) URL() string { return t.publicURL }

// Close stops the tunnel.
func (t *Tunnel) Close() error {
	if t == nil || t.cmd == nil {
		return nil
	}
	return process.Stop(t.cmd)
}

// ngrokLogRecord is the subset of ngrok's JSON log records aviary reads.
type ngrokLogRecord struct {
	Level   string `json:"lvl"`
	Message string `json:"msg"`
	URL     string `json:"url"`
	Err     string `json:"err"`
}

// ngrokLogWatcher reads the tunnel's public URL out of the log stream of the
// ngrok process aviary itself started. The local ngrok API cannot be used for
// this: it lives on a fixed port (4040) owned by whichever agent bound it
// first, so a second agent — an orphaned aviary tunnel, or an unrelated one —
// silently moves aviary's own API to 4041 and aviary adopts a stranger's tunnel
// URL, pointing the GitHub webhook at a dead port.
//
// Only failures are forwarded to the wrapped writer, which keeps ngrok's
// routine chatter (including a benign Windows agent-path diagnostic) out of
// aviary's UI while still showing the user why a tunnel never came up.
type ngrokLogWatcher struct {
	mu      sync.Mutex
	out     io.Writer
	pending []byte
	url     string
	failure []string
}

func (w *ngrokLogWatcher) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.pending = append(w.pending, p...)
	for {
		index := bytes.IndexByte(w.pending, '\n')
		if index < 0 {
			break
		}
		line := string(w.pending[:index])
		w.pending = w.pending[index+1:]
		w.consume(line)
	}
	return len(p), nil
}

// consume interprets one log line, recording the tunnel URL and any failure.
func (w *ngrokLogWatcher) consume(line string) {
	line = strings.TrimRight(line, "\r")
	if strings.TrimSpace(line) == "" {
		return
	}
	var record ngrokLogRecord
	if err := json.Unmarshal([]byte(line), &record); err != nil {
		if npmProgress(line) {
			return
		}
		// Not a JSON record, so it cannot be classified; show it rather than
		// swallow output that may explain a failure.
		w.record(line, true)
		return
	}
	if record.Message == "started tunnel" && record.URL != "" && w.url == "" {
		w.url = strings.TrimRight(record.URL, "/")
	}
	if ngrokLogFailure(record.Level) {
		w.record(ngrokLogMessage(record), ngrokLogFatal(record.Level))
	}
}

// record keeps a failure for the error aviary returns if no tunnel comes up.
// Only fatal records are also printed, matching what the previous
// critical-only log level showed: ngrok reports recoverable problems (such as
// a host-info probe it could not run) at error level, and those would
// otherwise clutter aviary's UI on an otherwise healthy tunnel.
func (w *ngrokLogWatcher) record(message string, fatal bool) {
	w.failure = append(w.failure, message)
	if fatal && w.out != nil {
		_, _ = fmt.Fprintln(w.out, message)
	}
}

// URL is the tunnel's public URL.
func (w *ngrokLogWatcher) URL() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.url
}

// Failure summarizes the error output ngrok produced, for the error aviary
// returns when no tunnel is established.
func (w *ngrokLogWatcher) Failure() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return strings.Join(w.failure, "; ")
}

// ngrokLogFailure reports whether a log level means ngrok could not do its
// job. ngrok spells its error level both ways depending on log format.
func ngrokLogFailure(level string) bool {
	switch strings.ToLower(level) {
	case "eror", "error", "crit", "fatal":
		return true
	}
	return false
}

// ngrokLogFatal reports whether a log level means ngrok is giving up, as
// opposed to reporting a problem it recovered from.
func ngrokLogFatal(level string) bool {
	switch strings.ToLower(level) {
	case "crit", "fatal":
		return true
	}
	return false
}

func ngrokLogMessage(record ngrokLogRecord) string {
	message := "ngrok: " + record.Message
	if record.Err != "" && record.Err != "<nil>" {
		message += ": " + strings.TrimSpace(record.Err)
	}
	return message
}

// Start starts an ngrok tunnel to listenAddr and waits for its public URL.
func Start(ctx context.Context, binary, listenAddr string, out io.Writer) (*Tunnel, error) {
	invocation, err := resolveCommand(binary)
	if err != nil {
		return nil, err
	}
	if invocation.viaNpx && out != nil {
		_, _ = fmt.Fprintf(out, "no %s executable found; running ngrok through %s\n", binary, npxBinary)
	}
	// Agents abandoned by an earlier run hold the local ngrok API port and one
	// of the account's simultaneous agent sessions, so clear them out before
	// asking for a tunnel of aviary's own.
	reapOrphanedNgrokAgents(out)
	watcher := &ngrokLogWatcher{out: out}
	cmd := exec.CommandContext(ctx, invocation.name, invocation.args(listenAddr)...)
	cmd.Stdout, cmd.Stderr = watcher, watcher
	if err := process.Start(cmd); err != nil {
		return nil, fmt.Errorf("start ngrok: %w", err)
	}
	tunnel := &Tunnel{cmd: cmd}
	deadline := time.NewTimer(invocation.timeout())
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if tunnel.publicURL = watcher.URL(); tunnel.publicURL != "" {
			return tunnel, nil
		}
		select {
		case <-ctx.Done():
			_ = tunnel.Close()
			return nil, ctx.Err()
		case <-deadline.C:
			_ = tunnel.Close()
			return nil, ngrokStartError(watcher.Failure())
		case <-ticker.C:
		}
	}
}

func ngrokStartError(failure string) error {
	if failure == "" {
		return fmt.Errorf("wait for ngrok tunnel: timed out")
	}
	return fmt.Errorf("wait for ngrok tunnel: timed out (%s)", failure)
}

// ngrokArgs keeps ngrok from entering its interactive terminal dashboard,
// which clears aviary's UI, and asks for JSON records so aviary can read the
// tunnel URL of this exact process out of the stream. Info level is required
// because the "started tunnel" record carrying the URL is logged at it;
// ngrokLogWatcher filters the stream back down to failures.
func ngrokArgs(listenAddr string) []string {
	return []string{"http", "--log=stdout", "--log-format=json", "--log-level=info", listenAddr}
}

// WebhookURL joins a tunnel's public URL and the webhook path.
func WebhookURL(publicURL, webhookPath string) (string, error) {
	u, err := url.Parse(strings.TrimRight(publicURL, "/"))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("invalid ngrok public URL %q", publicURL)
	}
	if webhookPath == "" {
		webhookPath = "/webhook"
	}
	if !strings.HasPrefix(webhookPath, "/") {
		webhookPath = "/" + webhookPath
	}
	u.Path = strings.TrimRight(webhookPath, "/")
	return u.String(), nil
}

// IsURL reports whether value is an http or https URL.
func IsURL(value string) bool {
	u, err := url.Parse(value)
	return err == nil && strings.Contains(strings.ToLower(u.Hostname()), "ngrok")
}
