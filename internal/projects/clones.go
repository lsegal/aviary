package projects

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// cloneMinFreeBytes is the free space a temp directory must have before a new
// clone is placed in it (issue #830). A directory with less is skipped for the
// next one in the configured order.
var cloneMinFreeBytes uint64 = 2 << 30

// diskFree reports the bytes available to this user on the volume holding
// dir. It is a variable so tests can simulate a full directory.
var diskFree = diskFreeBytes

// errNoCloneSpace is returned when no sanctioned temp directory has room for
// a new clone. Clones are never placed anywhere else (issue #830).
var errNoCloneSpace = errors.New("no temp directory has room for a new clone")

// cloneRoots returns the sanctioned directories clones may live in, in the
// order new clones try them. An empty configuration means the system temp
// directory, which is implied rather than stored (issue #830).
func cloneRoots(configured []string) []string {
	roots := make([]string, 0, len(configured))
	seen := map[string]bool{}
	for _, dir := range configured {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if abs, err := filepath.Abs(dir); err == nil {
			dir = abs
		}
		dir = filepath.Clean(dir)
		key := strings.ToLower(dir)
		if seen[key] {
			continue
		}
		seen[key] = true
		roots = append(roots, dir)
	}
	if len(roots) == 0 {
		return []string{filepath.Clean(os.TempDir())}
	}
	return roots
}

// normalizeTempDirs validates a temp-directories setting: every entry must be
// an absolute path, and blanks are dropped. An empty result means the system
// temp directory.
func normalizeTempDirs(dirs []string) ([]string, error) {
	result := make([]string, 0, len(dirs))
	for _, dir := range dirs {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if !filepath.IsAbs(dir) {
			return nil, fmt.Errorf("temp directory %q must be an absolute path", dir)
		}
		result = append(result, filepath.Clean(dir))
	}
	return result, nil
}

// pickCloneRoot returns the first root, in order, that exists (or can be
// created) and has at least cloneMinFreeBytes free. When none does, it fails
// with errNoCloneSpace and a description of every root it tried, and never
// falls back to another location.
func pickCloneRoot(roots []string) (string, error) {
	var tried []string
	for _, root := range roots {
		if err := os.MkdirAll(root, 0o755); err != nil {
			tried = append(tried, fmt.Sprintf("%s (%v)", root, err))
			continue
		}
		free, err := diskFree(root)
		if err != nil {
			tried = append(tried, fmt.Sprintf("%s (%v)", root, err))
			continue
		}
		if free < cloneMinFreeBytes {
			tried = append(tried, fmt.Sprintf("%s (%s free)", root, formatBytes(int64(free))))
			continue
		}
		return root, nil
	}
	return "", fmt.Errorf("%w: need %s free in one of: %s", errNoCloneSpace, formatBytes(int64(cloneMinFreeBytes)), strings.Join(tried, ", "))
}

// underRoots reports whether path lies inside one of roots.
func underRoots(path string, roots []string) bool {
	path = filepath.Clean(path)
	for _, root := range roots {
		rel, err := filepath.Rel(root, path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		return true
	}
	return false
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value, suffix := float64(n), "KMGTPE"
	i := -1
	for value >= unit && i < len(suffix)-1 {
		value /= unit
		i++
	}
	return fmt.Sprintf("%.1f %ciB", value, suffix[i])
}

// cloneRecord is the bookkeeping for one clone glorp created or adopted.
type cloneRecord struct {
	Path                  string    `json:"path"`
	Repository            string    `json:"repository,omitempty"`
	Issue                 int       `json:"issue,omitempty"`
	PullRequest           int       `json:"pullRequest,omitempty"`
	PullRequestRepository string    `json:"pullRequestRepository,omitempty"`
	Job                   string    `json:"job,omitempty"`
	Created               time.Time `json:"created"`
	// Adopted marks a clone found on disk rather than announced by a run.
	Adopted bool `json:"adopted,omitempty"`
}

// cloneRegistry persists every clone record beside the work state file, so
// the list outlives the jobs and survives restarts (issue #830).
type cloneRegistry struct {
	mu      sync.Mutex
	path    string
	records map[string]cloneRecord
}

// clonesPathFor names the registry file kept beside statePath: a state file
// .glorp.json keeps its clones in .glorp.clones.json. An empty state path
// keeps the registry in memory only.
func clonesPathFor(statePath string) string {
	if statePath == "" {
		return ""
	}
	ext := filepath.Ext(statePath)
	return strings.TrimSuffix(statePath, ext) + ".clones.json"
}

func loadCloneRegistry(path string) (*cloneRegistry, error) {
	reg := &cloneRegistry{path: path, records: map[string]cloneRecord{}}
	if path == "" {
		return reg, nil
	}
	raw, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return reg, nil
	}
	if err != nil {
		return nil, err
	}
	var list []cloneRecord
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("decode clones %s: %w", path, err)
	}
	for _, rec := range list {
		if rec.Path != "" {
			reg.records[cloneKey(rec.Path)] = rec
		}
	}
	return reg, nil
}

