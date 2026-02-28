// Package agent manages agent lifecycle, sessions, and LLM prompting.
package agent

// StreamEventType identifies the kind of event in a streaming response.
type StreamEventType string

const (
	StreamEventText  StreamEventType = "text"   // partial text from the LLM
	StreamEventDone  StreamEventType = "done"   // stream complete
	StreamEventError StreamEventType = "error"  // error occurred
	StreamEventStop  StreamEventType = "stop"   // agent was stopped mid-stream
)

// StreamEvent is a single event emitted during an agent response.
type StreamEvent struct {
	Type    StreamEventType
	AgentID string
	Text    string // set for StreamEventText
	Err     error  // set for StreamEventError
}

// StreamConsumer receives StreamEvents.
type StreamConsumer func(StreamEvent)
