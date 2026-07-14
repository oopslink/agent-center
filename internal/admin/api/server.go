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
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// ServerDeps is the dependency bag for Server-level state shared
// across handlers (NOT the per-request HandlerDeps in deps.go).
type ServerDeps struct {
	// GitHandler is the center-hosted git smart-HTTP handler mounted at
	// /admin/git/ (centergit.Handler wrapped by NewGitHandler). It is Server-level
	// (constructed once at boot with the git Host + membership) rather than
	// per-request. nil → the /admin/git/ route returns git_not_wired (501).
	GitHandler http.Handler
}

// Server is the admin endpoint HTTP server. v2.2 bound only to a unix
// socket; v2.3-7a (task #27) adds an optional concurrent TCP+TLS
// listener for cross-host worker / CLI access. Either or both
// listeners may be configured; both empty = boot error.
type Server struct {
	socketPath string
	mux        *http.ServeMux
	srv        *http.Server
	listener   net.Listener
	deps       ServerDeps

	// v2.3-7a — optional TCP+TLS transport.
	tcpListenAddr  string
	tlsCert        *tls.Certificate
	tlsFingerprint string
	tcpSrv         *http.Server
	tcpListener    net.Listener

	// mu guards the listener-lifecycle fields (listener, tcpListener, tcpSrv)
	// that ListenAndServe writes (on the serving goroutine path) and Shutdown
	// reads (possibly on another goroutine). Without it, the bind→assign in
	// ListenAndServe races a concurrent Shutdown read — the socket file
	// becoming visible does NOT establish a happens-before on the field write.
	// srv/socketPath are set once in the constructor (before any goroutine) and
	// are not guarded. (#128)
	mu sync.Mutex
}

// NewServer constructs a Server with default (empty) server-level deps.
func NewServer(socketPath string) *Server {
	return NewServerWithDeps(socketPath, ServerDeps{})
}

