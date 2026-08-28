package agentruntime

// local_runtime.go — the in-process Runtime. Phase 0b moves the per-agent SESSION
// execution面 (session lifecycle + onEvent/onExit + self-heal/rate-limit/api-error/
// taskevents) OFF workerdaemon.AgentController INTO here as REAL implementations.
//
// Locking (T839 §4.1 去共享状态): the runtime now OWNS its SessionState lock (r.mu,
// exposed to the daemon via StateMu()) instead of sharing &AgentController.mu.
// onEvent/onExit (reader goroutine) and the daemon's drainLeaseRenewals/workViaExecutor
// guard the identical SessionState fields under this per-agent lock — critical sections
// preserved. c.mu still guards the daemon's c.agents map (the RemoveAgent seam takes it).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/reporepo"
	"github.com/oopslink/agent-center/internal/agentruntime/sessioninstance"
	"github.com/oopslink/agent-center/internal/agentruntime/skillscan"
	"github.com/oopslink/agent-center/internal/agentruntime/taskexec"
	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/cognition/memory"
	"github.com/oopslink/agent-center/internal/mcphost"
	"github.com/oopslink/agent-center/internal/supervisormanager"
)

// Shared constants moved down with the session面 (workerdaemon aliases them back).
const (
	// CLICodex is the canonical agent.cli for the codex path.
	CLICodex = "codex"
	// MCPServerName is the mcpServers map key for the per-agent worker mcp-host server.
	MCPServerName = "agent-center"
	// WakeDedupCap bounds the per-agent wake/converse dedup set.
	WakeDedupCap = 256
	// defaultCodexRecycleInputTokens is disabled by default. A fixed input-token
	// threshold caused large-memory agents to rebuild fresh sessions repeatedly,
	// reopening the MCP tool-index initialization window after every clean turn.
	// Operators may opt in with CodexRecycleInputTokens > 0 for a specific runtime.
	defaultCodexRecycleInputTokens = -1
	// defaultCodexRecycleCleanTurns bounds one long-lived Codex logical session by clean
	// turn count when usage accounting is unavailable or consistently small.
	defaultCodexRecycleCleanTurns = 8
	// DefaultResumeNudge is injected to re-drive an interrupted turn (self-heal /
	// boot relaunch / rate-limit / api-error resume) when cfg.ResumeNudge is unset.
	DefaultResumeNudge = "Resume your current task."
	// AgentCLIMarkerFile records the agent's execution cli under the agent home.
	AgentCLIMarkerFile = "agent.cli"
)

// LocalRuntimeConfig carries the deps the moved session面 needs. The daemon builds
// one per agent (cheap value copy — every field is a pointer/func/scalar, NO
// sync.Mutex by value → copylocks-clean) and passes it to NewLocalRuntime.
type LocalRuntimeConfig struct {
	AgentID string

	Reporter     Reporter
	Starter      SessionStarter
	CodexStarter CodexSessionStarter

	// ToolCaller reaches the center agent-tools endpoints (get_task / start_task /
	// complete_task / block_task / report_usage) for the executor fork + W2 writeback.
	// A func seam so it is read LIVE (the daemon owns c.cfg.ToolCaller, which tests
	// wire after the runtime is built — matching the pre-move c.cfg.ToolCaller read).
	// nil func / nil result ⇒ the fork path leaves tasks queued and the Monitor
	// degrades to reap-and-free-slot with no center writeback.
	ToolCaller func() ToolCaller

	WorkerID          string
	AdminURL          string
	WorkerToken       string
	ServerFingerprint string
	BinaryPath        string
	ClaudeBinary      string
	CodexBinary       string
	AgentHomeBase     string

	// Log is the daemon's prefixed logger (== AgentController.log) so log lines stay
	// byte-identical to before the move.
	Log func(format string, args ...any)
	// Now is the clock seam (nil → time.Now).
	Now func() time.Time

	StopGrace time.Duration
	// DisableUsageReport is read LIVE (the daemon owns the ops kill-switch, which may
	// be toggled after the runtime is built). nil ⇒ reporting on.
	DisableUsageReport func() bool
	ResumeNudge        string

	// OnFatal is called when the supervisor session crashes unexpectedly (T860 piece ③,
	// controller model): the agent-runtime process signals itself to exit via this seam,
	// and the worker's launcher rebuilds it (bounded backoff/max-attempts) — replacing
	// the retired in-process SelfHealStore. nil ⇒ no-op (the single-claude/daemon-less
	// test path).
	OnFatal func(reason string)

	RateLimitDefaultBackoff time.Duration
	RateLimitMinBackoff     time.Duration
	RateLimitMaxBackoff     time.Duration

	APIErrorBackoffBase time.Duration
	APIErrorBackoffCap  time.Duration
	APIErrorMaxRetries  int

	// CodexRecycleInputTokens exits the agent-runtime after a successful Codex turn at
	// or above this input size, letting the worker rebuild a fresh Codex session. 0 uses
	// the production default; a negative value disables the token trigger.
	CodexRecycleInputTokens int
	// CodexRecycleCleanTurns exits after this many successful Codex turns in one
	// logical session. 0 uses the production default; a negative value disables the
	// count trigger.
	CodexRecycleCleanTurns int

	TaskDirManager  *taskexec.DirManager
	SegmentMaxBytes int64
	TaskLogMaxBytes int64
	EventWriter     *taskexec.EventStreamWriter

	// Materializer is the executor_git_worktree=ON repo-workspace port: shared
	// canonical source + per-executor git worktree. Nil when the switch is OFF.
	Materializer reporepo.RepoMaterializer
	// CloneMaterializer is the executor_git_worktree=OFF repo-workspace port:
	// independent per-executor clones directly under the executor workspace. It must
	// not use shared canonical sources or linked worktrees.
	CloneMaterializer interface {
		PrepareClone(context.Context, reporepo.RepoTarget, reporepo.CloneRequest) (reporepo.PreparedClone, error)
	}
	// ReposRoot is the canonical <agent_home>/repos root the Materializer is anchored
	// at (informational; the Materializer already carries it). Empty when the flag is off.
	ReposRoot string

	// Repo-source prewarm tunables (issue-13e7bfe8 layer 1 — see source_prewarm.go).
	// All zero ⇒ the defaults; tests collapse the timings to stay deterministic.
	//
	// SourcePrewarmTimeout bounds ONE background EnsureSource. SourceFreshFor is how
	// long a materialized source is reused without a re-fetch. SourcePrewarmBackoff is
	// the pause between failed attempts (negative ⇒ zero, for tests).
	// SourcePrewarmAttempts is the bounded retry budget before a task is failed loudly.
	SourcePrewarmTimeout  time.Duration
	SourceFreshFor        time.Duration
	SourcePrewarmBackoff  time.Duration
	SourcePrewarmAttempts int
	// ClonePrepareTimeout bounds one executor_git_worktree=OFF independent clone.
	// The clone runs off the control path; zero uses the production default.
	ClonePrepareTimeout time.Duration

	// SkillLayerRoots resolves the four claude-code skill-layer directories to scan for
	// the OBSERVED installed-skill report (issue-4a45e9cc). It is injected so tests can
	// point at temp dirs; nil ⇒ the runtime's default resolver derived from the agent
	// home (home/skills + tasks/.claude/skills = project) and $HOME/.claude (user +
	// plugins). home is the agent home dir, tasksDir is its project cwd.
	SkillLayerRoots func(home, tasksDir string) skillscan.LayerRoots

	// MCPPreflight validates that the supervisor's agent-center MCP surface exists
	// before a Codex session is allowed to start. nil ⇒ production mcphost catalog
	// preflight. Tests may replace it to force the blocked path.
	MCPPreflight func(context.Context, mcphost.Config, ...string) error
}

