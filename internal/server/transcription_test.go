package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/transcribe"
)

type stubTranscriber struct {
	text string
	err  error
	got  []byte
}

func (s *stubTranscriber) Transcribe(_ context.Context, path string, _ transcribe.Options) (transcribe.Transcript, error) {
	s.got, _ = os.ReadFile(path)
	if s.err != nil {
		return transcribe.Transcript{}, s.err
	}
	return transcribe.Transcript{Text: s.text, Language: "en", Duration: 3 * time.Second}, nil
}

func useTranscriber(t *testing.T, engine transcribe.Engine) {
	t.Helper()
	orig := newTranscriber
	newTranscriber = func(cfg config.TranscriptionConfig) (transcribe.Engine, transcribe.Settings, error) {
		s := transcribe.SettingsFrom(cfg)
		if !s.Enabled {
			return nil, s, transcribe.ErrDisabled
		}
		return engine, s, nil
	}
	t.Cleanup(func() { newTranscriber = orig })
}

func postRecording(t *testing.T, s *Server, body string) (int, transcribeResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/transcribe", strings.NewReader(body))
	req.Header.Set("Content-Type", "audio/webm;codecs=opus")
	rec := httptest.NewRecorder()
	s.transcribeHandler(rec, req)
	var resp transcribeResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return rec.Code, resp
}

func TestTranscribeHandler_ReturnsTranscript(t *testing.T) {
	engine := &stubTranscriber{text: "hello from the mic"}
	useTranscriber(t, engine)
	code, resp := postRecording(t, &Server{cfg: &config.Config{}}, "webm-bytes")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, transcribeResponse{Text: "hello from the mic", Language: "en", DurationMS: 3000}, resp)
	assert.Equal(t, "webm-bytes", string(engine.got))
}

func TestTranscribeHandler_EngineMissing(t *testing.T) {
	useTranscriber(t, &stubTranscriber{err: &transcribe.UnavailableError{Missing: []string{"whisper-cli", "whisper model base.en"}}})
	code, resp := postRecording(t, &Server{cfg: &config.Config{}}, "webm-bytes")
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, []string{"whisper-cli", "whisper model base.en"}, resp.Missing)
	assert.Equal(t, transcribe.InstallHint, resp.Install)
	assert.Contains(t, resp.Error, "aviary deps install")
}

func TestTranscribeHandler_Disabled(t *testing.T) {
	useTranscriber(t, &stubTranscriber{})
	off := false
	code, resp := postRecording(t, &Server{cfg: &config.Config{Transcription: config.TranscriptionConfig{Enabled: &off}}}, "x")
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Contains(t, resp.Error, "disabled")
}

func TestTranscribeHandler_EmptyRecording(t *testing.T) {
	useTranscriber(t, &stubTranscriber{})
	code, resp := postRecording(t, &Server{cfg: &config.Config{}}, "")
	assert.Equal(t, http.StatusBadRequest, code)
	assert.Contains(t, resp.Error, "empty")
}

func TestTranscribeHandler_ReportsNoSpeech(t *testing.T) {
	useTranscriber(t, noSpeechEngine{})
	code, resp := postRecording(t, &Server{cfg: &config.Config{}}, "webm-bytes")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, transcribeResponse{DurationMS: 9000, NoSpeech: true}, resp)
}

type noSpeechEngine struct{}

func (noSpeechEngine) Transcribe(context.Context, string, transcribe.Options) (transcribe.Transcript, error) {
	return transcribe.Transcript{Duration: 9 * time.Second, NoSpeech: true}, nil
}

func TestRecordingExt(t *testing.T) {
	cases := []struct {
		name, head, contentType, want string
	}{
		{"webm magic beats a wrong type", "\x1a\x45\xdf\xa3\x9f\x42\x86\x81", "audio/mp4", ".webm"},
		{"ogg magic", "OggS\x00\x02", "", ".ogg"},
		{"safari mp4 magic", "\x00\x00\x00\x1cftypisom", "audio/webm", ".m4a"},
		{"wav magic", "RIFF\x24\x00\x00\x00WAVE", "", ".wav"},
		{"mp3 magic", "ID3\x04\x00", "", ".mp3"},
		{"type with parameters", "????", "audio/ogg; codecs=opus", ".ogg"},
		{"mp4 type", "????", "audio/mp4;codecs=mp4a.40.2", ".m4a"},
		{"unknown defaults to webm", "????", "", ".webm"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "rec")
			require.NoError(t, os.WriteFile(path, []byte(tc.head), 0o600))
			assert.Equal(t, tc.want, recordingExt(path, tc.contentType))
		})
	}
}

