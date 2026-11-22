package agent

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestPromptMedia_FileAttachmentInlinedButNotStored(t *testing.T) {
	setTestDataDir(t)
	prov := &captureProvider{}
	runner := NewAgentRunner(
		&domain.Agent{ID: "agent_file_media", Name: "file-media"},
		&config.AgentConfig{Name: "file-media", Model: "test/model"},
		prov, nil,
	)
	path := writeIncomingFile(t, "log.json", []byte(`{"secret":"contents"}`))
	image := "data:image/png;base64,cG5n"
	message := "what is this?\n[file attached: log.json (application/json, 21 B) saved at " + path + "]"
	fileURL := llm.FileURL(path)

	done := make(chan struct{}, 1)
	runner.PromptMedia(context.Background(), message, []string{image, fileURL}, func(e StreamEvent) {
		if e.Type == StreamEventDone || e.Type == StreamEventError || e.Type == StreamEventStop {
			select {
			case done <- struct{}{}:
			default:
			}
		}
	})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		assert.FailNow(t, "timeout")
	}
	runner.Wait()

	require.NotEmpty(t, prov.requests)
	msgs := prov.requests[0].Messages
	last := msgs[len(msgs)-1]
	assert.Equal(t, []string{image}, last.MediaURLs)
	assert.Contains(t, last.Content, `{"secret":"contents"}`)
	assert.NotContains(t, last.Content, "image attached")

	sess, err := NewSessionManager().GetOrCreateNamed(runner.agent.ID, "main")
	require.NoError(t, err)
	lines, err := store.ReadJSONL[domain.Message](store.SessionPath(runner.agent.ID, sess.ID))
	require.NoError(t, err)
	var user *domain.Message
	for i := range lines {
		if lines[i].Role == domain.MessageRoleUser {
			user = &lines[i]
		}
	}
	if assert.NotNil(t, user) {
		assert.Equal(t, message, user.Content)
		assert.Equal(t, []string{image, fileURL}, user.MediaURLs)
		assert.False(t, strings.Contains(user.Content, "secret\":"))
	}
}
