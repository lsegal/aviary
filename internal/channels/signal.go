package channels

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	nethtml "golang.org/x/net/html"

	"github.com/lsegal/aviary/internal/config"
)

var (
	urlRegex = regexp.MustCompile(`https?://[^\s<>"{}|\\^\x60\[\]]+`)
)

// linkPreview holds metadata for a URL preview to include in outgoing Signal messages.
type linkPreview struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	Image       string `json:"image,omitempty"` // local file path for signal-cli
}

// fetchLinkPreviews extracts the first URL from text, fetches its OG metadata
// (including downloading the og:image to a temp file), and returns a slice of
// previews suitable for the signal-cli send RPC plus a cleanup function to
// remove any temp files once the send has completed.
func fetchLinkPreviews(text string) ([]linkPreview, func()) {
	raw := urlRegex.FindString(text)
	if raw == "" {
		return nil, nil
	}
	u := strings.TrimRight(raw, ".,;:!?)")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, nil
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Aviary/1.0)")
	req.Header.Set("Accept", "text/html")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Debug("signal: link preview fetch failed", "url", u, "err", err)
		return nil, nil
	}
	defer resp.Body.Close() //nolint:errcheck

	if !strings.Contains(resp.Header.Get("Content-Type"), "text/html") {
		return nil, nil
	}

	p := linkPreview{URL: u}
	z := nethtml.NewTokenizer(io.LimitReader(resp.Body, 64*1024))
	var inTitle bool

	for {
		tt := z.Next()
		if tt == nethtml.ErrorToken {
			break
		}
		t := z.Token()

		switch tt {
		case nethtml.StartTagToken, nethtml.SelfClosingTagToken:
			tagName := strings.ToLower(t.Data)
			if tagName == "title" {
				inTitle = true
			}
			if tagName == "meta" {
				var property, content string
				for _, a := range t.Attr {
					key := strings.ToLower(a.Key)
					if key == "property" || key == "name" {
						property = strings.ToLower(a.Val)
					}
					if key == "content" {
						content = a.Val
					}
				}
				switch property {
				case "og:title", "twitter:title":
					if p.Title == "" {
						p.Title = strings.TrimSpace(content)
					}
				case "og:description", "description", "twitter:description":
					if p.Description == "" {
						p.Description = strings.TrimSpace(content)
					}
				case "og:image", "twitter:image":
					if p.Image == "" && content != "" {
						p.Image = content
					}
				}
			}
			if tagName == "link" {
				var rel, href string
				for _, a := range t.Attr {
					key := strings.ToLower(a.Key)
					if key == "rel" {
						rel = strings.ToLower(a.Val)
					}
					if key == "href" {
						href = a.Val
					}
				}
				if (rel == "icon" || rel == "apple-touch-icon" || rel == "shortcut icon") && p.Image == "" {
					p.Image = href
				}
			}
			if tagName == "img" && p.Image == "" {
				for _, a := range t.Attr {
					if strings.ToLower(a.Key) == "src" {
						p.Image = a.Val
						break
					}
				}
			}
		case nethtml.EndTagToken:
			if strings.ToLower(t.Data) == "title" {
				inTitle = false
			}
		case nethtml.TextToken:
			if inTitle && p.Title == "" {
				p.Title = strings.TrimSpace(t.Data)
			}
		}
	}

	if p.Title == "" {
		return nil, nil
	}

	p.Title = html.UnescapeString(p.Title)
	p.Description = html.UnescapeString(p.Description)

	// Resolve image URL and download to a temp file for signal-cli.
	var cleanup func()
	imageURL := html.UnescapeString(p.Image)
	p.Image = "" // clear it; will be set to local path if download succeeds
	if imageURL != "" {
		// Resolve relative image URLs against the page URL.
		if base, err := url.Parse(u); err == nil {
			if rel, err := url.Parse(imageURL); err == nil {
				imageURL = base.ResolveReference(rel).String()
			}
		}

		if strings.HasPrefix(imageURL, "http") {
			if path, err := downloadTempImage(ctx, imageURL); err == nil {
				p.Image = path
				cleanup = func() { os.Remove(path) } //nolint:errcheck
			} else {
				slog.Debug("signal: link preview image download failed", "url", imageURL, "err", err)
			}
		}
	}

	return []linkPreview{p}, cleanup
}

