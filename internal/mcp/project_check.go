package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lsegal/aviary/internal/codingskills"
	"github.com/lsegal/aviary/internal/projects"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Check levels reported by project_check.
const (
	projectCheckOK   = "ok"
	projectCheckWarn = "warn"
	projectCheckFail = "fail"
)

// projectDefaultReadyLabel is checked when the caller names no ready label.
const projectDefaultReadyLabel = "ready"

// projectCheckTimeout bounds every gh call so a stalled GitHub read cannot
// hang the add-project dialog.
const projectCheckTimeout = 15 * time.Second

var (
	projectGH = func(ctx context.Context, args ...string) ([]byte, error) {
		return projects.GHCLI{Binary: "gh", Timeout: projectCheckTimeout}.Run(ctx, args...)
	}
	// projectCodingCLIs lists the coding CLIs whose agent definition names a
	// skills.sh target; their skills directories come from codingskills.
	projectCodingCLIs = func() []codingskills.CLI { return codingskills.Default().CLIs() }
	projectLookPath   = exec.LookPath
	projectHomeDir    = os.UserHomeDir
)

// projectSkills are the skills a project job runs.
var projectSkills = []string{"gh-fix"}

type projectCheckArgs struct {
	Target     string `json:"target" jsonschema:"repository (owner/repo or a GitHub URL), Project board or Discussions board to check"`
	ReadyLabel string `json:"ready_label,omitempty" jsonschema:"ready label to look for; defaults to ready"`
}

// ProjectCheck is one add-project prerequisite and its outcome.
type ProjectCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

// ProjectCheckCLI is a coding CLI found on PATH and its installed skills.
type ProjectCheckCLI struct {
	Name   string            `json:"name"`
	Path   string            `json:"path"`
	Skills map[string]string `json:"skills"`
}

// ProjectCheckResult is project_check's response.
type ProjectCheckResult struct {
	Target     string            `json:"target"`
	Repo       string            `json:"repo,omitempty"`
	Kind       string            `json:"kind"`
	ReadyLabel string            `json:"ready_label"`
	Fork       bool              `json:"fork"`
	OK         bool              `json:"ok"`
	Checks     []ProjectCheck    `json:"checks"`
	CLIs       []ProjectCheckCLI `json:"clis"`
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
// installed coding CLIs and their skills, the ready label, and whether pull
// requests must come from a fork.
func runProjectCheck(ctx context.Context, args projectCheckArgs) (*ProjectCheckResult, error) {
	target, kind, repo, err := parseProjectTarget(args.Target)
	if err != nil {
		return nil, err
	}
	label := strings.TrimSpace(args.ReadyLabel)
	if label == "" {
		label = projectDefaultReadyLabel
	}
	res := &ProjectCheckResult{Target: target, Repo: repo, Kind: kind, ReadyLabel: label}
	add := func(name, status, format string, a ...any) {
		res.Checks = append(res.Checks, ProjectCheck{Name: name, Status: status, Message: fmt.Sprintf(format, a...)})
	}

	signedIn := checkGHAuth(ctx, add)
	if signedIn && repo != "" {
		if checkRepoAccess(ctx, repo, res, add) && kind == "repo" {
			checkReadyLabel(ctx, repo, label, add)
		}
	} else if signedIn {
		add("github", projectCheckOK, "gh is signed in")
	}
	checkCodingCLIs(res, add)

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

func checkRepoAccess(ctx context.Context, repo string, res *ProjectCheckResult, add func(string, string, string, ...any)) bool {
	out, err := projectGH(ctx, "api", "repos/"+repo)
	if err != nil {
		add("github", projectCheckFail, "gh is signed in but cannot read %s: %v", repo, err)
		return false
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
		return false
	}
	if info.Archived {
		add("github", projectCheckFail, "%s is archived, so no pull requests can be opened", repo)
		return false
	}
	add("github", projectCheckOK, "gh is signed in and can read %s", repo)
	if !info.Permissions.Push {
		res.Fork = true
		add("fork", projectCheckWarn, "You can't push to %s, so pull requests will come from your fork", repo)
	}
	return true
}

func checkReadyLabel(ctx context.Context, repo, label string, add func(string, string, string, ...any)) {
	if _, err := projectGH(ctx, "api", "repos/"+repo+"/labels/"+url.PathEscape(label)); err != nil {
		add("ready_label", projectCheckFail, "%s has no %q label; create it or pick another ready label", repo, label)
		return
	}
	q := fmt.Sprintf("repo:%s is:issue is:open label:%q", repo, label)
	out, err := projectGH(ctx, "api", "-X", "GET", "search/issues", "-f", "q="+q, "-f", "per_page=1", "--jq", ".total_count")
	count := strings.TrimSpace(string(out))
	if err != nil || count == "" {
		add("ready_label", projectCheckOK, "%q exists", label)
		return
	}
	noun := "issues"
	if count == "1" {
		noun = "issue"
	}
	add("ready_label", projectCheckOK, "%q exists, with %s open %s", label, count, noun)
}

func checkCodingCLIs(res *ProjectCheckResult, add func(string, string, string, ...any)) {
	home, _ := projectHomeDir()
	env := codingskills.Env{Home: home}
	var known, found, missingSkill []string
	for _, def := range projectCodingCLIs() {
		known = append(known, def.ID)
		path, err := projectLookPath(def.Binary)
		if err != nil {
			continue
		}
		cli := ProjectCheckCLI{Name: def.ID, Path: path, Skills: map[string]string{}}
		for _, skill := range projectSkills {
			if v, ok := findInstalledSkill(env.SkillsDirs(def.Target), skill); ok {
				cli.Skills[skill] = v
			} else {
				missingSkill = append(missingSkill, def.ID)
			}
		}
		res.CLIs = append(res.CLIs, cli)
		found = append(found, def.ID)
	}
	if len(found) == 0 {
		add("clis", projectCheckFail, "No coding CLI found; install one of %s", joinList(known, "or"))
		add("skills", projectCheckFail, "%s cannot be installed until a coding CLI is found", strings.Join(projectSkills, ", "))
		return
	}
	add("clis", projectCheckOK, "Found %s", joinAnd(found))
	if len(missingSkill) > 0 {
		add("skills", projectCheckFail, "%s is not installed for %s", strings.Join(projectSkills, ", "), joinAnd(missingSkill))
		return
	}
	versions := map[string]bool{}
	for _, c := range res.CLIs {
		for _, v := range c.Skills {
			versions[v] = true
		}
	}
	msg := strings.Join(projectSkills, ", ")
	if len(versions) == 1 {
		for v := range versions {
			if v != "" {
				msg += " " + v
			}
		}
	}
	if len(found) == 1 {
		add("skills", projectCheckOK, "%s installed for %s", msg, found[0])
	} else {
		add("skills", projectCheckOK, "%s installed for all %d", msg, len(found))
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

func registerProjectCheckTool(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name: "project_check",
		Description: "Check whether a repository, Project board or Discussions board can be watched as a project: " +
			"GitHub access, installed coding CLIs and their gh-fix skill, the ready label, and whether pull requests must come from a fork. " +
			"Read-only; writes nothing.",
		Annotations: readOnlyTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectCheckArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		slog.Info("mcp: tool call", "component", "projects", "tool", "project_check", "target", args.Target)
		res, err := runProjectCheck(ctx, args)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(res)
	})
}
