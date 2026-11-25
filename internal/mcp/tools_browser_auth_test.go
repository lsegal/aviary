package mcp

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/agent"
	"github.com/lsegal/aviary/internal/auth"
	"github.com/lsegal/aviary/internal/browser"
	"github.com/lsegal/aviary/internal/channels"
	"github.com/lsegal/aviary/internal/store"
)

// ── Fake Chrome DevTools endpoint ────────────────────────────────────────────

// baCDP is a minimal in-process stand-in for Chrome's DevTools endpoint: the
// HTTP /json routes plus a browser WebSocket that answers the CDP commands the
// browser manager and chromedp send. It records what it was asked to do so
// tests can assert the side effects of each browser tool.
type baCDP struct {
	srv *httptest.Server

	mu          sync.Mutex
	tabs        []browser.ChromeTab
	nextTab     int
	closed      []string
	failNew     bool
	failAttach  map[string]bool   // tab IDs whose attach fails
	failMethods map[string]string // CDP method -> error message
	evalFn      func(expr string) (any, error)
	exprs       []string
	navigated   []string
	bounds      [][2]int64
	setValues   []string
	keys        []string
}

type baCDPMsg struct {
	ID        int64           `json:"id"`
	Method    string          `json:"method"`
	Params    json.RawMessage `json:"params"`
	SessionID string          `json:"sessionId,omitempty"`
}

func baNewCDP(t *testing.T) *baCDP {
	t.Helper()
	f := &baCDP{failAttach: map[string]bool{}, failMethods: map[string]string{}}
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"webSocketDebuggerUrl": "ws://" + r.Host + "/devtools/browser/fake",
		})
	})
	mux.HandleFunc("/json/list", func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		tabs := append([]browser.ChromeTab(nil), f.tabs...)
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(tabs)
	})
	mux.HandleFunc("/json/new", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.failNew {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		pageURL, _ := url.QueryUnescape(r.URL.RawQuery)
		f.nextTab++
		tab := browser.ChromeTab{ID: "new-" + strconv.Itoa(f.nextTab), Type: "page", URL: pageURL}
		f.tabs = append(f.tabs, tab)
		_ = json.NewEncoder(w).Encode(tab)
	})
	mux.HandleFunc("/json/close/", func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimPrefix(r.URL.Path, "/json/close/")
		f.mu.Lock()
		f.closed = append(f.closed, id)
		kept := f.tabs[:0]
		for _, tab := range f.tabs {
			if tab.ID != id {
				kept = append(kept, tab)
			}
		}
		f.tabs = kept
		f.mu.Unlock()
		_, _ = io.WriteString(w, "Target is closing")
	})
	mux.HandleFunc("/devtools/browser/fake", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close() //nolint:errcheck
		f.serveWS(conn)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *baCDP) port(t *testing.T) int {
	t.Helper()
	u, err := url.Parse(f.srv.URL)
	require.NoError(t, err)
	p, err := strconv.Atoi(u.Port())
	require.NoError(t, err)
	return p
}

