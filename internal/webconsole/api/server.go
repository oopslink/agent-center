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
	addr string
	mux  *http.ServeMux
	srv  *http.Server
	deps Deps
}

// Deps is the dependency bag for handlers.
type Deps struct {
	App AppFacade
	SSE SSEBus
	// SPA is the catch-all handler that serves the embedded React build
	// (web/dist/) for "/" and every non-/api path. Wired by the cli
	// adapter (webconsole_wiring.go) to spa.Handler(). Optional —
	// nil means no SPA mounted (e.g. headless test harness).
	SPA http.Handler
	// Version is the linker-injected build tag (e.g. "v2.4.0") echoed
	// back from /api/health. Empty falls back to "dev" for the
	// `go run` / unversioned path. v2.4-D-X1 fix B10.
	Version string
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

	// v2.6-FE-1: Auth endpoints — exempt from the JWT middleware.
	s.mux.HandleFunc("POST /api/auth/signup", s.signupHandler)
	s.mux.HandleFunc("POST /api/auth/signin", s.signinHandler)
	s.mux.HandleFunc("POST /api/auth/signout", s.signoutHandler)
	s.mux.HandleFunc("GET /api/auth/me", s.meHandler)
	// v2.6-FE-5: Passcode change (requires auth cookie).
	s.mux.HandleFunc("PATCH /api/auth/me/passcode", s.changePasscodeHandler)

	// v2.6-FE-3: Organization CRUD.
	s.mux.HandleFunc("GET /api/orgs", s.listOrgsHandler)
	s.mux.HandleFunc("POST /api/orgs", s.createOrgHandler)
	s.mux.HandleFunc("PATCH /api/orgs/{id}", s.updateOrgHandler)
	s.mux.HandleFunc("DELETE /api/orgs/{id}", s.deleteOrgHandler)

	// v2.6-FE-4: Member management.
	s.mux.HandleFunc("GET /api/members", s.listMembersHandler)
	s.mux.HandleFunc("POST /api/members", s.addMemberHandler)
	s.mux.HandleFunc("POST /api/members/agent", s.addAgentMemberHandler)
	s.mux.HandleFunc("PATCH /api/members/{id}/role", s.changeMemberRoleHandler)
	s.mux.HandleFunc("POST /api/members/{id}/disable", s.disableMemberHandler)
	s.mux.HandleFunc("POST /api/members/{id}/reenable", s.reEnableMemberHandler)

	// Conversations.
	s.mux.HandleFunc("GET /api/conversations", s.listConversationsHandler)
	s.mux.HandleFunc("POST /api/conversations", s.createConversationHandler)
	s.mux.HandleFunc("GET /api/conversations/{id}", s.showConversationHandler)
	s.mux.HandleFunc("GET /api/conversations/{id}/messages", s.listMessagesHandler)
	s.mux.HandleFunc("POST /api/conversations/{id}/messages", s.sendMessageHandler)
	s.mux.HandleFunc("POST /api/conversations/{id}/archive", s.archiveConversationHandler)
	s.mux.HandleFunc("GET /api/conversations/{id}/refs", s.listRefsHandler)
	s.mux.HandleFunc("GET /api/conversations/{id}/unread", s.unreadHandler)
	s.mux.HandleFunc("POST /api/conversations/{id}/seen", s.markSeenHandler)

	// Participants (channel kind only — service enforces).
	s.mux.HandleFunc("POST /api/conversations/{id}/participants", s.inviteParticipantHandler)
	s.mux.HandleFunc("DELETE /api/conversations/{id}/participants/{identity_id}", s.removeParticipantHandler)

	// v2.7 B3-c: the legacy flat Issue/Task HTTP surface (Discussion +
	// TaskRuntime BCs, derive-from-message per ADR-0036) is RETIRED. Issues and
	// Tasks are now ProjectManager work items, exposed only under the nested
	// /api/projects/{project_id}/{issues,tasks} routes below. (The old handler
	// methods remain as dead code pending a follow-up BC removal; they are no
	// longer routed.)

	// Input requests.
	s.mux.HandleFunc("GET /api/input_requests", s.listInputRequestsHandler)
	s.mux.HandleFunc("POST /api/input_requests/{id}/respond", s.respondInputRequestHandler)
	s.mux.HandleFunc("POST /api/input_requests/{id}/cancel", s.cancelInputRequestHandler)

	// Projects — v2.7 B3-c: repointed to the ProjectManager BC (ADR-0046).
	// The legacy Workforce project routes + worker↔project mapping routes are
	// retired (worker↔project is now transitive: Project→Task→AgentWorkItem→
	// Worker). Project is the work-management truth; CRUD is org-scoped.
	s.mux.HandleFunc("GET /api/projects", s.pmListProjectsHandler)
	s.mux.HandleFunc("POST /api/projects", s.pmCreateProjectHandler)
	s.mux.HandleFunc("GET /api/projects/{project_id}", s.pmGetProjectHandler)
	s.mux.HandleFunc("PATCH /api/projects/{project_id}", s.pmUpdateProjectHandler)
	s.mux.HandleFunc("DELETE /api/projects/{project_id}", s.pmArchiveProjectHandler)

	// v2.7 B3 ProjectManager nested routes (ADR-0046). Project-owned resources
	// nest under /api/projects/{project_id}/... so membership gating is uniform
	// (requireProjectMember on the path project). NET-NEW paths — distinct from
	// the legacy flat /api/issues|tasks (B3-c retires those + repoints flat
	// /api/projects to the pm Service).
	s.mux.HandleFunc("GET /api/projects/{project_id}/members", s.pmListMembersHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/members", s.pmAddMemberHandler)
	s.mux.HandleFunc("GET /api/projects/{project_id}/code-repos", s.pmListCodeReposHandler)
	s.mux.HandleFunc("GET /api/projects/{project_id}/issues", s.pmListIssuesHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/issues", s.pmCreateIssueHandler)
	s.mux.HandleFunc("GET /api/projects/{project_id}/issues/{issue_id}", s.pmGetIssueHandler)
	s.mux.HandleFunc("PATCH /api/projects/{project_id}/issues/{issue_id}", s.pmUpdateIssueHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/issues/{issue_id}/transition", s.pmTransitionIssueHandler)
	s.mux.HandleFunc("GET /api/projects/{project_id}/tasks", s.pmListTasksHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks", s.pmCreateTaskHandler)
	s.mux.HandleFunc("GET /api/projects/{project_id}/tasks/{task_id}", s.pmGetTaskHandler)
	s.mux.HandleFunc("PATCH /api/projects/{project_id}/tasks/{task_id}", s.pmUpdateTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/assign", s.pmAssignTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/start", s.pmStartTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/block", s.pmBlockTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/unblock", s.pmUnblockTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/complete", s.pmCompleteTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/verify", s.pmVerifyTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/cancel", s.pmCancelTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/unassign", s.pmUnassignTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/reopen", s.pmReopenTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/subscribe", s.pmSubscribeTaskHandler)
	s.mux.HandleFunc("POST /api/projects/{project_id}/tasks/{task_id}/unsubscribe", s.pmUnsubscribeTaskHandler)

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

	// v2.4-D-F3 fix: AddWorkerModal mints enroll tokens here.
	s.mux.HandleFunc("POST /api/admintoken/mint-enroll", s.mintEnrollHandler)
	s.mux.HandleFunc("POST /api/admintoken/revoke", s.revokeEnrollHandler)

	// v2.4-D-X1 (@oopslink ask): rename worker friendly name.
	s.mux.HandleFunc("PATCH /api/workers/{id}/name", s.workerRenameHandler)

	// v2.5-B2 (#50): re-display the install command for a worker
	// whose enroll token is still alive (not used / expired / revoked).
	s.mux.HandleFunc("GET /api/workers/{id}/install-command", s.showInstallCommandHandler)

	// v2.5-B3 (#51): mint a fresh enroll token for an existing worker
	// whose old enroll token has expired or been burned. 403 if the
	// worker already has a long-term token (daemon already enrolled).
	s.mux.HandleFunc("POST /api/workers/{id}/install-command/re-mint", s.reMintInstallCommandHandler)

	// v2.5-B4 (#52): drop the worker row + revoke its tokens. SSE
	// emits workforce.worker.removed so Fleet rows in other tabs
	// retire automatically.
	s.mux.HandleFunc("DELETE /api/workers/{id}", s.removeWorkerHandler)

	// SPA catch-all. Registered LAST so all the /api/* patterns take
	// precedence. Serves the embedded React build (web/dist/ baked in
	// by go:embed) for "/" + every non-/api path, with index.html
	// fallback so react-router can handle client-side routes on
	// reload + deep link.
	if h := s.deps.SPA; h != nil {
		s.mux.Handle("/", h)
	}
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	v := s.deps.Version
	if v == "" {
		v = "dev"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "ok",
		"version": v,
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
