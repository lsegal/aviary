package llm

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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