// LocalRuntime is the in-process Runtime for one agent.
type LocalRuntime struct {
	cfg   LocalRuntimeConfig
	state *SessionState

	// mu is the PER-AGENT SessionState lock (T839 §4.1 去共享状态). It replaces the
	// formerly SHARED cfg.Mu (== &AgentController.mu): the runtime now owns the lock
	// that guards its own SessionState, and the daemon reaches it via StateMu() during
	// the transitional decoupling. Critical sections are bit-for-bit those the shared
	// mutex protected before the move.
	mu sync.Mutex

	// bg tracks best-effort clean-turn goroutines (formerly &AgentController.bg) so
	// Shutdown drains them via WaitBG(). Per-agent now (去共享状态).
	bg sync.WaitGroup

	// lifecycleCtx is the owner cancellation boundary for runtime-owned background
	// work. It deliberately does not inherit control-command request deadlines; Stop
	// cancels it and then joins the goroutines it owns before test/server teardown.
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc

	// lifeMu/lifeWG are the single lifecycle admission protocol for runtime-owned
	// background work and executor forks. Stop flips stopping under lifeMu before
	// waiting, so no goroutine/process path can Add after teardown observes zero.
	lifeMu   sync.Mutex
	lifeWG   sync.WaitGroup
	stopping bool

	// exec is the per-agent concurrent-execution wiring (Phase 0c), installed via
	// AttachExecutor when the agent opts into concurrency. nil ⇒ single-claude inject
	// path. Guarded by r.mu exactly as ma.exec was guarded by c.mu.
	exec *ExecutorEngine
	// execDrainWG tracks this-process executor drain goroutines launched by this
	// runtime so Stop can quiesce filesystem/writeback activity before teardown.
	execDrainWG sync.WaitGroup

	// sources is the per-repo_key repo-source prewarm gate (issue-13e7bfe8 layer 1):
	// it keeps `git clone` OFF the control-command path and owns the background
	// materialize + re-drive of the tasks that deferred on it. See source_prewarm.go.
	// Has its own lock; never nested with r.mu or forkStateMu.
	sources sourceGate
	// clones keeps executor_git_worktree=OFF network clones off the control path.
	// It is keyed by task because each task gets one independent executor checkout.
	clones cloneGate

	// forkStateMu guards ONLY the short-lived fork identity registries below. It is
	// deliberately never held across center RPC, git/worktree I/O, or process spawn:
	// different tasks must be able to fill distinct executor slots concurrently. The
	// executor Pool owns the atomic ≤N reservation; this registry owns per-task
	// idempotency and the pre-pool liveness gap used by orphan pruning.
	// SEPARATE from r.mu (the SessionState lock); never guards the same field.
	forkStateMu        sync.Mutex
	forkingTasks       map[string]struct{} // task is inside fetch/prepare/admit/launch
	taskExecutors      map[string]string   // task → live/reconciling executor id
	preparingExecutors map[string]struct{} // worktree may exist before Pool sees it

	// execConfig caches the concurrency ExecutorConfig this runtime last built its
	// engine from (T848 §4.4 migration: was AgentController.execConfig, keyed by agent).
	// A durable, per-runtime runtime now self-holds it so boot self-reconcile can
	// re-derive the executor env / model routing for a recovery relaunch WITHOUT a
	// daemon-side cache. Guarded by r.mu; execConfigSet gates "have we built one yet".
	execConfig    ExecutorConfig
	execConfigSet bool

	// recoveredOnce is the per-runtime "executor crash-recovery has run once" guard
	// (T848 §4.4 migration: was AgentController.recoveredExec[agentID]). A durable,
	// per-runtime runtime owns its own guard so a second in-process engine rebuild
	// does NOT re-scan the executor dirs and double-finalize an orphan already
	// classified this process. Guarded by r.mu.
	recoveredOnce bool

	// fuseExecutor is a test seam for the block-fuse circuit-break (issue-88e32d98): when
	// a lease renew comes back ErrLeaseRevoked, drainLeaseRenewals calls it to graceful-
	// kill the in-flight executor. nil (production) routes to the executor engine's
	// FuseExecutorForTask; tests set it to record the fuse without signalling a real pid.
	fuseExecutor func(ctx context.Context, taskID string) (bool, error)

	// nextLeaseRenewAt / lastGCAt rate-limit the per-Tick execution-lease renewal and
	// task-dir GC (T860 piece ③: moved off the daemon AgentController.OnTick into the
	// agent-runtime process itself — self-contained, k8s-aligned). Guarded by r.mu.
	nextLeaseRenewAt time.Time
	lastGCAt         time.Time

	// agentRef is the agent's STABLE identity-member ref (bare, e.g. "agent-20d5e05c"),
	// seeded once at Boot from ResumeState (SetAgentRef). It is the id namespace
	// task.assignee uses ("agent:"+ref); the executor self-recovery should-continue check
	// compares a task's assignee against THIS ref, not the ULID cfg.AgentID, so a crashed
	// executor's still-mine task is not misjudged "reassigned" and IS tier-1 resumed
	// (T872). SEPARATE from execConfig (which a reconcile overwrites) because identity is
	// stable and must survive config changes. Empty ⇒ identityRef falls back to the ULID.
	// Guarded by r.mu.
	agentRef string

	// skill observability (issue-4a45e9cc): lastSkillFingerprint holds the hash of the
	// installed-skill set last reported to the center, so a Tick re-reports ONLY when it
	// changes ("变了才重报"); lastSkillScanAt rate-limits the disk scan so a fast (active)
	// heartbeat cadence does not re-walk the skill tree every few seconds. Guarded by r.mu.
	lastSkillFingerprint string
	lastSkillScanAt      time.Time

	// pending is the durable option-b judgment store (issue-68ccb310): executor results
	// awaiting the supervisor's judged completion. nil ⇒ the reconcile is disabled
	// (degraded/test). nextPendingReconcileAt rate-limits the per-Tick reconcile sweep.
	pending                *pendingStore
	nextPendingReconcileAt time.Time
}

// SetAgentRef seeds the agent's stable identity-member ref (from ResumeState at Boot).
// A blank ref is ignored so a partial/old ResumeState never clears a good value.
func (r *LocalRuntime) SetAgentRef(ref string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return
	}
	r.mu.Lock()
	r.agentRef = ref
	r.mu.Unlock()
}

// identityRef returns the agent's center identity-member ref (the namespace
// task.assignee uses), falling back to the ULID cfg.AgentID when the ref was never
// seeded (e.g. an old center without the agent_ref projection). The self-recovery
// should-continue check keys on this so a crashed executor's task is matched to THIS
// agent instead of being falsely read as reassigned (T872).
func (r *LocalRuntime) identityRef() string {
	r.mu.Lock()
	ref := r.agentRef
	r.mu.Unlock()
	if ref != "" {
		return ref
	}
	return r.cfg.AgentID
}

// cacheExecConfig records the ExecutorConfig this runtime's engine was built from
// (T848 §4.4 migration). Boot self-reconcile reads it back to relaunch a recovered
// executor with the same env / model routing. Idempotent; last write wins.
func (r *LocalRuntime) cacheExecConfig(pl ExecutorConfig) {
	r.mu.Lock()
	r.execConfig = pl
	r.execConfigSet = true
	r.mu.Unlock()
}

// cachedExecConfig returns the cached ExecutorConfig and whether one was ever set.
func (r *LocalRuntime) cachedExecConfig() (ExecutorConfig, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.execConfig, r.execConfigSet
}

// markRecoveredOnce sets the per-runtime recovery guard and reports whether THIS
// call was the first (i.e. recovery should run now). Mirrors the daemon's
// firstAttach := !c.recoveredExec[id]; c.recoveredExec[id] = true.
func (r *LocalRuntime) markRecoveredOnce() (first bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.recoveredOnce {
		return false
	}
	r.recoveredOnce = true
	return true
}

// WaitBG blocks until this agent's best-effort clean-turn goroutines have drained
// (Shutdown calls it per-runtime, replacing the old shared c.bg.Wait()).
func (r *LocalRuntime) WaitBG() {
	r.bg.Wait()
	r.lifeWG.Wait()
}

var _ Runtime = (*LocalRuntime)(nil)

// NewLocalRuntime builds a LocalRuntime over the shared state pointer.
func NewLocalRuntime(cfg LocalRuntimeConfig, state *SessionState) *LocalRuntime {
	lifecycleCtx, lifecycleCancel := context.WithCancel(context.Background())
	r := &LocalRuntime{cfg: cfg, state: state, lifecycleCtx: lifecycleCtx, lifecycleCancel: lifecycleCancel}
	// option b (issue-68ccb310): load the durable pending-judgment store from the agent
	// home so a relaunch re-drives dropped judgments (boot recovery). nil when the home
	// isn't resolvable (single-claude/test path) ⇒ the reconcile is disabled.
	if strings.TrimSpace(cfg.AgentHomeBase) != "" && strings.TrimSpace(cfg.AgentID) != "" {
		r.pending = newPendingStore(filepath.Join(cfg.AgentHomeBase, "agents", cfg.AgentID, "pending_judgments.json"))
	}
	return r
}

