// Package update implements Aviary version checks and binary upgrades.
package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lsegal/aviary/internal/buildinfo"
	"github.com/lsegal/aviary/internal/version"
)

const (
	// DefaultRepo is the canonical GitHub repository for Aviary releases.
	DefaultRepo = "lsegal/aviary"
	// CheckInterval is how often the server's self-updater looks for a new release.
	CheckInterval = 5 * time.Minute
	checkTTL      = CheckInterval
	// checkErrorBackoff is how long a failed check is reused before the
	// next attempt; it doubles with each consecutive failure (see
	// checkBackoff).
	checkErrorBackoff = 30 * time.Second
)

// CheckResult describes the local version and the latest known release.
type CheckResult struct {
	CurrentVersion   string    `json:"currentVersion"`
	LatestVersion    string    `json:"latestVersion,omitempty"`
	UpgradeAvailable bool      `json:"upgradeAvailable"`
	UpgradeSupported bool      `json:"upgradeSupported"`
	Emulated         bool      `json:"emulated"`
	CheckedAt        time.Time `json:"checkedAt,omitempty"`
	Message          string    `json:"message,omitempty"`
	// ReleaseURL links the latest version to its release page.
	ReleaseURL string `json:"releaseUrl,omitempty"`
	// Git is set in git mode, when a dev build runs from a source checkout:
	// updates are pulled from the checkout's upstream instead of a release.
	Git *GitState `json:"git,omitempty"`
	// NoCheckout is set for a dev build that would update in git mode but
	// cannot locate its source checkout, so it has no way to check for updates.
	NoCheckout bool `json:"noCheckout,omitempty"`
}

// noCheckoutMessage explains the NoCheckout state.
const noCheckoutMessage = "This dev build found no source checkout, so updates cannot be checked. Run Aviary with " + CheckoutEnv + " set to the checkout root (pnpm dev sets it)."

// InstallOptions controls how a release is downloaded and installed.
type InstallOptions struct {
	Version    string
	TargetPath string
	Repo       string
	// DownloadBase is the release host; see downloadBase.
	DownloadBase string
	Client       *http.Client
}

// InstallResult describes the outcome of an attempted upgrade install.
type InstallResult struct {
	Version  string `json:"version"`
	Emulated bool   `json:"emulated"`
	Noop     bool   `json:"noop"`
}

var (
	emulateMu     sync.RWMutex
	emulateLocal  string
	emulateRemote string

	cacheMu       sync.RWMutex
	cachedCheck   CheckResult
	cachedErr     error
	cachedGit     bool
	cacheExpiry   time.Time
	checkFailures int

	// checkSem lets one version check run at a time.
	checkSem = make(chan struct{}, 1)

	renameFile = os.Rename
)

// ConfigureEmulation enables dev-only version emulation using <local>:<remote>.
func ConfigureEmulation(raw string) error {
	emulateMu.Lock()
	defer emulateMu.Unlock()

	emulateLocal = ""
	emulateRemote = ""
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if !IsDevBuild() {
		return fmt.Errorf("AVIARY_EMULATE_VERSIONS is only supported for dev builds")
	}
	parts := strings.SplitN(strings.TrimSpace(raw), ":", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid AVIARY_EMULATE_VERSIONS value %q: expected <local>:<remote>", raw)
	}
	local := strings.TrimSpace(parts[0])
	remote := strings.TrimSpace(parts[1])
	if _, err := version.ParseRelease(local); err != nil {
		return fmt.Errorf("invalid emulated local version: %w", err)
	}
	if _, err := version.ParseRelease(remote); err != nil {
		return fmt.Errorf("invalid emulated remote version: %w", err)
	}
	emulateLocal = local
	emulateRemote = remote
	cacheExpiry = time.Time{}
	return nil
}

// EmulationActive reports whether dev-only version emulation is enabled.
func EmulationActive() bool {
	emulateMu.RLock()
	defer emulateMu.RUnlock()
	return emulateLocal != "" && emulateRemote != ""
}

// IsDevBuild reports whether the current binary version is not a clean semver release.
func IsDevBuild() bool {
	_, err := version.ParseRelease(buildinfo.Version)
	return err != nil
}

