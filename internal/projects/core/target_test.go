package core

import (
	"slices"
	"strings"
	"sync"
	"testing"
)

func TestValidRepo(t *testing.T) {
	tests := []struct {
		repo string
		want bool
	}{
		{repo: "o/r", want: true},
		{repo: "o", want: false},
		{repo: "o/r/x", want: false},
		{repo: "/r", want: false},
		{repo: "o/", want: false},
		{repo: "o/r x", want: false},
		{repo: "o/r\n", want: false},
	}
	for _, tt := range tests {
		if got := ValidRepo(tt.repo); got != tt.want {
			t.Errorf("ValidRepo(%q) = %v, want %v", tt.repo, got, tt.want)
		}
	}
}

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  Target
	}{
		{name: "owner/repo", value: "o/r", want: Target{Repo: "o/r"}},
		{name: "repository URL", value: "https://github.com/o/r", want: Target{Repo: "o/r"}},
		{name: "repository URL with .git", value: "https://github.com/o/r.git", want: Target{Repo: "o/r"}},
		{name: "repository URL with trailing slash", value: "https://github.com/o/r/", want: Target{Repo: "o/r"}},
		{
			name:  "user project",
			value: "https://github.com/users/u/projects/3",
			want:  Target{Owner: "u", ProjectID: "3", ProjectOwnerType: "users", IsProject: true},
		},
		{
			name:  "org project",
			value: "https://github.com/orgs/acme/projects/5",
			want:  Target{Owner: "acme", ProjectID: "5", ProjectOwnerType: "orgs", IsProject: true},
		},
		{
			name:  "repository project",
			value: "https://github.com/o/r/projects/2",
			want:  Target{Repo: "o/r", Owner: "o", ProjectID: "2", IsProject: true},
		},
		{
			name:  "discussions",
			value: "https://github.com/o/r/discussions",
			want:  Target{Repo: "o/r", IsDiscussion: true},
		},
		{
			name:  "discussion category",
			value: "https://github.com/o/r/discussions/categories/ideas",
			want:  Target{Repo: "o/r", IsDiscussion: true, DiscussionCategory: "ideas"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseTarget(tt.value)
			if err != nil {
				t.Fatalf("ParseTarget(%q) error = %v", tt.value, err)
			}
			if got != tt.want {
				t.Fatalf("ParseTarget(%q) = %+v, want %+v", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseTargetRejects(t *testing.T) {
	for _, value := range []string{
		"",
		"o",
		"o/r/x",
		"http://github.com/o/r",
		"https://gitlab.com/o/r",
		"https://www.github.com/o/r",
		"https://github.com/o/r?tab=readme",
		"https://github.com/o/r#readme",
		"https://github.com/%zz",
		"https://github.com/",
		"https://github.com/o",
		"https://github.com/o/r/issues",
		"https://github.com/o/r/projects",
		"https://github.com/users/u/projects",
		"https://github.com/o/r/discussions/7",
		"https://github.com/o/r/discussions/categories",
		"https://github.com/o/r/discussions/sections/ideas",
		"https://github.com/o/r/discussions/categories/ideas/x",
	} {
		t.Run(value, func(t *testing.T) {
			got, err := ParseTarget(value)
			if err == nil {
				t.Fatalf("ParseTarget(%q) = %+v, want error", value, got)
			}
			if !strings.Contains(err.Error(), "OWNER/REPO") {
				t.Fatalf("ParseTarget(%q) error = %q, want it to name OWNER/REPO", value, err)
			}
			if got != (Target{}) {
				t.Fatalf("ParseTarget(%q) = %+v, want zero Target", value, got)
			}
		})
	}
}

func TestIsProjectAndDiscussionTarget(t *testing.T) {
	tests := []struct {
		value               string
		project, discussion bool
	}{
		{value: "o/r"},
		{value: "https://github.com/o/r"},
		{value: "https://github.com/orgs/acme/projects/5", project: true},
		{value: "https://github.com/o/r/projects/2", project: true},
		{value: "https://github.com/o/r/discussions", discussion: true},
		{value: "https://github.com/o/r/discussions/categories/ideas", discussion: true},
		{value: "not a target"},
		{value: "http://github.com/orgs/acme/projects/5"},
	}
	for _, tt := range tests {
		if got := IsProjectTarget(tt.value); got != tt.project {
			t.Errorf("IsProjectTarget(%q) = %v, want %v", tt.value, got, tt.project)
		}
		if got := IsDiscussionTarget(tt.value); got != tt.discussion {
			t.Errorf("IsDiscussionTarget(%q) = %v, want %v", tt.value, got, tt.discussion)
		}
	}
}

func TestFilterOverridesFor(t *testing.T) {
	var none *FilterOverrides
	if filter, all := none.For("o/r", "label:bug", true); filter != "label:bug" || !all {
		t.Fatalf("nil For = %q, %v; want the run's filter and all-issues setting", filter, all)
	}

	o := NewFilterOverrides(map[string]string{"o/r": "label:ready", "o/empty": ""})
	if filter, all := o.For("o/r", "label:bug", true); filter != "label:ready" || all {
		t.Fatalf("overridden For = %q, %v; want the override with all-issues off", filter, all)
	}
	if filter, all := o.For("o/other", "label:bug", true); filter != "label:bug" || !all {
		t.Fatalf("unoverridden For = %q, %v; want the run's filter and all-issues setting", filter, all)
	}
	if filter, all := o.For("o/empty", "label:bug", false); filter != "label:bug" || all {
		t.Fatalf("empty override For = %q, %v; want the run's filter", filter, all)
	}

	o.Set(map[string]string{"o/other": "label:docs"})
	if filter, all := o.For("o/r", "label:bug", true); filter != "label:bug" || !all {
		t.Fatalf("For after Set = %q, %v; want the removed override gone", filter, all)
	}
	if filter, _ := o.For("o/other", "label:bug", true); filter != "label:docs" {
		t.Fatalf("For after Set = %q, want the new override", filter)
	}

	o.Set(nil)
	if filter, all := o.For("o/other", "label:bug", true); filter != "label:bug" || !all {
		t.Fatalf("For after Set(nil) = %q, %v; want no overrides", filter, all)
	}
}

// Set keeps its own copy, so a caller changing its map afterwards does not
// change a running engine's overrides.
func TestFilterOverridesSetClones(t *testing.T) {
	filters := map[string]string{"o/r": "label:ready"}
	o := NewFilterOverrides(filters)
	filters["o/r"] = "label:changed"
	filters["o/other"] = "label:added"

	if filter, _ := o.For("o/r", "", false); filter != "label:ready" {
		t.Fatalf("For(o/r) = %q after mutating the caller's map, want label:ready", filter)
	}
	if filter, _ := o.For("o/other", "", false); filter != "" {
		t.Fatalf("For(o/other) = %q after mutating the caller's map, want none", filter)
	}

	replacement := map[string]string{"o/r": "label:second"}
	o.Set(replacement)
	delete(replacement, "o/r")
	if filter, _ := o.For("o/r", "", false); filter != "label:second" {
		t.Fatalf("For(o/r) = %q after deleting from the Set map, want label:second", filter)
	}
}

func TestIssueFilterTerms(t *testing.T) {
	tests := []struct {
		filter string
		want   []string
	}{
		{filter: "", want: nil},
		{filter: "   ", want: nil},
		{filter: "assignee:@me", want: []string{"assignee:@me"}},
		{
			filter: "is:pr state:closed type:bug  label:ready -label:wip",
			want:   []string{"label:ready", "-label:wip"},
		},
		{filter: "is:issue is:open", want: nil},
		// Only a leading qualifier names a kind or state.
		{filter: "-is:pr label:is:odd", want: []string{"-is:pr", "label:is:odd"}},
	}
	for _, tt := range tests {
		if got := IssueFilterTerms(tt.filter); !slices.Equal(got, tt.want) {
			t.Errorf("IssueFilterTerms(%q) = %q, want %q", tt.filter, got, tt.want)
		}
	}
}

func TestIssueSearchTerms(t *testing.T) {
	tests := []struct {
		filter    string
		allIssues bool
		want      []string
	}{
		{filter: "", want: []string{"is:issue", "state:open"}},
		{filter: "assignee:@me", want: []string{"is:issue", "state:open", "assignee:@me"}},
		{filter: "is:pr state:closed label:bug", want: []string{"is:issue", "state:open", "label:bug"}},
		{filter: "label:bug", allIssues: true, want: []string{"is:issue", "state:open"}},
	}
	for _, tt := range tests {
		if got := IssueSearchTerms(tt.filter, tt.allIssues); !slices.Equal(got, tt.want) {
			t.Errorf("IssueSearchTerms(%q, %v) = %q, want %q", tt.filter, tt.allIssues, got, tt.want)
		}
	}
}

func TestProjectItemQuery(t *testing.T) {
	tests := []struct {
		filter    string
		allIssues bool
		want      string
	}{
		{filter: "", want: "is:issue is:open"},
		{filter: DefaultIssueFilter, want: "is:issue is:open"},
		{filter: "assignee:octocat", want: "is:issue is:open assignee:octocat"},
		{filter: "is:closed state:open type:pr label:bug", want: "is:issue is:open label:bug"},
		{filter: "label:bug", allIssues: true, want: "is:issue is:open"},
	}
	for _, tt := range tests {
		if got := ProjectItemQuery(tt.filter, tt.allIssues); got != tt.want {
			t.Errorf("ProjectItemQuery(%q, %v) = %q, want %q", tt.filter, tt.allIssues, got, tt.want)
		}
	}
}

func TestValidateIssueFilter(t *testing.T) {
	tests := []struct {
		name    string
		filter  string
		wantErr string
	}{
		{name: "empty", filter: ""},
		{name: "default", filter: DefaultIssueFilter},
		{name: "quoted value", filter: `label:"good first issue" -label:wip`},
		{name: "balanced parentheses", filter: "(label:bug OR label:docs) assignee:@me"},
		{name: "parentheses inside quotes", filter: `label:"a (b" label:"c) d"`},
		{name: "bare words", filter: "crash on start"},
		{name: "colon without qualifier", filter: ":value"},
		{name: "unbalanced quote", filter: `label:"bug`, wantErr: "unbalanced quote"},
		{name: "unclosed parenthesis", filter: "(label:bug OR label:docs", wantErr: "unbalanced parenthesis"},
		{name: "negative parenthesis depth", filter: "label:bug) (label:docs", wantErr: "unbalanced parenthesis"},
		{name: "empty qualifier", filter: "label:", wantErr: "label: needs a value"},
		{name: "empty negated quoted qualifier", filter: `-label:""`, wantErr: "label: needs a value"},
		{name: "empty qualifier in parentheses", filter: "(label:) OR assignee:@me", wantErr: "label: needs a value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIssueFilter(tt.filter)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ValidateIssueFilter(%q) = %v, want nil", tt.filter, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ValidateIssueFilter(%q) = %v, want an error containing %q", tt.filter, err, tt.wantErr)
			}
		})
	}
}

func TestManualTargetsNil(t *testing.T) {
	var none *ManualTargets
	if none.Has("o/r") {
		t.Fatal("nil ManualTargets.Has(o/r) = true, want false")
	}
	if got := none.List(); got != nil {
		t.Fatalf("nil ManualTargets.List() = %v, want nil", got)
	}
}

func TestManualTargetsHasAndList(t *testing.T) {
	m := NewManualTargets([]string{"z/r", "a/r", "m/r", "a/r"})
	if got, want := m.List(), []string{"a/r", "m/r", "z/r"}; !slices.Equal(got, want) {
		t.Fatalf("List() = %v, want %v", got, want)
	}
	for target, want := range map[string]bool{"a/r": true, "m/r": true, "z/r": true, "o/r": false, "": false} {
		if got := m.Has(target); got != want {
			t.Errorf("Has(%q) = %v, want %v", target, got, want)
		}
	}
	if got := NewManualTargets(nil).List(); len(got) != 0 {
		t.Fatalf("empty List() = %v, want none", got)
	}
}

func TestManualTargetsSet(t *testing.T) {
	tests := []struct {
		name          string
		before, after []string
		wantLeft      []string
	}{
		{name: "empty to empty"},
		{name: "empty to some", after: []string{"a/r", "b/r"}},
		{name: "unchanged", before: []string{"a/r", "b/r"}, after: []string{"b/r", "a/r"}},
		{name: "some leave", before: []string{"a/r", "b/r", "c/r"}, after: []string{"b/r", "d/r"}, wantLeft: []string{"a/r", "c/r"}},
		{name: "all leave", before: []string{"a/r", "b/r"}, wantLeft: []string{"a/r", "b/r"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewManualTargets(tt.before)
			left := m.Set(tt.after)
			slices.Sort(left)
			if !slices.Equal(left, tt.wantLeft) {
				t.Fatalf("Set(%v) left = %v, want %v", tt.after, left, tt.wantLeft)
			}
			want := slices.Compact(slices.Sorted(slices.Values(tt.after)))
			if got := m.List(); !slices.Equal(got, want) {
				t.Fatalf("List() after Set = %v, want %v", got, want)
			}
			for _, target := range tt.wantLeft {
				if m.Has(target) {
					t.Errorf("Has(%q) after leaving = true, want false", target)
				}
			}
		})
	}
}

func TestManualTargetsSetDoesNotAliasInput(t *testing.T) {
	targets := []string{"a/r"}
	m := NewManualTargets(targets)
	targets[0] = "b/r"
	if !m.Has("a/r") || m.Has("b/r") {
		t.Fatalf("set followed a caller mutation: List() = %v", m.List())
	}
}

func TestManualTargetsConcurrent(t *testing.T) {
	m := NewManualTargets(nil)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for j := range 100 {
				if j%2 == i%2 {
					m.Set([]string{"a/r"})
				} else {
					m.Set([]string{"b/r"})
				}
			}
		}()
		go func() {
			defer wg.Done()
			for range 100 {
				m.Has("a/r")
				m.List()
			}
		}()
	}
	wg.Wait()
	if got := m.List(); len(got) != 1 || (got[0] != "a/r" && got[0] != "b/r") {
		t.Fatalf("List() after concurrent Set = %v, want exactly one of a/r or b/r", got)
	}
}
