package update

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

// Releases are resolved through GitHub's ordinary web URLs, never its REST
// API, whose unauthenticated rate limit (60 requests an hour per IP) answers
// with HTTP 403. installer/install.sh and installer/install.ps1 use the same
// scheme, with <base> defaulting to https://github.com:
//
//	<base>/<repo>/releases/latest                     302 to .../releases/tag/<tag>
//	<base>/<repo>/releases/download/<tag>/<asset>     release asset
//	<base>/<repo>/releases/download/<tag>/checksums.txt  SHA-256 "<hex>  <asset>" lines
//
// Keep these in sync when changing either side.

const (
	// DefaultDownloadBase is the host release URLs are built on.
	DefaultDownloadBase = "https://github.com"
	// DownloadBaseEnv overrides DefaultDownloadBase, like the install scripts'
	// variable of the same name.
	DownloadBaseEnv = "AVIARY_DOWNLOAD_BASE"
	checksumsAsset  = "checksums.txt"
	rateLimitHint   = "GitHub rate limited the request; retry in a few minutes"
)

// errChecksumMismatch reports a downloaded asset that does not match checksums.txt.
var errChecksumMismatch = errors.New("checksum mismatch")

// downloadBase returns the release host: base if set, else DownloadBaseEnv,
// else DefaultDownloadBase.
func downloadBase(base string) string {
	if strings.TrimSpace(base) == "" {
		base = os.Getenv(DownloadBaseEnv)
	}
	if strings.TrimSpace(base) == "" {
		base = DefaultDownloadBase
	}
	return strings.TrimRight(strings.TrimSpace(base), "/")
}

// releaseDownloadURL returns the download URL of asset in release version.
func releaseDownloadURL(base, repo, version, asset string) string {
	return fmt.Sprintf("%s/%s/releases/download/%s/%s", downloadBase(base), repo, NormalizeVersion(version), asset)
}

// LatestRelease returns the latest Aviary release tag, read from the
// releases/latest redirect without calling GitHub's REST API.
func LatestRelease(ctx context.Context, client *http.Client) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	return resolveLatestVersion(ctx, client, "", DefaultRepo)
}

// resolveLatestVersion reads the latest release tag from the Location of the
// <base>/<repo>/releases/latest redirect.
func resolveLatestVersion(ctx context.Context, client *http.Client, base, repo string) (string, error) {
	latestURL := fmt.Sprintf("%s/%s/releases/latest", downloadBase(base), repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, latestURL, nil)
	if err != nil {
		return "", fmt.Errorf("creating release request: %w", err)
	}
	noFollow := *client
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	res, err := noFollow.Do(req)
	if err != nil {
		return "", fmt.Errorf("checking %s releases: %w", repo, err)
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode < 300 || res.StatusCode >= 400 {
		return "", httpStatusError("release lookup failed", res)
	}
	loc, err := res.Location()
	if err != nil {
		return "", fmt.Errorf("release lookup failed: redirect has no location")
	}
	tag, ok := tagFromReleasePath(loc)
	if !ok {
		return "", fmt.Errorf("release lookup failed: %s has no published release", repo)
	}
	return tag, nil
}

// tagFromReleasePath extracts <tag> from a .../releases/tag/<tag> URL.
func tagFromReleasePath(u *url.URL) (string, bool) {
	_, tag, ok := strings.Cut(u.Path, "/releases/tag/")
	if !ok {
		return "", false
	}
	tag, err := url.PathUnescape(strings.Trim(tag, "/"))
	if err != nil || tag == "" || strings.Contains(tag, "/") {
		return "", false
	}
	return tag, true
}

// httpStatusError summarizes a failed response without echoing its body,
// which for GitHub's rate-limit errors includes the caller's public IP.
func httpStatusError(prefix string, res *http.Response) error {
	switch res.StatusCode {
	case http.StatusForbidden, http.StatusTooManyRequests:
		return fmt.Errorf("%s: HTTP %d (%s)", prefix, res.StatusCode, rateLimitHint)
	case http.StatusNotFound:
		return fmt.Errorf("%s: HTTP 404 (not found)", prefix)
	default:
		return fmt.Errorf("%s: HTTP %d", prefix, res.StatusCode)
	}
}

// fetchChecksum downloads the release's checksums.txt and returns the
// SHA-256 listed for asset.
func fetchChecksum(ctx context.Context, client *http.Client, base, repo, version, asset string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, releaseDownloadURL(base, repo, version, checksumsAsset), nil)
	if err != nil {
		return "", fmt.Errorf("creating checksums request: %w", err)
	}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("downloading %s for %s: %w", checksumsAsset, version, err)
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode == http.StatusNotFound {
		return "", fmt.Errorf("release %s not found in %s", NormalizeVersion(version), repo)
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return "", httpStatusError("downloading "+checksumsAsset, res)
	}
	scanner := bufio.NewScanner(io.LimitReader(res.Body, 1<<20))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 2 && strings.TrimPrefix(fields[1], "*") == asset {
			return strings.ToLower(fields[0]), nil
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("reading %s: %w", checksumsAsset, err)
	}
	return "", fmt.Errorf("%s for %s has no entry for %s", checksumsAsset, NormalizeVersion(version), asset)
}

// verifyChecksum checks that the SHA-256 of the file at path is want.
func verifyChecksum(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("opening download: %w", err)
	}
	defer f.Close() //nolint:errcheck
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hashing download: %w", err)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("%w for %s: expected %s, got %s", errChecksumMismatch, f.Name(), want, got)
	}
	return nil
}
