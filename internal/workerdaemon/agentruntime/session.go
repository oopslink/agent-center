package agentruntime

// session.go — the shared value types that the per-agent session execution面 needs,
// moved DOWN into agentruntime (Phase 0b, docs/plans/2026-07-02-agent-repo-
// workspaces-implementation.md §4.0.2). The daemon (workerdaemon) aliases these back
// so its existing code + tests keep compiling; the import arrow stays daemon →
// agentruntime, never back. See runtime.go for the boundary rationale.

import (
	"context"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/runtimefs"
	"github.com/oopslink/agent-center/internal/workerdaemon/tasklog"
)

// Session is the NARROW control surface the runtime needs from one agent's session
// (formerly workerdaemon.agentSession). The supervisor solely owns claude, so this
// exposes only socket-mediated control — Inject / Stop / Detach — never a process
// handle. The real *SupervisorSession and *CodexSession (both in workerdaemon)
// satisfy it; a test fake also satisfies it.
type Session interface {
	// Inject writes msg to claude's held-open stdin over the supervisor socket.
	// Returns ErrSessionClosed once Stop/Detach has begun.
	Inject(ctx context.Context, msg string) error
	// Stop is the EXPLICIT-terminate path: SIGTERM the supervisor, then join the
	// event-pump. Fires OnExit exactly once.
	Stop(ctx context.Context) error
	// Detach is the daemon-shutdown SURVIVAL path: close the socket WITHOUT
	// signalling, so the supervisor + claude keep running. Fires OnExit(nil) once.
	Detach()
}

// SessionStarter is the factory the runtime uses to start a claude supervisor
// session. Production = the workerdaemon supervisor-spawn adapter; tests inject a
// fake. Mirrors the former workerdaemon.sessionStarter.
type SessionStarter = func(ctx context.Context, cfg SupervisorSessionConfig) (Session, error)

// CodexSessionStarter is the cli=codex session factory. It takes the NEUTRAL
// CodexSpec (not workerdaemon's CodexSessionConfig, which references the
// workerdaemon-local codexLauncher — keeping the config in workerdaemon avoids an
// agentruntime → workerdaemon import). The workerdaemon adapter converts CodexSpec
// → CodexSessionConfig at the boundary.
type CodexSessionStarter = func(ctx context.Context, spec CodexSpec) (Session, error)

// CodexSpec is the neutral input the runtime hands the codex starter (Blocker 2,
// Option B). It carries only what startCodexSession set on CodexSessionConfig; the
// workerdaemon adapter fills Launcher + computes the merged env.
type CodexSpec struct {
	AgentID     string
	TasksDir    string
	Binary      string
	Model       string
	DisplayName string
	EnvVars     map[string]string
	OnEvent     func(ev claudestream.StreamEvent)
	OnExit      func(err error)
	Logger      func(msg string)
}

// SupervisorSessionConfig configures a claude supervisor session start. Moved from
// workerdaemon (decision #2); workerdaemon aliases it back. It references only
// neutral types (claudestream / stdlib), so the move keeps the import arrow clean.
// The concrete StartSupervisorSession that CONSTRUCTS the real session stays in
// workerdaemon and consumes this (via the alias).
type SupervisorSessionConfig struct {
	AgentID             string
	HomeDir             string
	MCPConfigPath       string
	TasksDir            string
	BinaryPath          string
	Model               string
	DisplayName         string
	AgentEnv            map[string]string
	PromptDescription   string
	ClaudeBin           string
	Epoch               int
	Generation          int
	ResumeFromSessionID string
	ConcurrencyEnabled  bool
	OnEvent             func(ev claudestream.StreamEvent)
	OnExit              func(err error)
	Logger              func(msg string)
	ComeUpTimeout       time.Duration
	StopGrace           time.Duration
}

// UsageReport is one per-turn usage sample the runtime's turn-end hook reports to
// the center. Moved from workerdaemon (referenced by Reporter.ReportUsage);
// workerdaemon aliases it back.
type UsageReport struct {
	AgentID          string
	Model            string
	TaskID           string
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	At               time.Time
}

