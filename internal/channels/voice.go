package channels

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/lsegal/aviary/internal/transcribe"
)

// looksLikeAudio reports whether an attachment is a voice note or other
// audio, by MIME type or, when that is missing or generic, by extension.
func looksLikeAudio(contentType, name string) bool {
	contentType = normalizeContentType(contentType)
	if contentType != "" && contentType != "application/octet-stream" {
		switch {
		case strings.HasPrefix(contentType, "audio/"):
			return true
		case contentType == "video/webm", contentType == "application/ogg":
			// Some platforms label voice notes this way.
			return true
		}
		return false
	}
	switch strings.ToLower(filepath.Ext(strings.TrimSpace(name))) {
	case ".ogg", ".oga", ".opus", ".m4a", ".aac", ".mp3", ".wav", ".webm", ".flac", ".amr", ".caf":
		return true
	default:
		return false
	}
}

// IsAudio reports whether the attachment is audio.
func (a Attachment) IsAudio() bool {
	return looksLikeAudio(a.ContentType, a.Name)
}

// TranscribeAttachments transcribes every saved audio attachment with
// engine. A failure is recorded on the attachment, so the agent still gets
// the message and the file. A nil engine (transcription disabled) leaves
// attachments untouched.
func TranscribeAttachments(ctx context.Context, engine transcribe.Engine, opts transcribe.Options, attachments []Attachment) []Attachment {
	if engine == nil {
		return attachments
	}
	out := make([]Attachment, len(attachments))
	copy(out, attachments)
	for i := range out {
		att := &out[i]
		if att.Path == "" || !att.IsAudio() {
			continue
		}
		t, err := engine.Transcribe(ctx, att.Path, opts)
		if err != nil {
			slog.Warn("channels: transcription failed", "path", att.Path, "err", err)
			att.TranscriptErr = transcriptError(err)
			continue
		}
		att.Transcript = &t
	}
	return out
}

// transcriptError is a short reason for the agent. Tool output and temp
// paths stay in the log.
func transcriptError(err error) string {
	var unavailable *transcribe.UnavailableError
	switch {
	case errors.As(err, &unavailable):
		return unavailable.Error()
	case errors.Is(err, transcribe.ErrUnreadableAudio):
		return "couldn't transcribe this voice message (audio was empty/corrupt)"
	default:
		return "couldn't transcribe this voice message (transcription failed)"
	}
}

// VoiceLine describes a transcribed (or untranscribable) audio attachment,
// for example "[voice message 0:12, transcribed]: hello". It is empty for
// attachments that were not sent for transcription.
func (a Attachment) VoiceLine() string {
	label := "voice message"
	if a.Transcript != nil && a.Transcript.Duration > 0 {
		label += " " + transcribe.FormatDuration(a.Transcript.Duration)
	}
	switch {
	case a.Transcript != nil:
		text := strings.TrimSpace(a.Transcript.Text)
		if text == "" {
			return fmt.Sprintf("[%s, transcribed: no speech detected]", label)
		}
		return fmt.Sprintf("[%s, transcribed]: %s", label, text)
	case a.TranscriptErr != "" && a.Path != "":
		return fmt.Sprintf("[%s, not transcribed: %s; audio saved at %s]", label, a.TranscriptErr, a.Path)
	case a.TranscriptErr != "":
		return fmt.Sprintf("[%s, not transcribed: %s]", label, a.TranscriptErr)
	}
	return ""
}

// LooksLikeAudioFile reports whether a saved file is audio, by extension.
func LooksLikeAudioFile(path string) bool {
	return looksLikeAudio("", path)
}

// sniffAudio returns the audio MIME type and file extension that data's
// magic bytes identify, or "" when it isn't a recognized audio format.
func sniffAudio(data []byte) (contentType, ext string) {
	switch {
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		return "audio/mp4", ".m4a"
	case bytes.HasPrefix(data, []byte("OggS")):
		return "audio/ogg", ".ogg"
	case len(data) >= 12 && string(data[:4]) == "RIFF" && string(data[8:12]) == "WAVE":
		return "audio/wav", ".wav"
	case bytes.HasPrefix(data, []byte("fLaC")):
		return "audio/flac", ".flac"
	case bytes.HasPrefix(data, []byte("#!AMR")):
		return "audio/amr", ".amr"
	case bytes.HasPrefix(data, []byte("caff")):
		return "audio/x-caf", ".caf"
	case bytes.HasPrefix(data, []byte{0x1a, 0x45, 0xdf, 0xa3}):
		return "audio/webm", ".webm"
	case bytes.HasPrefix(data, []byte("ID3")):
		return "audio/mpeg", ".mp3"
	case len(data) >= 2 && data[0] == 0xff && data[1]&0xf6 == 0xf0:
		// ADTS: MPEG-4/2 AAC frame sync with layer bits 00.
		return "audio/aac", ".aac"
	case len(data) >= 2 && data[0] == 0xff && data[1]&0xe0 == 0xe0 && data[1]&0x06 != 0:
		// MPEG audio frame sync with a nonzero layer.
		return "audio/mpeg", ".mp3"
	}
	return "", ""
}

// detectAudioType corrects an audio attachment's declared type and file
// extension from its content. Platforms mislabel voice notes (AAC/M4A as
// audio/mpeg, for example); non-audio content is left alone.
func detectAudioType(name, contentType string, data []byte) (string, string) {
	if !looksLikeAudio(contentType, name) && contentType != "" && contentType != "application/octet-stream" {
		return name, contentType
	}
	sniffed, ext := sniffAudio(data)
	if sniffed == "" {
		return name, contentType
	}
	if current := strings.ToLower(filepath.Ext(name)); name != "" && current != ext {
		if current == "" || looksLikeAudio("", name) {
			name = strings.TrimSuffix(name, filepath.Ext(name)) + ext
		}
	}
	return name, sniffed
}