func (f *baCDP) serveWS(conn *websocket.Conn) {
	send := func(v map[string]any) bool {
		return conn.WriteJSON(v) == nil
	}
	for {
		var msg baCDPMsg
		if err := conn.ReadJSON(&msg); err != nil {
			return
		}
		reply := func(result any) bool {
			out := map[string]any{"id": msg.ID, "result": result}
			if msg.SessionID != "" {
				out["sessionId"] = msg.SessionID
			}
			return send(out)
		}
		event := func(method string, params any) bool {
			return send(map[string]any{"method": method, "params": params, "sessionId": msg.SessionID})
		}
		f.mu.Lock()
		failMsg, fail := f.failMethods[msg.Method]
		f.mu.Unlock()
		if fail {
			out := map[string]any{"id": msg.ID, "error": map[string]any{"code": -32000, "message": failMsg}}
			if msg.SessionID != "" {
				out["sessionId"] = msg.SessionID
			}
			if !send(out) {
				return
			}
			continue
		}
		var params map[string]any
		_ = json.Unmarshal(msg.Params, &params)
		var ok bool
		switch msg.Method {
		case "Target.attachToTarget":
			id, _ := params["targetId"].(string)
			f.mu.Lock()
			bad := f.failAttach[id]
			f.mu.Unlock()
			if bad {
				ok = send(map[string]any{"id": msg.ID, "error": map[string]any{"code": -32602, "message": "No target with given id found"}})
				break
			}
			ok = reply(map[string]any{"sessionId": "S-" + id})
		case "Runtime.enable":
			ok = reply(map[string]any{}) && event("Runtime.executionContextCreated", map[string]any{
				"context": map[string]any{
					"id": 1, "origin": "", "name": "", "uniqueId": "u1",
					"auxData": map[string]any{"frameId": "F1", "isDefault": true, "type": "default"},
				},
			})
		case "Runtime.evaluate":
			expr, _ := params["expression"].(string)
			if expr == "self" {
				ok = reply(map[string]any{"result": map[string]any{"type": "object", "className": "Window", "objectId": "win"}})
				break
			}
			f.mu.Lock()
			f.exprs = append(f.exprs, expr)
			fn := f.evalFn
			f.mu.Unlock()
			var val any = "ok"
			var err error
			if fn != nil {
				val, err = fn(expr)
			}
			if err != nil {
				ok = send(map[string]any{"id": msg.ID, "sessionId": msg.SessionID, "error": map[string]any{"code": -32000, "message": err.Error()}})
				break
			}
			raw, _ := json.Marshal(val)
			typ := "object"
			if _, isStr := val.(string); isStr {
				typ = "string"
			}
			ok = reply(map[string]any{"result": map[string]any{"type": typ, "value": json.RawMessage(raw)}})
		case "Page.getFrameTree":
			ok = reply(map[string]any{"frameTree": map[string]any{"frame": map[string]any{
				"id": "F1", "loaderId": "L1", "url": "about:blank", "securityOrigin": "null", "mimeType": "text/html",
			}}})
		case "DOM.getDocument":
			ok = reply(map[string]any{"root": map[string]any{
				"nodeId": 1, "backendNodeId": 1, "nodeType": 9, "nodeName": "#document", "localName": "", "nodeValue": "",
				"childNodeCount": 1,
				"children": []any{map[string]any{
					"nodeId": 2, "parentId": 1, "backendNodeId": 2, "nodeType": 1, "nodeName": "INPUT",
					"localName": "input", "nodeValue": "", "attributes": []string{"id", "q"},
				}},
			}})
		case "DOM.querySelector":
			ok = reply(map[string]any{"nodeId": 2})
		case "DOM.getBoxModel":
			quad := []float64{0, 0, 10, 0, 10, 10, 0, 10}
			ok = reply(map[string]any{"model": map[string]any{
				"content": quad, "padding": quad, "border": quad, "margin": quad, "width": 10, "height": 10,
			}})
		case "DOM.resolveNode":
			ok = reply(map[string]any{"object": map[string]any{"type": "object", "objectId": "node-2"}})
		case "Runtime.callFunctionOn":
			args, _ := params["arguments"].([]any)
			if len(args) > 0 {
				last, _ := args[len(args)-1].(map[string]any)
				f.mu.Lock()
				f.setValues = append(f.setValues, fmt.Sprint(last["value"]))
				f.mu.Unlock()
				raw, _ := json.Marshal(last["value"])
				ok = reply(map[string]any{"result": map[string]any{"type": "string", "value": json.RawMessage(raw)}})
				break
			}
			ok = reply(map[string]any{"result": map[string]any{"type": "boolean", "value": true}})
		case "Input.dispatchKeyEvent":
			if txt, _ := params["text"].(string); txt != "" {
				f.mu.Lock()
				f.keys = append(f.keys, txt)
				f.mu.Unlock()
			}
			ok = reply(map[string]any{})
		case "Page.captureScreenshot":
			ok = reply(map[string]any{"data": base64.StdEncoding.EncodeToString([]byte("fake-image-bytes"))})
		case "Browser.getWindowForTarget":
			ok = reply(map[string]any{"windowId": 7, "bounds": map[string]any{"width": 800, "height": 600}})
		case "Browser.setWindowBounds":
			b, _ := params["bounds"].(map[string]any)
			w, _ := b["width"].(float64)
			h, _ := b["height"].(float64)
			f.mu.Lock()
			f.bounds = append(f.bounds, [2]int64{int64(w), int64(h)})
			f.mu.Unlock()
			ok = reply(map[string]any{})
		case "Page.navigate":
			target, _ := params["url"].(string)
			f.mu.Lock()
			f.navigated = append(f.navigated, target)
			f.mu.Unlock()
			if strings.Contains(target, "unreachable") {
				ok = reply(map[string]any{"frameId": "F1", "loaderId": "L2", "errorText": "net::ERR_NAME_NOT_RESOLVED"})
				break
			}
			ok = event("Page.lifecycleEvent", map[string]any{"frameId": "F1", "loaderId": "L2", "name": "init", "timestamp": 1}) &&
				reply(map[string]any{"frameId": "F1", "loaderId": "L2"}) &&
				event("Page.loadEventFired", map[string]any{"timestamp": 2})
		default:
			ok = reply(map[string]any{})
		}
		if !ok {
			return
		}
	}
}

// baSetupBrowser wires a browser manager pointed at a fresh fake DevTools
// endpoint into the global deps and returns an in-process dispatcher.
func baSetupBrowser(t *testing.T) (*Dispatcher, *baCDP) {
	t.Helper()
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	f := baNewCDP(t)
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{Browser: browser.NewManager("", f.port(t), t.TempDir(), true)})
	return NewDispatcher("https://localhost:16677", ""), f
}

// baDeadPort returns a loopback port nothing listens on.
func baDeadPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return p
}

func baCallErr(t *testing.T, d *Dispatcher, tool string, args map[string]any) string {
	t.Helper()
	out, err := d.CallTool(context.Background(), tool, args)
	if err != nil {
		return err.Error()
	}
	return out
}

// ── Browser tool tests ───────────────────────────────────────────────────────

func TestBrowserAuthOpenReusesTabAndCloseRemovesIt(t *testing.T) {
	d, f := baSetupBrowser(t)
	f.tabs = []browser.ChromeTab{{ID: "existing", Type: "page", URL: "https://example.com/"}}

	out, err := d.CallTool(context.Background(), "browser_tabs", nil)
	require.NoError(t, err)
	assert.Contains(t, out, `"id": "existing"`)

	// An exact URL match reuses the existing tab instead of creating one.
	out, err = d.CallTool(context.Background(), "browser_open", map[string]any{"url": "https://example.com"})
	require.NoError(t, err)
	assert.Contains(t, out, `"tab_id": "existing"`)

	// A new URL creates a tab through /json/new.
	out, err = d.CallTool(context.Background(), "browser_open", map[string]any{"url": "https://other.test/page"})
	require.NoError(t, err)
	var opened map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &opened))
	assert.Equal(t, "new-1", opened["tab_id"])
	assert.Equal(t, "https://other.test/page", opened["url"])

	out, err = d.CallTool(context.Background(), "browser_close", map[string]any{"tab_id": "new-1"})
	require.NoError(t, err)
	assert.Contains(t, out, `"closed": true`)
	assert.Equal(t, []string{"new-1"}, f.closed)
	assert.Len(t, f.tabs, 1)
}

