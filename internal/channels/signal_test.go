package channels

import (
    "context"
    "io/ioutil"
    "net/http"
    "net/http/httptest"
    "os"
    "strings"
    "testing"

    "github.com/lsegal/aviary/internal/config"
)

// Test that the object-replacement character is substituted with AgentName
// (or phone fallback) for both message and quoted text.
func TestDispatchEnvelopeAgentNameReplacement(t *testing.T) {
    sc := &SignalChannel{
        phone:    "+15550000",
        AgentName: "Alice",
        done:     make(chan struct{}),
    }

    // allowFrom entry that permits any DM sender
    sc.allowFrom = []config.AllowFromEntry{{From: "*"}}

    got := make(chan string, 1)
    sc.OnMessage(func(im IncomingMessage) {
        got <- im.Text
    })

    dm := &signalDataMessage{Message: "\uFFFC hello"}
    sc.dispatchEnvelope("+1555123", 0, false, false, dm)

    select {
    case text := <-got:
        if !strings.Contains(text, "Alice") {
            t.Fatalf("expected AgentName replacement, got %q", text)
        }
    default:
        t.Fatal("no message dispatched")
    }

    // blank AgentName falls back to phone
    sc2 := &SignalChannel{phone: "+19990000", done: make(chan struct{})}
    sc2.allowFrom = []config.AllowFromEntry{{From: "*"}}
    got2 := make(chan string, 1)
    sc2.OnMessage(func(im IncomingMessage) { got2 <- im.Text })
    dm2 := &signalDataMessage{Message: "\uFFFC bye"}
    sc2.dispatchEnvelope("+1555123", 0, false, false, dm2)
    select {
    case text := <-got2:
        if !strings.Contains(text, "+19990000") {
            t.Fatalf("expected phone fallback replacement, got %q", text)
        }
    default:
        t.Fatal("no message dispatched for phone fallback")
    }
}

// Test fetchLinkPreviews and downloadTempImage using an httptest server.
func TestFetchLinkPreviewsAndDownloadTempImage(t *testing.T) {
    // Start a server that serves an HTML page and an image.
    ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if strings.HasSuffix(r.URL.Path, "/img.jpg") {
            w.Header().Set("Content-Type", "image/jpeg")
            w.Write([]byte("JPEGDATA"))
            return
        }
        // HTML page with title and og:image
        w.Header().Set("Content-Type", "text/html")
        w.Write([]byte(`<!doctype html><html><head><title>My Title</title><meta property="og:image" content="` + ts.URL + `/img.jpg"></head><body></body></html>`))
    }))
    defer ts.Close()

    previews, cleanup := fetchLinkPreviews("check " + ts.URL + "/")
    if cleanup != nil {
        defer cleanup()
    }
    if previews == nil || len(previews) == 0 {
        t.Fatal("expected a preview")
    }
    p := previews[0]
    if p.Title != "My Title" {
        t.Fatalf("unexpected title: %q", p.Title)
    }
    if p.Image == "" {
        t.Fatal("expected an image path")
    }
    // ensure file exists
    if _, err := os.Stat(p.Image); err != nil {
        t.Fatalf("preview image not found: %v", err)
    }
    // cleanup should remove the file
    if cleanup != nil {
        cleanup()
        if _, err := os.Stat(p.Image); err == nil {
            t.Fatal("expected image file removed by cleanup")
        }
    }

    // Also test downloadTempImage directly
    ctx := context.Background()
    path, err := downloadTempImage(ctx, ts.URL+"/img.jpg")
    if err != nil {
        t.Fatalf("downloadTempImage failed: %v", err)
    }
    // file should exist and contain data
    data, _ := ioutil.ReadFile(path)
    if len(data) == 0 {
        t.Fatal("downloaded file empty")
    }
    os.Remove(path) //nolint:errcheck
}
package channels

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/store"
)

func init() {
	// Speed up reconnect delay for tests.
	reconnectDelay = 0
}

// ── helpers ──────────────────────────────────────────────────────────────────

// fakeDaemon is a minimal in-process signal-cli TCP JSON-RPC server.
// It accepts TCP connections and handles newline-delimited JSON-RPC messages.
type fakeDaemon struct {
	listener net.Listener
	mu       sync.Mutex
	conns    []net.Conn
	sent     []map[string]interface{} // captured send requests
	rpc      []map[string]interface{} // captured all rpc requests
	results  map[string]any
	stopCh   chan struct{}
}

func newFakeDaemon(t *testing.T) *fakeDaemon {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)

	fd := &fakeDaemon{
		listener: ln,
		results:  map[string]any{},
		stopCh:   make(chan struct{}),
	}
	go fd.acceptLoop()
	return fd
}

// Addr returns host:port, matching what SignalChannel expects.
func (fd *fakeDaemon) Addr() string {
	return fd.listener.Addr().String()
}

