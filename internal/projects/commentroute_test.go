package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/core"
)

func TestNeutralizeMentionsBreaksEveryMention(t *testing.T) {
	got := neutralizeMentions("Thanks @lsegal, cc @org/team and @/glorp:SELF; mail a@b.test")
	if mentionedLogin(got, "lsegal") || mentionedIdentity(got, "SELF") || strings.Contains(got, "@org") {
		t.Errorf("neutralizeMentions left a mention: %q", got)
	}
	if strings.ReplaceAll(got, "​", "") != "Thanks @lsegal, cc @org/team and @/glorp:SELF; mail a@b.test" {
		t.Errorf("neutralizeMentions changed more than the mentions: %q", got)
	}
}

func TestRoutedCommentURLLinksEachKindOfComment(t *testing.T) {
	for _, tc := range []struct {
		comment routedComment
		want    string
	}{
		{routedComment{Repository: "o/r", Number: 7, ID: 42}, "https://github.com/o/r/issues/7#issuecomment-42"},
		{routedComment{Kind: ReviewComment, Repository: "o/r", Number: 9, ID: 5}, "https://github.com/o/r/pull/9#discussion_r5"},
		{routedComment{Kind: ReviewBody, Repository: "o/r", Number: 9, ID: 6}, "https://github.com/o/r/pull/9#pullrequestreview-6"},
		{routedComment{Repository: "o/r", Number: 7}, "https://github.com/o/r/issues/7"},
	} {
		if got := tc.comment.URL(); got != tc.want {
			t.Errorf("URL() = %q, want %q", got, tc.want)
		}
	}
}

// startRoutedCommentRun starts a polled run whose #7 has completed and whose
// prompt agent answers with answer, reporting each prompt it is given.
var routedCommentLog = &syncBuffer{}

func startRoutedCommentRun(t *testing.T, answer string, answerErr error) (*fakeRunner, *fakeCommentClient, *fakeMentionSource, chan string, func()) {
	t.Helper()
	prompts := make(chan string, 8)
	runner, comments, mentions, stop := startPolledRunWith(t, []string{"lsegal"}, "", func(w *Glorp) {
		w.Registry = agents.MustBuiltin()
		w.Out = routedCommentLog
		w.SetPromptAgent(unblockConfig{Agent: "claude"})
		w.promptRun = func(_ context.Context, _ agentSpec, prompt string) (string, error) {
			prompts <- prompt
			return answer, answerErr
		}
	})
	return runner, comments, mentions, prompts, stop
}

func awaitRoutedPrompt(t *testing.T, prompts chan string) string {
	t.Helper()
	select {
	case prompt := <-prompts:
		return prompt
	case <-time.After(2 * time.Second):
		t.Fatal("the comment was not routed to the prompt agent")
	}
	return ""
}

func postMention(comments *fakeCommentClient, mentions *fakeMentionSource, id int64, body string) {
	comments.inject("o/r", 7, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now()})
	mentions.add("o/r", RecentComment{ID: id, Number: 7, Body: body, Author: "lsegal", CreatedAt: time.Now(), UpdatedAt: time.Now()})
}

