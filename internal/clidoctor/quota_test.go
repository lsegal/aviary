package clidoctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
)

// definitionWithQuota builds a minimal valid definition carrying quota.
func definitionWithQuota(t *testing.T, name string, quota clidefs.Quota) clidefs.Definition {
	t.Helper()
	return clidefs.Definition{
		Name: name, Binary: name, Quota: quota,
		Args:    clidefs.Args{Run: []clidefs.Fragment{{Args: []string{"{prompt}"}}}, Resume: []clidefs.Fragment{{Args: []string{"{prompt}"}}}},
		Session: clidefs.Session{Assign: clidefs.AssignNone},
		Output:  clidefs.Output{Format: clidefs.FormatText},
	}
}

func registryWith(t *testing.T, definitions ...clidefs.Definition) *clidefs.Registry {
	t.Helper()
	registry, err := clidefs.NewRegistry(definitions...)
	if err != nil {
		t.Fatal(err)
	}
	return registry
}

// fakeQuotaBinary writes a shell script that prints body on stdout and returns
// its path, so the generic reader can be exercised against a real process.
func fakeQuotaBinary(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake quota binary needs a POSIX shell")
	}
	path := filepath.Join(t.TempDir(), "fake-quota")
	script := "#!/bin/sh\n" + body + "\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestNamedQuotaReadersComeFromTheDefinition checks the reader an agent gets
// is chosen by its quota block rather than by its name, which is what lets a
// config-defined agent report a quota at all.
func TestNamedQuotaReadersComeFromTheDefinition(t *testing.T) {
	registry := registryWith(t,
		definitionWithQuota(t, "codex", clidefs.Quota{Reader: clidefs.QuotaCodex}),
		definitionWithQuota(t, "claude", clidefs.Quota{Reader: clidefs.QuotaClaude}),
		definitionWithQuota(t, "muse", clidefs.Quota{Reader: clidefs.QuotaCommand, Command: []string{"muse", "usage"}, PercentUsed: "used"}),
		definitionWithQuota(t, "quiet", clidefs.Quota{}),
	)
	readers := QuotaReaders(registry, []string{"codex", "claude", "muse", "quiet", "codex", "stranger"}, func(string) string { return "bin" })
	var names []string
	for _, reader := range readers {
		names = append(names, reader.name)
	}
	if got := strings.Join(names, ","); got != "codex,claude,muse,quiet,stranger" {
		t.Fatalf("reader names = %q, want the configured agents deduplicated in order", got)
	}
}

// TestQuotaReadersWithoutASourceSpawnNothing pins the rule that an agent that
// declares no quota costs no process on every poll: the reader an unknown or
// quota-less agent gets reports empty without ever running anything. A reader
// that shelled out would be visible here as a non-empty reading, since the
// executable named does not exist.
func TestQuotaReadersWithoutASourceSpawnNothing(t *testing.T) {
	registry := registryWith(t, definitionWithQuota(t, "quiet", clidefs.Quota{}))
	binaryCalls := 0
	readers := QuotaReaders(registry, []string{"quiet", "stranger"}, func(string) string {
		binaryCalls++
		return "definitely-not-a-real-binary"
	})
	for _, reader := range readers {
		got, err := reader.read(context.Background())
		if got != "" || err != nil {
			t.Fatalf("%s quota = %q (err %v), want untracked and no error", reader.name, got, err)
		}
	}
	if binaryCalls != 0 {
		t.Fatalf("resolved %d binaries for agents with no quota source, want 0", binaryCalls)
	}
	if got := formatNamedQuotas(map[string]string{"quiet": "", "stranger": ""}); got != "quiet: not tracked, stranger: not tracked" {
		t.Fatalf("formatted = %q", got)
	}
}

// TestCommandQuotaReaderFormatsConfiguredFields runs the generic reader
// against a fake binary and checks the configured fields reach the template.
func TestCommandQuotaReaderFormatsConfiguredFields(t *testing.T) {
	binary := fakeQuotaBinary(t, `printf '{"limits":{"used":37,"resets":"Fri 3pm"}}'`)
	spec := clidefs.Quota{
		Reader: clidefs.QuotaCommand, Command: []string{"{binary}", "usage", "--json"},
		PercentUsed: "limits.used", ResetAt: "limits.resets",
		Format: "{percentLeft}% left ({percentUsed}% used) until {resetAt}",
	}
	got, err := readCommandQuota(context.Background(), binary, spec)
	if err != nil {
		t.Fatal(err)
	}
	if want := "63% left (37% used) until Fri 3pm"; got != want {
		t.Fatalf("quota = %q, want %q", got, want)
	}
}

