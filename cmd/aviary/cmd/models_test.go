package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// useModelsConfig points cfgFile and the auth store at a temp dir holding the
// given YAML config, so no real user config or credentials are read.
func useModelsConfig(t *testing.T, yaml string) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	path := filepath.Join(base, "aviary.yaml")
	require.NoError(t, os.WriteFile(path, []byte(yaml), 0o600))
	cfgFile = path
	t.Cleanup(func() { cfgFile = "" })
}

// newModelListServer serves an OpenAI-compatible /v1/models list and counts
// the requests it receives.
func newModelListServer(t *testing.T, ids ...string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		data := make([]map[string]string, 0, len(ids))
		for _, id := range ids {
			data = append(data, map[string]string{"id": id})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

func TestRunModelsList_All(t *testing.T) {
	useModelsConfig(t, "")
	var out bytes.Buffer
	require.NoError(t, runModelsList(&out, ""))
	text := out.String()
	assert.Contains(t, text, "MODEL")
	assert.Contains(t, text, "INPUT")
	assert.Contains(t, text, "OUTPUT")
	assert.Contains(t, text, "TYPE")
	assert.Contains(t, text, "anthropic/claude-sonnet-4-5")
	assert.Contains(t, text, "openai-codex/gpt-6-astra")
	assert.Contains(t, text, "text only")
}

func TestRunModelsList_FilterProvider(t *testing.T) {
	var out bytes.Buffer
	require.NoError(t, runModelsList(&out, "openai"))
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	require.Greater(t, len(lines), 1)
	for _, line := range lines[1:] {
		assert.True(t, strings.HasPrefix(strings.TrimSpace(line), "openai/"))
	}
}

func TestRunModelsList_UnknownProvider(t *testing.T) {
	var out bytes.Buffer
	err := runModelsList(&out, "bogus")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown provider "bogus"`)
}

func TestRunModelsList_DynamicProviderRows(t *testing.T) {
	for _, provider := range []string{"vllm", "ollama"} {
		t.Run(provider, func(t *testing.T) {
			srv, hits := newModelListServer(t, "alpha", " ", "beta")
			useModelsConfig(t, "models:\n  providers:\n    "+provider+":\n      base_uri: "+srv.URL+"\n")

			var out bytes.Buffer
			require.NoError(t, runModelsList(&out, provider))
			lines := strings.Split(strings.TrimSpace(out.String()), "\n")
			require.Len(t, lines, 3)
			assert.Equal(t, []string{"MODEL", "INPUT", "OUTPUT", "TYPE"}, strings.Fields(lines[0]))
			assert.Equal(t, []string{provider + "/alpha", "-", "-"}, strings.Fields(lines[1]))
			assert.Equal(t, []string{provider + "/beta", "-", "-"}, strings.Fields(lines[2]))
			assert.Equal(t, int32(1), hits.Load())
		})
	}
}

func TestRunModelsList_AllIncludesConfiguredDynamicProviders(t *testing.T) {
	vllm, _ := newModelListServer(t, "served-vllm")
	ollama, _ := newModelListServer(t, "served-ollama")
	useModelsConfig(t, "models:\n  providers:\n    vllm:\n      base_uri: "+vllm.URL+"\n    ollama:\n      base_uri: "+ollama.URL+"\n")

	var out bytes.Buffer
	require.NoError(t, runModelsList(&out, ""))
	text := out.String()
	assert.Contains(t, text, "anthropic/claude-sonnet-4-5")
	assert.Contains(t, text, "vllm/served-vllm")
	assert.Contains(t, text, "ollama/served-ollama")
	assert.Less(t, strings.Index(text, "vllm/served-vllm"), strings.Index(text, "ollama/served-ollama"))
}

func TestRunModelsList_DynamicProviderError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	useModelsConfig(t, "models:\n  providers:\n    ollama:\n      base_uri: "+srv.URL+"\n")

	var out bytes.Buffer
	err := runModelsList(&out, "ollama")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ollama models")
	assert.Contains(t, err.Error(), "boom")
	assert.Empty(t, out.String(), "nothing is flushed when listing fails")
}

func TestRunModelsList_ConfigLoadError(t *testing.T) {
	useModelsConfig(t, "models: [not, a, mapping\n")

	var out bytes.Buffer
	err := runModelsList(&out, "vllm")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parsing config")
}

func TestListDynamicModels_StaticProviderIsSkipped(t *testing.T) {
	useModelsConfig(t, "models: [not, a, mapping\n")

	names, err := listDynamicModels("anthropic", true)
	require.NoError(t, err)
	assert.Nil(t, names)
}

func TestListDynamicModels_UnconfiguredProviderWithoutDefault(t *testing.T) {
	useModelsConfig(t, "")
	dynamicModelsContext = func() (context.Context, context.CancelFunc) {
		t.Fatal("an unconfigured provider must not be queried")
		return nil, nil
	}
	t.Cleanup(func() { dynamicModelsContext = defaultDynamicModelsContext })

	for _, provider := range []string{"vllm", "ollama"} {
		names, err := listDynamicModels(provider, false)
		require.NoError(t, err)
		assert.Nil(t, names)
	}
}

func TestListDynamicModels_UnconfiguredProviderWithDefault(t *testing.T) {
	useModelsConfig(t, "")
	// A cancelled context fails the request before it dials, so the default
	// local endpoint is targeted without touching the network.
	dynamicModelsContext = func() (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		return ctx, cancel
	}
	t.Cleanup(func() { dynamicModelsContext = defaultDynamicModelsContext })

	_, err := listDynamicModels("vllm", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "http://127.0.0.1:8000/v1/models")
	assert.ErrorIs(t, err, context.Canceled)

	_, err = listDynamicModels("ollama", true)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ollama models")
	assert.ErrorIs(t, err, context.Canceled)
}

func TestFormatTokenCount(t *testing.T) {
	cases := map[int]string{
		0:         "0",
		999:       "999",
		1_000:     "1k",
		8_192:     "8.2k",
		200_000:   "200k",
		999_999:   "1000.0k",
		1_000_000: "1.00M",
		1_048_576: "1.05M",
		2_500_000: "2.50M",
	}
	for n, want := range cases {
		assert.Equal(t, want, formatTokenCount(n), "formatTokenCount(%d)", n)
	}
}

func TestModelsCommands_UseProviderFlag(t *testing.T) {
	srv, _ := newModelListServer(t, "from-flag")
	useModelsConfig(t, "models:\n  providers:\n    vllm:\n      base_uri: "+srv.URL+"\n")
	prev := modelsProvider
	modelsProvider = "vllm"
	t.Cleanup(func() { modelsProvider = prev })

	for _, cmd := range []*cobra.Command{modelsCmd, modelsListCmd} {
		var out bytes.Buffer
		cmd.SetOut(&out)
		require.NoError(t, cmd.RunE(cmd, nil), cmd.Name())
		cmd.SetOut(nil)
		assert.Contains(t, out.String(), "vllm/from-flag", cmd.Name())
		assert.NotContains(t, out.String(), "anthropic/", cmd.Name())
	}
}
