package projects

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/lsegal/aviary/internal/projects/core"
)

// CommitMessageReader reads a commit's full message, so a merge can be
// checked for markers that keep its push workflows from running.
type CommitMessageReader interface {
	CommitMessage(ctx context.Context, repo, sha string) (string, error)
}

// CommitMessage reads the message of commit sha in repo.
func (g GHCLI) CommitMessage(ctx context.Context, repo, sha string) (string, error) {
	output, err := g.apiGETObject(ctx, g.isPublicRepo(ctx, repo), "repos/"+repo+"/commits/"+sha)
	if err != nil {
		return "", fmt.Errorf("read commit %s in %s: %w: %s", sha, repo, err, strings.TrimSpace(string(output)))
	}
	var commit struct {
		Commit struct {
			Message string `json:"message"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(output, &commit); err != nil {
		return "", fmt.Errorf("decode commit %s in %s: %w", sha, repo, err)
	}
	return commit.Commit.Message, nil
}

// skipCIInMerge returns the skip-CI markers in the message of pullRequest's
// merge commit, which belongs to issue repo. GitHub runs no push workflow for
// such a commit, so CI and deploys silently miss the merge (issue #1176).
func skipCIInMerge(ctx context.Context, reader CommitMessageReader, repo string, pullRequest PullRequestWorkState) ([]string, error) {
	if reader == nil || pullRequest.MergeCommitSHA == "" {
		return nil, nil
	}
	message, err := reader.CommitMessage(ctx, pullRequest.RepoOr(repo), pullRequest.MergeCommitSHA)
	if err != nil {
		return nil, err
	}
	return core.SkipCITokens(message), nil
}

// warnSkipCIInMerge logs a warning when pullRequest, which fixed issue
// repo#number, merged with a skip-CI marker in its merge commit's message.
func warnSkipCIInMerge(ctx context.Context, reader CommitMessageReader, repo string, number int, pullRequest PullRequestWorkState) {
	tokens, err := skipCIInMerge(ctx, reader, repo, pullRequest)
	if err != nil {
		slog.Warn("projects: reading merge commit failed", "component", "projects", "repo", pullRequest.RepoOr(repo), "pull_request", pullRequest.Number, "err", err)
		return
	}
	if len(tokens) == 0 {
		return
	}
	slog.Warn("projects: merge commit skips push workflows; dispatch them on the default branch", "component", "projects", "repo", pullRequest.RepoOr(repo), "issue", number, "pull_request", pullRequest.Number, "merge_commit", pullRequest.MergeCommitSHA, "tokens", strings.Join(tokens, " "))
}