// withState runs fn with the SessionState held under r.mu. Every SessionState field
// is guarded by this lock (see session.go), and the runtime hands out goroutines
// (launchExecutorNow → drainExecutor → resetRecoveredTask) that write those fields
// concurrently — so this is the ONLY way to reach them. Deliberately unexported and
// deliberately closure-shaped: there is no `*SessionState` escape hatch to forget the
// lock on, because a bare-pointer accessor made the unlocked read the shortest thing
// to type and it went unnoticed twice (issue-573e81c4, then I105).
//
// fn must not call back into a LocalRuntime method that takes r.mu.
func (r *LocalRuntime) withState(fn func(s *SessionState)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r.state)
}

// CurrentTaskID returns the last-injected WorkItem's task id under r.mu.
func (r *LocalRuntime) CurrentTaskID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.CurrentTaskID
}

// injectSession delivers text to this agent's supervisor session as a turn (option b,
// issue-68ccb310). Guards r.state under r.mu; a nil session (no supervisor / mid-
// restart) errors so the writeback surfaces "cannot judge" rather than auto-completing.
// injectToSupervisor wraps this to ALSO record a pending judgment; the reconcile uses
// injectSession directly for nudges (which must NOT reset the pending clock).
func (r *LocalRuntime) injectSession(ctx context.Context, text string) error {
	r.mu.Lock()
	sess := r.state.Session
	r.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("agentruntime: no supervisor session to inject (agent %s)", r.cfg.AgentID)
	}
	if err := sess.Inject(ctx, text); err != nil {
		r.signalFatalIfSessionClosed("inject_session", err)
		return err
	}
	return nil
}

// injectToSupervisor is the writeback's option-b seam: deliver a judgment prompt AND
// record the pending judgment so the reconcile re-drives it if the supervisor drops
// it. Recorded (keyed by taskRef) only after a successful inject.
func (r *LocalRuntime) injectToSupervisor(ctx context.Context, taskRef, text string) error {
	if err := r.injectSession(ctx, text); err != nil {
		return err
	}
	if r.pending != nil && strings.TrimSpace(taskRef) != "" {
		r.pending.record(taskRef, text, r.now())
	}
	return nil
}

// AgentID reports the agent this runtime serves.
func (r *LocalRuntime) AgentID() string { return r.cfg.AgentID }

func (r *LocalRuntime) now() time.Time {
	if r.cfg.Now != nil {
		return r.cfg.Now()
	}
	return time.Now()
}

func (r *LocalRuntime) runtimeContext(timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		return context.WithCancel(r.lifecycleCtx)
	}
	return context.WithTimeout(r.lifecycleCtx, timeout)
}

func (r *LocalRuntime) runtimeStopped() bool {
	select {
	case <-r.lifecycleCtx.Done():
		return true
	default:
		return false
	}
}

func (r *LocalRuntime) beginRuntimeWork() bool {
	r.lifeMu.Lock()
	defer r.lifeMu.Unlock()
	if r.stopping {
		return false
	}
	r.lifeWG.Add(1)
	return true
}

func (r *LocalRuntime) endRuntimeWork() { r.lifeWG.Done() }

func (r *LocalRuntime) closeRuntimeAdmission() {
	r.lifeMu.Lock()
	r.stopping = true
	r.lifeMu.Unlock()
}

func (r *LocalRuntime) isStopping() bool {
	r.lifeMu.Lock()
	defer r.lifeMu.Unlock()
	return r.stopping
}

