package channels

import (
	"sync"
	"time"
)

// receiptTTL outlasts platform redelivery schedules, such as Slack's retries
// (immediately, after one minute, and after five minutes).
const receiptTTL = 15 * time.Minute

const maxReceipts = 1024

// receipts remembers recently handled inbound deliveries, so a platform
// redelivering one (a retry, a resend, an update that changed nothing) does
// not start a second agent run that would compete with the first.
type receipts struct {
	mu sync.Mutex
	m  map[string]time.Time
}

// seen reports whether key was recorded within receiptTTL, recording it when
// it was not. An empty key is never seen.
func (r *receipts) seen(key string) bool {
	if key == "" {
		return false
	}
	now := time.Now()
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = make(map[string]time.Time)
	}
	if seenAt, ok := r.m[key]; ok && now.Sub(seenAt) < receiptTTL {
		return true
	}
	r.m[key] = now
	if len(r.m) > maxReceipts {
		for k, seenAt := range r.m {
			if now.Sub(seenAt) >= receiptTTL {
				delete(r.m, k)
			}
		}
		for k := range r.m {
			if len(r.m) <= maxReceipts {
				break
			}
			delete(r.m, k)
		}
	}
	return false
}