// CurrentVersion returns the effective local version, honoring emulation when
// enabled. A dev build reports the full commit SHA of the code it runs: the
// git-mode checkout's HEAD (see checkoutHead, which caches it for headTTL so
// request paths do not run git), else the VCS revision Go embedded in the
// binary, else "dev". ctx names the caller of any git command it runs (see
// WithCaller).
func CurrentVersion(ctx context.Context) string {
	emulateMu.RLock()
	local := emulateLocal
	emulateMu.RUnlock()
	if local != "" {
		return local
	}
	if !IsDevBuild() {
		return buildinfo.Version
	}
	if dir := GitCheckout(); dir != "" {
		if sha := checkoutHead(ctx, dir); sha != "" {
			return sha
		}
	}
	if rev := buildRevision(); rev != "" {
		return rev
	}
	return buildinfo.Version
}

// buildRevision returns the VCS revision embedded in the binary, if any.
var buildRevision = func() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == "vcs.revision" {
			return s.Value
		}
	}
	return ""
}

// Check compares the current version with the latest available release, or
// in git mode with the source checkout's upstream branch.
func Check(ctx context.Context, client *http.Client) (CheckResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if gitModeEligible() {
		if dir := checkoutDir(); dir != "" {
			return cachedGitCheck(ctx, dir)
		}
		return CheckResult{CurrentVersion: CurrentVersion(ctx), CheckedAt: time.Now().UTC(), NoCheckout: true, Message: noCheckoutMessage}, nil
	}
	if EmulationActive() {
		emulateMu.RLock()
		local := emulateLocal
		remote := emulateRemote
		emulateMu.RUnlock()
		return compareVersions(local, remote, true)
	}

	return throttledCheck(ctx, false, func() (CheckResult, error) {
		latest, err := resolveLatestVersion(ctx, client, "", DefaultRepo)
		if err != nil {
			current := CurrentVersion(ctx)
			out := CheckResult{
				CurrentVersion:   current,
				UpgradeSupported: isComparableVersion(current),
				Message:          err.Error(),
			}
			return out, err
		}
		return compareVersions(CurrentVersion(ctx), latest, false)
	})
}

func cachedGitCheck(ctx context.Context, dir string) (CheckResult, error) {
	out, err := throttledCheck(ctx, true, func() (CheckResult, error) {
		return gitCheckResult(ctx, dir)
	})
	out.CurrentVersion = CurrentVersion(ctx)
	return out, err
}

// throttledCheck returns the cached result of the last check of the same kind
// (git or release) while it is fresh, and otherwise runs check. Checks run one
// at a time, so callers that arrive during a check share its result. A
// success is reused for checkTTL; a failure is reused for checkBackoff, which
// grows with each consecutive failure, so a broken network or checkout is not
// retried on every call. A failure caused by ctx ending is not cached.
func throttledCheck(ctx context.Context, git bool, check func() (CheckResult, error)) (CheckResult, error) {
	select {
	case checkSem <- struct{}{}:
	case <-ctx.Done():
		return CheckResult{CurrentVersion: CurrentVersion(ctx)}, fmt.Errorf("waiting for another version check: %w", ctx.Err())
	}
	defer func() { <-checkSem }()

	cacheMu.RLock()
	if time.Now().Before(cacheExpiry) && cachedGit == git {
		out, err := cachedCheck, cachedErr
		cacheMu.RUnlock()
		return out, err
	}
	cacheMu.RUnlock()

	out, err := check()
	if err != nil && ctx.Err() != nil {
		return out, err // the caller gave up; that says nothing about the next check
	}

	cacheMu.Lock()
	defer cacheMu.Unlock()
	cachedCheck, cachedErr, cachedGit = out, err, git
	if err != nil {
		checkFailures++
		cacheExpiry = time.Now().Add(checkBackoff(checkFailures))
	} else {
		checkFailures = 0
		cacheExpiry = time.Now().Add(checkTTL)
	}
	return out, err
}

// checkBackoff is how long the failure of the n-th consecutive check is
// reused before the next attempt: checkErrorBackoff, doubling per failure, up
// to checkTTL.
func checkBackoff(n int) time.Duration {
	d := checkErrorBackoff
	for i := 1; i < n && d < checkTTL; i++ {
		d *= 2
	}
	return min(d, checkTTL)
}

