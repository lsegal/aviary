// Package browser provides browser automation via Chrome DevTools Protocol.
package browser

import (
	"context"
	"fmt"
	"sync"

	"github.com/chromedp/chromedp"
)

// Session wraps a chromedp browser context for a single automation session.
type Session struct {
	allocCtx  context.Context
	cancelAlloc context.CancelFunc
	taskCtx   context.Context
	cancelTask context.CancelFunc
	mu        sync.Mutex
}

// newSession creates a new browser session, launching Chromium.
// profileDir is a path used for the user-data-dir (isolated from the user's profile).
// cdpPort is the remote debugging port (0 = let Chrome pick one).
func newSession(ctx context.Context, opts []chromedp.ExecAllocatorOption) (*Session, error) {
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(ctx, opts...)
	taskCtx, cancelTask := chromedp.NewContext(allocCtx)

	// Initialize the browser.
	if err := chromedp.Run(taskCtx); err != nil {
		cancelTask()
		cancelAlloc()
		return nil, fmt.Errorf("launching browser: %w", err)
	}

	return &Session{
		allocCtx:    allocCtx,
		cancelAlloc: cancelAlloc,
		taskCtx:     taskCtx,
		cancelTask:  cancelTask,
	}, nil
}

// Run executes a chromedp action within the session.
func (s *Session) Run(actions ...chromedp.Action) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return chromedp.Run(s.taskCtx, actions...)
}

// Close terminates the browser session.
func (s *Session) Close() {
	s.cancelTask()
	s.cancelAlloc()
}
