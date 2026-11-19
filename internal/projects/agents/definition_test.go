package agents

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

// TestBuiltinDefinitionsLoad checks the embedded documents parse and validate.
// They are the only definitions a default run has, so a broken one is a glorp
// that cannot dispatch anything.
func TestBuiltinDefinitionsLoad(t *testing.T) {
	registry, err := Builtin()
	if err != nil {
		t.Fatalf("Builtin() error = %v", err)
	}
	// Asserted as a superset rather than an exact list: every agent glorp
	// adds a definition for lands here, and a test that has to be edited to
	// add one only ever reports that an agent was added.
	requireRegistered(t, registry, "claude", "codex")
	for _, name := range registry.Names() {
		definition, _ := registry.Lookup(name)
		for _, mode := range []Mode{ModeRun, ModeResume, ModeVision} {
			if !definition.Supports(mode) {
				t.Errorf("%s declares no %q template", name, mode)
			}
		}
	}
}

// TestBuiltinArgvIsUnchanged pins the argv every mode renders for the agents
// glorp already shipped. This is the parity claim the refactor rests on: the
// definitions replace hand-written branches, so the arguments they produce
// have to be exactly what those branches produced.
func TestBuiltinArgvIsUnchanged(t *testing.T) {
	registry := MustBuiltin()
	prompt := "/gh-fix o/r#7"
	for _, test := range []struct {
		name   string
		agent  string
		mode   Mode
		values Values
		want   []string
	}{
		{
			name: "codex fresh", agent: "codex", mode: ModeRun,
			values: Values{Prompt: prompt},
			want:   []string{"exec", prompt},
		},
		{
			name: "codex fresh with model and level", agent: "codex", mode: ModeRun,
			values: Values{Prompt: prompt, Model: "gpt-5.6-luna", Level: "high"},
			want:   []string{"exec", "--model", "gpt-5.6-luna", "-c", "model_reasoning_effort=high", prompt},
		},
		{
			name: "codex fresh yolo", agent: "codex", mode: ModeRun,
			values: Values{Prompt: prompt, Yolo: true},
			want:   []string{"exec", "--dangerously-bypass-approvals-and-sandbox", prompt},
		},
		{
			// A resume takes neither the model nor the level: the session it
			// continues already has them.
			name: "codex resume", agent: "codex", mode: ModeResume,
			values: Values{Prompt: prompt, Session: "sess-1", Model: "gpt-5.6-luna", Level: "high"},
			want:   []string{"exec", "resume", "sess-1", prompt},
		},
		{
			name: "codex vision", agent: "codex", mode: ModeVision,
			values: Values{Prompt: prompt, Image: "/tmp/shot.png", Model: "gpt-5"},
			want:   []string{"exec", "--skip-git-repo-check", "--image", "/tmp/shot.png", "--model", "gpt-5", prompt},
		},
		{
			name: "claude fresh", agent: "claude", mode: ModeRun,
			values: Values{Prompt: prompt},
			want:   []string{"-p", "--permission-mode", "auto", "--output-format", "stream-json", "--verbose", prompt},
		},
		{
			name: "claude fresh with session, model, and level", agent: "claude", mode: ModeRun,
			values: Values{Prompt: prompt, Session: "session-12", Model: "opus", Level: "low"},
			want: []string{
				"-p", "--session-id", "session-12", "--permission-mode", "auto",
				"--model", "opus", "--effort", "low", "--output-format", "stream-json", "--verbose", prompt,
			},
		},
		{
			name: "claude fresh yolo", agent: "claude", mode: ModeRun,
			values: Values{Prompt: prompt, Yolo: true},
			want:   []string{"-p", "--dangerously-skip-permissions", "--output-format", "stream-json", "--verbose", prompt},
		},
		{
			name: "claude fresh with remote control", agent: "claude", mode: ModeRun,
			values: Values{Prompt: prompt, RemoteControl: true, Settings: `{"remoteControlAtStartup":true}`, SessionName: "glorp o/r#7"},
			want: []string{
				"-p", "--permission-mode", "auto", "--settings", `{"remoteControlAtStartup":true}`,
				"--rc", "glorp o/r#7", "--output-format", "stream-json", "--verbose", prompt,
			},
		},
		{
			// Codex declares no remote-control fragment, so the run's flag
			// reaches its argv not at all.
			name: "codex ignores remote control", agent: "codex", mode: ModeRun,
			values: Values{Prompt: prompt, RemoteControl: true, Settings: `{"remoteControlAtStartup":true}`, SessionName: "glorp o/r#7"},
			want:   []string{"exec", prompt},
		},
		{
			name: "claude resume", agent: "claude", mode: ModeResume,
			values: Values{Prompt: prompt, Session: "session-12", Model: "opus", Level: "low"},
			want: []string{
				"-p", "--resume", "session-12", "--permission-mode", "auto",
				"--output-format", "stream-json", "--verbose", prompt,
			},
		},
		{
			name: "claude vision", agent: "claude", mode: ModeVision,
			values: Values{Prompt: prompt, Image: "/tmp/shot.png"},
			want:   []string{"-p", "--permission-mode", "auto", prompt},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition, ok := registry.Lookup(test.agent)
			if !ok {
				t.Fatalf("no definition for %q", test.agent)
			}
			if got := definition.Render(test.mode, test.values); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("argv = %#v, want %#v", got, test.want)
			}
		})
	}
}

