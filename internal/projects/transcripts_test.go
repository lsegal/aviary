package projects

import (
	"testing"

	"github.com/lsegal/aviary/internal/projects/transcript"
)

// TestBuiltinDefinitionsHaveTranscriptMappers covers #390 and #388: a run's
// recorder is built from the definition's transcript format, so the shipped
// opencode, cline and gemini definitions record structured transcripts like
// claude and codex do.
func TestBuiltinDefinitionsHaveTranscriptMappers(t *testing.T) {
	for _, name := range []string{"claude", "codex", "opencode", "cline", "gemini"} {
		if transcript.NewMapper(builtinDefinition(t, name).Output.TranscriptFormat()) == nil {
			t.Errorf("%s runs record no structured transcript", name)
		}
	}
}
