package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The tests here drive a real Chromium-based browser, which is what the fake
// cookie jar in browsersession_test.go cannot do: they are the only thing that
// proves the browser accepts the cookies Aviary writes back to it (issue #420).
// The risk they cover is entirely in the browser's own rules — the `__Host-`
// prefix, `SameSite` round-tripping, `Secure` and `HttpOnly` — so a fake jar
// that stores whatever it is handed would pass no matter what Aviary wrote.
//
// A machine with no browser installed skips them rather than failing: the rest
// of the suite has no browser dependency and neither should a `go test ./...`
// on a developer machine without Chrome.

// liveProfile starts a browser on a fresh profile directory, returning
// both so the caller can start a second browser on the same profile and see
// what the first one left behind.
func liveProfile(t *testing.T, profile string) *Browser {
	t.Helper()
	if _, err := findBinary(""); err != nil {
		t.Skipf("no Chromium-based browser installed: %v", err)
	}
	browser, err := Start(context.Background(), Config{Profile: profile})
	if err != nil {
		t.Skipf("browser would not start: %v", err)
	}
	return browser
}

// githubShapedCookieServer serves the cookies GitHub sets on a sign-in, with
// the attributes GitHub sets them with: `user_session` and `_gh_sess` are
// plain session cookies, `__Host-user_session_same_site` carries the prefix
// whose rules a browser enforces on the way back in, and `logged_in` is one of
// the expiring crumbs the browser would only commit to its own store on a
// batch timer Aviary does not wait for (issue #422).
// Its /echo path reports the cookies the browser actually sent, which is the
// only evidence that a restored cookie is live rather than merely present.
func githubShapedCookieServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/echo" {
			_, _ = fmt.Fprint(w, r.Header.Get("Cookie"))
			return
		}
		w.Header().Add("Set-Cookie", "user_session=session-value; path=/; secure; HttpOnly; SameSite=Lax")
		w.Header().Add("Set-Cookie", "__Host-user_session_same_site=same-site-value; path=/; secure; HttpOnly; SameSite=Strict")
		w.Header().Add("Set-Cookie", "_gh_sess=gh-sess-value; path=/; secure; HttpOnly")
		w.Header().Add("Set-Cookie", "logged_in=yes; path=/; secure; HttpOnly; SameSite=Lax; Max-Age=31536000")
		_, _ = fmt.Fprint(w, "signed in")
	}))
}

