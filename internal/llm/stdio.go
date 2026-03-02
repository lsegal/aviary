package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
)

// StdioProvider runs a CLI tool (e.g. claude, codex) as a subprocess and
// communicates over stdin/stdout using a simple JSON-lines protocol.
type StdioProvider struct {
	command string // e.g. "claude", "codex"
}

// NewStdioProvider creates a provider backed by the given CLI command.
func NewStdioProvider(command string) *StdioProvider {
	return &StdioProvider{command: command}
}

// stdioRequest is the JSON payload sent to the subprocess on stdin.
type stdioRequest struct {
	Messages []Message `json:"messages"`
	System   string    `json:"system,omitempty"`
}

// stdioEvent is a JSON line read from the subprocess stdout.
type stdioEvent struct {
	Type string `json:"type"` // "text" | "error" | "done"
	Text string `json:"text,omitempty"`
	Err  string `json:"error,omitempty"`
}

// Ping validates the stdio provider by checking if the command exists in PATH.
func (p *StdioProvider) Ping(_ context.Context) error {
	_, err := exec.LookPath(p.command)
	if err != nil {
		return fmt.Errorf("stdio command %q not found in PATH: %w", p.command, err)
	}
	return nil
}

// Stream launches the subprocess, writes the request, and reads streaming events.
func (p *StdioProvider) Stream(ctx context.Context, req Request) (<-chan Event, error) {
	cmd := exec.CommandContext(ctx, p.command, "--stream", "--json") //nolint:gosec
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdio stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdio stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %q: %w", p.command, err)
	}

	payload, _ := json.Marshal(stdioRequest{Messages: req.Messages, System: req.System})
	_, _ = fmt.Fprintf(stdin, "%s\n", payload)
	_ = stdin.Close()

	ch := make(chan Event, 32)
	go func() {
		defer close(ch)
		defer cmd.Wait() //nolint:errcheck

		scanner := bufio.NewScanner(stdout)
		for scanner.Scan() {
			var e stdioEvent
			if err := json.Unmarshal(scanner.Bytes(), &e); err != nil {
				continue
			}
			switch e.Type {
			case "text":
				ch <- Event{Type: EventTypeText, Text: e.Text}
			case "error":
				ch <- Event{Type: EventTypeError, Error: fmt.Errorf("%s", e.Err)}
				return
			case "done":
				ch <- Event{Type: EventTypeDone}
				return
			}
		}
		ch <- Event{Type: EventTypeDone}
	}()

	return ch, nil
}
