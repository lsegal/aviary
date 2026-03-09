//go:build !linux && !windows

package server

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// Sample uses ps(1) to read CPU% and RSS for each PID on non-Linux Unix systems
// (macOS, FreeBSD, etc.) and caches the results. The CPU% value comes from ps
// and reflects the process lifetime average.
func (s *ProcSampler) Sample(pids []int) {
	now := time.Now()
	for _, pid := range pids {
		stats := readPSStats(pid)
		s.mu.Lock()
		s.prev[pid] = prevSample{ticks: 0, at: now}
		s.stats[pid] = stats
		s.mu.Unlock()
	}
}

func readPSStats(pid int) ProcStats {
	result := ProcStats{CPUPercent: -1}
	// -o rss= pcpu= state=   (trailing = suppresses the header)
	out, err := exec.Command("ps", "-o", "rss=,pcpu=,state=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		result.Status = "gone"
		return result
	}
	fields := strings.Fields(strings.TrimSpace(string(out)))
	if len(fields) >= 1 {
		rssKB, _ := strconv.ParseUint(fields[0], 10, 64)
		result.RSSBytes = rssKB * 1024
	}
	if len(fields) >= 2 {
		cpu, _ := strconv.ParseFloat(fields[1], 64)
		result.CPUPercent = cpu
	}
	if len(fields) >= 3 && len(fields[2]) > 0 {
		switch fields[2][0] {
		case 'R':
			result.Status = "running"
		case 'S', 'I':
			result.Status = "sleeping"
		case 'Z':
			result.Status = "zombie"
		case 'T':
			result.Status = "stopped"
		default:
			result.Status = strings.ToLower(string(fields[2][0]))
		}
	} else {
		result.Status = "running"
	}
	return result
}
