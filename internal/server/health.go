package server

import (
	"encoding/json"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/saferestart"
	"github.com/lsegal/aviary/internal/update"
)

// healthPayload is the JSON shape sent by both /api/health and the WebSocket heartbeat.
type healthPayload struct {
	OK      bool   `json:"ok"`
	Version string `json:"version"`
	GOOS    string `json:"goos"`
	// AuthDisabled tells the web UI the server needs no token, so it can
	// skip the login prompt.
	AuthDisabled bool `json:"auth_disabled,omitempty"`
}

type wsEvent struct {
	Type         string `json:"type"`
	SessionID    string `json:"session_id,omitempty"`
	AgentID      string `json:"agent_id,omitempty"`
	PoolID       string `json:"pool_id,omitempty"`
	Role         string `json:"role,omitempty"`
	IsProcessing *bool  `json:"is_processing,omitempty"`
	// Turn is set on "session_turn" events: what the session's running turn
	// has streamed so far, or an idle turn once it ends.
	Turn    *agent.ActiveTurn `json:"turn,omitempty"`
	OK      bool              `json:"ok,omitempty"`
	Version string            `json:"version,omitempty"`
	GOOS    string            `json:"goos,omitempty"`
	// Restart is set on "restart" events and on health events while a
	// restart is waiting or in progress, so every dashboard shows it.
	Restart *saferestart.Status `json:"restart,omitempty"`
	// Check is set on "version" events when a new release is found.
	Check *update.CheckResult `json:"check,omitempty"`
}

type wsClient struct {
	conn    *websocket.Conn
	writeMu sync.Mutex
}

var wsClients = struct {
	mu sync.Mutex
	m  map[*websocket.Conn]*wsClient
}{m: make(map[*websocket.Conn]*wsClient)}

func wsRegister(conn *websocket.Conn) {
	wsClients.mu.Lock()
	wsClients.m[conn] = &wsClient{conn: conn}
	wsClients.mu.Unlock()
}

func wsUnregister(conn *websocket.Conn) {
	wsClients.mu.Lock()
	delete(wsClients.m, conn)
	wsClients.mu.Unlock()
}

func wsClientFor(conn *websocket.Conn) *wsClient {
	wsClients.mu.Lock()
	defer wsClients.mu.Unlock()
	return wsClients.m[conn]
}

func wsWriteJSON(conn *websocket.Conn, event wsEvent) error {
	client := wsClientFor(conn)
	if client == nil {
		return nil
	}

	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	return client.conn.WriteJSON(event)
}

func wsBroadcast(event wsEvent) {
	wsClients.mu.Lock()
	clients := make([]*wsClient, 0, len(wsClients.m))
	for _, client := range wsClients.m {
		clients = append(clients, client)
	}
	wsClients.mu.Unlock()

	for _, client := range clients {
		client.writeMu.Lock()
		_ = client.conn.WriteJSON(event)
		client.writeMu.Unlock()
	}
}

// wsUpgrader upgrades HTTP connections to WebSocket.
// Origin checking is intentionally permissive — auth is enforced separately via
// the session cookie / query-param token, and TLS provides transport security.
var wsUpgrader = websocket.Upgrader{
	CheckOrigin:     func(_ *http.Request) bool { return true },
	ReadBufferSize:  256,
	WriteBufferSize: 256,
}

// healthHandler handles GET /api/health.
// Public — no authentication required. Returns current version and status.
func healthHandler(authDisabled func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(healthPayload{
			OK:           true,
			Version:      update.CurrentVersion(),
			GOOS:         runtime.GOOS,
			AuthDisabled: tokenAuthDisabled(authDisabled),
		})
	}
}

// wsHandler returns a handler for GET /api/ws.
// Requires authentication via the aviary_session cookie (set at login) or a
// ?token= query parameter (fallback for environments where cookies are blocked).
//
// On connect it immediately sends a healthPayload message, then repeats every
// 30 s so the client can confirm the server is still alive. The connection is
// closed when the client disconnects.
func wsHandler(token string, authDisabled func() bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Auth: session cookie (preferred) or explicit query param, unless
		// token authentication is disabled.
		authed := tokenAuthDisabled(authDisabled)
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
		defer conn.Close() //nolint:errcheck
		defer wsUnregister(conn)

		healthEvent := func() wsEvent {
			ev := wsEvent{Type: "health", OK: true, Version: update.CurrentVersion(), GOOS: runtime.GOOS}
			ev.Restart = currentRestartSnapshot()
			return ev
		}

		// Send the initial status immediately on connect.
		if err := wsWriteJSON(conn, healthEvent()); err != nil {
			return
		}
		// A client connecting mid-turn restores each running turn's state.
		for _, turn := range agent.ActiveTurns() {
			if err := wsWriteJSON(conn, sessionTurnEvent(turn)); err != nil {
				return
			}
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
				if err := wsWriteJSON(conn, healthEvent()); err != nil {
					return
				}
			}
		}
	}
}

func sessionTurnEvent(turn agent.ActiveTurn) wsEvent {
	return wsEvent{Type: "session_turn", AgentID: turn.AgentID, SessionID: turn.SessionID, Turn: &turn}
}
