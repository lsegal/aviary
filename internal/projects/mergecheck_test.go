package projects

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"reflect"
	"strings"
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

func TestGHCLICommitMessageReadsTheCommitsMessage(t *testing.T) {
	fake := &fakeGH{responses: map[string]string{
		"api repos/o/r/commits/abc": `{"sha":"abc","commit":{"message":"Fix parser (#8)\n\nCloses #7"}}`,
	}}
	message, err := GHCLI{runCommand: fake.run}.CommitMessage(context.Background(), "o/r", "abc")
	if err != nil {
		t.Fatal(err)
	}
	if want := "Fix parser (#8)\n\nCloses #7"; message != want {
		t.Fatalf("message = %q, want %q", message, want)
	}
	if want := [][]string{{"api", "repos/o/r/commits/abc"}}; !reflect.DeepEqual(fake.calls, want) {
		t.Fatalf("calls = %q, want %q", fake.calls, want)
	}
}

func TestGHCLICommitMessageReportsAFailedRead(t *testing.T) {
	fake := &fakeGH{failures: map[string]string{
		"api repos/o/r/commits/abc": "gh: No commit found for SHA: abc (HTTP 422)\n",
	}}
	message, err := GHCLI{runCommand: fake.run}.CommitMessage(context.Background(), "o/r", "abc")
	if err == nil {
		t.Fatalf("message = %q, want an error", message)
	}
	if want := "read commit abc in o/r: exit status 1: gh: No commit found for SHA: abc (HTTP 422)"; err.Error() != want {
		t.Fatalf("err = %q, want %q", err, want)
	}
}

func TestGHCLICommitMessageReportsMalformedJSON(t *testing.T) {
	fake := &fakeGH{responses: map[string]string{"api repos/o/r/commits/abc": `{"commit":`}}
	message, err := GHCLI{runCommand: fake.run}.CommitMessage(context.Background(), "o/r", "abc")
	if err == nil || !strings.HasPrefix(err.Error(), "decode commit abc in o/r: ") {
		t.Fatalf("message = %q, err = %v, want a decode error", message, err)
	}
	if message != "" {
		t.Fatalf("message = %q, want empty on a decode error", message)
	}
}

type failingCommitMessages struct{ err error }

func (f failingCommitMessages) CommitMessage(context.Context, string, string) (string, error) {
	return "", f.err
}

// countingCommitMessages fails the test if the merge commit is read.
type countingCommitMessages struct{ t *testing.T }

func (c countingCommitMessages) CommitMessage(_ context.Context, repo, sha string) (string, error) {
	c.t.Errorf("CommitMessage(%q, %q) called, want no read without a merge commit", repo, sha)
	return "", nil
}

func TestSkipCIInMergeReturnsTheReadError(t *testing.T) {
	readErr := errors.New("boom")
	tokens, err := skipCIInMerge(context.Background(), failingCommitMessages{readErr}, "o/r", PullRequestWorkState{Number: 8, MergeCommitSHA: "abc"})
	if !errors.Is(err, readErr) || tokens != nil {
		t.Fatalf("tokens = %q, err = %v, want %v", tokens, err, readErr)
	}
}

// captureWarnings routes slog through a JSON handler for the test and returns
// the buffer it writes each record to, one JSON object per line.
func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &buf
}

func loggedRecords(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var records []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var record map[string]any
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			t.Fatalf("decode log line %q: %v", line, err)
		}
		records = append(records, record)
	}
	return records
}

func TestWarnSkipCIInMergeLogsAFailedRead(t *testing.T) {
	buf := captureWarnings(t)
	warnSkipCIInMerge(context.Background(), failingCommitMessages{errors.New("boom")}, "o/r", 7, PullRequestWorkState{Repository: "fork/r", Number: 8, MergeCommitSHA: "abc"})
	records := loggedRecords(t, buf)
	if len(records) != 1 {
		t.Fatalf("records = %v, want one warning", records)
	}
	record := records[0]
	for key, want := range map[string]any{
		"level":        "WARN",
		"msg":          "projects: reading merge commit failed",
		"component":    "projects",
		"repo":         "fork/r",
		"pull_request": float64(8),
		"err":          "boom",
	} {
		if record[key] != want {
			t.Errorf("%s = %v, want %v", key, record[key], want)
		}
	}
}

func TestWarnSkipCIInMergeLogsTheSkipCITokens(t *testing.T) {
	buf := captureWarnings(t)
	reader := commitMessages{"o/r@abc": "Fix parser (#8)\n\n* Start work on issue #7 [skip ci]\n* Checkpoint [ci skip]\n\nCloses #7"}
	warnSkipCIInMerge(context.Background(), reader, "o/r", 7, PullRequestWorkState{Number: 8, Merged: true, MergeCommitSHA: "abc"})
	records := loggedRecords(t, buf)
	if len(records) != 1 {
		t.Fatalf("records = %v, want one warning", records)
	}
	record := records[0]
	for key, want := range map[string]any{
		"level":        "WARN",
		"msg":          "projects: merge commit skips push workflows; dispatch them on the default branch",
		"component":    "projects",
		"repo":         "o/r",
		"issue":        float64(7),
		"pull_request": float64(8),
		"merge_commit": "abc",
		"tokens":       "[skip ci] [ci skip]",
	} {
		if record[key] != want {
			t.Errorf("%s = %v, want %v", key, record[key], want)
		}
	}
}

func TestWarnSkipCIInMergeStaysQuietForACleanMerge(t *testing.T) {
	buf := captureWarnings(t)
	warnSkipCIInMerge(context.Background(), commitMessages{"o/r@abc": "Fix parser (#8)\n\nCloses #7"}, "o/r", 7, PullRequestWorkState{Number: 8, Merged: true, MergeCommitSHA: "abc"})
	if buf.Len() != 0 {
		t.Fatalf("logged %q, want nothing for a clean merge", buf.String())
	}
}

func TestWarnSkipCIInMergeDoesNotReadWithoutAMergeCommit(t *testing.T) {
	buf := captureWarnings(t)
	warnSkipCIInMerge(context.Background(), countingCommitMessages{t}, "o/r", 7, PullRequestWorkState{Number: 8})
	if buf.Len() != 0 {
		t.Fatalf("logged %q, want nothing without a merge commit", buf.String())
	}
}
