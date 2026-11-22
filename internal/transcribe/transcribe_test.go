package transcribe

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

func useTempDataDir(t *testing.T) {
	t.Helper()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
}

// stubPath makes lookPath find only the given names.
func stubPath(t *testing.T, found ...string) {
	t.Helper()
	orig := lookPath
	lookPath = func(name string) (string, error) {
		if slices.Contains(found, name) {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	t.Cleanup(func() { lookPath = orig })
}

func writeModel(t *testing.T, model string) {
	t.Helper()
	path := ModelPath(model)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte("ggml"), 0o600))
}

func readyStatus() Status {
	return Status{
		Whisper: Binary{Name: "whisper-cli", Installed: true, Path: "/bin/whisper-cli"},
		FFmpeg:  Binary{Name: "ffmpeg", Installed: true, Path: "/bin/ffmpeg"},
		Model:   Model{Name: "base.en", Downloaded: true, Path: "/models/ggml-base.en.bin"},
	}
}

// fakeTools stands in for ffmpeg and whisper-cli: ffmpeg writes 12 s of
// 16 kHz mono WAV, whisper-cli writes its JSON output.
func fakeTools(t *testing.T, calls *[][]string) runFunc {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, append([]string{name}, args...))
		switch name {
		case "/bin/ffmpeg":
			out := args[len(args)-1]
			return nil, os.WriteFile(out, make([]byte, 44+12*32000), 0o600)
		case "/bin/whisper-cli":
			prefix := args[slices.Index(args, "-of")+1]
			return nil, os.WriteFile(prefix+".json", []byte(`{"result":{"language":"en"},"transcription":[{"text":" Hello"},{"text":" world. "}]}`), 0o600)
		}
		t.Fatalf("unexpected command %s", name)
		return nil, nil
	}
}

func TestLocal_ConvertsWithFFmpegThenRunsWhisper(t *testing.T) {
	for _, input := range []string{"note.ogg", "note.opus", "clip.m4a", "clip.aac", "rec.webm", "song.mp3", "raw.wav"} {
		t.Run(input, func(t *testing.T) {
			var calls [][]string
			l := &Local{Model: "base.en", Resolve: readyStatus, run: fakeTools(t, &calls)}
			got, err := l.Transcribe(context.Background(), "/in/"+input, Options{Language: "de"})
			require.NoError(t, err)
			assert.Equal(t, Transcript{Text: "Hello world.", Language: "en", Duration: 12 * time.Second}, got)

			require.Len(t, calls, 2)
			ffmpeg, whisper := calls[0], calls[1]
			assert.Equal(t, "/bin/ffmpeg", ffmpeg[0])
			assert.Equal(t, "/in/"+input, ffmpeg[slices.Index(ffmpeg, "-i")+1])
			assert.Equal(t, "16000", ffmpeg[slices.Index(ffmpeg, "-ar")+1])
			assert.Equal(t, "1", ffmpeg[slices.Index(ffmpeg, "-ac")+1])
			assert.Equal(t, "/bin/whisper-cli", whisper[0])
			assert.Equal(t, "/models/ggml-base.en.bin", whisper[slices.Index(whisper, "-m")+1])
			assert.Equal(t, ffmpeg[len(ffmpeg)-1], whisper[slices.Index(whisper, "-f")+1])
			assert.Equal(t, "de", whisper[slices.Index(whisper, "-l")+1])
		})
	}
}

func TestLocal_DefaultsLanguageToAuto(t *testing.T) {
	assert.Equal(t, "auto", whisperArgs("m", "w", "p", "")[5])
}

