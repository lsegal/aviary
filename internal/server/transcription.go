package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
	"github.com/lsegal/aviary/internal/transcribe"
)

// maxRecordingBytes caps a composer recording uploaded for transcription.
const maxRecordingBytes = 50 << 20

// newTranscriber is replaced in tests with a fake engine.
var newTranscriber = transcribe.New

// detectTranscription and installTranscription are replaced in tests so
// nothing real is probed or installed.
var (
	detectTranscription  = transcribe.Detect
	installTranscription = transcribe.Install
)

func (s *Server) transcriptionConfig() config.TranscriptionConfig {
	if s.cfg == nil {
		return config.TranscriptionConfig{}
	}
	return s.cfg.Transcription
}

// transcribeAttachments transcribes a channel message's audio attachments.
// With transcription disabled, attachments stay plain files.
func (s *Server) transcribeAttachments(ctx context.Context, attachments []channels.Attachment) []channels.Attachment {
	hasAudio := false
	for _, att := range attachments {
		hasAudio = hasAudio || (att.Path != "" && att.IsAudio())
	}
	if !hasAudio {
		return attachments
	}
	engine, settings, err := newTranscriber(s.transcriptionConfig())
	if err != nil {
		if !errors.Is(err, transcribe.ErrDisabled) {
			slog.Warn("server: transcription unavailable", "err", err)
		}
		return attachments
	}
	return channels.TranscribeAttachments(ctx, engine, transcribe.Options{Language: settings.Language}, attachments)
}

type transcribeResponse struct {
	Text       string   `json:"text,omitempty"`
	Language   string   `json:"language,omitempty"`
	DurationMS int64    `json:"duration_ms,omitempty"`
	NoSpeech   bool     `json:"no_speech,omitempty"`
	NoSignal   bool     `json:"no_signal,omitempty"`
	PeakDBFS   *float64 `json:"peak_dbfs,omitempty"`
	Error      string   `json:"error,omitempty"`
	Missing    []string `json:"missing,omitempty"`
	Install    string   `json:"install,omitempty"`
}

func writeTranscriptionJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// transcribeHandler handles POST /api/transcribe: the body is a recording,
// and the response is its transcript. Nothing is sent to an agent.
func (s *Server) transcribeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	engine, settings, err := newTranscriber(s.transcriptionConfig())
	if err != nil {
		writeTranscriptionJSON(w, http.StatusServiceUnavailable, transcribeResponse{Error: err.Error()})
		return
	}
	tmp, err := os.CreateTemp("", "aviary-recording-*")
	if err != nil {
		writeTranscriptionJSON(w, http.StatusInternalServerError, transcribeResponse{Error: err.Error()})
		return
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck
	n, err := io.Copy(tmp, io.LimitReader(r.Body, maxRecordingBytes+1))
	_ = tmp.Close()
	switch {
	case err != nil:
		writeTranscriptionJSON(w, http.StatusBadRequest, transcribeResponse{Error: "reading recording: " + err.Error()})
		return
	case n == 0:
		writeTranscriptionJSON(w, http.StatusBadRequest, transcribeResponse{Error: "the recording is empty"})
		return
	case n > maxRecordingBytes:
		writeTranscriptionJSON(w, http.StatusRequestEntityTooLarge, transcribeResponse{Error: "the recording is larger than 50 MB"})
		return
	}
	contentType := r.Header.Get("Content-Type")
	ext := recordingExt(tmp.Name(), contentType)
	path := tmp.Name() + ext
	if err := os.Rename(tmp.Name(), path); err != nil {
		writeTranscriptionJSON(w, http.StatusInternalServerError, transcribeResponse{Error: err.Error()})
		return
	}
	defer os.Remove(path) //nolint:errcheck
	slog.Debug("server: transcribing recording", "component", "transcribe", "content_type", contentType, "bytes", n, "format", ext)
	t, err := engine.Transcribe(r.Context(), path, transcribe.Options{Language: settings.Language})
	if err != nil {
		resp := transcribeResponse{Error: err.Error()}
		var unavailable *transcribe.UnavailableError
		if errors.As(err, &unavailable) {
			resp.Missing = unavailable.Missing
			resp.Install = transcribe.InstallHint
			writeTranscriptionJSON(w, http.StatusServiceUnavailable, resp)
			return
		}
		writeTranscriptionJSON(w, http.StatusInternalServerError, resp)
		return
	}
	slog.Debug("server: transcribed recording", "component", "transcribe", "bytes", n, "duration", t.Duration, "no_speech", t.NoSpeech, "no_signal", t.NoSignal, "peak_dbfs", t.PeakDBFS, "text", t.Text)
	writeTranscriptionJSON(w, http.StatusOK, transcribeResponse{Text: t.Text, Language: t.Language, DurationMS: t.Duration.Milliseconds(), NoSpeech: t.NoSpeech, NoSignal: t.NoSignal, PeakDBFS: t.PeakDBFS})
}