// acceptLoop accepts incoming connections and handles them.
func (fd *fakeDaemon) acceptLoop() {
	for {
		select {
		case <-fd.stopCh:
			return
		default:
		}

		conn, err := fd.listener.Accept()
		if err != nil {
			return
		}

		fd.mu.Lock()
		fd.conns = append(fd.conns, conn)
		fd.mu.Unlock()

		go fd.handleConn(conn)
	}
}

// handleConn reads JSON-RPC requests from a connection and responds.
func (fd *fakeDaemon) handleConn(conn net.Conn) {
	defer conn.Close() //nolint:errcheck
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		var req map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			continue
		}

		method, _ := req["method"].(string)
		reqID := req["id"]
		fd.mu.Lock()
		fd.rpc = append(fd.rpc, req)
		fd.mu.Unlock()

		// Capture outbound RPC requests used by tests.
		if method == "send" || method == "sendReaction" || method == "sendReceipt" {
			fd.mu.Lock()
			fd.sent = append(fd.sent, req)
			fd.mu.Unlock()
		}

		// Send response.
		resp := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqID,
			"result":  map[string]interface{}{},
		}
		fd.mu.Lock()
		if result, ok := fd.results[method]; ok {
			resp["result"] = result
		}
		fd.mu.Unlock()
		b, _ := json.Marshal(resp)
		b = append(b, '\n')
		_, _ = conn.Write(b)
	}
}

// Push sends a raw JSON-RPC notification to all connected clients.
func (fd *fakeDaemon) Push(msg []byte) {
	if msg[len(msg)-1] != '\n' {
		msg = append(msg, '\n')
	}
	fd.mu.Lock()
	conns := append([]net.Conn(nil), fd.conns...)
	fd.mu.Unlock()
	for _, c := range conns {
		_, _ = c.Write(msg)
	}
}

// PushNotification sends a well-formed "receive" notification.
func (fd *fakeDaemon) PushNotification(from, message string) {
	type dataMessage struct {
		Message string `json:"message"`
	}
	type envelope struct {
		Source      string      `json:"source"`
		DataMessage dataMessage `json:"dataMessage"`
	}
	type params struct {
		Envelope envelope `json:"envelope"`
	}
	notif := map[string]interface{}{
		"jsonrpc": "2.0",
		"method":  "receive",
		"params":  params{Envelope: envelope{Source: from, DataMessage: dataMessage{Message: message}}},
	}
	b, _ := json.Marshal(notif)
	fd.Push(b)
}

// SentRequests returns a copy of all captured RPC send requests.
func (fd *fakeDaemon) SentRequests() []map[string]interface{} {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	return append([]map[string]interface{}(nil), fd.sent...)
}

func (fd *fakeDaemon) Requests() []map[string]interface{} {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	return append([]map[string]interface{}(nil), fd.rpc...)
}

func (fd *fakeDaemon) SetResult(method string, result any) {
	fd.mu.Lock()
	defer fd.mu.Unlock()
	fd.results[method] = result
}

// Close closes all connections and the listener.
func (fd *fakeDaemon) Close() {
	close(fd.stopCh)
	fd.mu.Lock()
	for _, c := range fd.conns {
		c.Close() //nolint:errcheck
	}
	fd.mu.Unlock()
	fd.listener.Close() //nolint:errcheck
}

// startChannel starts a SignalChannel and returns a cancel func + error channel.
func startChannel(ch *SignalChannel) (context.CancelFunc, <-chan error) {
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- ch.Start(ctx) }()
	return cancel, errCh
}

// waitMsg waits up to timeout for a message, or fails the test.
func waitMsg(t *testing.T, msgs <-chan IncomingMessage, timeout time.Duration) IncomingMessage {
	t.Helper()
	var msg IncomingMessage
	var ok bool
	select {
	case msg = <-msgs:
		ok = true
	case <-time.After(timeout):
	}
	assert.True(t, ok)
	return msg
}

// waitMsgTimeout waits up to timeout for a message, returning (msg, true) or
// the zero value and false if nothing arrives in time.
func waitMsgTimeout(msgs <-chan IncomingMessage, timeout time.Duration) (IncomingMessage, bool) {
	select {
	case m, ok := <-msgs:
		if !ok {
			return IncomingMessage{}, false
		}
		return m, true
	case <-time.After(timeout):
		return IncomingMessage{}, false
	}
}