func TestLocal_MissingToolsReturnsUnavailable(t *testing.T) {
	st := Status{Whisper: Binary{Name: "whisper-cli"}, FFmpeg: Binary{Name: "ffmpeg"}, Model: Model{Name: "base.en"}}
	l := &Local{Resolve: func() Status { return st }, run: func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("nothing should run")
		return nil, nil
	}}
	_, err := l.Transcribe(context.Background(), "/in/a.ogg", Options{})
	require.ErrorIs(t, err, ErrEngineUnavailable)
	var unavailable *UnavailableError
	require.ErrorAs(t, err, &unavailable)
	assert.Equal(t, []string{"whisper-cli", "whisper model base.en", "ffmpeg"}, unavailable.Missing)
	assert.Contains(t, err.Error(), "aviary deps install")
}

func TestLocal_FFmpegFailureIsReported(t *testing.T) {
	l := &Local{Resolve: readyStatus, run: func(context.Context, string, ...string) ([]byte, error) {
		return []byte("Invalid data found when processing input\n"), errors.New("exit status 1")
	}}
	_, err := l.Transcribe(context.Background(), "/in/a.ogg", Options{})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrEngineUnavailable)
	assert.Contains(t, err.Error(), "Invalid data found")
}

func TestSettingsFrom(t *testing.T) {
	assert.Equal(t, Settings{Enabled: true, Engine: "local", Model: "base.en", Language: "auto"}, SettingsFrom(config.TranscriptionConfig{}))
	off := false
	assert.Equal(t, Settings{Enabled: false, Engine: "local", Model: "small", Language: "fr"},
		SettingsFrom(config.TranscriptionConfig{Enabled: &off, Model: "small", Language: "FR"}))
}

func TestNew_Disabled(t *testing.T) {
	off := false
	engine, _, err := New(config.TranscriptionConfig{Enabled: &off})
	assert.Nil(t, engine)
	assert.ErrorIs(t, err, ErrDisabled)
}

func TestNew_UsesConfiguredModel(t *testing.T) {
	useTempDataDir(t)
	engine, s, err := New(config.TranscriptionConfig{Model: "small.en", Language: "en"})
	require.NoError(t, err)
	assert.Equal(t, "en", s.Language)
	cache := engine.(*Cache)
	assert.Equal(t, "small.en", cache.Engine.(*Local).Model)
}

type countingEngine struct{ n int }

func (c *countingEngine) Transcribe(context.Context, string, Options) (Transcript, error) {
	c.n++
	return Transcript{Text: "hi", Duration: time.Second}, nil
}

func TestCache_ReusesStoredTranscript(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(t.TempDir(), "a.ogg")
	require.NoError(t, os.WriteFile(audio, []byte("voice"), 0o600))
	// A redelivered message is saved again under a new name.
	again := filepath.Join(t.TempDir(), "b.ogg")
	require.NoError(t, os.WriteFile(again, []byte("voice"), 0o600))

	inner := &countingEngine{}
	got, err := NewCache(inner, dir, "base.en").Transcribe(context.Background(), audio, Options{Language: "auto"})
	require.NoError(t, err)
	assert.Equal(t, "hi", got.Text)

	// A new Cache, as after a restart, reads the stored transcript.
	got, err = NewCache(inner, dir, "base.en").Transcribe(context.Background(), again, Options{Language: "auto"})
	require.NoError(t, err)
	assert.Equal(t, Transcript{Text: "hi", Duration: time.Second}, got)
	assert.Equal(t, 1, inner.n)

	// A different language or model is a different transcript.
	_, _ = NewCache(inner, dir, "base.en").Transcribe(context.Background(), audio, Options{Language: "de"})
	_, _ = NewCache(inner, dir, "small").Transcribe(context.Background(), audio, Options{Language: "auto"})
	assert.Equal(t, 3, inner.n)
}

func TestFormatDuration(t *testing.T) {
	assert.Equal(t, "0:12", FormatDuration(12*time.Second))
	assert.Equal(t, "1:05", FormatDuration(65*time.Second+400*time.Millisecond))
}

