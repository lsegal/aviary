package projects

import (
	"testing"

	"github.com/lsegal/aviary/internal/projects/transcript"
)

// TestBuiltinDefinitionsHaveTranscriptMappers covers #390: a run's recorder is
// built from the definition's transcript format, so the shipped opencode and
// cline definitions record structured transcripts like claude and codex do,
// and so does the gemini (#388) definition. The in-process aviary definition
// records core's events directly (#435).
func TestBuiltinDefinitionsHaveTranscriptMappers(t *testing.T) {
	for _, name := range []string{"claude", "codex", "opencode", "cline", "gemini"} {
		if transcript.NewMapper(builtinDefinition(t, name).Output.TranscriptFormat()) == nil {
			t.Errorf("%s runs record no structured transcript", name)
		}
	}
}
