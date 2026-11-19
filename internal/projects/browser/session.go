package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/chromedp/cdproto/cdp"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/cdproto/storage"
	"github.com/chromedp/chromedp"
)

// sessionCookieFileName is the file, inside the browser profile Aviary owns,
// that holds the sign-in Aviary carries from one browser process to the next.
// It lives in the profile directory because it belongs to that profile: point
// -browser-profile somewhere else and the sign-in there is the one restored.
const sessionCookieFileName = "aviary-session-cookies.json"

// sessionCookie is one cookie Aviary carries across browser processes. It is a
// Aviary-owned record rather than the CDP type so the file format does not move
// when the protocol's own cookie struct gains fields.
type sessionCookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
	SameSite string `json:"sameSite,omitempty"`

	// Expires is the cookie's expiry as seconds since the UNIX epoch, zero for
	// a cookie the browser holds only in memory. It is serialized so an expiry
	// that has passed by the time the next browser process starts is dropped
	// rather than replayed as if it were still live.
	Expires float64 `json:"expires,omitempty"`

	// session marks a cookie the browser holds only in memory. It is not
	// serialized: Expires already tells the two apart in the file, and the
	// flag exists to pick the in-memory ones out of the browser's full jar.
	session bool
}

// expired reports whether the cookie's own expiry has already passed. A
// session cookie carries no expiry and never expires on its own: it dies with
// the browser process, which is the whole reason Aviary carries it by hand.
func (c sessionCookie) expired(now time.Time) bool {
	return c.Expires > 0 && !now.Before(time.Unix(0, int64(c.Expires*float64(time.Second))))
}

// cookieJar is the browser's cookie store, narrowed to the two operations the
// sign-in handover needs. It is an interface so the save and restore logic can
// be exercised without a browser.
type cookieJar interface {
	readCookies() ([]sessionCookie, error)
	writeCookies(cookies []sessionCookie) error
}

// sessionCookiePath reports where a profile's carried-over sign-in is kept.
func sessionCookiePath(profile string) string {
	return filepath.Join(profile, sessionCookieFileName)
}

// carriedCookies keeps the cookies the next browser process would otherwise
// start without: the session ones the browser holds only in memory, and the
// expiring crumbs around them that it has not necessarily committed to disk.
//
// The expiring ones used to be left to the profile's own cookie database on the
// premise that the browser had already written them there. That only holds for
// a process that lives long enough: Chrome commits its cookie database on a
// batch timer of roughly 30 seconds and Aviary stops a browser by signalling its
// process tree, which does not flush the store first, so a short-lived process
// (`Aviary auth`, the suspend/resume hand-off the sign-in recovery does) loses
// GitHub's `logged_in`, `_octo` and `_device_id` outright (issue #422). Writing
// them alongside the session cookies costs one injection and is correct whether
// or not the browser got round to its own commit.
//
// A cookie whose expiry has already passed is dropped rather than carried: the
// browser would be entitled to reject it, and replaying an expired crumb is
// exactly the stale state the save is supposed to avoid.
func carriedCookies(cookies []sessionCookie, now time.Time) []sessionCookie {
	kept := make([]sessionCookie, 0, len(cookies))
	for _, cookie := range cookies {
		if cookie.expired(now) {
			continue
		}
		kept = append(kept, cookie)
	}
	return kept
}

// signedIn reports whether a jar still holds a sign-in worth carrying. The
// sign-in itself is a session cookie (`user_session` and friends), so a jar
// with none left is a profile that was signed out — the expiring crumbs it
// still carries say who was signed in, not that anyone is.
func signedIn(cookies []sessionCookie) bool {
	for _, cookie := range cookies {
		if cookie.session {
			return true
		}
	}
	return false
}

// saveSessionCookies writes a signed-in profile's cookies to the profile so the
// next browser process on it starts signed in.
//
// GitHub's sign-in lives in session cookies (`user_session` and friends), which
// a browser keeps in memory and drops when its process exits, so persisting the
// profile directory alone persists everything about the sign-in except the
// sign-in, which is why `Aviary auth` had to be run again for every watch (issue
// #414). The expiring crumbs around them (`logged_in`, `_octo`, `_device_id`)
// are carried too, because the browser only commits those to the profile's own
// cookie database on a batch timer Aviary does not wait for (issue #422).
func saveSessionCookies(profile string, jar cookieJar) error {
	if profile == "" {
		return nil
	}
	cookies, err := jar.readCookies()
	if err != nil {
		return fmt.Errorf("read browser session cookies: %w", err)
	}
	path := sessionCookiePath(profile)
	if !signedIn(cookies) {
		cookies = nil
	} else {
		cookies = carriedCookies(cookies, time.Now())
	}
	if len(cookies) == 0 {
		// A profile that was signed out has no sign-in left to carry, and a
		// stale file would keep offering the browser a session GitHub has
		// already invalidated.
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("clear browser session cookies %s: %w", path, err)
		}
		return nil
	}
	data, err := json.Marshal(cookies)
	if err != nil {
		return fmt.Errorf("encode browser session cookies: %w", err)
	}
	// The file holds a live GitHub session, so it is written no wider than the
	// profile directory it sits in.
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write browser session cookies %s: %w", path, err)
	}
	return nil
}

