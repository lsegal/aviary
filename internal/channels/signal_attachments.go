package channels

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	signalAttachmentFetcher = fetchSignalAttachmentData
	signalAttachmentDirs    = defaultSignalAttachmentDirs

	// signalAttachmentFetchTimeout bounds every attempt to fetch one
	// attachment by id, retries included. Attachments are ingested before a
	// message is handed off, so this also bounds how long a message waits.
	signalAttachmentFetchTimeout = 5 * time.Second
	// signalAttachmentRetryDelay is the first backoff between attempts; it
	// doubles after each failed attempt.
	signalAttachmentRetryDelay = 250 * time.Millisecond
)

// signalAttachmentResultFields are the result fields signal-cli has used for
// getAttachment's base64 payload, in the order they are tried.
var signalAttachmentResultFields = []string{"data", "base64", "content", "attachment", "file"}

func (c *SignalChannel) signalAttachments(attachments []signalAttachment, source, channelID string, isGroup bool) []Attachment {
	var out []Attachment
	var failedKinds []string
	for _, attachment := range attachments {
		att := c.ingestSignalAttachment(attachment, source, channelID, isGroup)
		if att.Err != "" {
			failedKinds = appendUnique(failedKinds, signalAttachmentKind(attachment.mimeType()))
		}
		out = append(out, att)
	}
	if len(failedKinds) > 0 {
		go c.notifyAttachmentFailure(channelID, failedKinds)
	}
	return out
}

// notifyAttachmentFailure asks the sender to resend attachments that could
// not be received, so the failure is not silent on their side.
func (c *SignalChannel) notifyAttachmentFailure(channelID string, kinds []string) {
	text := fmt.Sprintf("Couldn't receive your %s, please resend.", strings.Join(kinds, " and "))
	if err := c.Send(channelID, text); err != nil {
		slog.Warn("signal: failed to notify sender about attachment failure", "channel", channelID, "err", err)
	}
}

// signalAttachmentKind names an attachment for the sender: images, voice
// notes, videos, and everything else as files.
func signalAttachmentKind(contentType string) string {
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return "image"
	case strings.HasPrefix(contentType, "audio/"):
		return "voice note"
	case strings.HasPrefix(contentType, "video/"):
		return "video"
	}
	return "file"
}

func appendUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}

func (c *SignalChannel) ingestSignalAttachment(attachment signalAttachment, source, channelID string, isGroup bool) Attachment {
	name := signalAttachmentName(attachment.fileName(), attachment.storedPath(), attachment.localPath(), attachment.remoteURL())
	contentType := attachment.mimeType()
	lastErr := fmt.Errorf("no attachment source")
	for _, localPath := range []string{attachment.localPath(), attachment.storedPath()} {
		localPath = strings.TrimSpace(localPath)
		if localPath == "" {
			continue
		}
		att, err := ingestLocalMedia("signal", localPath, name, contentType)
		if err == nil {
			return att
		}
		lastErr = err
	}
	if sourceURL := strings.TrimSpace(attachment.remoteURL()); sourceURL != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		att, err := ingestRemoteMedia(ctx, "signal", sourceURL, name, contentType, nil)
		cancel()
		if err == nil {
			return att
		}
		lastErr = err
		slog.Warn("signal: failed to ingest remote attachment", "url", sourceURL, "err", err)
	}
	attachmentID := attachment.id()
	if attachmentID != "" {
		att, err := c.ingestSignalAttachmentByID(attachmentID, source, channelID, isGroup, name, contentType)
		if err == nil {
			return att
		}
		lastErr = err
	}
	slog.Error("signal: attachment could not be fetched",
		"id", attachmentID, "name", name, "contentType", contentType, "size", attachment.Size, "err", lastErr)
	return failedAttachment(name, contentType, attachment.Size, lastErr)
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
	return "attachment"
}