func TestCarriesSessionCookiesToTheNextProcess(t *testing.T) {
	server := githubShapedCookieServer()
	defer server.Close()
	profile := t.TempDir()

	signIn := liveProfile(t, profile)
	tab, err := signIn.Tab("sign-in")
	if err != nil {
		t.Fatalf("open tab: %v", err)
	}
	if err := tab.Navigate(server.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	// Close saves the session on its way out, exactly as a watch run's browser
	// restart and `Aviary auth` both do.
	if err := signIn.Close(); err != nil {
		t.Fatalf("close signed-in browser: %v", err)
	}

	data, err := os.ReadFile(sessionCookiePath(profile))
	if err != nil {
		t.Fatalf("read saved session: %v", err)
	}
	var saved []sessionCookie
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("decode saved session: %v", err)
	}
	byName := map[string]sessionCookie{}
	for _, cookie := range saved {
		byName[cookie.Name] = cookie
	}
	for _, name := range []string{"user_session", "__Host-user_session_same_site", "_gh_sess"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("saved session is missing %s, got %v", name, saved)
		}
	}
	if got, ok := byName["logged_in"]; !ok {
		t.Errorf("expiring cookie logged_in was not saved, got %v", saved)
	} else if got.Expires == 0 {
		t.Errorf("logged_in was saved without its expiry and would be replayed forever: %+v", got)
	}
	if got := byName["user_session"]; got.Value != "session-value" || !got.Secure || !got.HTTPOnly || got.SameSite != "Lax" || got.Path != "/" {
		t.Errorf("user_session round-tripped as %+v", got)
	}
	if got := byName["__Host-user_session_same_site"]; got.SameSite != "Strict" || !got.Secure {
		t.Errorf("__Host-user_session_same_site round-tripped as %+v", got)
	}

	watch := liveProfile(t, profile)
	defer func() { _ = watch.Close() }()
	// The restore rides the first tab the run opens, before it navigates.
	restored, err := watch.Tab("watch")
	if err != nil {
		t.Fatalf("open tab on restored profile: %v", err)
	}
	jar, err := tabJar{restored}.readCookies()
	if err != nil {
		t.Fatalf("read restored jar: %v", err)
	}
	accepted := map[string]sessionCookie{}
	for _, cookie := range jar {
		accepted[cookie.Name] = cookie
	}
	for name, want := range byName {
		got, ok := accepted[name]
		if !ok {
			t.Errorf("browser rejected restored cookie %s (%+v)", name, want)
			continue
		}
		if got.Value != want.Value || got.Domain != want.Domain || got.Path != want.Path ||
			got.Secure != want.Secure || got.HTTPOnly != want.HTTPOnly || got.SameSite != want.SameSite {
			t.Errorf("restored cookie %s changed: saved %+v, browser holds %+v", name, want, got)
		}
		if got.session != (want.Expires == 0) {
			t.Errorf("restored cookie %s changed kind: saved %+v, browser holds %+v", name, want, got)
		}
		// The browser stores an expiry to whole-second granularity, so a saved
		// sub-second fraction comes back truncated rather than changed.
		if want.Expires != 0 && math.Abs(got.Expires-want.Expires) >= 1 {
			t.Errorf("restored cookie %s changed expiry: saved %+v, browser holds %+v", name, want, got)
		}
	}

	// Present in the jar is not the same as sent on the wire: ask the server
	// what it actually received on this process's first request.
	if err := restored.Navigate(server.URL + "/echo"); err != nil {
		t.Fatalf("navigate restored tab: %v", err)
	}
	var sent string
	if err := restored.Eval("document.body.innerText", &sent); err != nil {
		t.Fatalf("read echoed cookies: %v", err)
	}
	names := make([]string, 0, len(byName))
	for name := range byName {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !cookieSent(sent, name, byName[name].Value) {
			t.Errorf("restored %s was not sent to the server; request carried %q", name, sent)
		}
	}
}

// cookieSent reports whether an echoed Cookie header carries a name=value pair.
func cookieSent(header, name, value string) bool {
	for _, pair := range strings.Split(header, "; ") {
		if pair == name+"="+value {
			return true
		}
	}
	return false
}

func TestRestoresCarriedSessionBeforeNavigating(t *testing.T) {
	server := githubShapedCookieServer()
	defer server.Close()
	profile := t.TempDir()
	path := sessionCookiePath(profile)
	// A sign-in saved by an earlier run, written by hand so the injection is
	// tested against the file format rather than against whatever the browser
	// happened to hand back a moment earlier.
	saved := `[{"name":"user_session","value":"carried","domain":"127.0.0.1","path":"/","secure":true,"httpOnly":true,"sameSite":"Lax"}]`
	if err := os.WriteFile(path, []byte(saved), 0o600); err != nil {
		t.Fatalf("seed saved session: %v", err)
	}

	browser := liveProfile(t, profile)
	defer func() { _ = browser.Close() }()
	tab, err := browser.Tab("watch")
	if err != nil {
		t.Fatalf("open tab: %v", err)
	}
	// The first navigation of the run must already carry the sign-in: that is
	// the whole point of restoring on the tab rather than after the first read.
	if err := tab.Navigate(server.URL + "/echo"); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	var sent string
	if err := tab.Eval("document.body.innerText", &sent); err != nil {
		t.Fatalf("read echoed cookies: %v", err)
	}
	if !cookieSent(sent, "user_session", "carried") {
		t.Fatalf("first request of the run did not carry the saved sign-in; it sent %q", sent)
	}
}

