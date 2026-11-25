package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/codingskills"
	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/projects"
)

// Check levels reported by project_check.
const (
	projectCheckOK   = "ok"
	projectCheckWarn = "warn"
	projectCheckFail = "fail"
	// projectCheckInfo is a neutral note that never fails the check.
	projectCheckInfo = "info"
)

// projectCheckTimeout bounds every gh call so a stalled GitHub read cannot
// hang the add-project dialog.
const projectCheckTimeout = 15 * time.Second

var (
	projectGH = func(ctx context.Context, args ...string) ([]byte, error) {
		return projects.GHCLI{Binary: "gh", Timeout: projectCheckTimeout}.Run(ctx, args...)
	}
	// projectCLIProviders lists the CLI providers whose agent definition names a
	// skills.sh target; their skills directories come from codingskills.
	projectCLIProviders = func() []codingskills.CLI { return codingskills.Default().CLIs() }
	projectLookPath     = exec.LookPath
	projectHomeDir      = os.UserHomeDir
	projectConfig       = func() (*config.Config, error) { return config.Load("") }
)

// projectSkills are the skills a project job runs.
var projectSkills = []string{"gh-fix"}

type projectCheckArgs struct {
	Target string `json:"target" jsonschema:"repository (owner/repo or a GitHub URL), Project board or Discussions board to check"`
	Agent  string `json:"agent,omitempty" jsonschema:"Aviary agent that owns or will own the project; its model and fallbacks pick the CLI providers whose skills are checked. Empty uses the agent that owns the target"`
	// Models are the owning agent's model and fallbacks as an unsaved form
	// holds them, so the check follows edits before they are saved.
	Models []string `json:"models,omitempty" jsonschema:"the owning agent's model and fallbacks, in order, in place of its saved ones"`
	// Mode is the mode the project will use, as an unsaved form holds it.
	Mode string `json:"mode,omitempty" jsonschema:"watch to pick up issues matching the filter, or manual to only run work queued on demand; empty uses the saved project's mode, else watch"`
}

// ProjectCheck is one add-project prerequisite and its outcome.
type ProjectCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ProjectCheckCLI is a CLI provider found on PATH and its installed skills.
// Used reports whether the project runs on it, so its skills are required.
type ProjectCheckCLI struct {
	Name   string            `json:"name"`
	Path   string            `json:"path"`
	Used   bool              `json:"used"`
	Skills map[string]string `json:"skills"`
}

// ProjectCheckResult is project_check's response.
type ProjectCheckResult struct {
	Target string            `json:"target"`
	Repo   string            `json:"repo,omitempty"`
	Kind   string            `json:"kind"`
	Fork   bool              `json:"fork"`
	Mode   string            `json:"mode"`
	OK     bool              `json:"ok"`
	Checks []ProjectCheck    `json:"checks"`
	CLIs   []ProjectCheckCLI `json:"clis"`
}

var (
	projectRepoRE  = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
	projectBoardRE = regexp.MustCompile(`^(users|orgs)/[A-Za-z0-9_.-]+/projects/\d+$`)
)

// parseProjectTarget normalizes a target and reports its kind ("repo",
// "board" or "discussions") and, when it names one, its repository.
func parseProjectTarget(raw string) (target, kind, repo string, err error) {
	t := strings.TrimSpace(raw)
	if u, perr := url.Parse(t); perr == nil && (u.Host == "github.com" || u.Host == "www.github.com") {
		t = strings.Trim(u.Path, "/")
		t = strings.TrimSuffix(t, ".git")
	}
	t = strings.TrimSuffix(strings.Trim(t, "/"), ".git")
	switch {
	case projectBoardRE.MatchString(t):
		return t, "board", "", nil
	case projectRepoRE.MatchString(t):
		return t, "repo", t, nil
	}
	parts := strings.Split(t, "/")
	if len(parts) >= 3 && projectRepoRE.MatchString(parts[0]+"/"+parts[1]) {
		switch parts[2] {
		case "discussions":
			return t, "discussions", parts[0] + "/" + parts[1], nil
		case "projects":
			return t, "board", parts[0] + "/" + parts[1], nil
		}
	}
	return "", "", "", fmt.Errorf("%q is not a repository (owner/repo), Project board or Discussions board", raw)
}

