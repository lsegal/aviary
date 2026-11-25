package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authpkg "github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/config"
)

// useConfigureCfgFile points cfgFile at a temp config containing contents
// (or at a missing file when contents is empty) and returns its path.
func useConfigureCfgFile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "aviary.yaml")
	if contents != "" {
		require.NoError(t, os.WriteFile(path, []byte(contents), 0o600))
	}
	prev := cfgFile
	cfgFile = path
	t.Cleanup(func() { cfgFile = prev })
	return path
}

// stubConfigureVar replaces a form entry point for the duration of the test.
func stubConfigureVar[T any](t *testing.T, target *T, stub T) {
	t.Helper()
	prev := *target
	*target = stub
	t.Cleanup(func() { *target = prev })
}

const configureTestYAML = "server:\n  port: 17171\n"
const configureInvalidYAML = ": invalid: yaml: ["

func TestConfigureCommandsRegistered(t *testing.T) {
	names := map[string]*cobra.Command{}
	for _, c := range configureCmd.Commands() {
		names[c.Name()] = c
	}
	for _, name := range []string{"general", "providers", "agents", "skills", "server", "browser", "scheduler"} {
		assert.Contains(t, names, name)
	}
	assert.Contains(t, names["providers"].Aliases, "auth")

	found, _, err := rootCmd.Find([]string{"configure"})
	require.NoError(t, err)
	assert.Same(t, configureCmd, found)
}

func TestRunConfigurePassesLoadedConfig(t *testing.T) {
	path := useConfigureCfgFile(t, configureTestYAML)
	var gotCfg *config.Config
	var gotPath string
	var gotStore authpkg.Store
	stubConfigureVar(t, &configureWizard, func(cfg *config.Config, cfgPath string, st authpkg.Store) error {
		gotCfg, gotPath, gotStore = cfg, cfgPath, st
		return nil
	})

	require.NoError(t, runConfigure(nil, nil))
	require.NotNil(t, gotCfg)
	assert.Equal(t, 17171, gotCfg.Server.Port)
	assert.Equal(t, path, gotPath)
	assert.IsType(t, &authpkg.FileStore{}, gotStore)
}

func TestRunConfigureFallsBackToDefaultOnLoadError(t *testing.T) {
	path := useConfigureCfgFile(t, configureInvalidYAML)
	var gotCfg *config.Config
	var gotPath string
	stubConfigureVar(t, &configureWizard, func(cfg *config.Config, cfgPath string, _ authpkg.Store) error {
		gotCfg, gotPath = cfg, cfgPath
		return nil
	})

	require.NoError(t, runConfigure(nil, nil))
	require.NotNil(t, gotCfg)
	assert.Equal(t, config.Default(), *gotCfg)
	assert.Equal(t, path, gotPath)
}

func TestRunConfigureReturnsWizardError(t *testing.T) {
	useConfigureCfgFile(t, configureTestYAML)
	want := errors.New("wizard failed")
	stubConfigureVar(t, &configureWizard, func(*config.Config, string, authpkg.Store) error { return want })

	assert.ErrorIs(t, runConfigure(nil, nil), want)
}

func TestRunConfigureProviders(t *testing.T) {
	var gotStore authpkg.Store
	stubConfigureVar(t, &configureProviderMgr, func(st authpkg.Store) error {
		gotStore = st
		return nil
	})
	require.NoError(t, runConfigureProviders(nil, nil))
	assert.IsType(t, &authpkg.FileStore{}, gotStore)

	want := errors.New("provider manager failed")
	stubConfigureVar(t, &configureProviderMgr, func(authpkg.Store) error { return want })
	assert.ErrorIs(t, runConfigureProviders(nil, nil), want)
}

func TestRunConfigureFormHandlers(t *testing.T) {
	type form = func(*config.Config, string) error
	tests := []struct {
		name   string
		target *form
		run    func(*cobra.Command, []string) error
	}{
		{"general", &configureGeneralForm, runConfigureGeneral},
		{"agents", &configureAgentMgr, runConfigureAgents},
		{"skills", &configureSkillMgr, runConfigureSkills},
		{"server", &configureServerForm, runConfigureServer},
		{"browser", &configureBrowserForm, runConfigureBrowser},
		{"scheduler", &configureSchedulerForm, runConfigureScheduler},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Run("passes loaded config and path", func(t *testing.T) {
				path := useConfigureCfgFile(t, configureTestYAML)
				var gotCfg *config.Config
				var gotPath string
				stubConfigureVar(t, tt.target, func(cfg *config.Config, cfgPath string) error {
					gotCfg, gotPath = cfg, cfgPath
					return nil
				})

				require.NoError(t, tt.run(nil, nil))
				require.NotNil(t, gotCfg)
				assert.Equal(t, 17171, gotCfg.Server.Port)
				assert.Equal(t, path, gotPath)
			})

			t.Run("returns load error without opening form", func(t *testing.T) {
				useConfigureCfgFile(t, configureInvalidYAML)
				called := false
				stubConfigureVar(t, tt.target, func(*config.Config, string) error {
					called = true
					return nil
				})

				require.Error(t, tt.run(nil, nil))
				assert.False(t, called)
			})

			t.Run("returns form error", func(t *testing.T) {
				useConfigureCfgFile(t, configureTestYAML)
				want := errors.New(tt.name + " form failed")
				stubConfigureVar(t, tt.target, func(*config.Config, string) error { return want })

				assert.ErrorIs(t, tt.run(nil, nil), want)
			})
		})
	}
}
