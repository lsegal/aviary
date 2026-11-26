package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"

	"github.com/lsegal/aviary/internal/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigGetKey_Simple(t *testing.T) {
	cfg := config.Default()
	cfg.Browser.ProfileDir = "/my/profile"
	cfg.Browser.Binary = "/usr/bin/chromium"
	cfg.Browser.CDPPort = 9222
	reuseTabs := false
	cfg.Browser.ReuseTabs = &reuseTabs
	cfg.Scheduler.Concurrency = "auto"

	tests := []struct {
		key  string
		want string
	}{
		{"browser.profile_directory", "/my/profile"},
		{"browser.binary", "/usr/bin/chromium"},
		{"browser.cdp_port", "9222"},
		{"browser.reuse_tabs", "false"},
		{"server.port", "16677"},
		{"scheduler.concurrency", "auto"},
	}

	for _, tt := range tests {
		got, err := configGetKey(&cfg, tt.key)
		assert.NoError(t, err)
		if err != nil {
			continue
		}
		assert.Equal(t, tt.want, got)

	}
}

func TestConfigGetKey_NotFound(t *testing.T) {
	cfg := config.Default()
	_, err := configGetKey(&cfg, "browser.nonexistent")
	assert.Error(t, err)

}

func TestConfigGetKey_TraverseError(t *testing.T) {
	cfg := config.Default()
	// browser.cdp_port is a scalar (int), not a map — can't traverse into it.
	_, err := configGetKey(&cfg, "browser.cdp_port.foo")
	assert.Error(t, err)

}

func TestConfigSetKey_String(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "browser.profile_directory", "/my/profile")
	assert.NoError(t, err)

	assert.Equal(t, "/my/profile", cfg.Browser.ProfileDir)

}

func TestConfigSetKey_Int(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "server.port", "9999")
	assert.NoError(t, err)

	assert.Equal(t, 9999, cfg.Server.Port)

}

func TestConfigSetKey_BrowserBinary(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "browser.binary", "/usr/bin/chromium")
	assert.NoError(t, err)

	assert.Equal(t, "/usr/bin/chromium", cfg.Browser.Binary)

}

func TestConfigSetKey_CDPPort(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "browser.cdp_port", "9333")
	assert.NoError(t, err)

	assert.Equal(t, 9333, cfg.Browser.CDPPort)

}

func TestConfigSetKey_BrowserReuseTabs(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "browser.reuse_tabs", "false")
	assert.NoError(t, err)

	if assert.NotNil(t, cfg.Browser.ReuseTabs) {
		assert.False(t, *cfg.Browser.ReuseTabs)
	}
}

func TestConfigSetKey_ServerExternalAccess(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "server.external_access", "true")
	assert.NoError(t, err)

	assert.True(t, cfg.Server.ExternalAccess)
}

func TestConfigSetKey_NestedModel(t *testing.T) {
	cfg := config.Default()
	err := configSetKey(&cfg, "models.defaults.model", "anthropic/claude-sonnet-4-5")
	assert.NoError(t, err)

	assert.Equal(t, "anthropic/claude-sonnet-4-5", cfg.Models.Defaults.Model)

}

func TestConfigRoundtrip(t *testing.T) {
	cfg := config.Default()

	// Set several values, then get them back.
	sets := []struct{ key, val string }{
		{"browser.profile_directory", "/aviary/profile"},
		{"browser.cdp_port", "9444"},
		{"browser.reuse_tabs", "false"},
		{"server.external_access", "true"},
		{"server.port", "8080"},
	}
	for _, s := range sets {
		err := configSetKey(&cfg, s.key, s.val)
		assert.NoError(t, err)

	}
	for _, s := range sets {
		got, err := configGetKey(&cfg, s.key)
		assert.NoError(t, err)
		assert.Equal(t, s.val, got)

	}
}

// runConfigCommand drives the config command's RunE against cfgFile and
// returns what it printed.
func runConfigCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()
	return captureStdout(t, func() error { return runConfigGetSet(configCmd, args) })
}

func TestRunConfigGetSet_GetPrintsValue(t *testing.T) {
	useConfigureCfgFile(t, "server:\n  port: 8123\nbrowser:\n  binary: /opt/chromium\n")

	out, err := runConfigCommand(t, "server.port")
	require.NoError(t, err)
	assert.Equal(t, "8123\n", out)

	out, err = runConfigCommand(t, "browser.binary")
	require.NoError(t, err)
	assert.Equal(t, "/opt/chromium\n", out)
}

func TestRunConfigGetSet_SetSavesValue(t *testing.T) {
	path := useConfigureCfgFile(t, "server:\n  port: 8123\n")

	out, err := runConfigCommand(t, "browser.cdp_port", "9333")
	require.NoError(t, err)
	assert.Empty(t, out)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 9333, cfg.Browser.CDPPort)
	assert.Equal(t, 8123, cfg.Server.Port, "existing values are preserved")
}