// waitConnected polls until the fake daemon has at least one connection.
func waitConnected(t *testing.T, fd *fakeDaemon, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	connected := false
	for time.Now().Before(deadline) {
		fd.mu.Lock()
		n := len(fd.conns)
		fd.mu.Unlock()
		if n > 0 {
			connected = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.True(t, connected)
}

// ── dispatch tests ────────────────────────────────────────────────────────────

func TestDispatch_ValidReceive(t *testing.T) {
	ch := NewSignalChannel("", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"hello"}}}}`
	ch.dispatch([]byte(line))

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "+15550001111", msg.From)
	assert.Equal(t, "hello", msg.Text)
	assert.Equal(t, "+15550001111", msg.Channel)

}

func TestDispatch_IngestsImageAttachmentFromStoredFilename(t *testing.T) {
	base := t.TempDir()
	store.SetDataDir(base)
	t.Cleanup(func() { store.SetDataDir("") })

	source := filepath.Join(t.TempDir(), "photo.png")
	err := os.WriteFile(source, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, 0o600)
	assert.NoError(t, err)

	ch := NewSignalChannel("", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	line := fmt.Sprintf(`{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"see image","attachments":[{"storedFilename":%q}]}}}}`, source)
	ch.dispatch([]byte(line))

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "see image", msg.Text)
	assert.True(t, strings.HasPrefix(msg.MediaURL, "data:image/"))

	entries, err := os.ReadDir(store.IncomingMediaDir("signal"))
	assert.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestDispatch_AllowsImageOnlyMessageWithAlternateAttachmentFields(t *testing.T) {
	base := t.TempDir()
	store.SetDataDir(base)
	t.Cleanup(func() { store.SetDataDir("") })

	source := filepath.Join(t.TempDir(), "photo.png")
	err := os.WriteFile(source, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, 0o600)
	assert.NoError(t, err)

	ch := NewSignalChannel("", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	line := fmt.Sprintf(`{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"","attachments":[{"content_type":"image/png","storedFileName":%q}]}}}}`, source)
	ch.dispatch([]byte(line))

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "", msg.Text)
	assert.True(t, strings.HasPrefix(msg.MediaURL, "data:image/png;base64,"))
}

func TestDispatch_TextMessageStillProcessesWhenAttachmentFetchTimesOut(t *testing.T) {
	origFetcher := signalAttachmentFetcher
	signalAttachmentFetcher = func(ctx context.Context, _ *SignalChannel, _ string, _ string, _ string, _ string, _ bool) ([]byte, error) {
		select {
		case <-time.After(3 * time.Second):
			return nil, context.DeadlineExceeded
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	t.Cleanup(func() { signalAttachmentFetcher = origFetcher })

	ch := NewSignalChannel("+12135550123", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	start := time.Now()
	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"see this","attachments":[{"contentType":"image/png","id":"attachment-1"}]}}}}`
	ch.dispatch([]byte(line))

	msg := waitMsg(t, msgs, 3*time.Second)
	assert.Equal(t, "see this", msg.Text)
	assert.Empty(t, msg.MediaURL)
	assert.Less(t, time.Since(start), 2500*time.Millisecond)
}

func TestFetchSignalAttachmentData_UsesDaemonRPC(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()
	fd.SetResult("getAttachment", map[string]any{
		"data": base64.StdEncoding.EncodeToString([]byte("png-bytes")),
	})

	ch := NewSignalChannel("+12135550123", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	ch.addrMu.Lock()
	ch.addr = fd.Addr()
	ch.addrMu.Unlock()

	data, err := fetchSignalAttachmentData(context.Background(), ch, ch.phone, "SCQAxyYZxx3Bt8EqQMFx.png", "+15550001111", "", false)
	assert.NoError(t, err)
	assert.Equal(t, []byte("png-bytes"), data)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reqs := fd.Requests()
		for _, req := range reqs {
			method, _ := req["method"].(string)
			if method != "getAttachment" {
				continue
			}
			params, _ := req["params"].(map[string]any)
			assert.Equal(t, "SCQAxyYZxx3Bt8EqQMFx.png", params["id"])
			assert.Equal(t, "+15550001111", params["recipient"])
			_, hasGroup := params["groupId"]
			assert.False(t, hasGroup)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Fail(t, "expected getAttachment rpc request")
}

func TestDecodeSignalAttachmentResult_AcceptsObjectPayload(t *testing.T) {
	raw := json.RawMessage(`{"data":"` + base64.StdEncoding.EncodeToString([]byte("png-bytes")) + `"}`)
	encoded, err := decodeSignalAttachmentResult(raw)
	assert.NoError(t, err)
	assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("png-bytes")), encoded)
}

