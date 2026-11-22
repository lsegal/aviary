// Package agent manages agent lifecycle, sessions, and LLM prompting.
package agent

// StreamEventType identifies the kind of event in a streaming response.
type StreamEventType string

// StreamEventType values.
const (
	StreamEventText   StreamEventType = "text"   // partial text from the LLM
	StreamEventTool   StreamEventType = "tool"   // tool execution metadata
	StreamEventMedia  StreamEventType = "media"  // image data URL from the LLM
	StreamEventDone   StreamEventType = "done"   // stream complete
	StreamEventError  StreamEventType = "error"  // error occurred
	StreamEventStop   StreamEventType = "stop"   // agent was stopped mid-stream
	StreamEventStatus StreamEventType = "status" // verbose progress message before a tool call
	StreamEventFile   StreamEventType = "file"   // a tool changed a file
)

// ToolEvent carries structured tool execution details for debug-oriented UIs.
type ToolEvent struct {
	// ID ties a call to its result when the backend reports one.
	ID     string
	Name   string
	Args   map[string]any
	Result string
	Error  string
	// Done is set on the event reporting a call's outcome, telling it apart
	// from the event announcing the call when the result is empty.
	Done bool
}

// FileChangeEvent describes a file changed by a file editing tool.
type FileChangeEvent struct {
	Tool string // the tool that made the change
	Path string // absolute path of the changed file
	Diff string // unified diff of the change
}

// StreamEvent is a single event emitted during an agent response.
type StreamEvent struct {
	Type     StreamEventType
	AgentID  string
	Text     string // set for StreamEventText
	Tool     *ToolEvent
	File     *FileChangeEvent // set for StreamEventFile
	MediaURL string           // set for StreamEventMedia (image data URL or remote URL)
	Err      error            // set for StreamEventError
	// Interrupted is set on a StreamEventStop that Aviary's own shutdown or
	// restart caused, not a stop request. The prompt is kept for recovery.
	Interrupted bool
}

// StreamConsumer receives StreamEvents.
type StreamConsumer func(StreamEvent)
