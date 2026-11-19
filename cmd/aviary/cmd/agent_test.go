package cmd

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/projects/transcript"
)

func TestAgentRunEventWriter_PrintsProgressAsTranscriptEvents(t *testing.T) {
	var out bytes.Buffer
	w := newAgentRunEventWriter(&out)
	for _, message := range []string{
		"Renaming ",
		"the package.",
		`[tool]{"name":"file_edit","args":{"path":"main.go"}}`,
		`[file]{"tool":"file_edit","path":"/clone/main.go","diff":"-package main\n+package app\n"}`,
		"[media]data:image/png;base64,AAAA",
		"Done.",
		"[tool]not json",
	} {
		w.progress(message)
	}
	require.NoError(t, w.finish("Renaming the package.Done."))

	// The lines are exactly what the aviary agent definition's transcript
	// mapper reads back as job events.
	mapper := transcript.NewMapper(transcript.FormatAviaryJSON)
	var events []transcript.Event
	for _, line := range bytes.Split(bytes.TrimSpace(out.Bytes()), []byte("\n")) {
		events = append(events, mapper.Map(line)...)
	}
	require.Len(t, events, 4, out.String())
	assert.Equal(t, transcript.Event{Kind: transcript.KindMessage, Text: "Renaming the package."}, withoutTime(events[0]))
	assert.Equal(t, transcript.Event{Kind: transcript.KindToolCall, Name: "file_edit", Input: map[string]any{"path": "main.go"}}, withoutTime(events[1]))
	assert.Equal(t, transcript.Event{Kind: transcript.KindFileChange, Name: "file_edit", Path: "/clone/main.go", Diff: "-package main\n+package app\n"}, withoutTime(events[2]))
	assert.Equal(t, transcript.Event{Kind: transcript.KindMessage, Text: "Done."}, withoutTime(events[3]))
	assert.False(t, events[2].Time.IsZero())
}

func TestAgentRunEventWriter_PrintsTheReplyWhenNoTextStreamed(t *testing.T) {
	var out bytes.Buffer
	w := newAgentRunEventWriter(&out)
	require.NoError(t, w.finish("all done"))
	events := transcript.NewMapper(transcript.FormatAviaryJSON).Map(bytes.TrimSpace(out.Bytes()))
	require.Len(t, events, 1)
	assert.Equal(t, "all done", events[0].Text)
}

func withoutTime(event transcript.Event) transcript.Event {
	event.Time = time.Time{}
	return event
}