// downloadTempImage fetches imageURL and writes it to a temp file, returning the path.
func downloadTempImage(ctx context.Context, imageURL string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imageURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Aviary/1.0)")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close() //nolint:errcheck

	f, err := os.CreateTemp("", "aviary-preview-*.jpg")
	if err != nil {
		return "", err
	}
	defer f.Close() //nolint:errcheck

	// Ensure the file is readable by the signal-cli subprocess.
	_ = os.Chmod(f.Name(), 0644)

	if _, err := io.Copy(f, io.LimitReader(resp.Body, 4*1024*1024)); err != nil {
		os.Remove(f.Name()) //nolint:errcheck
		return "", err
	}
	return f.Name(), nil
}

// SignalChannel connects to a signal-cli daemon via its native TCP JSON-RPC interface.
// If url is set, it connects to an existing external daemon. Otherwise it
// launches signal-cli automatically as a managed subprocess.
//
// External daemon (signal-cli must already be running):
//
//	channels:
//	  - type: signal
//	    phone: "+15551234567"
//	    url: "127.0.0.1:7583"
//	    allowFrom:
//	      - from: "+15559876543"
//
// Managed daemon (signal-cli is launched and managed automatically):
//
//	channels:
//	  - type: signal
//	    phone: "+15551234567"
//	    allowFrom:
//	      - from: "+15559876543"
type SignalChannel struct {
	phone     string // registered Signal account phone number
	initAddr  string // configured TCP address; empty → managed daemon mode
	allowFrom []config.AllowFromEntry
	model     string
	fallbacks []string

	// Per-channel feature flags (defaults are true).
	showTyping       bool // show typing indicator while agent processes
	reactToEmoji     bool // mirror emoji reactions on agent's own messages
	replyToReplies   bool // respond to quoted replies targeting agent's messages
	sendReadReceipts bool // send read receipts for messages the agent will respond to

	addrMu sync.RWMutex
	addr   string // current effective daemon address (set dynamically in managed mode)

	procMu      sync.RWMutex
	procPID     int       // PID of the managed daemon (0 when not running)
	procStarted time.Time // time the managed daemon was started

	handler   func(IncomingMessage)
	handlerMu sync.RWMutex
	stopOnce  sync.Once
	done      chan struct{}
	idSeq     atomic.Int64

	logSinkMu sync.RWMutex
	logSink   *LogSink
}

// reconnectDelay is the wait between reconnect attempts after a connection error.
// Exposed as a variable so tests can set it to zero for fast iteration.
var reconnectDelay = 2 * time.Second

// NewSignalChannel creates a SignalChannel.
// phone is the registered account phone number; addr is the optional TCP
// address of an existing signal-cli JSON-RPC daemon (e.g. "127.0.0.1:7583").
// When addr is empty, signal-cli is launched and managed automatically.
// showTyping, reactToEmoji, replyToReplies, and sendReadReceipts enable the
// corresponding per-channel behaviours (all typically defaulted to true by the caller).
func NewSignalChannel(phone, addr string, allowFrom []config.AllowFromEntry, showTyping, reactToEmoji, replyToReplies, sendReadReceipts bool, model string, fallbacks []string) *SignalChannel {
	return &SignalChannel{
		phone:            phone,
		initAddr:         addr,
		addr:             addr,
		allowFrom:        allowFrom,
		showTyping:       showTyping,
		reactToEmoji:     reactToEmoji,
		replyToReplies:   replyToReplies,
		sendReadReceipts: sendReadReceipts,
		model:            model,
		fallbacks:        fallbacks,
		done:             make(chan struct{}),
	}
}

// SetLogSink attaches a LogSink that receives stdout/stderr lines from the
// managed signal-cli subprocess. Called by the Manager before Start.
func (c *SignalChannel) SetLogSink(s *LogSink) {
	c.logSinkMu.Lock()
	c.logSink = s
	c.logSinkMu.Unlock()
}