// runProjectCheck checks everything a new project needs: GitHub access,
// installed CLI providers and their skills, and whether pull requests
// must come from a fork.
func runProjectCheck(ctx context.Context, args projectCheckArgs) (*ProjectCheckResult, error) {
	target, kind, repo, err := parseProjectTarget(args.Target)
	if err != nil {
		return nil, err
	}
	mode, err := projectCheckMode(target, args.Mode)
	if err != nil {
		return nil, err
	}
	res := &ProjectCheckResult{Target: target, Repo: repo, Kind: kind, Mode: mode}
	add := func(name, status, format string, a ...any) {
		res.Checks = append(res.Checks, ProjectCheck{Name: name, Status: status, Message: fmt.Sprintf(format, a...)})
	}
	if mode == config.ProjectModeManual {
		add("mode", projectCheckInfo, "manual: no issue watching; only work you queue, scheduled project tasks and agent tools start jobs")
	} else {
		add("mode", projectCheckInfo, "watch: open issues matching the project's filter are picked up")
	}

	signedIn := checkGHAuth(ctx, add)
	if signedIn && repo != "" {
		checkRepoAccess(ctx, repo, res, add)
	} else if signedIn {
		add("github", projectCheckOK, "gh is signed in")
	}
	checkCLIProviders(res, projectUsedProviders(target, args), add)

	res.OK = true
	for _, c := range res.Checks {
		if c.Status == projectCheckFail {
			res.OK = false
		}
	}
	return res, nil
}

func checkGHAuth(ctx context.Context, add func(string, string, string, ...any)) bool {
	if _, err := projectLookPath("gh"); err != nil {
		add("github", projectCheckFail, "gh is not installed; install the GitHub CLI and run gh auth login")
		return false
	}
	if _, err := projectGH(ctx, "auth", "status"); err != nil {
		add("github", projectCheckFail, "gh is not signed in; run gh auth login")
		return false
	}
	return true
}

