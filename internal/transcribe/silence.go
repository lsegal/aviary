package transcribe

import (
	"encoding/binary"
	"errors"
	"fmt"
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
	// still counts as silence (-60 dBFS). A muted or wrong input device
	// records zeros or faint noise below it. Quiet speech from a low-gain
	// audio interface can peak around -50 dBFS, so the floor sits well
	// under that and whisper gets anything louder.
	silencePeak = 0.001
	// speechPeak is the level quiet speech is amplified to before whisper.
	speechPeak = 0.7
	// maxGain caps that amplification (60 dB).
	maxGain = 1000
	// sampleRate is the rate ffmpeg resamples every input to.
	sampleRate = 16000
)

// audioLevel describes the channel of a recording that is transcribed.
type audioLevel struct {
	Duration time.Duration
	Peak     float64 // loudest sample, 0..1 of full scale
	RMS      float64 // 0..1 of full scale
	Channels int     // channels in the recording
	Channel  int     // the loudest channel, which is transcribed
	Gain     float64 // amplification applied before whisper
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

// measure returns the peak and RMS of samples.
func measure(samples []float64) (peak, rms float64) {
	if len(samples) == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range samples {
		peak = max(peak, math.Abs(v))
		sum += v * v
	}
	return min(peak, 1), math.Sqrt(sum / float64(len(samples)))
}

// prepareSpeech reads the 16 kHz WAV ffmpeg wrote at in, keeps its loudest
// channel, and writes it to out as 16-bit mono WAV, amplified so quiet
// speech reaches whisper at a usable level. Keeping one channel rather than
// averaging them means a mic on one input of a stereo audio interface is
// neither halved nor cancelled by the other input. Nothing is written when
// the audio is silent.
func prepareSpeech(in, out string) (audioLevel, error) {
	data, err := os.ReadFile(in)
	if err != nil {
		return audioLevel{}, err
	}
	channels, err := decodeWAV(data)
	if err != nil {
		return audioLevel{}, err
	}
	level := audioLevel{Channels: len(channels), Gain: 1}
	var samples []float64
	for i, ch := range channels {
		peak, rms := measure(ch)
		if samples == nil || rms > level.RMS {
			samples, level.Channel, level.Peak, level.RMS = ch, i, peak, rms
		}
	}
	level.Duration = time.Duration(len(samples)) * time.Second / sampleRate
	if level.silent() != "" {
		return level, nil
	}
	if level.Peak < speechPeak {
		level.Gain = min(speechPeak/level.Peak, maxGain)
	}
	return level, os.WriteFile(out, encodeWAV(samples, level.Gain), 0o600)
}

const (
	wavPCM        = 1
	wavFloat      = 3
	wavExtensible = 0xFFFE
)

// decodeWAV returns each channel of a 16-bit PCM or 32-bit float WAV file
// as samples in -1..1.
func decodeWAV(data []byte) ([][]float64, error) {
	if len(data) < 12 || string(data[0:4]) != "RIFF" || string(data[8:12]) != "WAVE" {
		return nil, errors.New("not a WAV file")
	}
	var format, channels, bits int
	for off := 12; off+8 <= len(data); {
		id := string(data[off : off+4])
		size := int(binary.LittleEndian.Uint32(data[off+4:]))
		body := off + 8
		switch id {
		case "fmt ":
			if size < 16 || body+16 > len(data) {
				return nil, errors.New("WAV format chunk is truncated")
			}
			format = int(binary.LittleEndian.Uint16(data[body:]))
			channels = int(binary.LittleEndian.Uint16(data[body+2:]))
			bits = int(binary.LittleEndian.Uint16(data[body+14:]))
			if format == wavExtensible && size >= 26 && body+26 <= len(data) {
				format = int(binary.LittleEndian.Uint16(data[body+24:]))
			}
		case "data":
			if channels == 0 {
				return nil, errors.New("WAV file has no format chunk")
			}
			// ffmpeg can't seek back to fix the size when it streams, so
			// trust the file length over an oversized header.
			return deinterleave(data[body:min(len(data), body+size)], format, channels, bits)
		}
		off = body + size + size%2
	}
	return nil, errors.New("WAV file has no data chunk")
}

func deinterleave(pcm []byte, format, channels, bits int) ([][]float64, error) {
	var width int
	var sample func([]byte) float64
	switch {
	case format == wavPCM && bits == 16:
		width = 2
		sample = func(b []byte) float64 { return float64(int16(binary.LittleEndian.Uint16(b))) / 32768 }
	case format == wavFloat && bits == 32:
		width = 4
		sample = func(b []byte) float64 { return float64(math.Float32frombits(binary.LittleEndian.Uint32(b))) }
	default:
		return nil, fmt.Errorf("unsupported WAV encoding (format %d, %d bits)", format, bits)
	}
	frames := len(pcm) / (width * channels)
	out := make([][]float64, channels)
	for c := range out {
		out[c] = make([]float64, frames)
	}
	for f := range frames {
		for c := range channels {
			out[c][f] = sample(pcm[(f*channels+c)*width:])
		}
	}
	return out, nil
}

// encodeWAV writes samples, scaled by gain, as 16 kHz mono 16-bit WAV.
func encodeWAV(samples []float64, gain float64) []byte {
	size := 2 * len(samples)
	buf := make([]byte, 44+size)
	copy(buf, "RIFF")
	binary.LittleEndian.PutUint32(buf[4:], uint32(36+size))
	copy(buf[8:], "WAVEfmt ")
	binary.LittleEndian.PutUint32(buf[16:], 16)
	binary.LittleEndian.PutUint16(buf[20:], wavPCM)
	binary.LittleEndian.PutUint16(buf[22:], 1)
	binary.LittleEndian.PutUint32(buf[24:], sampleRate)
	binary.LittleEndian.PutUint32(buf[28:], sampleRate*2)
	binary.LittleEndian.PutUint16(buf[32:], 2)
	binary.LittleEndian.PutUint16(buf[34:], 16)
	copy(buf[36:], "data")
	binary.LittleEndian.PutUint32(buf[40:], uint32(size))
	for i, v := range samples {
		binary.LittleEndian.PutUint16(buf[44+2*i:], uint16(int16(math.Round(max(-1, min(1, v*gain))*32767))))
	}
	return buf
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
