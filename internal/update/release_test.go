package update

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/buildinfo"
)

const rateLimitBody = `{"message":"API rate limit exceeded for 203.0.113.7. (But here's the good news: ...)"}`

// fakeReleaseHost serves the non-API release URLs for one release and answers
// every REST API path with GitHub's rate-limit 403.
type fakeReleaseHost struct {
	*httptest.Server
	mu        sync.Mutex
	requests  []string
	checksums string
}

func newFakeReleaseHost(t *testing.T, tag string, archive []byte) *fakeReleaseHost {
	t.Helper()
	asset := assetBaseName(tag, runtime.GOOS, runtime.GOARCH) + ".tar.gz"
	sum := sha256.Sum256(archive)
	h := &fakeReleaseHost{checksums: fmt.Sprintf("%s  %s\n%s  other.tar.gz\n", hex.EncodeToString(sum[:]), asset, strings.Repeat("0", 64))}
	h.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		h.requests = append(h.requests, r.URL.Path)
		checksums := h.checksums
		h.mu.Unlock()
		switch r.URL.Path {
		case "/" + DefaultRepo + "/releases/latest":
			http.Redirect(w, r, "/"+DefaultRepo+"/releases/tag/"+tag, http.StatusFound)
		case "/" + DefaultRepo + "/releases/download/" + tag + "/checksums.txt":
			_, _ = w.Write([]byte(checksums))
		case "/" + DefaultRepo + "/releases/download/" + tag + "/" + asset:
			_, _ = w.Write(archive)
		default:
			w.WriteHeader(http.StatusForbidden)
			_, _ = w.Write([]byte(rateLimitBody))
		}
	}))
	t.Cleanup(h.Close)
	t.Setenv(DownloadBaseEnv, h.URL)
	return h
}

func (h *fakeReleaseHost) paths() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.requests...)
}

// noAPIClient fails any request to api.github.com.
func noAPIClient(t *testing.T) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host == "api.github.com" {
			t.Errorf("unexpected GitHub API request: %s", r.URL)
			return nil, errors.New("api.github.com is not allowed")
		}
		return http.DefaultTransport.RoundTrip(r)
	})}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func releaseArchive(t *testing.T, binary string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	name := "aviary"
	if runtime.GOOS == "windows" {
		name = "aviary.exe"
	}
	require.NoError(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(binary))}))
	_, err := tw.Write([]byte(binary))
	require.NoError(t, err)
	require.NoError(t, tw.Close())
	require.NoError(t, gz.Close())
	return buf.Bytes()
}

func useReleaseBuild(t *testing.T, v string) {
	t.Helper()
	orig := buildinfo.Version
	buildinfo.Version = v
	ClearCache()
	t.Cleanup(func() {
		buildinfo.Version = orig
		ClearCache()
	})
}

func forceDirectDownload(t *testing.T) {
	t.Helper()
	origLook := lookGH
	t.Cleanup(func() { lookGH = origLook })
	lookGH = func() (string, error) { return "", errors.New("no gh") }
}

func TestCheckResolvesLatestFromRedirect(t *testing.T) {
	useReleaseBuild(t, "1.2.3")
	host := newFakeReleaseHost(t, "v1.4.0", nil)

	check, err := Check(context.Background(), noAPIClient(t))
	require.NoError(t, err)
	assert.Equal(t, "v1.4.0", check.LatestVersion)
	assert.True(t, check.UpgradeAvailable)
	assert.Equal(t, []string{"/" + DefaultRepo + "/releases/latest"}, host.paths())
}

func TestCheckSummarizesRateLimitWithoutIP(t *testing.T) {
	useReleaseBuild(t, "1.2.3")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(rateLimitBody))
	}))
	t.Cleanup(srv.Close)
	t.Setenv(DownloadBaseEnv, srv.URL)

	_, err := Check(context.Background(), srv.Client())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retry")
	assert.NotContains(t, err.Error(), "203.0.113.7")
	assert.NotContains(t, err.Error(), "rate limit exceeded for")
}

func TestInstallLatestWhileAPIRateLimited(t *testing.T) {
	useReleaseBuild(t, "1.2.3")
	forceDirectDownload(t)
	host := newFakeReleaseHost(t, "v1.4.0", releaseArchive(t, "new binary"))
	target := filepath.Join(t.TempDir(), "aviary")
	require.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755))

	// Any REST API path on the host answers 403; the upgrade never needs it.
	res, err := http.Get(host.URL + "/repos/" + DefaultRepo + "/releases/latest")
	require.NoError(t, err)
	_ = res.Body.Close()
	require.Equal(t, http.StatusForbidden, res.StatusCode)

	result, err := Install(context.Background(), InstallOptions{TargetPath: target, Client: noAPIClient(t)})
	require.NoError(t, err)
	assert.Equal(t, "v1.4.0", result.Version)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "new binary", string(got))
	for _, p := range host.paths()[1:] {
		assert.NotContains(t, p, "/repos/", "install requested a REST API path")
	}
}

