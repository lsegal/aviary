package channels

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	nethtml "golang.org/x/net/html"

	"github.com/lsegal/aviary/internal/config"
)

var signalAttachmentFetcher = fetchSignalAttachmentData

var (
	urlRegex               = regexp.MustCompile(`https?://[^\s<>"{}|\\^\x60\[\]]+`)
	maxLinkPreviewHTMLSize = int64(256 * 1024)
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
	z := nethtml.NewTokenizer(io.LimitReader(resp.Body, maxLinkPreviewHTMLSize))
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
// When multiple channels share the same phone number in managed mode, they
// share a single signal-cli subprocess via the package-level daemonHub.
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
	phone         string // registered Signal account phone number
	uuid          string // Signal UUID for this account (populated after connect)
	initAddr      string // configured TCP address; empty → managed daemon mode
	allowFrom     []config.AllowFromEntry
	model         string
	fallbacks     []string
	disabledTools []string

	// Per-channel feature flags (defaults are true).
	showTyping       bool // show typing indicator while agent processes
	reactToEmoji     bool // mirror emoji reactions on agent's own messages
	replyToReplies   bool // respond to quoted replies targeting agent's messages
	sendReadReceipts bool // send read receipts for messages the agent will respond to

	// daemon is set in managed mode; nil in external mode.
	// It is the shared subprocess for this phone number.
	daemon *sharedDaemon

	// addr and addrMu are used in external mode only.
	addrMu sync.RWMutex
	addr   string

	// AgentName is the owning agent's name (set by the manager). Used to
	// replace object-replacement characters in incoming messages with a
	// human-readable agent identifier.
	AgentName string

	handler         func(IncomingMessage)
	groupLogHandler func(IncomingMessage)
	handlerMu       sync.RWMutex
	stopOnce        sync.Once
	done            chan struct{}
	idSeq           atomic.Int64

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

// getAddr returns the current daemon TCP address. In managed mode it reads
// from the shared daemon; in external mode it reads from the channel's own addr.
func (c *SignalChannel) getAddr() string {
	if c.daemon != nil {
		return c.daemon.getAddr()
	}
	c.addrMu.RLock()
	defer c.addrMu.RUnlock()
	return c.addr
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

// OnGroupChatMessage registers a callback invoked for all group messages before
// allowFrom filtering, enabling a full channel transcript to be maintained.
func (c *SignalChannel) OnGroupChatMessage(fn func(IncomingMessage)) {
	c.handlerMu.Lock()
	defer c.handlerMu.Unlock()
	c.groupLogHandler = fn
}

// Send sends a Signal message to a recipient or group via JSON-RPC over TCP.
// channel must be a phone number in E.164 format (starts with "+") for direct
// messages, or a base64-encoded group ID for group conversations.
func (c *SignalChannel) Send(channel, text string) error {
	addr := c.getAddr()
	if addr == "" {
		return fmt.Errorf("signal: daemon not ready")
	}

	type sendParams struct {
		Recipient          []string `json:"recipient,omitempty"`
		GroupID            string   `json:"groupId,omitempty"`
		Message            string   `json:"message"`
		TextStyle          []string `json:"textStyle,omitempty"`
		Attachments        []string `json:"attachments,omitempty"`
		PreviewURL         string   `json:"previewUrl,omitempty"`
		PreviewTitle       string   `json:"previewTitle,omitempty"`
		PreviewDescription string   `json:"previewDescription,omitempty"`
		PreviewImage       string   `json:"previewImage,omitempty"`
	}
	formatted := formatSignalMessage(text)
	previews, cleanupPreview := fetchLinkPreviews(formatted.Text)
	if cleanupPreview != nil {
		defer cleanupPreview()
	}

	params := sendParams{
		Message:   formatted.Text,
		TextStyle: formatted.TextStyles,
	}
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
	addr := c.getAddr()
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
	addr := c.getAddr()
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
	addr := c.getAddr()
	if addr == "" {
		return fmt.Errorf("signal: daemon not ready")
	}

	type sendParams struct {
		Recipient   []string `json:"recipient,omitempty"`
		GroupID     string   `json:"groupId,omitempty"`
		Message     string   `json:"message,omitempty"`
		TextStyle   []string `json:"textStyle,omitempty"`
		Attachments []string `json:"attachments"`
	}
	formatted := formatSignalMessage(caption)
	params := sendParams{
		Attachments: []string{filePath},
		Message:     formatted.Text,
		TextStyle:   formatted.TextStyles,
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
	addr := c.getAddr()
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

func (c *SignalChannel) rpcCallContext(ctx context.Context, body []byte, resp *jsonrpcResponse) error {
	addr := c.getAddr()
	if addr == "" {
		return fmt.Errorf("signal: daemon not ready")
	}

	body = append(body, '\n')

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("signal: dial: %w", err)
	}
	defer conn.Close() //nolint:errcheck

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	if _, err := conn.Write(body); err != nil {
		return fmt.Errorf("signal: write: %w", err)
	}
	if err := json.NewDecoder(conn).Decode(resp); err != nil {
		return fmt.Errorf("signal: read response: %w", err)
	}
	return nil
}

// Start connects to signal-cli and listens for incoming messages.
// In external mode (url configured) it connects directly and reconnects on loss.
// In managed mode it registers with the package-level daemonHub so that all
// channels sharing the same phone number share a single signal-cli subprocess.
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

	// Managed mode: share one signal-cli daemon per phone number.
	d := globalDaemonHub.acquire(c.phone)
	c.daemon = d
	d.addSub(c)
	d.once.Do(func() {
		dCtx, cancel := context.WithCancel(ctx)
		d.cancel = cancel
		go d.run(dCtx)
	})

	select {
	case <-ctx.Done():
	case <-c.done:
	}

	d.removeSub(c)
	globalDaemonHub.release(c.phone)
	return nil
}

// Stop disconnects and prevents reconnection.
func (c *SignalChannel) Stop() {
	c.stopOnce.Do(func() { close(c.done) })
}

// DaemonInfo returns info about the signal-cli daemon.
// For managed mode it reads from the shared daemon (subprocess PID and start time).
// For external mode it returns the configured address with PID=0.
// DaemonInfo returns info about the signal-cli daemon.
// For managed mode it reads from the shared daemon (subprocess PID and start time).
// For external mode it returns the configured address with PID=0.
// Returns nil only when no daemon is configured at all (phone and addr both empty).
func (c *SignalChannel) DaemonInfo() *DaemonInfo {
	if c.initAddr != "" {
		return &DaemonInfo{Addr: c.initAddr, External: true}
	}
	if c.daemon == nil {
		return nil
	}
	c.daemon.procMu.RLock()
	pid := c.daemon.procPID
	started := c.daemon.procStarted
	c.daemon.procMu.RUnlock()
	// Return a non-nil DaemonInfo even when PID==0 so the daemons handler can
	// deduplicate entries for channels sharing the same managed daemon.
	return &DaemonInfo{PID: pid, Addr: c.daemon.getAddr(), Started: started}
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

	// Enable link previews for the account and fetch the account UUID.
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
		c.fetchUUID(ctx)
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
	scanner.Buffer(make([]byte, 0, 64*1024), 2*1024*1024)
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

type signalAttachment struct {
	ContentType    string `json:"contentType"`
	ContentTypeAlt string `json:"content_type"`
	ID             string `json:"id"`
	AttachmentID   string `json:"attachmentId"`
	Filename       string `json:"filename"`
	FileName       string `json:"fileName"`
	StoredFilename string `json:"storedFilename"`
	StoredFileName string `json:"storedFileName"`
	Path           string `json:"path"`
	File           string `json:"file"`
	LocalPath      string `json:"localPath"`
	URL            string `json:"url"`
	RemoteURL      string `json:"remoteUrl"`
	RemoteURLAlt   string `json:"remoteURL"`
	DownloadURL    string `json:"downloadUrl"`
}

// signalDataMessage is the dataMessage block inside a signal-cli receive envelope.
type signalDataMessage struct {
	Message     string             `json:"message"`
	Attachments []signalAttachment `json:"attachments"`
	Mentions    []struct {
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

type signalReactionMessage struct {
	Emoji               string `json:"emoji"`
	TargetAuthor        string `json:"targetAuthor"`
	TargetSentTimestamp int64  `json:"targetSentTimestamp"`
	IsRemove            bool   `json:"isRemove"`
	GroupInfo           *struct {
		GroupID string `json:"groupId"`
	} `json:"groupInfo"`
}

type signalEditMessage struct {
	TargetSentTimestamp int64              `json:"targetSentTimestamp"`
	DataMessage         *signalDataMessage `json:"dataMessage"`
}

// receiveParams is the params block of a "receive" notification.
type receiveParams struct {
	Envelope struct {
		Source          string                 `json:"source"`
		Timestamp       int64                  `json:"timestamp"`
		DataMessage     *signalDataMessage     `json:"dataMessage"`
		EditMessage     *signalEditMessage     `json:"editMessage"`
		ReactionMessage *signalReactionMessage `json:"reactionMessage"`
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

	// Treat emoji reactions on the agent's own messages as direct prompts.
	if r := env.ReactionMessage; r != nil && !r.IsRemove &&
		c.reactToEmoji && c.phone != "" && r.TargetAuthor == c.phone {
		if err := c.sendReaction(env.Source, r.Emoji, r.TargetAuthor, r.TargetSentTimestamp); err != nil {
			slog.Warn("signal: failed to mirror reaction", "err", err)
		}
		c.dispatchReactionEnvelope(env.Source, env.Timestamp, r)
		return
	}

	dataMessage := env.DataMessage
	if env.EditMessage != nil && env.EditMessage.DataMessage != nil {
		dataMessage = env.EditMessage.DataMessage
	}

	isReplyToSelf := c.replyToReplies && c.phone != "" &&
		dataMessage != nil &&
		dataMessage.Quote != nil &&
		dataMessage.Quote.Author == c.phone

	c.dispatchEnvelope(env.Source, env.Timestamp, c.isMentioned(dataMessage), isReplyToSelf, dataMessage)
}

// fetchUUID calls listAccounts on the signal-cli daemon to discover and store
// the UUID for this account. This is needed to detect @mentions in groups
// because newer Signal clients send mentions with UUID only (not phone number).
func (c *SignalChannel) fetchUUID(ctx context.Context) {
	req := jsonrpcRequest[struct{}]{
		JSONRPC: "2.0",
		Method:  "listAccounts",
		ID:      c.idSeq.Add(1),
	}
	body, err := json.Marshal(req)
	if err != nil {
		return
	}
	var resp jsonrpcResponse
	if err := c.rpcCallContext(ctx, body, &resp); err != nil {
		slog.Debug("signal: listAccounts failed", "err", err)
		return
	}
	var accounts []struct {
		Number string `json:"number"`
		UUID   string `json:"uuid"`
	}
	if err := json.Unmarshal(resp.Result, &accounts); err != nil {
		slog.Debug("signal: listAccounts parse failed", "err", err)
		return
	}
	for _, a := range accounts {
		if a.Number == c.phone && a.UUID != "" {
			c.uuid = a.UUID
			slog.Info("signal: resolved account UUID", "phone", c.phone, "uuid", c.uuid)
			return
		}
	}
}

// isMentioned checks the dataMessage.mentions array for the bot's own phone
// number or UUID, which is how signal-cli signals a @mention.
func (c *SignalChannel) isMentioned(dataMessage *signalDataMessage) bool {
	if dataMessage == nil || c.phone == "" {
		return false
	}
	for _, m := range dataMessage.Mentions {
		if m.Number == c.phone || (c.uuid != "" && m.UUID == c.uuid) {
			return true
		}
	}
	return false
}

func (c *SignalChannel) dispatchEnvelope(source string, msgTimestamp int64, wasMentioned bool, isReplyToSelf bool, dataMessage *signalDataMessage) {
	if dataMessage == nil || (dataMessage.Message == "" && len(dataMessage.Attachments) == 0) {
		return
	}

	// Determine group context and channel ID.
	isGroup := dataMessage.GroupInfo != nil
	channelID := source
	if isGroup {
		channelID = dataMessage.GroupInfo.GroupID
	}

	receivedAt := time.Now().UTC()
	if msgTimestamp > 0 {
		receivedAt = time.UnixMilli(msgTimestamp).UTC()
	}

	// Prepare message text, replacing the object-replacement character with
	// the agent name (fallback to the channel phone if agent name not set).
	msgText := dataMessage.Message
	repl := c.AgentName
	if strings.TrimSpace(repl) == "" {
		repl = c.phone
	}
	if strings.Contains(msgText, "\uFFFC") {
		msgText = strings.ReplaceAll(msgText, "\uFFFC", repl)
	}

	// Log all group messages before allowFrom filtering.
	if isGroup {
		c.handlerMu.RLock()
		logFn := c.groupLogHandler
		c.handlerMu.RUnlock()
		if logFn != nil {
			logFn(IncomingMessage{
				Type:       "signal",
				From:       source,
				SenderName: source,
				Channel:    channelID,
				Text:       msgText,
				ReceivedAt: receivedAt,
			})
		}
	}

	// Replies to the agent's own messages must still match an allowFrom entry's
	// sender and group scope; replyToReplies only relaxes mention gating so the
	// user can continue the same allowed conversation without re-mentioning.
	result := checkAllowed(c.allowFrom, source, channelID, msgText, isGroup, "", wasMentioned)
	if isReplyToSelf {
		result = checkAllowedReplyToSelf(c.allowFrom, source, channelID, isGroup)
	}
	if !result.allowed {
		return
	}

	c.handlerMu.RLock()
	fn := c.handler
	c.handlerMu.RUnlock()

	if fn != nil {
		im := IncomingMessage{
			Type:          "signal",
			From:          source,
			SenderName:    source,
			Channel:       channelID,
			Text:          msgText,
			MediaURL:      c.firstSignalImageDataURL(dataMessage.Attachments, source, channelID, isGroup),
			ReceivedAt:    receivedAt,
			RestrictTools: result.restrictTools,
			DisabledTools: c.disabledTools,
			Model:         result.model,
			Fallbacks:     result.fallbacks,
		}
		if dataMessage.Quote != nil {
			im.QuoteAuthor = dataMessage.Quote.Author
			qtext := dataMessage.Quote.Text
			if strings.Contains(qtext, "\uFFFC") {
				qtext = strings.ReplaceAll(qtext, "\uFFFC", repl)
			}
			im.QuoteText = qtext
		}
		if im.Model == "" {
			im.Model = c.model
		}
		if len(im.Fallbacks) == 0 {
			im.Fallbacks = c.fallbacks
		}
		fn(im)
		// Send a read receipt only after the message has been handed off.
		// Receipts always go to the sender's phone number (source), even in groups.
		if c.sendReadReceipts && strings.HasPrefix(source, "+") && msgTimestamp > 0 {
			if err := c.sendReadReceipt(source, msgTimestamp); err != nil {
				slog.Warn("signal: failed to send read receipt", "err", err)
			}
		}
	} else {
		slog.Debug("signal: no handler registered", "from", source)
	}
}

func (c *SignalChannel) firstSignalImageDataURL(attachments []signalAttachment, source, channelID string, isGroup bool) string {
	for _, attachment := range attachments {
		name := signalAttachmentName(attachment.fileName(), attachment.storedPath(), attachment.localPath(), attachment.remoteURL())
		contentType := attachment.mimeType()
		if !looksLikeImage(contentType, name) {
			continue
		}
		for _, localPath := range []string{attachment.localPath(), attachment.storedPath()} {
			localPath = strings.TrimSpace(localPath)
			if localPath == "" {
				continue
			}
			if mediaURL, err := ingestLocalMedia("signal", localPath, name, contentType); err == nil {
				return mediaURL
			}
		}
		if sourceURL := strings.TrimSpace(attachment.remoteURL()); sourceURL != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			mediaURL, err := ingestRemoteMedia(ctx, "signal", sourceURL, name, nil)
			cancel()
			if err == nil {
				return mediaURL
			}
			slog.Warn("signal: failed to ingest remote attachment", "url", sourceURL, "err", err)
		}
		if attachmentID := attachment.id(); attachmentID != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			mediaURL, err := c.ingestSignalAttachmentByID(ctx, attachmentID, source, channelID, isGroup, name, contentType)
			cancel()
			if err == nil {
				return mediaURL
			}
			slog.Warn("signal: failed to ingest attachment by id", "id", attachmentID, "err", err)
		}
	}
	return ""
}

func (a signalAttachment) mimeType() string {
	return firstMeaningfulString(a.ContentType, a.ContentTypeAlt)
}

func (a signalAttachment) id() string {
	return firstMeaningfulString(a.ID, a.AttachmentID)
}

func (a signalAttachment) fileName() string {
	return firstMeaningfulString(a.Filename, a.FileName)
}

func (a signalAttachment) storedPath() string {
	return firstMeaningfulString(a.StoredFilename, a.StoredFileName)
}

func (a signalAttachment) localPath() string {
	return firstMeaningfulString(a.Path, a.File, a.LocalPath)
}

func (a signalAttachment) remoteURL() string {
	return firstMeaningfulString(a.URL, a.RemoteURL, a.RemoteURLAlt, a.DownloadURL)
}

func signalAttachmentName(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if parsed, err := url.Parse(value); err == nil && parsed.Path != "" {
			value = parsed.Path
		}
		base := filepath.Base(value)
		if strings.TrimSpace(base) != "" && base != "." && base != string(filepath.Separator) {
			return base
		}
	}
	return "image"
}

func firstMeaningfulString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func (c *SignalChannel) ingestSignalAttachmentByID(ctx context.Context, attachmentID, source, channelID string, isGroup bool, fileName, contentType string) (string, error) {
	if strings.TrimSpace(c.phone) == "" {
		return "", fmt.Errorf("signal account phone is required")
	}
	data, err := signalAttachmentFetcher(ctx, c, c.phone, attachmentID, source, channelID, isGroup)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(contentType) == "" {
		contentType = http.DetectContentType(data)
	}
	return persistIncomingMedia("signal", fileName, contentType, data)
}

func fetchSignalAttachmentData(ctx context.Context, c *SignalChannel, phone, attachmentID, source, channelID string, isGroup bool) ([]byte, error) {
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
	}
	_ = phone
	type attachmentParams struct {
		ID        string `json:"id"`
		Recipient string `json:"recipient,omitempty"`
		GroupID   string `json:"groupId,omitempty"`
	}
	req := jsonrpcRequest[attachmentParams]{
		JSONRPC: "2.0",
		Method:  "getAttachment",
		Params:  attachmentParams{ID: attachmentID},
		ID:      c.idSeq.Add(1),
	}
	if isGroup {
		req.Params.GroupID = channelID
	} else {
		req.Params.Recipient = source
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("signal: marshal getAttachment request: %w", err)
	}
	var resp jsonrpcResponse
	if err := c.rpcCallContext(ctx, body, &resp); err != nil {
		return nil, err
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("signal: rpc error: %d %s", resp.Error.Code, resp.Error.Message)
	}
	encoded, err := decodeSignalAttachmentResult(resp.Result)
	if err != nil {
		return nil, err
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return nil, fmt.Errorf("decoding attachment %s: %w", attachmentID, err)
	}
	return decoded, nil
}

func decodeSignalAttachmentResult(raw json.RawMessage) (string, error) {
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil && strings.TrimSpace(encoded) != "" {
		return encoded, nil
	}

	var payload struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("signal: decode getAttachment result: %w", err)
	}
	if strings.TrimSpace(payload.Data) == "" {
		return "", fmt.Errorf("signal: decode getAttachment result: empty attachment data")
	}
	return payload.Data, nil
}

func (c *SignalChannel) dispatchReactionEnvelope(source string, msgTimestamp int64, reactionMessage *signalReactionMessage) {
	if reactionMessage == nil || strings.TrimSpace(reactionMessage.Emoji) == "" {
		return
	}

	channelID := source
	if reactionMessage.GroupInfo != nil && reactionMessage.GroupInfo.GroupID != "" {
		channelID = reactionMessage.GroupInfo.GroupID
	}

	result := allowResult{allowed: true}
	if r := checkAllowed(c.allowFrom, source, channelID, reactionMessage.Emoji, channelID != source, "", false); r.allowed {
		result.restrictTools = r.restrictTools
		result.model = r.model
		result.fallbacks = r.fallbacks
	}

	c.handlerMu.RLock()
	fn := c.handler
	c.handlerMu.RUnlock()

	if fn != nil {
		receivedAt := time.Now().UTC()
		if msgTimestamp > 0 {
			receivedAt = time.UnixMilli(msgTimestamp).UTC()
		}
		im := IncomingMessage{
			Type:          "signal",
			From:          source,
			SenderName:    source,
			Channel:       channelID,
			Text:          reactionMessage.Emoji,
			ReceivedAt:    receivedAt,
			RestrictTools: result.restrictTools,
			DisabledTools: c.disabledTools,
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
		if c.sendReadReceipts && strings.HasPrefix(source, "+") && msgTimestamp > 0 {
			if err := c.sendReadReceipt(source, msgTimestamp); err != nil {
				slog.Warn("signal: failed to send read receipt", "err", err)
			}
		}
	} else {
		slog.Debug("signal: no handler registered", "from", source)
	}
}
