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
type ServerDeps struct{}

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
	s.mux.HandleFunc("GET /admin/workforce/worker/find-all", s.workerFindAllHandler)
	s.mux.HandleFunc("GET /admin/workforce/worker/find-by-id", s.workerFindByIDHandler)
	s.mux.HandleFunc("GET /admin/workforce/worker/find-by-status", s.workerFindByStatusHandler)
	s.mux.HandleFunc("POST /admin/workforce/agent-instance/create", s.agentCreateHandler)
	s.mux.HandleFunc("POST /admin/workforce/agent-instance/archive", s.agentArchiveHandler)
	s.mux.HandleFunc("GET /admin/workforce/agent-instance/find-all", s.agentFindAllHandler)
	s.mux.HandleFunc("GET /admin/workforce/agent-instance/find-by-id", s.agentFindByIDHandler)
	s.mux.HandleFunc("GET /admin/workforce/agent-instance/find-by-name", s.agentFindByNameHandler)
	s.mux.HandleFunc("GET /admin/workforce/project/find-all", s.projectFindAllHandler)
	s.mux.HandleFunc("GET /admin/workforce/project/find-by-id", s.projectFindByIDHandler)

	// --- environment (v2.7 D1, ADR-0050, task #102) ----------------------
	// Worker-initiated control channel. ADDITIVE — rides the same bearer
	// auth + TLS as the workforce worker routes above; does NOT touch the
	// legacy /admin/workforce/... dispatch surface.
	s.mux.HandleFunc("POST /admin/environment/worker/connect", s.envWorkerConnectHandler)
	s.mux.HandleFunc("GET /admin/environment/worker/commands", s.envWorkerCommandsHandler)
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
	s.mux.HandleFunc("POST /admin/environment/agent/work-item-state", s.envAgentWorkItemStateHandler)
	// v2.7 D2-e-ii (OQ5): controller→center mark-seen. Monotonically advances the
	// agent participant's read-state cursor after a wake inject so the next batch
	// flush won't re-deliver. Same requireAgentOnWorker guardrail; only-forward.
	s.mux.HandleFunc("POST /admin/environment/agent/mark-seen", s.envAgentMarkSeenHandler)

	// --- agent tools (v2.7 D2-b1, ADR-0049) ------------------------------
	// Per-agent MCP tool surface. ADDITIVE — rides the same bearer auth as
	// the worker routes above. The per-agent auth gate takes the worker from
	// the TOKEN OWNER and verifies the target agent is bound to it (guardrail)
	// before any tool runs. b1 ships one representative read tool.
	s.mux.HandleFunc("POST /admin/agent-tools/get_my_work", s.getMyWorkHandler)
	// v2.7 D2-b2 — explicit human-visible communication write tools. The agent
	// posts to the task it is working; composite tools are atomic (one outer tx).
	s.mux.HandleFunc("POST /admin/agent-tools/post_task_message", s.postTaskMessageHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/request_input", s.requestInputHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/block_task", s.blockTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/complete_task", s.completeTaskHandler)
	// v2.7 D2 b2/d-ii-B — passthrough tools: thin wrappers over the pm
	// AppServices (writes use actor=agent; the AppService's requireProjectMember
	// is the write-gate) + per-agent-scoped reads (get_task own-work, get_issue
	// project-membership domain).
	s.mux.HandleFunc("POST /admin/agent-tools/create_task", s.createTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/assign_task", s.assignTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/reassign_task", s.assignTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/subscribe", s.subscribeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/unsubscribe", s.unsubscribeHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/verify_task", s.verifyTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_task", s.getTaskHandler)
	s.mux.HandleFunc("GET /admin/agent-tools/get_task", s.getTaskHandler)
	s.mux.HandleFunc("POST /admin/agent-tools/get_issue", s.getIssueHandler)
	s.mux.HandleFunc("GET /admin/agent-tools/get_issue", s.getIssueHandler)
	// v2.7 post-D3 (task #104) — agent file MCP tools. Upload/download/attach with
	// agent-domain reachability authz (the agent's OWN enumerable scopes). The
	// byte mechanics mirror D3-d's human transport; only the authorization model
	// differs. The literal `transfer` segment is more specific than `{ulid}`, so
	// ServeMux routes PUT/complete to the transfer handlers and bare GET to
	// download (same precedence trick as D3-d).
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
