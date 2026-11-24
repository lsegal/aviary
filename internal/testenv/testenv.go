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
	// checkoutEnv mirrors update.CheckoutEnv, which testenv cannot import.
	checkoutEnv = "AVIARY_CHECKOUT"
)

var configHomeOnce sync.Once

// A shell spawned by a running Aviary exports AVIARY_CONFIG_BASE_DIR pointing
// at the real config directory, and config.BaseDir prefers it over every other
// source. A dev server also exports AVIARY_CHECKOUT, which would make the
// updater pull the real checkout. Drop both inherited values so test binaries
// never touch either; tests that need them set them with t.Setenv.
func init() {
	if IsGoTestProcess() {
		_ = os.Unsetenv(configBaseEnv)
		_ = os.Unsetenv(checkoutEnv)
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
