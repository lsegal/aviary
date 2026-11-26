package server

import (
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
)

// resetWSClients empties the global WebSocket client registry for the test
// and restores the previous registry afterwards.
func resetWSClients(t *testing.T) {
	t.Helper()
	wsClients.mu.Lock()
	saved := wsClients.m
	wsClients.m = make(map[*websocket.Conn]*wsClient)
	wsClients.mu.Unlock()
	t.Cleanup(func() {
		wsClients.mu.Lock()
		wsClients.m = saved
		wsClients.mu.Unlock()
	})
}

// startWSServer serves wsHandler on a test server. Cleanup waits for every
// handler to unregister so no handler outlives the test's seams.
func startWSServer(t *testing.T, token string, authDisabled func() bool) *httptest.Server {
	t.Helper()
	resetWSClients(t)
	srv := httptest.NewServer(wsHandler(token, authDisabled))
	t.Cleanup(func() {
		waitForWSClients(t, 0)
		srv.Close()
	})
	return srv
}

// dialWS connects to srv's WebSocket endpoint with the given query and
// headers, closing the connection when the test ends.
func dialWS(t *testing.T, srv *httptest.Server, query string, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/api/ws" + query
	conn, resp, err := websocket.DefaultDialer.Dial(url, header)
	if resp != nil && resp.Body != nil {
		t.Cleanup(func() { _ = resp.Body.Close() })
	}
	if conn != nil {
		t.Cleanup(func() { _ = conn.Close() })
	}
	return conn, resp, err
}

func mustDialWS(t *testing.T, srv *httptest.Server, query string, header http.Header) *websocket.Conn {
	t.Helper()
	conn, _, err := dialWS(t, srv, query, header)
	require.NoError(t, err)
	return conn
}

func readWSEvent(t *testing.T, conn *websocket.Conn) wsEvent {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	var ev wsEvent
	require.NoError(t, conn.ReadJSON(&ev))
	return ev
}

func waitForWSClients(t *testing.T, n int) {
	t.Helper()
	assert.Eventually(t, func() bool { return wsClientCount() == n }, 5*time.Second, 5*time.Millisecond,
		"want %d connected WebSocket clients", n)
}

func assertHealthEvent(t *testing.T, ev wsEvent) {
	t.Helper()
	assert.Equal(t, "health", ev.Type)
	assert.True(t, ev.OK)
	assert.NotEmpty(t, ev.Version)
	assert.Equal(t, runtime.GOOS, ev.GOOS)
}

func TestWsHandlerRejectsMissingOrWrongCredentials(t *testing.T) {
	srv := startWSServer(t, "secret-token", nil)

	cases := map[string]struct {
		query  string
		header http.Header
	}{
		"no credentials": {},
		"wrong query":    {query: "?token=wrong"},
		"wrong cookie":   {header: http.Header{"Cookie": {"aviary_session=wrong"}}},
		"auth enabled":   {query: "?token=nope", header: http.Header{"Cookie": {"aviary_session=nope"}}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			conn, resp, err := dialWS(t, srv, tc.query, tc.header)
			require.Error(t, err)
			assert.Nil(t, conn)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			assert.Zero(t, wsClientCount())
		})
	}
}

func TestWsHandlerRejectsEmptyQueryTokenWhenTokenUnset(t *testing.T) {
	// An empty server token must never match an empty ?token= value.
	srv := startWSServer(t, "", func() bool { return false })

	_, resp, err := dialWS(t, srv, "?token=", nil)
	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestWsHandlerAuthenticatesAndSendsHealthFirst(t *testing.T) {
	const token = "aviary_tok_ws"
	cases := map[string]struct {
		authDisabled func() bool
		query        string
		header       http.Header
	}{
		"session cookie": {header: http.Header{"Cookie": {"aviary_session=" + token}}},
		"query token":    {query: "?token=" + token},
		"auth disabled":  {authDisabled: func() bool { return true }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			srv := startWSServer(t, token, tc.authDisabled)

			conn := mustDialWS(t, srv, tc.query, tc.header)
			assertHealthEvent(t, readWSEvent(t, conn))
			waitForWSClients(t, 1)

			require.NoError(t, conn.Close())
			waitForWSClients(t, 0)
		})
	}
}

