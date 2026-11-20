// Package codingskills installs and tracks the skills Aviary ships for coding
// CLIs (gh-fix, gh-discuss and gh-create). They install into each CLI through
// skills.sh, so they keep working without Aviary.
package codingskills

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects/agents"
	"github.com/lsegal/aviary/internal/update"
)

// Repo is the GitHub repository the skills install from.
const Repo = "lsegal/aviary"

// installTimeout bounds one `npx skills add` run, so a hung network clone
// cannot hold an install open forever.
const installTimeout = 5 * time.Minute

// CLI is a coding CLI the skills install into. Target is its skills.sh agent
// id, and Binary the executable whose presence on PATH means it is set up.
type CLI struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Target string `json:"target"`
	Binary string `json:"binary"`
}

// cliNames are display names for agent definitions whose name does not read
// well capitalized.
var cliNames = map[string]string{
	"agy":      "Antigravity",
	"opencode": "OpenCode",
}

// CLIsFromRegistry lists the coding CLIs whose agent definition names a
// skills.sh target, in the registry's sorted order.
func CLIsFromRegistry(registry *agents.Registry) []CLI {
	var clis []CLI
	for _, name := range registry.Names() {
		definition, _ := registry.Lookup(name)
		target := definition.SkillsTarget()
		if target == "" {
			continue
		}
		display := cliNames[name]
		if display == "" {
			display = strings.ToUpper(name[:1]) + name[1:]
		}
		binary := definition.Binary
		if binary == "" {
			binary = name
		}
		clis = append(clis, CLI{ID: name, Name: display, Target: target, Binary: binary})
	}
	return clis
}

// builtinCLIs lists the coding CLIs of the built-in agent definitions.
func builtinCLIs() []CLI {
	registry, err := agents.Builtin()
	if err != nil {
		slog.Warn("coding skills: could not load the built-in agent definitions", "err", err)
		return nil
	}
	return CLIsFromRegistry(registry)
}

// skillInfo is the short, user-facing summary of each skill.
var skillInfo = map[string]struct {
	Summary       string
	RunByProjects bool
}{
	"gh-fix": {
		Summary:       "Fixes an issue end to end: draft PR, implement, test, changelog, CI repair, merge. Runs for issues on watched repos and boards.",
		RunByProjects: true,
	},
	"gh-discuss": {
		Summary:       "Reads the repo and posts one reply to a new Discussion thread, or stays silent. Read-only. Runs for watched Discussions boards.",
		RunByProjects: true,
	},
	"gh-create": {
		Summary: "Files a well-formed issue from a short request. Projects never runs it, but the issues it files are what Projects picks up.",
	},
}

// Env is the part of the host the manager reads. Zero fields fall back to
// the real host.
type Env struct {
	Home     string
	Getenv   func(string) string
	LookPath func(string) (string, error)
}

func (e Env) home() string {
	if e.Home != "" {
		return e.Home
	}
	home, _ := os.UserHomeDir()
	return home
}

func (e Env) getenv(key string) string {
	if e.Getenv != nil {
		return e.Getenv(key)
	}
	return os.Getenv(key)
}

func (e Env) detected(binary string) bool {
	lookPath := e.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	_, err := lookPath(binary)
	return err == nil
}

// canonicalDir is where `npx skills add --global` keeps the copy every
// CLI's skills directory links to.
func (e Env) canonicalDir() string {
	return filepath.Join(e.home(), ".agents", "skills")
}

// SkillsDir is the global skills directory skills.sh installs target into.
// Targets that read .agents/skills share the canonical directory; the others
// get a link to it in their own home.
func (e Env) SkillsDir(target string) string {
	home := e.home()
	configHome := e.getenv("XDG_CONFIG_HOME")
	if configHome == "" {
		configHome = filepath.Join(home, ".config")
	}
	switch target {
	case "claude-code":
		if dir := strings.TrimSpace(e.getenv("CLAUDE_CONFIG_DIR")); dir != "" {
			return filepath.Join(dir, "skills")
		}
		return filepath.Join(home, ".claude", "skills")
	case "gemini-cli":
		return filepath.Join(home, ".gemini", "skills")
	case "antigravity-cli":
		return filepath.Join(home, ".gemini", "antigravity-cli", "skills")
	case "opencode":
		return filepath.Join(configHome, "opencode", "skills")
	default:
		return e.canonicalDir()
	}
}

