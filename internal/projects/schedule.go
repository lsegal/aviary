package projects

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lsegal/aviary/internal/projects/core"
)

// schedulesPathFor names the schedule store kept beside statePath: a state file
// .glorp.json keeps its schedules in .glorp.schedules.json (issue #848). An
// empty state path keeps schedules in memory only.
func schedulesPathFor(statePath string) string {
	if statePath == "" {
		return ""
	}
	ext := filepath.Ext(statePath)
	return strings.TrimSuffix(statePath, ext) + ".schedules.json"
}

// scheduleFile is the schedule store's JSON layout.
type scheduleFile struct {
	Schedules []core.Schedule `json:"schedules"`
}

// loadSchedules reads the schedule store. A missing file holds no schedules.
func loadSchedules(path string) ([]core.Schedule, error) {
	if path == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return nil, nil
	}
	var file scheduleFile
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("read schedules %s: %w", path, err)
	}
	for i := range file.Schedules {
		file.Schedules[i].Normalize()
	}
	return file.Schedules, nil
}

// saveSchedules writes the schedule store through a temporary file, so a
// reader never sees it half written.
func saveSchedules(path string, schedules []core.Schedule) error {
	if path == "" {
		return nil
	}
	if schedules == nil {
		schedules = []core.Schedule{}
	}
	data, err := json.MarshalIndent(scheduleFile{Schedules: schedules}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	temp := path + ".tmp"
	if err := os.WriteFile(temp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

var scheduleSlugPattern = regexp.MustCompile(`[^a-z0-9]+`)

// newScheduleID makes an ID from name, with a random suffix so two schedules
// with one name stay distinct.
func newScheduleID(name string) string {
	slug := strings.Trim(scheduleSlugPattern.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(slug) > 32 {
		slug = strings.Trim(slug[:32], "-")
	}
	if slug == "" {
		slug = "schedule"
	}
	var suffix [3]byte
	_, _ = rand.Read(suffix[:])
	return slug + "-" + hex.EncodeToString(suffix[:])
}

// scheduleMissedGrace is how late a fire may be and still count as on time.
// A fire later than this was missed while the machine slept or glorp was
// stopped, and the schedule's missed-run policy decides what happens to it.
const scheduleMissedGrace = 2 * time.Minute

// schedulerMaxSleep caps how long the scheduler sleeps between checks, so a
// clock change or a sleep the wake detector did not catch delays a fire by at
// most this long.
const schedulerMaxSleep = time.Minute

// upcomingPerSchedule is how many of one schedule's fires the upcoming runs
// list at most.
const upcomingPerSchedule = 3

// scheduleSummaryLimit bounds the summary a run's history entry keeps.
const scheduleSummaryLimit = 200

// scheduleRequest carries one schedule operation into the scheduler's loop.
type scheduleRequest struct {
	request core.ScheduleRequest
	done    chan scheduleReply
}

type scheduleReply struct {
	response core.ScheduleResponse
	err      error
}

// scheduleFinish reports a scheduled run's end to the scheduler.
type scheduleFinish struct {
	id      string
	fired   time.Time
	status  string
	summary string
	elapsed time.Duration
}

// scheduler owns the schedules and decides when each fires (issue #848). All
// of its state belongs to its own loop goroutine; everything else reaches it
// through requests and finished.
type scheduler struct {
	path string
	now  func() time.Time
	logf func(string, ...interface{})
	// dispatch starts a run of schedule fired at fired. It must not block on
	// a concurrency slot; the run waits for one on its own goroutine.
	dispatch func(schedule core.Schedule, fired time.Time)
	// publish reports the upcoming runs whenever they change.
	publish func([]core.UpcomingRun)
	// wakes reports the machine resuming from sleep; nil uses watchForWake.
	wakes <-chan time.Duration

	requests chan scheduleRequest
	finished chan scheduleFinish

	schedules  []core.Schedule
	next       map[string]time.Time
	running    map[string]int
	queued     map[string]bool
	lastFired  map[string]time.Time
	fileLoaded string
}

func newScheduler(path string) *scheduler {
	return &scheduler{
		path:      path,
		now:       time.Now,
		logf:      func(string, ...interface{}) {},
		dispatch:  func(core.Schedule, time.Time) {},
		publish:   func([]core.UpcomingRun) {},
		requests:  make(chan scheduleRequest),
		finished:  make(chan scheduleFinish, 16),
		next:      map[string]time.Time{},
		running:   map[string]int{},
		queued:    map[string]bool{},
		lastFired: map[string]time.Time{},
	}
}

// load reads the store at startup. A run still marked running was cut off by
// the restart, so its history says so, and fires missed while glorp was
// stopped come due at once for the missed-run policy to handle.
func (s *scheduler) load() error {
	schedules, err := loadSchedules(s.path)
	if err != nil {
		return err
	}
	now := s.now()
	for i := range schedules {
		for j := range schedules[i].History {
			if schedules[i].History[j].Status == core.ScheduleRunRunning {
				schedules[i].History[j].Status = core.ScheduleRunFailed
				schedules[i].History[j].Summary = "interrupted when glorp stopped"
			}
		}
		if schedules[i].Created.IsZero() {
			schedules[i].Created = now
		}
	}
	s.schedules = schedules
	for _, schedule := range s.schedules {
		s.next[schedule.ID] = s.firstFire(schedule, now)
	}
	s.fileLoaded = stateFileFingerprint(s.path)
	return nil
}

// firstFire is when a loaded schedule next fires, counting from its last
// accounted fire so fires missed while glorp was stopped come due at once.
func (s *scheduler) firstFire(schedule core.Schedule, now time.Time) time.Time {
	if !schedule.Enabled {
		return time.Time{}
	}
	from := schedule.LastFire
	if from.IsZero() {
		from = schedule.Created
	}
	if from.IsZero() || from.After(now) {
		from = now
	}
	next, _ := schedule.Next(from)
	return next
}

func (s *scheduler) index(id string) int {
	return slices.IndexFunc(s.schedules, func(schedule core.Schedule) bool { return schedule.ID == id })
}

// loop runs the scheduler until ctx ends.
func (s *scheduler) loop(ctx context.Context) {
	wakes := s.wakes
	if wakes == nil {
		wakes = watchForWake(ctx)
	}
	changes := watchStateFile(ctx, s.path)
	s.fireDue()
	s.runRequested()
	s.publishUpcoming()
	for {
		timer := time.NewTimer(s.sleep())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			s.fireDue()
		case slept := <-wakes:
			timer.Stop()
			s.logf("schedules: woke after %s asleep; checking for missed runs", formatInterval(slept.Round(time.Second)))
			s.fireDue()
		case <-changes:
			timer.Stop()
			s.reload()
		case request := <-s.requests:
			timer.Stop()
			response, err := s.handle(request.request)
			request.done <- scheduleReply{response: response, err: err}
		case finish := <-s.finished:
			timer.Stop()
			s.finish(finish)
		}
		s.publishUpcoming()
	}
}

// sleep is how long until the earliest fire, capped at schedulerMaxSleep.
func (s *scheduler) sleep() time.Duration {
	wait := schedulerMaxSleep
	now := s.now()
	for _, schedule := range s.schedules {
		next := s.next[schedule.ID]
		if !schedule.Enabled || next.IsZero() {
			continue
		}
		if until := next.Sub(now); until < wait {
			wait = until
		}
	}
	if wait < 0 {
		wait = 0
	}
	return wait
}

// fireDue fires every enabled schedule whose next fire has come. A fire later
// than scheduleMissedGrace was missed, and its missed-run policy decides: skip
// records it and starts nothing, runOnce starts exactly one run however many
// fires were missed.
func (s *scheduler) fireDue() {
	now := s.now()
	changed := false
	for i := range s.schedules {
		schedule := &s.schedules[i]
		next := s.next[schedule.ID]
		if !schedule.Enabled || next.IsZero() || next.After(now) {
			continue
		}
		changed = true
		if late := now.Sub(next); late > scheduleMissedGrace {
			missed := 0
			for at, ok := next, true; ok && !at.After(now) && missed < 1000; at, ok = schedule.Next(at) {
				missed++
			}
			if schedule.Missed == core.MissedRunOnce {
				s.logf("schedule %s missed %d run(s); running once now", schedule.ID, missed)
				s.fire(schedule, now, false)
			} else {
				s.logf("schedule %s missed %d run(s); skipping them", schedule.ID, missed)
				schedule.AddRun(core.ScheduleRun{Fired: now, Status: core.ScheduleRunSkipped, Summary: fmt.Sprintf("missed %d run(s) while glorp was stopped or asleep", missed)})
			}
		} else {
			s.fire(schedule, next, false)
		}
		schedule.LastFire = now
		s.next[schedule.ID], _ = schedule.Next(now)
	}
	if changed {
		s.save()
	}
}

// fire starts a run of schedule, or applies its overlap policy when its
// previous run is still active. A manual run starts whatever the policy says.
func (s *scheduler) fire(schedule *core.Schedule, fired time.Time, manual bool) {
	if s.running[schedule.ID] > 0 && !manual {
		if schedule.Overlap == core.OverlapQueue {
			s.logf("schedule %s fired while its previous run is active; queued", schedule.ID)
			s.queued[schedule.ID] = true
			return
		}
		s.logf("schedule %s fired while its previous run is active; skipped", schedule.ID)
		schedule.AddRun(core.ScheduleRun{Fired: fired, Status: core.ScheduleRunSkipped, Summary: "previous run still active"})
		return
	}
	// Every run's job is keyed by its fire time in seconds, so two runs of one
	// schedule never share a second.
	fired = fired.Truncate(time.Second)
	if last := s.lastFired[schedule.ID]; !fired.After(last) {
		fired = last.Add(time.Second)
	}
	s.lastFired[schedule.ID] = fired
	s.running[schedule.ID]++
	schedule.AddRun(core.ScheduleRun{Fired: fired, Status: core.ScheduleRunRunning})
	s.dispatch(*schedule, fired)
}

// finish records a run's end in its history and starts a queued fire.
func (s *scheduler) finish(finish scheduleFinish) {
	if s.running[finish.id] > 0 {
		s.running[finish.id]--
	}
	index := s.index(finish.id)
	if index < 0 {
		delete(s.queued, finish.id)
		return
	}
	schedule := &s.schedules[index]
	run := core.ScheduleRun{Fired: finish.fired, Status: finish.status, Summary: truncateSummary(finish.summary), Duration: finish.elapsed.Round(time.Second).Seconds()}
	recorded := false
	for i := len(schedule.History) - 1; i >= 0; i-- {
		if schedule.History[i].Fired.Equal(finish.fired) {
			schedule.History[i] = run
			recorded = true
			break
		}
	}
	if !recorded {
		schedule.AddRun(run)
	}
	if s.queued[finish.id] && s.running[finish.id] == 0 {
		delete(s.queued, finish.id)
		if schedule.Enabled {
			s.logf("schedule %s running the fire queued behind its previous run", schedule.ID)
			s.fire(schedule, s.now(), false)
		}
	}
	s.save()
}

// runRequested starts a run of every schedule the store file asks to run now,
// which is how `glorp schedule run` reaches a watching instance.
func (s *scheduler) runRequested() {
	changed := false
	for i := range s.schedules {
		if !s.schedules[i].RunNow {
			continue
		}
		s.schedules[i].RunNow = false
		changed = true
		s.logf("schedule %s run requested", s.schedules[i].ID)
		s.fire(&s.schedules[i], s.now(), true)
	}
	if changed {
		s.save()
	}
}

// reload picks up a hand or CLI edit of the store file. Definitions come from
// the file; a schedule whose trigger or enabled flag did not change keeps its
// next fire, and the newer of the file's and the scheduler's run history wins.
func (s *scheduler) reload() {
	fingerprint := stateFileFingerprint(s.path)
	if fingerprint == s.fileLoaded {
		return
	}
	loaded, err := loadSchedules(s.path)
	if err != nil {
		s.logf("schedules: ignoring the edited store: %v", err)
		return
	}
	s.fileLoaded = fingerprint
	now := s.now()
	next := make(map[string]time.Time, len(loaded))
	kept := loaded[:0]
	for _, schedule := range loaded {
		if err := schedule.Validate(); err != nil {
			s.logf("schedules: ignoring %q from the edited store: %v", schedule.ID, err)
			continue
		}
		if schedule.Created.IsZero() {
			schedule.Created = now
		}
		if index := s.index(schedule.ID); index >= 0 {
			previous := s.schedules[index]
			if previous.LastFire.After(schedule.LastFire) {
				schedule.LastFire = previous.LastFire
			}
			if historyNewer(previous.History, schedule.History) {
				schedule.History = previous.History
			}
			if previous.Enabled == schedule.Enabled && triggersEqual(previous.Trigger, schedule.Trigger) {
				next[schedule.ID] = s.next[schedule.ID]
			}
		}
		if _, ok := next[schedule.ID]; !ok && schedule.Enabled {
			next[schedule.ID], _ = schedule.Next(now)
		}
		kept = append(kept, schedule)
	}
	s.schedules = kept
	s.next = next
	s.logf("schedules: reloaded %d schedule(s) from %s", len(s.schedules), s.path)
	s.runRequested()
}

func historyNewer(a, b []core.ScheduleRun) bool {
	if len(a) == 0 {
		return false
	}
	if len(b) == 0 {
		return true
	}
	last, other := a[len(a)-1], b[len(b)-1]
	return last.Fired.After(other.Fired) || last.Fired.Equal(other.Fired) && last.Status != other.Status && other.Status == core.ScheduleRunRunning
}

func triggersEqual(a, b core.ScheduleTrigger) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func (s *scheduler) save() {
	if err := saveSchedules(s.path, s.schedules); err != nil {
		s.logf("schedules: failed to save %s: %v", s.path, err)
		return
	}
	s.fileLoaded = stateFileFingerprint(s.path)
}

// handle answers one schedule request.
func (s *scheduler) handle(request core.ScheduleRequest) (core.ScheduleResponse, error) {
	now := s.now()
	switch request.Op {
	case core.ScheduleOpList:
		return core.ScheduleResponse{Schedules: s.views()}, nil
	case core.ScheduleOpPreview:
		if err := request.Trigger.Validate(); err != nil {
			return core.ScheduleResponse{}, err
		}
		return core.ScheduleResponse{Fires: request.Trigger.NextFires(now, now, 3)}, nil
	case core.ScheduleOpCreate:
		schedule := request.Schedule
		schedule.History, schedule.LastFire, schedule.RunNow = nil, time.Time{}, false
		if strings.TrimSpace(schedule.ID) == "" {
			schedule.ID = newScheduleID(schedule.Name)
		}
		schedule.Created = now
		schedule.Normalize()
		if err := schedule.Validate(); err != nil {
			return core.ScheduleResponse{}, err
		}
		if s.index(schedule.ID) >= 0 {
			return core.ScheduleResponse{}, fmt.Errorf("schedule %q already exists", schedule.ID)
		}
		s.schedules = append(s.schedules, schedule)
		s.next[schedule.ID] = time.Time{}
		if schedule.Enabled {
			s.next[schedule.ID], _ = schedule.Next(now)
		}
		s.save()
		s.logf("schedule %s created (%s, %s)", schedule.ID, schedule.Repository, schedule.Trigger.Describe())
		return s.viewResponse(schedule.ID), nil
	}
	index := s.index(request.ID)
	if index < 0 {
		return core.ScheduleResponse{}, fmt.Errorf("schedule %q not found", request.ID)
	}
	switch request.Op {
	case core.ScheduleOpUpdate:
		previous := s.schedules[index]
		updated := request.Update.Apply(previous)
		updated.Normalize()
		if err := updated.Validate(); err != nil {
			return core.ScheduleResponse{}, err
		}
		s.schedules[index] = updated
		if updated.Enabled != previous.Enabled || !triggersEqual(updated.Trigger, previous.Trigger) {
			s.next[updated.ID] = time.Time{}
			if updated.Enabled {
				s.next[updated.ID], _ = updated.Next(now)
			}
		}
		if !updated.Enabled {
			delete(s.queued, updated.ID)
		}
		s.save()
		s.logf("schedule %s updated", updated.ID)
		return s.viewResponse(updated.ID), nil
	case core.ScheduleOpDelete:
		id := s.schedules[index].ID
		s.schedules = slices.Delete(s.schedules, index, index+1)
		delete(s.next, id)
		delete(s.queued, id)
		s.save()
		s.logf("schedule %s deleted", id)
		return core.ScheduleResponse{}, nil
	case core.ScheduleOpRun:
		s.logf("schedule %s run requested", request.ID)
		s.fire(&s.schedules[index], now, true)
		s.save()
		return s.viewResponse(request.ID), nil
	}
	return core.ScheduleResponse{}, fmt.Errorf("unknown schedule operation %q", request.Op)
}

func (s *scheduler) view(schedule core.Schedule) core.ScheduleView {
	view := core.ScheduleView{Schedule: schedule, Running: s.running[schedule.ID] > 0}
	if schedule.Enabled {
		view.Next = s.next[schedule.ID]
	}
	return view
}

func (s *scheduler) views() []core.ScheduleView {
	views := make([]core.ScheduleView, 0, len(s.schedules))
	for _, schedule := range s.schedules {
		views = append(views, s.view(schedule))
	}
	return views
}

func (s *scheduler) viewResponse(id string) core.ScheduleResponse {
	index := s.index(id)
	if index < 0 {
		return core.ScheduleResponse{}
	}
	view := s.view(s.schedules[index])
	return core.ScheduleResponse{Schedule: &view}
}

// upcoming lists the next few runs of the enabled schedules, soonest first.
func (s *scheduler) upcoming() []core.UpcomingRun {
	var runs []core.UpcomingRun
	for _, schedule := range s.schedules {
		next := s.next[schedule.ID]
		if !schedule.Enabled || next.IsZero() {
			continue
		}
		// A few fires per schedule, so one frequent schedule does not hide
		// the next run of every other.
		fires := append([]time.Time{next}, schedule.Trigger.NextFires(next, schedule.Created, upcomingPerSchedule-1)...)
		for _, at := range fires {
			runs = append(runs, core.UpcomingRun{ScheduleID: schedule.ID, Name: schedule.Name, Repository: schedule.Repository, At: at})
		}
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].At.Before(runs[j].At) })
	if len(runs) > core.MaxUpcomingRuns {
		runs = runs[:core.MaxUpcomingRuns]
	}
	return runs
}

func (s *scheduler) publishUpcoming() {
	s.publish(s.upcoming())
}

func truncateSummary(summary string) string {
	summary = strings.TrimSpace(summary)
	if len(summary) <= scheduleSummaryLimit {
		return summary
	}
	return strings.TrimSpace(summary[:scheduleSummaryLimit-3]) + "..."
}

// scheduleRequestsChan lazily creates the channel schedule requests reach the
// scheduler through.
func (w *Glorp) scheduleRequestsChan() chan scheduleRequest {
	w.scheduleOnce.Do(func() { w.scheduleRequests = make(chan scheduleRequest) })
	return w.scheduleRequests
}

// HandleSchedule performs one schedule request on the running watch. Like a
// job action it fails with core.ErrNotReady while the run is still starting.
func (w *Glorp) HandleSchedule(ctx context.Context, request core.ScheduleRequest) (core.ScheduleResponse, error) {
	if err := w.checkReady(ctx); err != nil {
		return core.ScheduleResponse{}, err
	}
	done := make(chan scheduleReply, 1)
	select {
	case w.scheduleRequestsChan() <- scheduleRequest{request: request, done: done}:
	case <-ctx.Done():
		return core.ScheduleResponse{}, ctx.Err()
	}
	select {
	case reply := <-done:
		return reply.response, reply.err
	case <-ctx.Done():
		return core.ScheduleResponse{}, ctx.Err()
	}
}

// scheduleRuns tracks the scheduled runs in flight, so a dashboard can stop
// one and retry another (issue #848).
type scheduleRuns struct {
	mu      sync.Mutex
	cancels map[string]context.CancelCauseFunc
	exits   map[string]chan struct{}
	retry   map[string]bool
}

func newScheduleRuns() *scheduleRuns {
	return &scheduleRuns{cancels: map[string]context.CancelCauseFunc{}, exits: map[string]chan struct{}{}, retry: map[string]bool{}}
}

// scheduleJobKey is the jobs map key of a scheduled run.
func scheduleJobKey(id string, fired time.Time) string {
	return fmt.Sprintf("%s#%d", core.ScheduleJobTarget(id), fired.Unix())
}

// scheduleJobEnv is the part of Run's state a scheduled run reads and writes.
type scheduleJobEnv struct {
	ctx      context.Context
	sem      *concurrencySemaphore
	wg       *sync.WaitGroup
	tasks    *taskState
	jobMu    *sync.Mutex
	jobs     map[string]JobSnapshot
	publish  func()
	runs     *scheduleRuns
	finished chan<- scheduleFinish
	// rerun asks the scheduler to run the schedule again, for a retry.
	rerun func(id string)
}

// startScheduledRun queues a run of schedule as a job card and starts it once
// a concurrency slot is free. The agent gets the schedule's prompt instead of
// a gh-fix invocation, and the job finishes when the agent exits, since there
// is no issue to close.
func (w *Glorp) startScheduledRun(env scheduleJobEnv, schedule core.Schedule, fired time.Time) {
	key := scheduleJobKey(schedule.ID, fired)
	target := core.ScheduleJobTarget(schedule.ID)
	session := AgentSession{Agent: schedule.Agent}
	if session.Agent == "" {
		if identified, ok := w.runner().(AgentIdentifier); ok {
			session.Agent = identified.AgentName()
		}
	}
	if agentAssignsSessionID(session.Agent) {
		session.ID, _ = newSessionID()
	}
	spec, _ := parseAgentSpec(session.Agent)
	runCtx, cancel := context.WithCancelCause(env.ctx)
	exited := make(chan struct{})
	env.runs.mu.Lock()
	env.runs.cancels[key] = cancel
	env.runs.exits[key] = exited
	env.runs.mu.Unlock()
	env.jobMu.Lock()
	// Only the latest finished card of a schedule is kept, so a schedule that
	// fires every hour does not fill the dashboard with old runs.
	for existing, job := range env.jobs {
		if job.ScheduleID == schedule.ID && (job.Status == "complete" || job.Status == "failed") {
			delete(env.jobs, existing)
		}
	}
	env.jobs[key] = JobSnapshot{Number: int(fired.Unix()), Target: target, Repository: schedule.Repository, Title: schedule.Name, Status: "queued", SessionID: session.ID, Agent: spec.Name, Model: spec.Model, Effort: spec.Level, Started: time.Now(), Kind: core.JobKindSchedule, ScheduleID: schedule.ID}
	env.jobMu.Unlock()
	env.tasks.mu.Lock()
	env.tasks.queued++
	env.tasks.mu.Unlock()
	w.logf("schedule %s queued a run on %s", schedule.ID, schedule.Repository)
	env.publish()
	env.wg.Add(1)
	go func() {
		defer env.wg.Done()
		started := time.Now()
		status, summary := core.ScheduleRunFailed, ""
		defer func() {
			cancel(nil)
			env.runs.mu.Lock()
			delete(env.runs.cancels, key)
			delete(env.runs.exits, key)
			retry := env.runs.retry[key]
			delete(env.runs.retry, key)
			env.runs.mu.Unlock()
			close(exited)
			select {
			case env.finished <- scheduleFinish{id: schedule.ID, fired: fired, status: status, summary: summary, elapsed: time.Since(started)}:
			case <-env.ctx.Done():
			}
			if retry && env.ctx.Err() == nil {
				env.rerun(schedule.ID)
			}
		}()
		setStatus := func(status, log string) {
			env.jobMu.Lock()
			job := env.jobs[key]
			job.Status = status
			job.Log += log
			if status == "complete" {
				job.Finish()
			}
			env.jobs[key] = job
			env.jobMu.Unlock()
			env.publish()
		}
		if err := env.sem.acquire(runCtx); err != nil {
			env.tasks.mu.Lock()
			env.tasks.queued--
			env.tasks.mu.Unlock()
			summary = "stopped before it started"
			if env.ctx.Err() == nil {
				setStatus("failed", summary)
				w.logf("schedule %s run stopped before it started", schedule.ID)
			}
			return
		}
		defer env.sem.release()
		env.tasks.mu.Lock()
		env.tasks.queued--
		env.tasks.running++
		env.tasks.mu.Unlock()
		setStatus("active", "")
		w.logf("schedule %s run started", schedule.ID)
		issue := Issue{Title: schedule.Name, Target: schedule.Repository, Repository: schedule.Repository, Prompt: schedule.Prompt, ScheduleID: schedule.ID}
		output := jobOutputWriter{write: func(text string) {
			env.jobMu.Lock()
			job := env.jobs[key]
			job.Log += text
			env.jobs[key] = job
			env.jobMu.Unlock()
			env.publish()
		}}
		updateSession := func(update AgentSession) {
			env.jobMu.Lock()
			job := env.jobs[key]
			if update.ID != "" {
				job.SessionID = update.ID
			}
			if update.CheckoutDirectory != "" {
				job.CheckoutDirectory = update.CheckoutDirectory
			}
			env.jobs[key] = job
			env.jobMu.Unlock()
			env.publish()
		}
		var runErr error
		switch runner := w.runner().(type) {
		case SessionAgentOutputRunner:
			runErr = runner.RunSessionWithOutput(runCtx, issue, session, updateSession, output)
		case AgentOutputRunner:
			runErr = runner.RunWithOutput(runCtx, issue, output)
		default:
			runErr = w.runner().Run(runCtx, issue)
		}
		// The agent exiting is the run finishing; a wakeup it asked for has
		// nothing to come back to.
		var wakeup *scheduledWakeup
		if errors.As(runErr, &wakeup) {
			runErr = nil
		}
		if cause := context.Cause(runCtx); cause != nil && errors.Is(cause, errWorkStoppedFromWebUI) {
			runErr = cause
		}
		env.tasks.mu.Lock()
		env.tasks.running--
		if runErr != nil {
			env.tasks.failed++
		} else {
			env.tasks.completed++
		}
		env.tasks.mu.Unlock()
		env.jobMu.Lock()
		summary = lastLine(env.jobs[key].Log)
		env.jobMu.Unlock()
		if runErr != nil {
			status, summary = core.ScheduleRunFailed, runErr.Error()
			setStatus("failed", runErr.Error())
			w.logf("schedule %s run failed: %v", schedule.ID, runErr)
			return
		}
		status = core.ScheduleRunOK
		setStatus("complete", "")
		w.logf("schedule %s run completed", schedule.ID)
	}()
}

// serveScheduleJobAction answers a dashboard's stop or retry on a scheduled
// run's job card. A scheduled run has no session to hold, so it cannot be
// paused; a retry runs the schedule again, after stopping a run in flight.
func (w *Glorp) serveScheduleJobAction(env scheduleJobEnv, request jobActionRequest) {
	action := request.action
	id, _ := core.ScheduleIDFromTarget(action.Target)
	key := action.Target + "#" + fmt.Sprint(action.Number)
	env.jobMu.Lock()
	job, exists := env.jobs[key]
	if exists && action.Action == "stop" && (job.Status == "active" || job.Status == "queued") {
		job.Status = "stopping"
		env.jobs[key] = job
	}
	env.jobMu.Unlock()
	if !exists {
		request.done <- fmt.Errorf("job not found")
		return
	}
	env.runs.mu.Lock()
	cancel, exited := env.runs.cancels[key], env.runs.exits[key]
	env.runs.mu.Unlock()
	switch action.Action {
	case "pause":
		request.done <- fmt.Errorf("a scheduled run cannot be paused; stop it instead")
	case "stop":
		if cancel == nil {
			request.done <- fmt.Errorf("job is not running")
			return
		}
		w.logf("schedule %s run stop requested from web UI", id)
		env.publish()
		cancel(errWorkStoppedFromWebUI)
		go func() {
			select {
			case <-exited:
				w.logf("schedule %s run stopped from web UI; its agent has exited", id)
				request.done <- nil
			case <-env.ctx.Done():
				request.done <- fmt.Errorf("job is still stopping: %w", env.ctx.Err())
			}
		}()
	case "retry":
		if cancel != nil {
			w.logf("schedule %s run retry requested from web UI; stopping it and running the schedule again", id)
			env.runs.mu.Lock()
			env.runs.retry[key] = true
			env.runs.mu.Unlock()
			cancel(errWorkStoppedFromWebUI)
			request.done <- nil
			return
		}
		w.logf("schedule %s run retry requested from web UI; running the schedule again", id)
		env.rerun(id)
		request.done <- nil
	default:
		request.done <- fmt.Errorf("unknown job action %q", action.Action)
	}
}
