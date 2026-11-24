package server

import (
	"log"
	"log/slog"
	"net"
	"strings"
)

const tlsHandshakeErrorPrefix = "http: TLS handshake error from "

// newServerErrorLog returns the http.Server error log. net/http logs every
// failed TLS handshake, and local probes that connect and close without a
// handshake (port checks, liveness probes) would otherwise fill the log.
func newServerErrorLog() *log.Logger {
	return log.New(serverErrorLogWriter{}, "", 0)
}

type serverErrorLogWriter struct{}

func (serverErrorLogWriter) Write(p []byte) (int, error) {
	logServerError(strings.TrimSpace(string(p)))
	return len(p), nil
}

// logServerError routes one net/http error log line to slog. A loopback
// client that closes before the handshake logs at DEBUG; a plain HTTP request
// on the TLS port and every other handshake failure stay visible at WARN with
// the client's address.
func logServerError(msg string) {
	rest, ok := strings.CutPrefix(msg, tlsHandshakeErrorPrefix)
	if !ok {
		slog.Warn("server: http: " + strings.TrimPrefix(msg, "http: "))
		return
	}
	remote, reason, _ := strings.Cut(rest, ": ")
	switch {
	case reason == "EOF" && isLoopbackAddr(remote):
		slog.Debug("server: loopback client closed before the TLS handshake", "remote", remote)
	case strings.Contains(reason, "client sent an HTTP request to an HTTPS server"):
		slog.Warn("server: plain HTTP request on the TLS port; the client must use https://", "remote", remote)
	default:
		slog.Warn("server: TLS handshake failed", "remote", remote, "err", reason)
	}
}

func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
