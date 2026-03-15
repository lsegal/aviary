package server

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"time"
)

// serverStartTime records when the aviary process started (package-load time).
var serverStartTime = time.Now()

// DaemonStatus describes a single running daemon or integration service.
type DaemonStatus struct {
	Name       string  `json:"name"`          // display name, e.g. "aviary", "myagent/signal/0"
	Type       string  `json:"type"`          // "server", "signal", "slack", "discord"
	PID        int     `json:"pid,omitempty"` // process ID (0 = no subprocess)
	Addr       string  `json:"addr,omitempty"`
	Started    string  `json:"started"`
	Uptime     string  `json:"uptime"`
	CPUPercent float64 `json:"cpu_percent"` // -1 = unavailable
	RSSBytes   uint64  `json:"rss_bytes"`
	Status     string  `json:"status"`          // "running", "sleeping", "gone", "error", etc.
	Error      string  `json:"error,omitempty"` // last error message, set when status == "error"
	Managed    bool    `json:"managed"`         // true = aviary owns and monitors the process
}

// daemonsHandler handles GET /api/daemons.
// Returns a JSON array of all running daemon/service entries.
func (s *Server) daemonsHandler(w http.ResponseWriter, _ *http.Request) {
	now := time.Now()
	port := s.cfg.Server.Port
	if port == 0 {
		port = 16677
	}

	// Aviary process itself — use Go runtime for memory.
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	selfPID := os.Getpid()
	selfStats, _ := s.sampler.Get(selfPID)
	if selfStats.Status == "" {
		selfStats.Status = "running"
	}

	daemons := []DaemonStatus{
		{
			Name:       "aviary",
			Type:       "server",
			PID:        selfPID,
			Addr:       fmt.Sprintf(":%d", port),
			Started:    serverStartTime.UTC().Format(time.RFC3339),
			Uptime:     fmtUptime(now.Sub(serverStartTime)),
			CPUPercent: selfStats.CPUPercent,
			RSSBytes:   ms.Sys, // Go runtime total memory from OS
			Status:     selfStats.Status,
			Managed:    false,
		},
	}

	seenPIDs := map[int]bool{}
	for _, cs := range s.channels.List() {
		d := DaemonStatus{
			Name:       cs.Key,
			Type:       cs.Type,
			Started:    cs.Started.UTC().Format(time.RFC3339),
			Uptime:     fmtUptime(now.Sub(cs.Started)),
			CPUPercent: -1,
			Status:     "running",
			Managed:    false,
		}
		if cs.Error != "" {
			d.Status = "error"
			d.Error = cs.Error
		}
		if cs.Daemon != nil {
			d.Addr = cs.Daemon.Addr
			d.Managed = !cs.Daemon.External
			if cs.Daemon.PID > 0 {
				// Shared daemon: skip duplicate entries for the same subprocess.
				if seenPIDs[cs.Daemon.PID] {
					continue
				}
				seenPIDs[cs.Daemon.PID] = true
				d.PID = cs.Daemon.PID
				if !cs.Daemon.Started.IsZero() {
					d.Started = cs.Daemon.Started.UTC().Format(time.RFC3339)
					d.Uptime = fmtUptime(now.Sub(cs.Daemon.Started))
				}
				if stats, ok := s.sampler.Get(cs.Daemon.PID); ok {
					d.CPUPercent = stats.CPUPercent
					d.RSSBytes = stats.RSSBytes
					d.Status = stats.Status
				}
			} else if cs.Daemon.External && cs.Daemon.Addr != "" {
				// External daemon: probe reachability with a short-timeout dial.
				conn, err := net.DialTimeout("tcp", cs.Daemon.Addr, 300*time.Millisecond)
				if err != nil {
					d.Status = "unreachable"
				} else {
					conn.Close() //nolint:errcheck
					d.Status = "connected"
				}
			}
		}
		daemons = append(daemons, d)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(daemons)
}

// daemonLogsHandler handles GET /api/daemons/logs?key=<channel-key>.
// Streams raw stdout/stderr lines from the managed subprocess via SSE.
// Each event's data field is a JSON-encoded string (the raw log line).
func (s *Server) daemonLogsHandler(w http.ResponseWriter, r *http.Request) {
	key := r.URL.Query().Get("key")
	if key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}

	history, live, unsub, ok := s.channels.SubscribeLogs(key)
	if !ok {
		http.Error(w, "daemon not found", http.StatusNotFound)
		return
	}
	defer unsub()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	send := func(line string) {
		data, _ := json.Marshal(line)
		fmt.Fprintf(w, "data: %s\n\n", data) //nolint:errcheck
		flusher.Flush()
	}

	for _, line := range history {
		send(line)
	}

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-live:
			if !ok {
				return
			}
			send(line)
		}
	}
}

type daemonRestartRequest struct {
	Key string `json:"key"`
}

// daemonRestartHandler handles POST /api/daemons/restart.
func (s *Server) daemonRestartHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req daemonRestartRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if req.Key == "" {
		http.Error(w, "missing key", http.StatusBadRequest)
		return
	}

	if req.Key == "aviary" {
		select {
		case s.hardRestartCh <- struct{}{}:
		default:
		}
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "restarting"})
		return
	}

	if s.runCtx == nil || s.msgFn == nil {
		http.Error(w, "server runtime not initialized", http.StatusServiceUnavailable)
		return
	}

	var restartable bool
	for _, cs := range s.channels.List() {
		if cs.Key == req.Key {
			restartable = cs.Daemon != nil && !cs.Daemon.External
			break
		}
	}
	if !restartable {
		http.Error(w, "daemon not restartable", http.StatusBadRequest)
		return
	}

	if err := s.channels.Restart(s.runCtx, req.Key, s.msgFn); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "restarting"})
}

// fmtUptime formats a duration as a short human-readable string.
func fmtUptime(d time.Duration) string {
	d = d.Truncate(time.Second)
	if d < 0 {
		return "0s"
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}
