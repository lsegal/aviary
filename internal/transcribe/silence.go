package transcribe

import (
	"encoding/binary"
	"errors"
	"math"
	"os"
	"strings"
	"time"
	"unicode"
)

const (
	// minSpeechDuration is the shortest recording worth transcribing.
	minSpeechDuration = 500 * time.Millisecond
	// silencePeak is the loudest sample, as a fraction of full scale, that
	// still counts as silence (about -46 dBFS). A muted or wrong input
	// device records zeros or faint noise well below it; speech peaks far
	// above it.
	silencePeak = 0.005
)

// audioLevel describes 16-bit mono PCM audio.
type audioLevel struct {
	Duration time.Duration
	Peak     float64 // loudest sample, 0..1 of full scale
	RMS      float64 // 0..1 of full scale
}

// dBFS renders a 0..1 level in decibels relative to full scale.
func dBFS(level float64) float64 {
	if level <= 0 {
		return math.Inf(-1)
	}
	return 20 * math.Log10(level)
}

// silent reports why audio holds no speech worth transcribing, or "".
func (l audioLevel) silent() string {
	switch {
	case l.Duration < minSpeechDuration:
		return "recording is too short"
	case l.Peak < silencePeak:
		return "recording is silent"
	}
	return ""
}

// readWAVLevel measures a 16 kHz mono 16-bit WAV file written by ffmpeg.
func readWAVLevel(path string) (audioLevel, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return audioLevel{}, err
	}
	pcm, err := wavData(data)
	if err != nil {
		return audioLevel{}, err
	}
	samples := len(pcm) / 2
	if samples == 0 {
		return audioLevel{}, nil
	}
	var peak, sum float64
	for i := 0; i+1 < len(pcm); i += 2 {
		v := math.Abs(float64(int16(binary.LittleEndian.Uint16(pcm[i:])))) / 32768
		peak = max(peak, v)
		sum += v * v
	}
	return audioLevel{
		Duration: time.Duration(samples) * time.Second / 16000,
		Peak:     peak,
		RMS:      math.Sqrt(sum / float64(samples)),
	}, nil
}

// wavData returns the PCM bytes of a RIFF WAV file's data chunk.
func wavData(data []byte) ([]byte, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4:]))
		body := off + 8
		if id == "data" {
			// ffmpeg can't seek back to fix the size when it streams, so
			// trust the file length over an oversized header.
			return data[body:min(len(data), body+size)], nil
		}
		off = body + size + size%2
	}
	return nil, errors.New("WAV file has no data chunk")
}

// hallucinations are what Whisper is known to produce for silent or empty
// input, compared after normalizeHallucination.
var hallucinations = map[string]bool{
	"you":                    true,
	"thankyou":               true,
	"thankyouforwatching":    true,
	"thanksforwatching":      true,
	"blankaudio":             true,
	"silence":                true,
	"nospeech":               true,
	"inaudible":              true,
	"music":                  true,
	"pleasesubscribe":        true,
	"subtitlesbytheamaraorg": true,
}

func normalizeHallucination(text string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, text)
}

// isHallucination reports whether a whole transcript is one of Whisper's
// stock outputs for silence rather than real speech.
func isHallucination(text string) bool {
	return text == "" || hallucinations[normalizeHallucination(text)]
}

// isNonSpeechMarker reports whether a segment is a bracketed annotation such
// as "[BLANK_AUDIO]" or "(music)" rather than spoken words.
func isNonSpeechMarker(text string) bool {
	return len(text) >= 2 && strings.ContainsRune("[(*", rune(text[0])) && strings.ContainsRune("])*", rune(text[len(text)-1]))
}