// SkillsDirs lists every global directory target may read skills from:
// SkillsDir first, then the CLI's own directories that a copy installed by
// hand or by an older skills.sh may sit in.
func (e Env) SkillsDirs(target string) []string {
	dirs := []string{e.SkillsDir(target)}
	home := e.home()
	switch target {
	case "codex":
		dirs = append(dirs, filepath.Join(home, ".codex", "skills"))
	case "cline":
		dirs = append(dirs, filepath.Join(home, ".cline", "skills"))
	case "opencode":
		dirs = append(dirs, filepath.Join(filepath.Dir(dirs[0]), "skill"))
	}
	return dirs
}

// lockPath is skills.sh's record of where each global skill came from.
func (e Env) lockPath() string {
	if state := e.getenv("XDG_STATE_HOME"); state != "" {
		return filepath.Join(state, "skills", ".skill-lock.json")
	}
	return filepath.Join(e.home(), ".agents", ".skill-lock.json")
}

type lockEntry struct {
	Source string `json:"source"`
	Ref    string `json:"ref"`
}

func (e Env) readLock() map[string]lockEntry {
	data, err := os.ReadFile(e.lockPath())
	if err != nil {
		return nil
	}
	var lock struct {
		Skills map[string]lockEntry `json:"skills"`
	}
	if json.Unmarshal(data, &lock) != nil {
		return nil
	}
	return lock.Skills
}

// Status is what the For coding CLIs tab shows.
type Status struct {
	// Version is the Aviary release the bundled skills belong to, or "dev".
	Version string `json:"version"`
	Source  string `json:"source"`
	// Release is false for development builds, which install from the
	// bundled copy instead of a release tag and never update on their own.
	Release bool          `json:"release"`
	CLIs    []CLI         `json:"clis"`
	Skills  []SkillStatus `json:"skills"`
	// Outdated counts installed copies that differ from the bundled skills.
	Outdated int `json:"outdated"`
}

// SkillStatus is one skill and its install in each listed CLI, in the order
// of Status.CLIs.
type SkillStatus struct {
	Name          string    `json:"name"`
	Summary       string    `json:"summary"`
	RunByProjects bool      `json:"run_by_projects"`
	Installs      []Install `json:"installs"`
}

// Install is one skill's copy in one CLI.
type Install struct {
	CLI       string `json:"cli"`
	Installed bool   `json:"installed"`
	// Current is true when the copy matches the bundled skill.
	Current bool `json:"current"`
	// Version is the Aviary release the copy came from, when known.
	Version string `json:"version,omitempty"`
	// Source is the skills.sh source the copy was installed from, when known.
	Source string `json:"source,omitempty"`
	Path   string `json:"path,omitempty"`
}

// Manager reads and installs the coding-CLI skills.
type Manager struct {
	Env Env
	// Bundle holds the expected skills under aviary.CodingSkillsRoot.
	Bundle fs.FS
	// Version is the running Aviary version.
	Version func() string
	// CLIs lists the coding CLIs with a skills.sh target.
	CLIs func() []CLI
	// Run runs npx with args.
	Run func(ctx context.Context, args ...string) error

	mu sync.Mutex
}

var (
	defaultOnce    sync.Once
	defaultManager *Manager
)

// Default is the manager for this binary and this user's home.
func Default() *Manager {
	defaultOnce.Do(func() {
		defaultManager = &Manager{
			Bundle:  aviary.CodingSkills,
			Version: update.CurrentVersion,
			CLIs:    builtinCLIs,
			Run:     runNpx,
		}
	})
	return defaultManager
}

func (m *Manager) version() string {
	if m.Version == nil {
		return "dev"
	}
	return strings.TrimSpace(m.Version())
}

// releaseVersion is the running version without a leading "v", and whether
// it is a release that has a tag to install from.
func (m *Manager) releaseVersion() (string, bool) {
	v := strings.TrimPrefix(m.version(), "v")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return v, false
	}
	for _, part := range parts {
		if part == "" || strings.Trim(part, "0123456789") != "" {
			return v, false
		}
	}
	return v, true
}

// Status reports the installed copy of every skill in every coding CLI on
// PATH.
func (m *Manager) Status() Status {
	version, release := m.releaseVersion()
	if !release {
		version = "dev"
	}
	status := Status{Version: version, Source: Repo, Release: release, CLIs: []CLI{}}
	for _, cli := range m.CLIs() {
		if m.Env.detected(cli.Binary) {
			status.CLIs = append(status.CLIs, cli)
		}
	}
	lock := m.Env.readLock()
	for _, name := range aviary.CodingSkillNames {
		info := skillInfo[name]
		skill := SkillStatus{Name: name, Summary: info.Summary, RunByProjects: info.RunByProjects, Installs: []Install{}}
		for _, cli := range status.CLIs {
			install := m.install(name, cli, lock, version)
			if install.Installed && !install.Current {
				status.Outdated++
			}
			skill.Installs = append(skill.Installs, install)
		}
		status.Skills = append(status.Skills, skill)
	}
	return status
}

