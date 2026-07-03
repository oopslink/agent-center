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

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/mcphost"
	"github.com/oopslink/agent-center/internal/supervisormanager"
	"github.com/oopslink/agent-center/internal/workerdaemon/reporepo"
	"github.com/oopslink/agent-center/internal/workerdaemon/sessioninstance"
	"github.com/oopslink/agent-center/internal/workerdaemon/taskexec"
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

	// SelfHeal is the daemon-level crash-recovery survival store (shared).
	SelfHeal *SelfHealStore

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

	// RemoveAgent deletes the managedAgent from the daemon map. onExit invokes it
	// OUTSIDE the runtime's StateMu (去共享状态), so the seam is responsible for taking
	// the daemon's c.mu (which guards c.agents) itself.
	RemoveAgent func(agentID string)

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
	return &LocalRuntime{cfg: cfg, state: state}
}

// State returns the shared SessionState (the daemon's managedAgent points at the
// SAME instance).
func (r *LocalRuntime) State() *SessionState { return r.state }

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
	// Session-scoped config carried across a crash (self-heal gets no fresh reconcile).
	r.mu.Lock()
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
	return nil
}

// startCodex starts a cli=codex session via the neutral CodexSpec (the daemon
// adapter fills Launcher + merged env).
func (r *LocalRuntime) startCodex(ctx context.Context, spec StartSpec, home, tasksDir string) error {
	agentID := spec.AgentID
	if err := WriteAgentCLIMarker(home, CLICodex); err != nil {
		return fmt.Errorf("agent_controller: write codex cli marker: %w", err)
	}
	sess, err := r.cfg.CodexStarter(ctx, CodexSpec{
		AgentID:     agentID,
		TasksDir:    tasksDir,
		Binary:      r.cfg.CodexBinary,
		Model:       spec.Model,
		DisplayName: spec.DisplayName,
		EnvVars:     spec.EnvVars,
		Logger:      r.rawLogger(),
		OnEvent:     func(ev claudestream.StreamEvent) { r.onEvent(ev) },
		OnExit:      func(exitErr error) { r.onExit(exitErr) },
	})
	if err != nil {
		return fmt.Errorf("agent_controller: start codex session: %w", err)
	}
	r.mu.Lock()
	r.state.Session = sess
	r.mu.Unlock()
	r.log("started codex agent=%s version=%d home=%s", agentID, spec.Version, home)
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
