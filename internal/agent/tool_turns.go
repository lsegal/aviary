package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/store"
)

// ToolCallTurn is one persisted tool call and the user message whose turn
// made it.
type ToolCallTurn struct {
	AgentID  string
	Session  string // the session file's path
	CalledAt time.Time
	Args     map[string]any
	Result   string
	// Prompt is the user message that started the call's turn, or nil when
	// no user message came before the call.
	Prompt *domain.Message
}

// FindToolCallTurns returns every call of tool that returned a result in any
// agent's session history, oldest first, each with the user message whose
// turn made it, so a tool's past effect can be traced back to the request
// behind it, such as the screenshots sent with an issue filing.
func FindToolCallTurns(tool string) ([]ToolCallTurn, error) {
	agentsDir := filepath.Join(store.DataDir(), store.DirAgents)
	agents, err := os.ReadDir(agentsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var turns []ToolCallTurn
	for _, a := range agents {
		if !a.IsDir() {
			continue
		}
		sessions, _ := filepath.Glob(filepath.Join(agentsDir, a.Name(), "sessions", "*.jsonl"))
		for _, path := range sessions {
			found, err := sessionToolCallTurns(path, tool)
			if err != nil {
				return nil, err
			}
			for i := range found {
				found[i].AgentID = a.Name()
			}
			turns = append(turns, found...)
		}
	}
	sort.SliceStable(turns, func(i, j int) bool { return turns[i].CalledAt.Before(turns[j].CalledAt) })
	return turns, nil
}

// sessionToolCallTurns finds tool's calls in one session file. A call's
// record is saved when its result arrives, which can be after a user message
// sent while the turn ran, so a call belongs to the last user message sent
// before the call started, not the one before its record.
func sessionToolCallTurns(path, tool string) ([]ToolCallTurn, error) {
	messages, err := store.ReadJSONL[domain.Message](path)
	if err != nil {
		return nil, err
	}
	var prompts []domain.Message
	var turns []ToolCallTurn
	for _, m := range messages {
		switch m.Role {
		case domain.MessageRoleUser:
			prompts = append(prompts, m)
		case domain.MessageRoleTool:
			var rec toolEventRecord
			if json.Unmarshal([]byte(m.Content), &rec) != nil || rec.Name != tool || rec.Result == "" {
				continue
			}
			turns = append(turns, ToolCallTurn{Session: path, CalledAt: toolCallTime(rec.ID, m.Timestamp), Args: rec.Args, Result: rec.Result})
		}
	}
	for i := range turns {
		for j := len(prompts) - 1; j >= 0; j-- {
			if !prompts[j].Timestamp.After(turns[i].CalledAt) {
				prompt := prompts[j]
				turns[i].Prompt = &prompt
				break
			}
		}
	}
	return turns, nil
}

// toolCallTime is when a call started: the time in an ID newID made, or
// saved, when it was recorded, for calls whose backend named them.
func toolCallTime(id string, saved time.Time) time.Time {
	parts := strings.Split(id, "_")
	if len(parts) == 3 && parts[0] == "call" {
		if nanos, err := strconv.ParseInt(parts[1], 10, 64); err == nil {
			return time.Unix(0, nanos)
		}
	}
	return saved
}