func TestDispatch_NonReceiveMethodIgnored(t *testing.T) {
	ch := NewSignalChannel("", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	called := false
	ch.OnMessage(func(_ IncomingMessage) { called = true })

	ch.dispatch([]byte(`{"jsonrpc":"2.0","method":"syncMessage","params":{}}`))
	assert.False(t, called)

}

func TestDispatch_EmptyMessageIgnored(t *testing.T) {
	ch := NewSignalChannel("", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	called := false
	ch.OnMessage(func(_ IncomingMessage) { called = true })

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1","dataMessage":{"message":""}}}}`
	ch.dispatch([]byte(line))
	assert.False(t, called)

}

func TestDispatch_NilDataMessageIgnored(t *testing.T) {
	ch := NewSignalChannel("", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	called := false
	ch.OnMessage(func(_ IncomingMessage) { called = true })

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1"}}}`
	ch.dispatch([]byte(line))
	assert.False(t, called)

}

func TestFormatSignalMarkup(t *testing.T) {
	in := "# Title\n**bold** and *italic* and ~~gone~~ and ||secret|| and [link](https://example.com)\n```json\n{\"ok\":true}\n```"
	got := formatSignalMessage(in)
	assert.Equal(t, "Title\nbold and italic and gone and secret and link (https://example.com)\n{\"ok\":true}", got.Text)
	assert.Equal(t, []string{
		"6:4:BOLD",
		"15:6:ITALIC",
		"26:4:STRIKETHROUGH",
		"35:6:SPOILER",
		"73:11:MONOSPACE",
	}, got.TextStyles)

}

func TestFormatSignalMarkup_StripsUnsupportedUnderline(t *testing.T) {
	got := formatSignalMessage("<u>under</u> and __bold__")
	assert.Equal(t, "under and bold", got.Text)
	assert.Equal(t, []string{"10:4:BOLD"}, got.TextStyles)
}

func TestSignalChannel_Send_FormatsSignalMarkup(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	ch.addrMu.Lock()
	ch.addr = fd.Addr()
	ch.addrMu.Unlock()
	err := ch.Send("+15550001111", "**bold** and *italic*")
	assert.NoError(t, err)

	deadline := time.Now().Add(time.Second)
	got := ""
	var styles []any
	for time.Now().Before(deadline) {
		reqs := fd.SentRequests()
		if len(reqs) == 1 {
			params, _ := reqs[0]["params"].(map[string]any)
			got, _ = params["message"].(string)
			styles, _ = params["textStyle"].([]any)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, "bold and italic", got)
	assert.Equal(t, []any{"0:4:BOLD", "9:6:ITALIC"}, styles)
}

func TestSignalChannel_SendMedia_FormatsSignalMarkup(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("+1", "", nil, false, false, false, false, "m", nil)
	ch.addrMu.Lock()
	ch.addr = fd.Addr()
	ch.addrMu.Unlock()
	err := ch.SendMedia("+15550001111", "__bold__ ~~gone~~", "/tmp/img.png")
	assert.NoError(t, err)

	deadline := time.Now().Add(time.Second)
	got := ""
	var styles []any
	for time.Now().Before(deadline) {
		reqs := fd.SentRequests()
		if len(reqs) == 1 {
			params, _ := reqs[0]["params"].(map[string]any)
			got, _ = params["message"].(string)
			styles, _ = params["textStyle"].([]any)
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Equal(t, "bold gone", got)
	assert.Equal(t, []any{"0:4:BOLD", "5:4:STRIKETHROUGH"}, styles)
}

func TestDispatch_MalformedJSON(t *testing.T) {
	ch := NewSignalChannel("", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	called := false
	ch.OnMessage(func(_ IncomingMessage) { called = true })

	ch.dispatch([]byte(`{not valid json`))
	assert.False(t, called)

}

func TestDispatch_NoHandlerRegistered(_ *testing.T) {
	ch := NewSignalChannel("", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	// No handler — should not panic.
	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1","dataMessage":{"message":"hi"}}}}`
	ch.dispatch([]byte(line))
}

func TestDispatch_DoesNotSendReadReceiptWithoutHandler(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("+12135550123", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	ch.addrMu.Lock()
	ch.addr = fd.Addr()
	ch.addrMu.Unlock()

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","timestamp":1773380083969,"dataMessage":{"message":"hi?"}}}}`
	ch.dispatch([]byte(line))

	time.Sleep(100 * time.Millisecond)
	assert.Empty(t, fd.SentRequests())
}

func TestDispatch_SendsReadReceiptAfterHandler(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("+12135550123", "", []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	ch.addrMu.Lock()
	ch.addr = fd.Addr()
	ch.addrMu.Unlock()

	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","timestamp":1773380083969,"dataMessage":{"message":"hi?"}}}}`
	ch.dispatch([]byte(line))

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "hi?", msg.Text)

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		reqs := fd.SentRequests()
		if len(reqs) > 0 {
			method, _ := reqs[0]["method"].(string)
			assert.Equal(t, "sendReceipt", method)
			params, _ := reqs[0]["params"].(map[string]any)
			assert.Equal(t, "+15550001111", params["recipient"])
			assert.Equal(t, "read", params["type"])
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	assert.Fail(t, "expected read receipt")
}

func TestDispatch_GroupMention_RespondToMentions(t *testing.T) {
	const botPhone = "+12130000000"
	const groupID = "Z2lkPQ=="
	allowFrom := []config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "*",
		RespondToMentions: true,
	}}
	ch := NewSignalChannel(botPhone, "", allowFrom, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	// Message that @mentions the bot via the mentions array → should be received.
	withMention := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1","dataMessage":{"message":"hey","groupInfo":{"groupId":"` + groupID + `"},"mentions":[{"number":"` + botPhone + `","uuid":"","start":0,"length":3}]}}}}`
	ch.dispatch([]byte(withMention))
	_, ok := waitMsgTimeout(msgs, 200*time.Millisecond)
	assert.True(t, ok)

	// Message without mention → should be blocked.
	noMention := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1","dataMessage":{"message":"hey","groupInfo":{"groupId":"` + groupID + `"}}}}}`
	ch.dispatch([]byte(noMention))
	_, ok = waitMsgTimeout(msgs, 50*time.Millisecond)
	assert.False(t, ok)

}

func TestDispatch_GroupMentionUUIDOnly_RespondToMentions(t *testing.T) {
	const botPhone = "+12130000000"
	const botUUID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
	const groupID = "Z2lkPQ=="
	allowFrom := []config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "*",
		RespondToMentions: true,
	}}
	ch := NewSignalChannel(botPhone, "", allowFrom, true, true, true, true, "test", nil)
	ch.uuid = botUUID // simulate UUID resolved via listAccounts
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	// Message that @mentions the bot via UUID only (number field empty) → should be received.
	withUUIDMention := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1","dataMessage":{"message":"hey","groupInfo":{"groupId":"` + groupID + `"},"mentions":[{"number":"","uuid":"` + botUUID + `","start":0,"length":3}]}}}}`
	ch.dispatch([]byte(withUUIDMention))
	_, ok := waitMsgTimeout(msgs, 200*time.Millisecond)
	assert.True(t, ok, "UUID-only mention should trigger response")

	// Message that @mentions a different UUID → should be blocked.
	otherUUID := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1","dataMessage":{"message":"hey","groupInfo":{"groupId":"` + groupID + `"},"mentions":[{"number":"","uuid":"other-uuid","start":0,"length":3}]}}}}`
	ch.dispatch([]byte(otherUUID))
	_, ok = waitMsgTimeout(msgs, 50*time.Millisecond)
	assert.False(t, ok, "mention of different UUID should be blocked")
}

func TestDispatch_EditedGroupMention_RespondToMentions(t *testing.T) {
	const botPhone = "+12130000000"
	const groupID = "Z2lkPQ=="
	allowFrom := []config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "*",
		RespondToMentions: true,
	}}
	ch := NewSignalChannel(botPhone, "", allowFrom, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+1","editMessage":{"targetSentTimestamp":123,"dataMessage":{"message":"hey","groupInfo":{"groupId":"` + groupID + `"},"mentions":[{"number":"` + botPhone + `","uuid":"","start":0,"length":3}]}}}}}`
	ch.dispatch([]byte(line))

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "+1", msg.From)
	assert.Equal(t, groupID, msg.Channel)
	assert.Equal(t, "hey", msg.Text)
}

func TestDispatch_ReplyToSelfStillRequiresAllowFrom_DirectMessage(t *testing.T) {
	const botPhone = "+12130000000"
	ch := NewSignalChannel(botPhone, "", []config.AllowFromEntry{{From: "+15550001111"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	blocked := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15559999999","dataMessage":{"message":"reply from blocked sender","quote":{"author":"` + botPhone + `","id":1,"text":"previous reply"}}}}}`
	ch.dispatch([]byte(blocked))

	_, ok := waitMsgTimeout(msgs, 100*time.Millisecond)
	assert.False(t, ok)

	allowed := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"reply from allowed sender","quote":{"author":"` + botPhone + `","id":2,"text":"previous reply"}}}}}`
	ch.dispatch([]byte(allowed))

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "+15550001111", msg.From)
	assert.Equal(t, "reply from allowed sender", msg.Text)
}

func TestDispatch_ReplyToSelfStillRequiresAllowFrom_GroupSenderAndChannel(t *testing.T) {
	const botPhone = "+12130000000"
	const allowedGroupID = "allowed-group"
	ch := NewSignalChannel(botPhone, "", []config.AllowFromEntry{{
		From:          "+15550001111",
		AllowedGroups: allowedGroupID,
	}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 2)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	blockedSender := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15559999999","dataMessage":{"message":"reply from blocked sender","groupInfo":{"groupId":"` + allowedGroupID + `"},"quote":{"author":"` + botPhone + `","id":3,"text":"previous reply"}}}}}`
	ch.dispatch([]byte(blockedSender))
	_, ok := waitMsgTimeout(msgs, 100*time.Millisecond)
	assert.False(t, ok)

	blockedGroup := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"reply in blocked group","groupInfo":{"groupId":"other-group"},"quote":{"author":"` + botPhone + `","id":4,"text":"previous reply"}}}}}`
	ch.dispatch([]byte(blockedGroup))
	_, ok = waitMsgTimeout(msgs, 100*time.Millisecond)
	assert.False(t, ok)

	allowed := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"reply in allowed group","groupInfo":{"groupId":"` + allowedGroupID + `"},"quote":{"author":"` + botPhone + `","id":5,"text":"previous reply"}}}}}`
	ch.dispatch([]byte(allowed))

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "+15550001111", msg.From)
	assert.Equal(t, allowedGroupID, msg.Channel)
	assert.Equal(t, "reply in allowed group", msg.Text)
}

