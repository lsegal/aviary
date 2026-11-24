package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// OpenPullRequest is an open pull request as the safe-merge picker lists it.
type OpenPullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Author string `json:"author"`
	URL    string `json:"url"`
	Draft  bool   `json:"draft"`
	// CI is passing, failing, pending, or none; Review is approved,
	// changes_requested, review_required, or none; Mergeable is mergeable,
	// conflicting, or unknown.
	CI        string    `json:"ci"`
	Review    string    `json:"review"`
	Mergeable string    `json:"mergeable"`
	UpdatedAt time.Time `json:"updated_at,omitzero"`
	// OwnedBy names the job already working on the pull request.
	OwnedBy string `json:"owned_by,omitempty"`
}

// OpenDiscussion is an open Discussion thread as the gh-discuss picker lists
// it.
type OpenDiscussion struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Author   string `json:"author"`
	URL      string `json:"url"`
	Category string `json:"category,omitempty"`
	Comments int    `json:"comments"`
	Answered bool   `json:"answered"`
	OwnedBy  string `json:"owned_by,omitempty"`
}

// QueueCandidateSource lists what a project's jobs can be queued for.
type QueueCandidateSource interface {
	ListOpenPullRequests(ctx context.Context, repo string) ([]OpenPullRequest, error)
	ListOpenDiscussions(ctx context.Context, repo string) ([]OpenDiscussion, error)
}