// NewServerWithDeps constructs a Server with Server-level deps.
func NewServerWithDeps(socketPath string, deps ServerDeps) *Server {
	s := &Server{
		socketPath: socketPath,
		mux:        http.NewServeMux(),
		deps:       deps,
	}
	s.routes()
	s.srv = &http.Server{
		Handler:           s.mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return s
}

// NewServerWithTransports constructs a Server that may listen on either
// or both of a unix socket (socketPath) and a TCP+TLS address
// (tcpListenAddr). The TLS cert + fingerprint are produced by
// LoadOrGenerateCert in tls.go; this constructor does not generate
// them — boot code must do that and pass the result in.
//
// Either socketPath OR tcpListenAddr may be empty (or both — caller's
// responsibility to validate that at least one is non-empty;
// ListenAndServe rejects the all-empty case).
//
// v2.3-7a (task #27): adds the TCP+TLS leg for cross-host access.
func NewServerWithTransports(
	socketPath, tcpListenAddr string,
	tlsCert *tls.Certificate, tlsFingerprint string,
	deps ServerDeps,
) *Server {
	s := NewServerWithDeps(socketPath, deps)
	s.tcpListenAddr = tcpListenAddr
	s.tlsCert = tlsCert
	s.tlsFingerprint = tlsFingerprint
	return s
}

// TLSFingerprint returns the SHA256 cert fingerprint of the TCP
// listener (empty if TCP not configured). Used by boot code to print
// the banner.
func (s *Server) TLSFingerprint() string {
	return s.tlsFingerprint
}

// Handler exposes the mux (testable without a listener).
func (s *Server) Handler() http.Handler { return s.mux }

// SetHandler swaps the http.Server.Handler so dep-injection
// middleware can wrap the mux for in-process serving. Mirrors
// webconsole/api.Server.SetHandler pattern.
//
// Both unix + TCP listeners share the same wrapped handler so auth +
// scope middleware applies uniformly across transports.
func (s *Server) SetHandler(h http.Handler) {
	s.srv.Handler = h
	if s.tcpSrv != nil {
		s.tcpSrv.Handler = h
	}
}

// ListenAndServe binds the configured listeners (unix socket, TCP+TLS,
// or both) and serves. Returns the first error from either leg or
// http.ErrServerClosed on graceful Shutdown.
//
// v2.3-7a (task #27): if tcpListenAddr is non-empty, also serves
// TLS using s.tlsCert. Both listeners share s.mux through SetHandler.
func (s *Server) ListenAndServe() error {
	if s.socketPath == "" && s.tcpListenAddr == "" {
		return errors.New("admin api: at least one of socket_path or tcp_listen required")
	}
	if s.tcpListenAddr != "" && s.tlsCert == nil {
		return errors.New("admin api: tcp_listen set but tlsCert is nil — boot code must call LoadOrGenerateCert + pass it via NewServerWithTransports")
	}

	type result struct {
		from string
		err  error
	}
	errs := make(chan result, 2)
	var legs int

	var unixLn net.Listener
	if s.socketPath != "" {
		legs++
		ln, err := s.bindUnix()
		if err != nil {
			return err
		}
		unixLn = ln
		s.mu.Lock()
		s.listener = ln
		s.mu.Unlock()
		go func() { errs <- result{from: "unix", err: s.srv.Serve(ln)} }()
	}

	if s.tcpListenAddr != "" {
		legs++
		ln, err := s.bindTCP()
		if err != nil {
			// Clean up the unix leg if it managed to bind, so the
			// caller doesn't have to. Use the local (not s.listener) to
			// avoid a guarded read on the error path.
			if unixLn != nil {
				_ = unixLn.Close()
			}
			return err
		}
		// Build the TCP http.Server lazily so SetHandler applied
		// before ListenAndServe is reflected. Mirror the unix
		// srv's Handler + timeouts.
		tcpSrv := &http.Server{
			Handler:           s.srv.Handler,
			ReadHeaderTimeout: 10 * time.Second,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{*s.tlsCert},
				MinVersion:   tls.VersionTLS12,
			},
		}
		s.mu.Lock()
		s.tcpListener = ln
		s.tcpSrv = tcpSrv
		s.mu.Unlock()
		go func() {
			// ServeTLS reads cert from disk if path-args non-empty;
			// our TLSConfig already has the cert in-memory so pass
			// empty strings. Use the captured locals (not the guarded
			// fields) so the serving goroutine needs no lock.
			errs <- result{from: "tcp", err: tcpSrv.ServeTLS(ln, "", "")}
		}()
	}

	// Return the first non-ErrServerClosed error, or wait for all to
	// finish (which only happens after Shutdown).
	var firstErr error
	for i := 0; i < legs; i++ {
		r := <-errs
		if r.err != nil && !errors.Is(r.err, http.ErrServerClosed) && firstErr == nil {
			firstErr = fmt.Errorf("admin api: %s leg: %w", r.from, r.err)
		}
	}
	if firstErr != nil {
		return firstErr
	}
	return http.ErrServerClosed
}

func (s *Server) bindUnix() (net.Listener, error) {
	// Ensure parent dir exists (mode 0700 — owner-only).
	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("admin api: mkdir %q: %w", dir, err)
	}
	// Remove stale socket file from prior crash. We intentionally
	// do NOT guard against "another server is using this socket" —
	// if someone is, the next Listen will fail and the user will see
	// the error; pre-emptively unlinking under a live peer would
	// silently break it.
	if _, err := os.Stat(s.socketPath); err == nil {
		if err := os.Remove(s.socketPath); err != nil {
			return nil, fmt.Errorf("admin api: remove stale socket %q: %w", s.socketPath, err)
		}
	}
	ln, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return nil, fmt.Errorf("admin api: listen unix %q: %w", s.socketPath, err)
	}
	// Restrict to owner only (mode 0600). MkdirAll above set the dir
	// to 0700 so this is defense-in-depth.
	if err := os.Chmod(s.socketPath, 0o600); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("admin api: chmod socket: %w", err)
	}
	return ln, nil
}

func (s *Server) bindTCP() (net.Listener, error) {
	ln, err := net.Listen("tcp", s.tcpListenAddr)
	if err != nil {
		return nil, fmt.Errorf("admin api: listen tcp %q: %w", s.tcpListenAddr, err)
	}
	return ln, nil
}

