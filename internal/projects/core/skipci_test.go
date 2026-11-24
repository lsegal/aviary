package core

import (
	"reflect"
	"testing"
)

func TestSkipCITokensFindsEveryVariantInAnyCase(t *testing.T) {
	message := "Fix parser (#12)\n\n* Start work on issue #11 [SKIP CI]\n* [Ci Skip] [no ci]\n* [skip actions] [ACTIONS skip]\n\nCloses #11"
	want := []string{"[SKIP CI]", "[Ci Skip]", "[no ci]", "[skip actions]", "[ACTIONS skip]"}
	if got := SkipCITokens(message); !reflect.DeepEqual(got, want) {
		t.Fatalf("SkipCITokens = %q, want %q", got, want)
	}
}

func TestSkipCITokensIgnoresMarkersDescribedInWords(t *testing.T) {
	for _, message := range []string{
		"Sanitize merge messages (#1177)\n\nStrips the skip-CI marker.\n\nCloses #1176\n\nCo-authored-by: A <a@example.com>",
		"skip ci",
		"[skip-ci] [skipci] [ci]",
	} {
		if got := SkipCITokens(message); len(got) != 0 {
			t.Errorf("SkipCITokens(%q) = %q, want none", message, got)
		}
	}
}
