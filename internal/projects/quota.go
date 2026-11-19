package projects

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/projects/process"
)

type codexPrimaryRateLimit struct {
	UsedPercent        int    `json:"usedPercent"`
	WindowDurationMins *int64 `json:"windowDurationMins"`
}

type codexRateLimitResponse struct {
	RateLimits struct {
		Primary *codexPrimaryRateLimit `json:"primary"`
	} `json:"rateLimits"`
}

type codexQuotaReader struct {
	Binary string
	mu     sync.Mutex
	quota  string
	err    error
	readAt time.Time
}

func (r *codexQuotaReader) Read(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.readAt) < time.Minute {
		return r.quota, r.err
	}
	quota, err := readCodexQuota(ctx, r.Binary)
	if err == nil {
		r.quota = quota
	}
	r.err = err
	r.readAt = time.Now()
	return r.quota, r.err
}

// codexQuotaArgv is the argv the Codex quota reader runs. `app-server` speaks
// JSON-RPC over stdio by default; it once took an explicit `--stdio` flag,
// which current releases reject outright, taking the whole reading with it.
// The transport is the default, so naming it is what breaks, not what works.
func codexQuotaArgv(binary string) []string {
	return []string{binary, "app-server"}
}

// builtinQuotaTimeout bounds one reading by the built-in Codex and Claude
// readers, as a definition's own timeout bounds a quota command. Quotas are
// read on the poll loop, so a CLI stuck on a connection that went dead while
// the machine slept would otherwise stop the watch for good. It
// is a variable only so tests need not spend it.
var builtinQuotaTimeout = agents.DefaultQuotaTimeout

func readCodexQuota(ctx context.Context, binary string) (string, error) {
	argv := codexQuotaArgv(binary)
	ctx, cancel := context.WithTimeout(ctx, builtinQuotaTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.WaitDelay = quotaCommandWaitDelay
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := process.Start(cmd); err != nil {
		return "", err
	}
	defer func() { _ = process.Stop(cmd) }()
	for _, request := range codexQuotaRequests() {
		if _, err := fmt.Fprintln(stdin, request); err != nil {
			return "", err
		}
	}
	// Keep stdin open while reading: app-server can finish the request only
	// after it has sent the response, and treats EOF as a client disconnect.
	// The read runs aside so the timeout can abandon it: the deferred Stop
	// then takes down the whole tree, which closes the pipe it is reading.
	type reading struct {
		quota string
		err   error
	}
	result := make(chan reading, 1)
	go func() {
		quota, err := scanCodexQuota(stdout)
		result <- reading{quota, err}
	}()
	select {
	case r := <-result:
		return r.quota, r.err
	case <-ctx.Done():
		return "", fmt.Errorf("codex rate limit response not received: %w", ctx.Err())
	}
}

// scanCodexQuota reads app-server's replies until the rate limit one arrives.
func scanCodexQuota(stdout io.Reader) (string, error) {
	scanner := bufio.NewScanner(stdout)
	for scanner.Scan() {
		var message struct {
			ID     int                     `json:"id"`
			Result *codexRateLimitResponse `json:"result"`
			Error  json.RawMessage         `json:"error"`
		}
		if json.Unmarshal(scanner.Bytes(), &message) != nil || message.ID != 2 {
			continue
		}
		if message.Result == nil {
			return "", fmt.Errorf("codex rate limit request failed: %s", message.Error)
		}
		return formatCodexQuota(message.Result.RateLimits.Primary), nil
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("codex rate limit response not received")
}

func codexQuotaRequests() []string {
	return []string{
		`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"aviary","title":"aviary","version":"dev"}}}`,
		`{"method":"initialized","params":{}}`,
		`{"id":2,"method":"account/rateLimits/read"}`,
	}
}

func formatCodexQuota(primary *codexPrimaryRateLimit) string {
	if primary == nil {
		return ""
	}
	remaining := max(0, 100-primary.UsedPercent)
	window := "quota"
	if primary.WindowDurationMins != nil && *primary.WindowDurationMins >= 7*24*60 {
		window = "weekly"
	}
	return fmt.Sprintf("%s %d%% left", window, remaining)
}

type claudeQuotaReader struct {
	Binary string
	mu     sync.Mutex
	quota  string
	err    error
	readAt time.Time
}

func (r *claudeQuotaReader) Read(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.readAt) < time.Minute {
		return r.quota, r.err
	}
	quota, err := readClaudeQuota(ctx, r.Binary)
	if err == nil {
		r.quota = quota
	}
	r.err = err
	r.readAt = time.Now()
	return r.quota, r.err
}

// readClaudeQuota runs the local `/usage` slash command, which reports the
// account's current session/week usage without making a billed API request
// (unlike a normal prompt, it costs no tokens and no money).
func readClaudeQuota(ctx context.Context, binary string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, builtinQuotaTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "--print", "--output-format=json")
	cmd.WaitDelay = quotaCommandWaitDelay
	cmd.Stdin = strings.NewReader(claudeQuotaRequest())
	out, err := process.Output(cmd)
	if err != nil {
		return "", err
	}
	quota := formatClaudeQuota(claudeUsageText(out))
	if quota == "" {
		return "", fmt.Errorf("claude usage response missing session and week percentages")
	}
	return quota, nil
}

