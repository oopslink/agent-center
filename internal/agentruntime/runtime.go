// Package agentruntime is the per-agent execution面 extracted out of the worker
// daemon's AgentController. The daemon holds ONE Runtime per managed agent and
// routes every signal that must reach the supervisor session (converse / work /
// wake / work_available) through it, instead of touching the session directly.
//
// Why this boundary exists (docs/plans/2026-07-02-agent-repo-workspaces-implementation.md
// §2.3 / §4.0): under the future k8s deployment the supervisor session lives in a
// per-agent pod, so the daemon can no longer call sess.Inject() directly — it must
// go through the Runtime interface, which today is an in-process LocalRuntime and
// tomorrow can be an RPC stub to a pod. Defining the contract now lets the daemon
// depend on the interface while the implementation migrates in later phases.
//
// Import direction: this package MUST NOT import internal/workerdaemon — the arrow
// is daemon → runtime, never back. Shared value types (requests below) are declared
// here so the daemon converts its wire payloads into them at the boundary.
//
// Phase 0a scope (this file): the interface + a thin LocalRuntime skeleton. Only
// NotifyConverse / NotifyWork / NotifyWake are wired — they forward to hooks the
// daemon injects, which call the EXISTING controller paths (behavior unchanged, no
// logic moved). Session lifecycle (Start/Stop) lands in 0b and the executor面
// (SpawnExecutor/Recover/Tick) in 0c; until then those methods return ErrNotWired.
package agentruntime

import (
	"context"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
)

// ErrNotWired is returned by Runtime methods whose implementation has not yet been
// migrated into LocalRuntime (Phase 0b/0c). In Phase 0a the daemon never routes
// these through the Runtime — it keeps driving its own paths — so a call reaching
// this sentinel signals a wiring mistake, loudly, rather than silently no-op'ing.
var ErrNotWired = errors.New("agentruntime: method not wired in this phase")

// Runtime is the per-agent execution runtime. The daemon holds one per agent.
// Current implementation: in-process LocalRuntime. Future k8s: an RPC stub → pod.
//
// See docs/plans/2026-07-02-agent-repo-workspaces-implementation.md §4.0.1 for the
// full contract and the migration boundary (what moves in 0a/0b/0c).
type Runtime interface {
	// === 信号投递（daemon → runtime → supervisor session） ===

	// NotifyWorkAvailable: 有新任务的信号（v2.8.1 #278 pull model）。
	// FIXME(phase-6): 过渡期直接触发 executor fork（现有 workAvailable 行为）。
	// Phase 6（supervisor 接入 fork_executor MCP tool）后必须改为 inject nudge。
	NotifyWorkAvailable(ctx context.Context, taskID string) error

	// NotifyConverse: 人类发来的日常对话消息（DM/channel），注入 supervisor session。
	NotifyConverse(ctx context.Context, req ConverseRequest) error

	// NotifyWork: agent.work brief，注入 supervisor session。
	NotifyWork(ctx context.Context, req WorkRequest) error

	// NotifyWake: wake nudge（task 对话新消息），注入 supervisor session。
	NotifyWake(ctx context.Context, req WakeRequest) error

	// === Supervisor session 生命周期 ===

	// Start: 启动 supervisor session（cli=claude-code 或 codex）。
	Start(ctx context.Context, spec StartSpec) error

	// Stop: 停止 supervisor session。
	Stop(ctx context.Context) error

	// IsRunning: session 是否存活。
	IsRunning() bool

	// === Executor 管理（supervisor MCP tool → runtime） ===

	// SpawnExecutor: supervisor 决定 fork 时调用（前置检查 → repo/worktree →
	// start_task → spawn）。整个序列由 per-runtime mutex 串行化以防 double-fork。
	SpawnExecutor(ctx context.Context, req SpawnRequest) (*SpawnResult, error)

	// === 周期性运维 ===

	// Tick: daemon OnTick 驱动的周期性维护（self-heal 重试、rate-limit / API-error
	// resume、executor watchdog sweep）。
	Tick(ctx context.Context, now time.Time) error

	// Recover: daemon 重启后重建 in-flight executor 状态。
	Recover(ctx context.Context) error

	// SnapshotConcurrency: heartbeat 上报的实时 executor 视图。
	SnapshotConcurrency() []concurrency.ExecutorSnapshot
}

// WorkRequest is the runtime-facing form of an agent.work command. It mirrors the
// daemon's workPayload; the daemon converts payload → request at the boundary.
type WorkRequest struct {
	AgentID string
	TaskID  string
	TaskRef string
	Brief   string
}

// WakeRequest is the runtime-facing form of an agent.wake command. It mirrors the
// daemon's wakePayload (ConversationID/MessageID drive the read-state cursor
// advance; RootMessageID is the thread root when the trigger was in a thread).
type WakeRequest struct {
	AgentID        string
	TaskID         string
	TaskRef        string
	ConversationID string
	MessageID      string
	MessageText    string
	RootMessageID  string
}

// ConverseRequest is the runtime-facing form of an agent.converse command. It
// mirrors the daemon's conversePayload field-for-field so the forwarded brief is
// byte-identical to the pre-extraction path.
type ConverseRequest struct {
	AgentID         string
	ConversationID  string
	ConvKind        string
	ConvName        string
	SenderRef       string
	SenderDisplay   string
	MessageID       string
	MessageText     string
	RootMessageID   string
	AttachmentCount int
	OwnerRef        string
}

// StartSpec parameterises supervisor session start. Mirrors the daemon's
// startSession/startCodexSession arguments (reconcile-derived). Consumed by
// LocalRuntime.Start once session lifecycle migrates in Phase 0b.
type StartSpec struct {
	AgentID            string
	Version            int
	ForkResume         bool
	Resume             bool
	Model              string
	DisplayName        string
	CLI                string
	PromptDescription  string
	EnvVars            map[string]string
	ConcurrencyEnabled bool
}

// SpawnRequest is the input for SpawnExecutor (supervisor → runtime fork entry).
type SpawnRequest struct {
	TaskID string
	// Model is the supervisor-specified model override (empty ⇒ runtime resolves).
	Model string
	// Context is the supervisor-aggregated context (empty ⇒ runtime builds it from
	// task detail).
	Context string

	// redrive marks a spawn issued BY the repo-source prewarm gate after a background
	// materialize (issue-13e7bfe8). Unexported on purpose: only this package can set it,
	// so external callers always get the plain control-path behavior.
	//
	// It exists to break a livelock: a re-drive that finds the source already stale
	// again would defer into ANOTHER prewarm, which re-drives, which defers… forever.
	// That is reachable in production whenever one fetch takes longer than
	// SourceFreshFor (a big repo on a slow link), not just in theory — the regression
	// suite hit it as an infinite loop. A re-drive therefore accepts the source the
	// episode just materialized even if the freshness window has already lapsed.
	redrive bool
}

// SpawnResult is the outcome of a successful SpawnExecutor.
type SpawnResult struct {
	ExecutorID string
	Model      string
	CLI        string
}