func TestBrowserAuthOpenFailsWhenTabCreationFails(t *testing.T) {
	d, f := baSetupBrowser(t)
	f.failNew = true
	got := baCallErr(t, d, "browser_open", map[string]any{"url": "https://new.test"})
	assert.Contains(t, got, "creating tab")
}

func TestBrowserAuthTabsAndCloseFailWithoutChrome(t *testing.T) {
	store.SetDataDir(t.TempDir())
	t.Cleanup(func() { store.SetDataDir("") })
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{Browser: browser.NewManager("", baDeadPort(t), t.TempDir(), true)})
	d := NewDispatcher("https://localhost:16677", "")

	assert.Contains(t, baCallErr(t, d, "browser_tabs", nil), "chrome not running")
	got := baCallErr(t, d, "browser_close", map[string]any{"tab_id": "abc"})
	assert.Contains(t, got, "connect")
}

func TestBrowserAuthEmptyTabIDRejected(t *testing.T) {
	d, f := baSetupBrowser(t)
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"browser_navigate", map[string]any{"tab_id": "", "url": "https://x.test"}},
		{"browser_wait", map[string]any{"tab_id": "", "selector": "#a"}},
		{"browser_click", map[string]any{"tab_id": "", "selector": "#a"}},
		{"browser_keystroke", map[string]any{"tab_id": "", "selector": "#a", "text": "x"}},
		{"browser_fill", map[string]any{"tab_id": "", "selector": "#a", "text": "x"}},
		{"browser_text", map[string]any{"tab_id": ""}},
		{"browser_query", map[string]any{"tab_id": "", "selector": "a"}},
		{"browser_screenshot", map[string]any{"tab_id": ""}},
		{"browser_resize", map[string]any{"tab_id": "", "width": 10, "height": 10}},
		{"browser_eval", map[string]any{"tab_id": "", "javascript": "1"}},
		{"browser_close", map[string]any{"tab_id": ""}},
	}
	for _, tc := range cases {
		assert.Contains(t, baCallErr(t, d, tc.tool, tc.args), "tab_id is required", tc.tool)
	}
	assert.Contains(t, baCallErr(t, d, "browser_navigate", map[string]any{"tab_id": "t", "url": ""}), "url is required")
	assert.Contains(t, baCallErr(t, d, "browser_wait", map[string]any{"tab_id": "t", "selector": ""}), "selector is required")
	assert.Contains(t, baCallErr(t, d, "browser_query", map[string]any{"tab_id": "t", "selector": ""}), "selector is required")
	assert.Contains(t, baCallErr(t, d, "browser_resize", map[string]any{"tab_id": "t", "width": 0, "height": 10}), "must be positive")
	// None of the rejected calls reached the browser.
	assert.Empty(t, f.exprs)
	assert.Empty(t, f.closed)
}

func TestBrowserAuthTabOperationsFailWhenAttachFails(t *testing.T) {
	d, f := baSetupBrowser(t)
	f.failAttach["gone"] = true
	cases := []struct {
		tool string
		args map[string]any
	}{
		{"browser_navigate", map[string]any{"tab_id": "gone", "url": "https://x.test"}},
		{"browser_wait", map[string]any{"tab_id": "gone", "selector": "#a"}},
		{"browser_click", map[string]any{"tab_id": "gone", "selector": "#a"}},
		{"browser_keystroke", map[string]any{"tab_id": "gone", "selector": "#a", "text": "x"}},
		{"browser_fill", map[string]any{"tab_id": "gone", "selector": "#a", "text": "x"}},
		{"browser_text", map[string]any{"tab_id": "gone"}},
		{"browser_query", map[string]any{"tab_id": "gone", "selector": "a"}},
		{"browser_screenshot", map[string]any{"tab_id": "gone"}},
		{"browser_resize", map[string]any{"tab_id": "gone", "width": 10, "height": 10}},
		{"browser_eval", map[string]any{"tab_id": "gone", "javascript": "1"}},
	}
	for _, tc := range cases {
		assert.Contains(t, baCallErr(t, d, tc.tool, tc.args), "attaching to tab gone", tc.tool)
	}
}

func TestBrowserAuthNavigateWaitsForLoadAndReportsLoadErrors(t *testing.T) {
	d, f := baSetupBrowser(t)
	out, err := d.CallTool(context.Background(), "browser_navigate", map[string]any{"tab_id": "t1", "url": "https://ok.test/"})
	require.NoError(t, err)
	assert.Contains(t, out, `"url": "https://ok.test/"`)
	assert.Contains(t, out, `"tab_id": "t1"`)

	got := baCallErr(t, d, "browser_navigate", map[string]any{"tab_id": "t1", "url": "https://unreachable.test/"})
	assert.Contains(t, got, "ERR_NAME_NOT_RESOLVED")
	assert.Equal(t, []string{"https://ok.test/", "https://unreachable.test/"}, f.navigated)
}

func TestBrowserAuthWaitClampsTimeout(t *testing.T) {
	d, _ := baSetupBrowser(t)
	out, err := d.CallTool(context.Background(), "browser_wait", map[string]any{"tab_id": "t1", "selector": "#q"})
	require.NoError(t, err)
	assert.Contains(t, out, `"status": "visible"`)
	assert.Contains(t, out, `"timeout_ms": 10000`)

	out, err = d.CallTool(context.Background(), "browser_wait", map[string]any{"tab_id": "t1", "selector": "#q", "timeout_ms": 999999})
	require.NoError(t, err)
	assert.Contains(t, out, `"timeout_ms": 60000`)
}