// Reporter is the RESULT-feedback seam the runtime depends on (formerly
// workerdaemon.feedbackReporter). *AdminClient satisfies it; tests inject a
// recording fake. Moved DOWN so the runtime depends on the interface, not the
// concrete transport; workerdaemon aliases it back.
type Reporter interface {
	ReportAgentActivity(ctx context.Context, agentID, eventType, payloadJSON, taskRef, interactionRef string, at time.Time) error
	ReportAgentLifecycle(ctx context.Context, agentID, state, errMsg string, at time.Time) error
	ReportMarkSeen(ctx context.Context, agentID, conversationID, messageID string, at time.Time) error
	ReportConverseError(ctx context.Context, agentID, conversationID, summary string, at time.Time) error
	FetchReplyNudges(ctx context.Context, agentID string) ([]string, error)
	ReportUsage(ctx context.Context, u UsageReport) error
	RenewTaskLease(ctx context.Context, agentID, taskID string, at time.Time) error
	ReportRuntimeFsResponse(ctx context.Context, resp runtimefs.Response) error
}

// SessionState is the per-agent SESSION state, moved off workerdaemon.managedAgent
// into a shared struct both the daemon (managedAgent.state) and the LocalRuntime
// (r.state) point to (decision #4, Option A). EVERY field is guarded by the SHARED
// mutex (LocalRuntimeConfig.Mu == &AgentController.mu) — the reviewer redline: the
// runtime's reader-goroutine callbacks (onEvent/onExit) and the daemon's
// drainLeaseRenewals/workViaExecutor guard the identical fields under the identical
// lock, critical sections bit-for-bit preserved. Always held by POINTER on both
// sides; never copied.
type SessionState struct {
	// Session is the live agent session (nil until Start / after exit).
	Session Session

	// Version is the reconcile version this session was started at, captured for a
	// self-heal relaunch (onExit reads it; the daemon's managedAgent.appliedVersion
	// is the reconcile-replay guard — the same value at start).
	Version int

	// wake/converse dedup (bounded FIFO); recreated empty on session restart.
	WakeSeen  map[string]struct{}
	WakeOrder []string

	// HadWork records that work was INJECTED (a WorkItem went active) → drives the
	// self-heal relaunch nudge.
	HadWork bool

	// CurrentTaskID / CurrentConversationID are the last-injected work / converse
	// context for the L2 no-silent-failure surface (mutually exclusive).
	CurrentTaskID         string
	CurrentConversationID string

	// ToolNames correlates a claude tool_use_id → tool_name within a turn.
	ToolNames map[string]string

	// W4 per-task log sink for the in-flight task.
	TaskLog   *tasklog.Writer
	TaskLogID string
	EventSeq  uint64

	// EventTaskID / LastEventTaskID: the task the W3/W4 local sink routes events to,
	// derived from the agent's own start_task/complete_task MCP calls.
	EventTaskID     string
	LastEventTaskID string

	// Carried across a crash (self-heal gets no fresh reconcile).
	Model             string
	DisplayName       string
	PromptDescription string
	EnvVars           map[string]string
	CLI               string

	// Rate-limit window remembered for the current turn.
	RLRetryAfterSecs int
	RLResetAtUnix    int64

	// RateLimitResumeAt is the shared, reason-agnostic resume slot (rate-limit OR
	// transient API error); OnTick/Tick injects the resume nudge when due.
	RateLimitResumeAt time.Time

	// APIErrorRetries counts consecutive transient-API-error resumes for the turn.
	APIErrorRetries int

	// Lifecycle coordination (onExit three-state).
	ExpectedStop  bool
	Detaching     bool
	LifecycleOnce sync.Once
}

// UsageTaskAtResult returns the task this turn's usage should be billed to and
// consumes the just-completed-task carry-over (issue-af03da2f). The CALLER must
// hold the shared mutex.
func (s *SessionState) UsageTaskAtResult() string {
	t := s.EventTaskID
	if t == "" {
		t = s.LastEventTaskID
	}
	s.LastEventTaskID = ""
	return t
}
