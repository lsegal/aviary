package projects

import (
	"testing"

	"github.com/lsegal/aviary/internal/projects/transcript"
)

// TestBuiltinDefinitionsHaveTranscriptMappers covers #390: a run's recorder is
// built from the definition's transcript format, so the shipped opencode and
// cline definitions record structured transcripts like claude and codex do,
// and so does the aviary definition (#392).
func TestBuiltinDefinitionsHaveTranscriptMappers(t *testing.T) {
	for _, name := range []string{"claude", "codex", "opencode", "cline", "aviary"} {
		if transcript.NewMapper(builtinDefinition(t, name).Output.TranscriptFormat()) == nil {
			t.Errorf("%s runs record no structured transcript", name)
		}
	}
}
