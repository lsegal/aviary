package ghbrowser

import (
	"context"
	"fmt"
	"log/slog"
)

// Browser is the part of Aviary's browser manager a session check needs.
type Browser interface {
	Open(ctx context.Context, url string) (string, error)
	EvalJS(ctx context.Context, tabID, expr string) (string, error)
	CloseTab(tabID string) error
}

// tabPage is one tab of a Browser, read as a Page.
type tabPage struct {
	browser Browser
	tabID   string
}

func (p tabPage) EvalJS(ctx context.Context, expr string) (string, error) {
	return p.browser.EvalJS(ctx, p.tabID, expr)
}

// CheckSession reports whether Aviary's browser has a GitHub session, by
// reading GitHub's home page in a tab of its own that is closed afterwards.
func CheckSession(ctx context.Context, b Browser) (SignInState, error) {
	tabID, err := b.Open(ctx, homeURL)
	if err != nil {
		return SignInState{}, fmt.Errorf("open GitHub in Aviary's browser: %w", err)
	}
	defer func() {
		if err := b.CloseTab(tabID); err != nil {
			slog.Warn("ghbrowser: close session check tab", "tab", tabID, "err", err)
		}
	}()
	return ReadSignInState(ctx, tabPage{browser: b, tabID: tabID})
}

// OpenSignIn opens GitHub's sign-in page in Aviary's browser and returns its
// tab, for the user to sign in there. The session persists in Aviary's
// browser profile.
func OpenSignIn(ctx context.Context, b Browser) (string, error) {
	tabID, err := b.Open(ctx, SignInURL)
	if err != nil {
		return "", fmt.Errorf("open GitHub sign-in in Aviary's browser: %w", err)
	}
	return tabID, nil
}