// recordingExt picks the file extension ffmpeg should see for a recording,
// from its magic bytes first and its Content-Type (parameters included)
// second. Browsers send WebM/Opus (Chrome, Firefox), MP4/AAC (Safari), or Ogg.
func recordingExt(path, contentType string) string {
	head := make([]byte, 12)
	if f, err := os.Open(path); err == nil {
		n, _ := io.ReadFull(f, head)
		head = head[:n]
		_ = f.Close()
	}
	switch {
	case bytes.HasPrefix(head, []byte{0x1A, 0x45, 0xDF, 0xA3}):
		return ".webm"
	case bytes.HasPrefix(head, []byte("OggS")):
		return ".ogg"
	case len(head) >= 8 && string(head[4:8]) == "ftyp":
		return ".m4a"
	case bytes.HasPrefix(head, []byte("RIFF")):
		return ".wav"
	case bytes.HasPrefix(head, []byte("ID3")):
		return ".mp3"
	}
	mediaType, _, _ := mime.ParseMediaType(contentType)
	switch {
	case strings.Contains(mediaType, "mp4"), strings.Contains(mediaType, "aac"), strings.Contains(mediaType, "m4a"):
		return ".m4a"
	case strings.Contains(mediaType, "ogg"):
		return ".ogg"
	case strings.Contains(mediaType, "wav"):
		return ".wav"
	case strings.Contains(mediaType, "mpeg"):
		return ".mp3"
	}
	return ".webm"
}

type transcriptionStatus struct {
	Enabled  bool              `json:"enabled"`
	Engine   string            `json:"engine"`
	Language string            `json:"language"`
	Ready    bool              `json:"ready"`
	Missing  []string          `json:"missing,omitempty"`
	Install  string            `json:"install,omitempty"`
	Status   transcribe.Status `json:"status"`
	Error    string            `json:"error,omitempty"`
	Log      []string          `json:"log,omitempty"`
}

var transcriptionInstallMu sync.Mutex

// transcriptionHandler handles /api/transcription: GET reports what local
// transcription needs, POST installs whatever is missing.
func (s *Server) transcriptionHandler(w http.ResponseWriter, r *http.Request) {
	settings := transcribe.SettingsFrom(s.transcriptionConfig())
	resp := transcriptionStatus{Enabled: settings.Enabled, Engine: settings.Engine, Language: settings.Language}
	switch r.Method {
	case http.MethodGet:
		resp.Status = detectTranscription(settings.Model)
	case http.MethodPost:
		if !transcriptionInstallMu.TryLock() {
			writeTranscriptionJSON(w, http.StatusConflict, transcriptionStatus{Error: "an install is already running"})
			return
		}
		defer transcriptionInstallMu.Unlock()
		st, err := installTranscription(r.Context(), settings.Model, func(line string) {
			resp.Log = append(resp.Log, line)
		})
		resp.Status = st
		if err != nil {
			resp.Error = err.Error()
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	resp.Missing = resp.Status.Missing()
	resp.Ready = len(resp.Missing) == 0
	if !resp.Ready {
		resp.Install = transcribe.InstallHint
	}
	writeTranscriptionJSON(w, http.StatusOK, resp)
}

// mediaHandler handles GET /api/media?path=…, serving saved incoming audio
// so chat history can play voice messages. Only audio files under the
// media directory are served.
func mediaHandler(w http.ResponseWriter, r *http.Request) {
	root := filepath.Clean(store.MediaDir())
	rel, err := filepath.Rel(root, filepath.Clean(r.URL.Query().Get("path")))
	if err != nil || rel == "." || strings.Contains(rel, "..") || filepath.IsAbs(rel) || !channels.LooksLikeAudioFile(rel) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer dir.Close() //nolint:errcheck
	f, err := dir.Open(rel)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	defer f.Close() //nolint:errcheck
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
