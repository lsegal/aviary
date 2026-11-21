// Package testenv provides process-wide helpers for isolating Go tests from
// the user's real Aviary config and data directories.
package testenv

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	configHomeEnv = "AVIARY_TEST_CONFIG_HOME"
	configBaseEnv = "AVIARY_CONFIG_BASE_DIR"
)

var configHomeOnce sync.Once

// A shell spawned by a running Aviary exports AVIARY_CONFIG_BASE_DIR pointing
// at the real config directory, and config.BaseDir prefers it over every other
// source. Drop the inherited value so test binaries never write there; tests
// that need it still set it themselves with t.Setenv.
func init() {
	if IsGoTestProcess() {
		_ = os.Unsetenv(configBaseEnv)
	}
}

// GoTestConfigHome returns an isolated config root for Go test binaries.
// It returns an empty string outside go test.
func GoTestConfigHome() string {
	if !IsGoTestProcess() {
		return ""
	}
	if root := strings.TrimSpace(os.Getenv(configHomeEnv)); root != "" {
		return root
	}
	configHomeOnce.Do(func() {
		root, err := os.MkdirTemp("", "aviary-go-test-*")
		if err != nil {
			return
		}
		_ = os.Setenv(configHomeEnv, root)
	})
	return strings.TrimSpace(os.Getenv(configHomeEnv))
}

// IsGoTestProcess reports whether the current process is a `go test` binary.
func IsGoTestProcess() bool {
	base := strings.ToLower(filepath.Base(os.Args[0]))
	return strings.HasSuffix(base, ".test") || strings.HasSuffix(base, ".test.exe")
}
