package channels

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newLinkPreviewServer serves body with contentType for every path except
// /img.png, which serves a fake PNG. Occurrences of {img} in body are replaced
// with the absolute URL of that image.
func newLinkPreviewServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/img.png" {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("\x89PNG fake image data"))
			return
		}
		w.Header().Set("Content-Type", contentType)
		_, _ = fmt.Fprint(w, strings.ReplaceAll(body, "{img}", srv.URL+"/img.png"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchLinkPreviews(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		body        string
		input       string // {url} is replaced with the server URL
		wantNil     bool
		wantTitle   string
		wantDesc    string
		wantImage   bool
	}{
		{
			name:    "no URL in text",
			input:   "no url here, just text",
			wantNil: true,
		},
		{
			name:        "non-HTML response",
			contentType: "application/json",
			body:        `{"key":"value"}`,
			input:       "{url}",
			wantNil:     true,
		},
		{
			name:        "HTML without title",
			contentType: "text/html",
			body:        `<html><head></head><body>No title</body></html>`,
			input:       "{url}",
			wantNil:     true,
		},
		{
			name:        "title from title tag",
			contentType: "text/html",
			body:        `<html><head><title>Page Title</title></head><body></body></html>`,
			input:       "See {url}",
			wantTitle:   "Page Title",
		},
		{
			name:        "og title and description",
			contentType: "text/html",
			body: `<html><head>
				<meta property="og:title" content="My Page">
				<meta property="og:description" content="My page description">
			</head><body></body></html>`,
			input:     "Check out {url} for more info",
			wantTitle: "My Page",
			wantDesc:  "My page description",
		},
		{
			name:        "twitter title, description and image",
			contentType: "text/html",
			body: `<html><head>
				<meta name="twitter:title" content="Twitter Article">
				<meta name="twitter:description" content="A desc">
				<meta name="twitter:image" content="{img}">
			</head><body></body></html>`,
			input:     "{url}",
			wantTitle: "Twitter Article",
			wantDesc:  "A desc",
			wantImage: true,
		},
		{
			name:        "image from og:image",
			contentType: "text/html",
			body:        `<!doctype html><html><head><title>My Title</title><meta property="og:image" content="{img}"></head><body></body></html>`,
			input:       "check {url}/",
			wantTitle:   "My Title",
			wantImage:   true,
		},
		{
			name:        "image from link icon",
			contentType: "text/html",
			body: `<html><head>
				<meta property="og:title" content="Site">
				<link rel="icon" href="{img}">
			</head><body></body></html>`,
			input:     "{url}",
			wantTitle: "Site",
			wantImage: true,
		},
		{
			name:        "image from img tag",
			contentType: "text/html",
			body: `<html><head>
				<meta property="og:title" content="Post">
			</head><body><img src="{img}"></body></html>`,
			input:     "check {url}",
			wantTitle: "Post",
			wantImage: true,
		},
		{
			name:        "URL with trailing punctuation",
			contentType: "text/html",
			body:        `<html><head><meta property="og:title" content="Trimmed"></head></html>`,
			input:       "Visit {url}.",
			wantTitle:   "Trimmed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := tt.input
			var srvURL string
			if strings.Contains(input, "{url}") {
				srvURL = newLinkPreviewServer(t, tt.contentType, tt.body).URL
				input = strings.ReplaceAll(input, "{url}", srvURL)
			}

			previews, cleanup := fetchLinkPreviews(input)
			if tt.wantNil {
				assert.Nil(t, previews)
				assert.Nil(t, cleanup)
				return
			}
			require.Len(t, previews, 1)
			p := previews[0]
			assert.True(t, strings.HasPrefix(p.URL, srvURL), "URL %q not trimmed to server URL %q", p.URL, srvURL)
			assert.False(t, strings.HasSuffix(p.URL, "."), "trailing punctuation not trimmed from %q", p.URL)
			assert.Equal(t, tt.wantTitle, p.Title)
			assert.Equal(t, tt.wantDesc, p.Description)

			if !tt.wantImage {
				assert.Empty(t, p.Image)
				assert.Nil(t, cleanup)
				return
			}
			require.NotEmpty(t, p.Image)
			require.NotNil(t, cleanup)
			data, err := os.ReadFile(p.Image)
			require.NoError(t, err)
			assert.Equal(t, "\x89PNG fake image data", string(data))
			cleanup()
			_, err = os.Stat(p.Image)
			assert.True(t, os.IsNotExist(err), "cleanup did not remove %s", p.Image)
		})
	}
}

// TestFetchLinkPreviews_FetchError verifies graceful handling of a URL that can't be fetched.
func TestFetchLinkPreviews_FetchError(t *testing.T) {
	// Use a port that nothing is listening on.
	previews, cleanup := fetchLinkPreviews("http://127.0.0.1:1/page")
	assert.Nil(t, previews)
	assert.Nil(t, cleanup)
}

// TestFetchLinkPreviews_LargeHead ensures metadata beyond the initial 64 KiB
// of HTML is still parsed. Some modern sites inject large inline scripts in
// <head> before the title and OG tags.
func TestFetchLinkPreviews_LargeHead(t *testing.T) {
	padding := strings.Repeat("a", 70*1024)
	srv := newLinkPreviewServer(t, "text/html", `<html><head><script>`+padding+`</script><title>Large Head Title</title><meta property="og:description" content="Large head description"></head></html>`)

	previews, cleanup := fetchLinkPreviews(srv.URL)
	assert.Nil(t, cleanup)
	require.Len(t, previews, 1)
	assert.Equal(t, "Large Head Title", previews[0].Title)
	assert.Equal(t, "Large head description", previews[0].Description)
}
