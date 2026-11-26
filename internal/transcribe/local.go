package transcribe

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// runFunc runs a command and returns its combined output.
type runFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Local transcribes with whisper.cpp's whisper-cli, converting input to
// 16 kHz WAV with ffmpeg and keeping its loudest channel first.
type Local struct {
	Model string
	// Resolve finds the tools; it defaults to Detect for Model.
	Resolve func() Status
	run     runFunc
}

// NewLocal returns a whisper-cli engine for model.
func NewLocal(model string) *Local {
	return &Local{Model: model}
}

func (l *Local) status() Status {
	if l.Resolve != nil {
		return l.Resolve()
	}
	return Detect(l.Model)
}

// Transcribe implements Engine.
func (l *Local) Transcribe(ctx context.Context, audioPath string, opts Options) (Transcript, error) {
	st := l.status()
	if missing := st.Missing(); len(missing) > 0 {
		return Transcript{}, &UnavailableError{Missing: missing}
	}
	run := l.run
	if run == nil {
		run = runCommand
	}
	dir, err := os.MkdirTemp("", "aviary-transcribe-")
	if err != nil {
		return Transcript{}, err
	}
	defer os.RemoveAll(dir) //nolint:errcheck

	converted := filepath.Join(dir, "converted.wav")
	if out, err := run(ctx, st.FFmpeg.Path, ffmpegArgs(audioPath, converted)...); err != nil {
		return Transcript{}, fmt.Errorf("converting audio with ffmpeg: %w: %s", err, firstLine(out))
	}
	wav := filepath.Join(dir, "audio.wav")
	level, err := prepareSpeech(converted, wav)
	if err != nil {
		return Transcript{}, fmt.Errorf("converting audio with ffmpeg: %w", err)
	}
	inputSize := int64(-1)
	if info, err := os.Stat(audioPath); err == nil {
		inputSize = info.Size()
	}
	slog.Debug("transcribe: converted audio", "component", "transcribe", "input", filepath.Base(audioPath), "input_bytes", inputSize,
		"duration", level.Duration, "channels", level.Channels, "channel", level.Channel,
		"peak_dbfs", roundDB(dBFS(level.Peak)), "rms_dbfs", roundDB(dBFS(level.RMS)), "max_peak_dbfs", roundDB(dBFS(level.MaxPeak)),
		"channel_peaks_dbfs", dBFSList(level.ChannelPeaks), "channel_rms_dbfs", dBFSList(level.ChannelRMS),
		"silence_peak_dbfs", roundDB(dBFS(silencePeak)), "gain_db", roundDB(dBFS(level.Gain)))
	peak := roundDB(dBFS(level.MaxPeak))
	if reason := level.silent(); reason != "" {
		slog.Debug("transcribe: no speech detected", "component", "transcribe", "reason", reason, "duration", level.Duration)
		return Transcript{Duration: level.Duration, NoSpeech: true, NoSignal: level.noSignal(), PeakDBFS: &peak}, nil
	}
	prefix := filepath.Join(dir, "transcript")
	if out, err := run(ctx, st.Whisper.Path, whisperArgs(st.Model.Path, wav, prefix, opts.Language)...); err != nil {
		return Transcript{}, fmt.Errorf("running whisper-cli: %w: %s", err, firstLine(out))
	}
	data, err := os.ReadFile(prefix + ".json")
	if err != nil {
		return Transcript{}, fmt.Errorf("reading whisper-cli output: %w", err)
	}
	t, err := parseWhisperJSON(data)
	if err != nil {
		return Transcript{}, err
	}
	t.Duration = level.Duration
	t.PeakDBFS = &peak
	slog.Debug("transcribe: whisper output", "component", "transcribe", "text", t.Text, "language", t.Language)
	if isHallucination(t.Text) {
		slog.Debug("transcribe: no speech detected", "component", "transcribe", "reason", "whisper returned a known silence hallucination", "text", t.Text)
		return Transcript{Language: t.Language, Duration: t.Duration, NoSpeech: true, PeakDBFS: &peak}, nil
	}
	return t, nil
}

func dBFSList(levels []float64) []float64 {
	out := make([]float64, len(levels))
	for i, l := range levels {
		out[i] = roundDB(dBFS(l))
	}
	return out
}

func roundDB(db float64) float64 {
	if math.IsInf(db, 0) {
		return -120
	}
	return math.Round(db*10) / 10
}

// ffmpegArgs converts any input, at any sample rate, to 16 kHz float WAV
// that keeps every channel, so prepareSpeech can pick the one with the mic
// on it without losing quiet speech to 16-bit rounding.
func ffmpegArgs(in, out string) []string {
	return []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", in, "-ar", "16000", "-c:a", "pcm_f32le", out}
}

func whisperArgs(model, wav, prefix, language string) []string {
	if language == "" {
		language = "auto"
	}
	return []string{"-m", model, "-f", wav, "-l", language, "-np", "-nt", "-oj", "-of", prefix}
}

type whisperOutput struct {
	Result struct {
		Language string `json:"language"`
	} `json:"result"`
	Transcription []struct {
		Text string `json:"text"`
	} `json:"transcription"`
}

func parseWhisperJSON(data []byte) (Transcript, error) {
	var out whisperOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return Transcript{}, fmt.Errorf("parsing whisper-cli output: %w", err)
	}
	parts := make([]string, 0, len(out.Transcription))
	for _, seg := range out.Transcription {
		if text := strings.TrimSpace(seg.Text); text != "" && !isNonSpeechMarker(text) {
			parts = append(parts, text)
		}
	}
	return Transcript{Text: strings.Join(parts, " "), Language: out.Result.Language}, nil
}

func firstLine(out []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line
}
