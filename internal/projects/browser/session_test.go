package browser

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeCookieJar stands in for a browser's cookie store.
type fakeCookieJar struct {
	jar       []sessionCookie
	written   []sessionCookie
	writes    int
	readErr   error
	writeErr  error
	readCalls int
}

func (f *fakeCookieJar) readCookies() ([]sessionCookie, error) {
	f.readCalls++
	if f.readErr != nil {
		return nil, f.readErr
	}
	return f.jar, nil
}

func (f *fakeCookieJar) writeCookies(cookies []sessionCookie) error {
	f.writes++
	if f.writeErr != nil {
		return f.writeErr
	}
	f.written = cookies
	return nil
}

// aYearFromNow is the expiry GitHub gives its `logged_in` crumb, as seconds
// since the epoch — the form the browser reports and the file stores.
func aYearFromNow() float64 {
	return float64(time.Now().Add(365*24*time.Hour).UnixNano()) / float64(time.Second)
}

func githubSessionCookies() []sessionCookie {
	return []sessionCookie{
		{Name: "user_session", Value: "abc", Domain: "github.com", Path: "/", Secure: true, HTTPOnly: true, SameSite: "Lax", session: true},
		{Name: "logged_in", Value: "yes", Domain: ".github.com", Path: "/", Expires: aYearFromNow()},
		{Name: "dotcom_user", Value: "octocat", Domain: "github.com", Path: "/", session: true},
	}
}

// readSavedCookies decodes the file a save left in a profile.
func readSavedCookies(t *testing.T, profile string) []sessionCookie {
	t.Helper()
	data, err := os.ReadFile(sessionCookiePath(profile))
	if err != nil {
		t.Fatalf("read saved cookies: %v", err)
	}
	var saved []sessionCookie
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatalf("decode saved cookies: %v", err)
	}
	return saved
}

// savedNames lists the cookies in a save, in order.
func savedNames(cookies []sessionCookie) string {
	names := make([]string, 0, len(cookies))
	for _, cookie := range cookies {
		names = append(names, cookie.Name)
	}
	return strings.Join(names, ",")
}

// TestSaveSessionCookiesKeepsTheExpiringCrumbsToo is issue #422: the browser
// only commits an expiring cookie to the profile's own database on a batch
// timer, so a save that skipped them lost GitHub's sign-in crumbs whenever the
// browser process was stopped before that timer fired.
func TestSaveSessionCookiesKeepsTheExpiringCrumbsToo(t *testing.T) {
	profile := t.TempDir()
	jar := &fakeCookieJar{jar: githubSessionCookies()}
	if err := saveSessionCookies(profile, jar); err != nil {
		t.Fatalf("saveSessionCookies: %v", err)
	}
	saved := readSavedCookies(t, profile)
	if got := savedNames(saved); got != "user_session,logged_in,dotcom_user" {
		t.Fatalf("saved %s, want the session cookies and the expiring crumb", got)
	}
	if saved[0].Value != "abc" || !saved[0].Secure || !saved[0].HTTPOnly || saved[0].SameSite != "Lax" {
		t.Fatalf("cookie attributes were not carried: %+v", saved[0])
	}
	if saved[0].Expires != 0 {
		t.Fatalf("session cookie was given an expiry: %+v", saved[0])
	}
	if saved[1].Expires == 0 {
		t.Fatalf("expiring crumb lost its expiry, so it would be replayed as a session cookie: %+v", saved[1])
	}
}

// TestSaveSessionCookiesDropsAnAlreadyExpiredCookie keeps the save from writing
// back a crumb whose expiry has already passed: the browser would be entitled
// to reject it, and replaying it is exactly the stale state the file avoids.
func TestSaveSessionCookiesDropsAnAlreadyExpiredCookie(t *testing.T) {
	profile := t.TempDir()
	expired := float64(time.Now().Add(-time.Hour).UnixNano()) / float64(time.Second)
	jar := &fakeCookieJar{jar: []sessionCookie{
		{Name: "user_session", Value: "abc", Domain: "github.com", session: true},
		{Name: "_device_id", Value: "stale", Domain: "github.com", Expires: expired},
	}}
	if err := saveSessionCookies(profile, jar); err != nil {
		t.Fatalf("saveSessionCookies: %v", err)
	}
	if got := savedNames(readSavedCookies(t, profile)); got != "user_session" {
		t.Fatalf("saved %s, want only the live sign-in", got)
	}
}

