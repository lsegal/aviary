package transcribe

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runFunc runs a command and returns its combined output.
type runFunc func(ctx context.Context, name string, args ...string) ([]byte, error)

func runCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Local transcribes with whisper.cpp's whisper-cli, converting input to
// 16 kHz mono WAV with ffmpeg first.
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

	wav := filepath.Join(dir, "audio.wav")
	if out, err := run(ctx, st.FFmpeg.Path, ffmpegArgs(audioPath, wav)...); err != nil {
		return Transcript{}, fmt.Errorf("converting audio with ffmpeg: %w: %s", err, firstLine(out))
	}
	info, err := os.Stat(wav)
	if err != nil {
		return Transcript{}, fmt.Errorf("converting audio with ffmpeg: %w", err)
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
	t.Duration = wavDuration(info.Size())
	return t, nil
}

// ffmpegArgs converts any input whisper-cli can't read to 16 kHz mono
// 16-bit WAV.
func ffmpegArgs(in, out string) []string {
	return []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", in, "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le", out}
}

func whisperArgs(model, wav, prefix, language string) []string {
	if language == "" {
		language = "auto"
	}
	return []string{"-m", model, "-f", wav, "-l", language, "-np", "-nt", "-oj", "-of", prefix}
}

// wavDuration is the length of 16 kHz mono 16-bit PCM with a 44-byte header.
func wavDuration(size int64) time.Duration {
	const header, bytesPerSecond = 44, 16000 * 2
	if size <= header {
		return 0
	}
	return time.Duration(size-header) * time.Second / bytesPerSecond
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
		if text := strings.TrimSpace(seg.Text); text != "" {
			parts = append(parts, text)
		}
	}
	return Transcript{Text: strings.Join(parts, " "), Language: out.Result.Language}, nil
}

func firstLine(out []byte) string {
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line
}
