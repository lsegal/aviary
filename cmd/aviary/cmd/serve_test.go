package cmd

import (
	"bytes"
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/server"
)

func TestServeRuntimeErrorPrintsNoUsage(t *testing.T) {
	prev := startServer
	t.Cleanup(func() {
		startServer = prev
		serveCmd.SilenceUsage = false
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})
	boom := errors.New("listener failed")
	startServer = func(_ *cobra.Command, _ []string) error { return boom }

	var out bytes.Buffer
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&out)
	rootCmd.SetArgs([]string{"serve"})
	err := rootCmd.Execute()

	require.ErrorIs(t, err, boom)
	assert.NotContains(t, out.String(), "Usage:")
}

func TestShutdownErrDropsRestartSentinels(t *testing.T) {
	assert.NoError(t, shutdownErr(server.ErrReexecRequired))
	assert.NoError(t, shutdownErr(fmt.Errorf("wrapped: %w", server.ErrReexecRequired)))
	assert.NoError(t, shutdownErr(server.ErrRestartRequired))
	assert.NoError(t, shutdownErr(nil))
	boom := errors.New("boom")
	assert.ErrorIs(t, shutdownErr(boom), boom)
}