func TestBrowserAuthClickReportsMissingElement(t *testing.T) {
	d, f := baSetupBrowser(t)
	f.evalFn = func(expr string) (any, error) {
		if strings.Contains(expr, `"#missing"`) {
			return "not_found", nil
		}
		return "clicked", nil
	}
	out, err := d.CallTool(context.Background(), "browser_click", map[string]any{"tab_id": "t1", "selector": "#go"})
	require.NoError(t, err)
	assert.Equal(t, `clicked "#go" in tab t1`, out)

	got := baCallErr(t, d, "browser_click", map[string]any{"tab_id": "t1", "selector": "#missing"})
	assert.Contains(t, got, `no visible element matched selector "#missing"`)
}

func TestBrowserAuthKeystrokeAndFillReachThePage(t *testing.T) {
	d, f := baSetupBrowser(t)
	out, err := d.CallTool(context.Background(), "browser_keystroke", map[string]any{"tab_id": "t1", "selector": "#q", "text": "hi"})
	require.NoError(t, err)
	assert.Equal(t, `keystrokes sent to "#q" in tab t1`, out)
	assert.Equal(t, []string{"h", "i"}, f.keys)

	out, err = d.CallTool(context.Background(), "browser_fill", map[string]any{"tab_id": "t1", "selector": "#q", "text": "filled value"})
	require.NoError(t, err)
	assert.Equal(t, `filled "#q" in tab t1`, out)
	assert.Contains(t, f.setValues, "filled value")
}

func TestBrowserAuthTextBuildsExpressionAndParsesPayload(t *testing.T) {
	d, f := baSetupBrowser(t)
	f.evalFn = func(string) (any, error) {
		return `{"tab_id":"t1","title":"Home","match_count":2,"text":"hello world"}`, nil
	}
	out, err := d.CallTool(context.Background(), "browser_text", map[string]any{"tab_id": "t1", "selector": ".msg"})
	require.NoError(t, err)
	assert.Contains(t, out, `"text": "hello world"`)
	assert.Contains(t, out, `"match_count": 2`)
	require.NotEmpty(t, f.exprs)
	last := f.exprs[len(f.exprs)-1]
	assert.Contains(t, last, `const selector = ".msg";`)
	assert.Contains(t, last, ".slice(0, 4000)")

	_, err = d.CallTool(context.Background(), "browser_text", map[string]any{"tab_id": "t1", "max_length": 50000})
	require.NoError(t, err)
	assert.Contains(t, f.exprs[len(f.exprs)-1], ".slice(0, 20000)")

	f.evalFn = func(string) (any, error) { return "not json", nil }
	assert.Contains(t, baCallErr(t, d, "browser_text", map[string]any{"tab_id": "t1"}), "parsing browser_text result")

	f.failMethods["Runtime.evaluate"] = "page crashed"
	assert.Contains(t, baCallErr(t, d, "browser_text", map[string]any{"tab_id": "t1"}), "page crashed")
}

func TestBrowserAuthQueryClampsLimitsAndParsesPayload(t *testing.T) {
	d, f := baSetupBrowser(t)
	f.evalFn = func(string) (any, error) {
		return `{"tab_id":"t1","count":1,"items":[{"index":0,"tag_name":"a","href":"https://x.test"}]}`, nil
	}
	out, err := d.CallTool(context.Background(), "browser_query", map[string]any{"tab_id": "t1", "selector": "a"})
	require.NoError(t, err)
	assert.Contains(t, out, `"href": "https://x.test"`)
	last := f.exprs[len(f.exprs)-1]
	assert.Contains(t, last, ".slice(0, 20)")
	assert.Contains(t, last, ".slice(0, 500)")
	assert.Contains(t, last, "html: false ?")

	_, err = d.CallTool(context.Background(), "browser_query", map[string]any{
		"tab_id": "t1", "selector": "a", "count": 1000, "max_text_length": 99999, "include_html": true,
	})
	require.NoError(t, err)
	last = f.exprs[len(f.exprs)-1]
	assert.Contains(t, last, ".slice(0, 100)")
	assert.Contains(t, last, ".slice(0, 5000)")
	assert.Contains(t, last, "html: true ?")

	f.evalFn = func(string) (any, error) { return "{broken", nil }
	assert.Contains(t, baCallErr(t, d, "browser_query", map[string]any{"tab_id": "t1", "selector": "a"}), "parsing browser_query result")

	f.failMethods["Runtime.evaluate"] = "query exploded"
	assert.Contains(t, baCallErr(t, d, "browser_query", map[string]any{"tab_id": "t1", "selector": "a"}), "query exploded")
}

func TestBrowserAuthScreenshotSavesFile(t *testing.T) {
	d, f := baSetupBrowser(t)
	out, err := d.CallTool(context.Background(), "browser_screenshot", map[string]any{"tab_id": "t1"})
	require.NoError(t, err)
	require.True(t, strings.HasPrefix(out, "screenshot saved: "), out)
	path := strings.TrimPrefix(out, "screenshot saved: ")
	assert.Equal(t, store.BrowserMediaDir(), filepath.Dir(path))
	assert.True(t, strings.HasPrefix(filepath.Base(path), "screenshot_t1_"))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "fake-image-bytes", string(data))

	f.failMethods["Page.captureScreenshot"] = "capture refused"
	assert.Contains(t, baCallErr(t, d, "browser_screenshot", map[string]any{"tab_id": "t1"}), "capture refused")
}

func TestBrowserAuthScreenshotFailsWhenMediaDirBlocked(t *testing.T) {
	d, _ := baSetupBrowser(t)
	// A regular file where the browser media directory should be.
	require.NoError(t, os.MkdirAll(filepath.Dir(store.BrowserMediaDir()), 0o700))
	require.NoError(t, os.WriteFile(store.BrowserMediaDir(), []byte("x"), 0o600))
	got := baCallErr(t, d, "browser_screenshot", map[string]any{"tab_id": "t1"})
	assert.Contains(t, got, "creating screenshot directory")
}

