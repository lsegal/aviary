package cmd

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/clidoctor"
	"github.com/lsegal/aviary/internal/startup"
	"github.com/lsegal/aviary/internal/store"
)

func TestWriteProjectsAgentGates_ListsAgy(t *testing.T) {
	registry, err := clidefs.Load(nil)
	require.NoError(t, err)
	var out strings.Builder
	writeProjectsAgentGates(&out, registry)
	assert.Contains(t, out.String(), "agy")
	assert.Contains(t, out.String(), "unavailable for Code Loop: agy has no machine-readable output mode")
	assert.NotContains(t, out.String(), "claude")
}

func TestWriteProjectsAgentGates_UngatedByConfig(t *testing.T) {
	registry, err := clidefs.Load(map[string]any{"agy": map[string]any{"projectsUnavailable": nil}})
	require.NoError(t, err)
	var out strings.Builder
	writeProjectsAgentGates(&out, registry)
	assert.Equal(t, "  (every agent can run Code Loop jobs)\n", out.String())
}

func TestDescribeCLIBackend(t *testing.T) {
	ok, line := describeCLIBackend(clidoctor.Report{Name: "codex", Binary: "codex", Auth: clidoctor.Unknown})
	assert.False(t, ok)
	assert.Equal(t, "codex CLI not found in PATH", line)

	ok, line = describeCLIBackend(clidoctor.Report{Binary: "claude", Path: "/opt/claude", Version: "2.1.312 (Claude Code)", Auth: clidoctor.SignedIn, AuthDetail: "claude.ai"})
	assert.True(t, ok)
	assert.Equal(t, "claude 2.1.312, signed in (claude.ai)", line)

	ok, line = describeCLIBackend(clidoctor.Report{Binary: "claude", Path: "/opt/claude", Version: "2.1", Auth: clidoctor.SignedOut})
	assert.False(t, ok)
	assert.Equal(t, "claude 2.1, not signed in", line)

	ok, line = describeCLIBackend(clidoctor.Report{Binary: "codex", Path: "/opt/codex", Version: "codex-cli 0.1.0", Auth: clidoctor.SignedIn, VersionNote: "older than the required 0.2.0"})
	assert.False(t, ok)
	assert.Equal(t, "codex 0.1.0, signed in; version older than the required 0.2.0", line)

	// The quota comes from the same readers provider_cli_list and Code Loop
	// use, so a tracked CLI reports its reading or why it has none.
	_, line = describeCLIBackend(clidoctor.Report{Binary: "codex", Path: "/opt/codex", Auth: clidoctor.SignedIn, TracksQuota: true, Quota: "weekly 60% left"})
	assert.Equal(t, "codex, signed in; quota weekly 60% left", line)
	_, line = describeCLIBackend(clidoctor.Report{Binary: "codex", Path: "/opt/codex", Auth: clidoctor.SignedIn, TracksQuota: true, QuotaErr: errors.New("boom")})
	assert.Equal(t, "codex, signed in; quota unavailable: boom", line)
}

func TestDoctorServerStatus_ReadsPIDFileBesideConfig(t *testing.T) {
	// A server started with --config writes its PID file beside that config,
	// wherever the default config dir is.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AVIARY_CONFIG_BASE_DIR", "")
	t.Setenv("AVIARY_PID_FILE", "")
	cfgPath := filepath.Join(t.TempDir(), "aviary.yaml")
	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "aviary.pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600))

	got := doctorServerStatus(cfgPath, freePort(t))
	assert.Contains(t, got, "running (PID "+strconv.Itoa(os.Getpid()))
}

func TestDoctorBootSummary_ShowsRunningServersBoot(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("AVIARY_CONFIG_BASE_DIR", "")
	t.Setenv("AVIARY_PID_FILE", "")
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	cfgPath := filepath.Join(t.TempDir(), "aviary.yaml")
	assert.Empty(t, doctorBootSummary(cfgPath), "no server is running")

	require.NoError(t, os.WriteFile(filepath.Join(filepath.Dir(cfgPath), "aviary.pid"), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o600))
	assert.Empty(t, doctorBootSummary(cfgPath), "the server saved no boot report")

	require.NoError(t, store.WriteJSON(startup.Path(), startup.Report{
		PID:        os.Getpid(),
		MCPReadyMS: 400,
		TotalMS:    3200,
		Ready:      true,
		Phases:     []startup.Phase{{Name: "channels connect", StartMS: 400, DurationMS: 2800, Background: true}},
	}))
	assert.Equal(t, "boot: 3.20s, MCP ready at 0.40s; slowest: channels connect 2.80s", doctorBootSummary(cfgPath))

	require.NoError(t, store.WriteJSON(startup.Path(), startup.Report{PID: os.Getpid() + 1, MCPReadyMS: 400}))
	assert.Empty(t, doctorBootSummary(cfgPath), "a report from an earlier process is stale")
}

func TestDoctorServerStatus_ProbesPortWithoutPIDFile(t *testing.T) {
	t.Setenv("AVIARY_PID_FILE", "")
	cfgPath := filepath.Join(t.TempDir(), "aviary.yaml")
	port := freePort(t)
	assert.Equal(t, "not running", doctorServerStatus(cfgPath, port))

	ln, err := net.Listen("tcp", net.JoinHostPort("localhost", strconv.Itoa(port)))
	require.NoError(t, err)
	defer ln.Close() //nolint:errcheck
	got := doctorServerStatus(cfgPath, port)
	assert.Contains(t, got, "running (answering on port "+strconv.Itoa(port))
	assert.Contains(t, got, filepath.Join(filepath.Dir(cfgPath), "aviary.pid"))
}

// freePort returns a localhost TCP port nothing listens on.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

func TestDescribeBrowserProfile(t *testing.T) {
	owner := &browser.ProfileOwner{PID: 4242, ConfigDir: "/home/u/.config/aviary", DevTools: "http://127.0.0.1:9222"}
	held := describeBrowserProfile(browser.ProfileStatus{Held: true, Owner: owner})
	assert.Contains(t, held, "pid 4242")
	assert.Contains(t, held, "config dir /home/u/.config/aviary")
	assert.Contains(t, held, "stop that instance")

	reclaimed := describeBrowserProfile(browser.ProfileStatus{Reclaimed: true, Owner: owner})
	assert.True(t, strings.HasPrefix(reclaimed, "[OK] lock free"), reclaimed)
	assert.Contains(t, reclaimed, "cleared a stale lock record")

	assert.Equal(t, "[OK] lock free; Aviary's browser is running on it", describeBrowserProfile(browser.ProfileStatus{ChromeRunning: true}))
}
