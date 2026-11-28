package channels

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/lsegal/aviary/internal/config"
)

// useSignalAttachmentRetry sets the by-id fetch budget, first backoff, and
// local attachment store directories for the duration of a test.
func useSignalAttachmentRetry(t *testing.T, timeout, delay time.Duration, dirs []string) {
	t.Helper()
	origTimeout, origDelay, origDirs := signalAttachmentFetchTimeout, signalAttachmentRetryDelay, signalAttachmentDirs
	signalAttachmentFetchTimeout, signalAttachmentRetryDelay = timeout, delay
	signalAttachmentDirs = func() []string { return dirs }
	t.Cleanup(func() {
		signalAttachmentFetchTimeout, signalAttachmentRetryDelay, signalAttachmentDirs = origTimeout, origDelay, origDirs
	})
}

func useSignalAttachmentFetcher(t *testing.T, fn func(attempt int) ([]byte, error)) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	orig := signalAttachmentFetcher
	signalAttachmentFetcher = func(context.Context, *SignalChannel, string, string, string, string, bool) ([]byte, error) {
		return fn(int(calls.Add(1)))
	}
	t.Cleanup(func() { signalAttachmentFetcher = orig })
	return &calls
}

// captureSlog routes the default logger into a buffer for the test.
func captureSlog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	orig := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, nil)))
	t.Cleanup(func() { slog.SetDefault(orig) })
	return buf
}

type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newScriptedSignalServer answers each request by writing the lines script
// returns for the request's id, one connection at a time.
func newScriptedSignalServer(t *testing.T, script func(id json.RawMessage) []string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	assert.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close() //nolint:errcheck
				scanner := bufio.NewScanner(conn)
				for scanner.Scan() {
					var req struct {
						ID json.RawMessage `json:"id"`
					}
					if json.Unmarshal(scanner.Bytes(), &req) != nil {
						continue
					}
					w := bufio.NewWriter(conn)
					for _, line := range script(req.ID) {
						_, _ = w.WriteString(line + "\n")
					}
					_ = w.Flush()
				}
			}()
		}
	}()
	return ln.Addr().String()
}

func TestFetchSignalAttachmentData_SkipsUnrelatedMessagesAndDecodesLargeBody(t *testing.T) {
	payload := bytes.Repeat([]byte("\x89PNG-large-body"), 20*1024) // ~300 KB, well over 64 KB
	encoded := base64.StdEncoding.EncodeToString(payload)
	addr := newScriptedSignalServer(t, func(id json.RawMessage) []string {
		return []string{
			`{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"hi"}}}}`,
			`{"jsonrpc":"2.0","id":999999,"result":{"data":"bm90LXRoaXM="}}`,
			fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":{"data":%q}}`, id, encoded),
		}
	})
	ch := NewSignalChannel("+12135550123", addr, nil, true, true, true, true, "test", nil)

	data, err := fetchSignalAttachmentData(context.Background(), ch, ch.phone, "att-1", "+15550001111", "", false)

	assert.NoError(t, err)
	assert.Equal(t, payload, data)
}

func TestReadRPCResponse(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr string
	}{
		{"string id", `{"jsonrpc":"2.0","id":"7","result":"x"}`, `"x"`, ""},
		{"rpc error", `{"jsonrpc":"2.0","id":7,"error":{"code":-1,"message":"nope"}}`, "", ""},
		{"empty result", `{"jsonrpc":"2.0","method":"receive","params":{}}` + "\n" + `{"jsonrpc":"2.0","id":7}`, "", "has no result"},
		{"no matching response", `{"jsonrpc":"2.0","id":8,"result":"x"}`, "", "EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var resp jsonrpcResponse
			err := readRPCResponse(strings.NewReader(tt.input), 7, &resp)
			if tt.wantErr != "" {
				if assert.Error(t, err) {
					assert.Contains(t, err.Error(), tt.wantErr)
				}
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.want, string(resp.Result))
		})
	}
}

func TestDecodeSignalAttachmentResult_Shapes(t *testing.T) {
	want := base64.StdEncoding.EncodeToString([]byte("png-bytes"))
	for _, raw := range []string{
		`"` + want + `"`,
		`{"data":"` + want + `"}`,
		`{"base64":"` + want + `"}`,
		`{"contentType":"image/png","content":"` + want + `"}`,
		`{"data":"","attachment":"` + want + `"}`,
	} {
		got, err := decodeSignalAttachmentResult(json.RawMessage(raw))
		assert.NoError(t, err, raw)
		assert.Equal(t, want, got, raw)
	}
	_, err := decodeSignalAttachmentResult(nil)
	if assert.Error(t, err) {
		assert.Contains(t, err.Error(), "empty result")
	}
}