// useLocalWhisper transcribes with the real whisper-cli and ffmpeg, skipping
// the test when they or the model are not installed. Tests use their own data
// directory, so point AVIARY_CONFIG_TRANSCRIPTION_MODEL at a downloaded model
// (for example ~/.config/aviary/deps/whisper/ggml-base.en.bin) to run them.
func useLocalWhisper(t *testing.T) {
	t.Helper()
	st := transcribe.Detect("")
	if missing := st.Missing(); len(missing) > 0 {
		t.Skipf("local transcription not installed: missing %s", strings.Join(missing, ", "))
	}
	useTranscriber(t, transcribe.NewLocal(st.Model.Path))
}

func postFixture(t *testing.T, name string) (int, transcribeResponse) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	require.NoError(t, err)
	return postRecording(t, &Server{cfg: &config.Config{}}, string(data))
}

func TestTranscribeHandler_WebMSpeechEndToEnd(t *testing.T) {
	useLocalWhisper(t)
	code, resp := postFixture(t, "speech.webm")
	require.Equal(t, http.StatusOK, code, resp.Error)
	assert.False(t, resp.NoSpeech)
	assert.Regexp(t, `(?i)^test (one|1), (two|2), (three|3)\.?$`, resp.Text)
	assert.Positive(t, resp.DurationMS)
}

func TestTranscribeHandler_WebMSilenceEndToEnd(t *testing.T) {
	useLocalWhisper(t)
	code, resp := postFixture(t, "silence.webm")
	require.Equal(t, http.StatusOK, code, resp.Error)
	assert.True(t, resp.NoSpeech)
	assert.Empty(t, resp.Text)
	assert.InDelta(t, 3000, resp.DurationMS, 100)
}

// derivedFixture converts testdata/speech.webm with ffmpeg filter and
// output options, the way a USB audio interface would have recorded it.
func derivedFixture(t *testing.T, name string, args ...string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), name)
	cmd := append([]string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", filepath.Join("testdata", "speech.webm")}, args...)
	data, err := exec.Command(transcribe.Detect("").FFmpeg.Path, append(cmd, out)...).CombinedOutput()
	require.NoError(t, err, string(data))
	data, err = os.ReadFile(out)
	require.NoError(t, err)
	return string(data)
}

func assertTestOneTwoThree(t *testing.T, body string) {
	t.Helper()
	code, resp := postRecording(t, &Server{cfg: &config.Config{}}, body)
	require.Equal(t, http.StatusOK, code, resp.Error)
	assert.False(t, resp.NoSpeech)
	assert.Regexp(t, `(?i)^test (one|1), (two|2), (three|3)\.?$`, resp.Text)
}

func TestTranscribeHandler_StereoLeftChannelEndToEnd(t *testing.T) {
	useLocalWhisper(t)
	// A mic on input 1 of a stereo interface at modest gain: speech on the
	// left, nothing on the right. Averaging the channels would halve it.
	assertTestOneTwoThree(t, derivedFixture(t, "left.webm", "-af", "volume=-40dB,pan=stereo|c0=c0|c1=0*c0", "-c:a", "libopus"))
}

func TestTranscribeHandler_192kHzEndToEnd(t *testing.T) {
	useLocalWhisper(t)
	assertTestOneTwoThree(t, derivedFixture(t, "hires.wav", "-af", "pan=stereo|c0=c0|c1=0*c0", "-ar", "192000", "-c:a", "pcm_s24le"))
}

func TestTranscribeHandler_LowLevelSpeechEndToEnd(t *testing.T) {
	useLocalWhisper(t)
	// About -50 dBFS peak, like a low-gain interface preamp.
	assertTestOneTwoThree(t, derivedFixture(t, "quiet.webm", "-af", "volume=-46dB", "-c:a", "libopus"))
}