// claudeUsageText pulls the usage report out of what the CLI printed. The
// result is meant to be one JSON object, but the CLI also prints diagnostics
// from whatever it loaded on the way -- MCP server warnings, for one -- onto
// the same stream, and decoding the whole stream strictly turns a perfectly
// readable usage report into "unavailable". So the JSON values on stdout are
// decoded one at a time and the first non-empty result wins; when none of it
// decodes, the raw output is used instead, because the usage lines the
// patterns look for survive JSON encoding unchanged.
func claudeUsageText(out []byte) string {
	decoder := json.NewDecoder(bytes.NewReader(out))
	for {
		var response struct {
			Result string `json:"result"`
		}
		if err := decoder.Decode(&response); err != nil {
			break
		}
		if strings.TrimSpace(response.Result) != "" {
			return response.Result
		}
	}
	return string(out)
}

// claudeQuotaRequest is the "/usage" slash command: a local status report
// handled by the CLI itself, so unlike a normal prompt it costs no tokens
// and makes no billed API request.
func claudeQuotaRequest() string {
	return "/usage"
}

var (
	claudeSessionUsagePattern = regexp.MustCompile(`Current session:\s*(\d+)% used`)
	claudeWeekUsagePattern    = regexp.MustCompile(`Current week \(all models\):\s*(\d+)% used`)
)

// formatClaudeQuota renders whichever of the session and week lines the
// report carries. Either one is a reading: the CLI leaves the session line out
// entirely when no session window is open, and requiring it turned a report
// with a perfectly good week percentage into no reading at all.
func formatClaudeQuota(usageText string) string {
	var parts []string
	for _, window := range []struct {
		label   string
		pattern *regexp.Regexp
	}{
		{"session", claudeSessionUsagePattern},
		{"week", claudeWeekUsagePattern},
	} {
		match := window.pattern.FindStringSubmatch(usageText)
		if match == nil {
			continue
		}
		if used, err := strconv.Atoi(match[1]); err == nil {
			parts = append(parts, fmt.Sprintf("%s %d%% left", window.label, max(0, 100-used)))
		}
	}
	return strings.Join(parts, ", ")
}

// quotaCommandWaitDelay is how long a cancelled quota command is given to let
// go of its output pipe before the read gives up on it.
const quotaCommandWaitDelay = time.Second

// commandQuotaReader is the generic quota source: it runs the argv a
// definition names, reads the JSON that argv prints, and renders the fields
// the definition points at into the definition's own template. It is what
// lets an agent aviary has never heard of report a quota, and it caches on the
// same one-minute terms as the built-in readers so a poll never re-runs it.
type commandQuotaReader struct {
	Binary string
	Spec   agents.Quota
	mu     sync.Mutex
	quota  string
	err    error
	readAt time.Time
}