// TestCommandQuotaReaderDefaultsToPercentLeft checks the default template, so
// an agent only has to name the field its CLI reports.
func TestCommandQuotaReaderDefaultsToPercentLeft(t *testing.T) {
	binary := fakeQuotaBinary(t, `printf '{"used":"12.4%%"}'`)
	spec := clidefs.Quota{Reader: clidefs.QuotaCommand, Command: []string{"{binary}"}, PercentUsed: "used"}
	got, err := readCommandQuota(context.Background(), binary, spec)
	if err != nil {
		t.Fatal(err)
	}
	if want := "88% left"; got != want {
		t.Fatalf("quota = %q, want %q", got, want)
	}
}

// TestCommandQuotaReaderRejectsMalformedOutput covers the paths a changed or
// broken agent CLI takes: output that is not JSON, output missing the field
// the definition points at, and a command that fails outright.
func TestCommandQuotaReaderRejectsMalformedOutput(t *testing.T) {
	spec := func(binary string) clidefs.Quota {
		return clidefs.Quota{Reader: clidefs.QuotaCommand, Command: []string{binary}, PercentUsed: "limits.used"}
	}
	cases := []struct {
		name, body, want string
	}{
		{name: "not json", body: `printf 'usage: 40%%'`, want: "did not print JSON"},
		{name: "missing field", body: `printf '{"limits":{}}'`, want: `"limits.used" is missing`},
		{name: "wrong type", body: `printf '{"limits":{"used":{"pct":4}}}'`, want: `"limits.used" is missing`},
		{name: "command fails", body: `echo boom >&2; exit 3`, want: "run quota command"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			binary := fakeQuotaBinary(t, test.body)
			got, err := readCommandQuota(context.Background(), binary, spec(binary))
			if err == nil {
				t.Fatalf("quota = %q, want an error", got)
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want it to mention %q", err, test.want)
			}
		})
	}
}

// TestCommandQuotaReaderTimesOut checks a quota command that hangs is
// abandoned rather than allowed to hold up the poll that asked for it.
func TestCommandQuotaReaderTimesOut(t *testing.T) {
	binary := fakeQuotaBinary(t, "sleep 30")
	spec := clidefs.Quota{Reader: clidefs.QuotaCommand, Command: []string{binary}, PercentUsed: "used", Timeout: "150ms"}
	start := time.Now()
	if got, err := readCommandQuota(context.Background(), binary, spec); err == nil {
		t.Fatalf("quota = %q, want a timeout error", got)
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("read took %s, want it abandoned at the configured timeout", elapsed)
	}
}

// TestCommandQuotaReaderKeepsTheLastGoodReading checks a failed read leaves
// the previous reading in the status bar instead of blanking it, and that the
// one-minute cache stops a second poll re-running the command.
func TestCommandQuotaReaderKeepsTheLastGoodReading(t *testing.T) {
	dir := t.TempDir()
	counter := filepath.Join(dir, "runs")
	binary := fakeQuotaBinary(t, fmt.Sprintf(`printf 'x' >> %q; printf '{"used":25}'`, counter))
	reader := &commandQuotaReader{Binary: binary, Spec: clidefs.Quota{
		Reader: clidefs.QuotaCommand, Command: []string{"{binary}"}, PercentUsed: "used",
	}}
	if got, err := reader.Read(context.Background()); got != "75% left" || err != nil {
		t.Fatalf("first read = %q (err %v)", got, err)
	}
	if got, err := reader.Read(context.Background()); got != "75% left" || err != nil {
		t.Fatalf("cached read = %q (err %v)", got, err)
	}
	runs, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("ran the quota command %d times within the cache window, want 1", len(runs))
	}
	// A later failure must not replace the good reading with an empty one,
	// but it must still report why it failed so the report can say so.
	reader.readAt = time.Time{}
	reader.Spec.Command = []string{filepath.Join(dir, "gone")}
	got, err := reader.Read(context.Background())
	if got != "75% left" {
		t.Fatalf("read after a failure = %q, want the last good reading", got)
	}
	if err == nil {
		t.Fatal("read after a failure reported no error, want the reason the command failed")
	}
}

// TestCodexQuotaArgvIsWhatTheCLIAccepts pins the argv the Codex quota reader
// runs. It once passed `app-server --stdio`, a flag current Codex releases
// reject outright, so every read failed and Codex reported no quota at all
// with nothing on screen to say why. `app-server` speaks JSON-RPC over stdio
// by default, so the working argv is the one that names no transport; this
// test fails loudly if a flag is ever put back.
func TestCodexQuotaArgvIsWhatTheCLIAccepts(t *testing.T) {
	argv := codexQuotaArgv("codex")
	if got := strings.Join(argv, " "); got != "codex app-server" {
		t.Fatalf("codex quota argv = %q, want %q", got, "codex app-server")
	}
	for _, arg := range argv[1:] {
		if strings.HasPrefix(arg, "-") {
			t.Fatalf("codex quota argv passes flag %q; app-server defaults to stdio and rejects transport flags it does not know", arg)
		}
	}
}

