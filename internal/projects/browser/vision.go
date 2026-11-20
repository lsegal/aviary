package browser

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/clidefs"
	"github.com/lsegal/aviary/internal/llm"
)

// The screenshot fallback exists for one situation only: GitHub changed its
// markup and the DOM extractor in browserissues.go (or browserproject.go) no
// longer recognises a page that plainly does contain items. It is a bridge
// until extraction is fixed in code, not a strategy, so every part of it is
// bounded. In the steady state a watch run makes zero vision calls no matter
// how long it polls, for issue pages and project boards alike.
const (
	// visionCooldown is the minimum time between screenshots of the
	// same target. A poll loop hitting a permanently broken page
	// would otherwise queue an agent call every tick.
	visionCooldown = 10 * time.Minute
	// visionRunLimit is how many vision calls a single run may make
	// before the fallback switches itself off for the rest of the run. The cap
	// is shared across every target and every page kind: if three screenshots
	// did not recover a list, a fourth will not either; the extractor needs
	// fixing.
	visionRunLimit = 3
	// visionTimeout bounds one agent invocation, so a wedged agent
	// cannot stall the poll loop it was called from.
	visionTimeout = 2 * time.Minute
)

// screenshotter is the part of a tab the fallback needs. It is kept
// separate from Page because screenshots are only ever taken on the
// failure path: a page source that cannot produce one simply has no fallback.
type screenshotter interface {
	Screenshot() ([]byte, error)
}

// VisionRef is one issue the agent read off a screenshot. Repository is
// empty for a repository target, where the page itself fixes the repository
// and a bare number is unambiguous; it is always set for a project board,
// which spans repositories and where a bare number cannot be turned back into
// an addressable issue.
//
// Status is the board's Status column for the item, and is only ever set for a
// project target: it is what the --ready-state gate reads, so a recovered board
// item without it is addressable but undispatchable (issue #398). It is empty
// when the board itself shows the item no status, which is a real answer rather
// than a parse failure.
type VisionRef struct {
	Repository string
	Number     int
	Status     string
}

// Vision is the budget around the screenshot-to-agent fallback. It owns
// the accounting (per-target cooldown, per-run cap) so the issue and board
// sources cannot spend the budget by accident, and it logs every call it does
// make with the reason and the running count so a runaway is obvious in the
// dashboard. One instance is shared by every target, so the per-run cap is a
// budget for the whole run rather than per page kind.
type Vision struct {
	cooldown time.Duration
	limit    int
	// ask performs one vision call and returns the issues the agent read off
	// the screenshot. qualified selects the answer shape it must ask for and
	// accept. Tests replace it; production wires it to an agent invocation.
	ask  func(ctx context.Context, screenshot []byte, pageURL string, qualified bool) ([]VisionRef, error)
	logf func(string, ...interface{})
	now  func() time.Time

	mu sync.Mutex
	// calls counts vision calls attempted this run, including ones that failed
	// or returned nothing usable: a failing call costs the same as a good one.
	calls int
	// disabled latches once the per-run cap is reached and is never cleared.
	disabled bool
	// lastCall is when each target last spent a screenshot.
	lastCall map[string]time.Time
}

// NewVision builds the fallback for a watch run, dispatching screenshots
// to the same agent the run is otherwise configured to use.
func NewVision(runner AgentRunner, logf func(string, ...interface{})) *Vision {
	agent := visionAgent{Runner: runner}
	return &Vision{
		cooldown: visionCooldown,
		limit:    visionRunLimit,
		ask:      agent.Ask,
		logf:     logf,
		now:      time.Now,
		lastCall: map[string]time.Time{},
	}
}

