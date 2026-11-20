package version

import "testing"

// TestParse covers the shapes a CLI prints its version in, since the
// number has to be read out of whatever banner surrounds it.
func TestParse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		text   string
		want   []int
		wantOK bool
	}{
		{name: "bare", text: "0.58.0", want: []int{0, 58, 0}, wantOK: true},
		{name: "named", text: "gemini 0.58.0", want: []int{0, 58, 0}, wantOK: true},
		{name: "banner", text: "codex-cli 0.5.0 (rust)\n", want: []int{0, 5, 0}, wantOK: true},
		{name: "two components", text: "1.2", want: []int{1, 2}, wantOK: true},
		{name: "prerelease", text: "2.0.0-beta.3", want: []int{2, 0, 0}, wantOK: true},
		{name: "no version", text: "unknown", wantOK: false},
		{name: "empty", text: "", wantOK: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Parse(tc.text)
			if ok != tc.wantOK {
				t.Fatalf("Parse(%q) ok = %v, want %v", tc.text, ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if len(got) != len(tc.want) {
				t.Fatalf("Parse(%q) = %v, want %v", tc.text, got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("Parse(%q) = %v, want %v", tc.text, got, tc.want)
				}
			}
		})
	}
}

// TestCompare pins the ordering the minimum-version check rests on,
// including the component-wise comparison that keeps 0.9.0 below 0.58.0 from
// being read the way a string comparison would.
func TestCompare(t *testing.T) {
	for _, tc := range []struct {
		a, b   string
		want   int
		wantOK bool
	}{
		{a: "0.58.0", b: "0.58.0", want: 0, wantOK: true},
		{a: "0.58", b: "0.58.0", want: 0, wantOK: true},
		{a: "0.2.2", b: "0.58.0", want: -1, wantOK: true},
		{a: "0.9.0", b: "0.58.0", want: -1, wantOK: true},
		{a: "1.0.0", b: "0.58.0", want: 1, wantOK: true},
		{a: "0.58.1", b: "0.58.0", want: 1, wantOK: true},
		{a: "unknown", b: "0.58.0", wantOK: false},
		{a: "0.58.0", b: "", wantOK: false},
	} {
		got, ok := Compare(tc.a, tc.b)
		if ok != tc.wantOK {
			t.Fatalf("Compare(%q, %q) ok = %v, want %v", tc.a, tc.b, ok, tc.wantOK)
		}
		if ok && got != tc.want {
			t.Fatalf("Compare(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestFind reads the version string out of the banners the doctor prints.
func TestFind(t *testing.T) {
	for text, want := range map[string]string{
		"2.1.312 (Claude Code)\n": "2.1.312",
		"codex-cli 0.153.2\n":     "0.153.2",
		"unknown":                 "",
	} {
		if got := Find(text); got != want {
			t.Errorf("Find(%q) = %q, want %q", text, got, want)
		}
	}
}

// TestParseRelease accepts only clean X.Y.Z release tags, which is what keeps
// a dev build from being offered an upgrade it cannot be ordered against.
func TestParseRelease(t *testing.T) {
	for raw, wantOK := range map[string]bool{
		"1.2.3": true, "v1.2.3": true, " v10.0.1 ": true,
		"dev": false, "1.2": false, "1.2.3.4": false, "1.2.3-dirty": false, "v1.2.3 (abc)": false, "": false,
	} {
		_, err := ParseRelease(raw)
		if (err == nil) != wantOK {
			t.Errorf("ParseRelease(%q) err = %v, want ok %v", raw, err, wantOK)
		}
	}
	a, _ := ParseRelease("v1.9.0")
	b, _ := ParseRelease("1.10.0")
	if CompareReleases(a, b) != -1 || CompareReleases(b, a) != 1 || CompareReleases(a, a) != 0 {
		t.Error("CompareReleases does not order 1.9.0 below 1.10.0")
	}
}
