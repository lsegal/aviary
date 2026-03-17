package store

import (
	"os"
	"path/filepath"
	"time"
)

// ChatLogEntry is a single message in a group channel's chat log.
type ChatLogEntry struct {
	From      string    `json:"from"`      // user ID or name who sent the message
	Role      string    `json:"role"`      // "user" or "assistant"
	Text      string    `json:"text"`      // message text
	Timestamp time.Time `json:"timestamp"` // when received
}

// ChatLogPath returns the path for a channel's chat log file.
// It lives alongside session files: <datadir>/agents/<agentID>/sessions/<channelType>:<channelID>.chat.jsonl
func ChatLogPath(agentID, channelType, channelID string) string {
	name := sanitizeFileComponent(channelType + ":" + channelID)
	return filepath.Join(AgentDir(agentID), "sessions", name+".chat.jsonl")
}

// AppendChatLog appends a ChatLogEntry to the chat log file. If maxEntries > 0
// and the resulting file exceeds that count, the oldest entries are trimmed.
func AppendChatLog(path string, entry ChatLogEntry, maxEntries int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	if err := AppendJSONL(path, entry); err != nil {
		return err
	}
	if maxEntries <= 0 {
		return nil
	}
	lines, err := ReadJSONL[ChatLogEntry](path)
	if err != nil || len(lines) <= maxEntries {
		return nil
	}
	trimmed := lines[len(lines)-maxEntries:]
	return RewriteJSONL(path, trimmed)
}

// ReadChatLog reads all entries from a chat log file.
// Returns nil if the file does not exist.
func ReadChatLog(path string) ([]ChatLogEntry, error) {
	entries, err := ReadJSONL[ChatLogEntry](path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return entries, nil
}
