package projects

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cloneReuseBudget bounds how long preparing a reused clone may take, fetch
// included, before the job gives up on it and clones fresh instead.
const cloneReuseBudget = 5 * time.Minute

// keptDependencyDirs are the ignored directories a reused clone keeps, so the
// next job does not pay for a fresh dependency install. Every other untracked
// or ignored file is removed.
var keptDependencyDirs = []string{"node_modules", ".venv"}

// keptCoreConfig are the core settings git writes on clone; a reused clone
// keeps only these, its extensions, and its origin URL.
var keptCoreConfig = []string{"repositoryformatversion", "filemode", "bare", "logallrefupdates", "symlinks", "ignorecase", "precomposeunicode"}

// pickReusableClone returns the oldest recorded clone of repo that no job
// holds, still exists as a clone of repo, and lies in a sanctioned temp
// directory. It applies the same in-use decision project_clone_list reports.
func pickReusableClone(reg *cloneRegistry, repo string, usage cloneUsage) (cloneRecord, bool) {
	if reg == nil || repo == "" || usage.workErr != nil {
		return cloneRecord{}, false
	}
	candidates := reg.list()
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].Created.Before(candidates[j].Created) })
	for _, rec := range candidates {
		if !strings.EqualFold(rec.Repository, repo) || !isDir(filepath.Join(rec.Path, ".git")) {
			continue
		}
		if origin := gitOriginRepository(rec.Path); origin != "" && !strings.EqualFold(origin, repo) {
			continue
		}
		if verdict, _ := classifyClone(rec, usage); verdict != cloneUnused {
			continue
		}
		return rec, true
	}
	return cloneRecord{}, false
}

// prepareReusedClone brings a clone left by an earlier job to the state a
// fresh clone starts in: local config reduced to what clone writes, latest
// origin fetched, the default branch checked out at its remote tip, and every
// other branch, stash, worktree, hook, untracked and ignored file removed
// (dependency directories excepted).
func prepareReusedClone(ctx context.Context, git checkpointGit, dir string) error {
	if err := resetCloneConfig(ctx, git, dir); err != nil {
		return fmt.Errorf("reset config: %w", err)
	}
	gitDir := filepath.Join(dir, ".git")
	for _, name := range []string{"rebase-merge", "rebase-apply", "sequencer", "MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "index.lock"} {
		_ = os.RemoveAll(filepath.Join(gitDir, name))
	}
	if entries, err := os.ReadDir(filepath.Join(gitDir, "hooks")); err == nil {
		for _, entry := range entries {
			if !strings.HasSuffix(entry.Name(), ".sample") {
				_ = os.RemoveAll(filepath.Join(gitDir, "hooks", entry.Name()))
			}
		}
	}
	if _, err := git(ctx, dir, "fetch", "--prune", "origin"); err != nil {
		return fmt.Errorf("fetch: %w", err)
	}
	branch := "main"
	if _, err := git(ctx, dir, "remote", "set-head", "origin", "--auto"); err == nil {
		if head, err := git(ctx, dir, "symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && strings.HasPrefix(head, "origin/") {
			branch = strings.TrimPrefix(head, "origin/")
		}
	}
	steps := [][]string{
		{"checkout", "--force", "-B", branch, "--track", "origin/" + branch},
		{"reset", "--hard", "origin/" + branch},
		append([]string{"clean", "-ffdx"}, cleanExcludes()...),
		{"stash", "clear"},
		{"worktree", "prune"},
	}
	for _, args := range steps {
		if _, err := git(ctx, dir, args...); err != nil {
			return fmt.Errorf("%s: %w", args[0], err)
		}
	}
	branches, err := git(ctx, dir, "for-each-ref", "--format=%(refname:short)", "refs/heads/")
	if err != nil {
		return fmt.Errorf("list branches: %w", err)
	}
	for _, name := range strings.Fields(branches) {
		if name == branch {
			continue
		}
		if _, err := git(ctx, dir, "branch", "-D", name); err != nil {
			return fmt.Errorf("delete branch %s: %w", name, err)
		}
	}
	return nil
}

func cleanExcludes() []string {
	args := make([]string, 0, 2*len(keptDependencyDirs))
	for _, name := range keptDependencyDirs {
		args = append(args, "-e", name)
	}
	return args
}

// resetCloneConfig rewrites the clone's local config with only the core
// settings clone writes, its extensions, and the origin URL with the default
// fetch refspec, so remotes, identities, branch settings and anything else an
// earlier job configured are gone.
func resetCloneConfig(ctx context.Context, git checkpointGit, dir string) error {
	url, err := git(ctx, dir, "config", "--local", "--get", "remote.origin.url")
	if err != nil || url == "" {
		return fmt.Errorf("no origin remote")
	}
	var b strings.Builder
	b.WriteString("[core]\n")
	for _, key := range keptCoreConfig {
		if value, err := git(ctx, dir, "config", "--local", "--get", "core."+key); err == nil && value != "" {
			fmt.Fprintf(&b, "\t%s = %s\n", key, quoteConfigValue(value))
		}
	}
	if extensions, err := git(ctx, dir, "config", "--local", "--get-regexp", `^extensions\.`); err == nil && extensions != "" {
		b.WriteString("[extensions]\n")
		for _, line := range strings.Split(extensions, "\n") {
			name, value, _ := strings.Cut(strings.TrimSpace(line), " ")
			fmt.Fprintf(&b, "\t%s = %s\n", strings.TrimPrefix(name, "extensions."), quoteConfigValue(value))
		}
	}
	fmt.Fprintf(&b, "[remote \"origin\"]\n\turl = %s\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n", quoteConfigValue(url))
	return os.WriteFile(filepath.Join(dir, ".git", "config"), []byte(b.String()), 0o644)
}

// quoteConfigValue quotes a config value only when git would otherwise read
// it differently.
func quoteConfigValue(value string) string {
	if !strings.ContainsAny(value, "\"\\;#") && strings.TrimSpace(value) == value {
		return value
	}
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