func TestDispatch_ReplyToSelfBypassesMentionRuleButStillRequiresSenderAndGroup(t *testing.T) {
	const botPhone = "+12130000000"
	const groupID = "Z2lkPQ=="
	ch := NewSignalChannel(botPhone, "", []config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "*",
		RespondToMentions: true,
	}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	withoutMention := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"reply without mention","groupInfo":{"groupId":"` + groupID + `"},"quote":{"author":"` + botPhone + `","id":6,"text":"previous reply"}}}}}`
	ch.dispatch([]byte(withoutMention))
	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "reply without mention", msg.Text)
}

func TestDispatch_ReplyToSelfWithoutReplySettingStillRequiresMentionRule(t *testing.T) {
	const botPhone = "+12130000000"
	const groupID = "Z2lkPQ=="
	ch := NewSignalChannel(botPhone, "", []config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "*",
		RespondToMentions: true,
	}}, true, true, false, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	withoutMention := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"reply without mention","groupInfo":{"groupId":"` + groupID + `"},"quote":{"author":"` + botPhone + `","id":7,"text":"previous reply"}}}}}`
	ch.dispatch([]byte(withoutMention))

	_, ok := waitMsgTimeout(msgs, 100*time.Millisecond)
	assert.False(t, ok)

	withMention := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"reply with mention","groupInfo":{"groupId":"` + groupID + `"},"quote":{"author":"` + botPhone + `","id":8,"text":"previous reply"},"mentions":[{"number":"` + botPhone + `","uuid":"","start":0,"length":5}]}}}}`
	ch.dispatch([]byte(withMention))

	msg := waitMsg(t, msgs, time.Second)
	assert.Equal(t, "reply with mention", msg.Text)
}

func TestDispatch_ReplyToReplyStillRequiresAllowFrom(t *testing.T) {
	const botPhone = "+12130000000"
	ch := NewSignalChannel(botPhone, "", []config.AllowFromEntry{{From: "+15550001111"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	line := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15559999999","dataMessage":{"message":"nested reply from blocked sender","quote":{"author":"` + botPhone + `","id":8,"text":"assistant reply to previous user reply"}}}}}`
	ch.dispatch([]byte(line))

	_, ok := waitMsgTimeout(msgs, 100*time.Millisecond)
	assert.False(t, ok)
}