func cloneKey(path string) string {
	key := filepath.Clean(path)
	if filepath.Separator == '\\' {
		key = strings.ToLower(key)
	}
	return key
}

// saveLocked writes the registry; the caller holds mu.
func (r *cloneRegistry) saveLocked() error {
	if r.path == "" {
		return nil
	}
	list := r.listLocked()
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, append(raw, '\n'), 0o600)
}

func (r *cloneRegistry) listLocked() []cloneRecord {
	list := make([]cloneRecord, 0, len(r.records))
	for _, rec := range r.records {
		list = append(list, rec)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	return list
}

// record adds or updates a clone, keeping fields the update leaves empty.
func (r *cloneRegistry) record(update cloneRecord) error {
	if r == nil || update.Path == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := cloneKey(update.Path)
	rec, ok := r.records[key]
	if !ok {
		rec = cloneRecord{Path: filepath.Clean(update.Path), Created: update.Created, Adopted: update.Adopted}
		if rec.Created.IsZero() {
			rec.Created = time.Now()
		}
	} else if !update.Adopted {
		rec.Adopted = false
	}
	changed := !ok
	set := func(dst *string, value string) {
		if value != "" && *dst != value {
			*dst, changed = value, true
		}
	}
	set(&rec.Repository, update.Repository)
	set(&rec.Job, update.Job)
	set(&rec.PullRequestRepository, update.PullRequestRepository)
	if update.Issue != 0 && rec.Issue != update.Issue {
		rec.Issue, changed = update.Issue, true
	}
	if update.PullRequest != 0 && rec.PullRequest != update.PullRequest {
		rec.PullRequest, changed = update.PullRequest, true
	}
	if !changed {
		return nil
	}
	r.records[key] = rec
	return r.saveLocked()
}

func (r *cloneRegistry) list() []cloneRecord {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.listLocked()
}

func (r *cloneRegistry) get(path string) (cloneRecord, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[cloneKey(path)]
	return rec, ok
}

func (r *cloneRegistry) remove(path string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.records, cloneKey(path))
	return r.saveLocked()
}

// cloneDirPattern matches the clone directory names the gh-fix skill creates,
// such as glorp-gh-fix-830.
var cloneDirPattern = regexp.MustCompile(`^(.+)-gh-fix-(\d+)$`)