// A comment on a completed job's issue goes to the prompt agent instead of
// restarting the job, the reply it asks for is posted without any mention,
// and a reply glorp posted never reaches the prompt agent again, even one
// whose text mentions this instance (issue #842).
func TestCommentOnCompletedJobIsRoutedToThePromptAgentWithoutLooping(t *testing.T) {
	runner, comments, mentions, prompts, stop := startRoutedCommentRun(t, "It already shipped in v1.2, @lsegal.\nGLORP comment o/r#7", nil)
	defer stop()

	postMention(comments, mentions, 42, "Did this ship? @/glorp:SELF")
	prompt := awaitRoutedPrompt(t, prompts)
	for _, want := range []string{"look at comment https://github.com/o/r/issues/7#issuecomment-42 on issue o/r#7 and determine next steps", "GLORP retry o/r#7", "GLORP comment o/r#7", "Never @mention anyone"} {
		if !strings.Contains(prompt, want) {
			t.Errorf("routed prompt is missing %q", want)
		}
	}
	var reply string
	deadline := time.Now().Add(2 * time.Second)
	for reply == "" {
		list, _ := comments.ListComments(context.Background(), "o/r", 7)
		for _, comment := range list {
			if isPromptReply(comment.Body) {
				reply = comment.Body
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("the prompt agent's reply was not posted")
		}
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(reply, "It already shipped in v1.2") || strings.Contains(reply, "GLORP") || mentionedLogin(reply, "lsegal") || mentionedLogin(reply, "glorp") {
		t.Errorf("posted reply = %q, want the answer without action lines or mentions", reply)
	}

	// A reply marked as the prompt agent's is exempt whatever it says.
	postMention(comments, mentions, 43, reply+"\n@/glorp:SELF")
	waitForScans(t, mentions, 5)
	select {
	case <-prompts:
		t.Fatal("the prompt agent's own reply was routed back to it")
	default:
	}
	select {
	case n := <-runner.dispatched:
		t.Fatalf("a routed comment restarted #%d", n)
	default:
	}
}

// A direct mention on a job paused on its dependencies, or paused by its
// owner, goes to the prompt agent, and the job stays paused, even when the
// prompt agent fails without deciding (issue #844).
func TestCommentOnPausedJobIsRoutedToThePromptAgent(t *testing.T) {
	for _, tc := range []struct {
		name      string
		kind      string
		issue     Issue
		answerErr error
	}{
		{"paused on dependencies", core.PausedBlocked, blockedOn12("open")[0], nil},
		{"paused by owner", core.PausedManual, Issue{Number: 7, Repository: "o/r"}, nil},
		{"prompt agent fails", core.PausedBlocked, blockedOn12("open")[0], errors.New("exit status 1")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			statePath := filepath.Join(t.TempDir(), "state.json")
			saved := `{"o/r#7": {"status": "paused", "sessionId": "session-7", "agent": "claude", "paused": {"kind": "` + tc.kind + `"}}}`
			if err := os.WriteFile(statePath, []byte(saved), 0600); err != nil {
				t.Fatal(err)
			}
			src := pausingSource([]Issue{tc.issue})
			src.states[7] = OriginatingWorkState{IssueState: "open"}
			src.states[12] = OriginatingWorkState{IssueState: "open"}
			runner := &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 4)}
			close(runner.release)
			comments, mentions := newFakeCommentClient(), newFakeMentionSource()
			prompts := make(chan string, 8)
			logs := &syncBuffer{}
			w := &Glorp{
				Repo: "o/r", Interval: 5 * time.Millisecond, Concurrency: 1, StatePath: statePath,
				Issues: src, Runner: runner, Identity: "SELF", AllowedCommenters: []string{"lsegal"},
				Comments: comments, Mentions: mentions, Out: logs, Registry: agents.MustBuiltin(),
				closureInterval: time.Millisecond,
			}
			w.SetPromptAgent(unblockConfig{Agent: "claude"})
			w.promptRun = func(_ context.Context, _ agentSpec, prompt string) (string, error) {
				prompts <- prompt
				if tc.answerErr != nil {
					return "", tc.answerErr
				}
				return "Still waiting on its dependencies.\nGLORP comment o/r#7", nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() { done <- w.Run(ctx) }()
			defer func() {
				cancel()
				<-done
			}()
			waitForLog(t, logs, "issue #7 paused: ")

			postMention(comments, mentions, 42, "What is the status? @/glorp:SELF")
			awaitRoutedPrompt(t, prompts)
			waitForLog(t, logs, "issue #7 comment https://github.com/o/r/issues/7#issuecomment-42 goes to the prompt agent to decide on")
			if tc.answerErr != nil {
				waitForLog(t, logs, "prompt agent could not decide on comment https://github.com/o/r/issues/7#issuecomment-42; the job stays paused")
			}
			waitForScans(t, mentions, 5)
			select {
			case n := <-runner.dispatched:
				t.Fatalf("a mention on a paused job dispatched #%d:\n%s", n, logs)
			default:
			}
			waitForPaused(t, statePath, 7, tc.kind)
		})
	}
}