func TestInstallAbortsOnChecksumMismatch(t *testing.T) {
	useReleaseBuild(t, "1.2.3")
	forceDirectDownload(t)
	host := newFakeReleaseHost(t, "v1.4.0", releaseArchive(t, "tampered binary"))
	host.mu.Lock()
	host.checksums = fmt.Sprintf("%s  %s\n", strings.Repeat("a", 64), assetBaseName("v1.4.0", runtime.GOOS, runtime.GOARCH)+".tar.gz")
	host.mu.Unlock()
	target := filepath.Join(t.TempDir(), "aviary")
	require.NoError(t, os.WriteFile(target, []byte("old binary"), 0o755))

	_, err := Install(context.Background(), InstallOptions{Version: "1.4.0", TargetPath: target})
	require.ErrorIs(t, err, errChecksumMismatch)
	got, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Equal(t, "old binary", string(got), "a mismatched download must not replace the binary")
}

func TestInstallUnknownVersion(t *testing.T) {
	useReleaseBuild(t, "1.2.3")
	forceDirectDownload(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)

	_, err := Install(context.Background(), InstallOptions{Version: "9.9.9", TargetPath: filepath.Join(t.TempDir(), "aviary"), DownloadBase: srv.URL})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "release v9.9.9 not found")
}

func TestDownloadBase(t *testing.T) {
	t.Run("explicit base wins over env", func(t *testing.T) {
		t.Setenv(DownloadBaseEnv, "https://env.example")
		assert.Equal(t, "https://explicit.example", downloadBase("  https://explicit.example/  "))
	})
	t.Run("env used when base is blank", func(t *testing.T) {
		t.Setenv(DownloadBaseEnv, " https://env.example// ")
		assert.Equal(t, "https://env.example", downloadBase("   "))
	})
	t.Run("default when base and env are blank", func(t *testing.T) {
		t.Setenv(DownloadBaseEnv, " ")
		assert.Equal(t, DefaultDownloadBase, downloadBase(""))
	})
}

func TestTagFromReleasePath(t *testing.T) {
	cases := []struct {
		name, path, tag string
		ok              bool
	}{
		{"plain tag", "/o/r/releases/tag/v1.2.3", "v1.2.3", true},
		{"trailing slash", "/o/r/releases/tag/v1.2.3/", "v1.2.3", true},
		{"escaped tag", "/o/r/releases/tag/v1.2.3%2Brc", "v1.2.3+rc", true},
		{"not a tag path", "/o/r/releases", "", false},
		{"empty tag", "/o/r/releases/tag/", "", false},
		{"nested tag", "/o/r/releases/tag/v1/extra", "", false},
		{"bad escape", "/o/r/releases/tag/v1%zz", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tag, ok := tagFromReleasePath(&url.URL{Path: tc.path})
			assert.Equal(t, tc.ok, ok)
			assert.Equal(t, tc.tag, tag)
		})
	}
}

func TestHTTPStatusError(t *testing.T) {
	for status, want := range map[int]string{
		http.StatusForbidden:           "pfx: HTTP 403 (" + rateLimitHint + ")",
		http.StatusTooManyRequests:     "pfx: HTTP 429 (" + rateLimitHint + ")",
		http.StatusNotFound:            "pfx: HTTP 404 (not found)",
		http.StatusInternalServerError: "pfx: HTTP 500",
		http.StatusOK:                  "pfx: HTTP 200",
	} {
		assert.EqualError(t, httpStatusError("pfx", &http.Response{StatusCode: status}), want)
	}
}

// failingClient returns a client whose every request fails with err.
func failingClient(err error) *http.Client {
	return &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, err })}
}

func TestResolveLatestVersionErrors(t *testing.T) {
	ctx := context.Background()
	serve := func(t *testing.T, h http.HandlerFunc) string {
		t.Helper()
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		return srv.URL
	}

	t.Run("invalid base URL", func(t *testing.T) {
		_, err := resolveLatestVersion(ctx, http.DefaultClient, "://bad", "o/r")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "creating release request")
	})
	t.Run("transport error", func(t *testing.T) {
		boom := errors.New("dial failed")
		_, err := resolveLatestVersion(ctx, failingClient(boom), "http://example.invalid", "o/r")
		require.ErrorIs(t, err, boom)
		assert.Contains(t, err.Error(), "checking o/r releases")
	})
	t.Run("non-redirect status", func(t *testing.T) {
		base := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) })
		_, err := resolveLatestVersion(ctx, http.DefaultClient, base, "o/r")
		assert.EqualError(t, err, "release lookup failed: HTTP 404 (not found)")
	})
	t.Run("redirect without location", func(t *testing.T) {
		base := serve(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusFound) })
		_, err := resolveLatestVersion(ctx, http.DefaultClient, base, "o/r")
		assert.EqualError(t, err, "release lookup failed: redirect has no location")
	})
	t.Run("redirect to non-tag path", func(t *testing.T) {
		var followed bool
		base := serve(t, func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/o/r/releases" {
				followed = true
				return
			}
			http.Redirect(w, r, "/o/r/releases", http.StatusFound)
		})
		_, err := resolveLatestVersion(ctx, http.DefaultClient, base, "o/r")
		assert.EqualError(t, err, "release lookup failed: o/r has no published release")
		assert.False(t, followed, "the latest redirect must not be followed")
	})
	t.Run("absolute redirect", func(t *testing.T) {
		base := serve(t, func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "https://elsewhere.example/o/r/releases/tag/v2.0.0", http.StatusMovedPermanently)
		})
		tag, err := resolveLatestVersion(ctx, http.DefaultClient, base+"/", "o/r")
		require.NoError(t, err)
		assert.Equal(t, "v2.0.0", tag)
	})
}