// TestRenderSkipsUnknownMode checks a mode a definition declares no template
// for renders nothing, so the caller reports it instead of invoking the agent
// with a bare prompt.
func TestRenderSkipsUnknownMode(t *testing.T) {
	definition := Definition{Args: Args{Run: []Fragment{{Args: []string{"go"}}}}}
	if got := definition.Render(ModeVision, Values{}); got != nil {
		t.Fatalf("vision argv = %#v, want nothing", got)
	}
	if definition.Supports(ModeVision) {
		t.Fatal("a definition with no vision template reported support for it")
	}
}

// TestRenderPromptStdinReplacesThePromptArgument checks an out-of-band call
// that writes its prompt to stdin renders the agent's stand-in for the prompt
// argument instead of the prompt (issue #815), and that every other call, and
// every agent that cannot read stdin, renders the prompt as before.
func TestRenderPromptStdinReplacesThePromptArgument(t *testing.T) {
	registry := MustBuiltin()
	for _, test := range []struct {
		agent string
		want  []string
	}{
		{"claude", []string{"-p", "--permission-mode", "auto", "--output-format", "stream-json", "--verbose"}},
		{"codex", []string{"exec", "-"}},
	} {
		definition, _ := registry.Lookup(test.agent)
		if !definition.ReadsPromptFromStdin() {
			t.Fatalf("%s does not read its prompt from stdin", test.agent)
		}
		if got := definition.Render(ModeRun, Values{Prompt: "a long prompt", PromptStdin: true}); !reflect.DeepEqual(got, test.want) {
			t.Errorf("%s stdin argv = %#v, want %#v", test.agent, got, test.want)
		}
		if got := definition.Render(ModeRun, Values{Prompt: "a long prompt"}); got[len(got)-1] != "a long prompt" {
			t.Errorf("%s argv without stdin = %#v, want the prompt last", test.agent, got)
		}
	}
	gemini, _ := registry.Lookup("gemini")
	if gemini.ReadsPromptFromStdin() {
		t.Fatal("gemini declares a stdin prompt it was never measured to read")
	}
	if got := gemini.Render(ModeRun, Values{Prompt: "a long prompt", PromptStdin: true}); got[len(got)-1] != "a long prompt" {
		t.Errorf("gemini argv = %#v, want the prompt kept in argv", got)
	}
}

