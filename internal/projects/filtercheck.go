package projects

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/lsegal/aviary/internal/projects/core"
)

// SetGHRunCommand replaces the gh calls of the manager's engine and reads,
// for tests outside this package.
func (m *Manager) SetGHRunCommand(run func(context.Context, ...string) ([]byte, error)) {
	m.ghRunCommand = run
}

// CheckIssueFilter checks filter's search syntax for target and dry-runs it,
// returning how many open issues of target it matches. An invalid filter,
// including one GitHub's search rejects, is an error naming the problem.
func (m *Manager) CheckIssueFilter(ctx context.Context, target, filter string) (int, error) {
	filter = strings.Join(strings.Fields(filter), " ")
	if filter == "" {
		return 0, errors.New("the filter is empty")
	}
	if err := core.ValidateIssueFilter(filter); err != nil {
		return 0, err
	}
	parsed, err := parseTarget(target)
	if err != nil {
		return 0, err
	}
	gh := m.readGH()
	if parsed.IsProject && parsed.ProjectOwnerType != "" {
		items, err := gh.listProjectItems(ctx, parsed, filter, false)
		if err != nil {
			return 0, fmt.Errorf("check filter: %w", err)
		}
		return len(issuesFromProjectItems(items)), nil
	}
	query := publicIssueSearchQuery(parsed.Repo, filter, false, "")
	output, err := gh.run(ctx, "api", "search/issues", "-X", "GET", "-f", "q="+query, "-f", "per_page=1")
	if err != nil {
		return 0, searchError(output, err)
	}
	var page struct {
		TotalCount int `json:"total_count"`
	}
	if err := json.Unmarshal(output, &page); err != nil {
		return 0, fmt.Errorf("check filter: decode search: %w", err)
	}
	return page.TotalCount, nil
}

// searchError turns a failed search into GitHub's own explanation when its
// response carries one.
func searchError(output []byte, err error) error {
	text := strings.TrimSpace(string(output))
	if i := strings.Index(text, "{"); i >= 0 {
		var body struct {
			Message string `json:"message"`
			Errors  []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if json.NewDecoder(strings.NewReader(text[i:])).Decode(&body) == nil {
			for _, e := range body.Errors {
				if e.Message != "" {
					return errors.New(e.Message)
				}
			}
			if body.Message != "" {
				return errors.New(body.Message)
			}
		}
	}
	return fmt.Errorf("check filter: %w", err)
}
