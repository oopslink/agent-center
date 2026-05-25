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

	"github.com/oopslink/agent-center/internal/admin/dispatchq"
)

// ServerDeps is the dependency bag for Server-level state shared
// across handlers (NOT the per-request HandlerDeps in deps.go).
// v2.2-A3: holds the dispatchq.Queue so dispatch/kill pull endpoints
// can drain it.
type ServerDeps struct {
	Queue *dispatchq.Queue
}

// Server is the admin endpoint HTTP server bound to a unix socket.
type Server struct {
	socketPath string
	mux        *http.ServeMux
	srv        *http.Server
	listener   net.Listener
	deps       ServerDeps
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

	// --- cognition -------------------------------------------------------
	s.mux.HandleFunc("POST /admin/cognition/supervisor/spawn", s.supervisorSpawnHandler)
	s.mux.HandleFunc("POST /admin/cognition/decision/record", s.decisionRecordHandler)
	s.mux.HandleFunc("GET /admin/cognition/invocation/find-by-id", s.invocationFindByIDHandler)
	s.mux.HandleFunc("POST /admin/cognition/invocation/save", s.invocationSaveHandler)
	s.mux.HandleFunc("POST /admin/cognition/invocation/update-status-to-terminal", s.invocationUpdateStatusToTerminalHandler)
	s.mux.HandleFunc("GET /admin/cognition/decision/find-by-invocation-id", s.decisionFindByInvocationIDHandler)

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

	// --- identity --------------------------------------------------------
	s.mux.HandleFunc("GET /admin/identity/find", s.identityFindHandler)
	s.mux.HandleFunc("POST /admin/identity/register", s.identityRegisterHandler)

	// --- observability ---------------------------------------------------
	s.mux.HandleFunc("GET /admin/observability/event/find-by-id", s.eventFindByIDHandler)
	s.mux.HandleFunc("GET /admin/observability/event/find", s.eventFindHandler)
	s.mux.HandleFunc("POST /admin/observability/query/query", s.queryHandler)
	s.mux.HandleFunc("GET /admin/observability/query/inspect", s.inspectHandler)
	s.mux.HandleFunc("GET /admin/observability/fleet/snapshot", s.fleetSnapshotHandler)
	s.mux.HandleFunc("GET /admin/observability/stats/aggregate", s.statsAggregateHandler)
	s.mux.HandleFunc("GET /admin/observability/logs/open", s.logsOpenHandler)

	// --- dispatch queue (v2.2-A3 — worker daemon drains via these) ------
	s.mux.HandleFunc("GET /admin/dispatch/queue/pull", s.dispatchQueuePullHandler)
	s.mux.HandleFunc("GET /admin/dispatch/queue/peek", s.queuePeekHandler)
	s.mux.HandleFunc("GET /admin/kill/queue/pull", s.killQueuePullHandler)
}
