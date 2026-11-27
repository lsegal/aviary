package projects

import (
	"bufio"
	"context"
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
	"github.com/lsegal/aviary/internal/store"
)

// cloneMinFreeBytes is the free space a temp directory must have before a new
// clone is placed in it. A directory with less is skipped for the
// next one in the configured order.
var cloneMinFreeBytes uint64 = 2 << 30

// diskFree reports the bytes available to this user on the volume holding
// dir. It is a variable so tests can simulate a full directory.
var diskFree = diskFreeBytes

// errNoCloneSpace is returned when no sanctioned temp directory has room for
// a new clone. Clones are never placed anywhere else.
var errNoCloneSpace = errors.New("no temp directory has room for a new clone")

// cloneRoots returns the sanctioned directories clones may live in, in the
// order new clones try them. An empty configuration means the system temp
// directory, which is implied rather than stored.
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

// resolvePath returns path made absolute and clean with every symlink in it
// resolved, so a directory reached through different paths (on macOS /var is
// a symlink to /private/var) is always named the same way (issue #752). A
// path that does not exist yet keeps its missing tail below the deepest
// ancestor that does, which is resolved.
func resolvePath(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = filepath.Clean(path)
	var tail []string
	for dir := path; ; {
		if resolved, err := filepath.EvalSymlinks(dir); err == nil {
			for i := len(tail) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, tail[i])
			}
			return resolved
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path
		}
		tail = append(tail, filepath.Base(dir))
		dir = parent
	}
}

// underRoots reports whether path lies inside one of roots, with symlinks in
// both resolved.
func underRoots(path string, roots []string) bool {
	path = resolvePath(path)
	for _, root := range roots {
		rel, err := filepath.Rel(resolvePath(root), path)
		if err == nil && isStrictlyInside(rel) {
			return true
		}
	}
	return false
}

// isStrictlyInside reports whether rel, a path relative to some root, names
// something below that root rather than the root itself or a path outside it.
func isStrictlyInside(rel string) bool {
	escapes := rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator))
	return rel != "." && !escapes && !filepath.IsAbs(rel)
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

// cloneRecord is the bookkeeping for one clone aviary created or adopted.
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
// the list outlives the jobs and survives restarts.
type cloneRegistry struct {
	mu      sync.Mutex
	path    string
	records map[string]cloneRecord
}

// clonesPathFor names the registry file kept beside statePath: a state file
// .aviary.json keeps its clones in .aviary.clones.json. An empty state path
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
	list, err := store.ReadJSON[[]cloneRecord](path)
	if errors.Is(err, fs.ErrNotExist) {
		return reg, nil
	}
	if err != nil {
		return nil, err
	}
	// Records saved before paths were resolved can name one directory
	// twice; the most recently created record is the directory's current
	// job, and the older one is a stale record of an earlier job (issue
	// #752).
	deduplicated := false
	for _, rec := range list {
		if rec.Path == "" {
			continue
		}
		key := cloneKey(rec.Path)
		if resolved := resolvePath(rec.Path); resolved != rec.Path {
			rec.Path, deduplicated = resolved, true
		}
		if existing, ok := reg.records[key]; ok {
			deduplicated = true
			if existing.Created.After(rec.Created) {
				continue
			}
		}
		reg.records[key] = rec
	}
	if deduplicated {
		if err := reg.saveLocked(); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

// cloneKey identifies the directory path names, however it is reached.
func cloneKey(path string) string {
	key := resolvePath(path)
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
	return store.WriteJSON(r.path, r.listLocked())
}

func (r *cloneRegistry) listLocked() []cloneRecord {
	list := make([]cloneRecord, 0, len(r.records))
	for _, rec := range r.records {
		list = append(list, rec)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Path < list[j].Path })
	return list
}

