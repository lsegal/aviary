package clidefs

import (
	"encoding/json"
	"slices"
	"strings"

	"github.com/lsegal/aviary/internal/transcript"
)

// TurnResult reads line as the final result event the definition's CLI ends a
// turn with. A generic JSONL stream uses the event its result block declares;
// every other format uses the one its transcript format knows (#1141).
func (o Output) TurnResult(line []byte) transcript.TurnOutcome {
	if o.JSONL == nil || o.JSONL.Result == nil {
		return transcript.TurnResult(o.TranscriptFormat(), line)
	}
	var event any
	if json.Unmarshal(line, &event) != nil {
		return transcript.TurnUnfinished
	}
	result := o.JSONL.Result
	if !slices.Contains(jsonlStrings(event, o.JSONL.Type), result.Event) {
		return transcript.TurnUnfinished
	}
	if result.Status == "" {
		return transcript.TurnSucceeded
	}
	statuses := jsonlStrings(event, result.Status)
	if len(statuses) == 0 || slices.Contains(result.Success, statuses[0]) {
		return transcript.TurnSucceeded
	}
	return transcript.TurnFailed
}

// LookupJSONL walks a field path through a decoded event, returning every
// value it names. A key suffixed with [] steps into an array and continues
// into each of its elements, so one path can name a value per content block.
// An empty path names the value it was given.
func LookupJSONL(value any, path string) []any {
	current := []any{value}
	if path == "" {
		return current
	}
	for _, segment := range strings.Split(path, ".") {
		key, array := strings.CutSuffix(segment, "[]")
		var next []any
		for _, item := range current {
			object, ok := item.(map[string]any)
			if !ok {
				continue
			}
			field, ok := object[key]
			if !ok || field == nil {
				continue
			}
			if !array {
				next = append(next, field)
				continue
			}
			items, ok := field.([]any)
			if !ok {
				continue
			}
			next = append(next, items...)
		}
		if current = next; len(current) == 0 {
			return nil
		}
	}
	return current
}

// jsonlStrings returns the non-empty, trimmed strings a path names.
func jsonlStrings(value any, path string) []string {
	var found []string
	for _, item := range LookupJSONL(value, path) {
		if text, ok := item.(string); ok {
			if text = strings.TrimSpace(text); text != "" {
				found = append(found, text)
			}
		}
	}
	return found
}
