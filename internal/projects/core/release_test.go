package core

import "testing"

func TestReleasePageURL(t *testing.T) {
	for _, tc := range []struct {
		repo, version, want string
	}{
		{"lsegal/aviary", "v1.5.22", "https://github.com/lsegal/aviary/releases/tag/v1.5.22"},
		{"lsegal/aviary", "1.5.22", "https://github.com/lsegal/aviary/releases/tag/v1.5.22"},
		{"fork/aviary", "v2.0.0", "https://github.com/fork/aviary/releases/tag/v2.0.0"},
		{"", "v1.0.0", "https://github.com/lsegal/aviary/releases/tag/v1.0.0"},
		{"lsegal/aviary", "dev", "https://github.com/lsegal/aviary/releases"},
		{"lsegal/aviary", "", "https://github.com/lsegal/aviary/releases"},
		{"lsegal/aviary", "v1.5.22-3-gabc123", "https://github.com/lsegal/aviary/releases"},
	} {
		if got := ReleasePageURL(tc.repo, tc.version); got != tc.want {
			t.Errorf("ReleasePageURL(%q, %q) = %q, want %q", tc.repo, tc.version, got, tc.want)
		}
	}
}
