package channels

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/store"
)

var pngBytes = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}

func useTempDataDir(t *testing.T) {
	t.Helper()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
}

func writeSource(t *testing.T, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, data, 0o600))
	return path
}

func dispatchSignalAttachments(t *testing.T, attachmentsJSON string) IncomingMessage {
	t.Helper()
	ch := NewSignalChannel("", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })
	ch.dispatch([]byte(`{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"what is this?","attachments":` + attachmentsJSON + `}}}}`))
	return waitMsg(t, msgs, time.Second)
}

func TestSignal_JSONAttachmentIsSavedFile(t *testing.T) {
	useTempDataDir(t)
	data := []byte(`{"events":[1,2,3]}`)
	source := writeSource(t, "abc123", data)

	msg := dispatchSignalAttachments(t, fmt.Sprintf(`[{"contentType":"application/json","filename":"chrome-net-export-log.json","storedFilename":%q,"size":%d}]`, source, len(data)))

	require.Len(t, msg.Attachments, 1)
	att := msg.Attachments[0]
	assert.Equal(t, "chrome-net-export-log.json", att.Name)
	assert.Equal(t, "application/json", att.ContentType)
	assert.Equal(t, int64(len(data)), att.Size)
	assert.Empty(t, att.DataURL)
	saved, err := os.ReadFile(att.Path)
	require.NoError(t, err)
	assert.Equal(t, data, saved)
	assert.True(t, strings.HasPrefix(att.Path, store.IncomingMediaDir("signal")))

	text, media := PromptInput(msg.Text, msg.Attachments)
	assert.Equal(t, "what is this?\n[file attached: chrome-net-export-log.json (application/json, 18 B) saved at "+att.Path+"]", text)
	assert.Equal(t, []string{llm.FileURL(att.Path)}, media)
	assert.NotContains(t, text, "image")
}

func TestSignal_ImageAttachmentStillInline(t *testing.T) {
	useTempDataDir(t)
	source := writeSource(t, "photo.png", pngBytes)

	msg := dispatchSignalAttachments(t, fmt.Sprintf(`[{"contentType":"image/png","storedFilename":%q}]`, source))

	require.Len(t, msg.Attachments, 1)
	text, media := PromptInput(msg.Text, msg.Attachments)
	assert.Equal(t, "what is this?", text)
	require.Len(t, media, 1)
	assert.True(t, strings.HasPrefix(media[0], "data:image/png;base64,"))
}

func TestSignal_LargeBinaryAttachmentSavedWithoutInlineData(t *testing.T) {
	useTempDataDir(t)
	data := bytes.Repeat([]byte{0x00, 0xff, 0x10}, 1<<20) // 3 MB
	source := writeSource(t, "dump.bin", data)

	msg := dispatchSignalAttachments(t, fmt.Sprintf(`[{"contentType":"application/octet-stream","filename":"dump.bin","storedFilename":%q}]`, source))

	require.Len(t, msg.Attachments, 1)
	att := msg.Attachments[0]
	assert.Empty(t, att.DataURL)
	info, err := os.Stat(att.Path)
	require.NoError(t, err)
	assert.Equal(t, int64(len(data)), info.Size())
	text, media := PromptInput(msg.Text, msg.Attachments)
	assert.Contains(t, text, "[file attached: dump.bin (application/octet-stream, 3.0 MB) saved at "+att.Path+"]")
	assert.Less(t, len(text), 1024)
	assert.Equal(t, []string{llm.FileURL(att.Path)}, media)
}

func TestSignal_MixedAttachmentsKeepEveryFile(t *testing.T) {
	useTempDataDir(t)
	image := writeSource(t, "photo.png", pngBytes)
	notes := writeSource(t, "notes.txt", []byte("hello"))
	pdf := writeSource(t, "report.pdf", []byte("%PDF-1.4"))

	msg := dispatchSignalAttachments(t, fmt.Sprintf(`[
		{"contentType":"text/plain","filename":"notes.txt","storedFilename":%q},
		{"contentType":"image/png","storedFilename":%q},
		{"contentType":"application/pdf","filename":"report.pdf","storedFilename":%q},
		{"contentType":"application/zip","filename":"missing.zip","storedFilename":"/does/not/exist"}
	]`, notes, image, pdf))

	require.Len(t, msg.Attachments, 4)
	text, media := PromptInput(msg.Text, msg.Attachments)
	lines := strings.Split(text, "\n")
	require.Len(t, lines, 4)
	assert.Equal(t, "what is this?", lines[0])
	assert.Contains(t, lines[1], "[file attached: notes.txt (text/plain, 5 B) saved at ")
	assert.Contains(t, lines[2], "[file attached: report.pdf (application/pdf, 8 B) saved at ")
	assert.Contains(t, lines[3], "[file attached: missing.zip (application/zip) could not be saved: ")
	require.Len(t, media, 3)
	assert.Equal(t, llm.FileURL(msg.Attachments[0].Path), media[0])
	assert.True(t, strings.HasPrefix(media[1], "data:image/png;base64,"))
	assert.Equal(t, llm.FileURL(msg.Attachments[2].Path), media[2])
}

