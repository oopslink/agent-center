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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/reporepo"
	"github.com/oopslink/agent-center/internal/agentruntime/sessioninstance"
	"github.com/oopslink/agent-center/internal/agentruntime/skillscan"
	"github.com/oopslink/agent-center/internal/agentruntime/taskexec"
	"github.com/oopslink/agent-center/internal/claudestream"
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

	TaskDirManager  *taskexec.DirManager
	SegmentMaxBytes int64
	TaskLogMaxBytes int64
	EventWriter     *taskexec.EventStreamWriter

	// Materializer is the repo-workspace port (AC_EXECUTOR_GIT_WORKTREE). nil ⇒ the
	// flag is OFF: SpawnExecutor prepares NO worktree and the pool/monitor/recovery
	// behave byte-for-byte as before (the zero-regression contract). Non-nil (a
	// reporepo.LocalGitMaterializer) ⇒ SpawnExecutor materializes a canonical source +
	// a per-executor worktree BEFORE start_task, and the Monitor tears it down on
	// finalize/recovery via a WorktreeCleaner adapter.
	Materializer reporepo.RepoMaterializer
	// ReposRoot is the canonical <agent_home>/repos root the Materializer is anchored
	// at (informational; the Materializer already carries it). Empty when the flag is off.
	ReposRoot string

	// SkillLayerRoots resolves the four claude-code skill-layer directories to scan for
	// the OBSERVED installed-skill report (issue-4a45e9cc). It is injected so tests can
	// point at temp dirs; nil ⇒ the runtime's default resolver derived from the agent
	// home (home/skills + tasks/.claude/skills = project) and $HOME/.claude (user +
	// plugins). home is the agent home dir, tasksDir is its project cwd.
	SkillLayerRoots func(home, tasksDir string) skillscan.LayerRoots
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

	// exec is the per-agent concurrent-execution wiring (Phase 0c), installed via
	// AttachExecutor when the agent opts into concurrency. nil ⇒ single-claude inject
	// path. Guarded by r.mu exactly as ma.exec was guarded by c.mu.
	exec *ExecutorEngine

	// forkMu (red line #1) serializes the get_task→start_task→launch fork sequence
	// (SpawnExecutor) AND the shared launch tail (launchExecutor) so two concurrent
	// forks for one agent cannot both pass the pool cap. SEPARATE from r.mu (the
	// SessionState lock) — a different concern; never guards the same field.
	forkMu sync.Mutex

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

// StateMu is the per-agent SessionState lock the daemon uses during the transitional
// decoupling (T839 §4.1 去共享状态). The daemon snapshots the *managedAgent out from
// under c.mu, then locks this to read/write that agent's ma.state.* fields — the same
// fields the runtime's own critical sections guard.
func (r *LocalRuntime) StateMu() *sync.Mutex { return &r.mu }

// WaitBG blocks until this agent's best-effort clean-turn goroutines have drained
// (Shutdown calls it per-runtime, replacing the old shared c.bg.Wait()).
func (r *LocalRuntime) WaitBG() { r.bg.Wait() }

var _ Runtime = (*LocalRuntime)(nil)

// NewLocalRuntime builds a LocalRuntime over the shared state pointer.
func NewLocalRuntime(cfg LocalRuntimeConfig, state *SessionState) *LocalRuntime {
	r := &LocalRuntime{cfg: cfg, state: state}
	// option b (issue-68ccb310): load the durable pending-judgment store from the agent
	// home so a relaunch re-drives dropped judgments (boot recovery). nil when the home
	// isn't resolvable (single-claude/test path) ⇒ the reconcile is disabled.
	if strings.TrimSpace(cfg.AgentHomeBase) != "" && strings.TrimSpace(cfg.AgentID) != "" {
		r.pending = newPendingStore(filepath.Join(cfg.AgentHomeBase, "agents", cfg.AgentID, "pending_judgments.json"))
	}
	return r
}

