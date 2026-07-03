package agentruntime

import (
	"context"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
)

// Hooks are the daemon-injected callbacks a LocalRuntime forwards to. In Phase 0a
// only the three inject signals are populated; each closure calls the EXISTING
// AgentController path (c.work / c.wake / c.converse), so routing a command through
// the runtime is behavior-identical to the pre-extraction direct call.
//
// The remaining operations (Start/Stop/SpawnExecutor/…) are declared on Runtime
// but NOT hooked yet — the daemon keeps driving them itself until 0b/0c move the
// implementations into LocalRuntime. A nil hook ⇒ the method returns ErrNotWired
// (or a zero value for the query methods), never a silent no-op.
type Hooks struct {
	// NotifyWork forwards an agent.work brief to the resident session path.
	NotifyWork func(ctx context.Context, req WorkRequest) error
	// NotifyWake forwards an agent.wake nudge to the resident session path.
	NotifyWake func(ctx context.Context, req WakeRequest) error
	// NotifyConverse forwards an agent.converse message to the resident session path.
	NotifyConverse func(ctx context.Context, req ConverseRequest) error
}

// LocalRuntime is the in-process Runtime implementation. In Phase 0a it is a thin
// forwarding skeleton: it owns no session state yet (that migrates in 0b/0c) and
// simply delegates the wired signals to daemon-supplied hooks. It implements the
// full Runtime interface so the daemon can depend on the interface today while the
// implementation fills in behind it.
type LocalRuntime struct {
	agentID string
	hooks   Hooks
}

// NewLocalRuntime constructs a LocalRuntime for one agent with the given hooks.
func NewLocalRuntime(agentID string, hooks Hooks) *LocalRuntime {
	return &LocalRuntime{agentID: agentID, hooks: hooks}
}

// AgentID reports the agent this runtime serves.
func (r *LocalRuntime) AgentID() string { return r.agentID }

var _ Runtime = (*LocalRuntime)(nil)

// === 信号投递（wired in 0a） ===

// NotifyConverse forwards to the injected hook (the existing converse path).
func (r *LocalRuntime) NotifyConverse(ctx context.Context, req ConverseRequest) error {
	if r.hooks.NotifyConverse == nil {
		return ErrNotWired
	}
	return r.hooks.NotifyConverse(ctx, req)
}

// NotifyWork forwards to the injected hook (the existing work path).
func (r *LocalRuntime) NotifyWork(ctx context.Context, req WorkRequest) error {
	if r.hooks.NotifyWork == nil {
		return ErrNotWired
	}
	return r.hooks.NotifyWork(ctx, req)
}

// NotifyWake forwards to the injected hook (the existing wake path).
func (r *LocalRuntime) NotifyWake(ctx context.Context, req WakeRequest) error {
	if r.hooks.NotifyWake == nil {
		return ErrNotWired
	}
	return r.hooks.NotifyWake(ctx, req)
}

// === 尚未迁移（daemon 仍走自身路径；0b/0c 落地） ===

// NotifyWorkAvailable is not yet owned by the runtime — the daemon still drives
// work_available directly (migrates with the executor面 in 0c).
func (r *LocalRuntime) NotifyWorkAvailable(ctx context.Context, taskID string) error {
	return ErrNotWired
}

// Start is not yet owned by the runtime — supervisor session lifecycle migrates in 0b.
func (r *LocalRuntime) Start(ctx context.Context, spec StartSpec) error { return ErrNotWired }

// Stop is not yet owned by the runtime — supervisor session lifecycle migrates in 0b.
func (r *LocalRuntime) Stop(ctx context.Context) error { return ErrNotWired }

// IsRunning is not yet owned by the runtime — reports false until 0b wires session state.
func (r *LocalRuntime) IsRunning() bool { return false }

// SpawnExecutor is not yet owned by the runtime — the executor面 migrates in 0c.
func (r *LocalRuntime) SpawnExecutor(ctx context.Context, req SpawnRequest) (*SpawnResult, error) {
	return nil, ErrNotWired
}

// Tick is not yet owned by the runtime — per-runtime maintenance migrates in 0b/0c.
func (r *LocalRuntime) Tick(ctx context.Context, now time.Time) error { return ErrNotWired }

// Recover is not yet owned by the runtime — executor recovery migrates in 0c.
func (r *LocalRuntime) Recover(ctx context.Context) error { return ErrNotWired }

// SnapshotConcurrency is not yet owned by the runtime — returns nil until 0c moves
// the executor engine in.
func (r *LocalRuntime) SnapshotConcurrency() []concurrency.ExecutorSnapshot { return nil }