// record adds or updates a clone, keeping fields the update leaves empty. A
// clone recorded for a different job is replaced rather than merged.
func (r *cloneRegistry) record(update cloneRecord) error {
	if r == nil || update.Path == "" {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := cloneKey(update.Path)
	rec, ok := r.records[key]
	if ok && update.Job != "" && rec.Job != "" && update.Job != rec.Job {
		// The directory was reused for another job, so nothing recorded
		// about the earlier job still describes it.
		ok = false
	}
	if !ok {
		rec = cloneRecord{Path: resolvePath(update.Path), Created: update.Created, Adopted: update.Adopted}
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
// such as aviary-gh-fix-830.
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
		// A recorded clone keeps the job it was last handed to; a reused
		// clone's earlier jobs and its directory name name older ones.
		if _, known := r.get(state.CheckoutDirectory); known {
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
			if _, known := r.get(path); known || !isDir(filepath.Join(path, ".git")) {
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
	defer file.Close() //nolint:errcheck
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
	// leased maps the clones handed to a job that has not recorded them in
	// its work state yet to that job's key.
	leased map[string]string
}

// inUseStatuses are the work statuses whose job may still need its clone. A
// paused job resumes from its checkout, so it is in use by definition.
var inUseStatuses = map[string]bool{"active": true, "waiting": true, "queued": true, "paused": true, "stopping": true}

// cloneVerdict is what classifyClone decides about a clone.
type cloneVerdict int

const (
	// cloneUnused is a clone no live job of this instance holds; clearing
	// removes it.
	cloneUnused cloneVerdict = iota
	// cloneInUse is a clone a live job of this instance holds.
	cloneInUse
	// cloneKept is a clone nothing holds that clearing still leaves alone,
	// such as one outside the configured temp directories.
	cloneKept
)

// errWorkStateUnreadable reports that the work state could not be read, so
// no clone can be classified. It is surfaced as an error rather than as
// every clone being in use.
var errWorkStateUnreadable = errors.New("work state could not be read")

// classifyClone decides whether a live job of this instance holds rec, and
// says why. Only a live agent run or a job in an in-use status counts as
// use; a clone no job here records is unused, not speculatively in use.
func classifyClone(rec cloneRecord, usage cloneUsage) (cloneVerdict, string) {
	if job, ok := usage.leased[cloneKey(rec.Path)]; ok {
		return cloneInUse, "being handed to job " + job
	}
	for _, run := range usage.live {
		if run.Checkout != "" && (cloneKey(run.Checkout) == cloneKey(rec.Path) || underRoots(run.Checkout, []string{rec.Path})) {
			return cloneInUse, fmt.Sprintf("an agent is working in it for #%d", run.Number)
		}
		if run.Checkout == "" && rec.Issue != 0 && run.Number == rec.Issue {
			return cloneInUse, fmt.Sprintf("an agent for #%d has not announced its checkout yet", run.Number)
		}
	}
	if job, ok := usage.jobs[rec.Job]; ok && inUseStatuses[job.Status] {
		return cloneInUse, "job is " + job.Status
	}
	state, known := usage.work[rec.Job]
	if known && inUseStatuses[state.Status] {
		return cloneInUse, "job is " + state.Status
	}
	for key, other := range usage.work {
		if other.CheckoutDirectory != "" && cloneKey(other.CheckoutDirectory) == cloneKey(rec.Path) && inUseStatuses[other.Status] {
			return cloneInUse, fmt.Sprintf("job %s is %s", key, other.Status)
		}
	}
	if !underRoots(rec.Path, usage.roots) {
		return cloneKept, "outside the configured temp directories; remove it by hand"
	}
	if _, err := os.Stat(rec.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return cloneKept, err.Error()
	}
	if known && state.Status != "" {
		return cloneUnused, "job is " + state.Status
	}
	if rec.Adopted {
		return cloneUnused, "not owned by this instance: no job here records it"
	}
	return cloneUnused, "its job is no longer tracked"
}

// cloneEntry presents rec with its verdict.
func cloneEntry(rec cloneRecord, verdict cloneVerdict, reason string) core.CloneEntry {
	return core.CloneEntry{
		Path: rec.Path, Repository: rec.Repository, Issue: rec.Issue,
		PullRequest: rec.PullRequest, PullRequestRepository: rec.PullRequestRepository,
		Job: rec.Job, InUse: verdict == cloneInUse, Kept: verdict == cloneKept, Reason: reason,
	}
}

// listClones enumerates every recorded clone with its in-use decision,
// filling in pull request numbers the published jobs know. It fails when the
// work state could not be read, since no clone can be classified without it.
func listClones(reg *cloneRegistry, usage cloneUsage) (core.ClonesSnapshot, error) {
	if usage.workErr != nil {
		return core.ClonesSnapshot{}, fmt.Errorf("%w: %v", errWorkStateUnreadable, usage.workErr)
	}
	snapshot := core.ClonesSnapshot{Roots: usage.roots, Clones: []core.CloneEntry{}}
	for _, rec := range reg.list() {
		if job, ok := usage.jobs[rec.Job]; ok && job.PullRequest != 0 {
			rec.PullRequest, rec.PullRequestRepository = job.PullRequest, job.PullRequestRepository
			_ = reg.record(cloneRecord{Path: rec.Path, PullRequest: job.PullRequest, PullRequestRepository: job.PullRequestRepository})
		}
		if !isDir(rec.Path) {
			if _, err := os.Stat(rec.Path); errors.Is(err, fs.ErrNotExist) {
				// Removed outside aviary; nothing is left to list or clear.
				_ = reg.remove(rec.Path)
				continue
			}
		}
		verdict, reason := classifyClone(rec, usage)
		snapshot.Clones = append(snapshot.Clones, cloneEntry(rec, verdict, reason))
	}
	return snapshot, nil
}

// clearClones removes every unused clone and reports the space reclaimed.
// The in-use decision is made again for each clone right before removing it.
func clearClones(reg *cloneRegistry, usage func() cloneUsage) (core.ClearClonesResult, error) {
	snapshot, err := listClones(reg, usage())
	if err != nil {
		return core.ClearClonesResult{}, err
	}
	result := core.ClearClonesResult{Removed: []core.CloneEntry{}}
	for _, entry := range snapshot.Clones {
		if !entry.InUse && !entry.Kept {
			// Read again right before removing, so a job dispatched since
			// the list was made keeps its clone.
			current := usage()
			if current.workErr != nil {
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v: %v", entry.Path, errWorkStateUnreadable, current.workErr))
				result.Kept++
				continue
			}
			if rec, ok := reg.get(entry.Path); ok {
				verdict, reason := classifyClone(rec, current)
				entry = cloneEntry(rec, verdict, reason)
			}
		}
		if entry.InUse || entry.Kept {
			result.Kept++
			continue
		}
		size, _ := dirSize(context.Background(), entry.Path)
		if err := removeTree(entry.Path); err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", entry.Path, err))
			result.Kept++
			continue
		}
		_ = reg.remove(entry.Path)
		entry.Size = &size
		result.Reclaimed += size
		result.Removed = append(result.Removed, entry)
	}
	return result, nil
}

// dirSize totals the regular files under path. WalkDir never follows
// symlinks or Windows junctions, so nothing outside the clone is counted.
// It stops with ctx's error when ctx ends first.
func dirSize(ctx context.Context, path string) (int64, error) {
	var total int64
	err := filepath.WalkDir(path, func(_ string, d fs.DirEntry, err error) error {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
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
	return total, err
}

// cloneSizeTimeout bounds how long listing clones spends measuring them, and
// cloneSizeWorkers how many clones are measured at once.
const (
	cloneSizeTimeout = 3 * time.Second
	cloneSizeWorkers = 8
)

// measureCloneSizes fills in each clone's size and the snapshot's total,
// measuring concurrently. A clone not measured before timeout (or ctx) ends
// keeps a nil size and counts as unmeasured, so a slow disk or a huge clone
// never holds the listing up for longer than timeout.
func measureCloneSizes(ctx context.Context, snapshot *core.ClonesSnapshot, timeout time.Duration, measure func(context.Context, string) (int64, error)) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var (
		mu       sync.Mutex
		sizes    = make([]*int64, len(snapshot.Clones))
		finished bool
		wg       sync.WaitGroup
	)
	indexes := make(chan int)
	for range min(cloneSizeWorkers, len(snapshot.Clones)) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range indexes {
				size, err := measure(ctx, snapshot.Clones[i].Path)
				mu.Lock()
				if err == nil && !finished {
					sizes[i] = &size
				}
				mu.Unlock()
			}
		}()
	}
	go func() {
		defer close(indexes)
		for i := range snapshot.Clones {
			select {
			case indexes <- i:
			case <-ctx.Done():
				return
			}
		}
	}()
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	// Workers still measuring at the deadline are not waited for; finished
	// makes them drop their results.
	select {
	case <-done:
	case <-ctx.Done():
	}
	mu.Lock()
	finished = true
	mu.Unlock()
	snapshot.TotalSize, snapshot.Unmeasured = 0, 0
	for i := range snapshot.Clones {
		snapshot.Clones[i].Size = sizes[i]
		if sizes[i] == nil {
			snapshot.Unmeasured++
			continue
		}
		snapshot.TotalSize += *sizes[i]
	}
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