func TestIngestSignalAttachment_RetriesUntilAvailable(t *testing.T) {
	useTempDataDir(t)
	useSignalAttachmentRetry(t, 2*time.Second, 10*time.Millisecond, nil)
	calls := useSignalAttachmentFetcher(t, func(attempt int) ([]byte, error) {
		if attempt == 1 {
			return nil, fmt.Errorf("signal: rpc error: -1 attachment not found")
		}
		return []byte("png-bytes"), nil
	})
	ch := NewSignalChannel("+12135550123", "", nil, true, true, true, true, "test", nil)

	att := ch.ingestSignalAttachment(signalAttachment{ContentType: "image/png", ID: "att-1"}, "+1555", "+1555", false)

	assert.Empty(t, att.Err)
	assert.NotEmpty(t, att.Path)
	assert.Equal(t, int32(2), calls.Load())
}

func TestIngestSignalAttachment_FallsBackToLocalStore(t *testing.T) {
	useTempDataDir(t)
	dir := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "att-9.png"), []byte("png-bytes"), 0o600))
	useSignalAttachmentRetry(t, 2*time.Second, 10*time.Millisecond, []string{t.TempDir(), dir})
	useSignalAttachmentFetcher(t, func(int) ([]byte, error) {
		return nil, fmt.Errorf("signal: decode getAttachment result: empty result")
	})
	ch := NewSignalChannel("+12135550123", "", nil, true, true, true, true, "test", nil)

	att := ch.ingestSignalAttachment(signalAttachment{ContentType: "image/png", ID: "att-9"}, "+1555", "+1555", false)

	assert.Empty(t, att.Err)
	if assert.NotEmpty(t, att.Path) {
		data, err := os.ReadFile(att.Path)
		assert.NoError(t, err)
		assert.Equal(t, []byte("png-bytes"), data)
	}
}

func TestFindSignalLocalAttachment_RejectsPathsOutsideStore(t *testing.T) {
	dir := t.TempDir()
	assert.NoError(t, os.WriteFile(filepath.Join(dir, "secret"), []byte("x"), 0o600))
	store := filepath.Join(dir, "attachments")
	assert.NoError(t, os.Mkdir(store, 0o700))
	useSignalAttachmentRetry(t, time.Second, time.Millisecond, []string{store})

	for _, id := range []string{"", "..", "../secret", "*", filepath.Join("..", "secret")} {
		assert.Empty(t, findSignalLocalAttachment(id), id)
	}
}

func TestDispatch_AttachmentFailureAsksSenderToResend(t *testing.T) {
	useTempDataDir(t)
	logs := captureSlog(t)
	useSignalAttachmentRetry(t, 200*time.Millisecond, 20*time.Millisecond, []string{t.TempDir()})
	useSignalAttachmentFetcher(t, func(int) ([]byte, error) {
		return nil, fmt.Errorf("signal: decode getAttachment result: empty result")
	})
	fd := newFakeDaemon(t)
	defer fd.Close()
	ch := NewSignalChannel("+12135550123", "", []config.AllowFromEntry{{From: "*"}}, false, true, true, false, "test", nil)
	connectChannel(ch, fd.Addr())
	msgs := make(chan IncomingMessage, 1)
	ch.OnMessage(func(m IncomingMessage) { msgs <- m })

	ch.dispatch([]byte(`{"jsonrpc":"2.0","method":"receive","params":{"envelope":{"source":"+15550001111","dataMessage":{"message":"","attachments":[` +
		`{"contentType":"image/png","id":"img-42","size":10450},{"contentType":"audio/aac","id":"voice-7"},{"contentType":"image/jpeg","id":"img-43"}]}}}}`))

	msg := waitMsg(t, msgs, 3*time.Second)
	if assert.Len(t, msg.Attachments, 3) {
		assert.Contains(t, msg.Attachments[0].Placeholder(), "could not be saved")
	}
	sent := requestParams(fd.waitSentRequest(t, 3*time.Second))
	assert.Equal(t, "Couldn't receive your image and voice note, please resend.", sent["message"])
	assert.Equal(t, []any{"+15550001111"}, sent["recipient"])
	out := logs.String()
	assert.Contains(t, out, "attachment could not be fetched")
	assert.Contains(t, out, "id=img-42")
	assert.Contains(t, out, "empty result")
}