func TestBrowserAuthResizeSetsWindowBounds(t *testing.T) {
	d, f := baSetupBrowser(t)
	out, err := d.CallTool(context.Background(), "browser_resize", map[string]any{"tab_id": "t1", "width": 1280, "height": 720})
	require.NoError(t, err)
	assert.Equal(t, "resized tab t1 to 1280x720", out)
	assert.Equal(t, [][2]int64{{1280, 720}}, f.bounds)

	f.failMethods["Browser.setWindowBounds"] = "bounds rejected"
	assert.Contains(t, baCallErr(t, d, "browser_resize", map[string]any{"tab_id": "t1", "width": 10, "height": 10}), "bounds rejected")
}

func TestBrowserAuthEvalReturnsResult(t *testing.T) {
	d, f := baSetupBrowser(t)
	f.evalFn = func(expr string) (any, error) {
		if expr == "document.title" {
			return "My Page", nil
		}
		return map[string]any{"n": 3}, nil
	}
	out, err := d.CallTool(context.Background(), "browser_eval", map[string]any{"tab_id": "t1", "javascript": "document.title"})
	require.NoError(t, err)
	assert.Equal(t, "My Page", out)

	out, err = d.CallTool(context.Background(), "browser_eval", map[string]any{"tab_id": "t1", "javascript": "({n: 3})"})
	require.NoError(t, err)
	assert.Equal(t, `{"n":3}`, out)

	f.failMethods["Runtime.evaluate"] = "ReferenceError"
	assert.Contains(t, baCallErr(t, d, "browser_eval", map[string]any{"tab_id": "t1", "javascript": "nope"}), "ReferenceError")
}

func TestBrowserAuthChannelSendFileUsesRegisteredMediaDelivery(t *testing.T) {
	d, _ := baSetupBrowser(t)
	agentID, sessionID := "ba-agent", "ba-session"
	var gotCaption, gotPath string
	agent.RegisterSessionMediaDelivery(agentID, sessionID, "slack", "C1", func(caption, path string) {
		gotCaption, gotPath = caption, path
	})
	t.Cleanup(func() { agent.ClearSessionDelivery(agentID, sessionID) })

	ctx := agent.WithSessionAgentID(agent.WithSessionID(context.Background(), sessionID), agentID)
	out, err := d.CallTool(ctx, "channel_send_file", map[string]any{"file_path": "/some/shot.png", "caption": "look"})
	require.NoError(t, err)
	assert.Equal(t, "file sent: /some/shot.png", out)
	assert.Equal(t, "look", gotCaption)
	assert.Equal(t, "/some/shot.png", gotPath)
}

func TestBrowserAuthChannelSendFileErrors(t *testing.T) {
	d, _ := baSetupBrowser(t)
	// Session without an agent.
	ctx := agent.WithSessionID(context.Background(), "s1")
	out, err := d.CallTool(ctx, "channel_send_file", map[string]any{"file_path": "/x.png"})
	if err == nil {
		assert.Contains(t, out, "no active session agent")
	} else {
		assert.Contains(t, err.Error(), "no active session agent")
	}

	// No media delivery registered and the file does not exist.
	ctx = agent.WithSessionAgentID(agent.WithSessionID(context.Background(), "s2"), "ba-nobody")
	missing := filepath.Join(t.TempDir(), "missing.png")
	out, err = d.CallTool(ctx, "channel_send_file", map[string]any{"file_path": missing})
	if err == nil {
		assert.Contains(t, out, "missing.png")
	} else {
		assert.Contains(t, err.Error(), "missing.png")
	}
}

// ── Auth tool tests ──────────────────────────────────────────────────────────

// baRoundTrip routes HTTP requests made through http.DefaultClient to an
// in-test handler keyed by host, so OAuth flows never touch the network.
type baRoundTrip func(*http.Request) (*http.Response, error)

func (f baRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func baJSONResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func baFakeHTTP(t *testing.T, fn func(*http.Request) *http.Response) {
	t.Helper()
	orig := http.DefaultClient
	t.Cleanup(func() { http.DefaultClient = orig })
	http.DefaultClient = &http.Client{Transport: baRoundTrip(func(r *http.Request) (*http.Response, error) {
		if resp := fn(r); resp != nil {
			return resp, nil
		}
		return nil, fmt.Errorf("unexpected request to %s", r.URL)
	})}
}

// baSetupAuth wires a temp-dir FileStore into the deps and makes sure no real
// browser can be opened by the login tools (an empty PATH).
func baSetupAuth(t *testing.T) (*Dispatcher, *auth.FileStore) {
	t.Helper()
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)
	t.Setenv("PATH", t.TempDir())
	store.SetDataDir(filepath.Join(base, "aviary"))
	t.Cleanup(func() { store.SetDataDir("") })
	st, err := auth.NewFileStore(filepath.Join(base, "aviary", "auth", "credentials.json"))
	require.NoError(t, err)
	old := GetDeps()
	t.Cleanup(func() { SetDeps(old) })
	prevChecker := checkServerRunning
	t.Cleanup(func() { checkServerRunning = prevChecker })
	SetServerChecker(func() bool { return false })
	SetDeps(&Deps{Agents: agent.NewManager(nil), Auth: st})
	return NewDispatcher("https://localhost:16677", ""), st
}

// baBreakStore makes every write to st fail by replacing its directory with a
// regular file.
func baBreakStore(t *testing.T, st *auth.FileStore) {
	t.Helper()
	dir := filepath.Join(store.DataDir(), "auth")
	require.NoError(t, os.RemoveAll(dir))
	require.NoError(t, os.MkdirAll(filepath.Dir(dir), 0o700))
	require.NoError(t, os.WriteFile(dir, []byte("not a dir"), 0o600))
	assert.Error(t, st.Set("probe", "x"))
}

