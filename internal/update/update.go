// Package update implements Aviary version checks and binary upgrades.
package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lsegal/aviary/internal/buildinfo"
)

const (
	// DefaultRepo is the canonical GitHub repository for Aviary releases.
	DefaultRepo = "lsegal/aviary"
	// DefaultAPIBase is the GitHub API base used for release lookups.
	DefaultAPIBase = "https://api.github.com"
	checkTTL       = 15 * time.Minute
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
}

// InstallOptions controls how a release is downloaded and installed.
type InstallOptions struct {
	Version    string
	TargetPath string
	Repo       string
	APIBase    string
	Client     *http.Client
}

// InstallResult describes the outcome of an attempted upgrade install.
type InstallResult struct {
	Version  string `json:"version"`
	Emulated bool   `json:"emulated"`
	Noop     bool   `json:"noop"`
}

type releasePayload struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
	} `json:"assets"`
}

type parsedVersion struct {
	major int
	minor int
	patch int
}

var (
	emulateMu     sync.RWMutex
	emulateLocal  string
	emulateRemote string

	cacheMu     sync.RWMutex
	cachedCheck CheckResult
	cacheExpiry time.Time

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
	if _, err := parseSemver(local); err != nil {
		return fmt.Errorf("invalid emulated local version: %w", err)
	}
	if _, err := parseSemver(remote); err != nil {
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
	_, err := parseSemver(buildinfo.Version)
	return err != nil
}

// CurrentVersion returns the effective local version, honoring emulation when enabled.
func CurrentVersion() string {
	emulateMu.RLock()
	defer emulateMu.RUnlock()
	if emulateLocal != "" {
		return emulateLocal
	}
	return buildinfo.Version
}

// Check compares the current version with the latest available release.
func Check(ctx context.Context, client *http.Client) (CheckResult, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if EmulationActive() {
		emulateMu.RLock()
		local := emulateLocal
		remote := emulateRemote
		emulateMu.RUnlock()
		return compareVersions(local, remote, true)
	}

	cacheMu.RLock()
	if time.Now().Before(cacheExpiry) {
		out := cachedCheck
		cacheMu.RUnlock()
		return out, nil
	}
	cacheMu.RUnlock()

	release, err := fetchLatestRelease(ctx, client, DefaultRepo, DefaultAPIBase)
	if err != nil {
		current := CurrentVersion()
		out := CheckResult{
			CurrentVersion:   current,
			UpgradeSupported: isComparableVersion(current),
			Message:          err.Error(),
		}
		return out, err
	}
	out, cmpErr := compareVersions(CurrentVersion(), release.TagName, false)
	if cmpErr != nil {
		return out, cmpErr
	}
	cacheMu.Lock()
	cachedCheck = out
	cacheExpiry = time.Now().Add(checkTTL)
	cacheMu.Unlock()
	return out, nil
}

// Install downloads and installs the requested Aviary release into TargetPath.
func Install(ctx context.Context, opts InstallOptions) (InstallResult, error) {
	if opts.Client == nil {
		opts.Client = http.DefaultClient
	}
	if opts.Repo == "" {
		opts.Repo = DefaultRepo
	}
	if opts.APIBase == "" {
		opts.APIBase = DefaultAPIBase
	}
	if strings.TrimSpace(opts.TargetPath) == "" {
		return InstallResult{}, fmt.Errorf("target path is required")
	}

	version := strings.TrimSpace(opts.Version)
	if version == "" {
		check, err := Check(ctx, opts.Client)
		if err != nil && check.LatestVersion == "" {
			return InstallResult{}, err
		}
		version = check.LatestVersion
	}
	if version == "" {
		return InstallResult{}, fmt.Errorf("unable to resolve target version")
	}

	if EmulationActive() {
		return InstallResult{Version: version, Emulated: true, Noop: true}, nil
	}

	release, err := fetchRelease(ctx, opts.Client, opts.Repo, opts.APIBase, version)
	if err != nil {
		return InstallResult{}, err
	}
	assetName := assetBaseName(version, runtime.GOOS, runtime.GOARCH) + ".tar.gz"
	assetURL := ""
	for _, asset := range release.Assets {
		if asset.Name == assetName {
			assetURL = asset.BrowserDownloadURL
			break
		}
	}
	if assetURL == "" {
		assetURL = fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", opts.Repo, version, assetName)
	}

	tmpDir, err := os.MkdirTemp("", "aviary-upgrade-*")
	if err != nil {
		return InstallResult{}, fmt.Errorf("creating temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir) //nolint:errcheck

	archivePath := filepath.Join(tmpDir, assetName)
	if err := downloadFile(ctx, opts.Client, assetURL, archivePath); err != nil {
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
	cur, err := parseSemver(current)
	if err != nil {
		out.UpgradeSupported = false
		out.Message = "current build version is not a semver release"
		return out, nil
	}
	lat, err := parseSemver(latest)
	if err != nil {
		out.UpgradeSupported = false
		out.Message = "latest release version is not valid semver"
		return out, err
	}
	out.UpgradeSupported = true
	out.UpgradeAvailable = compareSemver(cur, lat) < 0
	return out, nil
}

func isComparableVersion(v string) bool {
	_, err := parseSemver(v)
	return err == nil
}

func fetchLatestRelease(ctx context.Context, client *http.Client, repo, apiBase string) (releasePayload, error) {
	return fetchRelease(ctx, client, repo, apiBase, "")
}

func fetchRelease(ctx context.Context, client *http.Client, repo, apiBase, version string) (releasePayload, error) {
	url := fmt.Sprintf("%s/repos/%s/releases/latest", strings.TrimRight(apiBase, "/"), repo)
	if strings.TrimSpace(version) != "" {
		url = fmt.Sprintf("%s/repos/%s/releases/tags/%s", strings.TrimRight(apiBase, "/"), repo, version)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return releasePayload{}, fmt.Errorf("creating release request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	res, err := client.Do(req)
	if err != nil {
		return releasePayload{}, fmt.Errorf("checking %s releases: %w", repo, err)
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return releasePayload{}, fmt.Errorf("release lookup failed: %s %s", res.Status, strings.TrimSpace(string(body)))
	}
	var release releasePayload
	if err := json.NewDecoder(res.Body).Decode(&release); err != nil {
		return releasePayload{}, fmt.Errorf("decoding release metadata: %w", err)
	}
	return release, nil
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
		return fmt.Errorf("downloading release asset: %s", res.Status)
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
		if err := renameFile(target, backup); err != nil {
			return fmt.Errorf("backing up existing binary: %w", err)
		}
	}
	if err := installBinary(src, target); err != nil {
		if _, statErr := os.Stat(backup); statErr == nil {
			_ = renameFile(backup, target)
		}
		return fmt.Errorf("installing new binary: %w", err)
	}
	_ = os.Remove(backup)
	return nil
}

func installBinary(src, target string) error {
	if err := renameFile(src, target); err == nil {
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
	if err := renameFile(tempPath, target); err != nil {
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

func parseSemver(raw string) (parsedVersion, error) {
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(v, "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return parsedVersion{}, fmt.Errorf("%q is not X.Y.Z semver", raw)
	}
	parsePart := func(s string) (int, error) {
		if s == "" {
			return 0, fmt.Errorf("empty semver segment")
		}
		for _, r := range s {
			if r < '0' || r > '9' {
				return 0, fmt.Errorf("invalid semver segment %q", s)
			}
		}
		return strconv.Atoi(s)
	}
	major, err := parsePart(parts[0])
	if err != nil {
		return parsedVersion{}, err
	}
	minor, err := parsePart(parts[1])
	if err != nil {
		return parsedVersion{}, err
	}
	patch, err := parsePart(parts[2])
	if err != nil {
		return parsedVersion{}, err
	}
	return parsedVersion{major: major, minor: minor, patch: patch}, nil
}

func compareSemver(a, b parsedVersion) int {
	switch {
	case a.major != b.major:
		return cmpInt(a.major, b.major)
	case a.minor != b.minor:
		return cmpInt(a.minor, b.minor)
	default:
		return cmpInt(a.patch, b.patch)
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
