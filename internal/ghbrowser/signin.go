// Package ghbrowser reads GitHub through Aviary's own browser and profile, and
// tells a signed-in GitHub session apart from a signed-out one.
package ghbrowser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// SignInURL is GitHub's sign-in page, opened in Aviary's browser when the user
// chooses to sign in there.
const SignInURL = "https://github.com/login"

// homeURL is the page a session check reads: every GitHub page carries the
// session markers, and the home page exists for every account.
const homeURL = "https://github.com/"

// signInScript reports whether the current page was served to a signed-in
// GitHub session. Both answers are positive evidence rather than each other's
// negation: GitHub stamps a signed-in page with the viewer's login and a
// `logged-in` body class, and a signed-out one with a `logged-out` body class
// and a link to /login. A page carrying neither (a markup change, or a page
// that is not GitHub's) reports neither, so a broken probe never blames a
// profile that is signed in.
const signInScript = `(function () {
  var body = document.body;
  var actor = document.querySelector('meta[name="user-login"], meta[name="octolytics-actor-login"]');
  var login = actor ? (actor.getAttribute('content') || '').trim() : '';
  var signedIn = !!login || !!(body && body.classList.contains('logged-in'));
  var signedOut = !!(body && body.classList.contains('logged-out')) ||
    !!document.querySelector('a[href^="/login"], a[href^="https://github.com/login"]');
  return JSON.stringify({ signedIn: signedIn, signedOut: signedOut && !signedIn, login: login });
})()`

// SignInState is what a GitHub page says about the session it was served to.
type SignInState struct {
	SignedIn  bool   `json:"signedIn"`
	SignedOut bool   `json:"signedOut"`
	Login     string `json:"login"`
}

// Page is a rendered GitHub page that can run a script and return its result
// as text.
type Page interface {
	EvalJS(ctx context.Context, expr string) (string, error)
}

// ReadSignInState runs the sign-in probe on page.
func ReadSignInState(ctx context.Context, page Page) (SignInState, error) {
	raw, err := page.EvalJS(ctx, signInScript)
	if err != nil {
		return SignInState{}, fmt.Errorf("read GitHub session: %w", err)
	}
	var state SignInState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return SignInState{}, fmt.Errorf("decode GitHub session %q: %w", raw, err)
	}
	return state, nil
}

// SignedOutPage reports whether page was served to a signed-out session. A
// probe that fails answers false, so an extraction problem is never reported
// as a sign-in problem.
func SignedOutPage(ctx context.Context, page Page) bool {
	state, err := ReadSignInState(ctx, page)
	return err == nil && state.SignedOut
}

// SignedOutStatus reports whether a page served with status hides something
// from a signed-out session. GitHub serves a private repository or board the
// same 404 (or 403) as a missing one, so the status counts as signed out only
// when the page itself shows a signed-out session: a signed-in session gets a
// 404 stamped with its own login, and a mistyped target stays a 404.
func SignedOutStatus(ctx context.Context, page Page, status int) bool {
	if status != http.StatusNotFound && status != http.StatusForbidden {
		return false
	}
	return SignedOutPage(ctx, page)
}

// ErrSignedOut marks a read that came back empty because Aviary's browser is
// not signed in to GitHub. A signed-out read is never "no issues".
var ErrSignedOut = errors.New("aviary's browser is signed out of GitHub")

// SignedOutError is a signed-out read, naming the page that showed it.
type SignedOutError struct {
	URL string
	// Status is the HTTP status (404 or 403) that gave the session away, or
	// zero when the page loaded but showed a signed-out session.
	Status int
}

func (e *SignedOutError) Error() string {
	const fix = "Sign in to GitHub in Aviary's browser, or switch to gh polling"
	if e.Status != 0 {
		return fmt.Sprintf("GitHub returned HTTP %d for %s to a signed-out session: private repositories and boards look missing until you sign in. %s", e.Status, e.URL, fix)
	}
	return fmt.Sprintf("read %s while signed out of GitHub, so \"@me\" matches nobody and private repositories are invisible. %s", e.URL, fix)
}

// Is matches ErrSignedOut.
func (e *SignedOutError) Is(target error) bool { return target == ErrSignedOut }

// CheckEmptyResult turns an empty result into a signed-out error when page
// shows a signed-out session. An empty search (an `@me` filter in particular)
// read while signed out is GitHub's empty answer to nobody, not an empty list.
func CheckEmptyResult(ctx context.Context, page Page, url string, rows int) error {
	if rows == 0 && SignedOutPage(ctx, page) {
		return &SignedOutError{URL: url}
	}
	return nil
}