// baAssertStoreBlocked checks that msg reports the failure to create the auth
// directory that baBreakStore replaced with a file. The OS wording differs
// ("not a directory" on Unix, "cannot find the path" on Windows), so it matches
// the failing operation and path instead.
func baAssertStoreBlocked(t *testing.T, msg string) {
	t.Helper()
	assert.Contains(t, msg, "mkdir ")
	assert.Contains(t, msg, filepath.Join(filepath.Base(store.DataDir()), "auth")+":")
}

func TestBrowserAuthFallbackStoreUsesDataDir(t *testing.T) {
	d, _ := baSetupAuth(t)
	SetDeps(&Deps{}) // no injected store: tools open the one under the data dir

	out, err := d.CallTool(context.Background(), "auth_set", map[string]any{"name": "ba:key", "value": "secret-value"})
	require.NoError(t, err)
	assert.Contains(t, out, `credential "ba:key" stored`)

	data, err := os.ReadFile(filepath.Join(store.SubDir(store.DirAuth), "credentials.json"))
	require.NoError(t, err)
	assert.Contains(t, string(data), "secret-value")

	out, err = d.CallTool(context.Background(), "auth_get", map[string]any{"name": "ba:key"})
	require.NoError(t, err)
	assert.Contains(t, out, `"preview": "secr********"`)
}

func TestBrowserAuthCorruptFallbackStoreFailsEveryTool(t *testing.T) {
	d, _ := baSetupAuth(t)
	SetDeps(&Deps{})
	authDir := store.SubDir(store.DirAuth)
	require.NoError(t, os.MkdirAll(authDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(authDir, "credentials.json"), []byte("{not json"), 0o600))

	cases := []struct {
		tool string
		args map[string]any
	}{
		{"auth_set", map[string]any{"name": "a", "value": "b"}},
		{"auth_get", map[string]any{"name": "a"}},
		{"auth_list", map[string]any{}},
		{"auth_delete", map[string]any{"name": "a"}},
		{"slack_channels_list", map[string]any{"bot_token": "auth:slack"}},
	}
	for _, tc := range cases {
		assert.Contains(t, baCallErr(t, d, tc.tool, tc.args), "invalid character", tc.tool)
	}
}

func TestBrowserAuthStoreWriteAndLookupErrors(t *testing.T) {
	d, st := baSetupAuth(t)
	assert.Contains(t, baCallErr(t, d, "auth_delete", map[string]any{"name": "ba:absent"}), "ba:absent")
	assert.Contains(t, baCallErr(t, d, "auth_get", map[string]any{"name": "ba:absent"}), "ba:absent")

	require.NoError(t, st.Set("ba:present", "v"))
	baBreakStore(t, st)
	baAssertStoreBlocked(t, baCallErr(t, d, "auth_set", map[string]any{"name": "ba:new", "value": "v"}))
	got := baCallErr(t, d, "auth_delete", map[string]any{"name": "ba:present"})
	baAssertStoreBlocked(t, got)
}

func TestBrowserAuthSlackChannelsListErrors(t *testing.T) {
	d, _ := baSetupAuth(t)
	prev := listSlackWorkspaceChannels
	t.Cleanup(func() { listSlackWorkspaceChannels = prev })
	called := false
	listSlackWorkspaceChannels = func(context.Context, string) (*channels.SlackWorkspaceInfo, error) {
		called = true
		return nil, fmt.Errorf("invalid_auth")
	}
	// An auth: reference to a missing credential fails before Slack is called.
	assert.Contains(t, baCallErr(t, d, "slack_channels_list", map[string]any{"bot_token": "auth:ba:missing"}), "auth:ba:missing")
	assert.False(t, called)

	assert.Contains(t, baCallErr(t, d, "slack_channels_list", map[string]any{"bot_token": "xoxb-bad"}), "invalid_auth")
	assert.True(t, called)
}

func TestBrowserAuthAnthropicLoginRoundTrip(t *testing.T) {
	d, st := baSetupAuth(t)
	var tokenBody map[string]any
	baFakeHTTP(t, func(r *http.Request) *http.Response {
		if r.URL.Host != "platform.claude.com" {
			return nil
		}
		_ = json.NewDecoder(r.Body).Decode(&tokenBody)
		return baJSONResponse(http.StatusOK, `{"access_token":"ant-access","refresh_token":"ant-refresh","expires_in":3600}`)
	})

	out, err := d.CallTool(context.Background(), "auth_login_anthropic", map[string]any{})
	require.NoError(t, err)
	var start map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &start))
	authURL, _ := start["url"].(string)
	assert.True(t, strings.HasPrefix(authURL, auth.AnthropicAuthorizeURL), authURL)
	assert.Contains(t, authURL, "code_challenge=")
	assert.Contains(t, start["instructions"], "auth_login_anthropic_complete")

	out, err = d.CallTool(context.Background(), "auth_login_anthropic_complete", map[string]any{"code": "the-code#the-state"})
	require.NoError(t, err)
	assert.Contains(t, out, "Anthropic OAuth login successful")
	assert.Equal(t, "the-code", tokenBody["code"])
	assert.Equal(t, "the-state", tokenBody["state"])
	assert.NotEmpty(t, tokenBody["code_verifier"])

	stored, err := st.Get("anthropic:oauth")
	require.NoError(t, err)
	assert.Contains(t, stored, "ant-access")
	assert.Contains(t, stored, "ant-refresh")
}