func TestCheckAllowedReplyToSelf_GroupIgnoresMentionFilters(t *testing.T) {
	result := checkAllowedReplyToSelf([]config.AllowFromEntry{{
		From:              "*",
		AllowedGroups:     "group-1",
		RespondToMentions: true,
		MentionPrefixes:   []string{"aviary"},
	}}, "+15550001111", "group-1", true)
	assert.True(t, result.allowed)
}

func TestCheckAllowedReplyToSelf_GroupStillRequiresSenderAndChannel(t *testing.T) {
	entries := []config.AllowFromEntry{{
		From:              "+15550001111",
		AllowedGroups:     "group-1",
		RespondToMentions: true,
	}}
	assert.False(t, checkAllowedReplyToSelf(entries, "+15559999999", "group-1", true).allowed)
	assert.False(t, checkAllowedReplyToSelf(entries, "+15550001111", "group-2", true).allowed)
}

// ── checkAllowed tests ────────────────────────────────────────────────────────

func TestCheckAllowed_DirectMessages(t *testing.T) {
	tests := []struct {
		name      string
		allowFrom []config.AllowFromEntry
		from      string
		want      bool
	}{
		{"empty allowFrom blocks all", nil, "+1", false},
		{"wildcard allows all", []config.AllowFromEntry{{From: "*"}}, "+1", true},
		{"exact match", []config.AllowFromEntry{{From: "+15551111111"}}, "+15551111111", true},
		{"no match", []config.AllowFromEntry{{From: "+15551111111"}}, "+15559999999", false},
		{"multiple in one entry", []config.AllowFromEntry{{From: "+15551111111,+15552222222"}}, "+15552222222", true},
		{"multiple entries, match second", []config.AllowFromEntry{{From: "+15551111111"}, {From: "+15552222222"}}, "+15552222222", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := checkAllowed(tc.allowFrom, tc.from, tc.from, "", false, "", false)
			assert.Equal(t, tc.want, result.allowed)

		})
	}
}

