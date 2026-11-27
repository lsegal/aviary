package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newLocalTestProvider(t *testing.T, apiKey string, handler http.HandlerFunc) *openAICompatLocalProvider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	p := newOpenAICompatLocalProvider("testlocal", apiKey, "model", normalizeLocalOpenAIBaseURI(srv.URL, ""))
	return &p
}

func TestOpenAICompatLocalProvider_PingHealthy(t *testing.T) {
	var hits int
	p := newLocalTestProvider(t, "secret", func(w http.ResponseWriter, r *http.Request) {
		hits++
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "Bearer secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"a"}]}`))
	})

	require.NoError(t, p.Ping(context.Background()))
	assert.Equal(t, 1, hits)
}

func TestOpenAICompatLocalProvider_PingNonOK(t *testing.T) {
	p := newLocalTestProvider(t, "", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "  invalid api key  ", http.StatusUnauthorized)
	})

	err := p.Ping(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "testlocal models: GET ")
	assert.Contains(t, err.Error(), "/v1/models")
	assert.Contains(t, err.Error(), "401 Unauthorized: invalid api key")
}

func TestOpenAICompatLocalProvider_PingUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	base := normalizeLocalOpenAIBaseURI(srv.URL, "")
	srv.Close()

	p := newOpenAICompatLocalProvider("testlocal", "", "model", base)
	err := p.Ping(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "testlocal models: ")
	assert.NotContains(t, err.Error(), "GET ")
}

func TestOpenAICompatLocalProvider_ListModelsOmitsAuthWithoutKey(t *testing.T) {
	p := newLocalTestProvider(t, "   ", func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Authorization"))
		_, _ = w.Write([]byte(`{"data":[{"id":" llama "},{"id":""},{"id":"  "},{"id":"qwen"}]}`))
	})

	models, err := p.ListModels(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"llama", "qwen"}, models)
}

func TestOpenAICompatLocalProvider_ListModelsMalformedJSON(t *testing.T) {
	p := newLocalTestProvider(t, "", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":[`))
	})

	models, err := p.ListModels(context.Background())
	require.Error(t, err)
	assert.Nil(t, models)
	assert.Contains(t, err.Error(), "testlocal models: decoding response:")
}

func TestOpenAICompatLocalProvider_ListModelsBadBaseURL(t *testing.T) {
	p := newOpenAICompatLocalProvider("testlocal", "", "model", "http://bad\x7fhost/v1")

	models, err := p.ListModels(context.Background())
	require.Error(t, err)
	assert.Nil(t, models)
	assert.Contains(t, err.Error(), "testlocal models: building request:")
}

func TestNormalizeLocalOpenAIBaseURI_Unparseable(t *testing.T) {
	assert.Equal(t, "http://[::1", normalizeLocalOpenAIBaseURI(" http://[::1/ ", "http://default"))
}