type ghPullRequestListItem struct {
	Number         int    `json:"number"`
	Title          string `json:"title"`
	URL            string `json:"url"`
	IsDraft        bool   `json:"isDraft"`
	Mergeable      string `json:"mergeable"`
	ReviewDecision string `json:"reviewDecision"`
	Author         struct {
		Login string `json:"login"`
	} `json:"author"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	StatusCheckRollup []ghCheckState `json:"statusCheckRollup"`
}

// ghCheckState is one check run (status and conclusion) or commit status
// (state) of a pull request's head.
type ghCheckState struct {
	Status     string `json:"status"`
	Conclusion string `json:"conclusion"`
	State      string `json:"state"`
}

// rollupCI summarizes a pull request's checks: failing when any failed,
// pending while any is still going, passing once all passed, and none when
// it has none.
func rollupCI(checks []ghCheckState) string {
	if len(checks) == 0 {
		return "none"
	}
	pending := false
	for _, check := range checks {
		result := strings.ToUpper(check.Conclusion)
		if check.State != "" {
			result = strings.ToUpper(check.State)
		}
		switch result {
		case "FAILURE", "ERROR", "CANCELLED", "TIMED_OUT", "ACTION_REQUIRED", "STARTUP_FAILURE":
			return "failing"
		case "SUCCESS", "NEUTRAL", "SKIPPED":
		default:
			pending = true
		}
	}
	if pending {
		return "pending"
	}
	return "passing"
}

// ListOpenPullRequests lists repo's open pull requests, most recently updated
// first, with their checks, review decision, and mergeability.
func (g GHCLI) ListOpenPullRequests(ctx context.Context, repo string) ([]OpenPullRequest, error) {
	output, err := g.Run(ctx, "pr", "list", "--repo", repo, "--state", "open", "--limit", "100", "--json", "number,title,url,author,isDraft,mergeable,reviewDecision,statusCheckRollup,updatedAt")
	if err != nil {
		return nil, fmt.Errorf("list open pull requests for %s: %w", repo, err)
	}
	var items []ghPullRequestListItem
	if err := json.Unmarshal(output, &items); err != nil {
		return nil, fmt.Errorf("decode open pull requests for %s: %w", repo, err)
	}
	pulls := make([]OpenPullRequest, 0, len(items))
	for _, item := range items {
		review := strings.ToLower(item.ReviewDecision)
		if review == "" {
			review = "none"
		}
		mergeable := strings.ToLower(item.Mergeable)
		if mergeable != "mergeable" && mergeable != "conflicting" {
			mergeable = "unknown"
		}
		pulls = append(pulls, OpenPullRequest{Number: item.Number, Title: item.Title, Author: item.Author.Login, URL: item.URL, Draft: item.IsDraft, CI: rollupCI(item.StatusCheckRollup), Review: review, Mergeable: mergeable, UpdatedAt: item.UpdatedAt})
	}
	return pulls, nil
}

const openDiscussionsQuery = `query($owner:String!,$name:String!){
  repository(owner:$owner,name:$name){
    discussions(first:100,states:[OPEN],orderBy:{field:UPDATED_AT,direction:DESC}){
      nodes{number title url isAnswered author{login} category{name} comments{totalCount}}
    }
  }
}`

type openDiscussionsResponse struct {
	Data struct {
		Repository struct {
			Discussions struct {
				Nodes []struct {
					Number     int    `json:"number"`
					Title      string `json:"title"`
					URL        string `json:"url"`
					IsAnswered bool   `json:"isAnswered"`
					Author     struct {
						Login string `json:"login"`
					} `json:"author"`
					Category struct {
						Name string `json:"name"`
					} `json:"category"`
					Comments struct {
						TotalCount int `json:"totalCount"`
					} `json:"comments"`
				} `json:"nodes"`
			} `json:"discussions"`
		} `json:"repository"`
	} `json:"data"`
}

// ListOpenDiscussions lists repo's open Discussion threads, most recently
// updated first. Discussions have no REST API, so this is GraphQL.
func (g GHCLI) ListOpenDiscussions(ctx context.Context, repo string) ([]OpenDiscussion, error) {
	owner, name, ok := strings.Cut(repo, "/")
	if !ok {
		return nil, fmt.Errorf("invalid repository %q", repo)
	}
	output, err := g.Run(ctx, "api", "graphql", "-f", "query="+openDiscussionsQuery, "-F", "owner="+owner, "-F", "name="+name)
	if err != nil {
		return nil, fmt.Errorf("list open discussions for %s: %w", repo, err)
	}
	var response openDiscussionsResponse
	if err := json.Unmarshal(output, &response); err != nil {
		return nil, fmt.Errorf("decode open discussions for %s: %w", repo, err)
	}
	nodes := response.Data.Repository.Discussions.Nodes
	discussions := make([]OpenDiscussion, 0, len(nodes))
	for _, node := range nodes {
		discussions = append(discussions, OpenDiscussion{Number: node.Number, Title: node.Title, Author: node.Author.Login, URL: node.URL, Category: node.Category.Name, Comments: node.Comments.TotalCount, Answered: node.IsAnswered})
	}
	return discussions, nil
}

// QueueCandidates lists what target's board can queue for skill: open pull
// requests for gh-safe-merge, open Discussion threads for gh-discuss, each
// naming the job that already owns it. Repository is needed only when target
// is not a repository.
func (w *Engine) QueueCandidates(ctx context.Context, target, repository, skill string) (map[string]any, error) {
	if repository == "" {
		repository = issueRepository(target, Issue{})
	}
	if repository == "" {
		return nil, fmt.Errorf("name the repository to list for %s", target)
	}
	source, ok := w.Issues.(QueueCandidateSource)
	if !ok {
		return nil, fmt.Errorf("this instance's GitHub client (%T) cannot list pull requests or discussions; it needs gh polling", w.Issues)
	}
	jobs := map[string]JobSnapshot{}
	if snapshot := w.published.Load(); snapshot != nil {
		for _, job := range snapshot.Jobs {
			jobs[job.Target+"#"+strconv.Itoa(job.Number)] = job
		}
	}
	result := map[string]any{"target": target, "repository": repository, "skill": skill, "no_merge": w.noMerge.Load()}
	switch skill {
	case SkillSafeMerge:
		pulls, err := source.ListOpenPullRequests(ctx, repository)
		if err != nil {
			return nil, err
		}
		for i := range pulls {
			pulls[i].OwnedBy = queuedJobOwner(jobs, queuedJob{Skill: skill, Target: target, Repository: repository, Number: pulls[i].Number})
		}
		result["pull_requests"] = pulls
	case SkillDiscuss:
		discussions, err := source.ListOpenDiscussions(ctx, repository)
		if err != nil {
			return nil, err
		}
		for i := range discussions {
			discussions[i].OwnedBy = queuedJobOwner(jobs, queuedJob{Skill: skill, Target: target, Repository: repository, Number: discussions[i].Number})
		}
		result["discussions"] = discussions
	default:
		return nil, fmt.Errorf("skill %q has nothing to list; use %s or %s", skill, SkillSafeMerge, SkillDiscuss)
	}
	return result, nil
}
