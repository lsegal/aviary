//go:build windows

package server

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modpsapi                 = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessMemoryInfo = modpsapi.NewProc("GetProcessMemoryInfo")
)

// processMemoryCounters mirrors the Win32 PROCESS_MEMORY_COUNTERS struct.
type processMemoryCounters struct {
	Cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaNonPagedPoolUsage     uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	PeakPagefileUsage          uintptr
	PagefileUsage              uintptr
}

// Sample reads CPU ticks and working-set size for each PID using Windows APIs,
// computes CPU% as a delta from the previous call, and caches the results.
func (s *ProcSampler) Sample(pids []int) {
	now := time.Now()
	for _, pid := range pids {
		ticks, stats := readWindowsProcStats(uint32(pid))

		s.mu.Lock()
		if prev, ok := s.prev[pid]; ok {
			elapsed := now.Sub(prev.at).Seconds()
			if elapsed > 0 && ticks >= prev.ticks {
				// Windows process times are in 100-nanosecond intervals.
				// Convert delta ticks to seconds: delta * 100ns = delta * 1e-7 s
				delta := float64(ticks-prev.ticks) * 1e-7
				stats.CPUPercent = (delta / elapsed) * 100
			}
		}
		s.prev[pid] = prevSample{ticks: ticks, at: now}
		s.stats[pid] = stats
		s.mu.Unlock()
	}
}

func readWindowsProcStats(pid uint32) (ticks uint64, stats ProcStats) {
	stats.CPUPercent = -1

	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		stats.Status = "gone"
		return
	}
	defer windows.CloseHandle(h) //nolint:errcheck

	stats.Status = "running"

	var mem processMemoryCounters
	mem.Cb = uint32(unsafe.Sizeof(mem))
	r, _, _ := procGetProcessMemoryInfo.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(&mem)),
		uintptr(mem.Cb),
	)
	if r != 0 {
		stats.RSSBytes = uint64(mem.WorkingSetSize)
	}

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(h, &creation, &exit, &kernel, &user); err == nil {
		k := uint64(kernel.HighDateTime)<<32 | uint64(kernel.LowDateTime)
		u := uint64(user.HighDateTime)<<32 | uint64(user.LowDateTime)
		ticks = k + u
	}

	return
}
