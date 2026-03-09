package server

import (
	"sync"
	"time"
)

// ProcStats holds a snapshot of process resource usage.
type ProcStats struct {
	CPUPercent float64 `json:"cpu_percent"` // CPU usage 0-100; -1 if unavailable
	RSSBytes   uint64  `json:"rss_bytes"`   // resident set size in bytes; 0 if unavailable
	Status     string  `json:"status"`      // "running", "sleeping", "zombie", etc.
}

// prevSample stores the previous reading used to compute CPU% delta.
type prevSample struct {
	ticks uint64
	at    time.Time
}

// ProcSampler periodically measures CPU% and RSS for tracked PIDs.
// It must be refreshed via Sample on a regular interval; callers retrieve
// cached values via Get without blocking.
type ProcSampler struct {
	mu    sync.RWMutex
	stats map[int]ProcStats
	prev  map[int]prevSample
}

// NewProcSampler creates an empty ProcSampler.
func NewProcSampler() *ProcSampler {
	return &ProcSampler{
		stats: make(map[int]ProcStats),
		prev:  make(map[int]prevSample),
	}
}

// Get returns the most recently cached stats for pid, or (zero, false) if not tracked.
func (s *ProcSampler) Get(pid int) (ProcStats, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.stats[pid]
	return v, ok
}

// Forget removes cached state for a PID that is no longer needed.
func (s *ProcSampler) Forget(pid int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.stats, pid)
	delete(s.prev, pid)
}
