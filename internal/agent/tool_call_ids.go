package agent

import "strconv"

// ToolCallIDs gives one run's tool calls IDs when the backend reports none.
// Without IDs a result is paired with the oldest unfinished call of the same
// tool, since a runner reports a call's result after announcing it. The zero
// value is ready to use; it is not safe for concurrent use.
type ToolCallIDs struct {
	next    int
	pending map[string][]string
}

// ID returns the ID of the call or result tool reports.
func (t *ToolCallIDs) ID(tool *ToolEvent) string {
	if tool.ID != "" {
		return tool.ID
	}
	if tool.Done {
		queue := t.pending[tool.Name]
		if len(queue) == 0 {
			return ""
		}
		t.pending[tool.Name] = queue[1:]
		return queue[0]
	}
	t.next++
	id := "call_" + strconv.Itoa(t.next)
	if t.pending == nil {
		t.pending = map[string][]string{}
	}
	t.pending[tool.Name] = append(t.pending[tool.Name], id)
	return id
}
