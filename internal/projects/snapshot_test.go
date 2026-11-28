package projects

import (
	"testing"
	"unicode/utf8"
)

// TestTruncate covers every branch of truncate: non-positive widths, strings
// that already fit, short widths cut without an ellipsis, wider widths cut
// with one, and multi-byte strings cut by runes rather than bytes.
func TestTruncate(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		width int
		want  string
	}{
		{"zero width", "hello", 0, ""},
		{"negative width", "hello", -4, ""},
		{"empty string", "", 5, ""},
		{"shorter than width", "hi", 5, "hi"},
		{"equal to width", "hello", 5, "hello"},
		{"width one", "hello", 1, "h"},
		{"width two", "hello", 2, "he"},
		{"width three", "hello", 3, "hel"},
		{"width four adds ellipsis", "hello", 4, "h..."},
		{"wide cut adds ellipsis", "hello, world", 8, "hello..."},
		{"emoji fits by runes", "😀😁😂", 3, "😀😁😂"},
		{"emoji short width", "😀😁😂😃", 2, "😀😁"},
		{"emoji ellipsis", "😀😁😂😃😄😅", 5, "😀😁..."},
		{"cjk ellipsis", "日本語のテキスト", 6, "日本語..."},
		{"cjk short width", "日本語のテキスト", 3, "日本語"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncate(tt.s, tt.width)
			if got != tt.want {
				t.Fatalf("truncate(%q, %d) = %q, want %q", tt.s, tt.width, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Fatalf("truncate(%q, %d) = %q is not valid UTF-8", tt.s, tt.width, got)
			}
			if tt.width > 0 && utf8.RuneCountInString(got) > tt.width {
				t.Fatalf("truncate(%q, %d) = %q has more than %d runes", tt.s, tt.width, got, tt.width)
			}
		})
	}
}