func TestPersistIncomingMedia_OversizedImageBecomesFile(t *testing.T) {
	useTempDataDir(t)
	att, err := persistIncomingMedia("signal", "huge.png", "image/png", make([]byte, maxInlineIncomingMediaBytes+1))
	require.NoError(t, err)
	assert.Empty(t, att.DataURL)
	assert.Contains(t, att.Placeholder(), "[file attached: huge.png (image/png, 8.0 MB) saved at ")
}

func TestPersistIncomingMedia_DetectsTypeFromName(t *testing.T) {
	useTempDataDir(t)
	att, err := persistIncomingMedia("signal", "log.json", "", []byte(`{}`))
	require.NoError(t, err)
	assert.Equal(t, "application/json", att.ContentType)
	assert.False(t, looksLikeImage(att.ContentType, att.Name))
	assert.False(t, looksLikeImage("application/json", "export.png"))
	assert.True(t, looksLikeImage("", "photo.PNG"))
}

func TestSlack_NonImageAttachmentSaved(t *testing.T) {
	useTempDataDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer xoxb-token", r.Header.Get("Authorization"))
		if strings.HasSuffix(r.URL.Path, ".png") {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(pngBytes)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(`{"a":1}`))
	}))
	defer srv.Close()

	ch := NewSlackChannel("xapp-token", "xoxb-token", []config.AllowFromEntry{{From: "*"}}, "m", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })
	ch.handleMessageEvent(&slackevents.MessageEvent{
		User:    "U123",
		Channel: "D123",
		Message: &slack.Msg{
			User:    "U123",
			Channel: "D123",
			Files: []slack.File{
				{Name: "data.json", Mimetype: "application/json", URLPrivateDownload: srv.URL + "/data.json"},
				{Name: "photo.png", Mimetype: "image/png", URLPrivateDownload: srv.URL + "/photo.png"},
			},
		},
	})

	msg := waitMsg(t, msgs, time.Second)
	require.Len(t, msg.Attachments, 2)
	assert.Equal(t, "application/json", msg.Attachments[0].ContentType)
	assert.Empty(t, msg.Attachments[0].DataURL)
	assert.FileExists(t, msg.Attachments[0].Path)
	assert.True(t, strings.HasPrefix(msg.Attachments[1].DataURL, "data:image/png;base64,"))
	text, _ := PromptInput(msg.Text, msg.Attachments)
	assert.Contains(t, text, "[file attached: data.json (application/json, 7 B) saved at ")
}

func TestDiscord_NonImageAttachmentSaved(t *testing.T) {
	useTempDataDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("log line"))
	}))
	defer srv.Close()

	ch := NewDiscordChannel("token123", []config.AllowFromEntry{{From: "*"}}, "gpt-4", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })
	ok := ch.handleMessage(&discordgo.Message{
		Author:    &discordgo.User{ID: "U123"},
		ChannelID: "C123",
		Content:   "check this",
		Attachments: []*discordgo.MessageAttachment{{
			Filename: "server.log",
			URL:      srv.URL + "/server.log",
		}},
	}, "BOT123")
	assert.True(t, ok)

	msg := waitMsg(t, msgs, time.Second)
	require.Len(t, msg.Attachments, 1)
	assert.Equal(t, "text/plain", msg.Attachments[0].ContentType)
	assert.Empty(t, msg.Attachments[0].DataURL)
	assert.FileExists(t, msg.Attachments[0].Path)
	text, media := PromptInput(msg.Text, msg.Attachments)
	assert.Equal(t, "check this\n[file attached: server.log (text/plain, 8 B) saved at "+msg.Attachments[0].Path+"]", text)
	assert.Equal(t, []string{llm.FileURL(msg.Attachments[0].Path)}, media)
}