// Install downloads and installs the requested Aviary release into TargetPath.
func Install(ctx context.Context, opts InstallOptions) (InstallResult, error) {
	if opts.Client == nil {
		opts.Client = http.DefaultClient
	}
	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if strings.TrimSpace(opts.TargetPath) == "" {
		return InstallResult{}, fmt.Errorf("target path is required")
	}

	version := NormalizeVersion(opts.Version)
	if version == "" {
		check, err := Check(ctx, opts.Client)
		if err != nil && check.LatestVersion == "" {
			return InstallResult{}, err
		}
		version = NormalizeVersion(check.LatestVersion)
	}
	if version == "" {
		return InstallResult{}, fmt.Errorf("unable to resolve target version")
	}

	if EmulationActive() {
		return InstallResult{Version: version, Emulated: true, Noop: true}, nil
	}

	assetName := assetBaseName(version, runtime.GOOS, runtime.GOARCH) + ".tar.gz"
	wantSum, err := fetchChecksum(ctx, opts.Client, opts.DownloadBase, opts.Repo, version, assetName)
	if err != nil {
		return InstallResult{}, err
	}
	assetURL := releaseDownloadURL(opts.DownloadBase, opts.Repo, version, assetName)

	tmpDir, err := os.MkdirTemp("", "aviary-upgrade-*")
	if err != nil {
		return InstallResult{}, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	archivePath := filepath.Join(tmpDir, assetName)
	if err := downloadAsset(ctx, opts.Client, opts.Repo, version, assetName, assetURL, archivePath); err != nil {
		return InstallResult{}, err
	}
	if err := verifyChecksum(archivePath, wantSum); err != nil {
		return InstallResult{}, err
	}
	binaryName := "aviary"
	if runtime.GOOS == "windows" {
		binaryName = "aviary.exe"
	}
	extractedPath, err := extractBinary(archivePath, tmpDir, binaryName)
	if err != nil {
		return InstallResult{}, err
	}
	if err := replaceFile(extractedPath, opts.TargetPath); err != nil {
		return InstallResult{}, err
	}
	cacheMu.Lock()
	cacheExpiry = time.Time{}
	cacheMu.Unlock()
	return InstallResult{Version: version}, nil
}

// NormalizeVersion returns version as a release tag with a leading "v", so
// "1.2.3" and "v1.2.3" name the same release. Empty input stays empty.
func NormalizeVersion(version string) string {
	v := strings.TrimPrefix(strings.TrimSpace(version), "v")
	if v == "" {
		return ""
	}
	return "v" + v
}

// SameVersion reports whether a and b name the same release, ignoring a
// leading "v". Non-semver versions compare as plain strings.
func SameVersion(a, b string) bool {
	pa, errA := version.ParseRelease(a)
	pb, errB := version.ParseRelease(b)
	if errA != nil || errB != nil {
		return NormalizeVersion(a) == NormalizeVersion(b)
	}
	return version.CompareReleases(pa, pb) == 0
}

// ReleaseURL returns the GitHub release page for version in repo.
func ReleaseURL(repo, version string) string {
	if repo == "" {
		repo = DefaultRepo
	}
	return fmt.Sprintf("https://github.com/%s/releases/tag/%s", repo, NormalizeVersion(version))
}

// CheckMessage returns a short human-readable summary for a version check result.
func CheckMessage(check CheckResult) string {
	if check.LatestVersion == "" {
		if check.Message != "" {
			return check.Message
		}
		return "unable to determine latest version"
	}
	if check.UpgradeAvailable {
		return fmt.Sprintf("new version available: %s", check.LatestVersion)
	}
	return "already up to date"
}

func compareVersions(current, latest string, emulated bool) (CheckResult, error) {
	out := CheckResult{
		CurrentVersion: current,
		LatestVersion:  latest,
		Emulated:       emulated,
		CheckedAt:      time.Now().UTC(),
	}
	if latest != "" {
		out.ReleaseURL = ReleaseURL(DefaultRepo, latest)
	}
	cur, err := version.ParseRelease(current)
	if err != nil {
		out.UpgradeSupported = false
		out.Message = "current build version is not a semver release"
		return out, nil
	}
	lat, err := version.ParseRelease(latest)
	if err != nil {
		out.UpgradeSupported = false
		out.Message = "latest release version is not valid semver"
		return out, err
	}
	out.UpgradeSupported = true
	out.UpgradeAvailable = version.CompareReleases(cur, lat) < 0
	return out, nil
}

func isComparableVersion(v string) bool {
	_, err := version.ParseRelease(v)
	return err == nil
}

func assetBaseName(version, goos, goarch string) string {
	return fmt.Sprintf("aviary_%s_%s_%s", version, goos, goarch)
}

func downloadFile(ctx context.Context, client *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("creating download request: %w", err)
	}
	res, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("downloading release asset: %w", err)
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return httpStatusError("downloading release asset", res)
	}
	f, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("creating download file: %w", err)
	}
	defer f.Close() //nolint:errcheck
	if _, err := io.Copy(f, res.Body); err != nil {
		return fmt.Errorf("writing download file: %w", err)
	}
	return nil
}