func (m *Manager) install(name string, cli CLI, lock map[string]lockEntry, version string) Install {
	dir := filepath.Join(m.Env.SkillsDir(cli.Target), name)
	install := Install{CLI: cli.ID}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		return install
	}
	install.Installed = true
	install.Path = dir
	// The lock describes the canonical copy, so it applies only to a CLI
	// whose skill resolves there.
	if resolved, err := filepath.EvalSymlinks(dir); err == nil {
		canonical, _ := filepath.EvalSymlinks(m.Env.canonicalDir())
		if canonical != "" && resolved == filepath.Join(canonical, name) {
			if entry, ok := lock[name]; ok {
				install.Source = entry.Source
				if entry.Source == Repo && entry.Ref != "" {
					install.Version = strings.TrimPrefix(entry.Ref, "v")
				}
			}
		}
	}
	if m.matchesBundle(name, dir) {
		install.Current = true
		install.Version = version
	}
	return install
}

// matchesBundle reports whether dir holds exactly the bundled files of skill
// name. Line endings are ignored, since a skills.sh clone on Windows may check
// the files out with CRLF.
func (m *Manager) matchesBundle(name, dir string) bool {
	root := path.Join(aviary.CodingSkillsRoot, name)
	matches := true
	err := fs.WalkDir(m.Bundle, root, func(file string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		want, err := fs.ReadFile(m.Bundle, file)
		if err != nil {
			return err
		}
		have, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(strings.TrimPrefix(file, root+"/"))))
		if err != nil || !bytes.Equal(normalizeNewlines(have), normalizeNewlines(want)) {
			matches = false
			return fs.SkipAll
		}
		return nil
	})
	return err == nil && matches
}

func normalizeNewlines(text []byte) []byte {
	return bytes.ReplaceAll(text, []byte("\r\n"), []byte("\n"))
}

// Request selects the copies InstallMissing and Update act on. Empty Skill or
// CLI selects every skill or every coding CLI on PATH.
type Request struct {
	Skill string `json:"skill,omitempty"`
	CLI   string `json:"cli,omitempty"`
	// Missing installs skills a CLI does not have yet.
	Missing bool `json:"missing,omitempty"`
	// Outdated reinstalls copies that differ from the bundled skills.
	Outdated bool `json:"outdated,omitempty"`
}

// Result names the copies an Install call installed, as "skill/cli".
type Result struct {
	Installed []string `json:"installed"`
}

// Install installs every selected copy that is missing or outdated, as the
// request allows, with one `npx skills add` per skill.
func (m *Manager) Install(ctx context.Context, req Request) (Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := Result{Installed: []string{}}
	if req.Skill != "" && !isCodingSkill(req.Skill) {
		return result, fmt.Errorf("unknown coding-CLI skill %q", req.Skill)
	}
	status := m.Status()
	if req.CLI != "" && !hasCLI(status.CLIs, req.CLI) {
		return result, fmt.Errorf("coding CLI %q is not installed", req.CLI)
	}
	targets := map[string]string{}
	for _, cli := range status.CLIs {
		targets[cli.ID] = cli.Target
	}

	var source func(string) string
	if status.Release {
		tag := "v" + status.Version
		source = func(skill string) string {
			return fmt.Sprintf("https://github.com/%s/tree/%s/%s/%s", Repo, tag, aviary.CodingSkillsRoot, skill)
		}
	}
	for _, skill := range status.Skills {
		if req.Skill != "" && skill.Name != req.Skill {
			continue
		}
		var agents, names []string
		for _, install := range skill.Installs {
			if req.CLI != "" && install.CLI != req.CLI {
				continue
			}
			if install.Current || (!install.Installed && !req.Missing) || (install.Installed && !req.Outdated) {
				continue
			}
			agents = appendUnique(agents, targets[install.CLI])
			names = append(names, skill.Name+"/"+install.CLI)
		}
		if len(agents) == 0 {
			continue
		}
		if source == nil {
			dir, cleanup, err := m.extractBundle()
			if err != nil {
				return result, err
			}
			defer cleanup()
			source = func(skill string) string {
				return filepath.Join(dir, filepath.FromSlash(aviary.CodingSkillsRoot), skill)
			}
		}
		runCtx, cancel := context.WithTimeout(ctx, installTimeout)
		err := m.Run(runCtx, addArgs(source(skill.Name), agents)...)
		cancel()
		if err != nil {
			return result, fmt.Errorf("install %s: %w", skill.Name, err)
		}
		result.Installed = append(result.Installed, names...)
	}
	return result, nil
}

