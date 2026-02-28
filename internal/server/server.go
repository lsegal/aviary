// Package server implements the Aviary HTTPS server.
package server

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/lsegal/aviary/internal/config"
	"github.com/lsegal/aviary/internal/mcp"
)

// Server wraps an HTTPS server with token auth and MCP routing.
type Server struct {
	cfg    *config.Config
	token  string
	mux    *http.ServeMux
	httpSrv *http.Server
}

// New creates a new Server with the given config and auth token.
func New(cfg *config.Config, token string) *Server {
	s := &Server{
		cfg:   cfg,
		token: token,
		mux:   http.NewServeMux(),
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	// MCP server (unprotected by bearer — auth is handled per-session by the handler below).
	mcpSrv := mcp.NewServer()
	mcpHandler := mcp.HTTPHandler(mcpSrv)

	// Login does not require auth.
	s.mux.HandleFunc("/api/login", LoginHandler(s.token))

	// MCP endpoint: wrapped in bearer middleware.
	s.mux.Handle("/mcp", BearerMiddleware(s.token, mcpHandler))
	s.mux.Handle("/mcp/", BearerMiddleware(s.token, mcpHandler))

	// Catch-all: protected.
	s.mux.Handle("/", BearerMiddleware(s.token, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Not Found", http.StatusNotFound)
	})))
}

// ListenAndServe starts the HTTPS server on the configured port.
// It returns only when the context is cancelled or an error occurs.
func (s *Server) ListenAndServe(ctx context.Context) error {
	port := s.cfg.Server.Port
	if port == 0 {
		port = 16677
	}

	cert, err := LoadOrGenerateTLS(s.cfg.Server.TLS.Cert, s.cfg.Server.TLS.Key)
	if err != nil {
		return fmt.Errorf("loading TLS: %w", err)
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
	}

	addr := fmt.Sprintf(":%d", port)
	ln, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", addr, err)
	}

	s.httpSrv = &http.Server{Handler: s.mux}

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.httpSrv.Serve(ln)
	}()

	select {
	case <-ctx.Done():
		return s.httpSrv.Shutdown(context.Background())
	case err := <-errCh:
		return err
	}
}

// Addr returns the server address string.
func (s *Server) Addr() string {
	port := s.cfg.Server.Port
	if port == 0 {
		port = 16677
	}
	return fmt.Sprintf("https://localhost:%d", port)
}
