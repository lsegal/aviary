package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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
