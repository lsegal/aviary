package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
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
	silent, quiet := -120.0, -20.0
	for name, tc := range map[string]struct {
		engine noSpeechEngine
		want   transcribeResponse
	}{
		"no signal":  {noSpeechEngine{noSignal: true, peak: &silent}, transcribeResponse{DurationMS: 9000, NoSpeech: true, NoSignal: true, PeakDBFS: &silent}},
		"no words":   {noSpeechEngine{peak: &quiet}, transcribeResponse{DurationMS: 9000, NoSpeech: true, PeakDBFS: &quiet}},
		"unmeasured": {noSpeechEngine{}, transcribeResponse{DurationMS: 9000, NoSpeech: true}},
	} {
		t.Run(name, func(t *testing.T) {
			useTranscriber(t, tc.engine)
			code, resp := postRecording(t, &Server{cfg: &config.Config{}}, "webm-bytes")
			assert.Equal(t, http.StatusOK, code)
			assert.Equal(t, tc.want, resp)
		})
	}
}

type noSpeechEngine struct {
	noSignal bool
	peak     *float64
}

func (e noSpeechEngine) Transcribe(context.Context, string, transcribe.Options) (transcribe.Transcript, error) {
	return transcribe.Transcript{Duration: 9 * time.Second, NoSpeech: true, NoSignal: e.noSignal, PeakDBFS: e.peak}, nil
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
	assert.True(t, resp.NoSignal)
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
	assert.False(t, resp.NoSignal)
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

func TestTranscriptionConfig_NilConfig(t *testing.T) {
	assert.Equal(t, config.TranscriptionConfig{}, (&Server{}).transcriptionConfig())
	model := "tiny"
	s := &Server{cfg: &config.Config{Transcription: config.TranscriptionConfig{Model: model}}}
	assert.Equal(t, model, s.transcriptionConfig().Model)
}

func TestTranscribeHandler_NilConfigUsesDefaults(t *testing.T) {
	useTranscriber(t, &stubTranscriber{text: "defaults"})
	code, resp := postRecording(t, &Server{}, "webm-bytes")
	assert.Equal(t, http.StatusOK, code)
	assert.Equal(t, "defaults", resp.Text)
}

func TestTranscribeHandler_RejectsNonPost(t *testing.T) {
	engine := &stubTranscriber{text: "unused"}
	useTranscriber(t, engine)
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		rec := httptest.NewRecorder()
		(&Server{cfg: &config.Config{}}).transcribeHandler(rec, httptest.NewRequest(method, "/api/transcribe", strings.NewReader("x")))
		assert.Equal(t, http.StatusMethodNotAllowed, rec.Code, method)
	}
	assert.Nil(t, engine.got, "the engine must not run for a rejected method")
}

func TestTranscribeHandler_TranscriberConstructionError(t *testing.T) {
	orig := newTranscriber
	newTranscriber = func(config.TranscriptionConfig) (transcribe.Engine, transcribe.Settings, error) {
		return nil, transcribe.Settings{}, errors.New(`unknown transcription engine "cloud"`)
	}
	t.Cleanup(func() { newTranscriber = orig })
	code, resp := postRecording(t, &Server{cfg: &config.Config{}}, "webm-bytes")
	assert.Equal(t, http.StatusServiceUnavailable, code)
	assert.Equal(t, `unknown transcription engine "cloud"`, resp.Error)
}

func TestTranscribeHandler_TempFileError(t *testing.T) {
	engine := &stubTranscriber{text: "unused"}
	useTranscriber(t, engine)
	missing := filepath.Join(t.TempDir(), "missing")
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, missing)
	}
	code, resp := postRecording(t, &Server{cfg: &config.Config{}}, "webm-bytes")
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.NotEmpty(t, resp.Error)
	assert.Nil(t, engine.got)
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func TestTranscribeHandler_BodyReadError(t *testing.T) {
	engine := &stubTranscriber{text: "unused"}
	useTranscriber(t, engine)
	req := httptest.NewRequest(http.MethodPost, "/api/transcribe", failingReader{err: errors.New("connection reset")})
	rec := httptest.NewRecorder()
	(&Server{cfg: &config.Config{}}).transcribeHandler(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var resp transcribeResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "reading recording: connection reset", resp.Error)
	assert.Nil(t, engine.got)
}