func TestWsHandlerReplaysActiveTurnsAfterHealth(t *testing.T) {
	turns := []agent.ActiveTurn{
		{AgentID: "a1", SessionID: "s1", Processing: true, Items: []agent.ActiveTurnItem{{Text: "partial"}}},
		{AgentID: "a2", SessionID: "s2", Processing: true},
	}
	prev := wsActiveTurns
	wsActiveTurns = func() []agent.ActiveTurn { return turns }
	t.Cleanup(func() { wsActiveTurns = prev })

	srv := startWSServer(t, "", func() bool { return true })
	conn := mustDialWS(t, srv, "", nil)

	assertHealthEvent(t, readWSEvent(t, conn))
	for _, want := range turns {
		ev := readWSEvent(t, conn)
		assert.Equal(t, "session_turn", ev.Type)
		assert.Equal(t, want.AgentID, ev.AgentID)
		assert.Equal(t, want.SessionID, ev.SessionID)
		require.NotNil(t, ev.Turn)
		assert.Equal(t, want, *ev.Turn)
	}
}

func TestWsHandlerResendsHealthOnHeartbeat(t *testing.T) {
	prev := wsHeartbeatInterval
	wsHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { wsHeartbeatInterval = prev })

	srv := startWSServer(t, "", func() bool { return true })
	conn := mustDialWS(t, srv, "", nil)

	assertHealthEvent(t, readWSEvent(t, conn))
	assertHealthEvent(t, readWSEvent(t, conn))
	assertHealthEvent(t, readWSEvent(t, conn))
}

func TestWsHandlerHeartbeatStopsAfterClientLeaves(t *testing.T) {
	prev := wsHeartbeatInterval
	wsHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { wsHeartbeatInterval = prev })

	srv := startWSServer(t, "", func() bool { return true })
	conn := mustDialWS(t, srv, "", nil)
	assertHealthEvent(t, readWSEvent(t, conn))
	waitForWSClients(t, 1)

	// Dropping the TCP connection without a close frame still ends the
	// handler, through either the read loop or a failed heartbeat write.
	require.NoError(t, conn.UnderlyingConn().Close())
	waitForWSClients(t, 0)
}

func TestWsBroadcastReachesEveryClient(t *testing.T) {
	srv := startWSServer(t, "", func() bool { return true })

	conns := []*websocket.Conn{mustDialWS(t, srv, "", nil), mustDialWS(t, srv, "", nil), mustDialWS(t, srv, "", nil)}
	for _, conn := range conns {
		assertHealthEvent(t, readWSEvent(t, conn))
	}
	waitForWSClients(t, len(conns))

	wsBroadcast(wsEvent{Type: "project-added", Target: "lsegal/aviary"})

	for _, conn := range conns {
		ev := readWSEvent(t, conn)
		assert.Equal(t, "project-added", ev.Type)
		assert.Equal(t, "lsegal/aviary", ev.Target)
	}

	require.NoError(t, conns[0].Close())
	waitForWSClients(t, len(conns)-1)
}

func TestWsWriteJSONIgnoresUnregisteredConn(t *testing.T) {
	srv := startWSServer(t, "", func() bool { return true })
	conn := mustDialWS(t, srv, "", nil)
	assertHealthEvent(t, readWSEvent(t, conn))

	// The client side of the connection was never registered, so nothing is
	// written and no error is reported.
	assert.Nil(t, wsClientFor(conn))
	assert.NoError(t, wsWriteJSON(conn, wsEvent{Type: "health"}))
}

func TestWsWriteJSONWritesToRegisteredConn(t *testing.T) {
	received := make(chan wsEvent, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := wsUpgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		var ev wsEvent
		if conn.ReadJSON(&ev) == nil {
			received <- ev
		}
	}))
	t.Cleanup(srv.Close)
	resetWSClients(t)

	conn := mustDialWS(t, srv, "", nil)
	wsRegister(conn)
	t.Cleanup(func() { wsUnregister(conn) })
	require.NotNil(t, wsClientFor(conn))
	assert.Equal(t, 1, wsClientCount())

	require.NoError(t, wsWriteJSON(conn, wsEvent{Type: "notification", SessionID: "s1"}))
	select {
	case ev := <-received:
		assert.Equal(t, "notification", ev.Type)
		assert.Equal(t, "s1", ev.SessionID)
	case <-time.After(5 * time.Second):
		t.Fatal("event was not delivered")
	}
}
