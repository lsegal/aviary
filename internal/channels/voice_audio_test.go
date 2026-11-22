package channels

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/transcribe"
)

// m4aBytes starts like the MP4 container Signal phones record voice notes in.
var m4aBytes = append([]byte("\x00\x00\x00\x18ftypM4A \x00\x00\x00\x00M4A mp42isom"), make([]byte, 64)...)

func TestSniffAudio(t *testing.T) {
	for _, tc := range []struct {
		name, data, want, ext string
	}{
		{"m4a", string(m4aBytes), "audio/mp4", ".m4a"},
		{"ogg", "OggS\x00\x02", "audio/ogg", ".ogg"},
		{"wav", "RIFF\x24\x00\x00\x00WAVEfmt ", "audio/wav", ".wav"},
		{"flac", "fLaC\x00", "audio/flac", ".flac"},
		{"amr", "#!AMR\n", "audio/amr", ".amr"},
		{"caf", "caff\x00\x01", "audio/x-caf", ".caf"},
		{"webm", "\x1a\x45\xdf\xa3\x01", "audio/webm", ".webm"},
		{"id3", "ID3\x04\x00", "audio/mpeg", ".mp3"},
		{"mp3 frame", "\xff\xfb\x98\xc4", "audio/mpeg", ".mp3"},
		{"adts aac", "\xff\xf1\x50\x80", "audio/aac", ".aac"},
		{"json", `{"id":"x"}`, "", ""},
		{"png", "\x89PNG\r\n\x1a\n", "", ""},
	} {
		got, ext := sniffAudio([]byte(tc.data))
		assert.Equal(t, tc.want, got, tc.name)
		assert.Equal(t, tc.ext, ext, tc.name)
	}
}

func TestPersistIncomingMedia_DetectsMislabelledAudio(t *testing.T) {
	useTempDataDir(t)
	att, err := persistIncomingMedia("signal", "attachment", "audio/mpeg", m4aBytes)
	require.NoError(t, err)
	assert.Equal(t, "audio/mp4", att.ContentType)
	assert.Equal(t, "attachment.m4a", att.Name)
	assert.True(t, strings.HasSuffix(att.Path, "_attachment.m4a"), att.Path)

	att, err = persistIncomingMedia("signal", "voice.mp3", "audio/mpeg", m4aBytes)
	require.NoError(t, err)
	assert.Equal(t, "voice.m4a", att.Name)

	// Non-audio content keeps its declared type.
	att, err = persistIncomingMedia("signal", "notes.txt", "text/plain", m4aBytes)
	require.NoError(t, err)
	assert.Equal(t, "text/plain", att.ContentType)
	assert.Equal(t, "notes.txt", att.Name)
}

// signalVoiceNoteEnvelope is a signal-cli JSON-RPC receive notification for
// a voice note, as the daemon sends it: no file name, no local path, just
// the attachment id, declared type, and size.
const signalVoiceNoteEnvelope = `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","sourceNumber":"+15550001111","sourceUuid":"7d2c1f9e-2b5a-4b8e-9c3d-1a2b3c4d5e6f","sourceName":"Sam","sourceDevice":1,"timestamp":1795391003079,"serverReceivedTimestamp":1795391003210,"serverDeliveredTimestamp":1795391003260,"dataMessage":{"timestamp":1795391003079,"message":null,"expiresInSeconds":0,"viewOnce":false,"attachments":[{"contentType":"audio/aac","filename":null,"id":"vFnSl8pGUJMCS67FMODP","size":%d,"width":null,"height":null,"caption":null,"uploadTimestamp":1795391002950}]}},"account":"+12135550123"}}`

func dispatchSignalVoiceNote(t *testing.T, size int, fetch func() ([]byte, error)) IncomingMessage {
	t.Helper()
	orig := signalAttachmentFetcher
	signalAttachmentFetcher = func(_ context.Context, _ *SignalChannel, _, id, _, _ string, _ bool) ([]byte, error) {
		assert.Equal(t, "vFnSl8pGUJMCS67FMODP", id)
		return fetch()
	}
	t.Cleanup(func() { signalAttachmentFetcher = orig })
	ch := NewSignalChannel("+12135550123", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })
	ch.dispatch(fmt.Appendf(nil, signalVoiceNoteEnvelope, size))
	return waitMsg(t, msgs, 3*time.Second)
}

func TestSignal_VoiceNoteEnvelopeSavesFetchedAudio(t *testing.T) {
	useTempDataDir(t)
	msg := dispatchSignalVoiceNote(t, len(m4aBytes), func() ([]byte, error) { return m4aBytes, nil })
	require.Len(t, msg.Attachments, 1)
	att := msg.Attachments[0]
	require.NotEmpty(t, att.Path, att.Err)
	saved, err := os.ReadFile(att.Path)
	require.NoError(t, err)
	assert.Equal(t, m4aBytes, saved)
	assert.Equal(t, int64(len(m4aBytes)), att.Size)
	assert.Equal(t, "audio/mp4", att.ContentType)
	assert.Equal(t, "attachment.m4a", att.Name)
	assert.True(t, att.IsAudio())
}

func TestSignal_IncompleteFetchedVoiceNoteIsNotSaved(t *testing.T) {
	useTempDataDir(t)
	msg := dispatchSignalVoiceNote(t, 4096, func() ([]byte, error) { return m4aBytes, nil })
	require.Len(t, msg.Attachments, 1)
	assert.Empty(t, msg.Attachments[0].Path)
	assert.Contains(t, msg.Attachments[0].Err, "incomplete")
}

func TestSignal_WaitsForStoredFileToFinishWriting(t *testing.T) {
	useTempDataDir(t)
	source := writeSource(t, "vFnSl8pGUJMCS67FMODP", m4aBytes[:10])
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = os.WriteFile(source, m4aBytes, 0o600)
	}()
	msg := dispatchSignalAttachments(t, fmt.Sprintf(`[{"contentType":"audio/aac","storedFilename":%q,"size":%d}]`, source, len(m4aBytes)))
	require.Len(t, msg.Attachments, 1)
	saved, err := os.ReadFile(msg.Attachments[0].Path)
	require.NoError(t, err)
	assert.Equal(t, m4aBytes, saved)
}

func TestTranscribeAttachments_FailuresAreShortAndKeepPath(t *testing.T) {
	att := Attachment{Name: "attachment.mp3", ContentType: "audio/mpeg", Size: 864, Path: "/media/incoming/signal/1_attachment.mp3"}
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{
			"empty or corrupt",
			fmt.Errorf("%w: ffmpeg wrote 72ms of audio", transcribe.ErrUnreadableAudio),
			"[voice message, not transcribed: couldn't transcribe this voice message (audio was empty/corrupt); audio saved at " + att.Path + "]",
		},
		{
			"whisper-cli crash",
			fmt.Errorf("%w: exit status 11: read_audio_data: reading audio data from '/tmp/aviary-transcribe-123/audio.wav'", transcribe.ErrTranscriberFailed),
			"[voice message, not transcribed: couldn't transcribe this voice message (transcription failed); audio saved at " + att.Path + "]",
		},
		{
			"other",
			errors.New("something odd at /tmp/aviary-transcribe-123"),
			"[voice message, not transcribed: couldn't transcribe this voice message (transcription failed); audio saved at " + att.Path + "]",
		},
	} {
		atts := TranscribeAttachments(context.Background(), &fakeEngine{err: tc.err}, transcribe.Options{}, []Attachment{att})
		line := atts[0].VoiceLine()
		assert.Equal(t, tc.want, line, tc.name)
		assert.NotContains(t, line, "aviary-transcribe", tc.name)
	}
}
