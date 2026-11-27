package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/domain"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

func writeIncomingFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	dir := store.IncomingMediaDir("signal")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, "1700000000000_"+name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestResolveFileAttachments_InlinesSmallJSON(t *testing.T) {
	setTestDataDir(t)
	path := writeIncomingFile(t, "log.json", []byte(`{"ok":true}`))

	msg, media := resolveFileAttachments("read this", []string{llm.FileURL(path)}, false)
	assert.Empty(t, media)
	assert.Equal(t, "read this\n\n<attachment name=\"log.json\" path=\""+path+"\">\n{\"ok\":true}\n</attachment>", msg)
}

func TestResolveFileAttachments_TruncatesLargeText(t *testing.T) {
	setTestDataDir(t)
	path := writeIncomingFile(t, "big.log", bytes.Repeat([]byte("x"), maxInlineAttachmentTextBytes+100))

	msg, media := resolveFileAttachments("", []string{llm.FileURL(path)}, false)
	assert.Empty(t, media)
	assert.Contains(t, msg, "[truncated after 32 KB; read the full file at "+path+"]")
	assert.Less(t, len(msg), maxInlineAttachmentTextBytes+1024)
}

func TestResolveFileAttachments_PDFDocumentOrPath(t *testing.T) {
	setTestDataDir(t)
	path := writeIncomingFile(t, "report.pdf", []byte("%PDF-1.4"))

	msg, media := resolveFileAttachments("summarize", []string{llm.FileURL(path)}, false)
	assert.Equal(t, "summarize", msg)
	assert.Equal(t, []string{"data:application/pdf;name=report.pdf;base64,JVBERi0xLjQ="}, media)

	// CLI agents take only images; the message's placeholder gives the path.
	msg, media = resolveFileAttachments("summarize", []string{llm.FileURL(path)}, true)
	assert.Equal(t, "summarize", msg)
	assert.Empty(t, media)
}

func TestResolveFileAttachments_DropsBinaryAndOutsideFiles(t *testing.T) {
	setTestDataDir(t)
	binary := writeIncomingFile(t, "blob.bin", append([]byte{0, 1, 2, 0xff}, bytes.Repeat([]byte{0}, 1<<20)...))
	outside := filepath.Join(t.TempDir(), "secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0o600))
	image := "data:image/png;base64,cG5n"

	msg, media := resolveFileAttachments("hi", []string{llm.FileURL(binary), llm.FileURL(outside), image}, false)
	assert.Equal(t, "hi", msg)
	assert.Equal(t, []string{image}, media)
}

func TestAnnotateHistoricalMedia_NamesFiles(t *testing.T) {
	image := "data:image/png;base64,cG5n"
	file := llm.FileURL("/data/media/incoming/signal/1700000000000_log.json")

	assert.Equal(t, "see\n[prior image attached]\n[prior file attached: log.json]",
		annotateHistoricalMedia("see", []string{image, file}, true))
	// Channel messages already carry a placeholder for the file.
	content := "see\n[file attached: log.json (application/json, 11 B) saved at /x]"
	assert.Equal(t, content, annotateHistoricalMedia(content, []string{file}, true))
	assert.Equal(t, "[prior media attached]", annotateHistoricalMedia("", []string{image}, false))
}

func TestPromptMedia_InlinesAttachmentsButStoresOriginals(t *testing.T) {
	setTestDataDir(t)
	first := "data:image/png;base64,Zmlyc3Q="
	second := "data:image/jpeg;base64,c2Vjb25k"
	notes := "data:text/plain;name=notes.txt;base64," + base64.StdEncoding.EncodeToString([]byte("remember this"))
	path := writeIncomingFile(t, "log.json", []byte(`{"secret":"contents"}`))
	fileMessage := "what is this?\n[file attached: log.json (application/json, 21 B) saved at " + path + "]"
	fileURL := llm.FileURL(path)

	tests := []struct {
		name              string
		message           string
		media             []string
		wantMedia         []string
		wantContains      string
		wantNotContains   string
		storedNotContains string
	}{
		{
			name:         "inline image and text attachments",
			message:      "look",
			media:        []string{first, notes, second},
			wantMedia:    []string{first, second},
			wantContains: "<attachment name=\"notes.txt\">\nremember this\n</attachment>",
		},
		{
			name:              "file attachment inlined but not stored",
			message:           fileMessage,
			media:             []string{first, fileURL},
			wantMedia:         []string{first},
			wantContains:      `{"secret":"contents"}`,
			wantNotContains:   "image attached",
			storedNotContains: `secret":`,
		},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := &captureProvider{}
			agentID := fmt.Sprintf("agent_prompt_media_%d", i)
			runner := NewAgentRunner(
				&domain.Agent{ID: agentID, Name: "prompt-media"},
				&config.AgentConfig{Name: "prompt-media", Model: "test/model"},
				prov, nil,
			)
			promptAndWait(context.Background(), t, runner, tt.message, tt.media, RunOverrides{})

			require.NotEmpty(t, prov.requests)
			msgs := prov.requests[0].Messages
			last := msgs[len(msgs)-1]
			assert.Equal(t, tt.wantMedia, last.MediaURLs)
			assert.Contains(t, last.Content, tt.wantContains)
			if tt.wantNotContains != "" {
				assert.NotContains(t, last.Content, tt.wantNotContains)
			}

			if user := lastStoredUserMessage(t, agentID); assert.NotNil(t, user) {
				assert.Equal(t, tt.message, user.Content)
				assert.Equal(t, tt.media, user.MediaURLs)
				if tt.storedNotContains != "" {
					assert.NotContains(t, user.Content, tt.storedNotContains)
				}
			}
		})
	}
}

