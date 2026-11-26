package browser

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilteredChromeDPErrorfLogsIgnorableErrorsAtDebug(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	tests := []struct {
		name      string
		format    string
		args      []any
		wantLevel string
	}{
		{
			name:      "unhandled node event",
			format:    "unhandled node event %s",
			args:      []any{"*dom.EventTopLayerElementsUpdated"},
			wantLevel: "level=DEBUG",
		},
		{
			name:      "FedCM initiator",
			format:    "could not unmarshal event: %v",
			args:      []any{`parse error: unknown InitiatorType value: FedCM`},
			wantLevel: "level=DEBUG",
		},
		{
			name:      "other unmarshal error",
			format:    "could not unmarshal event: %v",
			args:      []any{"unknown ResourceType value: Bogus"},
			wantLevel: "level=ERROR",
		},
		{
			name:      "unrelated error",
			format:    "could not retrieve document root for %s: %v",
			args:      []any{"F1", "boom"},
			wantLevel: "level=ERROR",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf.Reset()
			FilteredChromeDPErrorf(tt.format, tt.args...)
			out := buf.String()
			assert.Contains(t, out, tt.wantLevel)
			assert.Equal(t, 1, strings.Count(out, "\n"), "want exactly one log line, got %q", out)
			if tt.wantLevel == "level=ERROR" {
				assert.Contains(t, out, "component=browser")
				assert.NotContains(t, out, "ignored chromedp error")
			} else {
				assert.Contains(t, out, "ignored chromedp error")
			}
		})
	}
}

// fakeCDP is a minimal DevTools endpoint that answers the commands chromedp
// sends when attaching to an existing tab, plus the Browser window commands.
type fakeCDP struct {
	server *httptest.Server
	// fail maps a CDP method to the error message it answers with.
	fail map[string]string

	mu    sync.Mutex
	calls []fakeCDPCall
}

type fakeCDPCall struct {
	Method string
	Params json.RawMessage
}

func newFakeCDP(t *testing.T, fail map[string]string) *fakeCDP {
	t.Helper()
	f := &fakeCDP{fail: fail}
	upgrader := websocket.Upgrader{}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		f.serve(conn)
	}))
	t.Cleanup(f.server.Close)
	return f
}

// serve records and answers each CDP command on conn until it closes.
func (f *fakeCDP) serve(conn *websocket.Conn) {
	for {
		var msg struct {
			ID        int64           `json:"id"`
			SessionID string          `json:"sessionId,omitempty"`
			Method    string          `json:"method"`
			Params    json.RawMessage `json:"params"`
		}
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		f.mu.Lock()
		f.calls = append(f.calls, fakeCDPCall{Method: msg.Method, Params: msg.Params})
		f.mu.Unlock()
		if err := conn.WriteJSON(f.reply(msg.ID, msg.SessionID, msg.Method)); err != nil {
			return
		}
	}
}

// reply answers one command, with the error fail names for its method or
// its canned result.
func (f *fakeCDP) reply(id int64, sessionID, method string) map[string]any {
	reply := map[string]any{"id": id}
	if sessionID != "" {
		reply["sessionId"] = sessionID
	}
	if errMsg, ok := f.fail[method]; ok {
		reply["error"] = map[string]any{"code": -32000, "message": errMsg}
	} else {
		reply["result"] = fakeCDPResult(method)
	}
	return reply
}

func fakeCDPResult(method string) any {
	switch method {
	case "Target.attachToTarget":
		return map[string]any{"sessionId": "session-1"}
	case "Runtime.evaluate":
		return map[string]any{"result": map[string]any{"type": "object", "className": "Window"}}
	case "Page.getFrameTree":
		return map[string]any{"frameTree": map[string]any{"frame": map[string]any{
			"id": "frame-1", "loaderId": "loader-1", "url": "about:blank",
			"domainAndRegistry": "", "securityOrigin": "null", "mimeType": "text/html",
			"secureContextType": "InsecureScheme", "crossOriginIsolatedContextType": "NotIsolated",
			"gatedAPIFeatures": []string{},
		}}}
	case "DOM.getDocument":
		return map[string]any{"root": map[string]any{
			"nodeId": 1, "backendNodeId": 1, "nodeType": 9, "nodeName": "#document",
			"localName": "", "nodeValue": "",
		}}
	case "Browser.getWindowForTarget":
		return map[string]any{"windowId": 7, "bounds": map[string]any{"width": 800, "height": 600}}
	default:
		return map[string]any{}
	}
}