// adopt records clones already on disk so leftovers from before bookkeeping,
// or from before a restart, are listed too: every checkout the work state
// names, and every directory under a root that matches the gh-fix clone name
// and holds a git repository.
func (r *cloneRegistry) adopt(roots []string, work map[string]workState) {
	if r == nil {
		return
	}
	for key, state := range work {
		if state.CheckoutDirectory == "" || !isDir(state.CheckoutDirectory) {
			continue
		}
		rec := cloneRecord{Path: state.CheckoutDirectory, Job: key, Adopted: true}
		if separator := strings.LastIndexByte(key, '#'); separator > 0 {
			rec.Issue, _ = strconv.Atoi(key[separator+1:])
		}
		rec.Repository = gitOriginRepository(state.CheckoutDirectory)
		_ = r.record(rec)
	}
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			match := cloneDirPattern.FindStringSubmatch(entry.Name())
			if match == nil || !entry.IsDir() {
				continue
			}
			path := filepath.Join(root, entry.Name())
			if !isDir(filepath.Join(path, ".git")) {
				continue
			}
			number, _ := strconv.Atoi(match[2])
			repo := gitOriginRepository(path)
			rec := cloneRecord{Path: path, Repository: repo, Issue: number, Adopted: true}
			if repo != "" {
				rec.Job = jobKeyFor(repo, number, work)
			}
			_ = r.record(rec)
		}
	}
}

// jobKeyFor finds the work key of repo's issue number, preferring one the
// work state already holds.
func jobKeyFor(repo string, number int, work map[string]workState) string {
	suffix := "#" + strconv.Itoa(number)
	for key := range work {
		if strings.HasSuffix(key, suffix) && strings.EqualFold(strings.TrimSuffix(key, suffix), repo) {
			return key
		}
	}
	return repo + suffix
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

var githubRemotePattern = regexp.MustCompile(`github\.com[:/]+([^/\s]+)/([^/\s]+?)(?:\.git)?/?$`)

// gitOriginRepository reads OWNER/REPO from a clone's origin remote without
// running git, or returns "" when it cannot.
func gitOriginRepository(dir string) string {
	file, err := os.Open(filepath.Join(dir, ".git", "config"))
	if err != nil {
		return ""
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	inOrigin := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "[") {
			inOrigin = line == `[remote "origin"]`
			continue
		}
		if !inOrigin || !strings.HasPrefix(line, "url") {
			continue
		}
		_, url, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if match := githubRemotePattern.FindStringSubmatch(strings.TrimSpace(url)); match != nil {
			return match[1] + "/" + match[2]
		}
	}
	return ""
}

// cloneUsage is what deciding whether a clone is in use reads: the work
// state (or why it could not be read), the live agent runs, the published
// jobs, and the sanctioned roots.
type cloneUsage struct {
	work    map[string]workState
	workErr error
	live    []checkpointRun
	jobs    map[string]JobSnapshot
	roots   []string
}

// inUseStatuses are the work statuses whose job may still need its clone. A
// paused job resumes from its checkout, so it is in use by definition.
var inUseStatuses = map[string]bool{"active": true, "waiting": true, "queued": true, "paused": true, "stopping": true}

// classifyClone decides whether rec may still be needed. Anything that cannot
// be verified is reported in use, with the reason, so clearing keeps it.
func classifyClone(rec cloneRecord, usage cloneUsage) (bool, string) {
	if usage.workErr != nil {
		return true, "kept: work state could not be read: " + usage.workErr.Error()
	}
	for _, run := range usage.live {
		if run.Checkout != "" && (cloneKey(run.Checkout) == cloneKey(rec.Path) || underRoots(run.Checkout, []string{rec.Path})) {
			return true, fmt.Sprintf("in use: an agent is working in it for #%d", run.Number)
		}
		if run.Checkout == "" && rec.Issue != 0 && run.Number == rec.Issue {
			return true, fmt.Sprintf("in use: an agent for #%d has not announced its checkout yet", run.Number)
		}
	}
	if job, ok := usage.jobs[rec.Job]; ok && inUseStatuses[job.Status] {
		return true, "in use: job is " + job.Status
	}
	state, known := usage.work[rec.Job]
	if known && inUseStatuses[state.Status] {
		return true, "in use: job is " + state.Status
	}
	for key, other := range usage.work {
		if other.CheckoutDirectory != "" && cloneKey(other.CheckoutDirectory) == cloneKey(rec.Path) && inUseStatuses[other.Status] {
			return true, fmt.Sprintf("in use: job %s is %s", key, other.Status)
		}
	}
	if !underRoots(rec.Path, usage.roots) {
		return true, "kept: outside the configured temp directories; remove it by hand"
	}
	if rec.Adopted && !known {
		return true, "kept: no job of this instance records it, so another instance may own it"
	}
	if _, err := os.Stat(rec.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return true, "kept: " + err.Error()
	}
	if known && state.Status != "" {
		return false, "unused: job is " + state.Status
	}
	return false, "unused: its job is no longer tracked"
}

