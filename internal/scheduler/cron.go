package scheduler

import (
	"log/slog"
	"sync"

	"github.com/robfig/cron/v3"
)

// CronRunner wraps robfig/cron to add/remove named entries idempotently.
type CronRunner struct {
	c    *cron.Cron
	mu   sync.Mutex
	ids  map[string]cron.EntryID // task name → entry ID
}

// NewCronRunner creates a CronRunner with second-precision support.
func NewCronRunner() *CronRunner {
	return &CronRunner{
		c:   cron.New(cron.WithSeconds()),
		ids: make(map[string]cron.EntryID),
	}
}

// Add registers a named cron entry. If the name already exists, it is replaced.
func (r *CronRunner) Add(name, schedule string, fn func()) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Remove existing entry for this name.
	if id, ok := r.ids[name]; ok {
		r.c.Remove(id)
		delete(r.ids, name)
	}

	id, err := r.c.AddFunc(schedule, fn)
	if err != nil {
		return err
	}
	r.ids[name] = id
	slog.Info("cron task added", "name", name, "schedule", schedule)
	return nil
}

// Remove unregisters a named cron entry.
func (r *CronRunner) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if id, ok := r.ids[name]; ok {
		r.c.Remove(id)
		delete(r.ids, name)
		slog.Info("cron task removed", "name", name)
	}
}

// Start begins the cron scheduler.
func (r *CronRunner) Start() { r.c.Start() }

// Stop halts the cron scheduler, waiting for any running jobs to complete.
func (r *CronRunner) Stop() { r.c.Stop() }
