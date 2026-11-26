package projects

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/domain"
)

// writeLingeringAgent writes a fake claude that prints result, leaves a
// background child holding its stdout, and exits 0, as an agent that started
// a `&` shell does (#1122).
func writeLingeringAgent(t *testing.T, result string) string {
	t.Helper()
	return writeLingeringCLI(t, "claude", result)
}

// writeLingeringCLI writes a fake named binary that prints output, leaves a
// background child holding its stdout, and exits 0.
func writeLingeringCLI(t *testing.T, name, output string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
	previous := agentCommandWaitDelay
	agentCommandWaitDelay = 200 * time.Millisecond
	t.Cleanup(func() { agentCommandWaitDelay = previous })
	binary := filepath.Join(t.TempDir(), name)
	script := "#!/bin/sh\ncase \"$1\" in --version) echo '9.9.9 (Claude Code)'; exit 0;; esac\n" +
		"cat <<'EOF'\n" + output + "\nEOF\nsleep 30 &\nexit 0\n"
	if err := os.WriteFile(binary, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return binary
}

func TestCommandRunnerSucceedsWhenOutputLingersAfterFinalResult(t *testing.T) {
	binary := writeLingeringAgent(t, `{"type":"result","subtype":"success","is_error":false,"result":"Filed issues"}`)
	var output strings.Builder
	runner := CommandRunner{Agent: "claude", ClaudeBinary: binary, Repo: "o/r", Output: &output}
	session := AgentSession{ID: "session-7", Agent: "claude"}
	started := time.Now()
	if err := runner.RunSession(context.Background(), Issue{Number: 7}, session, ignoreSessionUpdate); err != nil {
		t.Fatalf("RunSession() error = %v, want the finished run to succeed", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("run took %s, want it to stop waiting on the leftover child", elapsed)
	}
	if !strings.Contains(output.String(), "process cleanup timed out") {
		t.Fatalf("output = %q, want a cleanup note", output.String())
	}
}

func TestCommandRunnerFailsReadablyWhenOutputLingersWithoutFinalResult(t *testing.T) {
	binary := writeLingeringAgent(t, `{"type":"assistant","message":{"content":[{"type":"text","text":"still working"}]}}`)
	runner := CommandRunner{Agent: "claude", ClaudeBinary: binary, Repo: "o/r"}
	session := AgentSession{ID: "session-7", Agent: "claude"}
	err := runner.RunSession(context.Background(), Issue{Number: 7}, session, ignoreSessionUpdate)
	var failure *domain.RunFailure
	if !errors.As(err, &failure) {
		t.Fatalf("RunSession() error = %#v, want a RunFailure", err)
	}
	if !strings.Contains(err.Error(), "didn't exit cleanly: WaitDelay expired") {
		t.Fatalf("error = %q, want a short WaitDelay message", err)
	}
	if strings.Contains(err.Error(), "http") || strings.Contains(err.Error(), "\n") {
		t.Fatalf("error = %q, want one line with no URL", err)
	}
	if !strings.HasPrefix(failure.BugReportURL, "https://github.com/o/r/issues/new?") {
		t.Fatalf("bug report URL = %q, want a prefilled issue", failure.BugReportURL)
	}
	if !strings.Contains(failure.Output, "still working") {
		t.Fatalf("output = %q, want the agent's last lines", failure.Output)
	}
}

// lingeringOpencodeRunner runs the bundled opencode definition, a generic JSONL
// agent, against a fake that prints output and leaves a child holding stdout.
func lingeringOpencodeRunner(t *testing.T, output string) CommandRunner {
	t.Helper()
	definition := builtinDefinition(t, "opencode")
	definition.Binary = writeLingeringCLI(t, "opencode", output)
	registry, err := clidefs.NewRegistry(definition)
	if err != nil {
		t.Fatal(err)
	}
	return CommandRunner{Agent: "opencode", Definitions: registry, Repo: "o/r"}
}

func TestCommandRunnerSucceedsWhenJSONLAgentLingersAfterDeclaredResult(t *testing.T) {
	runner := lingeringOpencodeRunner(t, `{"type":"text","part":{"text":"Filed issues"}}`+"\n"+`{"type":"step_finish","part":{"reason":"stop"}}`)
	var output strings.Builder
	runner.Output = &output
	session := AgentSession{ID: "ses_7", Agent: "opencode"}
	started := time.Now()
	if err := runner.RunSession(context.Background(), Issue{Number: 7}, session, ignoreSessionUpdate); err != nil {
		t.Fatalf("RunSession() error = %v, want the finished run to succeed", err)
	}
	if elapsed := time.Since(started); elapsed > 10*time.Second {
		t.Fatalf("run took %s, want it to stop waiting on the leftover child", elapsed)
	}
	if !strings.Contains(output.String(), "process cleanup timed out") {
		t.Fatalf("output = %q, want a cleanup note", output.String())
	}
}

func TestCommandRunnerFailsWhenJSONLAgentLingersWithoutDeclaredResult(t *testing.T) {
	runner := lingeringOpencodeRunner(t, `{"type":"text","part":{"text":"still working"}}`+"\n"+`{"type":"step_finish","part":{"reason":"tool-calls"}}`)
	session := AgentSession{ID: "ses_7", Agent: "opencode"}
	err := runner.RunSession(context.Background(), Issue{Number: 7}, session, ignoreSessionUpdate)
	if err == nil || !strings.Contains(err.Error(), "didn't exit cleanly: WaitDelay expired") {
		t.Fatalf("RunSession() error = %v, want a short WaitDelay message", err)
	}
}

func TestProjectTaskOutcomeKeepsFailureDetailsBesideSummary(t *testing.T) {
	runErr := &domain.RunFailure{Err: errors.New("agent failed: exit status 1"), Output: "boom", BugReportURL: "https://github.com/o/r/issues/new?body=x"}
	result := projectTaskOutcome(runErr, nil, "")
	var failure *domain.RunFailure
	if !errors.As(result.err, &failure) || failure.BugReportURL != runErr.BugReportURL || failure.Output != "boom" {
		t.Fatalf("outcome error = %#v, want the failure's output and bug report", result.err)
	}
	if result.summary != "agent failed: exit status 1" || strings.Contains(result.err.Error(), "http") {
		t.Fatalf("outcome = %q / %q, want the short message alone", result.summary, result.err)
	}
}
