package agents

import (
	"fmt"
	"sort"
	"strings"
)

// Decoder returns the canonical format name the definition's output is decoded
// with, resolving the aliases a definition may spell it as. An unrecognised
// value never reaches here: Validate rejects it.
func (o Output) Decoder() string {
	if format, ok := outputFormats[strings.TrimSpace(o.Format)]; ok {
		return format
	}
	return FormatText
}

// validate reports the first thing wrong with the output block.
func (o Output) validate() error {
	format, ok := outputFormats[strings.TrimSpace(o.Format)]
	if !ok {
		return fmt.Errorf(`field "output.format": %q must be one of %s`, o.Format, JoinOr(outputFormatNames()))
	}
	if format != FormatJSONL {
		if o.JSONL != nil {
			return fmt.Errorf(`field "output.jsonl" is only meaningful when "output.format" is %q`, FormatJSONL)
		}
		return nil
	}
	if o.JSONL == nil {
		return fmt.Errorf(`field "output.jsonl" is required when "output.format" is %q`, FormatJSONL)
	}
	return o.JSONL.validate()
}

func (j JSONL) validate() error {
	if err := j.event().validate("output.jsonl"); err != nil {
		return err
	}
	if j.Type != "" && !jsonlPathPattern.MatchString(j.Type) {
		return jsonlPathError("output.jsonl.type", j.Type)
	}
	if j.Text == "" && j.ToolName == "" && !j.eventsRead() {
		return fmt.Errorf(`field "output.jsonl" needs at least one of "text" or "toolName"; a decoder that reads neither renders nothing`)
	}
	if j.TextDelta && j.Text == "" {
		return fmt.Errorf(`field "output.jsonl.textDelta" needs "output.jsonl.text"; there is no text to join without it`)
	}
	if len(j.Ignore) > 0 && j.Type == "" {
		return fmt.Errorf(`field "output.jsonl.ignore" needs "output.jsonl.type"; event types cannot be skipped without knowing where the type is`)
	}
	for _, ignored := range j.Ignore {
		if strings.TrimSpace(ignored) == "" {
			return fmt.Errorf(`field "output.jsonl.ignore" cannot contain an empty value`)
		}
	}
	return j.validateEvents()
}

// validateEvents checks the per-event-type overrides. Each is keyed by a value
// of the type path, so there is nothing to match an override against without
// one, and an override on a type the stream never reads is dead configuration
// rather than a decoder that quietly renders nothing.
func (j JSONL) validateEvents() error {
	if len(j.Events) == 0 {
		return nil
	}
	if j.Type == "" {
		return fmt.Errorf(`field "output.jsonl.events" needs "output.jsonl.type"; an override cannot be matched without knowing where the type is`)
	}
	ignored := make(map[string]bool, len(j.Ignore))
	for _, event := range j.Ignore {
		ignored[event] = true
	}
	for _, kind := range sortedKeys(j.Events) {
		if strings.TrimSpace(kind) == "" {
			return fmt.Errorf(`field "output.jsonl.events" cannot contain an empty event type`)
		}
		if ignored[kind] {
			return fmt.Errorf(`field "output.jsonl.events": %q is also in "output.jsonl.ignore", so nothing is ever read out of it`, kind)
		}
		field := fmt.Sprintf("output.jsonl.events.%s", kind)
		event := j.Events[kind]
		if err := event.validate(field); err != nil {
			return err
		}
		if event.Text == "" && event.ToolName == "" {
			return fmt.Errorf(`field %q needs at least one of "text" or "toolName"; an override that reads neither changes nothing`, field)
		}
	}
	return nil
}

// event is the JSONL block's own paths seen as the overrides they default to,
// so one set of rules covers the block and every per-type override of it.
func (j JSONL) event() JSONLEvent {
	return JSONLEvent{Text: j.Text, ToolName: j.ToolName, ToolInput: j.ToolInput, ToolNamePrefix: j.ToolNamePrefix}
}

// eventsRead reports whether any per-type override reads something, which is
// what lets a definition leave the shared paths empty and describe each of its
// event types on its own.
func (j JSONL) eventsRead() bool {
	for _, event := range j.Events {
		if event.Text != "" || event.ToolName != "" {
			return true
		}
	}
	return false
}

// validate reports the first thing wrong with one set of event paths, named by
// the field they were read from so the message points at the block at fault.
func (e JSONLEvent) validate(field string) error {
	for suffix, path := range map[string]string{
		"text": e.Text, "toolName": e.ToolName, "toolInput": e.ToolInput,
	} {
		if path != "" && !jsonlPathPattern.MatchString(path) {
			return jsonlPathError(field+"."+suffix, path)
		}
	}
	if e.ToolInput != "" && e.ToolName == "" {
		return fmt.Errorf(`field %q needs %q; a tool input is rendered as part of the call it belongs to`, field+".toolInput", field+".toolName")
	}
	if e.ToolNamePrefix != "" && e.ToolName == "" {
		return fmt.Errorf(`field %q needs %q; there is no name to match a prefix against without it`, field+".toolNamePrefix", field+".toolName")
	}
	return nil
}

func jsonlPathError(field, path string) error {
	return fmt.Errorf(`field %q: %q must be dot-separated object keys, each optionally suffixed with [] to step into an array`, field, path)
}

// sortedKeys returns a map's keys in a fixed order, so validation reports the
// same first problem on every run rather than whichever one ranging hit first.
func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// MissingSessionPatterns are the phrases that mean the agent no longer holds
// the session it was asked to resume, so the work is restarted rather than
// reported as a failure. The shared defaults always apply; a definition's own
// phrases are added to them, which is what keeps one agent's distinctive
// wording out of every other agent's detection.
func (d Definition) MissingSessionPatterns() []string {
	if len(d.MissingSession) == 0 {
		return DefaultMissingSessionPatterns
	}
	patterns := make([]string, 0, len(DefaultMissingSessionPatterns)+len(d.MissingSession))
	patterns = append(patterns, DefaultMissingSessionPatterns...)
	return append(patterns, d.MissingSession...)
}
