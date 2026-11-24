package projects

import (
	"context"
	"reflect"
	"testing"
)

type commitMessages map[string]string

func (c commitMessages) CommitMessage(_ context.Context, repo, sha string) (string, error) {
	return c[repo+"@"+sha], nil
}

func TestSkipCIInMergeReadsTheMergeCommitInThePullRequestsRepository(t *testing.T) {
	reader := commitMessages{
		"o/r@abc":    "Fix parser (#8)\n\n* Start work on issue #7 [skip ci]\n* Checkpoint issue #7 progress [Skip CI]\n\nCloses #7",
		"fork/r@def": "Fix lexer (#9)\n\nCloses o/r#7",
	}
	tokens, err := skipCIInMerge(context.Background(), reader, "o/r", PullRequestWorkState{Number: 8, Merged: true, MergeCommitSHA: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"[skip ci]", "[Skip CI]"}; !reflect.DeepEqual(tokens, want) {
		t.Fatalf("tokens = %q, want %q", tokens, want)
	}
	tokens, err = skipCIInMerge(context.Background(), reader, "o/r", PullRequestWorkState{Repository: "fork/r", Number: 9, Merged: true, MergeCommitSHA: "def"})
	if err != nil || len(tokens) != 0 {
		t.Fatalf("clean merge tokens = %q, err = %v, want none", tokens, err)
	}
}

func TestSkipCIInMergeSkipsPullRequestsWithoutAMergeCommit(t *testing.T) {
	tokens, err := skipCIInMerge(context.Background(), commitMessages{"o/r@": "[skip ci]"}, "o/r", PullRequestWorkState{Number: 8, Merged: true})
	if err != nil || len(tokens) != 0 {
		t.Fatalf("tokens = %q, err = %v, want none without a merge commit", tokens, err)
	}
}