// TestRenderCodexOneShotSkipsTheGitRepoCheck checks a Codex call outside any
// checkout, which runs in the temp directory, passes --skip-git-repo-check so
// Codex does not refuse to run outside a trusted directory (issues #817,
// #819), and that a dispatched run inside its clone renders without it.
func TestRenderCodexOneShotSkipsTheGitRepoCheck(t *testing.T) {
	codex, _ := MustBuiltin().Lookup("codex")
	values := Values{Prompt: "status?", Model: "gpt-5", PromptStdin: true, OneShot: true, NoCheckout: true}
	want := []string{"exec", "--skip-git-repo-check", "--model", "gpt-5", "-"}
	if got := codex.Render(ModeRun, values); !reflect.DeepEqual(got, want) {
		t.Errorf("one-shot argv = %#v, want %#v", got, want)
	}
	values.Yolo = true
	want = []string{"exec", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox", "--model", "gpt-5", "-"}
	if got := codex.Render(ModeRun, values); !reflect.DeepEqual(got, want) {
		t.Errorf("one-shot yolo argv = %#v, want %#v", got, want)
	}
	for _, mode := range []Mode{ModeRun, ModeResume} {
		for _, arg := range codex.Render(mode, Values{Prompt: "fix it", Session: "abc"}) {
			if arg == "--skip-git-repo-check" {
				t.Errorf("dispatched %s argv carries --skip-git-repo-check", mode)
			}
		}
	}
	want = []string{"exec", "resume", "--skip-git-repo-check", "abc", "fix it"}
	if got := codex.Render(ModeResume, Values{Prompt: "fix it", Session: "abc", NoCheckout: true}); !reflect.DeepEqual(got, want) {
		t.Errorf("resume argv outside a checkout = %#v, want %#v", got, want)
	}
}

// TestInvalidDefinitionsNameTheirField checks every rejection says which field
// was wrong. A definition dropped without one is indistinguishable, at the
// --agent prompt, from a typo in the agent's name.
func TestInvalidDefinitionsNameTheirField(t *testing.T) {
	valid := func() Definition {
		return Definition{
			Name: "acme", Binary: "acme",
			Args:    Args{Run: []Fragment{{Args: []string{"{prompt}"}}}, Resume: []Fragment{{Args: []string{"{session}"}}}},
			Session: Session{Assign: AssignGlorp},
			Output:  Output{Format: FormatText},
		}
	}
	for _, test := range []struct {
		name    string
		mutate  func(*Definition)
		wantSub string
	}{
		{"no name", func(d *Definition) { d.Name = "" }, `"name"`},
		{"unusable name", func(d *Definition) { d.Name = "my agent" }, `"name"`},
		{"no binary", func(d *Definition) { d.Binary = "" }, `"binary"`},
		{"no run template", func(d *Definition) { d.Args.Run = nil }, `"args.run"`},
		{"no resume template", func(d *Definition) { d.Args.Resume = nil }, `"args.resume"`},
		{"no resume template with a captured session", func(d *Definition) {
			d.Args.Resume = nil
			d.Session = Session{Assign: AssignCapture, Capture: `session (\S+)`}
		}, `"args.resume"`},
		{"empty fragment", func(d *Definition) { d.Args.Run = []Fragment{{}} }, `"args.run"[0].args`},
		{"unknown condition", func(d *Definition) {
			d.Args.Run = []Fragment{{When: "sandbox", Args: []string{"x"}}}
		}, `unknown condition "sandbox"`},
		{"unknown placeholder", func(d *Definition) {
			d.Args.Run = []Fragment{{Args: []string{"--say={greeting}"}}}
		}, `unknown placeholder {greeting}`},
		{"unknown assignment", func(d *Definition) { d.Session.Assign = "somehow" }, `"session.assign"`},
		{"capture without a pattern", func(d *Definition) { d.Session = Session{Assign: AssignCapture} }, `"session.capture"`},
		{"capture without a group", func(d *Definition) {
			d.Session = Session{Assign: AssignCapture, Capture: "session id"}
		}, "capture group"},
		{"capture that does not compile", func(d *Definition) {
			d.Session = Session{Assign: AssignCapture, Capture: "session id: ("}
		}, `"session.capture"`},
		{"capture where none is read", func(d *Definition) { d.Session.Capture = "x" }, `"session.capture"`},
		{"unknown output format", func(d *Definition) { d.Output.Format = "yaml" }, `"output.format"`},
		{"empty level", func(d *Definition) { d.Levels = NewAllowList("high", " ") }, `"levels"`},
		{"empty model", func(d *Definition) { d.Models = NewAllowList("") }, `"models"`},
		{"unusable env name", func(d *Definition) { d.Env = map[string]string{"A=B": "c"} }, `"env"`},
		{"stdin prompt with a placeholder", func(d *Definition) { d.PromptStdin = &[]string{"{model}"} }, `"promptStdin"`},
		{"stdin prompt without a prompt argument", func(d *Definition) {
			d.PromptStdin = &[]string{}
			d.Args.Run = []Fragment{{Args: []string{"--say={prompt}"}}}
		}, `"promptStdin"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			definition := valid()
			test.mutate(&definition)
			err := definition.Validate()
			if err == nil {
				t.Fatal("the definition was accepted")
			}
			if !strings.Contains(err.Error(), test.wantSub) {
				t.Fatalf("error = %v, want it to name %s", err, test.wantSub)
			}
		})
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("a valid definition was rejected: %v", err)
	}
}

// TestAllowListsAdmitAnythingWhenEmpty checks an allow-list left out of a
// definition constrains nothing, so a definition need not enumerate every
// model its agent has.
func TestAllowListsAdmitAnythingWhenEmpty(t *testing.T) {
	open := Definition{}
	if !open.AcceptsLevel("whatever") || !open.AcceptsModel("whatever") {
		t.Fatal("an empty allow-list rejected a value")
	}
	limited := Definition{Levels: NewAllowList("low"), Models: NewAllowList("opus")}
	if limited.AcceptsLevel("high") || limited.AcceptsModel("sonnet") {
		t.Fatal("an allow-list admitted a value it does not list")
	}
	if !limited.AcceptsLevel("low") || !limited.AcceptsModel("opus") {
		t.Fatal("an allow-list rejected a value it lists")
	}
}

func TestJoinOrReadsAsASentence(t *testing.T) {
	for _, test := range []struct {
		values []string
		want   string
	}{
		{nil, ""},
		{[]string{"low"}, "low"},
		{[]string{"low", "high"}, "low or high"},
		{[]string{"low", "medium", "high"}, "low, medium, or high"},
	} {
		if got := JoinOr(test.values); got != test.want {
			t.Fatalf("JoinOr(%v) = %q, want %q", test.values, got, test.want)
		}
	}
}

// TestSessionAccessors checks the two session shapes a definition can declare
// resolve to the behaviour the runner keys off.
func TestSessionAccessors(t *testing.T) {
	registry := MustBuiltin()
	claude, _ := registry.Lookup("claude")
	if !claude.AssignsSessionID() || claude.CapturesSessionID() || claude.SessionPattern() != nil {
		t.Fatal("claude should take the session ID glorp assigns it")
	}
	codex, _ := registry.Lookup("codex")
	if codex.AssignsSessionID() || !codex.CapturesSessionID() {
		t.Fatal("codex should announce its own session ID")
	}
	pattern := codex.SessionPattern()
	if pattern == nil {
		t.Fatal("codex declares no session pattern")
	}
	match := pattern.FindStringSubmatch("session id: 3f2504e0-4f89-11d3-9a0c-0305e82c3301")
	if len(match) != 2 || match[1] != "3f2504e0-4f89-11d3-9a0c-0305e82c3301" {
		t.Fatalf("session pattern read %#v, want the announced ID", match)
	}
	if !codex.Session.ClearOnResumeFailure {
		t.Fatal("codex should drop a session ID it can no longer resume")
	}
}

// requireRegistered checks a registry holds at least the named agents. The
// built-in set grows an agent at a time, so the tests that care which agents
// exist state the ones they are about rather than the whole list, which would
// otherwise have to be edited by every agent added after them.
func requireRegistered(t *testing.T, registry *Registry, names ...string) {
	t.Helper()
	for _, name := range names {
		if _, ok := registry.Lookup(name); !ok {
			t.Fatalf("agents = %v, want them to include %q", registry.Names(), name)
		}
	}
}

// TestSkillsTargetShapeIsValidated checks a malformed skills.sh target id is
// rejected while an id glorp has never heard of is accepted: the set of ids
// skills.sh knows grows without glorp, so only the shape is glorp's business.
func TestSkillsTargetShapeIsValidated(t *testing.T) {
	definition := Definition{
		Name: "acme", Binary: "acme",
		Session: Session{Assign: AssignNone}, Output: Output{Format: FormatText},
		Args: Args{Run: []Fragment{{Args: []string{"{prompt}"}}}, Resume: []Fragment{{Args: []string{"{prompt}"}}}},
	}
	definition.Skills = Skills{Target: "some-new-cli"}
	if err := definition.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want an unknown-but-well-formed id accepted", err)
	}
	if got := definition.SkillsTarget(); got != "some-new-cli" {
		t.Fatalf("SkillsTarget() = %q, want the declared id", got)
	}
	definition.Skills = Skills{Target: "Claude Code"}
	err := definition.Validate()
	if err == nil || !strings.Contains(err.Error(), `"skills.target"`) {
		t.Fatalf("Validate() error = %v, want it to name skills.target", err)
	}
	definition.Skills = Skills{}
	if err := definition.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want an absent target accepted", err)
	}
}

// TestQuotaValidation covers the quota block the registry-driven quota reader
// selects on (issue #489). A block that names a reader glorp does not have, or
// that carries generic-reader fields the chosen reader would ignore, is
// rejected rather than silently doing nothing: a quota that never appears
// looks exactly like an agent that has none.
func TestQuotaValidation(t *testing.T) {
	base := Definition{
		Name: "muse", Binary: "muse",
		Args:    Args{Run: []Fragment{{Args: []string{"{prompt}"}}}, Resume: []Fragment{{Args: []string{"{prompt}"}}}},
		Session: Session{Assign: AssignNone},
		Output:  Output{Format: FormatText},
	}
	valid := []struct {
		name  string
		quota Quota
	}{
		{name: "none by default", quota: Quota{}},
		{name: "explicit none", quota: Quota{Reader: QuotaNone}},
		{name: "built-in codex", quota: Quota{Reader: QuotaCodex}},
		{name: "built-in claude", quota: Quota{Reader: QuotaClaude}},
		{name: "command", quota: Quota{Reader: QuotaCommand, Command: []string{"{binary}", "usage"}, PercentUsed: "used"}},
		{name: "command with reset", quota: Quota{
			Reader: QuotaCommand, Command: []string{"muse", "usage"},
			PercentUsed: "a.b", ResetAt: "a.c", Format: "{percentLeft}% until {resetAt}", Timeout: "5s",
		}},
	}
	for _, test := range valid {
		t.Run(test.name, func(t *testing.T) {
			definition := base
			definition.Quota = test.quota
			if err := definition.Validate(); err != nil {
				t.Fatalf("Validate() = %v, want nil", err)
			}
		})
	}
	invalid := []struct {
		name  string
		quota Quota
		want  string
	}{
		{name: "unknown reader", quota: Quota{Reader: "gemini"}, want: `"quota.reader"`},
		{name: "command without argv", quota: Quota{Reader: QuotaCommand}, want: `"quota.command" is required`},
		{name: "empty argument", quota: Quota{Reader: QuotaCommand, Command: []string{"muse", " "}, PercentUsed: "used"}, want: "empty argument"},
		{name: "unknown placeholder", quota: Quota{Reader: QuotaCommand, Command: []string{"muse"}, PercentUsed: "used", Format: "{tokens} left"}, want: "unknown placeholder"},
		{name: "percent without field", quota: Quota{Reader: QuotaCommand, Command: []string{"muse"}}, want: `"quota.percentUsed" is required`},
		{name: "reset without field", quota: Quota{Reader: QuotaCommand, Command: []string{"muse"}, PercentUsed: "used", Format: "{resetAt}"}, want: `"quota.resetAt" is required`},
		{name: "bad timeout", quota: Quota{Reader: QuotaCommand, Command: []string{"muse"}, PercentUsed: "used", Timeout: "soon"}, want: `"quota.timeout"`},
		{name: "zero timeout", quota: Quota{Reader: QuotaCommand, Command: []string{"muse"}, PercentUsed: "used", Timeout: "0s"}, want: "must be positive"},
		{name: "command field on codex", quota: Quota{Reader: QuotaCodex, Command: []string{"codex"}}, want: `only meaningful when "quota.reader" is "command"`},
		{name: "format on none", quota: Quota{Format: "{percentLeft}%"}, want: `only meaningful when "quota.reader" is "command"`},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			definition := base
			definition.Quota = test.quota
			err := definition.Validate()
			if err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
			if !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() = %v, want it to mention %q", err, test.want)
			}
		})
	}
}

// TestQuotaDefaults pins the values the generic reader falls back to when a
// definition names only the field it reads.
func TestQuotaDefaults(t *testing.T) {
	quota := Quota{Reader: QuotaCommand, Command: []string{"{binary}", "usage", "--for={binary}"}, PercentUsed: "used"}
	if got := quota.ReaderName(); got != QuotaCommand {
		t.Fatalf("ReaderName() = %q", got)
	}
	if got := (Quota{}).ReaderName(); got != QuotaNone {
		t.Fatalf("empty ReaderName() = %q, want %q", got, QuotaNone)
	}
	if got := quota.FormatTemplate(); got != "{percentLeft}% left" {
		t.Fatalf("FormatTemplate() = %q", got)
	}
	if got := quota.TimeoutDuration(); got != DefaultQuotaTimeout {
		t.Fatalf("TimeoutDuration() = %s, want %s", got, DefaultQuotaTimeout)
	}
	quota.Timeout = "250ms"
	if got := quota.TimeoutDuration(); got != 250*time.Millisecond {
		t.Fatalf("TimeoutDuration() = %s", got)
	}
	if got := strings.Join(quota.Argv("/opt/muse"), " "); got != "/opt/muse usage --for=/opt/muse" {
		t.Fatalf("Argv() = %q, want {binary} substituted", got)
	}
}

// TestBuiltinAgentsKeepTheirQuotaReaders checks the definitions glorp ships
// still select the readers whose status-bar strings the UI tests pin.
func TestBuiltinAgentsKeepTheirQuotaReaders(t *testing.T) {
	registry := MustBuiltin()
	for name, want := range map[string]string{"codex": QuotaCodex, "claude": QuotaClaude} {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("built-in agent %q is missing", name)
		}
		if got := definition.Quota.ReaderName(); got != want {
			t.Fatalf("%s quota reader = %q, want %q", name, got, want)
		}
	}
}

// TestADeclaredEmptyAllowListAdmitsNothing checks the third state the schema
// spells: an agent whose CLI has no reasoning-effort flag declares the empty
// list and rejects a level instead of accepting one it cannot render (issue
// #532).
func TestADeclaredEmptyAllowListAdmitsNothing(t *testing.T) {
	none := Definition{Name: "acme", Levels: NewAllowList()}
	if !none.Levels.Declared() || !none.Levels.AcceptsNothing() {
		t.Fatal("an empty list did not read as declared-and-empty")
	}
	for _, level := range []string{"high", "none", ""} {
		if none.AcceptsLevel(level) {
			t.Fatalf("level %q was admitted by a list that admits nothing", level)
		}
	}
	// The message names the agent rather than telling the caller to pick from
	// an empty set, which is the whole reason the state exists.
	if err := none.LevelError(); err == nil || err.Error() != "agent acme takes no reasoning level" {
		t.Fatalf("LevelError() = %v, want it to name the agent", err)
	}
	listed := Definition{Name: "acme", Levels: NewAllowList("low", "high")}
	if err := listed.LevelError(); err == nil || err.Error() != "agent level must be low or high" {
		t.Fatalf("LevelError() = %v, want the allow-list", err)
	}
	models := Definition{Name: "acme", Models: NewAllowList()}
	if models.AcceptsModel("acme-1") {
		t.Fatal("a model was admitted by a list that admits nothing")
	}
	if err := models.ModelError(); err == nil || err.Error() != "agent acme takes no model" {
		t.Fatalf("ModelError() = %v, want it to name the agent", err)
	}
}

// TestAllowListRoundTripsItsThreeStates checks absent, empty, and populated
// survive the marshal-and-merge trip the agent config file puts a definition
// through, since an empty list flattened back to an absent one would silently
// restore the accept-anything behaviour it exists to replace.
func TestAllowListRoundTripsItsThreeStates(t *testing.T) {
	for _, test := range []struct {
		name    string
		encoded string
		want    string
	}{
		{"absent", `{"binary":"acme"}`, "null"},
		{"null", `{"binary":"acme","levels":null}`, "null"},
		{"empty", `{"binary":"acme","levels":[]}`, "[]"},
		{"listed", `{"binary":"acme","levels":["low","high"]}`, `["low","high"]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			var definition Definition
			if err := json.Unmarshal([]byte(test.encoded), &definition); err != nil {
				t.Fatal(err)
			}
			encoded, err := json.Marshal(definition.Levels)
			if err != nil {
				t.Fatal(err)
			}
			if string(encoded) != test.want {
				t.Fatalf("levels = %s, want %s", encoded, test.want)
			}
			var again Definition
			if err := json.Unmarshal([]byte(`{"levels":`+string(encoded)+`}`), &again); err != nil {
				t.Fatal(err)
			}
			if again.Levels.Declared() != definition.Levels.Declared() ||
				again.Levels.AcceptsNothing() != definition.Levels.AcceptsNothing() ||
				!reflect.DeepEqual(again.Levels.Values(), definition.Levels.Values()) {
				t.Fatalf("round trip lost the state: %#v", again.Levels)
			}
		})
	}
}

