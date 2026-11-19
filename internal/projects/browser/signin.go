package browser

import (
	"errors"
	"fmt"
	"net/http"
)

// signInScript reports whether the page Aviary is looking at was served
// to a signed-in GitHub session. `Aviary auth -status` asks the same question of
// the home page and only needs the login; this one runs on whatever page a poll
// just read, and needs the signed-out answer as its own positive evidence
// rather than as the absence of a login.
//
// Both answers are read as positive evidence rather than as each other's
// negation, because the diagnosis only helps when it is right: GitHub stamps a
// signed-in page with the viewer's login and a `logged-in` body class, and a
// signed-out one with a `logged-out` body class and a link to /login. A page
// carrying neither -- a markup change, or a page Aviary was never meant to read
// -- reports neither, and the caller leaves the session out of its explanation
// instead of blaming a profile that is signed in perfectly well.
const signInScript = `(function () {
  var body = document.body;
  var actor = document.querySelector('meta[name="user-login"], meta[name="octolytics-actor-login"]');
  var login = actor ? (actor.getAttribute('content') || '').trim() : '';
  var signedIn = !!login || !!(body && body.classList.contains('logged-in'));
  var signedOut = !!(body && body.classList.contains('logged-out')) ||
    !!document.querySelector('a[href^="/login"], a[href^="https://github.com/login"]');
  return { signedIn: signedIn, signedOut: signedOut && !signedIn, login: login };
})()`

// signInState is signInScript's result.
type signInState struct {
	SignedIn  bool   `json:"signedIn"`
	SignedOut bool   `json:"signedOut"`
	Login     string `json:"login"`
}

// signedOutPage reports whether a page Aviary could not read anything from
// was served to a signed-out browser profile. It is only ever consulted on that
// failure path, so a successful read costs no extra evaluation, and an
// evaluation that fails answers false: a broken probe must not turn a real
// extraction problem into a wrong diagnosis.
func signedOutPage(page Page) bool {
	var state signInState
	if err := page.Eval(signInScript, &state); err != nil {
		return false
	}
	return state.SignedOut
}

// signedOutStatus reports whether a main-frame status Aviary just read a
// page with is GitHub's way of hiding something from a signed-out session
// rather than a genuine mistake in the target. GitHub answers a private
// repository, a private board, and a deleted one identically -- 404 to anyone
// not entitled to see it, 403 where it prefers to say so -- and the status
// alone cannot tell the two apart. The page it served can: a session that is
// signed in gets a 404 page stamped with its own login, so only a 404 served
// to a signed-out profile is read as a sign-in signal, and a signed-in run
// that really did mistype a repository still gets told it was a 404.
//
// The probe runs only on a failing status, so a page that loaded costs
// nothing, and it piggybacks on the navigation the tick already made: no extra
// request, no agent, no API call.
func signedOutStatus(page Page, status int) bool {
	if status != http.StatusNotFound && status != http.StatusForbidden {
		return false
	}
	return signedOutPage(page)
}

// ErrSignedOut marks a page that came back with nothing because the
// browser profile is not signed in to GitHub. Callers match it with errors.Is
// to tell it apart from a page whose markup Aviary could not read
// (ErrExtraction): the two look identical from the outside -- no issues
// -- but only one of them can be fixed by changing an extractor, and neither
// can be fixed by looking at a screenshot of a signed-out page.
var ErrSignedOut = errors.New("browser profile is signed out of GitHub")

// SignedOutError is the error a signed-out read returns, carrying the
// page that produced it and the profile directory to sign in.
type SignedOutError struct {
	URL     string
	Profile string
	// Status is the main-frame HTTP status the page was served with when that
	// is what gave the session away (404 or 403), and zero when the diagnosis
	// came from a page that loaded normally. It is carried so the recovery
	// path can say which of the two it saw.
	Status int
}

func (e *SignedOutError) Error() string {
	where := "the browser profile"
	if e.Profile != "" {
		where = "the browser profile at " + e.Profile
	}
	if e.Status != 0 {
		return fmt.Sprintf("GitHub returned HTTP %d for %s to %s, which is signed out of GitHub: a private repository or board is invisible to a signed-out session and is served the same 404 a missing one is. Run `aviary auth` to sign that profile in (the session persists), or point -browser-profile at a profile that already is", e.Status, e.URL, where)
	}
	return fmt.Sprintf("read %s while %s is signed out of GitHub, so \"@me\" in the filter matches nobody and private repositories are invisible: GitHub's own empty result is not aviary's issue list. Run `aviary auth` to sign that profile in (the session persists), or point -browser-profile at a profile that already is", e.URL, where)
}

// Is reports ErrSignedOut so callers can match the category without
// depending on this concrete type.
func (e *SignedOutError) Is(target error) bool { return target == ErrSignedOut }
