package projects

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
	"github.com/lsegal/aviary/internal/store"
)

// The skills a job is queued for on demand (issue #1095). SkillFix names the
// gh-fix runs Code Loop dispatches from its own polls.
const (
	SkillFix       = "gh-fix"
	SkillSafeMerge = "gh-safe-merge"
	SkillDiscuss   = "gh-discuss"
)

// ErrNoMergeQueue refuses queueing a safe merge while the projects' no_merge
// setting asks for finished pull requests to be left for a human.
var ErrNoMergeQueue = errors.New("safe merge is turned off: the Code Loop no_merge setting leaves pull requests for a human to merge")

// errQueuedJobPaused is the cancel cause of a queued job the owner paused, so
// its run ends in the paused state and keeps its place in the queue.
var errQueuedJobPaused = errors.New("queued job paused by the owner")

// queuedJob is one merge or discuss job waiting in, or running from, the
// persisted job queue. The queue's order is the order jobs were queued in,
// which is the order each repository's merges run in.
type queuedJob struct {
	Skill      string    `json:"skill"`
	Target     string    `json:"target"`
	Repository string    `json:"repository"`
	Number     int       `json:"number"`
	Title      string    `json:"title,omitempty"`
	QueuedAt   time.Time `json:"queued_at"`
	// Paused holds the job in its place without running it.
	Paused bool `json:"paused,omitempty"`
}

func (q queuedJob) key() string { return q.Target + "#" + strconv.Itoa(q.Number) }

// kind is the board job kind of a queued skill.
func (q queuedJob) kind() string { return queuedKind(q.Skill) }

func queuedKind(skill string) string {
	if skill == SkillSafeMerge {
		return core.JobKindMerge
	}
	return core.JobKindDiscuss
}

func queuedSkill(kind string) string {
	if kind == core.JobKindMerge {
		return SkillSafeMerge
	}
	return SkillDiscuss
}

// jobQueue is the persisted list of queued jobs, saved beside the work state
// so queued and running jobs survive a restart.
type jobQueue struct {
	mu     sync.Mutex
	path   string
	loaded bool
	items  []queuedJob
}

func (q *jobQueue) loadLocked() {
	if q.loaded {
		return
	}
	q.loaded = true
	if q.path == "" {
		return
	}
	if saved, err := store.ReadJSON[[]queuedJob](q.path); err == nil {
		q.items = saved
	}
}

func (q *jobQueue) saveLocked() error {
	if q.path == "" {
		return nil
	}
	if len(q.items) == 0 {
		if err := os.Remove(q.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	return store.WriteJSON(q.path, q.items)
}

// list returns the queued jobs in queue order.
func (q *jobQueue) list() []queuedJob {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.loadLocked()
	return slices.Clone(q.items)
}

// add appends item to the end of the queue, replacing any earlier entry with
// its key.
func (q *jobQueue) add(item queuedJob) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.loadLocked()
	q.items = slices.DeleteFunc(q.items, func(existing queuedJob) bool { return existing.key() == item.key() })
	q.items = append(q.items, item)
	return q.saveLocked()
}

// remove drops the job keyed key from the queue.
func (q *jobQueue) remove(key string) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.loadLocked()
	before := len(q.items)
	q.items = slices.DeleteFunc(q.items, func(existing queuedJob) bool { return existing.key() == key })
	if len(q.items) == before {
		return nil
	}
	return q.saveLocked()
}

// setPaused holds or releases the job keyed key, keeping its place.
func (q *jobQueue) setPaused(key string, paused bool) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.loadLocked()
	for i := range q.items {
		if q.items[i].key() == key {
			q.items[i].Paused = paused
			return q.saveLocked()
		}
	}
	return nil
}

// jobQueue returns the engine's job queue, saved beside its work state.
func (w *Engine) jobQueue() *jobQueue {
	w.queueOnce.Do(func() {
		path := ""
		if w.StatePath != "" {
			path = filepath.Join(filepath.Dir(w.StatePath), "queue.json")
		}
		w.queue = &jobQueue{path: path}
	})
	return w.queue
}

