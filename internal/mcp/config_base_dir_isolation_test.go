package mcp

import (
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInheritedConfigBaseDirIsIgnored reruns the config tool tests in a child
// test binary that inherits AVIARY_CONFIG_BASE_DIR, as it does from a shell an
// Aviary instance spawned, and checks nothing lands in that directory.
func TestInheritedConfigBaseDirIsIgnored(t *testing.T) {
	sentinel := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=^TestConfig", "-test.count=1")
	cmd.Env = append(os.Environ(), "AVIARY_CONFIG_BASE_DIR="+sentinel)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", out)

	entries, err := os.ReadDir(sentinel)
	require.NoError(t, err)
	assert.Empty(t, entries, "test wrote into the inherited AVIARY_CONFIG_BASE_DIR")
}
