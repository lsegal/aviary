package scheduler

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

type jobLogBuilder struct {
	lines []string
}

func (b *jobLogBuilder) Addf(format string, args ...any) {
	line := strings.TrimSpace(fmt.Sprintf(format, args...))
	if line == "" {
		return
	}
	b.lines = append(b.lines, line)
}

func (b *jobLogBuilder) AddBlock(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	b.lines = append(b.lines, text)
}

func (b *jobLogBuilder) String() string {
	return strings.TrimSpace(strings.Join(b.lines, "\n"))
}

type persistedToolEvent struct {
	Name   string         `json:"name"`
	Args   map[string]any `json:"args,omitempty"`
	Result string         `json:"result,omitempty"`
	Error  string         `json:"error,omitempty"`
}

func collectJobSessionToolLogs(agentID, sessionID string, startedAt, endedAt time.Time) (string, error) {
	if strings.TrimSpace(agentID) == "" || strings.TrimSpace(sessionID) == "" {
		return "", nil
	}
	path := store.FindSessionPath(agentID, sessionID)
	if path == "" {
		return "", nil
	}
	messages, err := store.ReadJSONL[domain.Message](path)
	if err != nil {
		return "", err
	}
	var logs jobLogBuilder
	for _, msg := range messages {
		if msg.Role != domain.MessageRoleTool {
			continue
		}
		if !startedAt.IsZero() && msg.Timestamp.Before(startedAt) {
			continue
		}
		if !endedAt.IsZero() && msg.Timestamp.After(endedAt) {
			continue
		}
		logs.AddBlock(formatToolMessageLog(msg))
	}
	return logs.String(), nil
}

func formatToolMessageLog(msg domain.Message) string {
	content := strings.TrimSpace(msg.Content)
	if content == "" {
		return ""
	}
	var event persistedToolEvent
	if err := json.Unmarshal([]byte(content), &event); err != nil {
		return content
	}

	var lines []string
	line := "tool"
	if !msg.Timestamp.IsZero() {
		line += " [" + msg.Timestamp.Format(time.RFC3339Nano) + "]"
	}
	if strings.TrimSpace(event.Name) != "" {
		line += " " + strings.TrimSpace(event.Name)
	}
	lines = append(lines, line)
	if len(event.Args) > 0 {
		if encoded, err := json.MarshalIndent(event.Args, "", "  "); err == nil {
			lines = append(lines, "args:")
			lines = append(lines, string(encoded))
		}
	}
	if text := strings.TrimSpace(event.Result); text != "" {
		lines = append(lines, "result:")
		lines = append(lines, prettyLogText(text))
	}
	if text := strings.TrimSpace(event.Error); text != "" {
		lines = append(lines, "error:")
		lines = append(lines, text)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func prettyLogText(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	var decoded any
	if err := json.Unmarshal([]byte(text), &decoded); err == nil {
		if pretty, err := json.MarshalIndent(decoded, "", "  "); err == nil {
			return string(pretty)
		}
	}
	return text
}
