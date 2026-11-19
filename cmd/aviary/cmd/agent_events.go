package cmd

import (
	"encoding/json"
	"io"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/mcp"
)

// agentEventWriter prints an agent run's structured events as JSON lines,
// joining the reply's text fragments into one message event per stretch of
// text between tool calls.
type agentEventWriter struct {
	mu       sync.Mutex
	out      io.Writer
	text     strings.Builder
	messages bool
	errors   bool
}

// progress receives one agent_run progress message.
func (w *agentEventWriter) progress(message string) {
	var event mcp.AgentRunEvent
	if json.Unmarshal([]byte(message), &event) != nil || event.Type == "" {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if event.Type == "text" {
		w.text.WriteString(event.Text)
		return
	}
	w.flushText()
	w.write(event)
}

// finish prints what the run left: its last message, and the tool's result
// when no event carried it, as a server that streams no events sends.
func (w *agentEventWriter) finish(result string, isError bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushText()
	switch {
	case isError && !w.errors && result != "":
		w.write(mcp.AgentRunEvent{Type: "error", Text: result})
	case !isError && !w.messages && strings.TrimSpace(result) != "":
		w.write(mcp.AgentRunEvent{Type: "message", Text: result})
	}
}

func (w *agentEventWriter) flushText() {
	text := w.text.String()
	w.text.Reset()
	if strings.TrimSpace(text) == "" {
		return
	}
	w.write(mcp.AgentRunEvent{Type: "message", Text: text})
}

func (w *agentEventWriter) write(event mcp.AgentRunEvent) {
	switch event.Type {
	case "message":
		w.messages = true
	case "error":
		w.errors = true
	}
	data, err := json.Marshal(event)
	if err != nil {
		return
	}
	_, _ = w.out.Write(append(data, '\n'))
}
