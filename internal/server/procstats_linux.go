//go:build linux

package server

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// clkTck is Linux USER_HZ — the number of clock ticks per second.
// Almost universally 100 on modern kernels.
const clkTck = 100.0

// Sample reads /proc/<pid>/stat and /proc/<pid>/statm for each PID, computes
// CPU% as a delta from the previous call, and caches the results.
func (s *ProcSampler) Sample(pids []int) {
	now := time.Now()
	for _, pid := range pids {
		ticks, stats := readLinuxProcStats(pid)

		s.mu.Lock()
		if prev, ok := s.prev[pid]; ok {
			elapsed := now.Sub(prev.at).Seconds()
			if elapsed > 0 && ticks >= prev.ticks {
				delta := float64(ticks - prev.ticks)
				// delta is in clock ticks; CPU% = (ticks/clkTck) / elapsed * 100
				stats.CPUPercent = (delta / clkTck / elapsed) * 100
			}
		}
		s.prev[pid] = prevSample{ticks: ticks, at: now}
		s.stats[pid] = stats
		s.mu.Unlock()
	}
}

// readLinuxProcStats reads /proc/<pid>/stat for CPU ticks + state, and
// /proc/<pid>/statm for RSS.
func readLinuxProcStats(pid int) (ticks uint64, stats ProcStats) {
	stats.CPUPercent = -1

	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		stats.Status = "gone"
		return
	}

	// The comm field (second field) is wrapped in parentheses and may itself
	// contain spaces or parentheses. Find the last ')' to safely skip it.
	end := strings.LastIndex(string(data), ")")
	if end < 0 {
		stats.Status = "unknown"
		return
	}

	fields := strings.Fields(string(data[end+1:]))
	// After ')': state ppid pgrp session tty_nr tpgid flags
	//             minflt cminflt majflt cmajflt utime stime ...
	// indices:     0     1      2     3        4      5      6
	//              7     8      9     10       11     12
	if len(fields) > 0 {
		switch fields[0] {
		case "R":
			stats.Status = "running"
		case "S":
			stats.Status = "sleeping"
		case "D":
			stats.Status = "disk-wait"
		case "Z":
			stats.Status = "zombie"
		case "T", "t":
			stats.Status = "stopped"
		default:
			stats.Status = strings.ToLower(fields[0])
		}
	}
	if len(fields) >= 13 {
		utime, _ := strconv.ParseUint(fields[11], 10, 64)
		stime, _ := strconv.ParseUint(fields[12], 10, 64)
		ticks = utime + stime
	}

	// RSS from /proc/<pid>/statm: "total rss shared text lib data dt" in pages.
	if statm, err := os.ReadFile(fmt.Sprintf("/proc/%d/statm", pid)); err == nil {
		if sm := strings.Fields(string(statm)); len(sm) >= 2 {
			pages, _ := strconv.ParseUint(sm[1], 10, 64)
			stats.RSSBytes = pages * uint64(os.Getpagesize())
		}
	}

	return
}