// TestCodexQuotaReaderReportsWhyItCouldNotRead checks a Codex read that
// cannot run its binary reports the reason rather than only an empty string,
// which is what lets `aviary agents` say more than "unavailable".
func TestCodexQuotaReaderReportsWhyItCouldNotRead(t *testing.T) {
	reader := &codexQuotaReader{Binary: filepath.Join(t.TempDir(), "definitely-not-codex")}
	quota, err := reader.Read(context.Background())
	if quota != "" {
		t.Fatalf("quota = %q, want empty for a binary that does not exist", quota)
	}
	if err == nil {
		t.Fatal("read reported no error for a missing binary, want the reason it failed")
	}
}

// TestBuiltinQuotaReadersTimeOut checks the built-in Codex and Claude readers
// give up on a CLI that never answers, as a quota command does. They run on the
// poll loop, so one stuck on a connection that died while the machine slept
// used to stop the watch for good.
func TestBuiltinQuotaReadersTimeOut(t *testing.T) {
	binary := fakeQuotaBinary(t, "sleep 30")
	previous := builtinQuotaTimeout
	builtinQuotaTimeout = 150 * time.Millisecond
	defer func() { builtinQuotaTimeout = previous }()
	for name, read := range map[string]func(context.Context, string) (string, error){
		"codex":  readCodexQuota,
		"claude": readClaudeQuota,
	} {
		start := time.Now()
		if got, err := read(context.Background(), binary); err == nil {
			t.Fatalf("%s quota = %q, want a timeout error", name, got)
		}
		if elapsed := time.Since(start); elapsed > 10*time.Second {
			t.Fatalf("%s read took %s, want it abandoned at the timeout", name, elapsed)
		}
	}
}

// claudeWeekOnlyUsage is what current Claude CLIs print for `/usage` when no
// session window is open: a week line and no session line at all. Requiring
// the session line made this report no reading, which the status bar then
// showed as "not tracked".
const claudeWeekOnlyUsage = `{"type":"result","subtype":"success","local_command":"usage","result":"You are currently using your subscription to power your Claude Code usage\n\nCurrent week (all models): 22% used · resets Nov 18, 5:59am (America/Los_Angeles)\nCurrent week (Fable): 0% used · resets Nov 18, 6am (America/Los_Angeles)\n"}`

// TestClaudeQuotaReadsAWeekOnlyReport checks a `/usage` report without a
// session line still yields the week reading it does carry.
func TestClaudeQuotaReadsAWeekOnlyReport(t *testing.T) {
	if got := formatClaudeQuota(claudeUsageText([]byte(claudeWeekOnlyUsage))); got != "week 78% left" {
		t.Fatalf("quota = %q, want %q", got, "week 78% left")
	}
}

// TestClaudeQuotaReaderReadsTheCLIReport runs the Claude reader against a
// fake CLI printing the week-only report, and against one printing a report
// with neither percentage, which must fail with the reason rather than read
// as nothing.
func TestClaudeQuotaReaderReadsTheCLIReport(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	if err := os.WriteFile(path, []byte(claudeWeekOnlyUsage), 0o644); err != nil {
		t.Fatal(err)
	}
	binary := fakeQuotaBinary(t, fmt.Sprintf("cat %q", path))
	if got, err := readClaudeQuota(context.Background(), binary); got != "week 78% left" || err != nil {
		t.Fatalf("quota = %q (err %v), want %q", got, err, "week 78% left")
	}
	broken := fakeQuotaBinary(t, `printf '{"type":"result","result":"no usage data available"}'`)
	got, err := readClaudeQuota(context.Background(), broken)
	if err == nil || !strings.Contains(err.Error(), "missing session and week percentages") {
		t.Fatalf("quota = %q (err %v), want the missing-percentage reason", got, err)
	}
}