func (r *commandQuotaReader) Read(ctx context.Context) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if time.Since(r.readAt) < time.Minute {
		return r.quota, r.err
	}
	quota, err := readCommandQuota(ctx, r.Binary, r.Spec)
	if err == nil {
		r.quota = quota
	}
	r.err = err
	r.readAt = time.Now()
	return r.quota, r.err
}

// readCommandQuota runs one quota command under the definition's timeout. A
// quota is a status-bar nicety, so a command that hangs is abandoned rather
// than allowed to hold up the poll that asked for it.
func readCommandQuota(ctx context.Context, binary string, spec agents.Quota) (string, error) {
	argv := spec.Argv(binary)
	if len(argv) == 0 {
		return "", fmt.Errorf("quota command is empty")
	}
	ctx, cancel := context.WithTimeout(ctx, spec.TimeoutDuration())
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// Cancelling kills the command, but a grandchild it left holding the
	// output pipe would keep the read waiting on a process that is already
	// gone. WaitDelay closes the pipe behind it so the timeout is the timeout.
	cmd.WaitDelay = quotaCommandWaitDelay
	out, err := process.Output(cmd)
	if err != nil {
		return "", fmt.Errorf("run quota command %s: %w", argv[0], err)
	}
	var document any
	if err := json.Unmarshal(out, &document); err != nil {
		return "", fmt.Errorf("quota command %s did not print JSON: %w", argv[0], err)
	}
	return formatCommandQuota(spec, document)
}

// formatCommandQuota renders the definition's template from the decoded
// document. A field the template asks for and the document does not have is
// an error rather than an empty substitution, so a changed output format
// leaves the last good reading up instead of replacing it with nonsense.
func formatCommandQuota(spec agents.Quota, document any) (string, error) {
	text := spec.FormatTemplate()
	if strings.Contains(text, "{percent") {
		used, ok := quotaNumberAt(document, spec.PercentUsed)
		if !ok {
			return "", fmt.Errorf("quota field %q is missing or not a number", spec.PercentUsed)
		}
		rounded := int(math.Round(used))
		text = strings.ReplaceAll(text, "{percentUsed}", strconv.Itoa(rounded))
		text = strings.ReplaceAll(text, "{percentLeft}", strconv.Itoa(min(100, max(0, 100-rounded))))
	}
	if strings.Contains(text, "{resetAt}") {
		reset, ok := quotaStringAt(document, spec.ResetAt)
		if !ok {
			return "", fmt.Errorf("quota field %q is missing", spec.ResetAt)
		}
		text = strings.ReplaceAll(text, "{resetAt}", reset)
	}
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("quota command produced an empty reading")
	}
	return text, nil
}

// quotaValueAt walks a dotted path through the decoded JSON document. Only
// object keys are traversed: a definition points at a named field, not into
// an array, which keeps the path syntax something a config file can be read
// aloud from.
func quotaValueAt(document any, path string) (any, bool) {
	if strings.TrimSpace(path) == "" {
		return nil, false
	}
	value := document
	for _, key := range strings.Split(path, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, false
		}
		if value, ok = object[key]; !ok {
			return nil, false
		}
	}
	return value, true
}

// quotaNumberAt reads a numeric field, accepting a JSON string that holds a
// number so a CLI that quotes its percentages still works.
func quotaNumberAt(document any, path string) (float64, bool) {
	value, ok := quotaValueAt(document, path)
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case string:
		number, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(typed), "%"), 64)
		return number, err == nil
	}
	return 0, false
}

// quotaStringAt reads a text field, rendering a number as one so a reset time
// expressed as a Unix timestamp is still printable.
func quotaStringAt(document any, path string) (string, bool) {
	value, ok := quotaValueAt(document, path)
	if !ok {
		return "", false
	}
	switch typed := value.(type) {
	case string:
		text := strings.TrimSpace(typed)
		return text, text != ""
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	}
	return "", false
}