func TestRunConfigGetSet_SetCreatesMissingFile(t *testing.T) {
	path := useConfigureCfgFile(t, "")

	_, err := runConfigCommand(t, "server.port", "9001")
	require.NoError(t, err)

	require.FileExists(t, path)
	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Equal(t, 9001, cfg.Server.Port)

	out, err := runConfigCommand(t, "server.port")
	require.NoError(t, err)
	assert.Equal(t, "9001\n", out)
}

func TestRunConfigGetSet_InvalidConfigFile(t *testing.T) {
	const contents = "server: [unterminated\n"
	path := useConfigureCfgFile(t, contents)

	out, err := runConfigCommand(t, "server.port")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing config")
	assert.Empty(t, out)

	_, err = runConfigCommand(t, "server.port", "9000")
	require.Error(t, err)
	assertFileContents(t, path, contents)
}

func TestRunConfigGetSet_GetErrors(t *testing.T) {
	useConfigureCfgFile(t, "server:\n  port: 8123\n")

	out, err := runConfigCommand(t, "server.nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `config key "server.nonexistent": not found`)
	assert.Empty(t, out)

	out, err = runConfigCommand(t, "server.port.deeper")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `cannot traverse into int at "port"`)
	assert.Empty(t, out)
}

func TestRunConfigGetSet_SetErrorsLeaveFileUnchanged(t *testing.T) {
	const contents = "server:\n  port: 8123\n"
	tests := []struct {
		name    string
		key     string
		value   string
		wantErr string
	}{
		{"value does not decode into field", "server.port", "not-a-number", "cannot unmarshal"},
		{"traverses through a scalar", "server.port.deeper", "1", "cannot unmarshal"},
		{"value is invalid YAML", "server.port", "[unterminated", "invalid config value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := useConfigureCfgFile(t, contents)

			_, err := runConfigCommand(t, tt.key, tt.value)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
			assertFileContents(t, path, contents)
		})
	}
}

func TestConfigNullValues(t *testing.T) {
	got, err := getInMap(map[string]any{"a": map[string]any{"b": nil}}, []string{"a", "b"})
	require.NoError(t, err)
	assert.Nil(t, got)

	cfg := config.Default()
	require.NoError(t, configSetKey(&cfg, "browser.binary", "~"))
	assert.Empty(t, cfg.Browser.Binary, "YAML null clears the value")
}

func assertFileContents(t *testing.T, path, want string) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, want, string(data))
}

// startConfigTaskServer serves a fake running Aviary MCP endpoint whose
// agent_list reports agents and whose config task tools echo their arguments
// (or fail for the task named "broken"), and points the dispatcher at it.
func startConfigTaskServer(t *testing.T, agents ...string) {
	t.Helper()
	s := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fake-aviary"}, nil)
	obj := map[string]any{"type": "object"}
	s.AddTool(&sdkmcp.Tool{Name: "agent_list", InputSchema: obj}, func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		list := make([]map[string]string, 0, len(agents))
		for _, a := range agents {
			list = append(list, map[string]string{"name": a})
		}
		data, err := json.Marshal(list)
		if err != nil {
			return nil, err
		}
		return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: string(data)}}}, nil
	})
	for _, name := range []string{"config_task_convert_to_script", "config_task_move_to_file"} {
		s.AddTool(&sdkmcp.Tool{Name: name, InputSchema: obj}, func(_ context.Context, req *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			var args map[string]string
			if err := json.Unmarshal(req.Params.Arguments, &args); err != nil {
				return nil, err
			}
			if args["task"] == "broken" {
				return nil, errors.New("task exploded")
			}
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: req.Params.Name + " " + args["agent"] + "/" + args["task"]}}}, nil
		})
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server { return s }, &sdkmcp.StreamableHTTPOptions{JSONResponse: true}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	useRemoteDispatcher(t, srv.URL)
}

func TestConfigTaskSubcommands(t *testing.T) {
	for _, tt := range []struct {
		cmd  *cobra.Command
		tool string
	}{
		{configConvertTaskToScriptCmd, "config_task_convert_to_script"},
		{configMoveTaskToFileCmd, "config_task_move_to_file"},
	} {
		t.Run(tt.tool, func(t *testing.T) {
			run := func(args ...string) (string, error) {
				tt.cmd.SetContext(t.Context())
				return captureStdout(t, func() error { return tt.cmd.RunE(tt.cmd, args) })
			}

			startConfigTaskServer(t, "bot")
			out, err := run("helper", "daily")
			require.NoError(t, err)
			assert.Equal(t, tt.tool+" helper/daily\n", out, "an explicit agent is passed through")

			out, err = run("daily")
			require.NoError(t, err)
			assert.Equal(t, tt.tool+" bot/daily\n", out, "the sole agent is used when omitted")

			out, err = run("bot", "broken")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "task exploded")
			assert.Empty(t, out)

			startConfigTaskServer(t, "bot", "other")
			out, err = run("daily")
			require.Error(t, err)
			assert.Contains(t, err.Error(), "an agent is required")
			assert.Empty(t, out)
		})
	}
}
