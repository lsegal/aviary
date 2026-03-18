package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/lsegal/aviary/internal/store"
)

func TestMain(m *testing.M) {
	base, err := os.MkdirTemp("", "aviary-mcp-test-*")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(base) }()

	configHome := filepath.Join(base, "config")
	dataDir := filepath.Join(configHome, "aviary")

	if err := os.Setenv("XDG_CONFIG_HOME", configHome); err != nil {
		panic(err)
	}
	if err := os.Setenv("AVIARY_CONFIG_BASE_DIR", dataDir); err != nil {
		panic(err)
	}
	store.SetDataDir(dataDir)

	code := m.Run()
	store.SetDataDir("")
	os.Exit(code)
}
