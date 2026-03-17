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
)

// ToolEvent carries structured tool execution details for debug-oriented UIs.
type ToolEvent struct {
	Name   string
	Args   map[string]any
	Result string
	Error  string
}

// StreamEvent is a single event emitted during an agent response.
type StreamEvent struct {
	Type     StreamEventType
	AgentID  string
	Text     string // set for StreamEventText
	Tool     *ToolEvent
	MediaURL string // set for StreamEventMedia (image data URL or remote URL)
	Err      error  // set for StreamEventError
}

// StreamConsumer receives StreamEvents.
type StreamConsumer func(StreamEvent)