func (r *LocalRuntime) sleepRuntime(d time.Duration) bool {
	if d <= 0 {
		return !r.runtimeStopped()
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-r.lifecycleCtx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func waitGroupContext(ctx context.Context, wg *sync.WaitGroup) error {
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *LocalRuntime) waitOwnedBackground(ctx context.Context) error {
	if err := waitGroupContext(ctx, &r.lifeWG); err != nil {
		return fmt.Errorf("runtime lifecycle work: %w", err)
	}
	if err := waitGroupContext(ctx, &r.sources.wg); err != nil {
		return fmt.Errorf("source prewarm: %w", err)
	}
	if err := waitGroupContext(ctx, &r.clones.wg); err != nil {
		return fmt.Errorf("clone prewarm: %w", err)
	}
	if err := waitGroupContext(ctx, &r.execDrainWG); err != nil {
		return fmt.Errorf("executor drains: %w", err)
	}
	if err := waitGroupContext(ctx, &r.bg); err != nil {
		return fmt.Errorf("runtime background: %w", err)
	}
	return nil
}

func (r *LocalRuntime) fuseLiveExecutors(ctx context.Context) {
	ee := r.execEngine()
	if ee == nil || ee.monitor == nil {
		return
	}
	r.forkStateMu.Lock()
	tasks := make([]string, 0, len(r.taskExecutors))
	for taskID := range r.taskExecutors {
		tasks = append(tasks, taskID)
	}
	r.forkStateMu.Unlock()
	for _, taskID := range tasks {
		if _, err := ee.monitor.FuseKillTask(ctx, taskID); err != nil {
			r.log("stop agent=%s task=%s fuse executor: %v", r.cfg.AgentID, taskID, err)
		}
	}
}

// toolCaller resolves the live center agent-tool transport (nil when unwired).
func (r *LocalRuntime) toolCaller() ToolCaller {
	if r.cfg.ToolCaller == nil {
		return nil
	}
	return r.cfg.ToolCaller()
}

func (r *LocalRuntime) log(format string, args ...any) {
	if r.cfg.Log != nil {
		r.cfg.Log(format, args...)
	}
}

// resumeNudgeText is the message injected to re-drive an interrupted turn.
func (r *LocalRuntime) resumeNudgeText() string {
	if msg := strings.TrimSpace(r.cfg.ResumeNudge); msg != "" {
		return r.cfg.ResumeNudge
	}
	return DefaultResumeNudge
}

// agentPaths mirrors AgentController.agentPaths (kept in lockstep — the layout MUST
// match the daemon's boot scan).
func (r *LocalRuntime) agentPaths(agentID string) (home, tasksDir, plansDir string, err error) {
	if strings.TrimSpace(r.cfg.AgentHomeBase) == "" {
		return "", "", "", errors.New("agent_controller: agent_home_base required")
	}
	if strings.TrimSpace(r.cfg.WorkerID) == "" {
		return "", "", "", errors.New("agent_controller: worker_id required")
	}
	if strings.TrimSpace(agentID) == "" {
		return "", "", "", errors.New("agent_controller: agent_id required")
	}
	home = filepath.Join(r.cfg.AgentHomeBase, "agents", agentID)
	tasksDir = filepath.Join(home, "tasks")
	plansDir = filepath.Join(home, "plans")
	return home, tasksDir, plansDir, nil
}

// ---------------------------------------------------------------------------
// 信号投递 — real inject implementations (the non-executor branch).
// ---------------------------------------------------------------------------

// NotifyWork injects the work brief into the resident session (the daemon routes
// the executor branch to workViaExecutor before reaching here).
func (r *LocalRuntime) NotifyWork(ctx context.Context, req WorkRequest) error {
	agentID := req.AgentID
	if !r.beginRuntimeWork() {
		return ErrRuntimeStopping
	}
	defer r.endRuntimeWork()
	r.mu.Lock()
	sess := r.state.Session
	ee := r.exec
	r.mu.Unlock()
	if sess == nil {
		return fmt.Errorf("agent_controller: work for agent=%s but no running session (retry after reconcile)", agentID)
	}

	// Executor branch (Phase 0c): a concurrency-enabled agent forks an executor for
	// the brief instead of injecting into the resident claude. Mirrors today's
	// routeWork exec-vs-session decision (ma.exec != nil), which required a live
	// session first (checked above). The fork serializes under forkMu (red line #1).
	//
	// I105 Phase 1 adds the per-NODE override on top of that per-AGENT decision: an
	// explicitly-marked supervisor_inline node falls through to the inject path below
	// EVEN WHEN ee != nil (concurrency on). RED LINE: only an explicit
	// supervisor_inline suppresses the fork — missing / empty / executor_fork / an
	// unknown value all still fork exactly as before, so no ordinary Dev node can be
	// starved by an absent or malformed field.
	//
	// NOTE: no center-side producer emits agent.work today — the per-WorkItem
	// agent.work re-emit was retired with AgentWorkItem (I14/F7). work_available now
	// nudges the supervisor, and explicit fork_executor calls SpawnExecutor instead.
	// This branch remains as defense-in-depth if an agent.work producer is restored.
	if ee != nil && !routesSupervisorInline(req.DispatchMode) {
		// issue-d118b5dc instrument: the agent.work → NotifyWork route resolves to a FORK
		// here (concurrency ON, ee != nil). Fail-loud decision log so a double fan-out (this
		// firing for a task that ALSO got an explicit fork_executor, or a single-active
		// inject) is visible with the deciding namespace/mode. Instrument-only, no behavior change.
		r.log("DISPATCH-DECISION route=NotifyWork(agent.work) dispatch_mode=executor-fork agent_namespace=%s task_id=%s — forking via executor engine",
			agentID, req.TaskID)
		r.createTaskDir(agentID, req.TaskID)
		return r.workViaExecutor(ctx, req, ee)
	}
	// issue-d118b5dc instrument: the agent.work → NotifyWork route resolves to an INJECT
	// here — either single-active (concurrency OFF, ee == nil) or, since I105, an explicit
	// per-node supervisor_inline override with concurrency ON. Mode should be XOR with any
	// fork path — if this AND an explicit fork_executor both fire for one ready event, that is
	// the ① dual fan-out.
	inlineMode := "single-active-inject"
	if ee != nil {
		inlineMode = "supervisor-inline" // I105: overrode an otherwise-forking agent
	}
	r.log("DISPATCH-DECISION route=NotifyWork(agent.work) dispatch_mode=%s agent_namespace=%s task_id=%s — injecting into supervisor session",
		inlineMode, agentID, req.TaskID)

	if r.cfg.TaskDirManager != nil {
		_, tasksDir, _, pathErr := r.agentPaths(agentID)
		if pathErr != nil {
			r.log("agent=%s task=%s resolve paths: %v", agentID, req.TaskID, pathErr)
		} else {
			now := r.now()
			meta := taskexec.TaskExecutionMeta{
				TaskID:    req.TaskID,
				Status:    taskexec.StatusPending,
				CreatedAt: now,
				UpdatedAt: now,
			}
			if createErr := r.cfg.TaskDirManager.Create(tasksDir, meta, taskexec.ExecutionContext{}); createErr != nil {
				r.log("agent=%s task=%s create task dir: %v", agentID, req.TaskID, createErr)
			}
		}
	}

	if err := sess.Inject(ctx, req.Brief); err != nil {
		r.signalFatalIfSessionClosed("work inject", err)
		return fmt.Errorf("agent_controller: inject agent=%s: %w", agentID, err)
	}

	r.mu.Lock()
	r.state.HadWork = true
	if req.TaskID != "" {
		r.state.CurrentTaskID = req.TaskID
		r.state.CurrentConversationID = ""
	}
	r.mu.Unlock()
	return nil
}

// NotifyWake injects a posted task message into the resident session (dedup +
// mark-seen), mirroring the old wake().
func (r *LocalRuntime) NotifyWake(ctx context.Context, req WakeRequest) error {
	agentID := req.AgentID
	r.mu.Lock()
	sess := r.state.Session
	if req.MessageID != "" && r.state.WakeSeen != nil {
		if _, seen := r.state.WakeSeen[req.MessageID]; seen {
			r.mu.Unlock()
			r.log("wake agent=%s message=%s already injected — dedup no-op", agentID, req.MessageID)
			return nil
		}
	}
	r.mu.Unlock()

	if sess == nil {
		return fmt.Errorf("agent_controller: wake for agent=%s but no running session (retry after reconcile)", agentID)
	}

	if err := sess.Inject(ctx, req.MessageText); err != nil {
		r.signalFatalIfSessionClosed("wake inject", err)
		return fmt.Errorf("agent_controller: wake inject agent=%s: %w", agentID, err)
	}

	r.recordWake(req.MessageID)

	if req.ConversationID != "" && req.MessageID != "" {
		if err := r.cfg.Reporter.ReportMarkSeen(ctx, agentID, req.ConversationID, req.MessageID, time.Now()); err != nil {
			r.log("wake agent=%s mark-seen conv=%s msg=%s: %v", agentID, req.ConversationID, req.MessageID, err)
		}
	}

	if req.TaskID != "" {
		r.mu.Lock()
		r.state.CurrentTaskID = req.TaskID
		r.state.CurrentConversationID = ""
		r.mu.Unlock()
	}
	return nil
}

// NotifyConverse injects a DM/channel message into the resident session (no
// WorkItem), mirroring the old converse().
func (r *LocalRuntime) NotifyConverse(ctx context.Context, req ConverseRequest) error {
	agentID := req.AgentID
	r.mu.Lock()
	sess := r.state.Session
	if req.MessageID != "" && r.state.WakeSeen != nil {
		if _, seen := r.state.WakeSeen[req.MessageID]; seen {
			r.mu.Unlock()
			r.log("converse agent=%s message=%s already injected — dedup no-op", agentID, req.MessageID)
			return nil
		}
	}
	r.mu.Unlock()

	if sess == nil {
		return fmt.Errorf("agent_controller: converse for agent=%s but no running session (retry after reconcile)", agentID)
	}

	if err := sess.Inject(ctx, BuildConverseBrief(req)); err != nil {
		r.signalFatalIfSessionClosed("converse inject", err)
		return fmt.Errorf("agent_controller: converse inject agent=%s: %w", agentID, err)
	}
	if err := r.persistInterruptedConverse(agentID, req); err != nil {
		return fmt.Errorf("agent_controller: persist interrupted converse agent=%s: %w", agentID, err)
	}
	r.recordWake(req.MessageID)

	if err := r.cfg.Reporter.ReportAgentActivity(
		context.Background(), agentID, agentEventTypeMessageDelivered,
		messageDeliveredPayload(req), "", "", time.Now(),
	); err != nil {
		r.log("converse agent=%s message_delivered report: %v", agentID, err)
	}

	r.mu.Lock()
	r.state.CurrentConversationID = req.ConversationID
	r.state.CurrentTaskID = ""
	r.mu.Unlock()

	if req.ConversationID != "" && req.MessageID != "" {
		if err := r.cfg.Reporter.ReportMarkSeen(ctx, agentID, req.ConversationID, req.MessageID, time.Now()); err != nil {
			r.log("converse agent=%s mark-seen conv=%s msg=%s: %v", agentID, req.ConversationID, req.MessageID, err)
		}
	}
	return nil
}

func (r *LocalRuntime) signalFatalIfSessionClosed(op string, err error) {
	if err == nil || !isSessionClosedInjectError(err) {
		return
	}
	reason := fmt.Sprintf("%s: %v", op, err)
	r.log("agent=%s %s — exiting for launcher rebuild", r.cfg.AgentID, reason)
	if r.cfg.OnFatal != nil {
		r.cfg.OnFatal(reason)
	}
}

func isSessionClosedInjectError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrSessionClosed) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, ErrSessionClosed.Error()) ||
		strings.Contains(msg, "agentsupervisor: supervisor closed") ||
		strings.Contains(msg, "agentsupervisor: client closed") ||
		strings.Contains(msg, "supervisor_session: client closed")
}

// recordWake records messageID in the shared wake-dedup set (FIFO eviction). Unlike
// the old controller method it never lazily creates a managedAgent — the runtime
// always has its state.
func (r *LocalRuntime) recordWake(messageID string) {
	if messageID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state.WakeSeen == nil {
		r.state.WakeSeen = make(map[string]struct{}, WakeDedupCap)
	}
	if _, ok := r.state.WakeSeen[messageID]; ok {
		return
	}
	r.state.WakeSeen[messageID] = struct{}{}
	r.state.WakeOrder = append(r.state.WakeOrder, messageID)
	for len(r.state.WakeOrder) > WakeDedupCap {
		oldest := r.state.WakeOrder[0]
		r.state.WakeOrder = r.state.WakeOrder[1:]
		delete(r.state.WakeSeen, oldest)
	}
}

// ---------------------------------------------------------------------------
// Session lifecycle.
// ---------------------------------------------------------------------------

