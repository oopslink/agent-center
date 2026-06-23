// Package api hosts the Web Console HTTP API (P11 § 3.2). The server
// binds 127.0.0.1 only (per ADR-0037 / NF2 / A4 — no token auth on
// loopback; remote access goes through SSH tunnel).
//
// Endpoint surface mirrors plan-11 § 3.2:
//   - conversations / messages / participants / archive
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

	"github.com/oopslink/agent-center/internal/files"
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
	// Branch / Commit / BuiltAt are the rest of the build identity
	// (v2.8.1 version convention ${branch}-${commit}) echoed from
	// GET /api/system/version for the Settings version panel.
	Branch  string
	Commit  string
	BuiltAt string
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
	// v2.8.1: full build identity for the Settings version panel.
	s.mux.HandleFunc("GET /api/system/version", s.systemVersionHandler)
	// I7-D1 (T216): wake-guardrail config — read effective thresholds, write
	// overrides (validated > 0); consumed by the I7-D3 Settings panel.
	s.mux.HandleFunc("GET /api/system/wake-guardrail", s.getWakeGuardrailHandler)
	s.mux.HandleFunc("PUT /api/system/wake-guardrail", s.putWakeGuardrailHandler)

	// v2.6-FE-1: Auth endpoints — exempt from the JWT middleware.
	// v2.7 #145: public bootstrap-status probe — lets the SPA decide signup
	// (fresh install) vs signin without an authenticated /api/orgs 401 bounce.
	s.mux.HandleFunc("GET /api/auth/bootstrap", s.bootstrapHandler)
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

	// v2.6-FE-4: Member management. v2.9 org-routing-explicit: org via {slug} path.
	s.mux.HandleFunc("GET /api/orgs/{slug}/members", s.listMembersHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/members", s.addMemberHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/members/agent", s.addAgentMemberHandler)
	s.mux.HandleFunc("PATCH /api/orgs/{slug}/members/{id}/role", s.changeMemberRoleHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/members/{id}/disable", s.disableMemberHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/members/{id}/reenable", s.reEnableMemberHandler)
	// v2.7.1 #214: user profile detail (member-id path; Humans row → UserDetail).
	// EXEMPT (org-agnostic): cross-org profile — lists every org the user belongs
	// to + their per-org role; authenticated only, no requireOrgMember gate.
	s.mux.HandleFunc("GET /api/users/{user_id}", s.userDetailHandler)

	// Conversations. v2.9 org-routing-explicit: org carried by {slug} path.
	s.mux.HandleFunc("GET /api/orgs/{slug}/conversations", s.listConversationsHandler)
	// I23 (T332): cross-source "unread conversations" digest for the main sidebar
	// (registered before the /{id} routes so the static path isn't shadowed).
	s.mux.HandleFunc("GET /api/orgs/{slug}/unread-conversations", s.listUnreadConversationsHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/unread-conversations/mark-all-read", s.markAllUnreadConversationsSeenHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/conversations", s.createConversationHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/conversations/{id}", s.showConversationHandler)
	// v2.7 #198: hard-delete a DM (channels use archive → 400 use_archive).
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/conversations/{id}", s.deleteConversationHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/conversations/{id}/messages", s.listMessagesHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/conversations/{id}/messages", s.sendMessageHandler)
	// v2.9.1 Thread P1: read a thread's replies (children only, ordered).
	s.mux.HandleFunc("GET /api/orgs/{slug}/conversations/{id}/messages/{rootId}/replies", s.listThreadRepliesHandler)
	// v2.9.1 Thread P2: list the conversation's threads (Participants sidebar).
	s.mux.HandleFunc("GET /api/orgs/{slug}/conversations/{id}/threads", s.listThreadsHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/conversations/{id}/archive", s.archiveConversationHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/conversations/{id}/refs", s.listRefsHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/conversations/{id}/unread", s.unreadHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/conversations/{id}/seen", s.markSeenHandler)
	// v2.8 #268: follow / unfollow (badge model). POST = follow,
	// DELETE = unfollow; auto-follow happens on participate / @mention.
	s.mux.HandleFunc("POST /api/orgs/{slug}/conversations/{id}/follow", s.followConversationHandler)
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/conversations/{id}/follow", s.unfollowConversationHandler)

	// Participants (channel kind only — service enforces).
	s.mux.HandleFunc("POST /api/orgs/{slug}/conversations/{id}/participants", s.inviteParticipantHandler)
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/conversations/{id}/participants/{identity_id}", s.removeParticipantHandler)

	// v2.7 B3-c: the legacy flat Issue/Task HTTP surface (Discussion +
	// TaskRuntime BCs, derive-from-message per ADR-0036) is RETIRED. Issues and
	// Tasks are now ProjectManager work items, exposed only under the nested
	// /api/projects/{project_id}/{issues,tasks} routes below. (The old handler
	// methods remain as dead code pending a follow-up BC removal; they are no
	// longer routed.)

	// Projects — v2.7 B3-c: repointed to the ProjectManager BC (ADR-0046).
	// The legacy Workforce project routes + worker↔project mapping routes are
	// retired (worker↔project is now transitive: Project→Task→AgentWorkItem→
	// Worker). Project is the work-management truth; CRUD is org-scoped.
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects", s.pmListProjectsHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects", s.pmCreateProjectHandler)
	// v2.8 #258/#260: org-scoped cross-project work-item aggregation (Sidebar >
	// Workspace > Issues/Tasks). Org via requireOrgMember ({slug} path).
	s.mux.HandleFunc("GET /api/orgs/{slug}/issues", s.pmListOrgIssuesHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/tasks", s.pmListOrgTasksHandler)
	// v2.10.0 [T6]: org-scoped cross-project Plan list (global Workspace > Plan).
	s.mux.HandleFunc("GET /api/orgs/{slug}/plans", s.pmListOrgPlansHandler)
	// T207: human Reminder CRUD (Cognition BC) — org-scoped, session-authed.
	s.mux.HandleFunc("GET /api/orgs/{slug}/reminders", s.remListHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/reminders", s.remCreateHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/reminders/{reminder_id}", s.remGetHandler)
	s.mux.HandleFunc("PATCH /api/orgs/{slug}/reminders/{reminder_id}", s.remUpdateHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{project_id}", s.pmGetProjectHandler)
	s.mux.HandleFunc("PATCH /api/orgs/{slug}/projects/{project_id}", s.pmUpdateProjectHandler)
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/projects/{project_id}", s.pmArchiveProjectHandler)

	// v2.7 B3 ProjectManager nested routes (ADR-0046). Project-owned resources
	// nest under /api/projects/{project_id}/... so membership gating is uniform
	// (requireProjectMember on the path project). NET-NEW paths — distinct from
	// the legacy flat /api/issues|tasks (B3-c retires those + repoints flat
	// /api/projects to the pm Service).
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{project_id}/members", s.pmListMembersHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/members", s.pmAddMemberHandler)
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/projects/{project_id}/members/{identity_id}", s.pmRemoveMemberHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{project_id}/code-repos", s.pmListCodeReposHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{project_id}/issues", s.pmListIssuesHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/issues", s.pmCreateIssueHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{project_id}/issues/{issue_id}", s.pmGetIssueHandler)
	s.mux.HandleFunc("PATCH /api/orgs/{slug}/projects/{project_id}/issues/{issue_id}", s.pmBatchUpdateIssueHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/issues/{issue_id}/transition", s.pmTransitionIssueHandler)
	// v2.8.1: free status-set (any valid target, no adjacency) — the full-enum
	// Change-status menu. Symmetric task + issue.
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/issues/{issue_id}/status", s.pmSetIssueStatusHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{project_id}/tasks", s.pmListTasksHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks", s.pmCreateTaskHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}", s.pmGetTaskHandler)
	s.mux.HandleFunc("PATCH /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}", s.pmBatchUpdateTaskHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/assign", s.pmAssignTaskHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/start", s.pmStartTaskHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/block", s.pmBlockTaskHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/unblock", s.pmUnblockTaskHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/complete", s.pmCompleteTaskHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/discard", s.pmDiscardTaskHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/unassign", s.pmUnassignTaskHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/reopen", s.pmReopenTaskHandler)
	// v2.8.1: free status-set (any valid target, no adjacency) — the full-enum
	// Change-status menu. The typed endpoints above remain for the agent's
	// structured self-reports.
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/status", s.pmSetTaskStatusHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/subscribe", s.pmSubscribeTaskHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/tasks/{task_id}/unsubscribe", s.pmUnsubscribeTaskHandler)

	// v2.9 Plan Orchestration (#285). Plans nest under the project; node status is
	// DERIVED in the GET DTO (§9.2). Edits (select/deps/patch) are draft-only (§9.4);
	// start (§9.6 validation) / stop (§9.4) / advance (§9.3 idempotent dispatch).
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{project_id}/plans", s.pmListPlansHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/plans", s.pmCreatePlanHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}", s.pmGetPlanHandler)
	// v2.13.0 / I18 F4 — unmerged-branch board (un-done Integrate nodes).
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/unmerged-branches", s.pmListUnmergedBranchesHandler)
	s.mux.HandleFunc("PATCH /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}", s.pmUpdatePlanHandler)
	// v2.9 P3: hard-delete (non-running; unloads tasks to backlog + deletes the
	// plan + its conversation) and archive (non-running; cascade-archives tasks,
	// irreversible). Running → 409.
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}", s.pmDeletePlanHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/archive", s.pmArchivePlanHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/tasks", s.pmSelectTaskHandler)
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/tasks/{task_id}", s.pmRemoveTaskHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/dependencies", s.pmAddDependencyHandler)
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/dependencies", s.pmRemoveDependencyHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/start", s.pmStartPlanHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/stop", s.pmStopPlanHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/advance", s.pmAdvancePlanHandler)
	// T53: operator resume of a paused plan node (un-stick a set-aside node).
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{project_id}/plans/{plan_id}/nodes/{task_id}/resume", s.pmResumePausedNodeHandler)

	// Agents (read-only; admin verbs go through CLI).
	// v2.7 C3 Agent BC (ADR-0049). Org-scoped; replaces the legacy
	// workforce.AgentInstance /api/agents routes (retired — old handlers are
	// dead code, that backend retires with #107). {name}→{id} forces a repoint.
	s.mux.HandleFunc("GET /api/orgs/{slug}/agents", s.agentListHandler)
	// v2.7 #185 / no-middle-state: POST /api/agents (standalone agent WITHOUT an
	// identity member) is removed. The single creation entry is POST
	// /api/orgs/{slug}/members/agent, which atomically provisions identity+member+
	// execution agent (#157) so an agent ALWAYS has a member id (business-layer id).
	s.mux.HandleFunc("GET /api/orgs/{slug}/agents/{id}", s.agentGetHandler)
	// v2.7 #197: hard-delete an agent (stopped + idle) + cascade its identity-member.
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/agents/{id}", s.agentDeleteHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/agents/{id}/start", s.agentStartHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/agents/{id}/stop", s.agentStopHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/agents/{id}/restart", s.agentRestartHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/agents/{id}/reset", s.agentResetHandler)
	// T236: edit LLM config (model/cli/reasoning/mode/provider); applies on restart.
	s.mux.HandleFunc("PATCH /api/orgs/{slug}/agents/{id}/config", s.agentUpdateConfigHandler)
	// v2.8 #272: soft-delete (archive) — the sole user-facing delete path
	// (hard DELETE above is admin-only). Idempotent; running → 409 must-stop-first.
	s.mux.HandleFunc("POST /api/orgs/{slug}/agents/{id}/archive", s.agentArchiveHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/agents/{id}/tasks", s.agentTasksHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/agents/{id}/activity", s.agentActivityHandler)

	// Secrets (metadata only — plaintext only at create time). Org via {slug} path.
	s.mux.HandleFunc("GET /api/orgs/{slug}/secrets", s.listSecretsHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/secrets", s.createSecretHandler)
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/secrets/{id}", s.revokeSecretHandler)

	// v2.7 D3-d: files transport (human upload/download). Upload is a 3-call
	// create→put→complete flow; download is reachability-gated (fail-closed).
	// The literal `transfer` segment is more specific than `{ulid}`, so
	// PUT/complete and GET-download never collide on the matcher. Org via {slug}.
	s.mux.HandleFunc("POST /api/orgs/{slug}/files", s.createUploadHandler)
	s.mux.HandleFunc("PUT /api/orgs/{slug}/files/transfer/{transfer_id}", s.putBlobHandler)
	s.mux.HandleFunc("POST /api/orgs/{slug}/files/transfer/{transfer_id}/complete", s.completeUploadHandler)
	// v2.7 E1 #139: in-flight file-transfer sessions, org-scoped via scope→org
	// fail-closed resolution (ListOpen = open + unexpired, no global cap).
	// Registered before /files/{ulid} so the literal `transfers` wins the match.
	s.mux.HandleFunc("GET /api/orgs/{slug}/files/transfers", s.listTransfersHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/files/{ulid}", s.downloadHandler)

	// v2.10.0 [T73]: task/issue-scoped file attachments (Task/Issue detail pages).
	// List + create-upload + complete, project-member-gated; blob PUT reuses the
	// generic transfer route above and download reuses GET /files/{ulid}.
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{pid}/tasks/{tid}/files", s.scopeFilesListHandler(files.ScopeTask, "tid"))
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{pid}/tasks/{tid}/files", s.scopeFilesCreateHandler(files.ScopeTask, "tid"))
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{pid}/tasks/{tid}/files/transfer/{transfer_id}/complete", s.scopeFilesCompleteHandler(files.ScopeTask, "tid"))
	s.mux.HandleFunc("GET /api/orgs/{slug}/projects/{pid}/issues/{iid}/files", s.scopeFilesListHandler(files.ScopeIssue, "iid"))
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{pid}/issues/{iid}/files", s.scopeFilesCreateHandler(files.ScopeIssue, "iid"))
	s.mux.HandleFunc("POST /api/orgs/{slug}/projects/{pid}/issues/{iid}/files/transfer/{transfer_id}/complete", s.scopeFilesCompleteHandler(files.ScopeIssue, "iid"))

	// SSE — single user-level long connection per Q5=B. EXEMPT (user-level, not
	// org-scoped): subscribe/connect key on user_id + conversation_id.
	s.mux.HandleFunc("GET /api/sse", s.sseHandler)
	s.mux.HandleFunc("POST /api/sse/subscribe", s.sseSubscribeHandler)
	s.mux.HandleFunc("POST /api/sse/unsubscribe", s.sseUnsubscribeHandler)

	// Fleet snapshot. Org via {slug} path.
	s.mux.HandleFunc("GET /api/orgs/{slug}/fleet", s.fleetSnapshotHandler)

	// v2.4-D-F3 fix: AddWorkerModal mints enroll tokens here (org-scoped — stamps
	// the org on the minted worker).
	s.mux.HandleFunc("POST /api/orgs/{slug}/admintoken/mint-enroll", s.mintEnrollHandler)
	// EXEMPT: revoke is a fire-and-forget token-id close path with no org gate.
	s.mux.HandleFunc("POST /api/admintoken/revoke", s.revokeEnrollHandler)

	// v2.7 E1 #138 (Environment domain page): org-scoped READS of the org's
	// workers. v2.7 #140 step-2: sourced from canonical workforce.Worker
	// (enrolled set) — list is FindAll + in-handler org-filter; detail is
	// fetch-then-check-org (cross-org id → 404, E-10b).
	s.mux.HandleFunc("GET /api/orgs/{slug}/workers", s.listWorkersHandler)
	s.mux.HandleFunc("GET /api/orgs/{slug}/workers/{id}", s.getWorkerHandler)

	// v2.4-D-X1 (@oopslink ask): rename worker friendly name.
	s.mux.HandleFunc("PATCH /api/orgs/{slug}/workers/{id}/name", s.workerRenameHandler)

	// v2.5-B2 (#50): re-display the install command for a worker
	// whose enroll token is still alive (not used / expired / revoked).
	s.mux.HandleFunc("GET /api/orgs/{slug}/workers/{id}/install-command", s.showInstallCommandHandler)

	// v2.5-B3 (#51): mint a fresh enroll token for an existing worker
	// whose old enroll token has expired or been burned. 403 if the
	// worker already has a long-term token (daemon already enrolled).
	s.mux.HandleFunc("POST /api/orgs/{slug}/workers/{id}/install-command/re-mint", s.reMintInstallCommandHandler)

	// v2.5-B4 (#52): drop the worker row + revoke its tokens. SSE
	// emits workforce.worker.removed so Fleet rows in other tabs
	// retire automatically.
	s.mux.HandleFunc("DELETE /api/orgs/{slug}/workers/{id}", s.removeWorkerHandler)

	// Unmatched /api/* → JSON 404, NOT the SPA HTML catch-all (#196 / FINDING-M).
	// Without this, an unknown or wrong-method /api path (e.g. POST /api/agents
	// after that route was removed in #185) falls through to the "/" SPA handler
	// and returns 200 text/html, misleading programmatic clients. The /api/
	// subtree is more specific than "/", so it intercepts before the SPA; the
	// explicit method+path routes (GET /api/agents, …) are more specific still and
	// keep precedence for their own method.
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "no such API route")
	})

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

// systemVersionHandler returns the full build identity for the Settings version
// panel (v2.8.1): version (${branch}-${commit} or a release tag), branch, commit,
// built_at. Each field falls back to a sentinel for unversioned (`go run`) builds.
func (s *Server) systemVersionHandler(w http.ResponseWriter, r *http.Request) {
	fallback := func(v, sentinel string) string {
		if v == "" {
			return sentinel
		}
		return v
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":  fallback(s.deps.Version, "dev"),
		"branch":   fallback(s.deps.Branch, "unknown"),
		"commit":   fallback(s.deps.Commit, "unknown"),
		"built_at": fallback(s.deps.BuiltAt, "unknown"),
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
