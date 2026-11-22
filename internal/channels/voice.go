package channels

import (
	"context"
	"errors"
	"fmt"
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
			att.TranscriptErr = transcriptError(err)
			continue
		}
		att.Transcript = &t
	}
	return out
}

func transcriptError(err error) string {
	var unavailable *transcribe.UnavailableError
	if errors.As(err, &unavailable) {
		return unavailable.Error()
	}
	return strings.TrimSpace(err.Error())
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
	case a.TranscriptErr != "":
		return fmt.Sprintf("[%s, not transcribed: %s]", label, a.TranscriptErr)
	}
	return ""
}

// LooksLikeAudioFile reports whether a saved file is audio, by extension.
func LooksLikeAudioFile(path string) bool {
	return looksLikeAudio("", path)
}
