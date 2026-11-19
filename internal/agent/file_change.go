package agent

import "encoding/json"

// fileChangeTools are the tools whose results describe a file change.
var fileChangeTools = map[string]bool{
	"file_edit":       true,
	"file_multi_edit": true,
	"file_write":      true,
}

// FileChangeFromToolResult extracts the file change reported by a successful
// file_edit, file_multi_edit, or file_write result.
func FileChangeFromToolResult(toolName, result string) (*FileChangeEvent, bool) {
	if !fileChangeTools[toolName] {
		return nil, false
	}
	var payload struct {
		Path string `json:"path"`
		Diff string `json:"diff"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil || payload.Path == "" {
		return nil, false
	}
	return &FileChangeEvent{Tool: toolName, Path: payload.Path, Diff: payload.Diff}, true
}