func TestFetchChecksum(t *testing.T) {
	ctx := context.Background()
	serveChecksums := func(t *testing.T, status int, body string) string {
		t.Helper()
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/o/r/releases/download/v1.0.0/checksums.txt" {
				t.Errorf("unexpected path %s", r.URL.Path)
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		t.Cleanup(srv.Close)
		return srv.URL
	}
	upper := strings.Repeat("AB", 32)

	t.Run("finds binary-mode entry and lowercases it", func(t *testing.T) {
		base := serveChecksums(t, http.StatusOK, "garbage line with many fields\n"+strings.Repeat("0", 64)+"  other.zip\n"+upper+" *app.zip\n")
		sum, err := fetchChecksum(ctx, http.DefaultClient, base, "o/r", "1.0.0", "app.zip")
		require.NoError(t, err)
		assert.Equal(t, strings.ToLower(upper), sum)
	})
	t.Run("missing asset entry", func(t *testing.T) {
		base := serveChecksums(t, http.StatusOK, upper+"  other.zip\n")
		_, err := fetchChecksum(ctx, http.DefaultClient, base, "o/r", "v1.0.0", "app.zip")
		assert.EqualError(t, err, "checksums.txt for v1.0.0 has no entry for app.zip")
	})
	t.Run("not found", func(t *testing.T) {
		base := serveChecksums(t, http.StatusNotFound, "")
		_, err := fetchChecksum(ctx, http.DefaultClient, base, "o/r", "1.0.0", "app.zip")
		assert.EqualError(t, err, "release v1.0.0 not found in o/r")
	})
	t.Run("server error", func(t *testing.T) {
		base := serveChecksums(t, http.StatusBadGateway, rateLimitBody)
		_, err := fetchChecksum(ctx, http.DefaultClient, base, "o/r", "1.0.0", "app.zip")
		assert.EqualError(t, err, "downloading checksums.txt: HTTP 502")
	})
	t.Run("rate limited", func(t *testing.T) {
		base := serveChecksums(t, http.StatusForbidden, rateLimitBody)
		_, err := fetchChecksum(ctx, http.DefaultClient, base, "o/r", "1.0.0", "app.zip")
		require.Error(t, err)
		assert.Contains(t, err.Error(), rateLimitHint)
		assert.NotContains(t, err.Error(), "203.0.113.7")
	})
	t.Run("overlong line", func(t *testing.T) {
		base := serveChecksums(t, http.StatusOK, strings.Repeat("x", 128*1024))
		_, err := fetchChecksum(ctx, http.DefaultClient, base, "o/r", "1.0.0", "app.zip")
		require.ErrorIs(t, err, bufio.ErrTooLong)
		assert.Contains(t, err.Error(), "reading checksums.txt")
	})
	t.Run("invalid base URL", func(t *testing.T) {
		_, err := fetchChecksum(ctx, http.DefaultClient, "://bad", "o/r", "1.0.0", "app.zip")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "creating checksums request")
	})
	t.Run("transport error", func(t *testing.T) {
		boom := errors.New("connection reset")
		_, err := fetchChecksum(ctx, failingClient(boom), "http://example.invalid", "o/r", "1.0.0", "app.zip")
		require.ErrorIs(t, err, boom)
		assert.Contains(t, err.Error(), "downloading checksums.txt for 1.0.0")
	})
}

func TestVerifyChecksum(t *testing.T) {
	path := filepath.Join(t.TempDir(), "asset.bin")
	require.NoError(t, os.WriteFile(path, []byte("payload"), 0o600))
	sum := sha256.Sum256([]byte("payload"))
	want := hex.EncodeToString(sum[:])

	require.NoError(t, verifyChecksum(path, want))

	err := verifyChecksum(path, strings.Repeat("0", 64))
	require.ErrorIs(t, err, errChecksumMismatch)
	assert.Contains(t, err.Error(), "expected "+strings.Repeat("0", 64)+", got "+want)

	err = verifyChecksum(filepath.Join(t.TempDir(), "missing.bin"), want)
	require.ErrorIs(t, err, os.ErrNotExist)
	assert.Contains(t, err.Error(), "opening download")
}