func TestCheckAllowed_GroupMessages(t *testing.T) {
	const groupID = "abc123=="
	const sender = "+15551234567"
	tests := []struct {
		name      string
		allowFrom []config.AllowFromEntry
		from      string
		text      string
		want      bool
	}{
		{
			"wildcard sender + allowedGroups=* allows all",
			[]config.AllowFromEntry{{From: "*", AllowedGroups: "*"}},
			sender, "hello", true,
		},
		{
			"exact sender + allowedGroups=* allows that sender",
			[]config.AllowFromEntry{{From: sender, AllowedGroups: "*"}},
			sender, "hello", true,
		},
		{
			"exact sender + specific group allows matching",
			[]config.AllowFromEntry{{From: sender, AllowedGroups: groupID}},
			sender, "hello", true,
		},
		{
			"exact sender + wrong group blocked",
			[]config.AllowFromEntry{{From: sender, AllowedGroups: "other=="}},
			sender, "hello", false,
		},
		{
			"wrong sender blocked even with allowedGroups=*",
			[]config.AllowFromEntry{{From: "+19999999999", AllowedGroups: "*"}},
			sender, "hello", false,
		},
		{
			"no allowedGroups means DM-only — group message blocked",
			[]config.AllowFromEntry{{From: "*"}},
			sender, "hello", false,
		},
		{
			"allowedGroups=* with respond_to_mentions=true blocks non-mention",
			[]config.AllowFromEntry{{From: "*", AllowedGroups: "*", RespondToMentions: true}},
			sender, "hello", false,
		},
		{
			"allowedGroups=* with mention_prefixes match allows",
			[]config.AllowFromEntry{{From: "*", AllowedGroups: "*", MentionPrefixes: []string{"hey bot"}}},
			sender, "hey bot do something", true,
		},
		{
			"allowedGroups=* with mention_prefixes no match blocked",
			[]config.AllowFromEntry{{From: "*", AllowedGroups: "*", MentionPrefixes: []string{"hey bot"}}},
			sender, "hello", false,
		},
		{
			"allowedGroups comma-separated with spaces matches",
			[]config.AllowFromEntry{{From: "*", AllowedGroups: "other== , " + groupID}},
			sender, "hello", true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := checkAllowed(tc.allowFrom, tc.from, groupID, tc.text, true, "", false)
			assert.Equal(t, tc.want, result.allowed)

		})
	}
}

func TestCheckAllowed_SpaceTrimming(t *testing.T) {
	// Comma-separated IDs with spaces should behave identically to without spaces.
	entries := []config.AllowFromEntry{{From: "+15551111111 , +15552222222"}}
	assert.True(t, checkAllowed(entries, "+15552222222", "+15552222222", "", false, "", false).allowed)
	assert.False(t, checkAllowed(entries, "+15559999999", "+15559999999", "", false, "", false).allowed)

}

// ── DaemonInfo tests ──────────────────────────────────────────────────────────

func TestDaemonInfo_ExternalMode(t *testing.T) {
	ch := NewSignalChannel("+1", "127.0.0.1:7583", nil, true, true, true, true, "test", nil)
	info := ch.DaemonInfo()
	assert.NotNil(t, info)
	assert.Equal(t, "127.0.0.1:7583", info.Addr)
	assert.True(t, info.External)
	assert.Equal(t, 0, info.PID)

}

func TestDaemonInfo_ManagedMode_NotRunning(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil, true, true, true, true, "test", nil)
	info := ch.DaemonInfo()
	assert.Nil(t, info)

}

func TestDaemonInfo_ManagedMode_Running(t *testing.T) {
	ch := NewSignalChannel("+1", "", nil, true, true, true, true, "test", nil)
	d := &sharedDaemon{phone: "+1"}
	d.procMu.Lock()
	d.procPID = 1234
	d.procStarted = time.Now()
	d.procMu.Unlock()
	d.addrMu.Lock()
	d.addr = "127.0.0.1:9999"
	d.addrMu.Unlock()
	ch.daemon = d

	info := ch.DaemonInfo()
	assert.NotNil(t, info)
	assert.Equal(t, 1234, info.PID)
	assert.Equal(t, "127.0.0.1:9999", info.Addr)
	assert.False(t, info.External)

}

// ── Send tests (HTTP POST) ────────────────────────────────────────────────────

func TestSend_PostsJSONRPCOverTCP(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("", fd.Addr(), nil, true, true, true, true, "test", nil)
	err := ch.Send("+15550001111", "test message")
	assert.NoError(t, err)

	reqs := fd.SentRequests()
	assert.Equal(t, 1, len(reqs))

	req := reqs[0]
	assert.Equal(t, "send", req["method"])
	assert.Equal(t, "2.0", req["jsonrpc"])

	params, _ := req["params"].(map[string]interface{})
	assert.NotNil(t, params)

	recipients, _ := params["recipient"].([]interface{})
	assert.Len(t, recipients, 1)
	assert.Equal(t, "+15550001111", recipients[0])
	assert.Equal(t, "test message", params["message"])

}

func TestSend_DaemonNotReady(t *testing.T) {
	ch := NewSignalChannel("", "", nil, true, true, true, true, "test", nil)
	err := ch.Send("+1", "hi")
	assert.Error(t, err)

}

