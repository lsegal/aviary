package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInlineTextAttachments(t *testing.T) {
	notes := "data:text/plain;name=my%20notes.txt;base64," + base64.StdEncoding.EncodeToString([]byte("line one\nline two"))
	image := "data:image/png;base64,abc123"
	msg, media := InlineTextAttachments("read these", []string{image, notes, "https://example.com/a.png"})
	assert.Equal(t, "read these\n\n<attachment name=\"my notes.txt\">\nline one\nline two\n</attachment>", msg)
	assert.Equal(t, []string{image, "https://example.com/a.png"}, media)
}

func TestInlineTextAttachments_NoText(t *testing.T) {
	msg, media := InlineTextAttachments("", []string{"data:application/json;base64," + base64.StdEncoding.EncodeToString([]byte("{}\n"))})
	assert.Equal(t, "<attachment>\n{}\n</attachment>", msg)
	assert.Empty(t, media)
}

func TestParseTextDataURL_RejectsBinary(t *testing.T) {
	_, _, ok := ParseTextDataURL("data:application/pdf;base64,JVBERi0=")
	assert.False(t, ok)
	_, _, ok = ParseTextDataURL("data:image/png;base64,abc123")
	assert.False(t, ok)
}

func TestAnthropicProvider_Stream_SendsEveryImage(t *testing.T) {
	var body struct {
		Messages []struct {
			Content []struct {
				Type   string `json:"type"`
				Source struct {
					Data string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		anthropicSSEResponse(w, "two images")
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{
		Role:      RoleUser,
		Content:   "compare",
		MediaURLs: []string{"data:image/png;base64,Zmlyc3Q=", "data:image/jpeg;base64,c2Vjb25k"},
	}}})
	assert.NoError(t, err)
	collectEvents(t, ch)

	assert.Len(t, body.Messages, 1)
	var images []string
	for _, c := range body.Messages[0].Content {
		if c.Type == "image" {
			images = append(images, c.Source.Data)
		}
	}
	assert.Equal(t, []string{"Zmlyc3Q=", "c2Vjb25k"}, images)
}

func TestAnthropicProvider_Stream_SendsPDFAsDocument(t *testing.T) {
	var body struct {
		Messages []struct {
			Content []struct {
				Type   string `json:"type"`
				Source struct {
					MediaType string `json:"media_type"`
					Data      string `json:"data"`
				} `json:"source"`
			} `json:"content"`
		} `json:"messages"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &body)
		anthropicSSEResponse(w, "read it")
	}))
	defer srv.Close()

	p := newTestAnthropicProvider(srv.URL, "claude-3-5-sonnet-20241022")
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{
		Role:      RoleUser,
		Content:   "summarize",
		MediaURLs: []string{"data:application/pdf;base64,JVBERi0=", "data:image/png;base64,Zmlyc3Q="},
	}}})
	assert.NoError(t, err)
	collectEvents(t, ch)

	assert.Len(t, body.Messages, 1)
	var types []string
	for _, c := range body.Messages[0].Content {
		types = append(types, c.Type)
		if c.Type == "document" {
			assert.Equal(t, "application/pdf", c.Source.MediaType)
			assert.Equal(t, "JVBERi0=", c.Source.Data)
		}
	}
	assert.Equal(t, []string{"text", "document", "image"}, types)
}

func TestOpenAIProvider_Stream_SkipsNonImageMedia(t *testing.T) {
	var raw []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ = io.ReadAll(r.Body)
		openAISSEResponse(w, "ok")
	}))
	defer srv.Close()

	p := NewOpenAIProvider("test-key", "gpt-4o", srv.URL)
	ch, err := p.Stream(context.Background(), Request{Messages: []Message{{
		Role:      RoleUser,
		Content:   "look",
		MediaURLs: []string{"data:application/pdf;base64,JVBERi0=", "data:image/png;base64,Zmlyc3Q="},
	}}})
	assert.NoError(t, err)
	collectEvents(t, ch)
	assert.NotContains(t, string(raw), "application/pdf")
	assert.Contains(t, string(raw), "data:image/png;base64,Zmlyc3Q=")
}

func TestFileURLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "my log.json")
	u := FileURL(path)
	assert.True(t, strings.HasPrefix(u, "file:///"))
	got, ok := ParseFileURL(u)
	assert.True(t, ok)
	assert.Equal(t, path, got)
	_, ok = ParseFileURL("data:image/png;base64,abc")
	assert.False(t, ok)
}

func TestParseDocumentDataURL(t *testing.T) {
	mimeType, data, ok := ParseDocumentDataURL("data:application/pdf;base64,JVBERi0=")
	assert.True(t, ok)
	assert.Equal(t, "application/pdf", mimeType)
	assert.Equal(t, "JVBERi0=", data)
	_, _, ok = ParseDocumentDataURL("data:image/png;base64,abc")
	assert.False(t, ok)
}

func TestContentTypeFromName(t *testing.T) {
	assert.Equal(t, "application/json", ContentTypeFromName("chrome-net-export-log.json"))
	assert.Equal(t, "text/plain", ContentTypeFromName("server.log"))
	assert.Equal(t, "application/yaml", ContentTypeFromName("config.YML"))
	assert.Equal(t, "application/pdf", ContentTypeFromName("report.pdf"))
	assert.Equal(t, "", ContentTypeFromName("README"))
}
