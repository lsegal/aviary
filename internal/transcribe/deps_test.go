package transcribe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTool writes an executable script named name into a temp dir and makes
// lookPath find it (and nothing else). The script records its arguments to
// the returned file, prints output, and exits with code.
func fakeTool(t *testing.T, name, output string, code int) (argsFile string) {
	t.Helper()
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	var path, script string
	if runtime.GOOS == "windows" {
		path = filepath.Join(dir, name+".bat")
		var lines strings.Builder
		lines.WriteString("@echo off\r\necho %*>>\"" + argsFile + "\"\r\n")
		for line := range strings.SplitSeq(output, "\n") {
			lines.WriteString("echo " + line + "\r\n")
		}
		lines.WriteString("exit /b " + strconv.Itoa(code) + "\r\n")
		script = lines.String()
	} else {
		path = filepath.Join(dir, name)
		script = "#!/bin/sh\necho \"$@\" >> '" + argsFile + "'\nprintf '%s\\n' '" + strings.ReplaceAll(output, "\n", "' '") + "'\nexit " + strconv.Itoa(code) + "\n"
	}
	require.NoError(t, os.WriteFile(path, []byte(script), 0o755)) //nolint:gosec // test script must be executable
	orig := lookPath
	lookPath = func(n string) (string, error) {
		if n == name {
			return path, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = orig })
	return argsFile
}

// useGOOS makes installPackages choose the package manager for os.
func useGOOS(t *testing.T, os string) {
	t.Helper()
	orig := goos
	goos = os
	t.Cleanup(func() { goos = orig })
}

func readArgs(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var lines []string
	for line := range strings.SplitSeq(strings.TrimSpace(strings.ReplaceAll(string(data), "\r", "")), "\n") {
		lines = append(lines, strings.TrimSpace(line))
	}
	return lines
}

func TestInstallPackages_BrewSuccessLogsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("brew is not used on Windows")
	}
	args := fakeTool(t, "brew", "installed whisper-cpp\ninstalled ffmpeg", 0)
	var log []string
	err := installPackages(context.Background(), []string{"whisper-cpp", "ffmpeg"}, func(s string) { log = append(log, s) })
	require.NoError(t, err)
	assert.Equal(t, []string{"install whisper-cpp ffmpeg"}, readArgs(t, args))
	assert.Equal(t, []string{"installed whisper-cpp\ninstalled ffmpeg"}, log)
}

func TestInstallPackages_BrewFailureReportsFirstLine(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("brew is not used on Windows")
	}
	fakeTool(t, "brew", "Error: no formula ffmpeg\nsecond line", 1)
	err := installPackages(context.Background(), []string{"ffmpeg"}, func(string) { t.Fatal("failure must not log output") })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "brew install ffmpeg failed")
	assert.Contains(t, err.Error(), "Error: no formula ffmpeg")
	assert.NotContains(t, err.Error(), "second line")
}

func TestInstallPackages_WingetInstallsEachPackageByID(t *testing.T) {
	useGOOS(t, "windows")
	args := fakeTool(t, "winget", "ok", 0)
	err := installPackages(context.Background(), []string{"whisper-cpp", "ffmpeg"}, func(string) {})
	require.NoError(t, err)
	assert.Equal(t, []string{
		"install --silent --accept-package-agreements --accept-source-agreements --id ggerganov.whisper.cpp",
		"install --silent --accept-package-agreements --accept-source-agreements --id Gyan.FFmpeg",
	}, readArgs(t, args))
}

func TestInstallPackages_WingetFailureReportsFirstLine(t *testing.T) {
	useGOOS(t, "windows")
	fakeTool(t, "winget", "No package found\nmore", 1)
	err := installPackages(context.Background(), []string{"ffmpeg"}, func(string) {})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "winget install Gyan.FFmpeg failed")
	assert.Contains(t, err.Error(), "No package found")
}

func TestInstallPackages_NoPackageManager(t *testing.T) {
	useTempDataDir(t)
	stubPath(t)
	err := installPackages(context.Background(), []string{"whisper-cpp", "ffmpeg"}, func(string) { t.Fatal("nothing should be logged") })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no supported package manager found")
	assert.Contains(t, err.Error(), "whisper-cpp and ffmpeg")
	assert.Contains(t, err.Error(), filepath.Join(DepsDir(), "bin"))
}

// useModelServer points model downloads at handler.
func useModelServer(t *testing.T, handler http.HandlerFunc) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	orig := modelBaseURL
	modelBaseURL = srv.URL + "/"
	t.Cleanup(func() { modelBaseURL = orig })
}

// assertNoDownload checks that dest and no temporary download file exist.
func assertNoDownload(t *testing.T, dest string) {
	t.Helper()
	assert.NoFileExists(t, dest)
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(dest), ".download-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers)
}

func TestDownloadModel_Non200(t *testing.T) {
	useModelServer(t, func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	})
	dest := filepath.Join(t.TempDir(), "models", "ggml-tiny.bin")
	err := downloadModel(context.Background(), "tiny", dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downloading model tiny")
	assert.Contains(t, err.Error(), "404")
	assert.NoDirExists(t, filepath.Dir(dest))
}

func TestDownloadModel_TruncatedBodyLeavesNoFile(t *testing.T) {
	useModelServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		_, _ = w.Write([]byte("partial"))
	})
	dest := filepath.Join(t.TempDir(), "ggml-tiny.bin")
	err := downloadModel(context.Background(), "tiny", dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downloading model tiny")
	assertNoDownload(t, dest)
}

func TestDownloadModel_UnwritableDestination(t *testing.T) {
	useModelServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("model-bytes"))
	})
	// The destination's parent is a regular file, so no directory can be made.
	blocker := filepath.Join(t.TempDir(), "file")
	require.NoError(t, os.WriteFile(blocker, nil, 0o600))
	dest := filepath.Join(blocker, "ggml-tiny.bin")
	require.Error(t, downloadModel(context.Background(), "tiny", dest))
	assert.NoFileExists(t, dest)
}

func TestDownloadModel_DestinationIsDirectory(t *testing.T) {
	useModelServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("model-bytes"))
	})
	// A non-empty directory at dest makes the final rename fail.
	dest := filepath.Join(t.TempDir(), "ggml-tiny.bin")
	require.NoError(t, os.MkdirAll(filepath.Join(dest, "keep"), 0o755))
	require.Error(t, downloadModel(context.Background(), "tiny", dest))
	assert.DirExists(t, filepath.Join(dest, "keep"))
	leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(dest), ".download-*"))
	require.NoError(t, err)
	assert.Empty(t, leftovers)
}

func TestDownloadModel_ConnectionError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	orig := modelBaseURL
	modelBaseURL = url + "/"
	t.Cleanup(func() { modelBaseURL = orig })
	dest := filepath.Join(t.TempDir(), "ggml-tiny.bin")
	err := downloadModel(context.Background(), "tiny", dest)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "downloading model tiny")
	assertNoDownload(t, dest)
}

func TestDownloadModel_InvalidURL(t *testing.T) {
	orig := modelBaseURL
	modelBaseURL = "://bad/"
	t.Cleanup(func() { modelBaseURL = orig })
	dest := filepath.Join(t.TempDir(), "ggml-tiny.bin")
	require.Error(t, downloadModel(context.Background(), "tiny", dest))
	assertNoDownload(t, dest)
}
