package llm

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
)

// DebugHTTP reports whether HTTP debug logging is enabled via AVIARY_DEBUG_HTTP=1.
func DebugHTTP() bool {
	return os.Getenv("AVIARY_DEBUG_HTTP") == "1"
}

// newDebugClient wraps base (or http.DefaultTransport if nil) with debug
// logging when AVIARY_DEBUG_HTTP=1. Otherwise returns a plain *http.Client
// using base.
func newDebugClient(base http.RoundTripper) *http.Client {
	if base == nil {
		base = http.DefaultTransport
	}
	if !DebugHTTP() {
		return &http.Client{Transport: base}
	}
	return &http.Client{Transport: &debugTransport{base: base}}
}

// debugTransport logs outgoing requests and incoming responses, scrubbing
// credential headers so they never appear in logs.
type debugTransport struct{ base http.RoundTripper }

func (t *debugTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// --- log request ---
	var sb strings.Builder
	fmt.Fprintf(&sb, "[llm debug] --> %s %s\n", req.Method, req.URL)
	writeHeaders(&sb, req.Header, "    ")

	var reqBody []byte
	if req.Body != nil {
		reqBody, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(reqBody))
		fmt.Fprintf(&sb, "    body (%d bytes): %s\n", len(reqBody), truncate(reqBody, 1024))
	}
	log.Print(sb.String())

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		log.Printf("[llm debug] <-- ERROR: %v", err)
		return resp, err
	}

	// --- log response ---
	var sb2 strings.Builder
	fmt.Fprintf(&sb2, "[llm debug] <-- %s\n", resp.Status)
	writeHeaders(&sb2, resp.Header, "    ")

	// Peek at first 2 KB without consuming the rest of the body.
	if resp.Body != nil {
		peek := make([]byte, 2048)
		n, _ := io.ReadFull(resp.Body, peek)
		peek = peek[:n]
		// Stitch peeked bytes back in front of the remaining body stream.
		resp.Body = io.NopCloser(io.MultiReader(bytes.NewReader(peek), resp.Body))
		fmt.Fprintf(&sb2, "    body (first %d bytes): %s\n", n, truncate(peek, 512))
	}
	log.Print(sb2.String())

	return resp, nil
}

// writeHeaders appends sorted, scrubbed headers to sb.
var sensitiveHeaders = map[string]bool{
	"authorization": true,
	"x-api-key":     true,
}

func writeHeaders(sb *strings.Builder, h http.Header, indent string) {
	keys := make([]string, 0, len(h))
	for k := range h {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if sensitiveHeaders[strings.ToLower(k)] {
			fmt.Fprintf(sb, "%s%s: [REDACTED]\n", indent, k)
		} else {
			fmt.Fprintf(sb, "%s%s: %s\n", indent, k, strings.Join(h[k], ", "))
		}
	}
}

func truncate(b []byte, max int) string {
	if len(b) <= max {
		return string(b)
	}
	return string(b[:max]) + fmt.Sprintf(" …+%d bytes", len(b)-max)
}