// UpdateTo reinstalls every installed copy of the skills from release
// version, the release an upgrade is installing, for every coding CLI on PATH.
// The running binary bundles the release being replaced, so it cannot tell
// which copies already match version and reinstalls them all. Skills a CLI
// does not have are left to Sync after the restart. A version that is not a
// release has no tag to install from and changes nothing.
func (m *Manager) UpdateTo(ctx context.Context, version string) (Result, error) {
	target := &Manager{Env: m.Env, Bundle: m.Bundle, CLIs: m.CLIs, Run: m.Run, Version: func() string { return version }}
	result := Result{Installed: []string{}}
	version, release := target.releaseVersion()
	if !release {
		return result, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	status := target.Status()
	targets := map[string]string{}
	for _, cli := range status.CLIs {
		targets[cli.ID] = cli.Target
	}
	for _, skill := range status.Skills {
		var agents, names []string
		for _, install := range skill.Installs {
			if install.Installed {
				agents = appendUnique(agents, targets[install.CLI])
				names = append(names, skill.Name+"/"+install.CLI)
			}
		}
		if len(agents) == 0 {
			continue
		}
		source := fmt.Sprintf("https://github.com/%s/tree/v%s/%s/%s", Repo, version, aviary.CodingSkillsRoot, skill.Name)
		runCtx, cancel := context.WithTimeout(ctx, installTimeout)
		err := m.Run(runCtx, addArgs(source, agents)...)
		cancel()
		if err != nil {
			return result, fmt.Errorf("install %s: %w", skill.Name, err)
		}
		result.Installed = append(result.Installed, names...)
	}
	return result, nil
}

// addArgs is the npx command line that installs one skill globally for every
// skills.sh target in agents.
func addArgs(source string, agents []string) []string {
	args := []string{"--yes", "skills", "add", source, "--global"}
	for _, agent := range agents {
		args = append(args, "--agent", agent)
	}
	return append(args, "-y")
}

// extractBundle writes the bundled skills to a temporary directory, the
// source a development build installs from since it has no release tag.
func (m *Manager) extractBundle() (string, func(), error) {
	dir, err := os.MkdirTemp("", "aviary-coding-skills-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	err = fs.WalkDir(m.Bundle, aviary.CodingSkillsRoot, func(file string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(dir, filepath.FromSlash(file))
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := fs.ReadFile(m.Bundle, file)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}

// Sync is the automatic upkeep: it installs the skills for coding CLIs that
// lack them and updates outdated copies, as cfg allows. Development builds
// have no release to install from and are left alone.
func (m *Manager) Sync(ctx context.Context, cfg *config.Config) {
	if _, release := m.releaseVersion(); !release {
		return
	}
	req := Request{
		Missing:  config.EffectiveCodingSkillsInstallNewCLIs(cfg),
		Outdated: config.EffectiveCodingSkillsAutoUpdate(cfg),
	}
	if !req.Missing && !req.Outdated {
		return
	}
	result, err := m.Install(ctx, req)
	if len(result.Installed) > 0 {
		slog.Info("coding skills: installed", "skills", strings.Join(result.Installed, ", "))
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		slog.Warn("coding skills: could not install; coding CLIs keep their current copies", "err", err)
	}
}

// RunSync syncs once now and then every interval until ctx ends, so a CLI
// set up later gets the skills without a restart.
func (m *Manager) RunSync(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cfg, err := config.Load("")
		if err == nil {
			m.Sync(ctx, cfg)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// runNpx runs npx with an empty stdin, so skills.sh never waits on a prompt.
func runNpx(ctx context.Context, args ...string) error {
	binary, err := exec.LookPath("npx")
	if err != nil {
		return fmt.Errorf("npx is not installed; install Node.js to install skills: %w", err)
	}
	output, err := exec.CommandContext(ctx, binary, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, lastLine(string(output)))
	}
	return nil
}

func lastLine(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	return strings.TrimSpace(lines[len(lines)-1])
}

func isCodingSkill(name string) bool {
	for _, skill := range aviary.CodingSkillNames {
		if skill == name {
			return true
		}
	}
	return false
}

func hasCLI(clis []CLI, id string) bool {
	for _, cli := range clis {
		if cli.ID == id {
			return true
		}
	}
	return false
}

func appendUnique(list []string, value string) []string {
	for _, existing := range list {
		if existing == value {
			return list
		}
	}
	return append(list, value)
}