func TestTranscriptionHandler_ReportsStatus(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	s := &Server{cfg: &config.Config{Transcription: config.TranscriptionConfig{Model: "tiny"}}}
	rec := httptest.NewRecorder()
	s.transcriptionHandler(rec, httptest.NewRequest(http.MethodGet, "/api/transcription", nil))
	require.Equal(t, http.StatusOK, rec.Code)
	var resp transcriptionStatus
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.True(t, resp.Enabled)
	assert.Equal(t, "local", resp.Engine)
	assert.Equal(t, "tiny", resp.Status.Model.Name)
	assert.False(t, resp.Status.Model.Downloaded)
	assert.False(t, resp.Ready)
	assert.Contains(t, resp.Missing, "whisper model tiny")
	assert.Equal(t, transcribe.InstallHint, resp.Install)
}

func TestTranscribeAttachments_EngineMissingStillDeliversMessage(t *testing.T) {
	useTranscriber(t, &stubTranscriber{err: &transcribe.UnavailableError{Missing: []string{"whisper-cli"}}})
	s := &Server{cfg: &config.Config{}}
	att := channels.Attachment{Name: "voice.ogg", ContentType: "audio/ogg", Size: 4, Path: filepath.Join(t.TempDir(), "voice.ogg")}
	require.NoError(t, os.WriteFile(att.Path, []byte("OggS"), 0o600))

	text, media := channels.PromptInput("", s.transcribeAttachments(context.Background(), []channels.Attachment{att}))
	assert.Contains(t, text, "[voice message, not transcribed: transcription unavailable: whisper-cli missing; ")
	assert.Contains(t, text, "aviary deps install")
	assert.Contains(t, text, att.Placeholder())
	assert.Len(t, media, 1)
}

func TestTranscribeAttachments_Transcribed(t *testing.T) {
	useTranscriber(t, &stubTranscriber{text: "call me back"})
	s := &Server{cfg: &config.Config{}}
	att := channels.Attachment{Name: "voice.ogg", ContentType: "audio/ogg", Size: 4, Path: filepath.Join(t.TempDir(), "voice.ogg")}
	require.NoError(t, os.WriteFile(att.Path, []byte("OggS"), 0o600))
	text, _ := channels.PromptInput("", s.transcribeAttachments(context.Background(), []channels.Attachment{att}))
	assert.True(t, strings.HasPrefix(text, "[voice message 0:03, transcribed]: call me back\n"), text)
}

func TestTranscribeAttachments_DisabledKeepsFilePlaceholder(t *testing.T) {
	useTranscriber(t, &stubTranscriber{text: "unused"})
	off := false
	s := &Server{cfg: &config.Config{Transcription: config.TranscriptionConfig{Enabled: &off}}}
	att := channels.Attachment{Name: "voice.ogg", ContentType: "audio/ogg", Size: 4, Path: "/tmp/voice.ogg"}
	text, _ := channels.PromptInput("", s.transcribeAttachments(context.Background(), []channels.Attachment{att}))
	assert.Equal(t, att.Placeholder(), text)
}

func TestMediaHandler_ServesOnlyAudioUnderMediaDir(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	dir := store.IncomingMediaDir("signal")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	voice := filepath.Join(dir, "1_voice.ogg")
	require.NoError(t, os.WriteFile(voice, []byte("OggS"), 0o600))
	notes := filepath.Join(dir, "1_notes.txt")
	require.NoError(t, os.WriteFile(notes, []byte("secret"), 0o600))
	outside := filepath.Join(t.TempDir(), "x.ogg")
	require.NoError(t, os.WriteFile(outside, []byte("OggS"), 0o600))

	get := func(path string) int {
		rec := httptest.NewRecorder()
		mediaHandler(rec, httptest.NewRequest(http.MethodGet, "/api/media?path="+path, nil))
		return rec.Code
	}
	assert.Equal(t, http.StatusOK, get(voice))
	assert.Equal(t, http.StatusNotFound, get(notes))
	assert.Equal(t, http.StatusNotFound, get(outside))
	assert.Equal(t, http.StatusNotFound, get(dir+"/../../../../x.ogg"))
}