// restoreSessionCookies puts a previously saved sign-in back into a freshly
// launched browser. A profile that has never been signed in has no file and is
// not an error: the run is simply signed out, which the existing sign-in
// recovery already handles.
func restoreSessionCookies(profile string, jar cookieJar) error {
	if profile == "" {
		return nil
	}
	path := sessionCookiePath(profile)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read browser session cookies %s: %w", path, err)
	}
	var cookies []sessionCookie
	if err := json.Unmarshal(data, &cookies); err != nil {
		// A file Aviary cannot read is a file Aviary wrote wrong or something
		// else corrupted; dropping it costs one sign-in, keeping it would fail
		// every launch from here on.
		_ = os.Remove(path)
		return fmt.Errorf("decode browser session cookies %s: %w", path, err)
	}
	// A file written a while ago may name crumbs that have since expired; the
	// session cookies beside them are still worth restoring.
	cookies = carriedCookies(cookies, time.Now())
	if len(cookies) == 0 {
		return nil
	}
	if err := jar.writeCookies(cookies); err != nil {
		return fmt.Errorf("restore browser session cookies: %w", err)
	}
	return nil
}

// tabJar is a browser's cookie store reached through one of its tabs.
// Storage commands are browser-scoped, but CDP still needs a target to carry
// them, so the jar borrows a tab the run already has rather than opening one:
// a headed browser's first tab is the window `Aviary auth` signs in through
// (issue #412), and taking it for cookie work would put a second window on
// screen again.
type tabJar struct{ tab *Tab }

// readCookies reports every cookie the browser currently holds.
func (j tabJar) readCookies() ([]sessionCookie, error) {
	var jar []*network.Cookie
	if err := j.tab.run(chromedp.ActionFunc(func(ctx context.Context) error {
		cookies, err := storage.GetCookies().Do(ctx)
		jar = cookies
		return err
	})); err != nil {
		return nil, err
	}
	cookies := make([]sessionCookie, 0, len(jar))
	for _, cookie := range jar {
		if cookie == nil {
			continue
		}
		// CDP reports -1 rather than 0 as a session cookie's expiry; keep the
		// file's own convention that no expiry means a session cookie.
		expires := cookie.Expires
		if cookie.Session || expires < 0 {
			expires = 0
		}
		cookies = append(cookies, sessionCookie{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Secure:   cookie.Secure,
			HTTPOnly: cookie.HTTPOnly,
			SameSite: cookie.SameSite.String(),
			Expires:  expires,
			session:  cookie.Session,
		})
	}
	return cookies, nil
}

// writeCookies injects cookies into the browser. It is called before anything
// navigates, so the first page load of a run is already made as the signed-in
// user rather than being redirected to a login wall first.
func (j tabJar) writeCookies(cookies []sessionCookie) error {
	params := make([]*network.CookieParam, 0, len(cookies))
	for _, cookie := range cookies {
		// An omitted expiry is what makes the browser hold a cookie in memory
		// only, so it is set for the expiring crumbs and left out otherwise.
		var expires *cdp.TimeSinceEpoch
		if cookie.Expires > 0 {
			at := cdp.TimeSinceEpoch(time.Unix(0, int64(cookie.Expires*float64(time.Second))))
			expires = &at
		}
		params = append(params, &network.CookieParam{
			Name:     cookie.Name,
			Value:    cookie.Value,
			Domain:   cookie.Domain,
			Path:     cookie.Path,
			Secure:   cookie.Secure,
			HTTPOnly: cookie.HTTPOnly,
			SameSite: network.CookieSameSite(cookie.SameSite),
			Expires:  expires,
		})
	}
	return j.tab.run(chromedp.ActionFunc(func(ctx context.Context) error {
		return storage.SetCookies(params).Do(ctx)
	}))
}

// saveSession and restoreSession carry the profile's sign-in across the browser
// processes a run starts and stops, through a tab the run already owns. Both
// are best-effort: a browser that cannot be asked for its cookies still watches
// GitHub, and a sign-in that could not be carried is recovered by the sign-in
// guard exactly as it is today. The error is returned so callers may report it.
func (b *Browser) saveSession(tab *Tab) error {
	return saveSessionCookies(b.Profile(), tabJar{tab})
}

func (b *Browser) restoreSession(tab *Tab) error {
	return restoreSessionCookies(b.Profile(), tabJar{tab})
}