func TestDetect_Installed(t *testing.T) {
	useTempDataDir(t)
	stubPath(t, "whisper-cli", "ffmpeg")
	writeModel(t, "base.en")
	st := Detect("")
	assert.True(t, st.Ready())
	assert.Equal(t, "/usr/bin/whisper-cli", st.Whisper.Path)
	assert.Equal(t, "/usr/bin/ffmpeg", st.FFmpeg.Path)
	assert.Equal(t, "base.en", st.Model.Name)
	assert.Equal(t, filepath.Join(store.SubDir("deps"), "whisper", "ggml-base.en.bin"), st.Model.Path)
}

func TestDetect_Missing(t *testing.T) {
	useTempDataDir(t)
	stubPath(t)
	st := Detect("base.en")
	assert.False(t, st.Ready())
	assert.Equal(t, []string{"whisper-cli", "whisper model base.en", "ffmpeg"}, st.Missing())
}

func TestDetect_ModelMissing(t *testing.T) {
	useTempDataDir(t)
	stubPath(t, "whisper-cpp", "ffmpeg")
	st := Detect("small")
	assert.Equal(t, []string{"whisper model small"}, st.Missing())
	assert.Equal(t, "/usr/bin/whisper-cpp", st.Whisper.Path)
}

func TestDetect_PrefersAviaryDepsDir(t *testing.T) {
	useTempDataDir(t)
	stubPath(t, "whisper-cli")
	bin := filepath.Join(DepsDir(), "bin", "whisper-cli")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	require.NoError(t, os.MkdirAll(filepath.Dir(bin), 0o755))
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh"), 0o755))
	assert.Equal(t, bin, Detect("").Whisper.Path)
}

func TestDetect_ModelPath(t *testing.T) {
	useTempDataDir(t)
	path := filepath.Join(t.TempDir(), "custom.bin")
	require.NoError(t, os.WriteFile(path, []byte("ggml"), 0o600))
	st := Detect(path)
	assert.True(t, st.Model.Downloaded)
	assert.Equal(t, path, st.Model.Path)
}

func TestInstall_InstallsPackagesAndDownloadsModel(t *testing.T) {
	useTempDataDir(t)
	installed := []string{}
	stubPath(t)
	origInstall, origURL := packageInstall, modelBaseURL
	t.Cleanup(func() { packageInstall, modelBaseURL = origInstall, origURL })
	packageInstall = func(_ context.Context, pkgs []string, _ func(string)) error {
		installed = append(installed, pkgs...)
		lookPath = func(name string) (string, error) { return "/opt/bin/" + name, nil }
		return nil
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/ggml-base.en.bin", r.URL.Path)
		_, _ = w.Write([]byte("model-bytes"))
	}))
	defer srv.Close()
	modelBaseURL = srv.URL + "/"

	var log []string
	st, err := Install(context.Background(), "", func(line string) { log = append(log, line) })
	require.NoError(t, err)
	assert.True(t, st.Ready())
	assert.Equal(t, []string{"whisper-cpp", "ffmpeg"}, installed)
	data, err := os.ReadFile(st.Model.Path)
	require.NoError(t, err)
	assert.Equal(t, "model-bytes", string(data))
	assert.NotEmpty(t, log)
}

func TestInstall_NothingMissingDoesNothing(t *testing.T) {
	useTempDataDir(t)
	stubPath(t, "whisper-cli", "ffmpeg")
	writeModel(t, "base.en")
	origInstall := packageInstall
	t.Cleanup(func() { packageInstall = origInstall })
	packageInstall = func(context.Context, []string, func(string)) error {
		t.Fatal("nothing should be installed")
		return nil
	}
	st, err := Install(context.Background(), "base.en", nil)
	require.NoError(t, err)
	assert.True(t, st.Ready())
}

func TestInstall_ReportsFailures(t *testing.T) {
	useTempDataDir(t)
	stubPath(t, "whisper-cli", "ffmpeg")
	st, err := Install(context.Background(), "not-a-model", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not-a-model")
	assert.False(t, st.Model.Downloaded)
}
