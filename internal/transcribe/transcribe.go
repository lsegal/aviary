// Package transcribe turns recorded speech into text. Engines implement
// Engine; Local runs whisper.cpp's whisper-cli, and a provider API can be
// added as another Engine.
package transcribe

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/config"
)

// DefaultModel is the whisper.cpp model used when none is configured.
const DefaultModel = "base.en"

// ModelEnv names the environment variable that sets the model used when
// aviary.yaml configures none. The Docker image points it at its bundled
// model file.
const ModelEnv = "AVIARY_CONFIG_TRANSCRIPTION_MODEL"

// defaultModel is the model used when none is configured: ModelEnv when set,
// otherwise DefaultModel.
func defaultModel() string {
	if model := strings.TrimSpace(os.Getenv(ModelEnv)); model != "" {
		return model
	}
	return DefaultModel
}

// InstallHint tells a user how to install what transcription needs.
const InstallHint = "run `aviary deps install`, or Install on the System > Tools page"

// ErrEngineUnavailable reports that the engine, its model, or ffmpeg is
// missing. Errors from Transcribe match it with errors.Is.
var ErrEngineUnavailable = errors.New("transcription engine unavailable")

// ErrDisabled reports that transcription is turned off in aviary.yaml.
var ErrDisabled = errors.New("transcription is disabled in aviary.yaml")

// UnavailableError names what is missing. It matches ErrEngineUnavailable.
type UnavailableError struct {
	Missing []string
}

func (e *UnavailableError) Error() string {
	return fmt.Sprintf("transcription unavailable: %s missing; %s", strings.Join(e.Missing, ", "), InstallHint)
}

// Is makes errors.Is(err, ErrEngineUnavailable) true.
func (e *UnavailableError) Is(target error) bool { return target == ErrEngineUnavailable }

// Options tune a single transcription.
type Options struct {
	// Language is "auto" or an ISO 639-1 code.
	Language string
}

// Transcript is the result of a transcription.
type Transcript struct {
	Text     string        `json:"text"`
	Language string        `json:"language,omitempty"`
	Duration time.Duration `json:"duration"`
	// NoSpeech is set when the audio is silent or too short, or Whisper
	// returned only one of its stock outputs for silence. Text is empty.
	NoSpeech bool `json:"no_speech,omitempty"`
	// NoSignal is set with NoSpeech when the recording's peak across every
	// channel was near zero, rather than sound with no recognizable words.
	NoSignal bool `json:"no_signal,omitempty"`
	// PeakDBFS is the loudest sample across every channel, when the engine
	// measures it.
	PeakDBFS *float64 `json:"peak_dbfs,omitempty"`
}

// Engine transcribes an audio file.
type Engine interface {
	Transcribe(ctx context.Context, audioPath string, opts Options) (Transcript, error)
}

// Settings is a TranscriptionConfig with defaults applied.
type Settings struct {
	Enabled  bool
	Engine   string
	Model    string
	Language string
}

// SettingsFrom applies defaults to cfg.
func SettingsFrom(cfg config.TranscriptionConfig) Settings {
	s := Settings{
		Enabled:  config.BoolOr(cfg.Enabled, true),
		Engine:   strings.TrimSpace(cfg.Engine),
		Model:    strings.TrimSpace(cfg.Model),
		Language: strings.ToLower(strings.TrimSpace(cfg.Language)),
	}
	if s.Engine == "" {
		s.Engine = "local"
	}
	if s.Model == "" {
		s.Model = defaultModel()
	}
	if s.Language == "" {
		s.Language = "auto"
	}
	return s
}

// New returns the engine for cfg, or ErrDisabled. Results are cached by
// audio content, so the same recording is never transcribed twice.
func New(cfg config.TranscriptionConfig) (Engine, Settings, error) {
	s := SettingsFrom(cfg)
	if !s.Enabled {
		return nil, s, ErrDisabled
	}
	if s.Engine != "local" {
		return nil, s, fmt.Errorf("unknown transcription engine %q", s.Engine)
	}
	return NewCache(NewLocal(s.Model), CacheDir(), s.Model), s, nil
}

// FormatDuration renders d as M:SS, for example "0:12".
func FormatDuration(d time.Duration) string {
	secs := int(d.Round(time.Second) / time.Second)
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}