// QueueRequest asks the engine to queue Numbers on Target's board for Skill,
// gh-safe-merge for pull requests or gh-discuss for Discussion threads, in
// that order. Repository is the OWNER/REPO they live in, needed only when
// Target is not a repository; Titles names them for their cards.
type QueueRequest struct {
	Target     string
	Repository string
	Skill      string
	Numbers    []int
	Titles     map[int]string
}

// QueueOutcome is what queueing one number did: Queued with its card's Key,
// or not, with OwnedBy naming the job that already owns it.
type QueueOutcome struct {
	Number   int    `json:"number"`
	Key      string `json:"key,omitempty"`
	Queued   bool   `json:"queued"`
	OwnedBy  string `json:"owned_by,omitempty"`
	Reason   string `json:"reason,omitempty"`
	Position int    `json:"position,omitempty"`
}

type queueRequest struct {
	request QueueRequest
	done    chan queueReply
}

type queueReply struct {
	outcomes []QueueOutcome
	err      error
}

// queueRequestChannel carries queue requests into the Run loop, as
// projectTaskChannel carries project task runs.
type queueRequestChannel struct {
	once sync.Once
	ch   chan queueRequest
}

func (w *Engine) queueRequestsChan() chan queueRequest {
	w.queueRequests.once.Do(func() { w.queueRequests.ch = make(chan queueRequest) })
	return w.queueRequests.ch
}