// lastStoredUserMessage returns the last user message stored in the agent's
// main session, or nil when it has none.
func lastStoredUserMessage(t *testing.T, agentID string) *domain.Message {
	t.Helper()
	sess, err := NewSessionManager().GetOrCreateNamed(agentID, "main")
	require.NoError(t, err)
	lines, err := store.ReadJSONL[domain.Message](store.SessionPath(agentID, sess.ID))
	require.NoError(t, err)
	var user *domain.Message
	for i := range lines {
		if lines[i].Role == domain.MessageRoleUser {
			user = &lines[i]
		}
	}
	return user
}

func TestAttachmentContentType(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, data, 0o600))
		return path
	}

	tests := []struct {
		name string
		path string
		want string
	}{
		{"extension wins over content", write("data.json", []byte{0, 1, 2}), "application/json"},
		{"sniffed text without extension", write("README", []byte("plain words\n")), "text/plain"},
		{"sniffed PDF without extension", write("scan", []byte("%PDF-1.4\n")), "application/pdf"},
		{"sniffed binary without extension", write("blob", []byte{0, 1, 2, 0xff}), "application/octet-stream"},
		{"missing file without extension", filepath.Join(dir, "missing"), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, attachmentContentType(tt.path))
		})
	}
}

func TestAttachmentDisplayName(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		{"timestamp prefix stripped", filepath.Join("media", "1700000000000_log.json"), "log.json"},
		{"only first underscore splits", filepath.Join("media", "1700000000000_my_notes.txt"), "my_notes.txt"},
		{"non-numeric prefix kept", filepath.Join("media", "draft_log.json"), "draft_log.json"},
		{"empty remainder kept", filepath.Join("media", "1700000000000_"), "1700000000000_"},
		{"no underscore", filepath.Join("media", "report.pdf"), "report.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, attachmentDisplayName(tt.path))
		})
	}
}

func TestPDFAttachmentURL(t *testing.T) {
	dir := t.TempDir()

	t.Run("readable file becomes data URL with escaped name", func(t *testing.T) {
		path := filepath.Join(dir, "1700000000000_q3 report.pdf")
		require.NoError(t, os.WriteFile(path, []byte("%PDF-1.4"), 0o600))
		got, ok := pdfAttachmentURL(path)
		assert.True(t, ok)
		assert.Equal(t, "data:application/pdf;name=q3%20report.pdf;base64,JVBERi0xLjQ=", got)
	})

	t.Run("missing file", func(t *testing.T) {
		got, ok := pdfAttachmentURL(filepath.Join(dir, "missing.pdf"))
		assert.False(t, ok)
		assert.Empty(t, got)
	})

	t.Run("file over the document limit", func(t *testing.T) {
		path := filepath.Join(dir, "huge.pdf")
		f, err := os.Create(path)
		require.NoError(t, err)
		require.NoError(t, f.Truncate(maxDocumentAttachmentBytes+1))
		require.NoError(t, f.Close())
		got, ok := pdfAttachmentURL(path)
		assert.False(t, ok)
		assert.Empty(t, got)
	})

	t.Run("unreadable path", func(t *testing.T) {
		path := filepath.Join(dir, "folder.pdf")
		require.NoError(t, os.Mkdir(path, 0o700))
		got, ok := pdfAttachmentURL(path)
		assert.False(t, ok)
		assert.Empty(t, got)
	})
}

