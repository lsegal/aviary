package projects

import (
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/lsegal/aviary/internal/transcript"
)

// transcriptStore keeps every job's transcript, keyed by the job's
// "target#number" key, under one directory. It is shared by pointer between
// the engine, which records Code Loop's own events, and the runner, which
// records the CLI's output.
type transcriptStore struct {
	dir  string
	mu   sync.Mutex
	logs map[string]*transcript.Log
}

// newTranscriptStore keeps transcripts under dir; an empty dir keeps them in
// memory only.
func newTranscriptStore(dir string) *transcriptStore {
	return &transcriptStore{dir: dir, logs: map[string]*transcript.Log{}}
}

var unsafeKeyChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// transcriptDirName turns a job key such as "owner/repo#12" into a single
// directory name.
func transcriptDirName(key string) string {
	return strings.Trim(unsafeKeyChars.ReplaceAllString(key, "_"), "_")
}

// For returns the transcript of the job with key, opening it on first use. A
// nil store has no transcripts.
func (s *transcriptStore) For(key string) *transcript.Log {
	if s == nil || key == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if log, ok := s.logs[key]; ok {
		return log
	}
	dir := ""
	if s.dir != "" {
		dir = filepath.Join(s.dir, transcriptDirName(key))
	}
	log, err := transcript.Open(dir)
	if err != nil {
		log, _ = transcript.Open("")
	}
	s.logs[key] = log
	return log
}

// Lookup returns the transcript of the job with key when this run has one,
// opening a stored one from an earlier run.
func (s *transcriptStore) Lookup(key string) *transcript.Log {
	return s.For(key)
}

// jobSystem records one of Code Loop's own events in a job's transcript.
func (w *Engine) jobSystem(key, text string) {
	w.Transcripts.For(key).System(text)
}

// jobEvents records events in a job's transcript.
func (w *Engine) jobEvents(key string, events ...transcript.Event) {
	w.Transcripts.For(key).Add(events...)
}

// transcriptRecorder returns the writer that records a run's raw output and
// its mapped events, or nil when the runner keeps no transcripts.
func (r CommandRunner) transcriptRecorder(issue Issue, format string) *transcript.Recorder {
	log := r.Transcripts.For(issueKey(issue))
	if log == nil {
		return nil
	}
	return log.Recorder(transcript.NewMapper(format))
}
