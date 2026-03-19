package agent

import (
	"strings"
	"sync"
)

// deliveryRegistry maps agent-scoped session runtime keys → (channelKey → send fn) for per-session
// outbound channel delivery. It is keyed by both session ID and channel
// identity so that repeated messages from the same channel update the fn
// in place rather than accumulating duplicates.
var deliveryRegistry = struct {
	mu   sync.RWMutex
	fns  map[string]map[string]func(string)               // sessionRuntimeKey → (chType:chID → text fn)
	mfns map[string]map[string]func(caption, path string) // sessionRuntimeKey → (chType:chID → media fn)
}{
	fns:  make(map[string]map[string]func(string)),
	mfns: make(map[string]map[string]func(string, string)),
}

// RegisterSessionDelivery registers fn as the delivery function for
// channelType/channelID on the given session. A second call with the same
// session+channel overwrites the previous entry (idempotent).
func RegisterSessionDelivery(agentID, sessionID, channelType, channelID string, fn func(string)) {
	key := channelType + ":" + channelID
	sessionKey := SessionRuntimeKey(agentID, sessionID)
	deliveryRegistry.mu.Lock()
	defer deliveryRegistry.mu.Unlock()
	if deliveryRegistry.fns[sessionKey] == nil {
		deliveryRegistry.fns[sessionKey] = make(map[string]func(string))
	}
	deliveryRegistry.fns[sessionKey][key] = fn
}

// RegisterSessionMediaDelivery registers a media delivery function for
// channelType/channelID on the given session. fn receives (caption, filePath).
func RegisterSessionMediaDelivery(agentID, sessionID, channelType, channelID string, fn func(caption, path string)) {
	key := channelType + ":" + channelID
	sessionKey := SessionRuntimeKey(agentID, sessionID)
	deliveryRegistry.mu.Lock()
	defer deliveryRegistry.mu.Unlock()
	if deliveryRegistry.mfns[sessionKey] == nil {
		deliveryRegistry.mfns[sessionKey] = make(map[string]func(string, string))
	}
	deliveryRegistry.mfns[sessionKey][key] = fn
}

// HasSessionMediaDelivery reports whether the session has any registered media
// delivery functions.
func HasSessionMediaDelivery(agentID, sessionID string) bool {
	deliveryRegistry.mu.RLock()
	defer deliveryRegistry.mu.RUnlock()
	return len(deliveryRegistry.mfns[SessionRuntimeKey(agentID, sessionID)]) > 0
}

// ShouldDeliverReply reports whether text should be forwarded to any external
// delivery target. Empty replies and the explicit NO_REPLY sentinel are
// suppressed.
func ShouldDeliverReply(text string) bool {
	trimmed := strings.TrimSpace(text)
	return trimmed != "" && trimmed != "NO_REPLY"
}

// deliverToSession calls all registered delivery functions for the session,
// forwarding text to each associated channel. It is called by the runner
// before emitting StreamEventDone so every code path (web UI, MCP, scheduled
// tasks) routes completed responses back to any configured channels.
func deliverToSession(agentID, sessionID, text string) {
	if !ShouldDeliverReply(text) {
		return
	}
	deliveryRegistry.mu.RLock()
	fns := deliveryRegistry.fns[SessionRuntimeKey(agentID, sessionID)]
	deliveryRegistry.mu.RUnlock()
	for _, fn := range fns {
		fn(text)
	}
}

// DeliverMediaToSession calls all registered media delivery functions for
// the session, forwarding the file to each associated channel.
func DeliverMediaToSession(agentID, sessionID, caption, filePath string) {
	if filePath == "" {
		return
	}
	deliveryRegistry.mu.RLock()
	fns := deliveryRegistry.mfns[SessionRuntimeKey(agentID, sessionID)]
	deliveryRegistry.mu.RUnlock()
	for _, fn := range fns {
		fn(caption, filePath)
	}
}
