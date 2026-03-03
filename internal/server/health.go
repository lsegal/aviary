package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// healthPayload is the JSON shape sent by both /api/health and the WebSocket heartbeat.
type healthPayload struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
}

type wsEvent struct {
	Type         string `json:"type"`
	SessionID    string `json:"session_id,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	PoolID       string `json:"pool_id,omitempty"`
	Role         string `json:"role,omitempty"`
	IsProcessing *bool  `json:"is_processing,omitempty"`
	OK           bool   `json:"ok,omitempty"`
	Version      string `json:"version,omitempty"`
}

var wsClients = struct {
	mu sync.Mutex
	m  map[*websocket.Conn]struct{}
}{m: make(map[*websocket.Conn]struct{})}

func wsRegister(conn *websocket.Conn) {
	wsClients.mu.Lock()
	wsClients.m[conn] = struct{}{}
	wsClients.mu.Unlock()
}

func wsUnregister(conn *websocket.Conn) {
	wsClients.mu.Lock()
	delete(wsClients.m, conn)
	wsClients.mu.Unlock()
}

func wsBroadcast(event wsEvent) {
	wsClients.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(wsClients.m))
	for conn := range wsClients.m {
		clients = append(clients, conn)
	}
	wsClients.mu.Unlock()

	for _, conn := range clients {
		_ = conn.WriteJSON(event)
	}
}

// wsUpgrader upgrades HTTP connections to WebSocket.
// Origin checking is intentionally permissive — auth is enforced separately via
// the session cookie / query-param token, and TLS provides transport security.
var wsUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  256,
	WriteBufferSize: 256,
}

// healthHandler handles GET /api/health.
// Public — no authentication required. Returns current version and status.
func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(healthPayload{OK: true, Version: Version})
}

// wsHandler returns a handler for GET /api/ws.
// Requires authentication via the aviary_session cookie (set at login) or a
// ?token= query parameter (fallback for environments where cookies are blocked).
//
// On connect it immediately sends a healthPayload message, then repeats every
// 30 s so the client can confirm the server is still alive. The connection is
// closed when the client disconnects.
func wsHandler(token string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth: session cookie (preferred) or explicit query param.
		authed := false
		if c, err := r.Cookie("aviary_session"); err == nil && c.Value == token {
			authed = true
		}
		if !authed {
			if q := r.URL.Query().Get("token"); q == token && token != "" {
				authed = true
			}
		}
		if !authed {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		wsRegister(conn)
		defer conn.Close()
		defer wsUnregister(conn)

		payload := wsEvent{Type: "health", OK: true, Version: Version}

		// Send the initial status immediately on connect.
		if err := conn.WriteJSON(payload); err != nil {
			return
		}

		// Drain incoming frames so we detect client disconnects promptly.
		done := make(chan struct{})
		go func() {
			defer close(done)
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		}()

		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				if err := conn.WriteJSON(payload); err != nil {
					return
				}
			}
		}
	}
}