func (f *fakeCDP) wsURL() string {
	return "ws" + strings.TrimPrefix(f.server.URL, "http") + "/devtools/browser/fake"
}

// paramsOf returns the params of every call to method, in order.
func (f *fakeCDP) paramsOf(method string) []json.RawMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []json.RawMessage
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c.Params)
		}
	}
	return out
}

func attachFakeSession(t *testing.T, f *fakeCDP) *Session {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	s, err := newRemoteSessionForTab(ctx, f.wsURL(), "tab-1")
	require.NoError(t, err)
	t.Cleanup(s.Detach)
	return s
}

func TestNewRemoteSessionForTabAttachesToTheTab(t *testing.T) {
	f := newFakeCDP(t, nil)
	s := attachFakeSession(t, f)

	assert.Equal(t, "tab-1", s.TabID())
	attaches := f.paramsOf("Target.attachToTarget")
	require.Len(t, attaches, 1)
	var params struct {
		TargetID string `json:"targetId"`
		Flatten  bool   `json:"flatten"`
	}
	require.NoError(t, json.Unmarshal(attaches[0], &params))
	assert.Equal(t, "tab-1", params.TargetID)
	assert.True(t, params.Flatten)
	// Attaching must never create a new tab.
	assert.Empty(t, f.paramsOf("Target.createTarget"))
}

func TestNewRemoteSessionForTabReportsAnAttachFailure(t *testing.T) {
	f := newFakeCDP(t, map[string]string{"Target.attachToTarget": "No target with given id found"})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	s, err := newRemoteSessionForTab(ctx, f.wsURL(), "tab-1")
	require.Error(t, err)
	assert.Nil(t, s)
	assert.Contains(t, err.Error(), "attaching to tab tab-1")
	assert.Contains(t, err.Error(), "No target with given id found")
}

func TestResizeWindowSetsTheTabsWindowBounds(t *testing.T) {
	f := newFakeCDP(t, nil)
	s := attachFakeSession(t, f)

	require.NoError(t, s.ResizeWindow(1280, 720))

	sets := f.paramsOf("Browser.setWindowBounds")
	require.Len(t, sets, 1)
	var params struct {
		WindowID int64 `json:"windowId"`
		Bounds   struct {
			Width  int64 `json:"width"`
			Height int64 `json:"height"`
		} `json:"bounds"`
	}
	require.NoError(t, json.Unmarshal(sets[0], &params))
	assert.Equal(t, int64(7), params.WindowID, "bounds must be set on the tab's own window")
	assert.Equal(t, int64(1280), params.Bounds.Width)
	assert.Equal(t, int64(720), params.Bounds.Height)
}

func TestResizeWindowReportsAMissingWindow(t *testing.T) {
	f := newFakeCDP(t, map[string]string{"Browser.getWindowForTarget": "Browser window not found"})
	s := attachFakeSession(t, f)

	err := s.ResizeWindow(1280, 720)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Browser window not found")
	assert.Empty(t, f.paramsOf("Browser.setWindowBounds"))
}

func TestResizeWindowReportsASetBoundsFailure(t *testing.T) {
	f := newFakeCDP(t, map[string]string{"Browser.setWindowBounds": "Window bounds unsupported"})
	s := attachFakeSession(t, f)

	err := s.ResizeWindow(1280, 720)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Window bounds unsupported")
}

func TestResizeWindowFailsAfterTheSessionCloses(t *testing.T) {
	f := newFakeCDP(t, nil)
	s := attachFakeSession(t, f)
	s.Close()

	err := s.ResizeWindow(1280, 720)
	require.ErrorIs(t, err, context.Canceled)
	assert.Empty(t, f.paramsOf("Browser.getWindowForTarget"))
}
