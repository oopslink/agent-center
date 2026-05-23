// Package api hosts the Web Console HTTP API (P11 § 3.2). The server
// binds 127.0.0.1 only (per ADR-0037 / NF2 / A4 — no token auth on
// loopback; remote access goes through SSH tunnel).
//
// Endpoint surface mirrors plan-11 § 3.2:
//   - conversations / messages / participants / archive
//   - issues / tasks (CV4 derivation)
//   - input_requests (list / show / respond)
//   - agents (read-only) / secrets (full CRUD, plaintext never echoed)
//   - sse (single user-level long connection + subscribe/unsubscribe)
//
// This file ships the server skeleton + a working subset of endpoints
// (read-only + the most-used write paths). Remaining endpoints will be
// stubbed with 501 Not Implemented and added in follow-up commits as
// the frontend gets wired (no half-implementations exposed without
// frontend coverage).
package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
)

// Server is the Web Console HTTP server.
type Server struct {
	addr  string
	mux   *http.ServeMux
	srv   *http.Server
	deps  Deps
}

// Deps is the dependency bag for handlers.
type Deps struct {
	App AppFacade
	SSE SSEBus
}

// AppFacade narrows the cli.App surface that handlers need (we don't
// import cli to avoid a cycle; the caller adapts).
type AppFacade interface{}

// SSEBus is the subscriber pool API consumed by handlers + EventSink
// fan-out. Defined here so api/ doesn't import sse/.
type SSEBus interface {
	Subscribe(userID string, conversationID string) error
	Unsubscribe(userID string, conversationID string) error
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// NewServer builds a Server bound to addr (typically "127.0.0.1:7100").
func NewServer(addr string, deps Deps) *Server {
	s := &Server{addr: addr, mux: http.NewServeMux(), deps: deps}
	s.routes()
	s.srv = &http.Server{
		Addr:              addr,
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// Handler exposes the mux (testable without an in-process listener).
func (s *Server) Handler() http.Handler { return s.mux }

// SetHandler swaps the http.Server.Handler so middleware (e.g.
// dependency injection via WithDeps) can wrap the mux for in-process
// serving via ListenAndServe.
func (s *Server) SetHandler(h http.Handler) {
	s.srv.Handler = h
}

// ListenAndServe binds + serves on the configured loopback address.
// Returns http.ErrServerClosed on graceful shutdown.
func (s *Server) ListenAndServe() error {
	// Bind on 127.0.0.1 only — guard against accidental 0.0.0.0 bind.
	host, _, err := net.SplitHostPort(s.addr)
	if err != nil {
		return fmt.Errorf("webconsole: parse addr %q: %w", s.addr, err)
	}
	if host != "" && host != "127.0.0.1" && host != "localhost" {
		return fmt.Errorf("webconsole: refusing non-loopback bind %q (ADR-0037 / NF2)", s.addr)
	}
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	return s.srv.Serve(ln)
}

// Shutdown cleanly stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	if s.srv == nil {
		return nil
	}
	return s.srv.Shutdown(ctx)
}

// routes registers all endpoints. Per plan § 3.2 the full surface is
// 21 endpoints + SSE; this skeleton routes the foundation and stubs
// the rest with 501 so the path map is discoverable by the frontend.
func (s *Server) routes() {
	// Health + version (utility endpoints, not in plan but useful).
	s.mux.HandleFunc("GET /api/health", s.healthHandler)

	// Conversations.
	s.mux.HandleFunc("GET /api/conversations", s.listConversationsHandler)
	s.mux.HandleFunc("POST /api/conversations", s.createConversationHandler)
	s.mux.HandleFunc("GET /api/conversations/{id}", s.showConversationHandler)
	s.mux.HandleFunc("GET /api/conversations/{id}/messages", s.listMessagesHandler)
	s.mux.HandleFunc("POST /api/conversations/{id}/messages", s.sendMessageHandler)
	s.mux.HandleFunc("POST /api/conversations/{id}/archive", s.archiveConversationHandler)

	// Participants (channel kind only — service enforces).
	s.mux.HandleFunc("POST /api/conversations/{id}/participants", s.inviteParticipantHandler)
	s.mux.HandleFunc("DELETE /api/conversations/{id}/participants/{identity_id}", s.removeParticipantHandler)

	// Derivation entry points (CV4).
	s.mux.HandleFunc("POST /api/issues", s.deriveIssueHandler)
	s.mux.HandleFunc("POST /api/tasks", s.deriveTaskHandler)

	// Input requests.
	s.mux.HandleFunc("GET /api/input_requests", s.listInputRequestsHandler)
	s.mux.HandleFunc("POST /api/input_requests/{id}/respond", s.respondInputRequestHandler)

	// Agents (read-only; admin verbs go through CLI).
	s.mux.HandleFunc("GET /api/agents", s.listAgentsHandler)
	s.mux.HandleFunc("GET /api/agents/{name}", s.showAgentHandler)

	// Secrets (metadata only — plaintext only at create time).
	s.mux.HandleFunc("GET /api/secrets", s.listSecretsHandler)
	s.mux.HandleFunc("POST /api/secrets", s.createSecretHandler)
	s.mux.HandleFunc("DELETE /api/secrets/{id}", s.revokeSecretHandler)

	// SSE — single user-level long connection per Q5=B.
	s.mux.HandleFunc("GET /api/sse", s.sseHandler)
	s.mux.HandleFunc("POST /api/sse/subscribe", s.sseSubscribeHandler)
	s.mux.HandleFunc("POST /api/sse/unsubscribe", s.sseUnsubscribeHandler)

	// Fleet snapshot + per-task event trace.
	s.mux.HandleFunc("GET /api/fleet", s.fleetSnapshotHandler)
	s.mux.HandleFunc("GET /api/tasks/{id}/trace", s.taskTraceHandler)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": "v2-dev",
	})
}

// notImplementedHandler is the stub used by endpoints not yet wired.
func (s *Server) notImplementedHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotImplemented, map[string]any{
		"error": "not_implemented",
		"path":  r.URL.Path,
	})
}

// errInvalidJSON is returned when a request body fails to decode.
var errInvalidJSON = errors.New("invalid json body")