func checkRepoAccess(ctx context.Context, repo string, res *ProjectCheckResult, add func(string, string, string, ...any)) {
	out, err := projectGH(ctx, "api", "repos/"+repo)
	if err != nil {
		add("github", projectCheckFail, "gh is signed in but cannot read %s: %v", repo, err)
		return
	}
	var info struct {
		FullName    string `json:"full_name"`
		Archived    bool   `json:"archived"`
		Permissions struct {
			Push bool `json:"push"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(out, &info); err != nil {
		add("github", projectCheckFail, "unexpected response reading %s: %v", repo, err)
		return
	}
	if info.Archived {
		add("github", projectCheckFail, "%s is archived, so no pull requests can be opened", repo)
		return
	}
	add("github", projectCheckOK, "gh is signed in and can read %s", repo)
	if !info.Permissions.Push {
		res.Fork = true
		add("fork", projectCheckWarn, "You can't push to %s, so pull requests will come from your fork", repo)
	}
}

// projectUsedProviders returns the CLI providers the project runs on, as
// config.ProjectCLIProviders resolves them from the saved configuration.
func projectUsedProviders(target string, args projectCheckArgs) []string {
	cfg, err := projectConfig()
	if err != nil || cfg == nil {
		cfg = &config.Config{}
	}
	agent := strings.TrimSpace(args.Agent)
	if agent == "" {
		agent = cfg.ProjectOwner(target)
	}
	return config.ProjectCLIProviders(cfg, agent, target, args.Models)
}

// checkCLIProviders reports the CLI providers on PATH and whether the ones
// the project uses have its skills. A provider the project does not use
// never fails the check: its missing skills are only noted.
func checkCLIProviders(res *ProjectCheckResult, used []string, add func(string, string, string, ...any)) {
	home, _ := projectHomeDir()
	env := codingskills.Env{Home: home}
	var known, found, usedFound, missingSkill, unusedMissing []string
	for _, def := range projectCLIProviders() {
		known = append(known, def.ID)
		path, err := projectLookPath(def.Binary)
		if err != nil {
			continue
		}
		cli := ProjectCheckCLI{Name: def.ID, Path: path, Used: slices.Contains(used, def.ID), Skills: map[string]string{}}
		missing := false
		for _, skill := range projectSkills {
			if v, ok := findInstalledSkill(env.SkillsDirs(def.Target), skill); ok {
				cli.Skills[skill] = v
			} else {
				missing = true
			}
		}
		switch {
		case missing && cli.Used:
			missingSkill = append(missingSkill, def.ID)
		case missing:
			unusedMissing = append(unusedMissing, def.ID)
		}
		if cli.Used {
			usedFound = append(usedFound, def.ID)
		}
		res.CLIs = append(res.CLIs, cli)
		found = append(found, def.ID)
	}
	skills := strings.Join(projectSkills, ", ")
	if len(found) == 0 {
		add("clis", projectCheckFail, "No CLI provider found; install one of %s", joinList(known, "or"))
		add("skills", projectCheckFail, "%s cannot be installed until a CLI provider is found", skills)
		return
	}
	add("clis", projectCheckOK, "Found %s", joinAnd(found))
	defer func() {
		if len(unusedMissing) > 0 {
			add("skills_unused", projectCheckInfo, "%s also missing for %s (not used)", skills, joinAnd(unusedMissing))
		}
	}()
	if len(missingSkill) > 0 {
		add("skills", projectCheckFail, "%s is not installed for %s", skills, joinAnd(missingSkill))
		return
	}
	if len(usedFound) == 0 {
		add("skills", projectCheckOK, "No installed CLI provider runs this project's jobs, so none needs %s", skills)
		return
	}
	versions := map[string]bool{}
	for _, c := range res.CLIs {
		if !c.Used {
			continue
		}
		for _, v := range c.Skills {
			versions[v] = true
		}
	}
	msg := skills
	if len(versions) == 1 {
		for v := range versions {
			if v != "" {
				msg += " " + v
			}
		}
	}
	switch {
	case len(usedFound) == 1:
		add("skills", projectCheckOK, "%s installed for %s", msg, usedFound[0])
	case len(usedFound) == len(found):
		add("skills", projectCheckOK, "%s installed for all %d", msg, len(found))
	default:
		add("skills", projectCheckOK, "%s installed for %s", msg, joinAnd(usedFound))
	}
}

// findInstalledSkill reports whether skill is installed under any of dirs and
// returns the version from its SKILL.md frontmatter when it declares one.
func findInstalledSkill(dirs []string, skill string) (string, bool) {
	for _, d := range dirs {
		path := filepath.Join(d, skill, "SKILL.md")
		f, err := os.Open(path)
		if err != nil {
			continue
		}
		version := skillFrontmatterVersion(f)
		_ = f.Close()
		return version, true
	}
	return "", false
}

func skillFrontmatterVersion(f *os.File) string {
	sc := bufio.NewScanner(f)
	inFront := false
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "---" {
			if inFront {
				return ""
			}
			inFront = true
			continue
		}
		if !inFront {
			return ""
		}
		if v, ok := strings.CutPrefix(line, "version:"); ok {
			return strings.Trim(strings.TrimSpace(v), `"'`)
		}
	}
	return ""
}

func joinAnd(items []string) string { return joinList(items, "and") }

// joinList joins items as an English list ending in conjunction.
func joinList(items []string, conjunction string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " " + conjunction + " " + items[1]
	}
	return strings.Join(items[:len(items)-1], ", ") + " " + conjunction + " " + items[len(items)-1]
}

// projectCheckMode is the mode project_check reports: mode when given, else
// the saved project's, else watch.
func projectCheckMode(target, mode string) (string, error) {
	if strings.TrimSpace(mode) != "" {
		return config.NormalizeProjectMode(mode)
	}
	if cfg, err := projectConfig(); err == nil {
		for _, project := range cfg.ProjectList() {
			if strings.TrimSpace(project.Target) == target && project.Manual() {
				return config.ProjectModeManual, nil
			}
		}
	}
	return config.ProjectModeWatch, nil
}

func registerProjectCheckTool(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name: "project_check",
		Description: "Check whether a repository, Project board or Discussions board can be watched as a project: " +
			"GitHub access, installed CLI providers and the gh-fix skill on the ones the project runs on, whether pull requests must come from a fork, and the project's mode (watch or manual). " +
			"Read-only; writes nothing.",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectCheckArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		res, err := runProjectCheck(ctx, args)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(res)
	})
}
