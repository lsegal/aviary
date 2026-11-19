package core

import "testing"

func TestReleasePageURL(t *testing.T) {
	for _, tc := range []struct {
		repo, version, want string
	}{
		{"lsegal/glorp", "v1.5.22", "https://github.com/lsegal/glorp/releases/tag/v1.5.22"},
		{"lsegal/glorp", "1.5.22", "https://github.com/lsegal/glorp/releases/tag/v1.5.22"},
		{"fork/glorp", "v2.0.0", "https://github.com/fork/glorp/releases/tag/v2.0.0"},
		{"", "v1.0.0", "https://github.com/lsegal/glorp/releases/tag/v1.0.0"},
		{"lsegal/glorp", "dev", "https://github.com/lsegal/glorp/releases"},
		{"lsegal/glorp", "", "https://github.com/lsegal/glorp/releases"},
		{"lsegal/glorp", "v1.5.22-3-gabc123", "https://github.com/lsegal/glorp/releases"},
	} {
		if got := ReleasePageURL(tc.repo, tc.version); got != tc.want {
			t.Errorf("ReleasePageURL(%q, %q) = %q, want %q", tc.repo, tc.version, got, tc.want)
		}
	}
}
