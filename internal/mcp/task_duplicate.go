package mcp

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/lsegal/aviary/internal/config"
)

// Duplicating a scheduled task (#1465): the copy's name, and the source
// repository's mentions in its prompt when it is copied to another project.
// web/src/lib/taskDuplicate.ts mirrors both for the Duplicate form.

var taskNameSuffix = regexp.MustCompile(`^(.+)-(\d+)$`)

// nextTaskName is the name a copy of the task named source gets among the
// destination's taken task names: source itself when it is free there,
// else source with the next free numeric suffix. A source that already
// ends in -N counts up from the highest index its base has taken, so x and
// x-2 both copy to x-3.
func nextTaskName(source string, taken []string) string {
	names := make(map[string]bool, len(taken))
	for _, n := range taken {
		names[n] = true
	}
	if !names[source] {
		return source
	}
	base, highest := source, 1
	if m := taskNameSuffix.FindStringSubmatch(source); m != nil {
		base = m[1]
		highest, _ = strconv.Atoi(m[2])
	}
	for n := range names {
		if rest, ok := strings.CutPrefix(n, base+"-"); ok {
			if i, err := strconv.Atoi(rest); err == nil && strconv.Itoa(i) == rest && i > highest {
				highest = i
			}
		}
	}
	next := highest + 1
	for names[fmt.Sprintf("%s-%d", base, next)] {
		next++
	}
	return fmt.Sprintf("%s-%d", base, next)
}

// repoPathToken matches a path-like token: absolute, home-relative or
// relative with a slash.
var repoPathToken = regexp.MustCompile(`(?:~|\.{1,2})?(?:/\.{0,2}[\w@+-]+(?:\.[\w@+-]+)*)+/?`)