// Shutdown cleanly stops both listeners (if active) + removes the
// socket file. Safe to call multiple times.
func (s *Server) Shutdown(ctx context.Context) error {
	// Snapshot the listener-lifecycle fields under the lock (they may still be
	// being assigned by a concurrent ListenAndServe). srv is constructor-set.
	s.mu.Lock()
	srv, listener := s.srv, s.listener
	tcpSrv, tcpListener := s.tcpSrv, s.tcpListener
	s.mu.Unlock()

	var wg sync.WaitGroup
	// Each leg writes its OWN error var (read only after wg.Wait) so the two
	// shutdown goroutines never write a shared field — fixes the prior
	// concurrent-write race on firstErr.
	var unixErr, tcpErr error
	if srv != nil && listener != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			unixErr = srv.Shutdown(ctx)
		}()
	}
	if tcpSrv != nil && tcpListener != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tcpErr = tcpSrv.Shutdown(ctx)
		}()
	}
	wg.Wait()
	// Always try to clean the socket file — leaving it stranded
	// breaks the next start with EADDRINUSE.
	if s.socketPath != "" {
		_ = os.Remove(s.socketPath)
	}
	if unixErr != nil {
		return unixErr
	}
	return tcpErr
}

// routes registers the full v2.2-A2 admin surface: 79 AppService methods
// grouped by BC. Path convention `POST /admin/<bc>/<resource>/<action>`
// for writes and `GET /admin/<bc>/<resource>/<query>` for reads.
//
// Health endpoint is the only legacy route from A1.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /admin/health", s.healthHandler)

	// --- workforce -------------------------------------------------------
	s.mux.HandleFunc("POST /admin/workforce/worker/enroll", s.workerEnrollHandler)
	// v2.3-1 (task #24): proper heartbeat (replaces the v2.2 hack of
	// re-calling enroll + swallowing 409).
	s.mux.HandleFunc("POST /admin/workforce/worker/heartbeat", s.workerHeartbeatHandler)
	s.mux.HandleFunc("POST /admin/workforce/worker/rename", s.workerRenameHandler)
	// v2.7 #147: worker auto-discovers installed agent CLIs and reports them
	// here on every online (caller = the worker itself; rich capability shape).
	s.mux.HandleFunc("POST /admin/workforce/worker/capabilities", s.workerReportCapabilitiesHandler)
	// v2.7 #147 D4: operator per-CLI enable/disable toggle.
	s.mux.HandleFunc("PATCH /admin/workforce/worker/{id}/capabilities/{name}/enabled", s.workerSetCapabilityEnabledHandler)
	s.mux.HandleFunc("GET /admin/workforce/worker/find-all", s.workerFindAllHandler)
	s.mux.HandleFunc("GET /admin/workforce/worker/find-by-id", s.workerFindByIDHandler)
	s.mux.HandleFunc("GET /admin/workforce/worker/find-by-status", s.workerFindByStatusHandler)
	s.mux.HandleFunc("GET /admin/workforce/project/find-all", s.projectFindAllHandler)
	s.mux.HandleFunc("GET /admin/workforce/project/find-by-id", s.projectFindByIDHandler)

	// --- environment (v2.7 D1, ADR-0050, task #102) ----------------------
	// Worker-initiated control channel. ADDITIVE — rides the same bearer
	// auth + TLS as the workforce worker routes above; does NOT touch the
	// legacy /admin/workforce/... dispatch surface.
	s.mux.HandleFunc("POST /admin/environment/worker/connect", s.envWorkerConnectHandler)
	s.mux.HandleFunc("GET /admin/environment/worker/commands", s.envWorkerCommandsHandler)
	// v2.7 D5 slice-1 (center-side SSE down-push): the same command stream as the
	// poll endpoint above, pushed over SSE for low latency. Catch-up via
	// CommandsAfter(?after=offset) + live bus fan-out, deduped by offset; same
	// bearer auth; reconnect-by-offset. Degrades to 501 if the bus isn't wired.
	s.mux.HandleFunc("GET /admin/environment/worker/commands/stream", s.envWorkerCommandsStreamHandler)
	s.mux.HandleFunc("POST /admin/environment/worker/ack", s.envWorkerAckHandler)
	s.mux.HandleFunc("POST /admin/environment/worker/heartbeat", s.envWorkerHeartbeatHandler)
	// v2.7 D2-f s4 (ADR-0049/0050): worker boot-resume. On (re)start with the
	// control-stream path active the daemon asks "which of MY agents should be
	// running + their in-flight WorkItems" so it can re-attach/relaunch their
	// claude sessions. Worker-level authz: worker from the token owner, body
	// worker_id MUST == it (only-ask-self), else 403. ADDITIVE — same bearer auth;
	// default-off path (the daemon only calls it when the control loop is active).
	s.mux.HandleFunc("POST /admin/environment/worker/resume-state", s.envWorkerResumeStateHandler)

	// --- environment agent feedback (v2.7 D2-c-i, ADR-0049/0050) ---------
	// Controller→center feedback. ADDITIVE — worker/daemon-facing, same bearer
	// auth as the worker control routes; each is gated by requireAgentOnWorker
	// (worker from token owner + target agent must be bound to it). lifecycle-
	// feedback is PERSIST-ONLY (never emits agent.lifecycle_changed → no
	// reconcile loop). Nothing is activated; the legacy path is untouched.
	s.mux.HandleFunc("POST /admin/environment/agent/activity", s.envAgentActivityHandler)
	s.mux.HandleFunc("POST /admin/environment/agent/lifecycle-feedback", s.envAgentLifecycleFeedbackHandler)
	// v2.14.0 F7 (issue I14): the work-item-state feedback route was removed —
	// AgentWorkItem retired (the daemon pulls/advances work via list_my_tasks/
	// start_task, so the "active" work-item feedback post is obsolete).
	// v2.7 D2-e-ii (OQ5): controller→center mark-seen. Monotonically advances the
	// agent participant's read-state cursor after a wake inject so the next batch
	// flush won't re-deliver. Same requireAgentOnWorker guardrail; only-forward.
	s.mux.HandleFunc("POST /admin/environment/agent/mark-seen", s.envAgentMarkSeenHandler)
	// v2.7 #185 follow-up (UX Rule 9): controller→center converse-error. When an
	// agent.converse (DM/channel) turn ends is_error, the controller posts a
	// visible system message into the conversation so the human isn't left in a
	// silent black hole. Same requireAgentOnWorker guardrail + participant check.
	s.mux.HandleFunc("POST /admin/environment/agent/converse-error", s.envAgentConverseErrorHandler)
	// I5 (issue-921db054): worker→center correlated reply to a runtime-fs read command.
	s.mux.HandleFunc("POST /admin/environment/agent/runtime-fs/response", s.envAgentRuntimeFSResponseHandler)
	// T341 reply-guardrail: controller→center at turn-end + TrueIdle. The server
	// derives the agent's outstanding directed replies, gates agent-authored ones
	// through the shared wake-guardrail, and returns bounded re-inject prompts the
	// controller injects (方案 A). Same requireAgentOnWorker guardrail.
	s.mux.HandleFunc("POST /admin/environment/agent/reply-nudges", s.envAgentReplyNudgesHandler)
	// T456 (issue-21ba5b78/I30): worker process-alive lease auto-renew. The daemon
	// renews each live session's current-task lease on a tick — decoupled from the
	// agent's LLM turn — so a long build/test never lets the lease lapse. Same
	// requireAgentOnWorker guardrail; PERSIST-ONLY lease touch (no outbox emit).
	s.mux.HandleFunc("POST /admin/environment/agent/lease/heartbeat", s.envAgentLeaseHeartbeatHandler)

	// --- agent tools (v2.7 D2-b1, ADR-0049) ------------------------------
	// Per-agent MCP tool surface. ADDITIVE — rides the same bearer auth as
	// the worker routes above. The per-agent auth gate takes the worker from
	// the TOKEN OWNER and verifies the target agent is bound to it (guardrail)
	// before any tool runs. b1 ships one representative read tool.
	// v2.14.0 I14/F5 §五: the Task-model agent surface. list_my_tasks is the
	// "what do I have to do?" query (runnable open/running tasks, §13.A); start_task
	// is task-based (open→running + §2.5 lease, §13.A run-ahead gated); heartbeat
	// renews the running task's execution lease.
	// v2.14.0 F7 (issue I14): get_my_work + the work-item fail_task/pause_task/
	// resume_task routes were removed — AgentWorkItem retired (no compat).
	s.mux.HandleFunc("POST /admin/agent-tools/list_my_tasks", s.listMyTasksHandler)
	// Runtime-facing reconcile query (§4.2): the UNFILTERED in-flight (open/running)
	// set — includes deps-unsatisfied tasks list_my_tasks drops. Admin-only agent-tool
	// (the runtime's center client calls it; not an MCP tool for the supervisor).
	s.mux.HandleFunc("POST /admin/agent-tools/list_my_inflight_tasks", s.listMyInflightTasksHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/heartbeat", s.heartbeatHandler)
	// report_usage (v2.15.0 I28/F2): worker-initiated per-turn usage ingest — not
	// LLM-facing (deliberately absent from the agent-facing MCP set).
	s.mux.HandleFunc("POST /admin/agent-tools/report_usage", s.reportUsageHandler)
	// report_delivery (issue-f30b7e7b): worker-initiated per-executor terminal git
	// status ingest — not LLM-facing. Feeds the writeback auto-block (B②) + audit.
	s.mux.HandleFunc("POST /admin/agent-tools/report_delivery", s.reportDeliveryHandler)
	// report_installed_skills (issue-4a45e9cc): agent-runtime OBSERVED skill report —
	// worker-initiated, not LLM-facing. Replaces the agent's agent_installed_skills set.
	s.mux.HandleFunc("POST /admin/agent-tools/report_installed_skills", s.reportInstalledSkillsHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/start_task", s.startWorkHandler)
	// T83: claim an open built-in assignment-pool task — atomic assign+run, fail-closed.
	s.mux.HandleFunc("POST /admin/agent-tools/claim_task", s.claimTaskHandler)
	// v2.8.1 #278 D PR4b dual-stream: the agent's unread messages (DM + @mention).
	s.mux.HandleFunc("POST /admin/agent-tools/get_my_unread", s.getMyUnreadHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/mark_seen", s.markSeenHandler)
	// Browse a conversation's chat history (DM/channel/task/issue/plan) the agent
	// participates in — the read-history complement to get_my_unread's inbox.
	s.mux.HandleFunc("POST /admin/agent-tools/list_messages", s.listMessagesHandler)
	// v2.7.1 #239 — agent self/org-discovery reads (0 round-trip self-awareness):
	// own profile (org/projects/capabilities) + find peer org agents by name.
	s.mux.HandleFunc("POST /admin/agent-tools/get_my_profile", s.getMyProfileHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/find_org_agent", s.findOrgAgentHandler)
	// v2.7.1 #246: resolve a channel name → id (for post_message); empty name lists all.
	s.mux.HandleFunc("POST /admin/agent-tools/find_org_channel", s.findOrgChannelHandler)
	// v2.7 D2-b2 — explicit human-visible communication write tools. The agent
	// posts to the task it is working; composite tools are atomic (one outer tx).
	// T200 WS4 — ONE post_message routes to a DM/channel, a task, or an issue via
	// its target{type,id}; the former post_task_message / post_issue_message tools
	// are gone (their resolution+authz branches live inside postMessageHandler).
	s.mux.HandleFunc("POST /admin/agent-tools/post_message", s.postMessageHandler)
	// Agent→agent coordination: create/reuse a same-org DM and send the opening
	// message through the same MessageWriter path as human-created DMs.
	s.mux.HandleFunc("POST /admin/agent-tools/start_dm", s.startDMHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/block_task", s.blockTaskHandler)
	// v2.9.1 P0 recovery: pull a deadlocked-blocked task back to executable.
	s.mux.HandleFunc("POST /admin/agent-tools/unblock_task", s.unblockTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/reset_task", s.resetTaskHandler) // T862 tier-3 recovery
	s.mux.HandleFunc("POST /admin/agent-tools/rerun_failed_node", s.rerunFailedNodeHandler)
	// T53: operator resume of a paused plan node (un-stick a set-aside node).
	s.mux.HandleFunc("POST /admin/agent-tools/resume_paused_node", s.resumePausedNodeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/complete_task", s.completeTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/discard_task", s.discardTaskHandler)    // T119
	s.mux.HandleFunc("POST /admin/agent-tools/set_task_issue", s.setTaskIssueHandler) // T192: (re)set/clear derived_from_issue
	// T206: Reminder agent tools (Cognition BC). tool name == route segment.
	s.mux.HandleFunc("POST /admin/agent-tools/create_reminder", s.createReminderHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/list_reminders", s.listRemindersHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_reminder", s.getReminderHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/update_reminder", s.updateReminderHandler)
	// v2.7 D2 b2/d-ii-B — passthrough tools: thin wrappers over the pm
	// AppServices (writes use actor=agent; the AppService's requireProjectMember
	// is the write-gate) + per-agent-scoped reads (get_task own-work, get_issue
	// project-membership domain).
	s.mux.HandleFunc("POST /admin/agent-tools/create_task", s.createTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/assign_task", s.assignTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/reassign_task", s.assignTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/subscribe", s.subscribeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/unsubscribe", s.unsubscribeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_task", s.getTaskHandler)
	s.mux.HandleFunc("GET /admin/agent-tools/get_task", s.getTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/list_tasks", s.listTasksHandler) // v2.9.1 #T38
	s.mux.HandleFunc("POST /admin/agent-tools/get_issue", s.getIssueHandler)
	s.mux.HandleFunc("GET /admin/agent-tools/get_issue", s.getIssueHandler)
	// v2.10.3 T170 — agent issue-management tools (create/update/close/reopen/
	// comment/list/link-task). Parity guards (TestAgentFacingToolParity +
	// TestAgentFacingTool_HasAdminRoute) keep these in lockstep with the MCP
	// registration in mcphost.
	s.mux.HandleFunc("POST /admin/agent-tools/create_issue", s.createIssueHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/update_issue", s.updateIssueHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/close_issue", s.closeIssueHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/reopen_issue", s.reopenIssueHandler)
	// T200 WS4: post_issue_message merged into post_message (target type "issue").
	s.mux.HandleFunc("POST /admin/agent-tools/list_issues", s.listIssuesHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/list_tasks_of_issue", s.listTasksOfIssueHandler)
	// v2.18.4 BE-2 (issue-f980c8de) — agent repo-info MCP tools (project-member
	// scoped; credential never returned). get_repo_info(live) fetches remote
	// commits/branches server-side via the provider abstraction.
	s.mux.HandleFunc("POST /admin/agent-tools/list_project_repos", s.listProjectReposHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_repo_info", s.getRepoInfoHandler)
	// v2.7 post-D3 (task #104) — agent file MCP tools. Upload/download/attach with
	// agent-domain reachability authz (the agent's OWN enumerable scopes). The
	// byte mechanics mirror D3-d's human transport; only the authorization model
	// differs. The literal `transfer` segment is more specific than `{ulid}`, so
	// ServeMux routes PUT/complete to the transfer handlers and bare GET to
	// download (same precedence trick as D3-d).
	s.mux.HandleFunc("POST /admin/agent-tools/create_plan", s.createPlanHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/add_task_to_plan", s.addTaskToPlanHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/remove_task_from_plan", s.removeTaskFromPlanHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/add_plan_dependency", s.addPlanDependencyHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/remove_plan_dependency", s.removePlanDependencyHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/edit_plan_topology", s.editPlanTopologyHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/start_plan", s.startPlanHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/stop_plan", s.stopPlanHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/delete_plan", s.deletePlanHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/archive_plan", s.archivePlanHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_plan", s.getPlanHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/list_plans", s.listPlansHandler)
	// 2026-07-03 plan-stage-model §6: Stage authoring + read.
	s.mux.HandleFunc("POST /admin/agent-tools/create_stage", s.createStageHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_stage", s.getStageHandler)
	// v2.10 Plan Shared Findings (ADR-0053 — DeLM shared verified context).
	s.mux.HandleFunc("POST /admin/agent-tools/record_finding", s.recordFindingHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/list_findings", s.listFindingsHandler)
	// --- orchestration engine tools (P2-T2) ---------------------------------
	s.mux.HandleFunc("POST /admin/agent-tools/create_graph", s.createGraphHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_graph", s.getGraphHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/start_graph", s.startGraphHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/finish_graph", s.finishGraphHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/add_graph_node", s.addGraphNodeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/remove_graph_node", s.removeGraphNodeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/update_graph_node", s.updateGraphNodeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/start_graph_node", s.startGraphNodeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/complete_graph_node", s.completeGraphNodeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/discard_graph_node", s.discardGraphNodeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/resolve_condition", s.resolveConditionHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/add_graph_edge", s.addGraphEdgeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/remove_graph_edge", s.removeGraphEdgeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/list_graph_nodes", s.listGraphNodesHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_graph_node", s.getGraphNodeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_ready_nodes", s.getReadyNodesHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/bind_task_to_node", s.bindTaskToNodeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/unbind_task_from_node", s.unbindTaskFromNodeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/list_templates", s.listTemplatesHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_template", s.getTemplateHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/create_template", s.createTemplateHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/update_template", s.updateTemplateHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/delete_template", s.deleteTemplateHandler)
	// model catalog (issue-93dd8daa ①): org-level user-managed model catalog CRUD + import.
	s.mux.HandleFunc("POST /admin/agent-tools/list_model_catalog_entry", s.listModelCatalogHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/create_model_catalog_entry", s.createModelCatalogHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/update_model_catalog_entry", s.updateModelCatalogHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/delete_model_catalog_entry", s.deleteModelCatalogHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/import_model_catalog", s.importModelCatalogHandler)
	// Team BC (Team Phase-1 wiring, design §4/§6/§7/§9): CRUD + membership +
	// project association (S1 tool facade), plus template authoring / instantiation
	// / role→agent resolution (S3). See agent_tools_team.go.
	s.mux.HandleFunc("POST /admin/agent-tools/create_team", s.createTeamHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/update_team", s.updateTeamHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/delete_team", s.deleteTeamHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_team", s.getTeamHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/list_teams", s.listTeamsHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/add_member", s.addMemberHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/remove_member", s.removeMemberHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/associate_project", s.associateProjectHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/create_team_template", s.createTeamTemplateHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/curate_team_template", s.curateTeamTemplateHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/export_team_template", s.exportTeamTemplateHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/import_team_template", s.importTeamTemplateHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/instantiate_team", s.instantiateTeamHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/extract_from_team", s.extractFromTeamHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/assign_roles", s.assignRolesHandler)
	// Center-hosted git smart-HTTP (design §4.2/§4.3): per-agent/team/global memory
	// repos served under /admin/git/, behind the same bearer auth. See git_backend.go.
	s.mux.Handle("/admin/git/", http.HandlerFunc(s.gitPassthrough))
	s.mux.HandleFunc("POST /admin/agent-tools/upload_file", s.uploadFileHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/attach_file", s.attachFileHandler)
	s.mux.HandleFunc("PUT /admin/files/transfer/{transfer_id}", s.putAgentBlobHandler)
	s.mux.HandleFunc("POST /admin/files/transfer/{transfer_id}/complete", s.completeFileHandler)
	s.mux.HandleFunc("GET /admin/files/{ulid}", s.downloadFileHandler)

	// --- conversation ----------------------------------------------------
	s.mux.HandleFunc("GET /admin/conversation/conv/find", s.convFindHandler)
	s.mux.HandleFunc("GET /admin/conversation/conv/find-by-id", s.convFindByIDHandler)
	s.mux.HandleFunc("GET /admin/conversation/conv/find-by-name", s.convFindByNameHandler)
	s.mux.HandleFunc("GET /admin/conversation/msg/find-by-id", s.msgFindByIDHandler)
	s.mux.HandleFunc("GET /admin/conversation/msg/find-by-conversation-id", s.msgFindByConversationIDHandler)
	// v2.3-1 (task #24): proper tail/recent (replaces the v2.2 client-side
	// trim against the 200-cap find-by-conversation-id helper).
	s.mux.HandleFunc("GET /admin/conversation/msg/find-recent", s.msgFindRecentHandler)
	s.mux.HandleFunc("POST /admin/conversation/msg/append", s.msgAppendHandler)
	s.mux.HandleFunc("POST /admin/conversation/message-writer/open", s.openConversationHandler)
	s.mux.HandleFunc("POST /admin/conversation/message-writer/close", s.closeConversationHandler)
	s.mux.HandleFunc("POST /admin/conversation/message-writer/archive", s.archiveConversationHandler)
	s.mux.HandleFunc("POST /admin/conversation/channel/create", s.createChannelHandler)
	s.mux.HandleFunc("POST /admin/conversation/channel/archive", s.archiveChannelHandler)
	s.mux.HandleFunc("POST /admin/conversation/participant/invite", s.inviteParticipantHandler)
	s.mux.HandleFunc("POST /admin/conversation/participant/kick", s.kickParticipantHandler)
	// v2.3-1 (task #24): self-leave proxy (was missing, forcing CLI to
	// fall back to direct ParticipantMgmtSvc — broke single-entry rule).
	s.mux.HandleFunc("POST /admin/conversation/participant/leave", s.leaveParticipantHandler)
	s.mux.HandleFunc("GET /admin/conversation/carry-over/find-by-child-conv", s.carryOverFindByChildConvHandler)
	s.mux.HandleFunc("GET /admin/conversation/carry-over/find-by-source-msg", s.carryOverFindBySourceMsgHandler)
	s.mux.HandleFunc("GET /admin/conversation/conv-ref/find-by-child-conv-id", s.convRefFindByChildConvIDHandler)
	s.mux.HandleFunc("GET /admin/conversation/conv-ref/find-by-source-msg-id", s.convRefFindBySourceMsgIDHandler)

	// --- secret ----------------------------------------------------------
	s.mux.HandleFunc("GET /admin/secret/user-secret/find-all", s.secretFindAllHandler)
	s.mux.HandleFunc("GET /admin/secret/user-secret/find-by-id", s.secretFindByIDHandler)
	s.mux.HandleFunc("GET /admin/secret/user-secret/find-by-name", s.secretFindByNameHandler)
	s.mux.HandleFunc("POST /admin/secret/user-secret/create", s.secretCreateHandler)
	s.mux.HandleFunc("POST /admin/secret/user-secret/rotate", s.secretRotateHandler)
	s.mux.HandleFunc("POST /admin/secret/user-secret/revoke", s.secretRevokeHandler)
	s.mux.HandleFunc("POST /admin/secret/user-secret/resolve", s.secretResolveHandler)

	// --- observability ---------------------------------------------------
	s.mux.HandleFunc("GET /admin/observability/event/find-by-id", s.eventFindByIDHandler)
	s.mux.HandleFunc("GET /admin/observability/event/find", s.eventFindHandler)
	s.mux.HandleFunc("POST /admin/observability/query/query", s.queryHandler)
	s.mux.HandleFunc("GET /admin/observability/query/inspect", s.inspectHandler)
	s.mux.HandleFunc("GET /admin/observability/fleet/snapshot", s.fleetSnapshotHandler)
	s.mux.HandleFunc("GET /admin/observability/stats/aggregate", s.statsAggregateHandler)
	s.mux.HandleFunc("GET /admin/observability/logs/open", s.logsOpenHandler)

	// --- blob (v2.3-3b task #29) ----------------------------------------
	s.mux.HandleFunc("POST /admin/blob/put", s.blobPutHandler)

	// --- admin token (v2.3-3a task #28) ----------------------------------
	s.mux.HandleFunc("POST /admin/admintoken/create", s.admintokenCreateHandler)
	s.mux.HandleFunc("GET /admin/admintoken/list", s.admintokenListHandler)
	s.mux.HandleFunc("POST /admin/admintoken/revoke", s.admintokenRevokeHandler)
}
