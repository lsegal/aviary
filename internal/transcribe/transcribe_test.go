package transcribe

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
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

// writeWAV writes 16 kHz mono 16-bit PCM WAV, with a LIST chunk before the
// data chunk as ffmpeg writes it.
func writeWAV(t *testing.T, path string, samples []int16) {
	t.Helper()
	list := []byte("LIST\x04\x00\x00\x00INFO")
	var buf bytes.Buffer
	buf.WriteString("RIFF")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(4+24+len(list)+8+2*len(samples)))
	buf.WriteString("WAVEfmt ")
	for _, v := range []any{uint32(16), uint16(1), uint16(1), uint32(16000), uint32(32000), uint16(2), uint16(16)} {
		_ = binary.Write(&buf, binary.LittleEndian, v)
	}
	buf.Write(list)
	buf.WriteString("data")
	_ = binary.Write(&buf, binary.LittleEndian, uint32(2*len(samples)))
	_ = binary.Write(&buf, binary.LittleEndian, samples)
	require.NoError(t, os.WriteFile(path, buf.Bytes(), 0o600))
}

// tone is d of a 440 Hz sine at amplitude (0..1 of full scale).
func tone(d time.Duration, amplitude float64) []int16 {
	out := make([]int16, int(d*16000/time.Second))
	for i := range out {
		out[i] = int16(amplitude * 32767 * math.Sin(2*math.Pi*440*float64(i)/16000))
	}
	return out
}

// fakeTools stands in for ffmpeg and whisper-cli: ffmpeg writes samples as
// 16 kHz mono WAV, whisper-cli writes whisperJSON as its output.
func fakeTools(t *testing.T, calls *[][]string, samples []int16, whisperJSON string) runFunc {
	return func(_ context.Context, name string, args ...string) ([]byte, error) {
		*calls = append(*calls, append([]string{name}, args...))
		switch name {
		case "/bin/ffmpeg":
			writeWAV(t, args[len(args)-1], samples)
			return nil, nil
		case "/bin/whisper-cli":
			prefix := args[slices.Index(args, "-of")+1]
			return nil, os.WriteFile(prefix+".json", []byte(whisperJSON), 0o600)
		}
		t.Fatalf("unexpected command %s", name)
		return nil, nil
	}
}

const helloWorldJSON = `{"result":{"language":"en"},"transcription":[{"text":" Hello"},{"text":" world. "}]}`

func TestLocal_ConvertsWithFFmpegThenRunsWhisper(t *testing.T) {
	for _, input := range []string{"note.ogg", "note.opus", "clip.m4a", "clip.aac", "rec.webm", "song.mp3", "raw.wav"} {
		t.Run(input, func(t *testing.T) {
			var calls [][]string
			l := &Local{Model: "base.en", Resolve: readyStatus, run: fakeTools(t, &calls, tone(12*time.Second, 0.5), helloWorldJSON)}
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

func TestLocal_SilentOrShortAudioIsNoSpeech(t *testing.T) {
	for name, samples := range map[string][]int16{
		"silent":     make([]int16, 9*16000),
		"faint hiss": tone(9*time.Second, 0.001),
		"too short":  tone(300*time.Millisecond, 0.5),
		"empty":      nil,
	} {
		t.Run(name, func(t *testing.T) {
			var calls [][]string
			l := &Local{Model: "base.en", Resolve: readyStatus, run: fakeTools(t, &calls, samples, `{"transcription":[{"text":" you"}]}`)}
			got, err := l.Transcribe(context.Background(), "/in/rec.webm", Options{})
			require.NoError(t, err)
			assert.True(t, got.NoSpeech)
			assert.Empty(t, got.Text)
			require.Len(t, calls, 1, "whisper-cli should not run on silence")
		})
	}
}

func TestLocal_WhisperSilenceHallucinationIsNoSpeech(t *testing.T) {
	for _, text := range []string{" you", " You.", " Thank you.", "[BLANK_AUDIO]", " Thanks for watching!"} {
		t.Run(text, func(t *testing.T) {
			var calls [][]string
			out := `{"result":{"language":"en"},"transcription":[{"text":` + strconv.Quote(text) + `}]}`
			l := &Local{Model: "base.en", Resolve: readyStatus, run: fakeTools(t, &calls, tone(9*time.Second, 0.3), out)}
			got, err := l.Transcribe(context.Background(), "/in/rec.webm", Options{})
			require.NoError(t, err)
			assert.Equal(t, Transcript{Language: "en", Duration: 9 * time.Second, NoSpeech: true}, got)
		})
	}
}

func TestLocal_KeepsRealSpeechAroundNonSpeechMarkers(t *testing.T) {
	var calls [][]string
	out := `{"result":{"language":"en"},"transcription":[{"text":" [BLANK_AUDIO]"},{"text":" Thank you,"},{"text":" see you soon."},{"text":" (music)"}]}`
	l := &Local{Model: "base.en", Resolve: readyStatus, run: fakeTools(t, &calls, tone(4*time.Second, 0.3), out)}
	got, err := l.Transcribe(context.Background(), "/in/rec.webm", Options{})
	require.NoError(t, err)
	assert.Equal(t, Transcript{Text: "Thank you, see you soon.", Language: "en", Duration: 4 * time.Second}, got)
}

func TestReadWAVLevel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.wav")
	writeWAV(t, path, tone(2*time.Second, 0.5))
	level, err := readWAVLevel(path)
	require.NoError(t, err)
	assert.Equal(t, 2*time.Second, level.Duration)
	assert.InDelta(t, 0.5, level.Peak, 0.01)
	assert.InDelta(t, 0.5/math.Sqrt2, level.RMS, 0.01)

	require.NoError(t, os.WriteFile(path, []byte("not audio"), 0o600))
	_, err = readWAVLevel(path)
	assert.Error(t, err)
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

func TestSettingsFrom_ModelEnv(t *testing.T) {
	t.Setenv(ModelEnv, "/opt/aviary/models/ggml-base.en.bin")
	assert.Equal(t, "/opt/aviary/models/ggml-base.en.bin", SettingsFrom(config.TranscriptionConfig{}).Model)
	assert.Equal(t, "small", SettingsFrom(config.TranscriptionConfig{Model: "small"}).Model)
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

func TestDetect_ModelEnv(t *testing.T) {
	useTempDataDir(t)
	stubPath(t, "whisper-cli", "ffmpeg")
	path := filepath.Join(t.TempDir(), "ggml-base.en.bin")
	require.NoError(t, os.WriteFile(path, []byte("ggml"), 0o600))
	t.Setenv(ModelEnv, path)
	st := Detect("")
	assert.True(t, st.Ready())
	assert.Equal(t, path, st.Model.Path)
	assert.Equal(t, "small", Detect("small").Model.Name)
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