// State returns the shared SessionState (the daemon's managedAgent points at the
// SAME instance).
func (r *LocalRuntime) State() *SessionState { return r.state }

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
	return sess.Inject(ctx, text)
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
	if ee != nil {
		r.createTaskDir(agentID, req.TaskID)
		return r.workViaExecutor(ctx, req, ee)
	}

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
		return fmt.Errorf("agent_controller: converse inject agent=%s: %w", agentID, err)
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
		if spec.Version > cur {
			r.state.Version = spec.Version
		}
		r.mu.Unlock()
		r.log("start agent=%s: session already running (incoming v%d, current v%d) — no second start", agentID, spec.Version, cur)
		return nil
	}
	// Session-scoped config carried across a crash (self-heal gets no fresh reconcile).
	r.state.Version = spec.Version
	r.state.Model = spec.Model
	r.state.DisplayName = spec.DisplayName
	r.state.PromptDescription = spec.PromptDescription
	r.state.EnvVars = cloneEnv(spec.EnvVars)
	r.state.CLI = spec.CLI
	r.mu.Unlock()

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

	mcpBytes, err := mcphost.GenerateMCPConfig(mcphost.MCPConfigParams{
		ServerName:        MCPServerName,
		Command:           r.cfg.BinaryPath,
		Args:              []string{"worker", "mcp-host"},
		AgentID:           agentID,
		AdminURL:          r.cfg.AdminURL,
		WorkerToken:       r.cfg.WorkerToken,
		ServerFingerprint: r.cfg.ServerFingerprint,
		AgentRoot:         tasksDir,
	})
	if err != nil {
		return fmt.Errorf("agent_controller: generate mcp-config: %w", err)
	}
	mcpPath, err := WriteMCPConfig(home, mcpBytes)
	if err != nil {
		return fmt.Errorf("agent_controller: write mcp-config: %w", err)
	}

	epochState, err := supervisormanager.ReadEpoch(home)
	if err != nil {
		return fmt.Errorf("agent_controller: read epoch agent=%s: %w", agentID, err)
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
	// issue-4a45e9cc: BOOT installed-skill report (best-effort, off the start path so a
	// slow disk scan / center never blocks session start). force=true bypasses the scan
	// rate-limit so the panel populates on first online.
	r.kickInstalledSkillsReport()
	return nil
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

	// codex supervisor MCP (T972): generate the SAME canonical mcp_config.runtime.json
	// the claude supervisor gets (agent-center host binary + per-agent AC_MCP_* creds),
	// then translate it into $CODEX_HOME/config.toml so the codex supervisor reaches the
	// same center tools (create_task/complete_task/post_message) via config.toml instead
	// of claude's --mcp-config. CODEX_HOME is exported to the codex process (below).
	mcpBytes, err := mcphost.GenerateMCPConfig(mcphost.MCPConfigParams{
		ServerName:        MCPServerName,
		Command:           r.cfg.BinaryPath,
		Args:              []string{"worker", "mcp-host"},
		AgentID:           agentID,
		AdminURL:          r.cfg.AdminURL,
		WorkerToken:       r.cfg.WorkerToken,
		ServerFingerprint: r.cfg.ServerFingerprint,
		AgentRoot:         tasksDir,
	})
	if err != nil {
		return fmt.Errorf("agent_controller: generate codex mcp-config: %w", err)
	}
	codexHome, err := WriteCodexMCPConfig(home, mcpBytes)
	if err != nil {
		return fmt.Errorf("agent_controller: write codex mcp-config: %w", err)
	}
	// T977 fix #1: provision the codex login auth.json into the per-agent CODEX_HOME.
	// codex reads auth from $CODEX_HOME; the dedicated per-agent home has the generated
	// config.toml but NOT the login auth (which lives in the worker's real CODEX_HOME /
	// ~/.codex), so without this codex 401s and the ENTIRE MCP chain is unreachable
	// (tester3 T977 — the config-source-reaches-process blind spot). Fail-loud (loud
	// warn, the executor codexAuthPreflight discipline) if it can't be provisioned —
	// never a silent 401.
	if w := provisionCodexAuth(codexHome, resolveSourceCodexHome()); w != "" {
		r.log("codex agent=%s: WARNING codex supervisor auth NOT provisioned into %s — codex will FAIL auth (401) and MCP will be UNREACHABLE; %s", agentID, codexHome, w)
	}

	// T972 supervisor resume (early-persist): read the prior generation's captured
	// thread_id (persisted via OnThreadID→MarkSessionID) so this relaunch RESUMES it,
	// then claim the single-instance lease (like the claude path) seeded with it. A
	// relaunch (a prior generation existed) that left NO thread_id can't resume → a
	// FRESH session, logged fail-loud (codex mints its thread_id only at thread.started,
	// so a session that died before its first event has nothing to resume — the safe,
	// visible fallback, never a silent loss).
	prior, _ := sessioninstance.ReadInstance(home)
	resumeThreadID := prior.SessionID
	// T977 fix #3: never resume across a cli-switch — a session_id from a non-codex prior
	// generation is a claude session id, not a codex thread_id (`codex exec resume` →
	// "no rollout"). Discard it + start fresh, logged.
	if resumeThreadID != "" && priorCLI != "" && priorCLI != CLICodex {
		r.log("codex agent=%s: prior generation was cli=%q (not codex) — discarding its stale session_id, starting a FRESH codex session (no cross-cli resume)", agentID, priorCLI)
		resumeThreadID = ""
	}
	if prior.Generation > 0 && resumeThreadID == "" {
		r.log("codex agent=%s: prior generation left no thread_id (thread.started never captured) — starting a FRESH codex session, prior conversation NOT resumed", agentID)
	}
	if _, lerr := sessioninstance.AcquireInstance(home, resumeThreadID, os.Getpid()); lerr != nil {
		return fmt.Errorf("agent_controller: acquire codex instance: %w", lerr)
	}

	sess, err := r.cfg.CodexStarter(ctx, CodexSpec{
		AgentID:        agentID,
		TasksDir:       tasksDir,
		Binary:         r.cfg.CodexBinary,
		Model:          spec.Model,
		DisplayName:    spec.DisplayName,
		EnvVars:        spec.EnvVars,
		CodexHome:      codexHome,
		ResumeThreadID: resumeThreadID,
		OnThreadID: func(tid string) {
			if merr := sessioninstance.MarkSessionID(home, tid); merr != nil {
				r.log("codex agent=%s: persist thread_id failed: %v (resume unavailable on next restart)", agentID, merr)
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
	// issue-4a45e9cc: BOOT installed-skill report (best-effort, off the start path).
	r.kickInstalledSkillsReport()
	return nil
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
	r.mu.Lock()
	sess := r.state.Session
	if sess == nil {
		r.mu.Unlock()
		if reportLifecycle {
			r.reportLifecycleOnce(ctx, "stopped", "")
		}
		return nil
	}
	r.state.ExpectedStop = true
	r.mu.Unlock()

	if err := sess.Stop(ctx); err != nil {
		r.log("stop agent=%s: %v", agentID, err)
	}

	if home, _, _, pathErr := r.agentPaths(agentID); pathErr == nil {
		if relErr := sessioninstance.ReleaseInstance(home); relErr != nil {
			r.log("stop agent=%s release instance: %v", agentID, relErr)
		}
	}

	if reportLifecycle {
		r.reportLifecycleOnce(ctx, "stopped", "")
	}
	return nil
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

// NotifyWorkAvailable is the interface entry for a work_available signal that routes
// straight to a fork (SpawnExecutor). The daemon's workAvailable command handler owns
// the dedup/relaunch/nudge orchestration and calls SpawnExecutor directly for the
// concurrency branch, so this thin delegate is here for interface completeness /
// future supervisor-driven fork_executor wiring.
func (r *LocalRuntime) NotifyWorkAvailable(ctx context.Context, taskID string) error {
	_, err := r.SpawnExecutor(ctx, SpawnRequest{TaskID: taskID})
	return err
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