// TestSessionlessAgentMayOmitResume pins the answer issue #541 chose: an agent
// that declares no resumable session may leave args.resume out, and a resume
// renders its run template rather than nothing. Rendering nothing would fail
// the job it was supposed to recover, so the fallback is the whole point.
func TestSessionlessAgentMayOmitResume(t *testing.T) {
	definition := Definition{
		Name: "acme", Binary: "acme",
		Args:    Args{Run: []Fragment{{Args: []string{"--go"}}, {When: "model", Args: []string{"--model", "{model}"}}, {Args: []string{"{prompt}"}}}},
		Session: Session{Assign: AssignNone},
		Output:  Output{Format: FormatText},
	}
	if err := definition.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want nil", err)
	}
	if !definition.Supports(ModeResume) {
		t.Error("Supports(ModeResume) = false, want true through the run fallback")
	}
	values := Values{Prompt: "continue", Model: "acme-1", Session: "sess-1"}
	want := []string{"--go", "--model", "acme-1", "continue"}
	if got := definition.Render(ModeResume, values); !reflect.DeepEqual(got, want) {
		t.Errorf("Render(ModeResume) = %q, want %q", got, want)
	}
	// A declared resume still wins over the fallback.
	definition.Args.Resume = []Fragment{{Args: []string{"--continue", "{prompt}"}}}
	if got, want := definition.Render(ModeResume, values), []string{"--continue", "continue"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Render(ModeResume) with a declared resume = %q, want %q", got, want)
	}
}