func TestSaveSessionCookiesWritesProfileOnlyPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows has no Unix permission bits to report: Go writes the file
		// with the directory's inherited ACL and stats it as 0666.
		t.Skip("file modes are not enforced on Windows")
	}
	profile := t.TempDir()
	if err := saveSessionCookies(profile, &fakeCookieJar{jar: githubSessionCookies()}); err != nil {
		t.Fatalf("saveSessionCookies: %v", err)
	}
	info, err := os.Stat(sessionCookiePath(profile))
	if err != nil {
		t.Fatalf("stat saved cookies: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("saved cookies with mode %v, want 0600: a live GitHub session must not be world readable", mode)
	}
}

func TestSaveSessionCookiesClearsFileWhenSignedOut(t *testing.T) {
	profile := t.TempDir()
	path := sessionCookiePath(profile)
	if err := os.WriteFile(path, []byte(`[{"name":"user_session"}]`), 0o600); err != nil {
		t.Fatalf("seed saved cookies: %v", err)
	}
	// Only persistent crumbs left: the profile was signed out. They say who was
	// signed in, not that anyone is, so there is nothing left to carry.
	jar := &fakeCookieJar{jar: []sessionCookie{{Name: "logged_in", Domain: ".github.com", Expires: aYearFromNow()}}}
	if err := saveSessionCookies(profile, jar); err != nil {
		t.Fatalf("saveSessionCookies: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stale sign-in survived a signed-out save: %v", err)
	}
}

func TestSaveSessionCookiesWithoutProfileDoesNothing(t *testing.T) {
	jar := &fakeCookieJar{jar: githubSessionCookies()}
	if err := saveSessionCookies("", jar); err != nil {
		t.Fatalf("saveSessionCookies: %v", err)
	}
	if jar.readCalls != 0 {
		t.Fatalf("read the jar %d times for a browser with no profile, want 0", jar.readCalls)
	}
}

func TestSaveSessionCookiesReportsReadFailure(t *testing.T) {
	profile := t.TempDir()
	jar := &fakeCookieJar{readErr: errors.New("browser is closed")}
	err := saveSessionCookies(profile, jar)
	if err == nil || !strings.Contains(err.Error(), "browser is closed") {
		t.Fatalf("saveSessionCookies error = %v, want the jar's failure", err)
	}
	if _, statErr := os.Stat(sessionCookiePath(profile)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("wrote a cookie file from a failed read: %v", statErr)
	}
}

func TestRestoreSessionCookiesRoundTrips(t *testing.T) {
	profile := t.TempDir()
	source := &fakeCookieJar{jar: githubSessionCookies()}
	if err := saveSessionCookies(profile, source); err != nil {
		t.Fatalf("saveSessionCookies: %v", err)
	}
	// A new browser process starts with an empty jar, as Chrome does.
	target := &fakeCookieJar{}
	if err := restoreSessionCookies(profile, target); err != nil {
		t.Fatalf("restoreSessionCookies: %v", err)
	}
	if got := savedNames(target.written); got != "user_session,logged_in,dotcom_user" {
		t.Fatalf("restored %s, want every carried cookie: %+v", got, target.written)
	}
	if target.written[0].Name != "user_session" || target.written[0].Value != "abc" {
		t.Fatalf("restored the wrong cookie: %+v", target.written[0])
	}
	if !target.written[0].Secure || !target.written[0].HTTPOnly || target.written[0].Path != "/" {
		t.Fatalf("restored cookie lost its attributes: %+v", target.written[0])
	}
	if target.written[1].Expires == 0 {
		t.Fatalf("restored crumb lost its expiry: %+v", target.written[1])
	}
}

// TestRestoreSessionCookiesSkipsAnExpiredCookie covers a file saved long enough
// ago that one of its crumbs has since expired: the sign-in beside it is still
// worth restoring, the expired one is not replayed.
func TestRestoreSessionCookiesSkipsAnExpiredCookie(t *testing.T) {
	profile := t.TempDir()
	saved := `[{"name":"user_session","value":"abc"},{"name":"logged_in","value":"yes","expires":1000000000}]`
	if err := os.WriteFile(sessionCookiePath(profile), []byte(saved), 0o600); err != nil {
		t.Fatalf("seed saved cookies: %v", err)
	}
	target := &fakeCookieJar{}
	if err := restoreSessionCookies(profile, target); err != nil {
		t.Fatalf("restoreSessionCookies: %v", err)
	}
	if got := savedNames(target.written); got != "user_session" {
		t.Fatalf("restored %s, want only the cookie that has not expired", got)
	}
}

func TestRestoreSessionCookiesWithoutFileIsNotAnError(t *testing.T) {
	target := &fakeCookieJar{}
	if err := restoreSessionCookies(t.TempDir(), target); err != nil {
		t.Fatalf("restoreSessionCookies on a never-signed-in profile: %v", err)
	}
	if target.writes != 0 {
		t.Fatalf("wrote cookies %d times with nothing saved, want 0", target.writes)
	}
}