func TestSend_GroupUsesGroupId(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	const groupID = "Z2lkPQ=="
	ch := NewSignalChannel("", fd.Addr(), nil, true, true, true, true, "test", nil)
	err := ch.Send(groupID, "hello group")
	assert.NoError(t, err)

	reqs := fd.SentRequests()
	assert.Equal(t, 1, len(reqs))

	params, _ := reqs[0]["params"].(map[string]interface{})
	assert.NotNil(t, params)
	assert.Equal(t, groupID, params["groupId"])
	_, hasRecipient := params["recipient"]
	assert.False(t, hasRecipient)

	assert.Equal(t, "hello group", params["message"])

}

// ── Start / Stop (external mode, TCP JSON-RPC) ───────────────────────────────

func TestStart_ExternalMode_ReceivesMessages(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("", fd.Addr(), []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 4)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	cancel, errCh := startChannel(ch)
	defer cancel()

	waitConnected(t, fd, 2*time.Second)
	fd.PushNotification("+15550001111", "hello from signal")

	msg := waitMsg(t, msgs, 2*time.Second)
	assert.Equal(t, "+15550001111", msg.From)
	assert.Equal(t, "hello from signal", msg.Text)

	cancel()
	var stopErr error
	select {
	case stopErr = <-errCh:
	case <-time.After(2 * time.Second):
	}
	assert.NoError(t, stopErr)
}

func TestStart_ExternalMode_FiltersByAllowFrom(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("", fd.Addr(), []config.AllowFromEntry{{From: "+15550001111"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 4)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	cancel, _ := startChannel(ch)
	defer cancel()

	waitConnected(t, fd, 2*time.Second)

	// Blocked sender first, then allowed.
	fd.PushNotification("+19999999999", "should be blocked")
	fd.PushNotification("+15550001111", "should pass")

	msg := waitMsg(t, msgs, 2*time.Second)
	assert.Equal(t, "+15550001111", msg.From)

	var extra IncomingMessage
	var hasExtra bool
	select {
	case extra = <-msgs:
		hasExtra = true
	default:
	}
	_ = extra
	assert.False(t, hasExtra)
}

func TestStop_StopsChannel(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("", fd.Addr(), []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	cancel, errCh := startChannel(ch)
	defer cancel()

	waitConnected(t, fd, 2*time.Second)
	ch.Stop()

	var stopErr error
	select {
	case stopErr = <-errCh:
	case <-time.After(2 * time.Second):
	}
	assert.NoError(t, stopErr)
}

func TestStart_ExternalMode_MultipleMessages(t *testing.T) {
	fd := newFakeDaemon(t)
	defer fd.Close()

	ch := NewSignalChannel("", fd.Addr(), []config.AllowFromEntry{{From: "*"}}, true, true, true, true, "test", nil)
	msgs := make(chan IncomingMessage, 10)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	cancel, _ := startChannel(ch)
	defer cancel()

	waitConnected(t, fd, 2*time.Second)

	for i := range 5 {
		fd.PushNotification("+1", fmt.Sprintf("msg%d", i))
	}

	for i := range 5 {
		msg := waitMsg(t, msgs, 2*time.Second)
		want := fmt.Sprintf("msg%d", i)
		assert.Equal(t, want, msg.Text)

	}
}

func TestDispatch_MentionPrefixGroupOnly_False_DMRequiresPrefix(t *testing.T) {
	f := false
	allowFrom := []config.AllowFromEntry{{
		From:                   "*",
		MentionPrefixes:        []string{"aviary"},
		MentionPrefixGroupOnly: &f,
	}}
	ch := NewSignalChannel("", "", allowFrom, false, false, false, false, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	// DM without prefix → blocked.
	noPrefix := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"hello"}}}}`
	ch.dispatch([]byte(noPrefix))
	_, ok := waitMsgTimeout(msgs, 50*time.Millisecond)
	assert.False(t, ok)

	// DM with prefix → allowed.
	withPrefix := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"aviary do this"}}}}`
	ch.dispatch([]byte(withPrefix))
	msg, ok := waitMsgTimeout(msgs, 200*time.Millisecond)
	assert.True(t, ok)
	assert.Equal(t, "aviary do this", msg.Text)
}

func TestDispatch_MentionPrefixGroupOnly_Default_DMPassesWithoutPrefix(t *testing.T) {
	// Default (nil): DMs should pass without any prefix.
	allowFrom := []config.AllowFromEntry{{
		From:            "*",
		MentionPrefixes: []string{"aviary"},
	}}
	ch := NewSignalChannel("", "", allowFrom, false, false, false, false, "test", nil)
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	noPrefix := `{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"hello"}}}}`
	ch.dispatch([]byte(noPrefix))
	_, ok := waitMsgTimeout(msgs, 200*time.Millisecond)
	assert.True(t, ok)
}