func extractBinary(archivePath, dir, name string) (string, error) {
	f, err := os.Open(archivePath)
	if err != nil {
		return "", fmt.Errorf("opening archive: %w", err)
	}
	defer f.Close() //nolint:errcheck

	gz, err := gzip.NewReader(f)
	if err != nil {
		return "", fmt.Errorf("reading archive: %w", err)
	}
	defer gz.Close() //nolint:errcheck

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return "", fmt.Errorf("reading archive entry: %w", err)
		}
		if filepath.Base(hdr.Name) != name {
			continue
		}
		out := filepath.Join(dir, name)
		dst, err := os.OpenFile(out, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
		if err != nil {
			return "", fmt.Errorf("creating extracted binary: %w", err)
		}
		if _, err := io.Copy(dst, tr); err != nil {
			dst.Close() //nolint:errcheck
			return "", fmt.Errorf("extracting binary: %w", err)
		}
		if err := dst.Close(); err != nil {
			return "", fmt.Errorf("closing extracted binary: %w", err)
		}
		return out, nil
	}
	return "", fmt.Errorf("binary %q not found in release archive", name)
}

func replaceFile(src, target string) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return fmt.Errorf("creating target dir: %w", err)
	}
	backup := target + ".bak"
	_ = os.Remove(backup)
	if _, err := os.Stat(target); err == nil {
		if err := renameWithRetry(target, backup); err != nil {
			return fmt.Errorf("backing up existing binary: %w", err)
		}
	}
	if err := installBinary(src, target); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = renameWithRetry(backup, target)
		}
		return fmt.Errorf("installing new binary: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}

func installBinary(src, target string) error {
	if err := renameWithRetry(src, target); err == nil {
		return nil
	} else if !isCrossDeviceRename(err) {
		return err
	}

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source binary: %w", err)
	}
	tempTarget, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+".tmp-*")
	if err != nil {
		return fmt.Errorf("creating staged binary: %w", err)
	}
	tempPath := tempTarget.Name()
	defer os.Remove(tempPath) //nolint:errcheck

	srcFile, err := os.Open(src)
	if err != nil {
		tempTarget.Close() //nolint:errcheck
		return fmt.Errorf("opening source binary: %w", err)
	}

	if _, err := io.Copy(tempTarget, srcFile); err != nil {
		srcFile.Close()    //nolint:errcheck
		tempTarget.Close() //nolint:errcheck
		return fmt.Errorf("copying source binary: %w", err)
	}
	if err := srcFile.Close(); err != nil {
		tempTarget.Close() //nolint:errcheck
		return fmt.Errorf("closing source binary: %w", err)
	}
	if err := tempTarget.Close(); err != nil {
		return fmt.Errorf("closing staged binary: %w", err)
	}
	if err := os.Chmod(tempPath, info.Mode()); err != nil {
		return fmt.Errorf("setting staged binary mode: %w", err)
	}
	if err := renameWithRetry(tempPath, target); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("removing source binary: %w", err)
	}
	return nil
}

func isCrossDeviceRename(err error) bool {
	return errors.Is(err, syscall.EXDEV)
}

// ClearCache forgets the cached version check, and any failure backoff, so
// the next Check asks GitHub or the checkout's upstream, and forgets the
// cached checkout HEAD so the next CurrentVersion reads it again. Callers use
// it after a pull or an install, or before a scheduled check.
func ClearCache() {
	cacheMu.Lock()
	cacheExpiry = time.Time{}
	checkFailures = 0
	cacheMu.Unlock()
	expireHead()
}