// Recover spends one unit of budget, if any is left, to read the issues off a
// screenshot of a page the DOM extractor could not read. qualified asks the
// agent for OWNER/REPO#NUMBER strings, which is what a project board needs;
// without it the agent is asked for bare numbers, which is what an issues page
// needs. It returns nil whenever the budget forbids the call, the screenshot
// fails, or the agent's answer is not the strict shape it was asked for;
// nothing is ever retried.
//
// A nil receiver means the fallback is off, which is the default.
func (v *Vision) Recover(ctx context.Context, target, pageURL, reason string, screenshot func() ([]byte, error), qualified bool) []VisionRef {
	if v == nil || screenshot == nil {
		return nil
	}
	call, last, ok := v.reserve(target)
	if !ok {
		return nil
	}
	v.log("browser vision: reading %s from a screenshot because the page could not be read (%s) [call %d of %d this run]", pageURL, reason, call, v.limit)
	if last {
		v.log("browser vision: that was the last of %d screenshot(s) allowed per run; the fallback is off for the rest of this run and the page extractor needs fixing", v.limit)
	}
	image, err := screenshot()
	if err != nil {
		v.log("browser vision: could not capture a screenshot of %s: %v", pageURL, err)
		return nil
	}
	callCtx, cancel := context.WithTimeout(ctx, visionTimeout)
	defer cancel()
	refs, err := v.ask(callCtx, image, pageURL, qualified)
	if err != nil {
		v.log("browser vision: discarded the agent's answer for %s: %v", pageURL, err)
		return nil
	}
	if len(refs) == 0 {
		v.log("browser vision: the agent found no issue numbers on %s", pageURL)
		return nil
	}
	v.log("browser vision: recovered %d issue number(s) from %s; fix the page extractor rather than relying on this", len(refs), pageURL)
	return refs
}

// reserve applies the budget and, when a call is allowed, records it before it
// happens. Charging up front is deliberate: a call that panics, times out, or
// returns garbage must still count against the cap and the cooldown, otherwise
// a persistently broken page would be free to retry forever.
// It reports the call's number and whether that call exhausted the run's cap.
func (v *Vision) reserve(target string) (int, bool, bool) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.disabled {
		return 0, false, false
	}
	now := v.now()
	if last, ok := v.lastCall[target]; ok && now.Sub(last) < v.cooldown {
		return 0, false, false
	}
	v.lastCall[target] = now
	v.calls++
	if v.calls >= v.limit {
		v.disabled = true
	}
	return v.calls, v.disabled, true
}

func (v *Vision) log(format string, args ...interface{}) {
	if v.logf != nil {
		v.logf(format, args...)
	}
}

// AgentSpec names the one-shot coding-agent invocation a vision recovery is
// made with: which agent, run how, from which executable.
type AgentSpec struct {
	// Name is the agent provider, which decides how the screenshot is handed
	// over.
	Name string
	// Definition is that provider's agent definition, which carries the argv
	// template the invocation is rendered from. The root package supplies the
	// one the run was configured with; a spec that names only an agent falls
	// back to the built-in definition of that name.
	Definition *clidefs.Definition
	// Binary is the executable that provider is invoked through.
	Binary string
	// Model and Level are the model and reasoning level to ask for. Both are
	// empty when the agent's own defaults are wanted.
	Model, Level string
	// Yolo bypasses the agent's approval prompts, matching the run's own
	// setting.
	Yolo bool
}

// AgentRunner supplies and runs that invocation. The root package implements
// it over the same runner that dispatches issues, so a screenshot is read by
// the agent the run is already configured for, and so the browser driver needs
// to know nothing about how Aviary spawns clidefs.
type AgentRunner interface {
	// VisionAgent reports the agent to ask, read without advancing the
	// round-robin cursor that load balances real issue dispatch.
	VisionAgent() AgentSpec
	// RunAgent runs binary with args and returns everything it wrote.
	RunAgent(ctx context.Context, binary string, args []string) (string, error)
	// RunInProcess runs one turn of an in-process agent definition with the
	// image at mediaURL and returns the reply it wrote.
	RunInProcess(ctx context.Context, spec AgentSpec, prompt, mediaURL string) (string, error)
}

// visionAgent turns one screenshot into a list of issues by invoking the
// run's configured agent on it.
type visionAgent struct {
	Runner AgentRunner
}