// Start brings up the supervisor (or codex) session, wiring OnEvent→r.onEvent /
// OnExit→r.onExit. The daemon has already reserved the managedAgent (with this
// runtime + its fresh SessionState) so the reader-goroutine callbacks find their
// state. On failure the daemon rolls back the reservation.
func (r *LocalRuntime) Start(ctx context.Context, spec StartSpec) error {
	agentID := spec.AgentID
	// Idempotency guard (T860 fold-in): the supervisor session now has TWO possible
	// triggers — the autonomous boot self-start (from local ResumeState) and a later
	// control reconcile command. They MUST converge on ONE session. If a session is
	// already live, never start a second: a stale/duplicate trigger (Version ≤ the
	// running session's) is dropped; a strictly-newer reconcile keeps the live session
	// (no mid-session hot-swap in this scope) but records the version so a subsequent
	// relaunch resolves from it. This is the no-double-start / no-split-brain guard —
	// check-and-set under a single lock so the boot-start and a racing reconcile can't
	// both pass. (The boot self-start runs before the control server serves, so in
	// practice they are ordered; the guard hardens the general case.)
	r.mu.Lock()
	if r.state.Session != nil {
		cur := r.state.Version
		if spec.Version <= cur {
			r.mu.Unlock()
			r.log("start agent=%s: session already running (incoming v%d, current v%d) — no second start", agentID, spec.Version, cur)
			return nil
		}
		if !r.hasRecordedStartSpecLocked() || !r.startSpecRequiresSessionRestartLocked(spec) {
			r.recordStartSpecLocked(spec)
			r.mu.Unlock()
			r.log("start agent=%s: session already running (incoming v%d, current v%d) — no second start", agentID, spec.Version, cur)
			return nil
		}
		sess := r.state.Session
		r.state.ExpectedStop = true
		r.mu.Unlock()

		r.log("start agent=%s: session config changed (incoming v%d, current v%d) — restarting session", agentID, spec.Version, cur)
		if err := sess.Stop(ctx); err != nil {
			return fmt.Errorf("agent_controller: restart session: stop current session: %w", err)
		}
		if home, _, _, pathErr := r.agentPaths(agentID); pathErr == nil {
			if relErr := sessioninstance.ReleaseInstance(home); relErr != nil {
				r.log("start agent=%s restart release instance: %v", agentID, relErr)
			}
		}

		r.mu.Lock()
		if r.state.Session == sess {
			r.state.Session = nil
		}
		r.resetSessionStartStateLocked()
		r.recordStartSpecLocked(spec)
		r.mu.Unlock()
	} else {
		r.resetSessionStartStateLocked()
		r.recordStartSpecLocked(spec)
		r.mu.Unlock()
	}

	home, tasksDir, _, err := r.agentPaths(agentID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(tasksDir, 0o700); err != nil {
		return fmt.Errorf("agent_controller: mkdir tasks: %w", err)
	}
	if spec.CLI == CLICodex {
		return r.startCodex(ctx, spec, home, tasksDir)
	}
	if err := os.MkdirAll(filepath.Join(tasksDir, ".claude"), 0o700); err != nil {
		return fmt.Errorf("agent_controller: mkdir tasks/.claude: %w", err)
	}

	epochState, err := supervisormanager.ReadEpoch(home)
	if err != nil {
		return fmt.Errorf("agent_controller: read epoch agent=%s: %w", agentID, err)
	}
	plannedGeneration := epochState.Generation
	if spec.ForkResume {
		plannedGeneration++
	}

	mcpBytes, err := mcphost.GenerateMCPConfig(mcphost.MCPConfigParams{
		ServerName:        MCPServerName,
		Command:           r.cfg.BinaryPath,
		Args:              []string{"worker", "mcp-host"},
		AgentID:           agentID,
		AdminURL:          r.cfg.AdminURL,
		WorkerToken:       r.cfg.WorkerToken,
		ServerFingerprint: r.cfg.ServerFingerprint,
		AgentRoot:         tasksDir,
		Generation:        plannedGeneration,
	})
	if err != nil {
		return fmt.Errorf("agent_controller: generate mcp-config: %w", err)
	}
	mcpPath, err := WriteMCPConfig(home, mcpBytes)
	if err != nil {
		return fmt.Errorf("agent_controller: write mcp-config: %w", err)
	}
	if err := r.requireSupervisorMCP(ctx, agentID, tasksDir, true, []string{"post_message", "list_my_tasks", "search_tools"}); err != nil {
		return err
	}

	generation := epochState.Generation
	resumeFrom := ""
	if spec.ForkResume {
		if spec.Resume {
			resumeFrom = claudestream.SessionUUIDGen(agentID, epochState.Epoch, epochState.Generation)
		}
		bumped, berr := supervisormanager.BumpGenerationForRelaunch(home)
		if berr != nil {
			return fmt.Errorf("agent_controller: bump generation agent=%s: %w", agentID, berr)
		}
		generation = bumped.Generation
	}

	sess, err := r.cfg.Starter(ctx, SupervisorSessionConfig{
		AgentID:             agentID,
		HomeDir:             home,
		MCPConfigPath:       mcpPath,
		TasksDir:            tasksDir,
		BinaryPath:          r.cfg.BinaryPath,
		ClaudeBin:           r.cfg.ClaudeBinary,
		Model:               spec.Model,
		DisplayName:         spec.DisplayName,
		AgentEnv:            spec.EnvVars,
		PromptDescription:   spec.PromptDescription,
		Epoch:               epochState.Epoch,
		Generation:          generation,
		ResumeFromSessionID: resumeFrom,
		ConcurrencyEnabled:  spec.ConcurrencyEnabled,
		StopGrace:           r.cfg.StopGrace,
		Logger:              r.rawLogger(),
		OnEvent:             func(ev claudestream.StreamEvent) { r.onEvent(ev) },
		OnExit:              func(exitErr error) { r.onExit(exitErr) },
	})
	if err != nil {
		return fmt.Errorf("agent_controller: start session: %w", err)
	}

	r.mu.Lock()
	r.state.Session = sess
	r.mu.Unlock()

	sessionID := claudestream.SessionUUIDGen(agentID, epochState.Epoch, generation)
	if _, lerr := sessioninstance.AcquireInstance(home, sessionID, os.Getpid()); lerr != nil {
		r.log("started agent=%s: write session.instance: %v (non-fatal)", agentID, lerr)
	}

	r.log("started agent=%s version=%d epoch=%d generation=%d fork=%v resume=%v home=%s", agentID, spec.Version, epochState.Epoch, generation, spec.ForkResume, spec.Resume, home)
	r.reportControlLoaded(agentID, spec, controlLoadedInfo{
		Session:   "claude",
		Home:      home,
		TasksDir:  tasksDir,
		MCP:       "preflight_ok",
		Memory:    "progressive",
		Executor:  executorStatus(spec.ConcurrencyEnabled),
		Resume:    resumeFrom != "",
		SessionID: sessionID,
	})
	// issue-4a45e9cc: BOOT installed-skill report (best-effort, off the start path so a
	// slow disk scan / center never blocks session start). force=true bypasses the scan
	// rate-limit so the panel populates on first online.
	r.kickInstalledSkillsReport()
	return nil
}

func (r *LocalRuntime) hasRecordedStartSpecLocked() bool {
	return r.state.Version != 0 ||
		r.state.CLI != "" ||
		r.state.Model != "" ||
		r.state.DisplayName != "" ||
		r.state.PromptDescription != "" ||
		r.state.EnvVars != nil
}

func (r *LocalRuntime) startSpecRequiresSessionRestartLocked(spec StartSpec) bool {
	return r.state.CLI != spec.CLI ||
		r.state.Model != spec.Model ||
		r.state.DisplayName != spec.DisplayName ||
		r.state.PromptDescription != spec.PromptDescription ||
		!maps.Equal(r.state.EnvVars, spec.EnvVars) ||
		r.state.ConcurrencyEnabled != spec.ConcurrencyEnabled
}

func (r *LocalRuntime) recordStartSpecLocked(spec StartSpec) {
	r.state.Version = spec.Version
	r.state.Model = spec.Model
	r.state.DisplayName = spec.DisplayName
	r.state.PromptDescription = spec.PromptDescription
	r.state.EnvVars = cloneEnv(spec.EnvVars)
	r.state.CLI = spec.CLI
	r.state.ConcurrencyEnabled = spec.ConcurrencyEnabled
}

func (r *LocalRuntime) resetSessionStartStateLocked() {
	r.state.ExpectedStop = false
	r.state.Detaching = false
	r.state.LifecycleOnce = sync.Once{}
	r.state.WakeSeen = nil
	r.state.WakeOrder = nil
	r.state.HadWork = false
	r.state.CurrentTaskID = ""
	r.state.CurrentConversationID = ""
	r.state.ToolNames = nil
	r.state.EventTaskID = ""
	r.state.LastEventTaskID = ""
	r.state.TaskLog = nil
	r.state.TaskLogID = ""
	r.state.EventSeq = 0
	r.state.CodexCleanTurns = 0
	r.state.RLRetryAfterSecs = 0
	r.state.RLResetAtUnix = 0
	r.state.RateLimitResumeAt = time.Time{}
	r.state.APIErrorRetries = 0
	r.state.SawIncompleteTurn = false
	r.state.SawCodexPoisoningTransport = false
	r.state.SawCodexRegistryMissing = false
}

// startCodex starts a cli=codex session via the neutral CodexSpec (the daemon
// adapter fills Launcher + merged env).
func (r *LocalRuntime) startCodex(ctx context.Context, spec StartSpec, home, tasksDir string) error {
	agentID := spec.AgentID
	// T977 fix #3: read the PRIOR generation's cli BEFORE overwriting the marker. A
	// cli-switch (e.g. claude→codex) leaves a session_id from the OTHER cli in
	// session.instance; feeding a claude session id to `codex exec resume` yields a
	// "no rollout" error, so we only resume when the prior generation was ALSO codex.
	priorCLI := ReadAgentCLIMarker(home)
	if err := WriteAgentCLIMarker(home, CLICodex); err != nil {
		return fmt.Errorf("agent_controller: write codex cli marker: %w", err)
	}
	prior, _ := sessioninstance.ReadInstance(home)
	plannedGeneration := prior.Generation + 1

	// codex supervisor MCP (T972): generate the SAME canonical mcp_config.runtime.json
	// the claude supervisor gets (agent-center host binary + per-agent AC_MCP_* creds),
	// then translate it into $CODEX_HOME/config.toml so the codex supervisor reaches the
	// same center tools (create_task/complete_task/post_message) via config.toml instead
	// of claude's --mcp-config. CODEX_HOME is exported to the codex process (below).
	mcpBytes, err := mcphost.GenerateMCPConfig(mcphost.MCPConfigParams{
		ServerName:         MCPServerName,
		Command:            r.cfg.BinaryPath,
		Args:               []string{"worker", "mcp-host"},
		AgentID:            agentID,
		AdminURL:           r.cfg.AdminURL,
		WorkerToken:        r.cfg.WorkerToken,
		ServerFingerprint:  r.cfg.ServerFingerprint,
		AgentRoot:          tasksDir,
		Generation:         plannedGeneration,
		DisableToolTiering: true,
	})
	if err != nil {
		return fmt.Errorf("agent_controller: generate codex mcp-config: %w", err)
	}
	r.reportCodexMCPDiagnostic(agentID, "mcp_config_generated", map[string]any{
		"summary": "codex mcp config generated from canonical runtime config",
		"config":  summarizeRuntimeMCPConfig(mcpBytes),
	})
	codexHome, err := WriteCodexMCPConfig(home, mcpBytes)
	if err != nil {
		return fmt.Errorf("agent_controller: write codex mcp-config: %w", err)
	}
	r.reportCodexMCPDiagnostic(agentID, "codex_config_written", map[string]any{
		"summary":            "codex config.toml written under per-agent CODEX_HOME",
		"codex_home":         codexHome,
		"config_path":        filepath.Join(codexHome, codexConfigFileName),
		"config_file_status": fileStatus(filepath.Join(codexHome, codexConfigFileName)),
	})
	// T977 fix #1: provision the codex login auth.json into the per-agent CODEX_HOME.
	// codex reads auth from $CODEX_HOME; the dedicated per-agent home has the generated
	// config.toml but NOT the login auth (which lives in the worker's real CODEX_HOME /
	// ~/.codex), so without this codex 401s and the ENTIRE MCP chain is unreachable
	// (tester3 T977 — the config-source-reaches-process blind spot). Fail-loud (loud
	// warn, the executor codexAuthPreflight discipline) if it can't be provisioned —
	// never a silent 401.
	authStatus := "provisioned"
	if w := provisionCodexAuth(codexHome, resolveSourceCodexHome()); w != "" {
		authStatus = "warning"
		r.log("codex agent=%s: WARNING codex supervisor auth NOT provisioned into %s — codex will FAIL auth (401) and MCP will be UNREACHABLE; %s", agentID, codexHome, w)
	}
	r.reportCodexMCPDiagnostic(agentID, "codex_auth_preflight", map[string]any{
		"summary":     "codex auth.json link checked for per-agent CODEX_HOME",
		"codex_home":  codexHome,
		"auth_status": authStatus,
		"auth_file":   fileStatus(filepath.Join(codexHome, codexAuthFileName)),
	})
	if err := r.requireSupervisorMCP(ctx, agentID, tasksDir, false, []string{
		"post_message",
		"list_my_tasks",
		"get_my_profile",
		"get_plan",
		"list_task_executions",
	}); err != nil {
		return err
	}
	extraSystemPrompt := r.codexExtraSystemPrompt(ctx, home, spec.PromptDescription)

	// Codex resume is health-gated. A captured thread_id is only safe to seed when
	// the caller explicitly requested a resume AND the prior generation proved it
	// completed a clean turn. Otherwise a poisoned but locally-existing rollout can be
	// resumed forever, leaving new messages delivered/seen while Codex is stuck inside
	// an old incomplete turn.
	resumeThreadID := ""
	if spec.Resume && prior.CompletedTurn {
		resumeThreadID = prior.SessionID
	}
	// T977 fix #3: never resume across a cli-switch — a session_id from a non-codex prior
	// generation is a claude session id, not a codex thread_id (`codex exec resume` →
	// "no rollout"). Discard it + start fresh, logged.
	if resumeThreadID != "" && priorCLI != "" && priorCLI != CLICodex {
		r.log("codex agent=%s: prior generation was cli=%q (not codex) — discarding its stale session_id, starting a FRESH codex session (no cross-cli resume)", agentID, priorCLI)
		resumeThreadID = ""
	}
	if prior.Generation > 0 && resumeThreadID == "" {
		switch {
		case prior.SessionID != "" && !spec.Resume:
			r.log("codex agent=%s: resume not requested — discarding prior thread_id and starting a FRESH codex session", agentID)
		case prior.SessionID != "" && !prior.CompletedTurn:
			r.log("codex agent=%s: prior generation did not complete a clean turn — discarding prior thread_id and starting a FRESH codex session", agentID)
		default:
			r.log("codex agent=%s: prior generation left no thread_id (thread.started never captured) — starting a FRESH codex session, prior conversation NOT resumed", agentID)
		}
	}
	if _, lerr := sessioninstance.AcquireInstance(home, resumeThreadID, os.Getpid()); lerr != nil {
		return fmt.Errorf("agent_controller: acquire codex instance: %w", lerr)
	}
	r.reportCodexMCPDiagnostic(agentID, "codex_session_start", map[string]any{
		"summary":                 "starting logical codex session after config/auth/preflight",
		"resume_requested":        spec.Resume,
		"resume_thread_present":   resumeThreadID != "",
		"prior_generation":        prior.Generation,
		"prior_completed_turn":    prior.CompletedTurn,
		"prior_thread_present":    prior.SessionID != "",
		"prior_cli":               priorCLI,
		"extra_system_prompt_len": len(extraSystemPrompt),
		"codex_home":              codexHome,
	})

	sess, err := r.cfg.CodexStarter(ctx, CodexSpec{
		AgentID:           agentID,
		TasksDir:          tasksDir,
		Binary:            r.cfg.CodexBinary,
		Model:             spec.Model,
		DisplayName:       spec.DisplayName,
		EnvVars:           spec.EnvVars,
		CodexHome:         codexHome,
		ExtraSystemPrompt: extraSystemPrompt,
		ResumeThreadID:    resumeThreadID,
		OnThreadID: func(tid string) {
			if merr := sessioninstance.MarkSessionID(home, tid); merr != nil {
				r.log("codex agent=%s: persist thread_id failed: %v (resume unavailable on next restart)", agentID, merr)
			}
		},
		OnStaleThreadID: func(tid string) {
			if merr := sessioninstance.ClearSessionID(home); merr != nil {
				r.log("codex agent=%s: clear stale thread_id=%s failed: %v", agentID, tid, merr)
			}
		},
		Logger:  r.rawLogger(),
		OnEvent: func(ev claudestream.StreamEvent) { r.onEvent(ev) },
		OnExit:  func(exitErr error) { r.onExit(exitErr) },
	})
	if err != nil {
		return fmt.Errorf("agent_controller: start codex session: %w", err)
	}
	r.mu.Lock()
	r.state.Session = sess
	r.mu.Unlock()
	r.log("started codex agent=%s version=%d home=%s", agentID, spec.Version, home)
	r.reportControlLoaded(agentID, spec, controlLoadedInfo{
		Session:   "codex",
		Home:      home,
		TasksDir:  tasksDir,
		MCP:       "preflight_ok",
		CodexHome: codexHome,
		CodexAuth: authStatus,
		Memory:    "progressive",
		Executor:  executorStatus(spec.ConcurrencyEnabled),
		Resume:    resumeThreadID != "",
		SessionID: resumeThreadID,
	})
	// issue-4a45e9cc: BOOT installed-skill report (best-effort, off the start path).
	r.kickInstalledSkillsReport()
	return nil
}

type controlLoadedInfo struct {
	Session   string
	Home      string
	TasksDir  string
	MCP       string
	CodexHome string
	CodexAuth string
	Memory    string
	Executor  string
	Resume    bool
	SessionID string
}

func executorStatus(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func (r *LocalRuntime) reportControlLoaded(agentID string, spec StartSpec, info controlLoadedInfo) {
	if r.cfg.Reporter == nil {
		return
	}
	components := []map[string]any{
		{"name": "session", "status": info.Session},
		{"name": "mcp_agent_center", "status": info.MCP},
		{"name": "memory", "status": info.Memory},
		{"name": "executor_pool", "status": info.Executor},
	}
	if info.Session == CLICodex {
		components = append(components,
			map[string]any{"name": "codex_home", "status": pathStatus(info.CodexHome)},
			map[string]any{"name": "codex_auth", "status": info.CodexAuth},
			map[string]any{"name": "codex_transport", "status": "https_forced"},
		)
	}
	p := map[string]any{
		"event":               "control_loaded",
		"summary":             controlLoadedSummary(spec, info),
		"cli":                 spec.CLI,
		"model":               spec.Model,
		"version":             spec.Version,
		"components":          components,
		"home":                pathStatus(info.Home),
		"tasks_dir":           pathStatus(info.TasksDir),
		"resume":              info.Resume,
		"session_id_present":  strings.TrimSpace(info.SessionID) != "",
		"env_overrides_count": len(spec.EnvVars),
	}
	b, err := json.Marshal(p)
	if err != nil {
		return
	}
	if err := r.cfg.Reporter.ReportAgentActivity(
		context.Background(), agentID, "lifecycle", string(b), "", "", time.Now(),
	); err != nil {
		r.log("agent=%s control_loaded report: %v", agentID, err)
	}
}

func controlLoadedSummary(spec StartSpec, info controlLoadedInfo) string {
	transport := ""
	if info.Session == CLICodex {
		transport = " transport=https_forced auth=" + info.CodexAuth
	}
	return fmt.Sprintf("control loaded: cli=%s model=%s session=%s mcp=%s memory=%s executor_pool=%s resume=%t%s",
		spec.CLI, spec.Model, info.Session, info.MCP, info.Memory, info.Executor, info.Resume, transport)
}

func pathStatus(path string) string {
	if strings.TrimSpace(path) == "" {
		return "missing"
	}
	return "configured"
}

func (r *LocalRuntime) requireSupervisorMCP(ctx context.Context, agentID, tasksDir string, tierTools bool, required []string) error {
	r.reportCodexMCPDiagnostic(agentID, "mcp_preflight_start", map[string]any{
		"summary":            "checking worker mcp-host exposes required supervisor tools before codex starts",
		"required_tools":     required,
		"tasks_dir":          tasksDir,
		"tasks_dir_status":   pathStatus(tasksDir),
		"tier_tools_enabled": tierTools,
	})
	preflight := r.cfg.MCPPreflight
	if preflight == nil {
		preflight = mcphost.RequireTools
	}
	cfg := mcphost.Config{
		AgentID:   agentID,
		AgentRoot: tasksDir,
		TierTools: tierTools,
	}
	if err := preflight(ctx, cfg, required...); err != nil {
		r.reportCodexMCPDiagnostic(agentID, "mcp_preflight_failed", map[string]any{
			"summary":        "worker mcp-host preflight failed before codex session start",
			"required_tools": required,
			"error":          err.Error(),
		})
		return fmt.Errorf("agent_controller: supervisor MCP preflight failed for server %q: %w", MCPServerName, err)
	}
	r.reportCodexMCPDiagnostic(agentID, "mcp_preflight_ok", map[string]any{
		"summary":        "worker mcp-host preflight succeeded before codex session start",
		"required_tools": required,
		"server":         MCPServerName,
	})
	return nil
}

func (r *LocalRuntime) reportCodexMCPDiagnostic(agentID, phase string, fields map[string]any) {
	if r.cfg.Reporter == nil {
		return
	}
	if fields == nil {
		fields = map[string]any{}
	}
	fields["type"] = "codex_mcp_diagnostic"
	fields["phase"] = phase
	payload, err := json.Marshal(fields)
	if err != nil {
		r.log("codex agent=%s mcp diagnostic %s marshal: %v", agentID, phase, err)
		return
	}
	if err := r.cfg.Reporter.ReportAgentActivity(
		context.Background(), agentID, "codex_mcp_diagnostic", string(payload), "", "", time.Now(),
	); err != nil {
		r.log("codex agent=%s mcp diagnostic %s report: %v", agentID, phase, err)
	}
}

func summarizeRuntimeMCPConfig(raw []byte) map[string]any {
	var cfg mcphost.MCPConfig
	out := map[string]any{
		"bytes": len(raw),
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		out["parse_error"] = err.Error()
		return out
	}
	names := make([]string, 0, len(cfg.MCPServers))
	for name := range cfg.MCPServers {
		names = append(names, name)
	}
	sort.Strings(names)
	out["server_count"] = len(names)
	out["servers"] = names
	if server, ok := cfg.MCPServers[MCPServerName]; ok {
		out["agent_center"] = map[string]any{
			"command":                server.Command,
			"args":                   server.Args,
			"env_count":              len(server.Env),
			"has_agent_id":           strings.TrimSpace(server.Env["AC_MCP_AGENT_ID"]) != "",
			"has_admin_url":          strings.TrimSpace(server.Env["AC_MCP_ADMIN_URL"]) != "",
			"has_worker_token":       strings.TrimSpace(server.Env["AC_MCP_WORKER_TOKEN"]) != "",
			"has_server_fingerprint": strings.TrimSpace(server.Env["AC_MCP_SERVER_FINGERPRINT"]) != "",
		}
	}
	return out
}

func fileStatus(path string) map[string]any {
	info, err := os.Stat(path)
	if err != nil {
		return map[string]any{"exists": false, "error": err.Error()}
	}
	return map[string]any{"exists": true, "size": info.Size(), "mode": info.Mode().Perm().String()}
}

func (r *LocalRuntime) codexExtraSystemPrompt(ctx context.Context, home, promptDescription string) string {
	memEngine := memory.NewEngine(filepath.Join(home, "memory"), "")
	var memoryContext string
	if initErr := memEngine.EnsureRootInit(ctx); initErr != nil {
		r.log("codex agent=%s: memory init: %v (continuing without memory)", r.cfg.AgentID, initErr)
	} else if mc, stats, ctxErr := memEngine.HarnessContextWithOptions(ctx, memory.HarnessDisclosureOptionsFromEnv()); ctxErr != nil {
		r.log("codex agent=%s: memory load: %v (continuing without memory)", r.cfg.AgentID, ctxErr)
	} else {
		memoryContext = mc
		r.log("codex agent=%s: memory harness included=%d truncated=%d omitted=%d body_bytes=%d omitted_manifest_bytes=%d omitted_clipped=%t budget_bytes=%d per_file_bytes=%d",
			r.cfg.AgentID,
			stats.IncludedFiles,
			stats.TruncatedFiles,
			stats.OmittedFiles,
			stats.BodyBytes,
			stats.OmittedManifestBytes,
			stats.OmittedManifestClipped,
			stats.MemoryBudgetBytes,
			stats.PerFileBytes,
		)
	}
	return claudestream.ComposeCodexExtraSystemPrompt(promptDescription, memoryContext)
}

// rawLogger adapts the prefixed Log back to the func(string) the session configs
// want (they add their own context). It forwards the message verbatim.
func (r *LocalRuntime) rawLogger() func(msg string) {
	return func(msg string) { r.log("%s", msg) }
}

// Attach installs a re-attached session into the state (boot reattach). The daemon
// builds the *SupervisorSession via ReattachSupervisorSession wiring
// OnEventCallback/OnExitCallback, then hands it here.
func (r *LocalRuntime) Attach(sess Session) {
	r.mu.Lock()
	r.state.Session = sess
	r.mu.Unlock()
}

// OnEventCallback / OnExitCallback expose the reader-goroutine callbacks so the
// daemon's reattach path wires the SAME runtime handlers as Start.
func (r *LocalRuntime) OnEventCallback() func(ev claudestream.StreamEvent) {
	return func(ev claudestream.StreamEvent) { r.onEvent(ev) }
}
func (r *LocalRuntime) OnExitCallback() func(err error) {
	return func(err error) { r.onExit(err) }
}

// Stop terminates the live session (expected stop) without reporting lifecycle.
func (r *LocalRuntime) Stop(ctx context.Context) error {
	return r.stop(ctx, false)
}

// StopReporting is Stop with reportLifecycle=true (settles "stopped" once).
func (r *LocalRuntime) StopReporting(ctx context.Context) error {
	return r.stop(ctx, true)
}

func (r *LocalRuntime) stop(ctx context.Context, reportLifecycle bool) error {
	agentID := r.cfg.AgentID
	r.closeRuntimeAdmission()
	r.lifecycleCancel()

	r.mu.Lock()
	sess := r.state.Session
	if sess == nil {
		r.mu.Unlock()
		if reportLifecycle {
			r.reportLifecycleOnce(ctx, "stopped", "")
		}
		r.fuseLiveExecutors(ctx)
		return r.waitOwnedBackground(ctx)
	}
	r.state.ExpectedStop = true
	r.mu.Unlock()

	var retErr error
	if err := sess.Stop(ctx); err != nil {
		r.log("stop agent=%s: %v", agentID, err)
		retErr = err
	}
	r.fuseLiveExecutors(ctx)

	if home, _, _, pathErr := r.agentPaths(agentID); pathErr == nil {
		if relErr := sessioninstance.ReleaseInstance(home); relErr != nil {
			r.log("stop agent=%s release instance: %v", agentID, relErr)
		}
	}

	if reportLifecycle {
		r.reportLifecycleOnce(ctx, "stopped", "")
	}
	if err := r.waitOwnedBackground(ctx); err != nil && retErr == nil {
		retErr = err
	}
	return retErr
}

// IsRunning reports whether the session is live.
func (r *LocalRuntime) IsRunning() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.state.Session != nil
}

// Detach detaches the live session (daemon-shutdown survival). Sets Detaching so
// onExit recognises the nil exit as a survival detach, not a crash.
func (r *LocalRuntime) Detach() {
	r.mu.Lock()
	sess := r.state.Session
	if sess != nil {
		r.state.Detaching = true
	}
	r.mu.Unlock()
	if sess != nil {
		sess.Detach()
	}
}

// Tick performs per-agent live-session maintenance (rate-limit / api-error resume
// drain). Self-heal relaunch of DEAD agents is driven by the daemon (their runtime
// is gone), so it is NOT here.
func (r *LocalRuntime) Tick(ctx context.Context, now time.Time) error {
	r.drainResume(ctx, now)
	// T860 piece ③: renew every in-flight task's execution lease + GC this agent's
	// task-dir residue — the work the daemon's AgentController.OnTick used to do, now
	// self-contained in the agent-runtime process. Both internally rate-limited.
	r.drainLeaseRenewals(ctx, now)
	r.maybeRunGC(now)
	// issue-4a45e9cc: HEARTBEAT installed-skill re-report — rate-limited scan, POSTs only
	// when the fingerprint changed since the last report ("变了才重报").
	r.reportInstalledSkillsIfChanged(ctx, now, false)
	// issue-68ccb310 (option b): low-frequency heartbeat reconcile — re-drive any executor
	// judgment the supervisor dropped (or lost across a crash/restart) so no finished
	// executor strands its task. STRICT: never writes task status from Go. Rate-limited.
	r.reconcilePendingJudgments(ctx, now)
	return nil
}

// reportLifecycleOnce emits a lifecycle RESULT exactly once per instance.
func (r *LocalRuntime) reportLifecycleOnce(ctx context.Context, state, errMsg string) {
	emit := func() {
		if err := r.cfg.Reporter.ReportAgentLifecycle(ctx, r.cfg.AgentID, state, errMsg, time.Now()); err != nil {
			r.log("agent=%s report %s: %v", r.cfg.AgentID, state, err)
		}
	}
	r.state.LifecycleOnce.Do(emit)
}

// reportRecovered clears a lingering center `error` → running after a recovery.
func (r *LocalRuntime) reportRecovered() {
	if err := r.cfg.Reporter.ReportAgentLifecycle(context.Background(), r.cfg.AgentID, "running", "", time.Now()); err != nil {
		r.log("agent=%s report running (recovery): %v", r.cfg.AgentID, err)
	}
}

// ReportRecovered is the daemon-facing entry (boot reattach/relaunch recovery).
func (r *LocalRuntime) ReportRecovered() { r.reportRecovered() }

// ReportLifecycleOnce is the daemon-facing entry for the reconcile/reset settle
// (routes through this instance's sync.Once).
func (r *LocalRuntime) ReportLifecycleOnce(ctx context.Context, state, errMsg string) {
	r.reportLifecycleOnce(ctx, state, errMsg)
}

// ResumeNudgeText exposes the resume nudge for the daemon boot-relaunch path.
func (r *LocalRuntime) ResumeNudgeText() string { return r.resumeNudgeText() }

// NotifyWorkAvailable nudges the resident supervisor that a runnable task exists.
// It deliberately does NOT fork: the supervisor owns the dispatch decision and
// calls fork_executor only for tasks it wants isolated in an executor.
func (r *LocalRuntime) NotifyWorkAvailable(ctx context.Context, taskID string) error {
	if !r.beginRuntimeWork() {
		return ErrRuntimeStopping
	}
	defer r.endRuntimeWork()
	taskID = strings.TrimSpace(taskID)
	if taskID == "" {
		r.log("work_available agent=%s: empty task_id — skipping supervisor nudge", r.cfg.AgentID)
		return nil
	}
	brief := workAvailableBrief(taskID)
	if err := r.injectSession(ctx, brief); err != nil {
		return fmt.Errorf("agent_controller: work_available inject agent=%s task=%s: %w", r.cfg.AgentID, taskID, err)
	}
	r.mu.Lock()
	r.state.HadWork = true
	r.mu.Unlock()
	r.log("SUPERVISOR-WAKE route=NotifyWorkAvailable(agent.work_available) agent_namespace=%s task_id=%s — injected supervisor dispatch nudge; executor fork requires explicit fork_executor",
		r.cfg.AgentID, taskID)
	return nil
}

func workAvailableBrief(taskID string) string {
	return fmt.Sprintf("[work_available] Task %s is assigned to you.\n\nDecide in this supervisor session: inspect it with get_task/list_my_tasks, handle it inline when it is a supervisor/control task, or call fork_executor(task_id=%q) when it should run in an isolated executor. Do not complete the task until you have judged the result.", taskID, taskID)
}

// cloneEnv duplicates an env overlay (nil-safe).
func cloneEnv(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// agentEventTypeMessageDelivered mirrors agent.EventTypeMessageDelivered (kept local
// so agentruntime does not import the agent BC just for one string constant).
const agentEventTypeMessageDelivered = "message_delivered"
