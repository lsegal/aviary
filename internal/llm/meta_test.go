package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactoryForModel_Meta(t *testing.T) {
	var ref string
	f := NewFactory(func(r string) (string, error) {
		ref = r
		return "LLM_test-key", nil
	})

	p, err := f.ForModel("meta/muse-spark-1.3")
	require.NoError(t, err)
	_, ok := p.(*MetaProvider)
	assert.True(t, ok)
	assert.Equal(t, "auth:meta:default", ref)
}

func TestFactoryForModel_MetaNeedsAKey(t *testing.T) {
	f := NewFactory(func(string) (string, error) { return "", nil })

	_, err := f.ForModel("meta/muse-spark-1.3")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "meta:default")
}

func TestMetaProviderDefaultsToTheModelAPI(t *testing.T) {
	assert.Equal(t, "https://api.meta.ai/v1", NewMetaProvider("k", "muse-spark-1.3", "").inner.baseURL)
	assert.Equal(t, "https://proxy.example/v1", NewMetaProvider("k", "muse-spark-1.3", "https://proxy.example/v1/").inner.baseURL)
}

// TestMetaProviderStreamsChatCompletions checks a Muse model is sent the way
// Meta's Model API takes it: an OpenAI Chat Completions request carrying the
// bare model id and the key as a Bearer token.
func TestMetaProviderStreamsChatCompletions(t *testing.T) {
	var gotPath, gotAuth, gotModel string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Model string `json:"model"`
		}
		_ = json.Unmarshal(body, &req)
		gotModel = req.Model
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"id\":\"c1\",\"object\":\"chat.completion.chunk\",\"model\":\"muse-spark-1.3\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi there\"}}]}\n\n")
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	p := NewMetaProvider("LLM_test-key", "muse-spark-1.3", srv.URL+"/v1")
	ch, err := p.Stream(context.Background(), Request{
		Model:    "muse-spark-1.3",
		Messages: []Message{{Role: RoleUser, Content: "hello"}},
		Stream:   true,
	})
	require.NoError(t, err)
	var text strings.Builder
	for ev := range ch {
		require.NotEqual(t, EventTypeError, ev.Type, "%v", ev.Error)
		text.WriteString(ev.Text)
	}

	assert.Equal(t, "/v1/chat/completions", gotPath)
	assert.Equal(t, "Bearer LLM_test-key", gotAuth)
	assert.Equal(t, "muse-spark-1.3", gotModel)
	assert.Equal(t, "hi there", text.String())
}
