// Package browser provides browser automation via Chrome DevTools Protocol.
package browser

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	"github.com/chromedp/cdproto/target"
	"github.com/chromedp/chromedp"
)

// Session wraps a chromedp browser context for a single automation session.
type Session struct {
	allocCtx    context.Context
	cancelAlloc context.CancelFunc
	taskCtx     context.Context
	cancelTask  context.CancelFunc
	mu          sync.Mutex
}

// newRemoteSessionForTab connects to an already-running Chrome and attaches to
// an existing tab by its CDP target ID. Closing this session only disconnects
// Go from the tab — the tab itself remains open in Chrome.
func newRemoteSessionForTab(ctx context.Context, wsURL, tabID string) (*Session, error) {
	allocCtx, cancelAlloc := chromedp.NewRemoteAllocator(ctx, wsURL)
	taskCtx, cancelTask := chromedp.NewContext(
		allocCtx,
		chromedp.WithTargetID(target.ID(tabID)),
		chromedp.WithErrorf(filteredChromeDPErrorf),
	)

	if err := chromedp.Run(taskCtx); err != nil {
		cancelTask()
		cancelAlloc()
		return nil, fmt.Errorf("attaching to tab %s: %w", tabID, err)
	}

	return &Session{
		allocCtx:    allocCtx,
		cancelAlloc: cancelAlloc,
		taskCtx:     taskCtx,
		cancelTask:  cancelTask,
	}, nil
}

// TabID returns the CDP target ID for the tab this session is attached to.
func (s *Session) TabID() string {
	if c := chromedp.FromContext(s.taskCtx); c != nil && c.Target != nil {
		return string(c.Target.TargetID)
	}
	return ""
}

// Run executes a chromedp action within the session.
func (s *Session) Run(actions ...chromedp.Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return chromedp.Run(s.taskCtx, actions...)
}

// Close tears down the session context and allocator.
// Depending on how the session was created, this may close the tab.
func (s *Session) Close() {
	s.cancelTask()
	s.cancelAlloc()
}

// Detach disconnects from the browser without explicitly canceling the task
// context. This is used for operations attached to an existing tab where we
// must avoid closing the target on cleanup.
func (s *Session) Detach() {
	s.cancelAlloc()
}

func filteredChromeDPErrorf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if isIgnorableChromeDPError(msg) {
		slog.Debug("browser: ignored chromedp error", "msg", msg)
		return
	}
	slog.Error(msg)
}

func isIgnorableChromeDPError(msg string) bool {
	return strings.Contains(msg, "could not unmarshal event:") &&
		strings.Contains(msg, `unknown InitiatorType value: FedCM`)
}