func TestTranscribeHandler_RecordingTooLarge(t *testing.T) {
	engine := &stubTranscriber{text: "unused"}
	useTranscriber(t, engine)
	// Stream one byte past the limit without holding it in memory.
	body := io.LimitReader(zeroReader{}, maxRecordingBytes+1)
	req := httptest.NewRequest(http.MethodPost, "/api/transcribe", body)
	rec := httptest.NewRecorder()
	(&Server{cfg: &config.Config{}}).transcribeHandler(rec, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	var resp transcribeResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "the recording is larger than 50 MB", resp.Error)
	assert.Nil(t, engine.got, "an oversized recording must not be transcribed")
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestTranscribeHandler_GenericEngineError(t *testing.T) {
	useTranscriber(t, &stubTranscriber{err: errors.New("ffmpeg exited with status 1")})
	code, resp := postRecording(t, &Server{cfg: &config.Config{}}, "webm-bytes")
	assert.Equal(t, http.StatusInternalServerError, code)
	assert.Equal(t, transcribeResponse{Error: "ffmpeg exited with status 1"}, resp)
}

func TestTranscribeHandler_RemovesTemporaryFiles(t *testing.T) {
	tmp := t.TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, tmp)
	}
	var seen string
	useTranscriber(t, engineFunc(func(_ context.Context, path string, _ transcribe.Options) (transcribe.Transcript, error) {
		seen = path
		return transcribe.Transcript{Text: "ok"}, nil
	}))
	code, _ := postRecording(t, &Server{cfg: &config.Config{}}, "OggS\x00\x02rest")
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, ".ogg", filepath.Ext(seen), "the engine sees the detected extension")
	entries, err := os.ReadDir(tmp)
	require.NoError(t, err)
	assert.Empty(t, entries, "the recording is removed after transcription")
}

type engineFunc func(context.Context, string, transcribe.Options) (transcribe.Transcript, error)

func (f engineFunc) Transcribe(ctx context.Context, path string, opts transcribe.Options) (transcribe.Transcript, error) {
	return f(ctx, path, opts)
}

func TestRecordingExt_ContentTypeFallbacks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rec")
	require.NoError(t, os.WriteFile(path, []byte("????????????"), 0o600))
	for contentType, want := range map[string]string{
		"audio/aac":        ".m4a",
		"audio/x-m4a":      ".m4a",
		"audio/wav":        ".wav",
		"audio/x-wav":      ".wav",
		"audio/mpeg":       ".mp3",
		"audio/webm":       ".webm",
		"not a media type": ".webm",
	} {
		assert.Equal(t, want, recordingExt(path, contentType), contentType)
	}
	// An unreadable file falls back to the Content-Type alone.
	assert.Equal(t, ".ogg", recordingExt(filepath.Join(t.TempDir(), "missing"), "audio/ogg"))
	// A file shorter than the magic-byte window still uses its magic bytes.
	short := filepath.Join(t.TempDir(), "short")
	require.NoError(t, os.WriteFile(short, []byte("ID3"), 0o600))
	assert.Equal(t, ".mp3", recordingExt(short, "audio/webm"))
}

func useTranscriptionDeps(t *testing.T, detect func(string) transcribe.Status, install func(context.Context, string, func(string)) (transcribe.Status, error)) {
	t.Helper()
	origDetect, origInstall := detectTranscription, installTranscription
	if detect != nil {
		detectTranscription = detect
	}
	if install != nil {
		installTranscription = install
	}
	t.Cleanup(func() { detectTranscription, installTranscription = origDetect, origInstall })
}

func readyStatus(model string) transcribe.Status {
	return transcribe.Status{
		Whisper: transcribe.Binary{Name: "whisper-cli", Installed: true, Path: "/bin/whisper-cli"},
		FFmpeg:  transcribe.Binary{Name: "ffmpeg", Installed: true, Path: "/bin/ffmpeg"},
		Model:   transcribe.Model{Name: model, Downloaded: true, Path: "/models/" + model},
	}
}

func callTranscription(t *testing.T, s *Server, method string) (int, transcriptionStatus) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.transcriptionHandler(rec, httptest.NewRequest(method, "/api/transcription", nil))
	var resp transcriptionStatus
	if rec.Code == http.StatusOK || rec.Code == http.StatusConflict {
		require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
		require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	}
	return rec.Code, resp
}

func TestTranscriptionHandler_GetReady(t *testing.T) {
	var gotModel string
	useTranscriptionDeps(t, func(model string) transcribe.Status {
		gotModel = model
		return readyStatus(model)
	}, func(context.Context, string, func(string)) (transcribe.Status, error) {
		t.Fatal("GET must not install anything")
		return transcribe.Status{}, nil
	})
	s := &Server{cfg: &config.Config{Transcription: config.TranscriptionConfig{Model: "small", Language: "fr"}}}
	code, resp := callTranscription(t, s, http.MethodGet)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "small", gotModel)
	assert.True(t, resp.Enabled)
	assert.Equal(t, "local", resp.Engine)
	assert.Equal(t, "fr", resp.Language)
	assert.True(t, resp.Ready)
	assert.Empty(t, resp.Missing)
	assert.Empty(t, resp.Install, "a ready install needs no hint")
	assert.Equal(t, readyStatus("small"), resp.Status)
}

func TestTranscriptionHandler_PostInstalls(t *testing.T) {
	var gotModel string
	useTranscriptionDeps(t, func(string) transcribe.Status {
		t.Fatal("POST must install, not only detect")
		return transcribe.Status{}
	}, func(_ context.Context, model string, logf func(string)) (transcribe.Status, error) {
		gotModel = model
		logf("downloading model")
		logf("done")
		return readyStatus(model), nil
	})
	s := &Server{cfg: &config.Config{Transcription: config.TranscriptionConfig{Model: "tiny"}}}
	code, resp := callTranscription(t, s, http.MethodPost)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "tiny", gotModel)
	assert.Equal(t, []string{"downloading model", "done"}, resp.Log)
	assert.True(t, resp.Ready)
	assert.Empty(t, resp.Error)
	assert.Empty(t, resp.Install)
}