// TestSessionlessBuiltinsResumeAsTheyRan checks cline and opencode recover
// exactly as they did when each duplicated its run template under "resume":
// the argv a resume renders is still the argv a fresh run renders.
func TestSessionlessBuiltinsResumeAsTheyRan(t *testing.T) {
	registry := MustBuiltin()
	for _, name := range []string{"cline", "opencode"} {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) missing", name)
		}
		if definition.Session.Assign != AssignNone {
			t.Fatalf("%s session.assign = %q, want %q", name, definition.Session.Assign, AssignNone)
		}
		if len(definition.Args.Resume) != 0 {
			t.Errorf("%s declares args.resume, which duplicates its run template", name)
		}
		values := Values{Prompt: "continue", Model: "m", Level: "high"}
		run, resume := definition.Render(ModeRun, values), definition.Render(ModeResume, values)
		if len(resume) == 0 || !reflect.DeepEqual(run, resume) {
			t.Errorf("%s resume argv = %q, want the run argv %q", name, resume, run)
		}
	}
}

// TestResumingAgentsKeepTheirOwnTemplate checks the fallback is confined to
// sessionless agents: an agent glorp holds a session ID for still resumes with
// the arguments its definition declares.
func TestResumingAgentsKeepTheirOwnTemplate(t *testing.T) {
	registry := MustBuiltin()
	for _, name := range []string{"codex", "claude", "gemini", "muse"} {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("Lookup(%q) missing", name)
		}
		if len(definition.Args.Resume) == 0 {
			t.Errorf("%s declares no args.resume", name)
		}
		// The fallback must not reach these: a resume renders the resume
		// fragments the definition declares, whatever the run declares.
		values := Values{Prompt: "continue", Session: "sess-1"}
		declared := Definition{Args: Args{Run: definition.Args.Resume}}
		if got, want := definition.Render(ModeResume, values), declared.Render(ModeRun, values); !reflect.DeepEqual(got, want) {
			t.Errorf("%s resume argv = %q, want its declared template %q", name, got, want)
		}
	}
}