func TestBrowserAuthAnthropicCompleteFailures(t *testing.T) {
	d, st := baSetupAuth(t)
	status := http.StatusBadRequest
	baFakeHTTP(t, func(r *http.Request) *http.Response {
		if r.URL.Host != "platform.claude.com" {
			return nil
		}
		return baJSONResponse(status, `{"access_token":"x","expires_in":60}`)
	})

	_, err := d.CallTool(context.Background(), "auth_login_anthropic", map[string]any{})
	require.NoError(t, err)
	got := baCallErr(t, d, "auth_login_anthropic_complete", map[string]any{"code": "bad"})
	assert.Contains(t, got, "completing Anthropic login")
	assert.Contains(t, got, "400")

	// A successful exchange whose token cannot be persisted reports the store error.
	status = http.StatusOK
	_, err = d.CallTool(context.Background(), "auth_login_anthropic", map[string]any{})
	require.NoError(t, err)
	baBreakStore(t, st)
	got = baCallErr(t, d, "auth_login_anthropic_complete", map[string]any{"code": "good"})
	baAssertStoreBlocked(t, got)
}

// baRequirePortFree skips the test when a fixed OAuth callback port is taken
// by something outside the test.
func baRequirePortFree(t *testing.T, port int) {
	t.Helper()
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		t.Skipf("callback port %d unavailable: %v", port, err)
	}
	require.NoError(t, ln.Close())
}

// baHitCallback delivers an OAuth redirect to the local callback listener the
// way the user's browser would.
func baHitCallback(t *testing.T, callbackURL, authorizeURL string, extra url.Values) {
	t.Helper()
	au, err := url.Parse(authorizeURL)
	require.NoError(t, err)
	cb, err := url.Parse(callbackURL)
	require.NoError(t, err)
	cb.Host = strings.Replace(cb.Host, "localhost", "127.0.0.1", 1)
	q := url.Values{"state": {au.Query().Get("state")}}
	for k, v := range extra {
		q[k] = v
	}
	cb.RawQuery = q.Encode()
	client := &http.Client{Transport: &http.Transport{}, Timeout: 10 * time.Second}
	resp, err := client.Get(cb.String())
	require.NoError(t, err)
	_ = resp.Body.Close()
}

func baStartBrowserLogin(t *testing.T, d *Dispatcher, tool string) map[string]any {
	t.Helper()
	out, err := d.CallTool(context.Background(), tool, map[string]any{})
	require.NoError(t, err)
	var start map[string]any
	require.NoError(t, json.Unmarshal([]byte(out), &start), out)
	assert.Equal(t, false, start["browser_opened"])
	assert.NotEmpty(t, start["browser_open_error"])
	assert.EqualValues(t, int(auth.BrowserLoginTimeout/time.Second), start["timeout_seconds"])
	_, err = time.Parse(time.RFC3339, fmt.Sprint(start["expires_at"]))
	assert.NoError(t, err)
	return start
}

func TestBrowserAuthGeminiLoginRoundTrip(t *testing.T) {
	baRequirePortFree(t, auth.GeminiCallbackPort)
	d, st := baSetupAuth(t)
	var exchanged url.Values
	baFakeHTTP(t, func(r *http.Request) *http.Response {
		if r.URL.Host != "oauth2.googleapis.com" {
			return nil
		}
		body, _ := io.ReadAll(r.Body)
		exchanged, _ = url.ParseQuery(string(body))
		return baJSONResponse(http.StatusOK, `{"access_token":"gem-access","refresh_token":"gem-refresh","expires_in":3600}`)
	})

	start := baStartBrowserLogin(t, d, "auth_login_gemini")
	assert.Equal(t, fmt.Sprintf("http://localhost:%d", auth.GeminiCallbackPort), start["callback_url"])
	baHitCallback(t, start["callback_url"].(string)+"/", start["url"].(string), url.Values{"code": {"gem-code"}})

	out, err := d.CallTool(context.Background(), "auth_login_gemini_complete", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, "Gemini OAuth login successful")
	assert.Equal(t, "gem-code", exchanged.Get("code"))
	stored, err := st.Get("gemini:oauth")
	require.NoError(t, err)
	assert.Contains(t, stored, "gem-access")

	// The flow is consumed: completing again has nothing pending.
	assert.Contains(t, baCallErr(t, d, "auth_login_gemini_complete", map[string]any{}), "no pending Gemini login")
}

func TestBrowserAuthGeminiLoginFailures(t *testing.T) {
	baRequirePortFree(t, auth.GeminiCallbackPort)
	d, st := baSetupAuth(t)
	baFakeHTTP(t, func(r *http.Request) *http.Response {
		if r.URL.Host != "oauth2.googleapis.com" {
			return nil
		}
		return baJSONResponse(http.StatusOK, `{"access_token":"gem-access","expires_in":60}`)
	})

	// The provider redirects back with an error.
	start := baStartBrowserLogin(t, d, "auth_login_gemini")
	baHitCallback(t, start["callback_url"].(string)+"/", start["url"].(string), url.Values{"error": {"access_denied"}})
	got := baCallErr(t, d, "auth_login_gemini_complete", map[string]any{})
	assert.Contains(t, got, "gemini login")
	assert.Contains(t, got, "access_denied")

	// The exchange succeeds but the token cannot be stored.
	start = baStartBrowserLogin(t, d, "auth_login_gemini")
	baBreakStore(t, st)
	baHitCallback(t, start["callback_url"].(string)+"/", start["url"].(string), url.Values{"code": {"c"}})
	baAssertStoreBlocked(t, baCallErr(t, d, "auth_login_gemini_complete", map[string]any{}))

	// The callback port is already taken, so the login cannot start.
	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", auth.GeminiCallbackPort))
	require.NoError(t, err)
	defer ln.Close() //nolint:errcheck
	got = baCallErr(t, d, "auth_login_gemini", map[string]any{})
	assert.Contains(t, got, "gemini login")
	assert.Contains(t, got, strconv.Itoa(auth.GeminiCallbackPort))
}