// When the prompt agent decides the comment calls for work, it restarts the
// job through Retry.
func TestRoutedCommentCanRestartTheJob(t *testing.T) {
	runner, comments, mentions, prompts, stop := startRoutedCommentRun(t, "The owner wants the docs updated too.\nGLORP retry o/r#7", nil)
	defer stop()

	postMention(comments, mentions, 42, "Please also update the docs @/glorp:SELF")
	awaitRoutedPrompt(t, prompts)
	select {
	case n := <-runner.dispatched:
		if n != 7 {
			t.Fatalf("restart dispatched #%d, want #7", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("the prompt agent's retry did not restart the job:\n%s", routedCommentLog)
	}
}

// A prompt agent that fails without deciding anything does not drop the
// comment: the job is retried as a comment retried it before.
func TestRoutedCommentRetriesTheJobWhenThePromptAgentFails(t *testing.T) {
	runner, comments, mentions, prompts, stop := startRoutedCommentRun(t, "", errors.New("exit status 1"))
	defer stop()

	postMention(comments, mentions, 42, "Please revisit this @/glorp:SELF")
	awaitRoutedPrompt(t, prompts)
	select {
	case n := <-runner.dispatched:
		if n != 7 {
			t.Fatalf("fallback dispatched #%d, want #7", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a failed prompt agent dropped the comment")
	}
}

// Only a routed comment's own job can be replied for.
func TestPromptCommentActionRequiresARoutedComment(t *testing.T) {
	w := &Glorp{Comments: newFakeCommentClient(), Out: &syncBuffer{}}
	if got := w.applyPromptAction(context.Background(), promptAction{Kind: "comment", Key: "o/r#7"}, "hi", nil); !strings.Contains(got, "failed") {
		t.Errorf("comment without a route = %q, want a refusal", got)
	}
	route := &commentRoute{JobKey: "o/r#7", Repository: "o/r", Number: 7, Comment: routedComment{Repository: "o/r", Number: 7, ID: 1}}
	if got := w.applyPromptAction(context.Background(), promptAction{Kind: "comment", Key: "o/r#8"}, "hi", route); !strings.Contains(got, "failed") {
		t.Errorf("comment on another job = %q, want a refusal", got)
	}
}

// Over webhooks too, a reply marked as the prompt agent's triggers nothing,
// while a person's comment on the finished job reaches the prompt agent
// instead of restarting the job (issue #842).
func TestWebhookPromptReplyNeverTriggersAndCommentsAreRouted(t *testing.T) {
	src := &fakeSource{batches: [][]Issue{{{Number: 7}}}}
	runner := &fakeRunner{release: make(chan struct{}), dispatched: make(chan int, 4)}
	comments := newFakeCommentClient()
	events := make(chan WebhookEvent)
	prompts := make(chan string, 4)
	w := &Glorp{
		Repo: "o/r", Interval: time.Hour, Concurrency: 1, StatePath: filepath.Join(t.TempDir(), "state.json"), UseWebhooks: true, Events: events,
		Issues: src, Runner: runner, Identity: "SELF", SelfLogin: "bot", AllowedCommenters: []string{"lsegal"}, Comments: comments,
		Registry: agents.MustBuiltin(), Out: &syncBuffer{},
	}
	w.SetPromptAgent(unblockConfig{Agent: "claude"})
	w.promptRun = func(_ context.Context, _ agentSpec, prompt string) (string, error) {
		prompts <- prompt
		return "Noted.", nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()
	defer func() {
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}()
	<-runner.dispatched
	close(runner.release)
	deadline := time.Now().Add(time.Second)
	for {
		state, err := loadWorkState(w.StatePath)
		if err == nil && state[7].Status == "completed" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("initial run of #7 did not complete")
		}
		time.Sleep(time.Millisecond)
	}
	deliver := func(id int64, body string) {
		comments.inject("o/r", 7, Comment{Body: body, Author: "lsegal", CreatedAt: time.Now()})
		events <- WebhookEvent{Kind: "issue_comment", Action: "created", Repository: "o/r", IssueNumber: 7, CommentID: id, CommentBody: body, CommentAuthor: "lsegal"}
	}
	deliver(80, "@bot @/glorp:SELF it shipped\n\n"+promptReplyMarker)
	select {
	case <-prompts:
		t.Fatal("a prompt-agent reply was routed to the prompt agent")
	case n := <-runner.dispatched:
		t.Fatalf("a prompt-agent reply dispatched #%d", n)
	case <-time.After(50 * time.Millisecond):
	}
	deliver(81, "@bot did this ship?")
	if prompt := awaitRoutedPrompt(t, prompts); !strings.Contains(prompt, "look at comment https://github.com/o/r/issues/7#issuecomment-81 on issue o/r#7") {
		t.Errorf("routed request is wrong:\n%s", prompt)
	}
	select {
	case n := <-runner.dispatched:
		t.Fatalf("a routed comment restarted #%d", n)
	case <-time.After(50 * time.Millisecond):
	}
}