func TestTranscriptionHandler_PostInstallError(t *testing.T) {
	partial := readyStatus("tiny")
	partial.FFmpeg.Installed = false
	useTranscriptionDeps(t, nil, func(_ context.Context, _ string, logf func(string)) (transcribe.Status, error) {
		logf("installing ffmpeg")
		return partial, errors.New("brew install ffmpeg: exit status 1")
	})
	code, resp := callTranscription(t, &Server{cfg: &config.Config{}}, http.MethodPost)
	require.Equal(t, http.StatusOK, code)
	assert.Equal(t, "brew install ffmpeg: exit status 1", resp.Error)
	assert.Equal(t, []string{"installing ffmpeg"}, resp.Log)
	assert.False(t, resp.Ready)
	assert.Equal(t, []string{"ffmpeg"}, resp.Missing)
	assert.Equal(t, transcribe.InstallHint, resp.Install)
}

func TestTranscriptionHandler_RejectsOtherMethods(t *testing.T) {
	useTranscriptionDeps(t, func(string) transcribe.Status {
		t.Fatal("a rejected method must not detect")
		return transcribe.Status{}
	}, func(context.Context, string, func(string)) (transcribe.Status, error) {
		t.Fatal("a rejected method must not install")
		return transcribe.Status{}, nil
	})
	for _, method := range []string{http.MethodPut, http.MethodDelete, http.MethodPatch} {
		code, _ := callTranscription(t, &Server{cfg: &config.Config{}}, method)
		assert.Equal(t, http.StatusMethodNotAllowed, code, method)
	}
}

func TestTranscriptionHandler_ConcurrentInstallConflicts(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var installs int
	useTranscriptionDeps(t, nil, func(context.Context, string, func(string)) (transcribe.Status, error) {
		installs++
		close(started)
		<-release
		return readyStatus("base.en"), nil
	})
	s := &Server{cfg: &config.Config{}}
	first := make(chan int, 1)
	go func() {
		rec := httptest.NewRecorder()
		s.transcriptionHandler(rec, httptest.NewRequest(http.MethodPost, "/api/transcription", nil))
		first <- rec.Code
	}()
	<-started

	code, resp := callTranscription(t, s, http.MethodPost)
	assert.Equal(t, http.StatusConflict, code)
	assert.Equal(t, "an install is already running", resp.Error)

	close(release)
	assert.Equal(t, http.StatusOK, <-first)
	assert.Equal(t, 1, installs)

	// The lock is released once the first install finishes.
	useTranscriptionDeps(t, nil, func(context.Context, string, func(string)) (transcribe.Status, error) {
		return readyStatus("base.en"), nil
	})
	code, _ = callTranscription(t, s, http.MethodPost)
	assert.Equal(t, http.StatusOK, code)
}

func TestMediaHandler_ServesAudioContent(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	dir := store.IncomingMediaDir("telegram")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	voice := filepath.Join(dir, "2_voice.ogg")
	require.NoError(t, os.WriteFile(voice, []byte("OggS-audio-bytes"), 0o600))

	rec := httptest.NewRecorder()
	mediaHandler(rec, httptest.NewRequest(http.MethodGet, "/api/media?path="+url.QueryEscape(voice), nil))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "OggS-audio-bytes", rec.Body.String())
	assert.Equal(t, "audio/ogg", rec.Header().Get("Content-Type"))
}

func TestMediaHandler_NotFoundCases(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	root := store.MediaDir()

	get := func(path string) int {
		rec := httptest.NewRecorder()
		mediaHandler(rec, httptest.NewRequest(http.MethodGet, "/api/media?path="+url.QueryEscape(path), nil))
		return rec.Code
	}

	// The media directory does not exist yet.
	assert.Equal(t, http.StatusNotFound, get(filepath.Join(root, "missing.ogg")))

	dir := store.IncomingMediaDir("signal")
	require.NoError(t, os.MkdirAll(dir, 0o700))
	// A directory whose name looks like audio is not served.
	audioDir := filepath.Join(dir, "folder.ogg")
	require.NoError(t, os.MkdirAll(audioDir, 0o700))

	for name, path := range map[string]string{
		"empty path":       "",
		"media root":       root,
		"missing file":     filepath.Join(dir, "missing.ogg"),
		"directory":        audioDir,
		"relative escape":  filepath.Join("..", "x.ogg"),
		"dot-dot in name":  filepath.Join(dir, "a..b.ogg"),
		"non-audio file":   filepath.Join(dir, "notes.txt"),
		"traversal to etc": filepath.Join(root, "..", "..", "etc", "passwd.mp3"),
	} {
		assert.Equal(t, http.StatusNotFound, get(path), name)
	}
}
