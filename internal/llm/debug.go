package llm

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"log/slog"
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

// newOAuthClient creates an HTTP client that emulates the Claude CLI's exact
// request headers (X-Stainless-Lang: js, X-Stainless-Runtime: node, etc.) so
// that OAuth tokens grant access to the same models as the official claude binary.
// Debug logging wraps the inner transport when AVIARY_DEBUG_HTTP=1.
func newOAuthClient() *http.Client {
	base := http.DefaultTransport
	if DebugHTTP() {
		base = &debugTransport{base: base}
	}
	return &http.Client{Transport: &cliEmulatingTransport{base: base}}
}

// cliEmulatingTransport overwrites the Go SDK's X-Stainless telemetry headers
// with the values the Claude CLI (Node.js SDK 0.74.0) sends, so that the
// Anthropic server treats these requests identically to official CLI requests.
type cliEmulatingTransport struct{ base http.RoundTripper }

func (t *cliEmulatingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	r.Header.Set("X-Stainless-Lang", "js")
	r.Header.Set("X-Stainless-Runtime", "node")
	r.Header.Set("X-Stainless-Os", "Windows")
	r.Header.Set("X-Stainless-Package-Version", "0.74.0")
	r.Header.Set("X-Stainless-Runtime-Version", "v24.3.0")
	r.Header.Set("X-Stainless-Arch", "x64")
	r.Header.Set("X-Stainless-Timeout", "600")
	// Match the Claude CLI Accept-Encoding exactly. Setting this explicitly
	// disables Go's automatic gzip decompression, so we must handle it ourselves.
	r.Header.Set("Accept-Encoding", "gzip, deflate, br, zstd")

	resp, err := t.base.RoundTrip(r)
	if err != nil || resp == nil {
		return resp, err
	}
	if strings.EqualFold(resp.Header.Get("Content-Encoding"), "gzip") {
		gr, gerr := gzip.NewReader(resp.Body)
		if gerr != nil {
			return resp, gerr
		}
		resp.Body = gr
		resp.Header.Del("Content-Encoding")
		resp.ContentLength = -1
	}
	return resp, nil
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
		fmt.Fprintf(&sb, "    body (%d bytes):\n%s\n", len(reqBody), string(reqBody))
	}
	slog.Debug(sb.String())

	resp, err := t.base.RoundTrip(req)
	if err != nil {
		slog.Debug("[llm debug] <-- ERROR", "err", err)
		return resp, err
	}

	// --- log response ---
	var sb2 strings.Builder
	fmt.Fprintf(&sb2, "[llm debug] <-- %s\n", resp.Status)
	writeHeaders(&sb2, resp.Header, "    ")

	if resp.Body != nil {
		body, _ := io.ReadAll(resp.Body)
		resp.Body = io.NopCloser(bytes.NewReader(body))
		fmt.Fprintf(&sb2, "    body (%d bytes):\n%s\n", len(body), string(body))
	}
	slog.Debug(sb2.String())

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
