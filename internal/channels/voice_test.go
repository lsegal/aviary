package channels

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/llm"
	"github.com/lsegal/aviary/internal/transcribe"
)

// oggBytes starts like an Ogg file so content sniffing agrees it is audio.
var oggBytes = []byte("OggS\x00\x02voice-note-data")

type fakeEngine struct {
	calls []string
	text  string
	err   error
}

func (f *fakeEngine) Transcribe(_ context.Context, path string, opts transcribe.Options) (transcribe.Transcript, error) {
	f.calls = append(f.calls, path+"|"+opts.Language)
	if f.err != nil {
		return transcribe.Transcript{}, f.err
	}
	return transcribe.Transcript{Text: f.text, Language: "en", Duration: 12 * time.Second}, nil
}

func assertVoicePrompt(t *testing.T, msg IncomingMessage, wantText string) {
	t.Helper()
	require.Len(t, msg.Attachments, 1)
	require.True(t, msg.Attachments[0].IsAudio(), "attachment should be detected as audio: %+v", msg.Attachments[0])
	engine := &fakeEngine{text: "hello there"}
	atts := TranscribeAttachments(context.Background(), engine, transcribe.Options{Language: "auto"}, msg.Attachments)
	require.Len(t, engine.calls, 1)
	att := atts[0]
	text, media := PromptInput(msg.Text, atts)
	want := "[voice message 0:12, transcribed]: hello there\n" + att.Placeholder()
	if wantText != "" {
		want = wantText + "\n" + want
	}
	assert.Equal(t, want, text)
	// The original file stays available to the agent.
	assert.Equal(t, []string{llm.FileURL(att.Path)}, media)
	assert.FileExists(t, att.Path)
}

func TestSignal_VoiceNoteIsTranscribed(t *testing.T) {
	useTempDataDir(t)
	source := writeSource(t, "voice123", oggBytes)
	// Signal voice notes arrive as audio/aac with no file name.
	msg := dispatchSignalAttachments(t, fmt.Sprintf(`[{"contentType":"audio/aac","storedFilename":%q,"size":%d}]`, source, len(oggBytes)))
	assertVoicePrompt(t, msg, "what is this?")
}

func TestSlack_AudioClipIsTranscribed(t *testing.T) {
	useTempDataDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Slack sometimes serves files as octet-stream; the extension decides.
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(oggBytes)
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
			Files:   []slack.File{{Name: "audio_message.m4a", URLPrivateDownload: srv.URL + "/audio_message.m4a"}},
		},
	})
	assertVoicePrompt(t, waitMsg(t, msgs, time.Second), "")
}

func TestDiscord_VoiceMessageIsTranscribed(t *testing.T) {
	useTempDataDir(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/ogg")
		_, _ = w.Write(oggBytes)
	}))
	defer srv.Close()

	ch := NewDiscordChannel("token123", []config.AllowFromEntry{{From: "*"}}, "gpt-4", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })
	require.True(t, ch.handleMessage(&discordgo.Message{
		Author:      &discordgo.User{ID: "U123"},
		ChannelID:   "C123",
		Attachments: []*discordgo.MessageAttachment{{Filename: "voice-message.ogg", ContentType: "audio/ogg", URL: srv.URL + "/voice-message.ogg"}},
	}, "BOT123"))
	assertVoicePrompt(t, waitMsg(t, msgs, time.Second), "")
}

func TestLooksLikeAudio(t *testing.T) {
	for _, tc := range []struct {
		contentType, name string
		want              bool
	}{
		{"audio/ogg", "", true},
		{"audio/mp4; codecs=opus", "", true},
		{"video/webm", "", true},
		{"application/ogg", "", true},
		{"application/octet-stream", "note.opus", true},
		{"", "clip.m4a", true},
		{"", "clip.aac", true},
		{"", "clip.mp3", true},
		{"", "clip.wav", true},
		{"", "clip.webm", true},
		{"video/mp4", "movie.mp4", false},
		{"image/png", "voice.ogg", false},
		{"", "notes.txt", false},
	} {
		assert.Equal(t, tc.want, looksLikeAudio(tc.contentType, tc.name), "%q %q", tc.contentType, tc.name)
	}
}

func TestTranscribeAttachments_FailureKeepsMessageAndFile(t *testing.T) {
	att := Attachment{Name: "voice.ogg", ContentType: "audio/ogg", Size: 10, Path: "/tmp/voice.ogg"}
	engine := &fakeEngine{err: &transcribe.UnavailableError{Missing: []string{"whisper-cli"}}}
	atts := TranscribeAttachments(context.Background(), engine, transcribe.Options{}, []Attachment{att})

	text, media := PromptInput("listen", atts)
	assert.Equal(t, "listen\n[voice message, not transcribed: transcription unavailable: whisper-cli missing; "+transcribe.InstallHint+"; audio saved at "+att.Path+"]\n"+att.Placeholder(), text)
	assert.Equal(t, []string{llm.FileURL(att.Path)}, media)
}

func TestTranscribeAttachments_DisabledKeepsPlaceholder(t *testing.T) {
	att := Attachment{Name: "voice.ogg", ContentType: "audio/ogg", Size: 10, Path: "/tmp/voice.ogg"}
	atts := TranscribeAttachments(context.Background(), nil, transcribe.Options{}, []Attachment{att})
	text, _ := PromptInput("", atts)
	assert.Equal(t, att.Placeholder(), text)
}

func TestTranscribeAttachments_SkipsNonAudioAndUnsaved(t *testing.T) {
	engine := &fakeEngine{text: "x"}
	TranscribeAttachments(context.Background(), engine, transcribe.Options{}, []Attachment{
		{Name: "log.txt", ContentType: "text/plain", Path: "/tmp/log.txt"},
		{Name: "voice.ogg", ContentType: "audio/ogg", Err: "download failed"},
	})
	assert.Empty(t, engine.calls)
}