// TestDoctorAcceptsAModelsNoteOnItsOwn checks a definition may say why it
// cannot list models without carrying a list to caption. A CLI with no listing
// command is exactly the case issue #566 left with nothing to report, and the
// note is what it reports instead.
func TestDoctorAcceptsAModelsNoteOnItsOwn(t *testing.T) {
	definition := doctorFixture(Doctor{ModelsNote: "not listed by this CLI"})
	if err := definition.Validate(); err != nil {
		t.Errorf("Validate() error = %v, want a standalone note accepted", err)
	}
}

// TestDoctorRejectsModelReadersWithNoProbe checks the fields that read a model
// probe's output are held to the same rule as every other dependent field: one
// that silently does nothing looks exactly like a working one.
func TestDoctorRejectsModelReadersWithNoProbe(t *testing.T) {
	for field, doctor := range map[string]Doctor{
		"doctor.modelsJSON":  {ModelsJSON: "models[].id"},
		"doctor.modelsStdin": {ModelsStdin: []string{"{}"}},
	} {
		err := doctorFixture(doctor).Validate()
		if err == nil || !strings.Contains(err.Error(), field) {
			t.Errorf("Validate() error = %v, want it to reject %s with no doctor.models", err, field)
		}
	}
}

// TestDoctorRejectsAnUnreadableModelsPath checks a misspelled path is reported
// rather than left to extract nothing: a path that matches nothing and a path
// that is malformed produce the same empty list, and only one of them is the
// author's mistake.
func TestDoctorRejectsAnUnreadableModelsPath(t *testing.T) {
	for _, path := range []string{"models[.id", "models[bad].id", "models..id"} {
		doctor := Doctor{Models: []string{"{binary}", "models"}, ModelsJSON: path}
		err := doctorFixture(doctor).Validate()
		if err == nil || !strings.Contains(err.Error(), "doctor.modelsJSON") {
			t.Errorf("Validate() error = %v for path %q, want it rejected", err, path)
		}
	}
}

