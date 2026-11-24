package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteWatchPortFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "port")
	t.Setenv(watchPortFileEnv, path)

	remove, err := writeWatchPortFile(16679)
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "16679\n", string(data))

	remove()
	_, err = os.Stat(path)
	assert.True(t, os.IsNotExist(err), "the port file is removed when the session ends")
}

func TestWriteWatchPortFileUnset(t *testing.T) {
	t.Setenv(watchPortFileEnv, "")
	remove, err := writeWatchPortFile(16677)
	require.NoError(t, err)
	remove()
}

func TestWriteWatchPortFileError(t *testing.T) {
	t.Setenv(watchPortFileEnv, filepath.Join(t.TempDir(), "missing", "port"))
	_, err := writeWatchPortFile(16677)
	assert.ErrorContains(t, err, watchPortFileEnv)
}

func TestWatchWebBase(t *testing.T) {
	t.Setenv(watchWebURLEnv, "")
	assert.Equal(t, "http://127.0.0.1:16678", watchWebBase(16678))

	t.Setenv(watchWebURLEnv, "https://localhost:5174/")
	assert.Equal(t, "https://localhost:5174", watchWebBase(16678))
}
