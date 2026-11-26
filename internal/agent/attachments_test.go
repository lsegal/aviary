package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
