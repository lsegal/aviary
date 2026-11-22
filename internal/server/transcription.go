package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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
	ext := ".webm"
	if exts := strings.TrimSpace(r.Header.Get("Content-Type")); strings.Contains(exts, "mp4") {
		ext = ".m4a"
	} else if strings.Contains(exts, "ogg") {
		ext = ".ogg"
	}
	tmp, err := os.CreateTemp("", "aviary-recording-*"+ext)
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
	t, err := engine.Transcribe(r.Context(), tmp.Name(), transcribe.Options{Language: settings.Language})
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
	writeTranscriptionJSON(w, http.StatusOK, transcribeResponse{Text: t.Text, Language: t.Language, DurationMS: t.Duration.Milliseconds()})
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
		resp.Status = transcribe.Detect(settings.Model)
	case http.MethodPost:
		if !transcriptionInstallMu.TryLock() {
			writeTranscriptionJSON(w, http.StatusConflict, transcriptionStatus{Error: "an install is already running"})
			return
		}
		defer transcriptionInstallMu.Unlock()
		st, err := transcribe.Install(r.Context(), settings.Model, func(line string) {
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
	path := filepath.Clean(r.URL.Query().Get("path"))
	root := filepath.Clean(store.MediaDir())
	rel, err := filepath.Rel(root, path)
	if err != nil || !filepath.IsAbs(path) || rel == "." || strings.HasPrefix(rel, "..") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if !channels.LooksLikeAudioFile(path) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, path)
}
