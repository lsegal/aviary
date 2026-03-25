package mcp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	base, err := os.MkdirTemp("", "aviary-mcp-test-*")
	if err != nil {
		panic(err)
	}
	defer func() { _ = os.RemoveAll(base) }()

	configHome := filepath.Join(base, "config")

	if err := os.Setenv("XDG_CONFIG_HOME", configHome); err != nil {
		panic(err)
	}

	code := m.Run()
	os.Exit(code)
}
