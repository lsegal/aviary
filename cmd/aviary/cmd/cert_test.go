package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/store"
)

// runCertCmd runs `aviary cert` with args against a fresh data directory and
// returns its output. cfgFile must already point at the test's config.
func runCertCmd(t *testing.T, dataDir string, args ...string) (string, error) {
	t.Helper()
	store.SetDataDir(dataDir)
	t.Cleanup(func() {
		store.SetDataDir(os.Getenv("AVIARY_CONFIG_BASE_DIR"))
		certRegenerateFlag, certJSONFlag = false, false
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	certRegenerateFlag, certJSONFlag = false, false

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs(append([]string{"cert"}, args...))
	err := rootCmd.Execute()
	return out.String(), err
}

func TestCertCommandPrintsSelfSignedPaths(t *testing.T) {
	useConfigureCfgFile(t, configureTestYAML)
	dataDir := t.TempDir()

	out, err := runCertCmd(t, dataDir)
	require.NoError(t, err)

	certFile := filepath.Join(dataDir, store.DirCerts, "cert.pem")
	keyFile := filepath.Join(dataDir, store.DirCerts, "key.pem")
	assert.Equal(t, "cert: "+certFile+"\nkey:  "+keyFile+"\n", out)
	assert.FileExists(t, certFile)
	assert.FileExists(t, keyFile)
}

func TestCertCommandJSON(t *testing.T) {
	useConfigureCfgFile(t, configureTestYAML)
	dataDir := t.TempDir()

	out, err := runCertCmd(t, dataDir, "--json")
	require.NoError(t, err)

	var got map[string]string
	require.NoError(t, json.Unmarshal([]byte(out), &got))
	assert.Equal(t, filepath.Join(dataDir, store.DirCerts, "cert.pem"), got["cert"])
	assert.Equal(t, filepath.Join(dataDir, store.DirCerts, "key.pem"), got["key"])
	assert.Equal(t, "https://localhost:17171", got["server_url"])
	assert.FileExists(t, got["cert"])
}

func TestCertCommandRegenerateReplacesCert(t *testing.T) {
	useConfigureCfgFile(t, configureTestYAML)
	dataDir := t.TempDir()
	certFile := filepath.Join(dataDir, store.DirCerts, "cert.pem")

	_, err := runCertCmd(t, dataDir)
	require.NoError(t, err)
	before, err := os.ReadFile(certFile)
	require.NoError(t, err)

	_, err = runCertCmd(t, dataDir)
	require.NoError(t, err)
	unchanged, err := os.ReadFile(certFile)
	require.NoError(t, err)
	assert.Equal(t, before, unchanged, "cert changed without --regenerate")

	_, err = runCertCmd(t, dataDir, "--regenerate")
	require.NoError(t, err)
	after, err := os.ReadFile(certFile)
	require.NoError(t, err)
	assert.NotEqual(t, before, after)
}

func TestCertCommandConfigLoadError(t *testing.T) {
	useConfigureCfgFile(t, configureInvalidYAML)
	dataDir := t.TempDir()

	_, err := runCertCmd(t, dataDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading config")
	assert.NoDirExists(t, filepath.Join(dataDir, store.DirCerts))
}

func TestCertCommandMissingConfiguredTLSFiles(t *testing.T) {
	dir := t.TempDir()
	certFile := filepath.Join(dir, "missing-cert.pem")
	keyFile := filepath.Join(dir, "missing-key.pem")
	cfg := map[string]any{"server": map[string]any{"tls": map[string]string{"cert": certFile, "key": keyFile}}}
	// JSON is valid YAML and quotes Windows paths safely.
	contents, err := json.Marshal(cfg)
	require.NoError(t, err)
	useConfigureCfgFile(t, string(contents))
	dataDir := t.TempDir()

	out, err := runCertCmd(t, dataDir, "--json")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "loading TLS cert")
	assert.NotContains(t, out, `"cert"`)
	assert.NoDirExists(t, filepath.Join(dataDir, store.DirCerts))
}