func firstMeaningfulString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// ingestSignalAttachmentByID fetches an attachment through getAttachment,
// falling back to signal-cli's local attachment store when the RPC fails.
// A failed attempt is retried with backoff until signalAttachmentFetchTimeout,
// since signal-cli may still be downloading an attachment it just announced.
func (c *SignalChannel) ingestSignalAttachmentByID(attachmentID, source, channelID string, isGroup bool, fileName, contentType string) (Attachment, error) {
	if strings.TrimSpace(c.phone) == "" {
		return Attachment{}, fmt.Errorf("signal account phone is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), signalAttachmentFetchTimeout)
	defer cancel()

	delay := signalAttachmentRetryDelay
	for attempt := 1; ; attempt++ {
		data, err := signalAttachmentFetcher(ctx, c, c.phone, attachmentID, source, channelID, isGroup)
		if err == nil {
			return persistIncomingMedia("signal", fileName, contentType, data)
		}
		slog.Warn("signal: getAttachment failed", "id", attachmentID, "attempt", attempt, "err", err)
		if localPath := findSignalLocalAttachment(attachmentID); localPath != "" {
			att, localErr := ingestLocalMedia("signal", localPath, fileName, contentType)
			if localErr == nil {
				return att, nil
			}
			err = errors.Join(err, localErr)
		}
		select {
		case <-ctx.Done():
			return Attachment{}, err
		case <-time.After(delay):
		}
		delay *= 2
	}
}

// defaultSignalAttachmentDirs lists where signal-cli stores downloaded
// attachments: $XDG_DATA_HOME/signal-cli/attachments, defaulting to
// ~/.local/share/signal-cli/attachments.
func defaultSignalAttachmentDirs() []string {
	var dirs []string
	if xdg := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); xdg != "" {
		dirs = append(dirs, filepath.Join(xdg, "signal-cli", "attachments"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		dirs = appendUnique(dirs, filepath.Join(home, ".local", "share", "signal-cli", "attachments"))
	}
	return dirs
}

// findSignalLocalAttachment returns the path of attachmentID in signal-cli's
// attachment store, or "" when it is not there. Ids name a file directly,
// sometimes without the extension the stored file has.
func findSignalLocalAttachment(attachmentID string) string {
	if attachmentID == "" || attachmentID != filepath.Base(attachmentID) || attachmentID == ".." || strings.ContainsAny(attachmentID, `/\*?[`) {
		return ""
	}
	for _, dir := range signalAttachmentDirs() {
		candidate := filepath.Join(dir, attachmentID)
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			return candidate
		}
		matches, _ := filepath.Glob(candidate + ".*")
		for _, match := range matches {
			if info, err := os.Stat(match); err == nil && info.Mode().IsRegular() {
				return match
			}
		}
	}
	return ""
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
	if err := c.rpcCallContext(ctx, req.ID, body, &resp); err != nil {
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

// decodeSignalAttachmentResult extracts the base64 payload from a
// getAttachment result: a bare string, or an object carrying it in one of
// signalAttachmentResultFields.
func decodeSignalAttachmentResult(raw json.RawMessage) (string, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return "", fmt.Errorf("signal: decode getAttachment result: empty result")
	}
	var encoded string
	if err := json.Unmarshal(raw, &encoded); err == nil {
		if strings.TrimSpace(encoded) == "" {
			return "", fmt.Errorf("signal: decode getAttachment result: empty attachment data")
		}
		return encoded, nil
	}

	var payload map[string]json.RawMessage
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", fmt.Errorf("signal: decode getAttachment result %s: %w", truncateForLog(raw), err)
	}
	for _, field := range signalAttachmentResultFields {
		value, ok := payload[field]
		if !ok {
			continue
		}
		var s string
		if err := json.Unmarshal(value, &s); err == nil && strings.TrimSpace(s) != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("signal: decode getAttachment result %s: empty attachment data", truncateForLog(raw))
}

// truncateForLog shortens a raw JSON-RPC payload for error messages.
func truncateForLog(raw []byte) string {
	const limit = 200
	if len(raw) <= limit {
		return string(raw)
	}
	return string(raw[:limit]) + "…"
}
