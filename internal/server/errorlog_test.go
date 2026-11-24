package server

import (
	"crypto/tls"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lsegal/aviary/internal/config"
)

// captureSlog captures slog output at every level, DEBUG included.
func captureSlog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return buf
}

func TestLogServerErrorLevels(t *testing.T) {
	tests := []struct {
		msg, level, want string
	}{
		{"http: TLS handshake error from [::1]:64825: EOF", "DEBUG", "loopback client closed"},
		{"http: TLS handshake error from 127.0.0.1:64825: EOF", "DEBUG", "loopback client closed"},
		{"http: TLS handshake error from 192.168.1.20:5000: EOF", "WARN", "TLS handshake failed"},
		{"http: TLS handshake error from 127.0.0.1:64924: client sent an HTTP request to an HTTPS server", "WARN", "must use https://"},
		{"http: TLS handshake error from [::1]:1: remote error: tls: bad certificate", "WARN", "bad certificate"},
		{"http: superfluous response.WriteHeader call", "WARN", "superfluous"},
	}
	for _, tt := range tests {
		buf := captureSlog(t)
		logServerError(tt.msg)
		out := buf.String()
		assert.Contains(t, out, "level="+tt.level, tt.msg)
		assert.Contains(t, out, tt.want, tt.msg)
	}
}

// TestServerErrorLogOnTLSListener drives a real TLS listener the way local
// probes do: a bare TCP connect-and-close stays at DEBUG, and a plain HTTP
// request names the client at WARN.
func TestServerErrorLogOnTLSListener(t *testing.T) {
	setupServerDataDir(t)
	cert, err := LoadOrGenerateTLS(config.TLSConfig{})
	require.NoError(t, err)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12})
	require.NoError(t, err)
	srv := &http.Server{Handler: http.NotFoundHandler(), ErrorLog: newServerErrorLog()}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	addr := ln.Addr().String()

	buf := captureSlog(t)
	conn, err := net.Dial("tcp", addr)
	require.NoError(t, err)
	_ = conn.Close()
	require.Eventually(t, func() bool { return strings.Contains(buf.String(), "loopback client closed") }, 5*time.Second, 10*time.Millisecond)
	assert.NotContains(t, buf.String(), "level=WARN")
	assert.NotContains(t, buf.String(), "level=INFO")

	resp, err := http.Get("http://" + addr + "/") //nolint:noctx
	require.NoError(t, err)
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	assert.Contains(t, string(body), "HTTPS")
	require.Eventually(t, func() bool { return strings.Contains(buf.String(), "must use https://") }, 5*time.Second, 10*time.Millisecond)
	assert.Contains(t, buf.String(), "level=WARN")
}