func isSlugChar(r rune) bool {
	return r == '_' || r == '-' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

// repoSlugSpans returns the byte spans of repository repo's owner/repo
// name in text, case-insensitive and as a whole word: not part of a longer
// name such as owner/repo-docs, though owner/repo.git counts.
func repoSlugSpans(text, repo string) [][2]int {
	if repo == "" {
		return nil
	}
	lower, needle := strings.ToLower(text), strings.ToLower(repo)
	var spans [][2]int
	for from := 0; ; {
		i := strings.Index(lower[from:], needle)
		if i < 0 {
			return spans
		}
		start, end := from+i, from+i+len(needle)
		from = start + 1
		if start > 0 {
			if r := rune(text[start-1]); isSlugChar(r) || r == '.' {
				continue
			}
		}
		if end < len(text) {
			r := rune(text[end])
			if isSlugChar(r) {
				continue
			}
			if r == '.' && end+1 < len(text) && isSlugChar(rune(text[end+1])) && !strings.HasPrefix(lower[end:], ".git") {
				continue
			}
		}
		spans = append(spans, [2]int{start, end})
		from = end
	}
}

// replaceRepoSlug replaces repo's owner/repo mentions in text with with.
func replaceRepoSlug(text, repo, with string) string {
	spans := repoSlugSpans(text, repo)
	var b strings.Builder
	last := 0
	for _, s := range spans {
		b.WriteString(text[last:s[0]])
		b.WriteString(with)
		last = s[1]
	}
	b.WriteString(text[last:])
	return b.String()
}

func repoBaseName(repo string) string {
	return repo[strings.LastIndex(repo, "/")+1:]
}

// repoReferences returns the mentions of repository source (owner/repo) in
// prompt: its owner/repo name, which GitHub URLs contain, and local
// checkout paths ending in, or running through, a directory named after it.
func repoReferences(prompt, source string) []string {
	if !strings.Contains(source, "/") {
		return nil
	}
	var found []string
	for _, s := range repoSlugSpans(prompt, source) {
		found = append(found, prompt[s[0]:s[1]])
	}
	name := repoBaseName(source)
	for _, token := range repoPathToken.FindAllString(replaceRepoSlug(prompt, source, " "), -1) {
		for _, part := range strings.Split(token, "/") {
			if part == name {
				found = append(found, token)
				break
			}
		}
	}
	return found
}

// replaceRepoReferences replaces repository source's mentions in prompt,
// as repoReferences finds them, with repository dest.
func replaceRepoReferences(prompt, source, dest string) string {
	if !strings.Contains(source, "/") || !strings.Contains(dest, "/") {
		return prompt
	}
	const placeholder = "\x00repo\x00"
	from, to := repoBaseName(source), repoBaseName(dest)
	out := repoPathToken.ReplaceAllStringFunc(replaceRepoSlug(prompt, source, placeholder), func(token string) string {
		parts := strings.Split(token, "/")
		for i, p := range parts {
			if p == from {
				parts[i] = to
			}
		}
		return strings.Join(parts, "/")
	})
	return strings.ReplaceAll(out, placeholder, dest)
}

type projectTaskDuplicateArgs struct {
	Name                  string `json:"name" jsonschema:"The task to duplicate: its name, or <agent>/<task> as project_task_list returns it in id"`
	Project               string `json:"project,omitempty" jsonschema:"The source task's watched project; needed only when tasks of several projects share the name"`
	NewName               string `json:"new_name,omitempty" jsonschema:"The copy's name; defaults to the source's name, with the next free -N suffix when the destination's agent already has a task by that name"`
	NewProject            string `json:"new_project,omitempty" jsonschema:"The watched project (owner/repo) the copy runs on; defaults to the source's project"`
	ReplaceRepoReferences bool   `json:"replace_repo_references,omitempty" jsonschema:"Replace the source project's owner/repo name, GitHub URLs and checkout paths in the prompt with new_project's. Leave out to copy the prompt unchanged; the result lists the references either way"`
	DryRun                bool   `json:"dry_run,omitempty" jsonschema:"Validate and preview the copy without saving it"`
}

// duplicateProjectTask saves a copy of a watched project's scheduled task,
// through project_task_create, on its own or another watched project. The
// copy keeps the source's prompt, trigger, model, send_to, precompile and
// enabled settings, but not its compiled script, run history or session.
func duplicateProjectTask(ctx context.Context, args projectTaskDuplicateArgs) (map[string]any, error) {
	_, watched, err := watchedProjects()
	if err != nil {
		return nil, err
	}
	src, err := findProjectTask(watched, args.Name, args.Project)
	if err != nil {
		return nil, err
	}
	dest := strings.TrimSpace(args.NewProject)
	if dest == "" {
		dest = src.task.Project
	}
	if err := requireWatched(watched, dest); err != nil {
		return nil, err
	}
	destAgent := watched[dest]
	if destAgent == "" {
		return nil, fmt.Errorf("%s is a runtime watch no agent owns, and a scheduled task belongs to an agent; save the project to an agent's projects first", dest)
	}
	cfg, err := config.Load("")
	if err != nil {
		return nil, err
	}
	var taken []string
	for _, a := range cfg.Agents {
		if a.Name == destAgent {
			for _, t := range a.Tasks {
				taken = append(taken, t.Name)
			}
		}
	}
	name := strings.TrimSpace(args.NewName)
	if name == "" {
		name = nextTaskName(src.task.Name, taken)
	}
	prompt := src.task.Prompt
	var refs []string
	if !strings.EqualFold(dest, src.task.Project) {
		refs = repoReferences(prompt, src.task.Project)
		if args.ReplaceRepoReferences {
			prompt = replaceRepoReferences(prompt, src.task.Project, dest)
		}
	}
	precompile := EffectiveTaskPrecompile(src.task)
	tc := src.task
	result, err := createProjectTask(ctx, projectTaskCreateArgs{
		Project: dest, Name: name, Content: prompt, Model: tc.Model, SendTo: tc.Target,
		Precompile: &precompile, DryRun: args.DryRun,
		projectTaskTriggerArgs: projectTaskTriggerArgs{
			Schedule: tc.Schedule, Trigger: tc.Trigger, Every: tc.Every, At: tc.At, Days: tc.Days,
			Timezone: tc.Timezone, StartAt: tc.StartAt, RunOnce: tc.RunOnce, Overlap: tc.Overlap, Missed: tc.Missed,
		},
	})
	if err != nil {
		return nil, err
	}
	result["duplicated_from"] = src.id()
	result["name"] = name
	if refs == nil {
		refs = []string{}
	}
	result["repo_references"] = map[string]any{
		"source":   src.task.Project,
		"found":    refs,
		"replaced": args.ReplaceRepoReferences && len(refs) > 0,
	}
	if args.DryRun || config.BoolOr(tc.Enabled, true) {
		return result, nil
	}
	paused := false
	update, err := updateProjectTask(projectTaskUpdateArgs{Name: destAgent + "/" + name, Enabled: &paused}, "paused")
	if err != nil {
		return nil, fmt.Errorf("the copy was saved, but pausing it like its source failed: %w", err)
	}
	result["task"] = update["task"]
	return result, nil
}

func registerProjectTaskDuplicateTool(s *sdkmcp.Server) {
	addTool(s, &sdkmcp.Tool{
		Name:        "project_task_duplicate",
		Description: "Save a copy of a watched project's scheduled task through project_task_create, on the same or another watched project (new_project), as the Scheduled Tasks UI's Duplicate does. The copy keeps the source's prompt, trigger fields, model, send_to, precompile and enabled settings, validated against the destination; it starts with no compiled script, run history or session, and is compiled for the destination when precompile is on. Its name defaults to the source's, with the next free -N suffix only when the destination's agent already has a task by that name. The prompt's references to the source project are listed in repo_references and replaced only with replace_repo_references=true. dry_run=true previews it without saving",
		Annotations: mutatingTool,
	}, func(ctx context.Context, _ *sdkmcp.CallToolRequest, args projectTaskDuplicateArgs) (*sdkmcp.CallToolResult, struct{}, error) {
		if strings.TrimSpace(args.Name) == "" {
			return nil, struct{}{}, errors.New("name is required")
		}
		value, err := duplicateProjectTask(ctx, args)
		if err != nil {
			return nil, struct{}{}, err
		}
		return jsonResult(value)
	})
}