// TestCombinedQuotaReaderSaysWhyATrackedReadFailed pins the rule from issue
// #791: an agent with a built-in or command reader whose read fails shows
// "unavailable" and the reason, and only an agent with no reader at all shows
// "not tracked". The failed Claude read used to come back as an empty reading
// and render exactly like an agent aviary has no reader for.
func TestCombinedQuotaReaderSaysWhyATrackedReadFailed(t *testing.T) {
	registry := registryWith(t,
		definitionWithQuota(t, "claude", clidefs.Quota{Reader: clidefs.QuotaClaude}),
		definitionWithQuota(t, "codex", clidefs.Quota{Reader: clidefs.QuotaCodex}),
		definitionWithQuota(t, "quiet", clidefs.Quota{}),
	)
	missing := filepath.Join(t.TempDir(), "definitely-not-installed")
	read := CombinedQuota(QuotaReaders(registry, []string{"claude", "codex", "quiet", "stranger"}, func(string) string { return missing }))
	quotas := read(context.Background())
	for _, name := range []string{"claude", "codex"} {
		if got := quotas[name]; !strings.HasPrefix(got, "unavailable: ") {
			t.Fatalf("%s quota = %q, want unavailable with the reason", name, got)
		}
	}
	for _, name := range []string{"quiet", "stranger"} {
		if got := quotas[name]; got != "" {
			t.Fatalf("%s quota = %q, want empty so it renders as not tracked", name, got)
		}
	}
	formatted := formatNamedQuotas(quotas)
	if strings.Contains(formatted, "claude: not tracked") || strings.Contains(formatted, "codex: not tracked") {
		t.Fatalf("formatted = %q, want failed reads shown as unavailable, not not tracked", formatted)
	}
	if !strings.Contains(formatted, "quiet: not tracked") || !strings.Contains(formatted, "stranger: not tracked") {
		t.Fatalf("formatted = %q, want agents without a reader shown as not tracked", formatted)
	}
}

// TestQuotaReadingKeepsAGoodReading checks a reading wins over the error a
// later failed read reports, since the readers keep their last good value.
func TestQuotaReadingKeepsAGoodReading(t *testing.T) {
	if got := QuotaReading("week 78% left", true, fmt.Errorf("boom")); got != "week 78% left" {
		t.Fatalf("reading = %q", got)
	}
	if got := QuotaReading("", true, nil); got != "unavailable" {
		t.Fatalf("reading = %q, want unavailable", got)
	}
	if got := QuotaReading("", false, fmt.Errorf("boom")); got != "" {
		t.Fatalf("reading = %q, want empty for an agent with no reader", got)
	}
}

// TestScanCodexQuotaReadsTheRateLimitReply checks the app-server reply parser
// skips the initialize reply and stray output, formats the rate limit reply,
// and reports a failed or missing rate limit reply instead of an empty quota.
func TestScanCodexQuotaReadsTheRateLimitReply(t *testing.T) {
	weekly := `{"id":1,"result":{}}` + "\n" + "not json\n" +
		`{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":30,"windowDurationMins":10080}}}}` + "\n"
	if got, err := scanCodexQuota(strings.NewReader(weekly)); got != "weekly 70% left" || err != nil {
		t.Fatalf("weekly quota = %q (err %v)", got, err)
	}
	session := `{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":120,"windowDurationMins":300}}}}`
	if got, err := scanCodexQuota(strings.NewReader(session)); got != "quota 0% left" || err != nil {
		t.Fatalf("over-limit quota = %q (err %v), want it clamped at 0%%", got, err)
	}
	if got, err := scanCodexQuota(strings.NewReader(`{"id":2,"result":{"rateLimits":{}}}`)); got != "" || err != nil {
		t.Fatalf("quota without a primary limit = %q (err %v), want empty", got, err)
	}
	_, err := scanCodexQuota(strings.NewReader(`{"id":2,"error":{"message":"not signed in"}}`))
	if err == nil || !strings.Contains(err.Error(), "not signed in") {
		t.Fatalf("error reply err = %v, want the server's error", err)
	}
	_, err = scanCodexQuota(strings.NewReader(`{"id":1,"result":{}}`))
	if err == nil || !strings.Contains(err.Error(), "not received") {
		t.Fatalf("missing reply err = %v, want not received", err)
	}
}

// TestCodexQuotaReaderReadsAndCachesTheAppServerReply runs the Codex reader
// against a fake app-server that answers the rate limit request, and checks a
// second read inside the cache window does not start the CLI again.
func TestCodexQuotaReaderReadsAndCachesTheAppServerReply(t *testing.T) {
	counter := filepath.Join(t.TempDir(), "runs")
	binary := fakeQuotaBinary(t, fmt.Sprintf(`printf 'x' >> %q
while read -r line; do
  case "$line" in
    *rateLimits/read*) printf '%%s\n' '{"id":2,"result":{"rateLimits":{"primary":{"usedPercent":45}}}}' ;;
  esac
done`, counter))
	reader := &codexQuotaReader{Binary: binary}
	for range 2 {
		if got, err := reader.Read(context.Background()); got != "quota 55% left" || err != nil {
			t.Fatalf("quota = %q (err %v), want %q", got, err, "quota 55% left")
		}
	}
	runs, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("ran codex %d times within the cache window, want 1", len(runs))
	}
}