// listClones enumerates every recorded clone with its in-use decision,
// filling in pull request numbers the published jobs know.
func listClones(reg *cloneRegistry, usage cloneUsage) core.ClonesSnapshot {
	snapshot := core.ClonesSnapshot{Roots: usage.roots, Clones: []core.CloneEntry{}}
	for _, rec := range reg.list() {
		if job, ok := usage.jobs[rec.Job]; ok && job.PullRequest != 0 {
			rec.PullRequest, rec.PullRequestRepository = job.PullRequest, job.PullRequestRepository
			_ = reg.record(cloneRecord{Path: rec.Path, PullRequest: job.PullRequest, PullRequestRepository: job.PullRequestRepository})
		}
		if !isDir(rec.Path) {
			if _, err := os.Stat(rec.Path); errors.Is(err, fs.ErrNotExist) {
				// Removed outside glorp; nothing is left to list or clear.
				_ = reg.remove(rec.Path)
				continue
			}
		}
		inUse, reason := classifyClone(rec, usage)
		snapshot.Clones = append(snapshot.Clones, core.CloneEntry{
			Path: rec.Path, Repository: rec.Repository, Issue: rec.Issue,
			PullRequest: rec.PullRequest, PullRequestRepository: rec.PullRequestRepository,
			Job: rec.Job, InUse: inUse, Reason: reason,
		})
	}
	return snapshot
}

// clearClones removes every unused clone and reports the space reclaimed.
// The in-use decision is made again for each clone right before removing it.
func clearClones(reg *cloneRegistry, usage func() cloneUsage) core.ClearClonesResult {
	result := core.ClearClonesResult{Removed: []core.CloneEntry{}}
	for _, entry := range listClones(reg, usage()).Clones {
		if !entry.InUse {
			// Read again right before removing, so a job dispatched since
			// the list was made keeps its clone.
			if rec, ok := reg.get(entry.Path); ok {
				entry.InUse, entry.Reason = classifyClone(rec, usage())
			}
		}
		if entry.InUse {
			result.Kept++
			continue
		}
		size := dirSize(entry.Path)
		if err := removeTree(entry.Path); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entry.Path, err))
			result.Kept++
			continue
		}
		_ = reg.remove(entry.Path)
		entry.Size = size
		result.Reclaimed += size
		result.Removed = append(result.Removed, entry)
	}
	return result
}

// dirSize totals the regular files under path. WalkDir never follows
// symlinks or Windows junctions, so nothing outside the clone is counted.
func dirSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

// removeTree deletes a clone, clearing read-only bits (git marks its object
// files read-only, which Windows refuses to delete) when the first try fails.
func removeTree(path string) error {
	if err := os.RemoveAll(path); err == nil {
		return nil
	}
	_ = filepath.WalkDir(path, func(p string, _ fs.DirEntry, err error) error {
		if err == nil {
			_ = os.Chmod(p, 0o700)
		}
		return nil
	})
	return os.RemoveAll(path)
}

// tempDirFlag is the repeatable --temp-dir switch: the ordered temp
// directories new clones are placed in (issue #830).
type tempDirFlag struct {
	values []string
}

func (f *tempDirFlag) String() string {
	if f == nil {
		return ""
	}
	return strings.Join(f.values, ", ")
}

func (f *tempDirFlag) Set(value string) error {
	dirs, err := normalizeTempDirs([]string{value})
	if err != nil {
		return err
	}
	f.values = append(f.values, dirs...)
	return nil
}

func (f *tempDirFlag) Get() any {
	return append([]string{}, f.values...)
}