func TestBrowserAuthOpenAILoginRoundTrip(t *testing.T) {
	baRequirePortFree(t, auth.OpenAICallbackPort)
	d, st := baSetupAuth(t)
	var exchanged url.Values
	baFakeHTTP(t, func(r *http.Request) *http.Response {
		if r.URL.Host != "auth.openai.com" {
			return nil
		}
		body, _ := io.ReadAll(r.Body)
		exchanged, _ = url.ParseQuery(string(body))
		return baJSONResponse(http.StatusOK, `{"access_token":"oai-access","refresh_token":"oai-refresh","expires_in":3600}`)
	})

	start := baStartBrowserLogin(t, d, "auth_login_openai")
	assert.Equal(t, fmt.Sprintf("http://localhost:%d/auth/callback", auth.OpenAICallbackPort), start["callback_url"])
	baHitCallback(t, start["callback_url"].(string), start["url"].(string), url.Values{"code": {"oai-code"}})

	out, err := d.CallTool(context.Background(), "auth_login_openai_complete", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, "OpenAI OAuth login successful")
	assert.Equal(t, "oai-code", exchanged.Get("code"))
	stored, err := st.Get("openai:oauth")
	require.NoError(t, err)
	assert.Contains(t, stored, "oai-access")

	assert.Contains(t, baCallErr(t, d, "auth_login_openai_complete", map[string]any{}), "no pending OpenAI login")
}

func TestBrowserAuthOpenAILoginFailures(t *testing.T) {
	baRequirePortFree(t, auth.OpenAICallbackPort)
	d, st := baSetupAuth(t)
	baFakeHTTP(t, func(r *http.Request) *http.Response {
		if r.URL.Host != "auth.openai.com" {
			return nil
		}
		return baJSONResponse(http.StatusOK, `{"access_token":"oai","expires_in":60}`)
	})

	start := baStartBrowserLogin(t, d, "auth_login_openai")
	baHitCallback(t, start["callback_url"].(string), start["url"].(string), url.Values{"error": {"denied_by_user"}})
	got := baCallErr(t, d, "auth_login_openai_complete", map[string]any{})
	assert.Contains(t, got, "OpenAI login")
	assert.Contains(t, got, "denied_by_user")

	start = baStartBrowserLogin(t, d, "auth_login_openai")
	baBreakStore(t, st)
	baHitCallback(t, start["callback_url"].(string), start["url"].(string), url.Values{"code": {"c"}})
	baAssertStoreBlocked(t, baCallErr(t, d, "auth_login_openai_complete", map[string]any{}))

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", auth.OpenAICallbackPort))
	require.NoError(t, err)
	defer ln.Close() //nolint:errcheck
	got = baCallErr(t, d, "auth_login_openai", map[string]any{})
	assert.Contains(t, got, "OpenAI login")
	assert.Contains(t, got, strconv.Itoa(auth.OpenAICallbackPort))
}

// baCopilotServer fakes GitHub's device-code and token endpoints.
func baCopilotServer(t *testing.T, deviceBody string, pollBodies ...string) *int {
	t.Helper()
	polls := 0
	var mu sync.Mutex
	baFakeHTTP(t, func(r *http.Request) *http.Response {
		if r.URL.Host != "github.com" {
			return nil
		}
		switch r.URL.Path {
		case "/login/device/code":
			return baJSONResponse(http.StatusOK, deviceBody)
		case "/login/oauth/access_token":
			mu.Lock()
			defer mu.Unlock()
			body := pollBodies[min(polls, len(pollBodies)-1)]
			polls++
			return baJSONResponse(http.StatusOK, body)
		}
		return nil
	})
	return &polls
}

func TestBrowserAuthCopilotDeviceLoginRoundTrip(t *testing.T) {
	d, st := baSetupAuth(t)
	polls := baCopilotServer(t,
		`{"device_code":"dev-1","user_code":"ABCD-1234","verification_uri":"https://github.com/login/device","expires_in":60,"interval":1}`,
		`{"error":"authorization_pending"}`, `{"access_token":"gho_token"}`)

	out, err := d.CallTool(context.Background(), "auth_login_github_copilot", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, `"user_code": "ABCD-1234"`)
	assert.Contains(t, out, `"verification_uri": "https://github.com/login/device"`)

	out, err = d.CallTool(context.Background(), "auth_login_github_copilot_complete", map[string]any{})
	require.NoError(t, err)
	assert.Contains(t, out, "GitHub Copilot login successful")
	assert.Equal(t, 2, *polls)
	stored, err := st.Get("github-copilot:oauth")
	require.NoError(t, err)
	assert.Contains(t, stored, "gho_token")

	// The device state was consumed.
	assert.Contains(t, baCallErr(t, d, "auth_login_github_copilot_complete", map[string]any{}), "no pending Copilot login")
}

func TestBrowserAuthCopilotDeviceLoginFailures(t *testing.T) {
	d, st := baSetupAuth(t)
	baCopilotServer(t, `{"user_code":"X"}`, `{}`)
	got := baCallErr(t, d, "auth_login_github_copilot", map[string]any{})
	assert.Contains(t, got, "copilot device code")
	assert.Contains(t, got, "empty device_code")

	device := `{"device_code":"dev","user_code":"U","verification_uri":"https://github.com/login/device","expires_in":60,"interval":1}`
	baCopilotServer(t, device, `{"error":"expired_token"}`)
	_, err := d.CallTool(context.Background(), "auth_login_github_copilot", map[string]any{})
	require.NoError(t, err)
	got = baCallErr(t, d, "auth_login_github_copilot_complete", map[string]any{})
	assert.Contains(t, got, "copilot device poll")
	assert.Contains(t, got, "expired")

	baCopilotServer(t, device, `{"access_token":"gho_ok"}`)
	_, err = d.CallTool(context.Background(), "auth_login_github_copilot", map[string]any{})
	require.NoError(t, err)
	baBreakStore(t, st)
	baAssertStoreBlocked(t, baCallErr(t, d, "auth_login_github_copilot_complete", map[string]any{}))
}