// ShowTyping reports whether the typing-indicator feature is enabled for this channel.
func (c *SignalChannel) ShowTyping() bool { return c.showTyping }

// streamToSink reads lines from r and writes them to the channel's LogSink.
// Runs in its own goroutine; exits when r reaches EOF or an error occurs.
func (c *SignalChannel) streamToSink(r io.ReadCloser) {
	defer r.Close() //nolint:errcheck
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()
		c.logSinkMu.RLock()
		sink := c.logSink
		c.logSinkMu.RUnlock()
		if sink != nil {
			sink.Write(line)
		}
	}
}

// OnMessage registers a callback for incoming messages.
func (c *SignalChannel) OnMessage(fn func(IncomingMessage)) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	c.handler = fn
}

// Send sends a Signal message to a recipient or group via JSON-RPC over TCP.
// channel must be a phone number in E.164 format (starts with "+") for direct
// messages, or a base64-encoded group ID for group conversations.
func (c *SignalChannel) Send(channel, text string) error {
	c.addrMu.RLock()
	addr := c.addr
	c.addrMu.RUnlock()

	if addr == "" {
		return fmt.Errorf("signal: daemon not ready")
	}

	type sendParams struct {
		Recipient          []string `json:"recipient,omitempty"`
		GroupID            string   `json:"groupId,omitempty"`
		Message            string   `json:"message"`
		Attachments        []string `json:"attachments,omitempty"`
		PreviewURL         string   `json:"previewUrl,omitempty"`
		PreviewTitle       string   `json:"previewTitle,omitempty"`
		PreviewDescription string   `json:"previewDescription,omitempty"`
		PreviewImage       string   `json:"previewImage,omitempty"`
	}
	text = formatSignalMarkup(text)
	previews, cleanupPreview := fetchLinkPreviews(text)
	if cleanupPreview != nil {
		defer cleanupPreview()
	}

	params := sendParams{Message: text}
	if len(previews) > 0 {
		p := previews[0]
		params.PreviewURL = p.URL
		params.PreviewTitle = p.Title
		params.PreviewDescription = p.Description
		params.PreviewImage = p.Image
		// Note: signal-cli handles previewImage as a separate attachment internally;
		// it should not be included in the top-level Attachments array when sent as a preview.
	}
	if strings.HasPrefix(channel, "+") {
		params.Recipient = []string{channel}
	} else {
		params.GroupID = channel
	}
	req := jsonrpcRequest[sendParams]{
		JSONRPC: "2.0",
		Method:  "send",
		Params:  params,
		ID:      c.idSeq.Add(1),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("signal: marshal request: %w", err)
	}
	slog.Debug("signal: send request", "body", string(body))
	body = append(body, '\n')

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("signal: dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("signal: write: %w", err)
	}

	// Read response line.
	var resp jsonrpcResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("signal: read response: %w", err)
	}
	slog.Debug("signal: send response", "result", string(resp.Result))
	if resp.Error != nil {
		return fmt.Errorf("signal: rpc error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// SendTyping sends a typing indicator to a recipient phone number or Signal group.
// channel must be a phone number in E.164 format (starts with "+") for direct
// messages, or a base64-encoded group ID for group conversations.
// Pass stop=true to cancel the indicator.
func (c *SignalChannel) SendTyping(channel string, stop bool) error {
	c.addrMu.RLock()
	addr := c.addr
	c.addrMu.RUnlock()

	if addr == "" {
		return fmt.Errorf("signal: daemon not ready")
	}

	type typingParams struct {
		Recipient []string `json:"recipient,omitempty"`
		GroupID   string   `json:"groupId,omitempty"`
		Stop      bool     `json:"stop"`
	}
	params := typingParams{Stop: stop}
	if strings.HasPrefix(channel, "+") {
		params.Recipient = []string{channel}
	} else {
		params.GroupID = channel
	}

	req := jsonrpcRequest[typingParams]{
		JSONRPC: "2.0",
		Method:  "sendTyping",
		Params:  params,
		ID:      c.idSeq.Add(1),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("signal: marshal request: %w", err)
	}
	body = append(body, '\n')

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("signal: dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("signal: write: %w", err)
	}

	var resp jsonrpcResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("signal: read response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("signal: rpc error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// sendReaction sends a sendReaction JSON-RPC request to signal-cli.
// channel is a phone number (E.164) for 1-to-1 chats or a base64 group ID.
func (c *SignalChannel) sendReaction(channel, emoji, targetAuthor string, targetSentTimestamp int64) error {
	c.addrMu.RLock()
	addr := c.addr
	c.addrMu.RUnlock()

	if addr == "" {
		return fmt.Errorf("signal: daemon not ready")
	}

	type reactionParams struct {
		Recipient           []string `json:"recipient,omitempty"`
		GroupID             string   `json:"groupId,omitempty"`
		Emoji               string   `json:"emoji"`
		TargetAuthor        string   `json:"targetAuthor"`
		TargetSentTimestamp int64    `json:"targetSentTimestamp"`
	}
	params := reactionParams{
		Emoji:               emoji,
		TargetAuthor:        targetAuthor,
		TargetSentTimestamp: targetSentTimestamp,
	}
	if strings.HasPrefix(channel, "+") {
		params.Recipient = []string{channel}
	} else {
		params.GroupID = channel
	}

	req := jsonrpcRequest[reactionParams]{
		JSONRPC: "2.0",
		Method:  "sendReaction",
		Params:  params,
		ID:      c.idSeq.Add(1),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("signal: marshal request: %w", err)
	}
	body = append(body, '\n')

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("signal: dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("signal: write: %w", err)
	}

	var resp jsonrpcResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("signal: read response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("signal: rpc error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// SendMedia sends a file attachment with an optional caption via signal-cli.
// channel must be a phone number in E.164 format or a base64 group ID.
func (c *SignalChannel) SendMedia(channel, caption, filePath string) error {
	c.addrMu.RLock()
	addr := c.addr
	c.addrMu.RUnlock()

	if addr == "" {
		return fmt.Errorf("signal: daemon not ready")
	}

	type sendParams struct {
		Recipient   []string `json:"recipient,omitempty"`
		GroupID     string   `json:"groupId,omitempty"`
		Message     string   `json:"message,omitempty"`
		Attachments []string `json:"attachments"`
	}
	params := sendParams{
		Attachments: []string{filePath},
		Message:     formatSignalMarkup(caption),
	}
	if strings.HasPrefix(channel, "+") {
		params.Recipient = []string{channel}
	} else {
		params.GroupID = channel
	}

	req := jsonrpcRequest[sendParams]{
		JSONRPC: "2.0",
		Method:  "send",
		Params:  params,
		ID:      c.idSeq.Add(1),
	}
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("signal: marshal request: %w", err)
	}
	body = append(body, '\n')

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("signal: dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("signal: write: %w", err)
	}
	var resp jsonrpcResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("signal: read response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("signal: rpc error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// sendReadReceipt sends a read receipt for msgTimestamp to recipient via signal-cli.
// recipient must be a phone number in E.164 format.
func (c *SignalChannel) sendReadReceipt(recipient string, msgTimestamp int64) error {
	c.addrMu.RLock()
	addr := c.addr
	c.addrMu.RUnlock()

	if addr == "" {
		return fmt.Errorf("signal: daemon not ready")
	}

	type receiptParams struct {
		Recipient        string  `json:"recipient"`
		TargetTimestamps []int64 `json:"targetTimestamps"`
		Type             string  `json:"type"`
	}
	params := receiptParams{
		Recipient:        recipient,
		TargetTimestamps: []int64{msgTimestamp},
		Type:             "read",
	}

	req := jsonrpcRequest[receiptParams]{
		JSONRPC: "2.0",
		Method:  "sendReceipt",
		Params:  params,
		ID:      c.idSeq.Add(1),
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("signal: marshal request: %w", err)
	}
	body = append(body, '\n')

	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		return fmt.Errorf("signal: dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("signal: write: %w", err)
	}

	var resp jsonrpcResponse
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return fmt.Errorf("signal: read response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("signal: rpc error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	return nil
}

// Start connects to signal-cli and listens for incoming messages.
// If no daemon address was configured, signal-cli is launched automatically.
// Reconnects on connection loss until ctx is done or Stop is called.
func (c *SignalChannel) Start(ctx context.Context) error {
	if c.initAddr != "" {
		c.runLoop(ctx, c.initAddr)
		return nil
	}

	if c.phone == "" {
		slog.Warn("signal: no phone or daemon address configured; channel inactive")
		<-ctx.Done()
		return nil
	}

	return c.managedLoop(ctx)
}

// Stop disconnects and prevents reconnection.
func (c *SignalChannel) Stop() {
	c.stopOnce.Do(func() { close(c.done) })
}

// DaemonInfo returns info about the signal-cli daemon.
// For managed mode it includes the subprocess PID and start time.
// For external mode (url configured) it returns the configured address with PID=0.
// Returns nil only when in managed mode and the daemon is not currently running.
func (c *SignalChannel) DaemonInfo() *DaemonInfo {
	if c.initAddr != "" {
		// External daemon: show the configured address even though we don't own it.
		return &DaemonInfo{Addr: c.initAddr, External: true}
	}
	c.procMu.RLock()
	pid := c.procPID
	started := c.procStarted
	c.procMu.RUnlock()
	if pid == 0 {
		return nil
	}
	c.addrMu.RLock()
	addr := c.addr
	c.addrMu.RUnlock()
	return &DaemonInfo{PID: pid, Addr: addr, Started: started}
}

// managedLoop launches signal-cli as a subprocess, runs the connect loop,
// and restarts the daemon if it exits unexpectedly.
func (c *SignalChannel) managedLoop(ctx context.Context) error {
	for {
		addr, cmd, err := c.launchDaemon(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			case <-c.done:
				return nil
			default:
			}
			return fmt.Errorf("signal: launch daemon: %w", err)
		}

		c.addrMu.Lock()
		c.addr = addr
		c.addrMu.Unlock()

		c.procMu.Lock()
		c.procPID = cmd.Process.Pid
		c.procStarted = time.Now()
		c.procMu.Unlock()

		slog.Info("signal: managed daemon ready", "addr", addr, "phone", c.phone)
		c.runLoop(ctx, addr)

		// Ensure the process is stopped before waiting.
		cmd.Process.Kill() //nolint:errcheck
		cmd.Wait()         //nolint:errcheck

		c.addrMu.Lock()
		c.addr = ""
		c.addrMu.Unlock()

		c.procMu.Lock()
		c.procPID = 0
		c.procMu.Unlock()

		select {
		case <-ctx.Done():
			return nil
		case <-c.done:
			return nil
		default:
			slog.Warn("signal: managed daemon exited, restarting", "phone", c.phone)
		}
	}
}

// launchDaemon starts signal-cli daemon --tcp on a free local port and polls
// until it accepts TCP connections, then returns the address and the running Cmd.
func (c *SignalChannel) launchDaemon(ctx context.Context) (string, *exec.Cmd, error) {
	// Ask the OS for a free port, then release it for signal-cli to bind.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, fmt.Errorf("find free port: %w", err)
	}
	addr := ln.Addr().String()
	ln.Close() //nolint:errcheck

	// exec.CommandContext kills the process when ctx is cancelled.
	cmd := exec.CommandContext(ctx, "signal-cli", "--account", c.phone, "daemon", "--tcp", addr)

	// Capture stdout+stderr so the output is visible in the daemon log view.
	pr, pw, pipeErr := os.Pipe()
	if pipeErr == nil {
		cmd.Stdout = pw
		cmd.Stderr = pw
	}

	if err := cmd.Start(); err != nil {
		if pipeErr == nil {
			pr.Close() //nolint:errcheck
			pw.Close() //nolint:errcheck
		}
		return "", nil, fmt.Errorf("start signal-cli: %w", err)
	}
	if pipeErr == nil {
		_ = pw.Close()        // parent only needs the read end
		go c.streamToSink(pr) // goroutine exits when process closes its write end
	}
	slog.Info("signal: started managed daemon", "addr", addr, "phone", c.phone, "pid", cmd.Process.Pid)

	// Poll until the TCP server accepts connections (up to 30 s).
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			cmd.Process.Kill() //nolint:errcheck
			cmd.Wait()         //nolint:errcheck
			return "", nil, ctx.Err()
		case <-c.done:
			cmd.Process.Kill() //nolint:errcheck
			cmd.Wait()         //nolint:errcheck
			return "", nil, fmt.Errorf("stopped")
		default:
		}
		if conn, dialErr := net.DialTimeout("tcp", addr, 200*time.Millisecond); dialErr == nil {
			conn.Close() //nolint:errcheck
			return addr, cmd, nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	cmd.Process.Kill() //nolint:errcheck
	cmd.Wait()         //nolint:errcheck
	return "", nil, fmt.Errorf("daemon did not become ready within 30s")
}

// runLoop runs the reconnect loop against a known daemon address.
func (c *SignalChannel) runLoop(ctx context.Context, addr string) {
	for {
		if err := c.listen(ctx, addr); err != nil {
			slog.Warn("signal: connection lost, retrying", "addr", addr, "err", err, "delay", reconnectDelay)
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			case <-time.After(reconnectDelay):
				// reconnect after delay
			}
		} else {
			select {
			case <-ctx.Done():
				return
			case <-c.done:
				return
			default:
				// reconnect immediately after clean close (daemon restarted, etc.)
			}
		}
	}
}

// listen opens a TCP connection to signal-cli and reads incoming
// JSON-RPC notifications until the connection closes, ctx is done, or Stop is called.
func (c *SignalChannel) listen(ctx context.Context, addr string) error {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	slog.Info("signal: connected", "addr", addr, "phone", c.phone)

	// Enable link previews for the account.
	go func() {
		time.Sleep(500 * time.Millisecond) // wait a beat for the daemon to be ready
		type configParams struct {
			LinkPreviews bool `json:"linkPreviews"`
		}
		req := jsonrpcRequest[configParams]{
			JSONRPC: "2.0",
			Method:  "updateConfiguration",
			Params:  configParams{LinkPreviews: true},
			ID:      c.idSeq.Add(1),
		}
		if body, err := json.Marshal(req); err == nil {
			body = append(body, '\n')
			_, _ = conn.Write(body)
		}
	}()

	// Close the connection when context is cancelled or Stop is called.
	go func() {
		select {
		case <-ctx.Done():
		case <-c.done:
		}
		conn.Close() //nolint:errcheck
	}()

	// Read newline-delimited JSON-RPC messages.
	scanner := bufio.NewScanner(conn)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil
		case <-c.done:
			return nil
		default:
		}
		c.dispatch(scanner.Bytes())
	}

	if err := scanner.Err(); err != nil {
		select {
		case <-ctx.Done():
			return nil
		case <-c.done:
			return nil
		default:
			return fmt.Errorf("read: %w", err)
		}
	}
	return nil
}

type jsonrpcRequest[T any] struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  T      `json:"params,omitempty"`
	ID      int64  `json:"id"`
}

type jsonrpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type jsonrpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *jsonrpcError   `json:"error"`
}

// jsonrpcNotification is the top-level shape of a signal-cli JSON-RPC push notification.
type jsonrpcNotification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// signalDataMessage is the dataMessage block inside a signal-cli receive envelope.
type signalDataMessage struct {
	Message  string `json:"message"`
	Mentions []struct {
		Number string `json:"number"`
		UUID   string `json:"uuid"`
	} `json:"mentions"`
	Quote *struct {
		ID     int64  `json:"id"`
		Author string `json:"author"`
		Text   string `json:"text"`
	} `json:"quote"`
	GroupInfo *struct {
		GroupID string `json:"groupId"`
	} `json:"groupInfo"`
}

// receiveParams is the params block of a "receive" notification.
type receiveParams struct {
	Envelope struct {
		Source          string             `json:"source"`
		Timestamp       int64              `json:"timestamp"`
		DataMessage     *signalDataMessage `json:"dataMessage"`
		ReactionMessage *struct {
			Emoji               string `json:"emoji"`
			TargetAuthor        string `json:"targetAuthor"`
			TargetSentTimestamp int64  `json:"targetSentTimestamp"`
			IsRemove            bool   `json:"isRemove"`
		} `json:"reactionMessage"`
	} `json:"envelope"`
}

func (c *SignalChannel) dispatch(line []byte) {
	var notif jsonrpcNotification
	if err := json.Unmarshal(line, &notif); err != nil {
		slog.Debug("signal: parse error", "err", err)
		return
	}
	if notif.Method != "receive" {
		return
	}

	var p receiveParams
	if err := json.Unmarshal(notif.Params, &p); err != nil {
		slog.Debug("signal: parse receive params", "err", err)
		return
	}

	env := p.Envelope

	// Mirror emoji reactions placed on the agent's own messages.
	if r := env.ReactionMessage; r != nil && !r.IsRemove &&
		c.reactToEmoji && c.phone != "" && r.TargetAuthor == c.phone {
		if err := c.sendReaction(env.Source, r.Emoji, r.TargetAuthor, r.TargetSentTimestamp); err != nil {
			slog.Warn("signal: failed to mirror reaction", "err", err)
		}
		return
	}

	// Determine whether this is a reply to one of the agent's own messages.
	isReplyToSelf := c.replyToReplies && c.phone != "" &&
		env.DataMessage != nil &&
		env.DataMessage.Quote != nil &&
		env.DataMessage.Quote.Author == c.phone

	c.dispatchEnvelope(env.Source, env.Timestamp, c.isMentioned(env.DataMessage), isReplyToSelf, env.DataMessage)
}

// isMentioned checks the dataMessage.mentions array for the bot's own phone
// number, which is how signal-cli signals a @mention.
func (c *SignalChannel) isMentioned(dataMessage *signalDataMessage) bool {
	if dataMessage == nil || c.phone == "" {
		return false
	}
	for _, m := range dataMessage.Mentions {
		if m.Number == c.phone {
			return true
		}
	}
	return false
}

func (c *SignalChannel) dispatchEnvelope(source string, msgTimestamp int64, wasMentioned bool, isReplyToSelf bool, dataMessage *signalDataMessage) {
	if dataMessage == nil || dataMessage.Message == "" {
		return
	}

	// Determine group context and channel ID.
	isGroup := dataMessage.GroupInfo != nil
	channelID := source
	if isGroup {
		channelID = dataMessage.GroupInfo.GroupID
	}

	// Use the envelope's wasMentioned field for respondToMentions support.
	// Replies to the agent's own messages bypass the allowFrom filter but still
	// carry any per-entry tool restrictions from a matching entry if one exists.
	var result allowResult
	if isReplyToSelf {
		result.allowed = true
		if r := checkAllowed(c.allowFrom, source, channelID, dataMessage.Message, isGroup, "", wasMentioned); r.allowed {
			result.restrictTools = r.restrictTools
			result.model = r.model
			result.fallbacks = r.fallbacks
		}
	} else {
		result = checkAllowed(c.allowFrom, source, channelID, dataMessage.Message, isGroup, "", wasMentioned)
		if !result.allowed {
			return
		}
	}

	// Send a read receipt so the sender knows the agent saw their message.
	// Receipts always go to the sender's phone number (source), even in groups.
	if c.sendReadReceipts && strings.HasPrefix(source, "+") && msgTimestamp > 0 {
		if err := c.sendReadReceipt(source, msgTimestamp); err != nil {
			slog.Warn("signal: failed to send read receipt", "err", err)
		}
	}

	c.handlerMu.RLock()
	fn := c.handler
	c.handlerMu.RUnlock()

	if fn != nil {
		im := IncomingMessage{
			Type:          "signal",
			From:          source,
			Channel:       channelID,
			Text:          dataMessage.Message,
			RestrictTools: result.restrictTools,
			Model:         result.model,
			Fallbacks:     result.fallbacks,
		}
		if im.Model == "" {
			im.Model = c.model
		}
		if len(im.Fallbacks) == 0 {
			im.Fallbacks = c.fallbacks
		}
		fn(im)
	} else {
		slog.Debug("signal: no handler registered", "from", source)
	}
}