func TestSavesNothingForASignedOutProfile(t *testing.T) {
	profile := t.TempDir()
	browser := liveProfile(t, profile)
	if _, err := browser.Tab("watch"); err != nil {
		t.Fatalf("open tab: %v", err)
	}
	// Nothing signed this profile in, so there is no sign-in to carry and no
	// file should appear for the next run to replay.
	if err := browser.Close(); err != nil {
		t.Fatalf("close browser: %v", err)
	}
	if _, err := os.Stat(sessionCookiePath(profile)); !os.IsNotExist(err) {
		data, _ := os.ReadFile(sessionCookiePath(profile))
		t.Fatalf("signed-out profile saved a session: %v (%s)", err, data)
	}
}

// TestCarriesExpiringCookiesFromAShortLivedProcess is issue #422 itself.
// Chrome commits its cookie database on a batch timer of roughly 30 seconds and
// Aviary stops a browser by signalling its process tree, which does not flush
// the store, so a process that lives a few seconds — `Aviary auth`, or the
// suspend/resume hand-off the in-run sign-in recovery does — used to leave
// GitHub's expiring crumbs nowhere at all.
//
// The browser here is closed well inside that commit window, and its own store
// is then deleted so nothing but Aviary's saved file can carry the crumb.
func TestCarriesExpiringCookiesFromAShortLivedProcess(t *testing.T) {
	server := githubShapedCookieServer()
	defer server.Close()
	profile := t.TempDir()

	started := time.Now()
	signIn := liveProfile(t, profile)
	tab, err := signIn.Tab("auth")
	if err != nil {
		t.Fatalf("open tab: %v", err)
	}
	if err := tab.Navigate(server.URL); err != nil {
		t.Fatalf("navigate: %v", err)
	}
	if err := signIn.Close(); err != nil {
		t.Fatalf("close signed-in browser: %v", err)
	}
	if lived := time.Since(started); lived > 20*time.Second {
		t.Skipf("browser process lived %s, which is not inside Chrome's commit window", lived)
	}

	// Whatever the browser did or did not write to its own database, the saved
	// file is the only thing that may carry the crumb from here.
	matches, _ := filepath.Glob(filepath.Join(profile, "Default", "Cookies*"))
	for _, path := range matches {
		if err := os.Remove(path); err != nil {
			t.Fatalf("clear the browser's own cookie store: %v", err)
		}
	}

	watch := liveProfile(t, profile)
	defer func() { _ = watch.Close() }()
	restored, err := watch.Tab("watch")
	if err != nil {
		t.Fatalf("open tab on restored profile: %v", err)
	}
	if err := restored.Navigate(server.URL + "/echo"); err != nil {
		t.Fatalf("navigate restored tab: %v", err)
	}
	var sent string
	if err := restored.Eval("document.body.innerText", &sent); err != nil {
		t.Fatalf("read echoed cookies: %v", err)
	}
	if !cookieSent(sent, "logged_in", "yes") {
		t.Fatalf("expiring logged_in did not survive a short-lived browser process; the request carried %q", sent)
	}
	if !cookieSent(sent, "user_session", "session-value") {
		t.Fatalf("carrying the crumbs cost the sign-in itself; the request carried %q", sent)
	}

	// The crumb must still be the browser's own persistent cookie, not one
	// Aviary turned into a session cookie by dropping its expiry.
	jar, err := tabJar{restored}.readCookies()
	if err != nil {
		t.Fatalf("read restored jar: %v", err)
	}
	for _, cookie := range jar {
		if cookie.Name != "logged_in" {
			continue
		}
		if cookie.session || cookie.Expires <= 0 {
			t.Fatalf("logged_in came back without its expiry: %+v", cookie)
		}
		return
	}
	t.Fatal("logged_in is missing from the restored browser's jar")
}