// TestDoctorRejectsAnEmptyStdinLine checks a blank line in the handshake is
// rejected: a protocol that reads it as an empty frame answers nothing, and
// the probe would look like a CLI that has no models.
func TestDoctorRejectsAnEmptyStdinLine(t *testing.T) {
	doctor := Doctor{Models: []string{"{binary}", "serve"}, ModelsStdin: []string{"{}", "  "}, ModelsJSON: "result.models[].id"}
	err := doctorFixture(doctor).Validate()
	if err == nil || !strings.Contains(err.Error(), "doctor.modelsStdin") {
		t.Errorf("Validate() error = %v, want the empty line rejected", err)
	}
}

// doctorFixture is a valid definition carrying the doctor block under test.
func doctorFixture(doctor Doctor) Definition {
	return Definition{
		Name: "probe", Binary: "probe",
		Session: Session{Assign: AssignNone},
		Output:  Output{Format: FormatText},
		Args:    Args{Run: []Fragment{{Args: []string{"{prompt}"}}}},
		Doctor:  doctor,
	}
}

// TestDefaultModelFillsInAnUnspecifiedModel checks the managed default an
// --agent spec with no model of its own falls back to (issue #612), and that a
// definition naming none still leaves the choice to the agent's own CLI.
func TestDefaultModelFillsInAnUnspecifiedModel(t *testing.T) {
	withDefault := Definition{DefaultModel: "gpt-5.6-terra"}
	if got := withDefault.ModelOrDefault(""); got != "gpt-5.6-terra" {
		t.Fatalf("ModelOrDefault(\"\") = %q, want the definition's default", got)
	}
	if got := withDefault.ModelOrDefault("gpt-5.6-sol"); got != "gpt-5.6-sol" {
		t.Fatalf("ModelOrDefault(explicit) = %q, want the model the spec named", got)
	}
	if got := (Definition{}).ModelOrDefault(""); got != "" {
		t.Fatalf("ModelOrDefault(\"\") = %q, want none for a definition declaring no default", got)
	}
}