// visionPrompt asks for a bare JSON array and nothing else. Anything
// else the agent says is discarded rather than re-prompted, so the instruction
// is deliberately narrow. A project board is asked for OWNER/REPO#NUMBER
// strings because its rows come from several repositories and a bare number
// there names no particular issue.
func visionPrompt(imagePath, pageURL string, qualified bool) string {
	if qualified {
		return fmt.Sprintf(`The image at %s is a screenshot of the GitHub project board at %s.

The board's items come from several repositories, so a bare issue number is not enough, and each item's Status column decides whether it is ready to work on. Reply with a strict JSON array of objects, one per issue listed on that page, each with exactly two fields: "ref", the issue as "OWNER/REPO#NUMBER", and "status", the item's Status column copied verbatim as it is written on the board. Reply with that and absolutely nothing else: no prose, no explanation, no markdown code fence. For example: [{"ref":"octocat/hello-world#412","status":"Todo"},{"ref":"octocat/spoon-knife#398","status":"In Progress"}]

If an item shows no status at all, use "" for its status; do not guess one. If the board shows no issues, reply with []. Do not open a browser, run commands, or read anything other than the image.`, imagePath, pageURL)
	}
	return fmt.Sprintf(`The image at %s is a screenshot of the GitHub issue list at %s.

Reply with a strict JSON array of the issue numbers listed on that page, and absolutely nothing else: no prose, no explanation, no markdown code fence. For example: [412,398,377]

If the page shows no issues, reply with []. Do not open a browser, run commands, or read anything other than the image.`, imagePath, pageURL)
}