func TestRestoreSessionCookiesWithEmptyFileWritesNothing(t *testing.T) {
	profile := t.TempDir()
	if err := os.WriteFile(sessionCookiePath(profile), []byte(`[]`), 0o600); err != nil {
		t.Fatalf("seed saved cookies: %v", err)
	}
	target := &fakeCookieJar{}
	if err := restoreSessionCookies(profile, target); err != nil {
		t.Fatalf("restoreSessionCookies: %v", err)
	}
	if target.writes != 0 {
		t.Fatalf("wrote cookies %d times for an empty file, want 0", target.writes)
	}
}

func TestRestoreSessionCookiesDropsAnUnreadableFile(t *testing.T) {
	profile := t.TempDir()
	path := sessionCookiePath(profile)
	if err := os.WriteFile(path, []byte("not json"), 0o600); err != nil {
		t.Fatalf("seed saved cookies: %v", err)
	}
	if err := restoreSessionCookies(profile, &fakeCookieJar{}); err == nil {
		t.Fatal("restoreSessionCookies accepted a corrupt file")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("corrupt file survived and would fail every later launch: %v", err)
	}
}

func TestRestoreSessionCookiesReportsWriteFailure(t *testing.T) {
	profile := t.TempDir()
	if err := saveSessionCookies(profile, &fakeCookieJar{jar: githubSessionCookies()}); err != nil {
		t.Fatalf("saveSessionCookies: %v", err)
	}
	err := restoreSessionCookies(profile, &fakeCookieJar{writeErr: errors.New("browser is closed")})
	if err == nil || !strings.Contains(err.Error(), "browser is closed") {
		t.Fatalf("restoreSessionCookies error = %v, want the jar's failure", err)
	}
}

func TestSessionCookiePathLivesInTheProfile(t *testing.T) {
	if got, want := sessionCookiePath("/tmp/profile"), filepath.Join("/tmp/profile", sessionCookieFileName); got != want {
		t.Fatalf("sessionCookiePath = %q, want %q", got, want)
	}
	// Two profiles must not share a sign-in: -browser-profile picks which one.
	if sessionCookiePath("/tmp/a") == sessionCookiePath("/tmp/b") {
		t.Fatal("separate profiles share one saved sign-in")
	}
}

// TestSessionSurvivesAProcessRestart is the issue itself: sign in once,
// stop the browser, start another one on the same profile, and the second
// process must come up holding the sign-in rather than needing `Aviary auth`.
func TestSessionSurvivesAProcessRestart(t *testing.T) {
	profile := t.TempDir()
	signedIn := &fakeCookieJar{jar: githubSessionCookies()}
	if err := saveSessionCookies(profile, signedIn); err != nil {
		t.Fatalf("save on browser close: %v", err)
	}
	next := &fakeCookieJar{}
	if err := restoreSessionCookies(profile, next); err != nil {
		t.Fatalf("restore on browser start: %v", err)
	}
	if got := savedNames(next.written); got != "user_session,logged_in,dotcom_user" {
		t.Fatalf("second browser process came up with cookies %s, want the sign-in", got)
	}
}

func TestImportSessionCopiesASavedSignIn(t *testing.T) {
	from, to := t.TempDir(), filepath.Join(t.TempDir(), "profile")
	saved := `[{"name":"user_session","value":"abc","domain":"github.com","path":"/","secure":true,"httpOnly":true}]`
	if err := os.WriteFile(sessionCookiePath(from), []byte(saved), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ImportSession(from, to); err != nil {
		t.Fatalf("ImportSession: %v", err)
	}
	got, err := os.ReadFile(sessionCookiePath(to))
	if err != nil || string(got) != saved {
		t.Fatalf("imported %q, %v; want the saved sign-in", got, err)
	}
	if info, err := os.Stat(sessionCookiePath(to)); err == nil && runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("imported sign-in mode %v, want 0600", info.Mode().Perm())
	}
}

func TestImportSessionRefusesAProfileWithoutASignIn(t *testing.T) {
	from := t.TempDir()
	if err := ImportSession(from, t.TempDir()); !errors.Is(err, ErrNoSavedSession) {
		t.Fatalf("ImportSession from an empty profile = %v, want ErrNoSavedSession", err)
	}
	// Only expiring crumbs left: someone was signed in, but nobody is now.
	crumbs := `[{"name":"logged_in","value":"no","domain":"github.com","path":"/","expires":4102444800}]`
	if err := os.WriteFile(sessionCookiePath(from), []byte(crumbs), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ImportSession(from, t.TempDir()); !errors.Is(err, ErrNoSavedSession) {
		t.Fatalf("ImportSession from a signed-out profile = %v, want ErrNoSavedSession", err)
	}
}