// QueueJobs queues request's numbers as merge or discuss jobs, skipping any a
// job already owns. Merges are refused while no_merge is set.
func (w *Engine) QueueJobs(ctx context.Context, request QueueRequest) ([]QueueOutcome, error) {
	request.Target = strings.TrimSpace(request.Target)
	switch request.Skill {
	case SkillSafeMerge:
		if w.noMerge.Load() {
			return nil, ErrNoMergeQueue
		}
	case SkillDiscuss:
	default:
		return nil, fmt.Errorf("skill %q cannot be queued; use %s or %s", request.Skill, SkillSafeMerge, SkillDiscuss)
	}
	targets := w.Targets
	if len(targets) == 0 && w.Repo != "" {
		targets = []string{w.Repo}
	}
	if !slices.Contains(targets, request.Target) {
		return nil, fmt.Errorf("project %q is not watched", request.Target)
	}
	if request.Repository == "" {
		request.Repository = issueRepository(request.Target, Issue{})
	}
	if !core.ValidRepo(request.Repository) {
		return nil, fmt.Errorf("name the repository of the numbers to queue on %s", request.Target)
	}
	if len(request.Numbers) == 0 {
		return nil, errors.New("name at least one number to queue")
	}
	for _, number := range request.Numbers {
		if number <= 0 {
			return nil, fmt.Errorf("%d is not a pull request or discussion number", number)
		}
	}
	if err := w.checkReady(ctx); err != nil {
		return nil, err
	}
	reply := make(chan queueReply, 1)
	select {
	case w.queueRequestsChan() <- queueRequest{request: request, done: reply}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case result := <-reply:
		return result.outcomes, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// queueLoop runs the queued jobs of one Run of the engine. Its serve, action,
// restore and dispatch methods run on the Run loop's goroutine, so every
// wg.Add happens there; each job's run is its own goroutine. Merges run one
// at a time per repository, in queue order, so each starts from the latest
// default branch; discuss jobs run as soon as a slot is free.
type queueLoop struct {
	w             *Engine
	ctx           context.Context
	sem           *concurrencySemaphore
	wg            *sync.WaitGroup
	tasks         *taskState
	jobMu         *sync.Mutex
	jobs          map[string]JobSnapshot
	workMu        *sync.Mutex
	cancellations map[string]context.CancelCauseFunc
	runExits      map[string]chan struct{}
	publish       func()
	workFinished  chan struct{}
	// wake asks the Run loop to dispatch the next merges.
	wake chan struct{}
	// laneMu guards lanes, the key of the merge each repository is running.
	laneMu sync.Mutex
	lanes  map[string]string
}

func (l *queueLoop) nudge() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// ownedStatuses are the statuses of a job that still owns its number.
var ownedStatuses = []string{"queued", "active", "waiting", "paused", "stopping"}

// owner names the in-flight job that already owns item: a job with its key,
// or for a merge, any job working on the same pull request.
func (l *queueLoop) owner(item queuedJob) string {
	l.jobMu.Lock()
	defer l.jobMu.Unlock()
	return queuedJobOwner(l.jobs, item)
}

func queuedJobOwner(jobs map[string]JobSnapshot, item queuedJob) string {
	for key, job := range jobs {
		if !slices.Contains(ownedStatuses, job.Status) {
			continue
		}
		if key == item.key() {
			return key
		}
		repository := job.Repository
		if repository == "" {
			repository = issueRepository(job.Target, Issue{})
		}
		if !strings.EqualFold(repository, item.Repository) {
			continue
		}
		switch item.kind() {
		case core.JobKindMerge:
			pullRepository := job.PullRequestRepository
			if pullRepository == "" {
				pullRepository = repository
			}
			if job.PullRequest == item.Number && strings.EqualFold(pullRepository, item.Repository) {
				return key
			}
			if job.Kind == core.JobKindMerge && job.Number == item.Number {
				return key
			}
		case core.JobKindDiscuss:
			if (job.Kind == core.JobKindDiscuss || job.Kind == core.JobKindDiscussion) && job.Number == item.Number {
				return key
			}
		}
	}
	return ""
}

// serve queues a request's numbers, in order.
func (l *queueLoop) serve(request queueRequest) {
	r := request.request
	var outcomes []QueueOutcome
	for _, number := range r.Numbers {
		item := queuedJob{Skill: r.Skill, Target: r.Target, Repository: r.Repository, Number: number, Title: r.Titles[number], QueuedAt: time.Now()}
		if owner := l.owner(item); owner != "" {
			outcomes = append(outcomes, QueueOutcome{Number: number, OwnedBy: owner, Reason: "already owned by job " + owner})
			continue
		}
		if err := l.w.jobQueue().add(item); err != nil {
			request.done <- queueReply{outcomes: outcomes, err: fmt.Errorf("saving the job queue: %w", err)}
			return
		}
		l.enqueue(item, true)
		l.w.logf("%s %s queued for %s", item.kind(), item.key(), item.Skill)
		l.w.jobSystem(item.key(), "queued for "+item.Skill)
		outcomes = append(outcomes, QueueOutcome{Number: number, Key: item.key(), Queued: true})
	}
	l.dispatch()
	l.jobMu.Lock()
	for i := range outcomes {
		if outcomes[i].Queued {
			outcomes[i].Position = l.jobs[outcomes[i].Key].QueuePosition
		}
	}
	l.jobMu.Unlock()
	request.done <- queueReply{outcomes: outcomes}
}

// enqueue shows item's card as queued, or paused when it is held, and starts
// a discuss job's run. fresh starts the card over, as a new or retried job.
func (l *queueLoop) enqueue(item queuedJob, fresh bool) {
	status := "queued"
	if item.Paused {
		status = "paused"
	}
	key := item.key()
	l.jobMu.Lock()
	job, exists := l.jobs[key]
	if fresh || !exists || job.Kind != item.kind() {
		started := item.QueuedAt
		if started.IsZero() {
			started = time.Now()
		}
		job = JobSnapshot{Kind: item.kind(), Number: item.Number, Target: item.Target, Repository: item.Repository, Title: item.Title, Started: started}
		if item.kind() == core.JobKindMerge {
			job.PullRequest = item.Number
		}
	}
	job.Status = status
	l.jobs[key] = job
	l.jobMu.Unlock()
	if status == "queued" {
		l.tasks.mu.Lock()
		l.tasks.queued++
		l.tasks.mu.Unlock()
		if item.kind() == core.JobKindDiscuss {
			l.start(item)
		}
	}
}

// restore puts the jobs the queue saved back on the board after a restart,
// in their places, and starts them again.
func (l *queueLoop) restore() {
	items := l.w.jobQueue().list()
	for _, item := range items {
		l.enqueue(item, true)
	}
	if len(items) > 0 {
		l.w.logf("restored %d queued merge and discuss job(s)", len(items))
	}
	l.dispatch()
}

// dispatch starts the next merge of every repository not running one, and
// numbers each queued merge's place in its repository's lane.
func (l *queueLoop) dispatch() {
	items := l.w.jobQueue().list()
	l.laneMu.Lock()
	var next []queuedJob
	for _, item := range items {
		if item.kind() != core.JobKindMerge || item.Paused {
			continue
		}
		repository := strings.ToLower(item.Repository)
		if _, busy := l.lanes[repository]; busy {
			continue
		}
		l.jobMu.Lock()
		queued := l.jobs[item.key()].Status == "queued"
		l.jobMu.Unlock()
		if !queued {
			continue
		}
		l.lanes[repository] = item.key()
		next = append(next, item)
	}
	running := make(map[string]bool, len(l.lanes))
	for _, key := range l.lanes {
		running[key] = true
	}
	l.laneMu.Unlock()
	// A lane's running merge is first in it, and its queued merges follow
	// in queue order.
	l.jobMu.Lock()
	positions := map[string]int{}
	for _, item := range items {
		key := item.key()
		job, ok := l.jobs[key]
		if !ok || item.kind() != core.JobKindMerge || !running[key] {
			continue
		}
		positions[strings.ToLower(item.Repository)] = 1
		job.QueuePosition = 1
		l.jobs[key] = job
	}
	for _, item := range items {
		key := item.key()
		job, ok := l.jobs[key]
		if !ok || item.kind() != core.JobKindMerge || running[key] {
			continue
		}
		job.QueuePosition = 0
		if job.Status == "queued" {
			repository := strings.ToLower(item.Repository)
			positions[repository]++
			job.QueuePosition = positions[repository]
		}
		l.jobs[key] = job
	}
	l.jobMu.Unlock()
	for _, item := range next {
		l.start(item)
	}
	l.publish()
}

// start runs item's agent once a slot is free.
func (l *queueLoop) start(item queuedJob) {
	key := item.key()
	runCtx, cancelRun := context.WithCancelCause(l.ctx)
	exited := make(chan struct{})
	l.workMu.Lock()
	l.cancellations[key] = cancelRun
	l.runExits[key] = exited
	l.workMu.Unlock()
	l.wg.Add(1)
	go func() {
		defer l.wg.Done()
		l.run(runCtx, item)
		cancelRun(nil)
		l.workMu.Lock()
		delete(l.cancellations, key)
		delete(l.runExits, key)
		l.workMu.Unlock()
		close(exited)
		if item.kind() == core.JobKindMerge {
			l.laneMu.Lock()
			if l.lanes[strings.ToLower(item.Repository)] == key {
				delete(l.lanes, strings.ToLower(item.Repository))
			}
			l.laneMu.Unlock()
			l.nudge()
		}
	}()
}

// run waits for a slot, runs item's skill as a board job, and records how it
// ended: complete or failed with its last line as the card's reason, paused in
// its place, or, when the owner stopped it before it started, off the board.
func (l *queueLoop) run(runCtx context.Context, item queuedJob) {
	w, key := l.w, item.key()
	acquired := l.sem.acquire(runCtx) == nil
	l.tasks.mu.Lock()
	l.tasks.queued--
	if acquired {
		l.tasks.running++
	}
	l.tasks.mu.Unlock()
	var runErr error
	if acquired {
		defer func() {
			l.sem.release()
			select {
			case l.workFinished <- struct{}{}:
			default:
			}
		}()
		session := AgentSession{Agent: w.newJobAgent(item.Target)}
		if agentAssignsSessionID(session.Agent) {
			session.ID, runErr = newSessionID()
		}
		spec, _ := parseAgentSpec(session.Agent)
		l.jobMu.Lock()
		job := l.jobs[key]
		job.Status, job.SessionID, job.Agent, job.Model, job.Effort = "active", session.ID, spec.Name, spec.Model, spec.Level
		job.Started, job.Finished = time.Now(), time.Time{}
		l.jobs[key] = job
		l.jobMu.Unlock()
		w.jobSystem(key, item.Skill+" started on "+session.Agent)
		l.publish()
		issue := Issue{Number: item.Number, Target: item.Target, Repository: item.Repository, Title: item.Title, Skill: item.Skill}
		intro := queuedInboxIntro(item)
		if queued := w.takeInbox(key); len(queued) > 0 {
			session.Update = inboxPrompt(intro, queued)
		}
		output := jobOutputWriter{write: func(text string) {
			l.jobMu.Lock()
			job := l.jobs[key]
			job.Log += text
			l.jobs[key] = job
			l.jobMu.Unlock()
			l.publish()
		}}
		updateSession := func(update AgentSession) {
			l.jobMu.Lock()
			job := l.jobs[key]
			if update.ID != "" {
				job.SessionID = update.ID
			}
			if update.CheckoutDirectory != "" {
				job.CheckoutDirectory = update.CheckoutDirectory
			}
			l.jobs[key] = job
			l.jobMu.Unlock()
			l.publish()
		}
		if runErr == nil {
			runErr = w.runRelayingInbox(runCtx, inboxRun{key: key, subject: fmt.Sprintf("%s %s", item.kind(), key), intro: intro, issue: issue, output: output, updateSession: updateSession}, session)
		}
	}
	cause := context.Cause(runCtx)
	if l.ctx.Err() != nil && !errors.Is(cause, errWorkStoppedFromWebUI) && !errors.Is(cause, errQueuedJobPaused) {
		// The engine is stopping; the job stays queued and runs again
		// when the engine is back.
		if acquired {
			l.tasks.mu.Lock()
			l.tasks.running--
			l.tasks.mu.Unlock()
		}
		return
	}
	l.jobMu.Lock()
	job := l.jobs[key]
	status := ""
	switch {
	case errors.Is(cause, errQueuedJobPaused):
		job.Status, job.QueuePosition = "paused", 0
		status = "paused"
	case errors.Is(cause, errWorkStoppedFromWebUI) && !acquired:
		delete(l.jobs, key)
		status = "removed"
	default:
		result := projectTaskOutcome(runErr, cause, job.Log)
		job.Status, job.Finished, job.Summary, job.QueuePosition = "complete", time.Now(), result.summary, 0
		if result.err != nil {
			job.Status = "failed"
		}
		job.Finish()
		status = job.Status
	}
	if status != "removed" {
		l.jobs[key] = job
	}
	l.jobMu.Unlock()
	if status == "paused" {
		if err := w.jobQueue().setPaused(key, true); err != nil {
			w.logf("%s %s: saving its pause: %v", item.kind(), key, err)
		}
	} else if err := w.jobQueue().remove(key); err != nil {
		w.logf("%s %s: removing it from the job queue: %v", item.kind(), key, err)
	}
	l.tasks.mu.Lock()
	if acquired {
		l.tasks.running--
		switch status {
		case "complete":
			l.tasks.completed++
		case "failed":
			l.tasks.failed++
		}
	}
	l.tasks.mu.Unlock()
	w.jobSystem(key, item.Skill+" run "+status)
	w.logf("%s %s %s", item.kind(), key, status)
	l.publish()
}

// queuedInboxIntro opens the prompt that relays output sent to a queued job.
func queuedInboxIntro(item queuedJob) string {
	return fmt.Sprintf("Output was sent to this %s job for #%d.", item.Skill, item.Number)
}

// action answers a dashboard action on a merge or discuss job. Pause holds it
// in its place, stopping a running agent; Stop takes a waiting job off the
// queue and stops a running one; Retry resumes a paused job in its place and
// queues a finished one again at the end.
func (l *queueLoop) action(request jobActionRequest, key string, job JobSnapshot) {
	verb := request.action.Action
	available := core.JobActionsFor(job)
	l.workMu.Lock()
	cancel, exited := l.cancellations[key], l.runExits[key]
	l.workMu.Unlock()
	reply := func(err error) { request.done <- err }
	// waitExit replies once a cancelled run has exited.
	waitExit := func() {
		go func() {
			select {
			case <-exited:
				reply(nil)
			case <-l.ctx.Done():
				reply(fmt.Errorf("job is still stopping: %w", l.ctx.Err()))
			}
		}()
	}
	switch verb {
	case "pause":
		if !available.Pause {
			reply(fmt.Errorf("job cannot be paused while %s", job.Status))
			return
		}
		if cancel != nil {
			l.w.logf("%s %s pause requested", job.Kind, key)
			if job.Status != "queued" {
				l.setStatus(key, "stopping")
			}
			cancel(errQueuedJobPaused)
			waitExit()
			return
		}
		if err := l.w.jobQueue().setPaused(key, true); err != nil {
			reply(err)
			return
		}
		l.leaveQueued(key, "paused")
		l.w.jobSystem(key, "paused by the owner")
		l.dispatch()
		reply(nil)
	case "stop":
		if !available.Stop {
			reply(fmt.Errorf("job cannot be stopped while %s", job.Status))
			return
		}
		if cancel != nil {
			l.w.logf("%s %s stop requested", job.Kind, key)
			l.w.jobSystem(key, "stopped by the owner")
			if job.Status != "queued" {
				l.setStatus(key, "stopping")
			}
			cancel(errWorkStoppedFromWebUI)
			waitExit()
			return
		}
		if err := l.w.jobQueue().remove(key); err != nil {
			reply(err)
			return
		}
		if job.Status == "queued" {
			l.leaveQueued(key, "")
		} else {
			l.jobMu.Lock()
			delete(l.jobs, key)
			l.jobMu.Unlock()
		}
		l.w.logf("%s %s removed from the queue", job.Kind, key)
		l.dispatch()
		reply(nil)
	case "retry":
		if !available.Retry {
			reply(fmt.Errorf("job cannot be retried: %s", available.RetryReason))
			return
		}
		if job.Status == "paused" {
			if err := l.w.jobQueue().setPaused(key, false); err != nil {
				reply(err)
				return
			}
			item := queuedJob{Skill: queuedSkill(job.Kind), Target: job.Target, Repository: job.Repository, Number: job.Number, Title: job.Title}
			l.enqueue(item, false)
			l.w.jobSystem(key, "resumed by the owner")
			l.dispatch()
			reply(nil)
			return
		}
		item := queuedJob{Skill: queuedSkill(job.Kind), Target: job.Target, Repository: job.Repository, Number: job.Number, Title: job.Title, QueuedAt: time.Now()}
		if item.Skill == SkillSafeMerge && l.w.noMerge.Load() {
			reply(ErrNoMergeQueue)
			return
		}
		if err := l.w.jobQueue().add(item); err != nil {
			reply(err)
			return
		}
		l.enqueue(item, true)
		l.w.jobSystem(key, "queued again for "+item.Skill)
		l.dispatch()
		reply(nil)
	default:
		reply(fmt.Errorf("%s jobs cannot be %s", job.Kind, verb))
	}
}

// setStatus records status on the job keyed key and publishes it.
func (l *queueLoop) setStatus(key, status string) {
	l.jobMu.Lock()
	job := l.jobs[key]
	job.Status = status
	l.jobs[key] = job
	l.jobMu.Unlock()
	l.publish()
}

// leaveQueued moves a queued job with no run waiting for it to status, or off
// the board when status is empty.
func (l *queueLoop) leaveQueued(key, status string) {
	l.tasks.mu.Lock()
	l.tasks.queued--
	l.tasks.mu.Unlock()
	l.jobMu.Lock()
	if status == "" {
		delete(l.jobs, key)
	} else {
		job := l.jobs[key]
		job.Status, job.QueuePosition = status, 0
		l.jobs[key] = job
	}
	l.jobMu.Unlock()
}