// Ask writes the screenshot where the agent can read it, runs the agent once,
// and parses its answer.
func (a visionAgent) Ask(ctx context.Context, screenshot []byte, pageURL string, qualified bool) ([]VisionRef, error) {
	dir, err := os.MkdirTemp("", "aviary-vision-")
	if err != nil {
		return nil, fmt.Errorf("write screenshot: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	path := filepath.Join(dir, "issues.png")
	if err := os.WriteFile(path, screenshot, 0o600); err != nil {
		return nil, fmt.Errorf("write screenshot: %w", err)
	}
	spec := a.Runner.VisionAgent()
	var output string
	if definition := spec.definition(); definition != nil && definition.InProcess {
		mediaURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(screenshot)
		output, err = a.Runner.RunInProcess(ctx, spec, visionPrompt(path, pageURL, qualified), mediaURL)
	} else {
		output, err = a.Runner.RunAgent(ctx, spec.Binary, visionArgs(spec, path, pageURL, qualified))
	}
	if err != nil {
		return nil, fmt.Errorf("agent failed: %w", err)
	}
	return parseVisionRefs(output, qualified)
}

// visionArgs builds the one-shot agent invocation from the agent's own
// definition: how the image is handed over is the definition's vision template,
// which for Codex takes the image as a flag and for Claude leaves it to the
// path named in the prompt. Neither is asked for structured streaming output,
// because the answer is expected to be one line. An agent whose definition
// declares no vision template renders nothing, and the caller reports that
// rather than invoking it with a bare prompt.
func visionArgs(spec AgentSpec, imagePath, pageURL string, qualified bool) []string {
	definition := spec.definition()
	if definition == nil {
		return nil
	}
	return definition.Render(clidefs.ModeVision, clidefs.Values{
		Prompt: visionPrompt(imagePath, pageURL, qualified),
		Image:  imagePath,
		Model:  spec.Model,
		Level:  spec.Level,
		Yolo:   spec.Yolo,
	})
}

// definition resolves the definition the invocation is rendered from.
func (s AgentSpec) definition() *clidefs.Definition {
	if s.Definition != nil {
		return s.Definition
	}
	// A spec that names an agent without carrying its definition resolves
	// it through core's CLI providers.
	if found, ok := llm.CLIProvider(s.Name); ok {
		return &found
	}
	return nil
}

// visionRefPattern is the only qualified answer shape accepted: an
// owner, a repository, and a number, with nothing around them. It is as strict
// as the bare-number parser, so a board answer that drops the repository (or
// buries it in prose) is discarded rather than guessed at.
var visionRefPattern = regexp.MustCompile(`^([A-Za-z0-9._-]+)/([A-Za-z0-9._-]+)#(\d+)$`)

// parseVisionRefs reads the JSON array the agent was asked for. Only the
// last non-empty line is considered, and it has to be a plain array on its own:
// a model that thinks out loud before answering is still understood, but an
// array buried inside some other structure is not the answer that was
// requested. qualified selects which element shape is accepted, and the two are
// mutually exclusive: a bare-number answer to a board is an error, because the
// numbers it names cannot be resolved to repositories. Anything else is an
// error too, and the caller discards it rather than re-prompting.
func parseVisionRefs(output string, qualified bool) ([]VisionRef, error) {
	answer := lastNonEmptyLine(output)
	answer = strings.TrimSpace(strings.Trim(answer, "`"))
	if !strings.HasPrefix(answer, "[") || !strings.HasSuffix(answer, "]") {
		return nil, fmt.Errorf("the agent answered with something other than a JSON array")
	}
	if !qualified {
		var numbers []int
		if err := json.Unmarshal([]byte(answer), &numbers); err != nil {
			return nil, fmt.Errorf("the agent's answer is not a JSON array of issue numbers: %w", err)
		}
		refs := make([]VisionRef, 0, len(numbers))
		for _, number := range numbers {
			if number <= 0 {
				return nil, fmt.Errorf("the agent reported %d, which is not an issue number", number)
			}
			refs = append(refs, VisionRef{Number: number})
		}
		return refs, nil
	}
	var items []visionAnswer
	decoder := json.NewDecoder(strings.NewReader(answer))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&items); err != nil {
		return nil, fmt.Errorf(`the agent's answer is not a JSON array of {"ref","status"} objects: %w`, err)
	}
	refs := make([]VisionRef, 0, len(items))
	for _, item := range items {
		match := visionRefPattern.FindStringSubmatch(strings.TrimSpace(item.Ref))
		if match == nil {
			return nil, fmt.Errorf("the agent reported %q, which is not an OWNER/REPO#NUMBER issue reference", item.Ref)
		}
		number, err := strconv.Atoi(match[3])
		if err != nil || number <= 0 {
			return nil, fmt.Errorf("the agent reported %q, which is not an issue number", item.Ref)
		}
		status, err := visionStatus(item.Ref, item.Status)
		if err != nil {
			return nil, err
		}
		refs = append(refs, VisionRef{Repository: match[1] + "/" + match[2], Number: number, Status: status})
	}
	return refs, nil
}

// visionAnswer is one element of the qualified answer shape. Decoding
// with DisallowUnknownFields keeps the object as strict as the reference
// pattern is: an answer carrying extra fields is a different shape than the one
// that was asked for, and a different shape is discarded rather than
// interpreted.
type visionAnswer struct {
	Ref    string `json:"ref"`
	Status string `json:"status"`
}

// visionStatusLimit is the longest status a board column can plausibly
// be. A status is a short label a person reads at a glance ("Todo", "In
// Progress", "Ready for review"), so anything longer is prose that arrived in
// the status field rather than a column value.
const visionStatusLimit = 40

// visionStatus validates the Status column read off the board. An empty
// status is accepted as-is: boards really do leave items without one, and
// inventing a status would be exactly the guess this fallback must not make.
// Callers are expected to say loudly that such an item cannot dispatch.
func visionStatus(ref, status string) (string, error) {
	status = strings.TrimSpace(status)
	if len(status) > visionStatusLimit {
		return "", fmt.Errorf("the agent reported a %d-character status for %q, which is not a board column", len(status), ref)
	}
	if strings.ContainsFunc(status, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		return "", fmt.Errorf("the agent reported %q with a status that is not a single line", ref)
	}
	return status, nil
}

// lastNonEmptyLine returns the final line of text an agent printed, which is
// where a one-line answer ends up regardless of what preceded it.
func lastNonEmptyLine(output string) string {
	lines := strings.Split(output, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if line := strings.TrimSpace(lines[i]); line != "" {
			return line
		}
	}
	return ""
}