// namedQuotaReader reads quota text for a single named agent. The read
// reports why it could not answer as well as what it read, so a reader whose
// command fails is something a caller can show rather than an empty string
// indistinguishable from an agent that tracks no quota at all.
type namedQuotaReader struct {
	name string
	read func(context.Context) (string, error)
	// tracked says the agent's definition names a quota source, so an empty
	// reading is a failure to report rather than the not-tracked default.
	tracked bool
}

// namedQuotaReaders builds one quota reader per configured agent, deduplicated
// by name. Which reader an agent gets comes from its definition's quota block
// rather than from its name, so a config-defined agent can report a quota
// without a code change. An agent whose definition names no quota source --
// the default, and what an agent the registry does not know falls back to --
// still gets an entry so the UI can show that its quota is not tracked, but
// that entry never spawns a process.
func namedQuotaReaders(registry *agents.Registry, names []string, binaryFor func(string) string) []namedQuotaReader {
	seen := make(map[string]bool, len(names))
	readers := make([]namedQuotaReader, 0, len(names))
	untracked := func(context.Context) (string, error) { return "", nil }
	for _, name := range names {
		if seen[name] {
			continue
		}
		seen[name] = true
		definition, ok := registry.Lookup(name)
		if !ok {
			readers = append(readers, namedQuotaReader{name: name, read: untracked})
			continue
		}
		var read func(context.Context) (string, error)
		tracked := true
		switch definition.Quota.ReaderName() {
		case agents.QuotaCodex:
			read = (&codexQuotaReader{Binary: binaryFor(name)}).Read
		case agents.QuotaClaude:
			read = (&claudeQuotaReader{Binary: binaryFor(name)}).Read
		case agents.QuotaCommand:
			read = (&commandQuotaReader{Binary: binaryFor(name), Spec: definition.Quota}).Read
		default:
			read, tracked = untracked, false
		}
		readers = append(readers, namedQuotaReader{name: name, read: read, tracked: tracked})
	}
	return readers
}

// combinedQuotaReader reads every named reader's quota and returns the
// results keyed by agent name. A reader that has a source but no reading
// reports itself unavailable, with the reason when it has one, so a broken
// read is never shown as the "not tracked" of an agent with no source at all
// .
func combinedQuotaReader(readers []namedQuotaReader) func(context.Context) map[string]string {
	return func(ctx context.Context) map[string]string {
		result := make(map[string]string, len(readers))
		for _, reader := range readers {
			quota, err := reader.read(ctx)
			result[reader.name] = quotaReading(quota, reader.tracked, err)
		}
		return result
	}
}

// quotaReading is the text one agent's quota shows: the reading itself, or,
// for an agent that has a quota source and no reading, "unavailable" and why.
// It is empty only for an agent with no source, which every surface renders
// as "not tracked".
func quotaReading(quota string, tracked bool, err error) string {
	if text := strings.TrimSpace(quota); text != "" || !tracked {
		return text
	}
	if err != nil {
		return "unavailable: " + firstLine(strings.TrimSpace(err.Error()))
	}
	return "unavailable"
}

func formatNamedQuotas(quotas map[string]string) string {
	names := make([]string, 0, len(quotas))
	for name := range quotas {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, name := range names {
		text := strings.TrimSpace(quotas[name])
		if text == "" {
			text = "not tracked"
		}
		parts[i] = name + ": " + text
	}
	return strings.Join(parts, ", ")
}

func quotaText(snapshot EngineSnapshot) string {
	if len(snapshot.Quotas) > 0 {
		return "quota: " + formatNamedQuotas(snapshot.Quotas)
	}
	if strings.TrimSpace(snapshot.Quota) != "" {
		return "quota: " + snapshot.Quota
	}
	if snapshot.TokenLimit > 0 {
		return fmt.Sprintf("tokens: %d/%d", snapshot.TokensUsed, snapshot.TokenLimit)
	}
	return "quota: unavailable"
}
