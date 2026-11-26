package projects

import (
	"context"
	"maps"
	"sync/atomic"
	"time"
)

// quotaRefreshInterval is how often the engine rereads agent quotas. Each
// reader caches its own reading for a minute, so a shorter interval would not
// read any more often.
const quotaRefreshInterval = time.Minute

// engineQuotas holds the engine's most recent quota readings. Reading a quota
// can run a CLI subprocess for many seconds, so publish only ever reads the
// cached readings and a background goroutine refreshes them (issue #1429).
type engineQuotas struct {
	readings atomic.Pointer[map[string]string]
	// interval overrides quotaRefreshInterval in tests.
	interval time.Duration
}

// cachedQuotas returns the most recent quota readings, or nil before the
// first refresh finishes. It never calls a quota reader.
func (w *Engine) cachedQuotas() map[string]string {
	if readings := w.quotas.readings.Load(); readings != nil {
		return *readings
	}
	return nil
}

// refreshQuotasEvery reads quotas now and then on every interval until ctx
// ends, calling changed after each refresh whose readings differ from the
// last ones so the new readings reach the UIs.
func (w *Engine) refreshQuotasEvery(ctx context.Context, changed func()) {
	if w.Quota == nil {
		return
	}
	interval := w.quotas.interval
	if interval <= 0 {
		interval = quotaRefreshInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		readings := w.Quota(ctx)
		if ctx.Err() != nil {
			return
		}
		previous := w.quotas.readings.Swap(&readings)
		if previous == nil || !maps.Equal(*previous, readings) {
			changed()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
