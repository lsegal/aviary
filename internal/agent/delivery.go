package agent

import "sync"

// deliveryRegistry maps sessionID → (channelKey → send fn) for per-session
// outbound channel delivery. It is keyed by both session ID and channel
// identity so that repeated messages from the same channel update the fn
// in place rather than accumulating duplicates.
var deliveryRegistry = struct {
	mu   sync.RWMutex
	fns  map[string]map[string]func(string)               // sessionID → (chType:chID → text fn)
	mfns map[string]map[string]func(caption, path string) // sessionID → (chType:chID → media fn)
}{
	fns:  make(map[string]map[string]func(string)),
	mfns: make(map[string]map[string]func(string, string)),
}

// RegisterSessionDelivery registers fn as the delivery function for
// channelType/channelID on the given session. A second call with the same
// session+channel overwrites the previous entry (idempotent).
func RegisterSessionDelivery(sessionID, channelType, channelID string, fn func(string)) {
	key := channelType + ":" + channelID
	deliveryRegistry.mu.Lock()
	defer deliveryRegistry.mu.Unlock()
	if deliveryRegistry.fns[sessionID] == nil {
		deliveryRegistry.fns[sessionID] = make(map[string]func(string))
	}
	deliveryRegistry.fns[sessionID][key] = fn
}

// RegisterSessionMediaDelivery registers a media delivery function for
// channelType/channelID on the given session. fn receives (caption, filePath).
func RegisterSessionMediaDelivery(sessionID, channelType, channelID string, fn func(caption, path string)) {
	key := channelType + ":" + channelID
	deliveryRegistry.mu.Lock()
	defer deliveryRegistry.mu.Unlock()
	if deliveryRegistry.mfns[sessionID] == nil {
		deliveryRegistry.mfns[sessionID] = make(map[string]func(string, string))
	}
	deliveryRegistry.mfns[sessionID][key] = fn
}

// HasSessionMediaDelivery reports whether the session has any registered media
// delivery functions.
func HasSessionMediaDelivery(sessionID string) bool {
	deliveryRegistry.mu.RLock()
	defer deliveryRegistry.mu.RUnlock()
	return len(deliveryRegistry.mfns[sessionID]) > 0
}

// deliverToSession calls all registered delivery functions for the session,
// forwarding text to each associated channel. It is called by the runner
// before emitting StreamEventDone so every code path (web UI, MCP, scheduled
// tasks) routes completed responses back to any configured channels.
func deliverToSession(sessionID, text string) {
	if text == "" {
		return
	}
	deliveryRegistry.mu.RLock()
	fns := deliveryRegistry.fns[sessionID]
	deliveryRegistry.mu.RUnlock()
	for _, fn := range fns {
		fn(text)
	}
}

// DeliverMediaToSession calls all registered media delivery functions for
// the session, forwarding the file to each associated channel.
func DeliverMediaToSession(sessionID, caption, filePath string) {
	if filePath == "" {
		return
	}
	deliveryRegistry.mu.RLock()
	fns := deliveryRegistry.mfns[sessionID]
	deliveryRegistry.mu.RUnlock()
	for _, fn := range fns {
		fn(caption, filePath)
	}
}
