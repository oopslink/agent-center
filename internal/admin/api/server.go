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

	"github.com/oopslink/agent-center/internal/admin/dispatchq"
)

// ServerDeps is the dependency bag for Server-level state shared
// across handlers (NOT the per-request HandlerDeps in deps.go).
// v2.2-A3: holds the dispatchq.Queue so dispatch/kill pull endpoints
// can drain it.
type ServerDeps struct {
	Queue *dispatchq.Queue
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
}

// NewServer constructs a Server with no Queue wired. Use
// NewServerWithDeps when the dispatch/kill pull endpoints should be
// live (v2.2-A3 wiring path).
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

	if s.socketPath != "" {
		legs++
		ln, err := s.bindUnix()
		if err != nil {
			return err
		}
		s.listener = ln
		go func() { errs <- result{from: "unix", err: s.srv.Serve(ln)} }()
	}

	if s.tcpListenAddr != "" {
		legs++
		ln, err := s.bindTCP()
		if err != nil {
			// Clean up the unix leg if it managed to bind, so the
			// caller doesn't have to.
			if s.listener != nil {
				_ = s.listener.Close()
			}
			return err
		}
		s.tcpListener = ln
		// Build the TCP http.Server lazily so SetHandler applied
		// before ListenAndServe is reflected. Mirror the unix
		// srv's Handler + timeouts.
		s.tcpSrv = &http.Server{
			Handler:           s.srv.Handler,
			ReadHeaderTimeout: 10 * time.Second,
			TLSConfig: &tls.Config{
				Certificates: []tls.Certificate{*s.tlsCert},
				MinVersion:   tls.VersionTLS12,
			},
		}
		go func() {
			// ServeTLS reads cert from disk if path-args non-empty;
			// our TLSConfig already has the cert in-memory so pass
			// empty strings.
			errs <- result{from: "tcp", err: s.tcpSrv.ServeTLS(ln, "", "")}
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
	var firstErr error
	var wg sync.WaitGroup
	if s.srv != nil && s.listener != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.srv.Shutdown(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}()
	}
	if s.tcpSrv != nil && s.tcpListener != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := s.tcpSrv.Shutdown(ctx); err != nil && firstErr == nil {
				firstErr = err
			}
		}()
	}
	wg.Wait()
	// Always try to clean the socket file — leaving it stranded
	// breaks the next start with EADDRINUSE.
	if s.socketPath != "" {
		_ = os.Remove(s.socketPath)
	}
	return firstErr
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
	s.mux.HandleFunc("POST /admin/workforce/proposal/propose", s.proposalProposeHandler)
	s.mux.HandleFunc("POST /admin/workforce/proposal/accept", s.proposalAcceptHandler)
	s.mux.HandleFunc("POST /admin/workforce/proposal/ignore", s.proposalIgnoreHandler)
	s.mux.HandleFunc("POST /admin/workforce/proposal/unignore", s.proposalUnignoreHandler)
	s.mux.HandleFunc("GET /admin/workforce/proposal/find-by-id", s.proposalFindByIDHandler)
	s.mux.HandleFunc("GET /admin/workforce/proposal/find-by-worker-id", s.proposalFindByWorkerIDHandler)
	s.mux.HandleFunc("GET /admin/workforce/proposal/find-pending", s.proposalFindPendingHandler)
	s.mux.HandleFunc("POST /admin/workforce/agent-instance/create", s.agentCreateHandler)
	s.mux.HandleFunc("POST /admin/workforce/agent-instance/archive", s.agentArchiveHandler)
	s.mux.HandleFunc("GET /admin/workforce/agent-instance/find-all", s.agentFindAllHandler)
	s.mux.HandleFunc("GET /admin/workforce/agent-instance/find-by-id", s.agentFindByIDHandler)
	s.mux.HandleFunc("GET /admin/workforce/agent-instance/find-by-name", s.agentFindByNameHandler)
	s.mux.HandleFunc("GET /admin/workforce/project/find-all", s.projectFindAllHandler)
	s.mux.HandleFunc("GET /admin/workforce/project/find-by-id", s.projectFindByIDHandler)
	s.mux.HandleFunc("POST /admin/workforce/project/add", s.projectAddHandler)
	s.mux.HandleFunc("POST /admin/workforce/project/remove", s.projectRemoveHandler)
	s.mux.HandleFunc("POST /admin/workforce/project/update", s.projectUpdateHandler)

	// --- environment (v2.7 D1, ADR-0050, task #102) ----------------------
	// Worker-initiated control channel. ADDITIVE — rides the same bearer
	// auth + TLS as the workforce worker routes above; does NOT touch the
	// legacy /admin/workforce/... dispatch surface.
	s.mux.HandleFunc("POST /admin/environment/worker/connect", s.envWorkerConnectHandler)
	s.mux.HandleFunc("GET /admin/environment/worker/commands", s.envWorkerCommandsHandler)
	s.mux.HandleFunc("POST /admin/environment/worker/ack", s.envWorkerAckHandler)
	s.mux.HandleFunc("POST /admin/environment/worker/heartbeat", s.envWorkerHeartbeatHandler)

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
	s.mux.HandleFunc("POST /admin/conversation/derivation/derive-issue", s.deriveIssueHandler)
	s.mux.HandleFunc("POST /admin/conversation/derivation/derive-task", s.deriveTaskHandler)
	s.mux.HandleFunc("GET /admin/conversation/conv-ref/find-by-child-conv-id", s.convRefFindByChildConvIDHandler)
	s.mux.HandleFunc("GET /admin/conversation/conv-ref/find-by-source-msg-id", s.convRefFindBySourceMsgIDHandler)

	// --- taskruntime -----------------------------------------------------
	s.mux.HandleFunc("GET /admin/taskruntime/task/find-by-id", s.taskFindByIDHandler)
	s.mux.HandleFunc("GET /admin/taskruntime/task/find-by-status", s.taskFindByStatusHandler)
	s.mux.HandleFunc("GET /admin/taskruntime/exec/find-by-id", s.execFindByIDHandler)
	s.mux.HandleFunc("GET /admin/taskruntime/exec/find-by-task-id", s.execFindByTaskIDHandler)
	s.mux.HandleFunc("GET /admin/taskruntime/exec/find-by-status", s.execFindByStatusHandler)
	s.mux.HandleFunc("GET /admin/taskruntime/ir/find-by-id", s.irFindByIDHandler)
	s.mux.HandleFunc("GET /admin/taskruntime/ir/find-by-execution-id", s.irFindByExecutionIDHandler)
	s.mux.HandleFunc("GET /admin/taskruntime/ir/find-pending", s.irFindPendingHandler)
	s.mux.HandleFunc("GET /admin/taskruntime/artifact/find-by-id", s.artifactFindByIDHandler)
	s.mux.HandleFunc("GET /admin/taskruntime/artifact/find-by-execution-id", s.artifactFindByExecutionIDHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/task/create", s.taskCreateHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/task/bind-conversation", s.taskBindConversationHandler)
	// v2.3-1 (task #24): agent-facing read-context proxy (was missing,
	// `read-task-context` returned ExitNotImplemented in Client mode).
	s.mux.HandleFunc("GET /admin/taskruntime/task/read-context", s.taskReadContextHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/ir/create", s.irCreateHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/ir/respond", s.irRespondHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/ir/cancel", s.irCancelHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/artifact/append", s.artifactAppendHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/exec/report-progress", s.execReportProgressHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/exec/report-failure", s.execReportFailureHandler)
	// v2.2 Phase D: state-machine progression endpoints — worker daemon
	// calls notify-working post-spawn and conclude on clean exit. Without
	// these the execution never leaves `submitted`.
	s.mux.HandleFunc("POST /admin/taskruntime/exec/notify-working", s.execNotifyWorkingHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/exec/conclude", s.execConcludeHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/dispatch/dispatch", s.dispatchHandler)
	s.mux.HandleFunc("POST /admin/taskruntime/kill/request", s.killExecutionHandler)
	// --- discussion ------------------------------------------------------
	s.mux.HandleFunc("GET /admin/discussion/issue/find-by-id", s.issueFindByIDHandler)
	s.mux.HandleFunc("GET /admin/discussion/issue/find-by-project", s.issueFindByProjectHandler)
	s.mux.HandleFunc("GET /admin/discussion/issue/find-by-status", s.issueFindByStatusHandler)
	s.mux.HandleFunc("POST /admin/discussion/issue/open", s.issueOpenHandler)
	s.mux.HandleFunc("POST /admin/discussion/issue/conclude", s.issueConcludeHandler)
	s.mux.HandleFunc("POST /admin/discussion/issue/withdraw", s.issueWithdrawHandler)
	s.mux.HandleFunc("POST /admin/discussion/issue/comment", s.issueCommentHandler)
	s.mux.HandleFunc("POST /admin/discussion/issue/bind-auto", s.issueBindAutoHandler)
	s.mux.HandleFunc("POST /admin/discussion/issue/bind-to", s.issueBindToHandler)
	s.mux.HandleFunc("POST /admin/discussion/issue/link", s.issueLinkHandler)

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

	// --- dispatch queue (v2.2-A3 — worker daemon drains via these) ------
	s.mux.HandleFunc("GET /admin/dispatch/queue/pull", s.dispatchQueuePullHandler)
	s.mux.HandleFunc("GET /admin/dispatch/queue/peek", s.queuePeekHandler)
	s.mux.HandleFunc("GET /admin/kill/queue/pull", s.killQueuePullHandler)
}
