package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
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
