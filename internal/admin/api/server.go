// Package api hosts the admin endpoint HTTP server. It is the v2.2
// AppService transport for in-process tools (CLI + worker daemon) on
// the same host — see conventions § 0.4 "AppService 唯一入口".
//
// Listener: unix domain socket only. Permissions: file mode 0600,
// owned by the server process uid; no token auth (loopback trust on
// the filesystem boundary). Multi-host TCP transport is filed for
// v2.3 (ADR-0040 reserves the spot but v2.2 does not bind TCP).
//
// This Server is intentionally minimal in v2.2-A1 — it scaffolds the
// listener + 1 health endpoint. v2.2-A2 expands the route surface to
// cover the full CLI command set.
package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Server is the admin endpoint HTTP server bound to a unix socket.
type Server struct {
	socketPath string
	mux        *http.ServeMux
	srv        *http.Server
	listener   net.Listener
}

// NewServer constructs a Server. The socket file at socketPath will
// be created on ListenAndServe and removed on Shutdown.
func NewServer(socketPath string) *Server {
	s := &Server{
		socketPath: socketPath,
		mux:        http.NewServeMux(),
	}
	s.routes()
	s.srv = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Handler exposes the mux (testable without a listener).
func (s *Server) Handler() http.Handler { return s.mux }

// SetHandler swaps the http.Server.Handler so dep-injection
// middleware can wrap the mux for in-process serving. Mirrors
// webconsole/api.Server.SetHandler pattern.
func (s *Server) SetHandler(h http.Handler) {
	s.srv.Handler = h
}

// ListenAndServe binds the unix socket + serves. Returns
// http.ErrServerClosed on graceful Shutdown.
func (s *Server) ListenAndServe() error {
	if s.socketPath == "" {
		return errors.New("admin api: socket path required")
	}
	// Ensure parent dir exists (mode 0700 — owner-only).
	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("admin api: mkdir %q: %w", dir, err)
	}
	// Remove stale socket file from prior crash. We intentionally
	// do NOT guard against "another server is using this socket" —
	// if someone is, the next Listen will fail and the user will see
	// the error; pre-emptively unlinking under a live peer would
	// silently break it.
	if _, err := os.Stat(s.socketPath); err == nil {
		if err := os.Remove(s.socketPath); err != nil {
			return fmt.Errorf("admin api: remove stale socket %q: %w", s.socketPath, err)
		}
	}
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("admin api: listen unix %q: %w", s.socketPath, err)
	}
	// Restrict to owner only (mode 0600). MkdirAll above set the dir
	// to 0700 so this is defense-in-depth.
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = ln.Close()
		return fmt.Errorf("admin api: chmod socket: %w", err)
	}
	s.listener = ln
	return s.srv.Serve(ln)
}

// Shutdown cleanly stops the server + removes the socket file.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	err := s.srv.Shutdown(ctx)
	// Always try to clean the socket file — leaving it stranded
	// breaks the next start with EADDRINUSE.
	if s.socketPath != "" {
		_ = os.Remove(s.socketPath)
	}
	return err
}

// routes registers the v2.2-A1 minimal surface. A2 expands this.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /admin/health", s.healthHandler)
}