// TestDefaultModelHasToBeOneTheAgentAccepts checks the default is held to the
// same allow-list --agent is, so a definition cannot promise a model it says in
// the next field it would reject.
func TestDefaultModelHasToBeOneTheAgentAccepts(t *testing.T) {
	base := func() Definition {
		return Definition{
			Name:    "acme",
			Binary:  "acme",
			Session: Session{Assign: AssignNone},
			Output:  Output{Format: FormatText},
			Args:    Args{Run: []Fragment{{Args: []string{"{prompt}"}}}},
		}
	}
	accepted := base()
	accepted.Models = AllowList{}
	accepted.Models.UnmarshalJSON([]byte(`["fast","slow"]`))
	accepted.DefaultModel = "fast"
	if err := accepted.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want a default the allow-list admits to pass", err)
	}
	rejected := accepted
	rejected.DefaultModel = "enormous"
	err := rejected.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want a default outside the allow-list rejected")
	}
	if !strings.Contains(err.Error(), "defaultModel") {
		t.Fatalf("Validate() error = %v, want it to name the field", err)
	}
	unconstrained := base()
	unconstrained.DefaultModel = "anything"
	if err := unconstrained.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want any default on a definition with no models allow-list", err)
	}
}

// TestBuiltinDefaultModelsAreMidTier checks the shipped defaults are the ones
// glorp means to spend a queue of issues on rather than each CLI's own largest
// model (issue #612), and that the agents whose catalogs are per-account name
// none rather than a model id glorp cannot promise every account has.
//
// The four ids here were each read out of the CLI's own catalog rather than
// guessed (issue #622). gemini's list is built inside the CLI rather than
// fetched for the account, so a flash id can be promised the way codex's and
// claude's can; agy's is fetched, but every id it offers except the Claude and
// GPT-OSS ones ends in its own reasoning level. muse, cline, and opencode name
// none deliberately: their catalogs are whatever the signed-in account can
// reach, so any id glorp wrote down here would fail the dispatch outright on
// an account that does not have it -- strictly worse than letting the CLI
// choose. See TestBuiltinDefaultModelsCarryNoReasoningLevel for the other half
// of the agy decision.
func TestBuiltinDefaultModelsAreMidTier(t *testing.T) {
	registry := MustBuiltin()
	for name, want := range map[string]string{
		"codex":    "gpt-5.6-terra",
		"claude":   "sonnet",
		"gemini":   "gemini-3.5-flash",
		"agy":      "claude-sonnet-4-6",
		"muse":     "",
		"cline":    "",
		"opencode": "",
	} {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("built-in agent %q is missing", name)
		}
		if got := definition.ModelOrDefault(""); got != want {
			t.Errorf("%s default model = %q, want %q", name, got, want)
		}
	}
}

// TestBuiltinDefaultModelsCarryNoReasoningLevel guards the other half of the
// agy decision in issue #622: agy renders {level} into its own --effort
// fragment, and most of the ids its catalog offers spell the level into the id
// itself (gemini-3.8-flash-medium, gemini-3.1-pro-high). A default carrying an
// embedded level would have glorp dispatch --model gemini-3.8-flash-medium
// --effort high and let the CLI pick which of the two contradictory levels
// wins, so the default has to be an id with no level in it. The same holds for
// any built-in that declares levels at all.
func TestBuiltinDefaultModelsCarryNoReasoningLevel(t *testing.T) {
	registry := MustBuiltin()
	for _, name := range registry.Names() {
		definition, ok := registry.Lookup(name)
		if !ok {
			t.Fatalf("built-in agent %q is missing", name)
		}
		model := definition.ModelOrDefault("")
		if model == "" {
			continue
		}
		for _, level := range definition.Levels.Values() {
			if level == "none" {
				continue
			}
			if strings.HasSuffix(model, "-"+level) {
				t.Errorf("%s default model %q ends in its own reasoning level %q; --agent %s:LEVEL would render both", name, model, level, name)
			}
		}
	}
}