// TestClaudeQuotaReaderCachesItsReading checks the Claude reader keeps a good
// reading for the cache window instead of re-running the CLI on every poll.
func TestClaudeQuotaReaderCachesItsReading(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "usage.json")
	if err := os.WriteFile(path, []byte(claudeWeekOnlyUsage), 0o644); err != nil {
		t.Fatal(err)
	}
	counter := filepath.Join(dir, "runs")
	reader := &claudeQuotaReader{Binary: fakeQuotaBinary(t, fmt.Sprintf("printf 'x' >> %q; cat %q", counter, path))}
	for range 2 {
		if got, err := reader.Read(context.Background()); got != "week 78% left" || err != nil {
			t.Fatalf("quota = %q (err %v)", got, err)
		}
	}
	runs, err := os.ReadFile(counter)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 {
		t.Fatalf("ran claude %d times within the cache window, want 1", len(runs))
	}
}

// TestFormatCommandQuotaFieldTypes covers the field shapes a quota command
// may print: a numeric reset timestamp, a quoted percentage, values outside
// 0-100, and fields that are absent, blank, or not a value at all.
func TestFormatCommandQuotaFieldTypes(t *testing.T) {
	document := map[string]any{
		"used":   float64(130),
		"quoted": " 40% ",
		"bad":    "lots",
		"reset":  float64(1700000000),
		"blank":  "  ",
		"list":   []any{"a"},
		"nested": "leaf",
	}
	cases := []struct {
		name    string
		spec    clidefs.Quota
		want    string
		wantErr string
	}{
		{name: "numeric reset", spec: clidefs.Quota{PercentUsed: "used", ResetAt: "reset", Format: "{percentLeft}% left until {resetAt}"}, want: "0% left until 1700000000"},
		{name: "quoted percent", spec: clidefs.Quota{PercentUsed: "quoted", Format: "{percentUsed}% used"}, want: "40% used"},
		{name: "unparsable percent", spec: clidefs.Quota{PercentUsed: "bad"}, wantErr: `"bad" is missing or not a number`},
		{name: "list percent", spec: clidefs.Quota{PercentUsed: "list"}, wantErr: `"list" is missing or not a number`},
		{name: "empty path", spec: clidefs.Quota{PercentUsed: " "}, wantErr: "missing or not a number"},
		{name: "path through a leaf", spec: clidefs.Quota{PercentUsed: "nested.deeper"}, wantErr: "missing or not a number"},
		{name: "blank reset", spec: clidefs.Quota{ResetAt: "blank", Format: "resets {resetAt}"}, wantErr: `"blank" is missing`},
		{name: "list reset", spec: clidefs.Quota{ResetAt: "list", Format: "resets {resetAt}"}, wantErr: `"list" is missing`},
		{name: "absent reset", spec: clidefs.Quota{ResetAt: "gone", Format: "resets {resetAt}"}, wantErr: `"gone" is missing`},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, err := formatCommandQuota(test.spec, document)
			if test.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), test.wantErr) {
					t.Fatalf("quota = %q (err %v), want error mentioning %q", got, err, test.wantErr)
				}
				return
			}
			if err != nil || got != test.want {
				t.Fatalf("quota = %q (err %v), want %q", got, err, test.want)
			}
		})
	}
}

// TestReadQuotaUsesTheAgentsReader checks ReadQuota reads one agent through
// its definition's reader, trims the reading, and reads nothing for an agent
// that tracks no quota.
func TestReadQuotaUsesTheAgentsReader(t *testing.T) {
	binary := fakeQuotaBinary(t, `printf '{"used":10}'`)
	registry := registryWith(t,
		definitionWithQuota(t, "muse", clidefs.Quota{Reader: clidefs.QuotaCommand, Command: []string{"{binary}"}, PercentUsed: "used", Format: " {percentLeft}% left "}),
		definitionWithQuota(t, "quiet", clidefs.Quota{}),
	)
	if got, err := ReadQuota(context.Background(), registry, "muse", binary); got != "90% left" || err != nil {
		t.Fatalf("muse quota = %q (err %v), want %q", got, err, "90% left")
	}
	if got, err := ReadQuota(context.Background(), registry, "quiet", binary); got != "" || err != nil {
		t.Fatalf("quiet quota = %q (err %v), want nothing", got, err)
	}
}