func TestInlineTextAttachment(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(dir, name)
		require.NoError(t, os.WriteFile(path, data, 0o600))
		return path
	}

	t.Run("trailing newline is not doubled", func(t *testing.T) {
		path := write("1700000000000_notes.txt", []byte("line\n"))
		got, err := inlineTextAttachment(path)
		require.NoError(t, err)
		assert.Equal(t, "<attachment name=\"notes.txt\" path=\""+path+"\">\nline\n</attachment>", got)
	})

	t.Run("empty file", func(t *testing.T) {
		path := write("empty.txt", nil)
		got, err := inlineTextAttachment(path)
		require.NoError(t, err)
		assert.Equal(t, "<attachment name=\"empty.txt\" path=\""+path+"\">\n</attachment>", got)
	})

	t.Run("truncation does not split a multi-byte character", func(t *testing.T) {
		// "é" is two bytes; one leading byte puts a character across the limit.
		data := append([]byte("a"), bytes.Repeat([]byte("é"), maxInlineAttachmentTextBytes/2+10)...)
		path := write("accents.txt", data)
		got, err := inlineTextAttachment(path)
		require.NoError(t, err)
		body := strings.TrimPrefix(got, "<attachment name=\"accents.txt\" path=\""+path+"\">\n")
		body, _, ok := strings.Cut(body, "\n[truncated after 32 KB; read the full file at "+path+"]\n</attachment>")
		require.True(t, ok, "missing truncation footer: %q", got[len(got)-200:])
		assert.True(t, utf8.ValidString(body))
		assert.Equal(t, maxInlineAttachmentTextBytes-1, len(body))
		assert.Equal(t, string(data[:maxInlineAttachmentTextBytes-1]), body)
	})

	t.Run("non-UTF-8 content is rejected", func(t *testing.T) {
		_, err := inlineTextAttachment(write("latin1.txt", []byte{'c', 'a', 'f', 0xe9}))
		assert.EqualError(t, err, "not UTF-8 text")
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := inlineTextAttachment(filepath.Join(dir, "missing.txt"))
		assert.ErrorIs(t, err, os.ErrNotExist)
	})

	t.Run("directory cannot be read", func(t *testing.T) {
		path := filepath.Join(dir, "folder.txt")
		require.NoError(t, os.Mkdir(path, 0o700))
		_, err := inlineTextAttachment(path)
		assert.Error(t, err)
	})
}

func TestAppendTextAttachment(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "a.txt")
	require.NoError(t, os.WriteFile(text, []byte("hello"), 0o600))
	invalid := filepath.Join(dir, "b.txt")
	require.NoError(t, os.WriteFile(invalid, []byte{0xff, 0xfe}, 0o600))
	block := "<attachment name=\"a.txt\" path=\"" + text + "\">\nhello\n</attachment>"

	t.Run("empty builder gets no separator", func(t *testing.T) {
		var b strings.Builder
		appendTextAttachment(&b, text)
		assert.Equal(t, block, b.String())
	})

	t.Run("existing text is separated by a blank line", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("msg")
		appendTextAttachment(&b, text)
		assert.Equal(t, "msg\n\n"+block, b.String())
	})

	t.Run("unreadable attachment leaves builder unchanged", func(t *testing.T) {
		var b strings.Builder
		b.WriteString("msg")
		appendTextAttachment(&b, invalid)
		appendTextAttachment(&b, filepath.Join(dir, "missing.txt"))
		assert.Equal(t, "msg", b.String())
	})
}
