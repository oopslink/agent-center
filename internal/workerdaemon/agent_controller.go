// Package workerdaemon: AgentController is the v2.7 control-command executor. It
// drives a per-agent SupervisorSession (D2-f s3b-1) — the persistent supervisor
// that SOLELY owns claude — in response to declarative control commands pulled by
// the ControlLoop, and reports RESULT feedback to the center via the D2-c-i
// /admin/environment/agent/* endpoints (the feedbackReporter seam).
//
// It implements CommandHandler (control_loop.go). It is PURELY ADDITIVE and stays
// DORMANT until the D2-f cutover: the control loop only runs when
// RuntimeConfig.ControlClient != nil (set by --use-control-loop). Until activated,
// the daemon's observable behaviour is unchanged.
//
// Command dispatch (see Handle):
//   - "agent.reconcile" → reconcile the real process to the desired lifecycle
//     (start / stop / reset), keyed by a monotonic version for replay safety.
//   - "agent.work"      → inject the work brief into the running session +
//     report the WorkItem active.
//   - "agent.wake"      → inject a posted task message + report the WorkItem active.
//   - unknown           → log + return nil (never wedge the ack cursor).
//
// Idempotency: returning nil from Handle advances the cumulative ack cursor;
// returning an error keeps the command un-acked so the ControlLoop re-pulls it
// next tick. The controller therefore returns nil for "already applied" replays
// (no-op) and reserves errors for genuinely transient failures it WANTS retried.
//
// OWNERSHIP (s3b-2b, PM-pinned): the controller NEVER execs claude. Every session
// is started via the injected sessionStarter, which in PRODUCTION is the real
// supervisor-spawn adapter (claude's parent is the supervisor, never the daemon).
// The agentSession interface is a TEST SEAM only: controller LOGIC is unit-tested
// with a lightweight fake that lives ONLY in _test.go and never appears in a
// production path. grep-clean = no direct claude exec on any production path.
package workerdaemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/runtimefs"
	"github.com/oopslink/agent-center/internal/supervisormanager"
	"github.com/oopslink/agent-center/internal/workerdaemon/agentruntime"
	"github.com/oopslink/agent-center/internal/workerdaemon/sessioninstance"
	"github.com/oopslink/agent-center/internal/workerdaemon/taskexec"
)


// Command types (mirror the projector constants — kept local so the controller
// does not import the Environment/PM service packages).
const (
	cmdTypeAgentReconcile = "agent.reconcile"
	cmdTypeAgentWork      = "agent.work"
	cmdTypeAgentWake      = "agent.wake"
	cmdTypeAgentConverse  = "agent.converse"       // v2.7 #185: DM/channel message → inject (no WorkItem)
	cmdTypeWorkAvailable  = "agent.work_available" // v2.8.1 #278 D pull-model WAKE (PR2 emit / PR3 handle)
)

// mcpServerName is the `mcpServers` map key for the per-agent worker mcp-host
// server in the generated --mcp-config document.
const mcpServerName = "agent-center"

// reconcilePayload decodes an "agent.reconcile" command payload. Matches
// internal/environment/service/agent_control_projector.go reconcileCommandPayload.
type reconcilePayload struct {
	AgentID          string `json:"agent_id"`
	DesiredLifecycle string `json:"desired_lifecycle"`
	Model            string `json:"model,omitempty"`
	// DisplayName is the agent's human-readable display_name, carried the SAME way as
	// Model from the lifecycle event so the supervisor injects it as
	// GIT_{AUTHOR,COMMITTER}_NAME via the ② AgentEnv seam (T469). Empty → ULID fallback.
	DisplayName string `json:"display_name,omitempty"`
	// EnvVars is the persisted per-agent profile env overlay applied to agent CLI
	// processes (supervisor-owned claude, codex turns, and forked executors).
	EnvVars map[string]string `json:"env_vars,omitempty"`
	// CLI selects the per-CLI session starter ("codex" → CodexSession; empty /
	// "claude-code" → the claude supervisor path).
	CLI string `json:"cli,omitempty"`
	// T236 LLM tuning — transported all the way from the persisted agent profile
	// to this reconcile handler (modeled + persisted + carried through the control
	// loop). Model/CLI are applied at spawn today; reasoning/mode/provider are
	// reserved here for the spawn wiring (the supervisor→claude exec flags), which
	// lands as the CLI adapter gains flag support. Empty = runtime default.
	Reasoning string `json:"reasoning,omitempty"`
	Mode      string `json:"mode,omitempty"`
	Provider  string `json:"provider,omitempty"`
	// F3 model routing (design §5 & §10) — transported from the persisted agent
	// profile through the control loop to the daemon. The modelrouter package
	// consumes these at executor-spawn time. Empty/zero = center default.
	OrchestratorModel    string                  `json:"orchestrator_model,omitempty"`
	DefaultExecutorModel string                  `json:"default_executor_model,omitempty"`
	MaxConcurrentTasks   int                     `json:"max_concurrent_tasks,omitempty"`
	AllowedModels        []string                `json:"allowed_models,omitempty"`
	AllowedExecutors     []agent.ExecutorProfile `json:"allowed_executors,omitempty"` // v2.18.1 BE-1: authoritative {cli,model} candidates (opt-in gate reads this)
	// PromptDescription is the already-gated description text to inject into the
	// agent's system prompt (T728), carried the SAME way as DisplayName. Empty ⇒ no
	// injection. Threaded to the supervisor's --prompt-description at spawn.
	PromptDescription string `json:"prompt_description,omitempty"`
	Version           int    `json:"version"`
	ResetScope        string `json:"reset_scope,omitempty"`
}

// workPayload decodes an "agent.work" command payload. Matches
// internal/projectmanager/service/work_item_projector.go workCommandPayload.
type workPayload struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
	TaskRef string `json:"task_ref"`
	Brief   string `json:"brief"`
}

// wakePayload decodes an "agent.wake" command payload. Matches
// internal/environment/service/wake_projector.go wakeCommandPayload.
//
// D2-e-ii: ConversationID is carried so the controller can advance the agent's
// read-state cursor (ReportMarkSeen) after a successful inject — both for the
// e-i immediate wake (single message) and the e-ii batch flush. MessageID is
// the NEWEST delivered message id (the cursor target); MessageText is the merged
// batch text in the e-ii path (single message in the e-i path).
type wakePayload struct {
	AgentID        string `json:"agent_id"`
	TaskID         string `json:"task_id"`
	TaskRef        string `json:"task_ref"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	MessageText    string `json:"message_text"`
	// RootMessageID (F4): thread root of the triggering message (empty if top-level).
	RootMessageID string `json:"root_message_id,omitempty"`
}

// workAvailablePayload decodes an "agent.work_available" (wake) command. Matches
// the projectors' workAvailablePayload (pm WorkItemProjector + env
// AgentControlProjector). v2.8.1 #278 D pull model: a per-agent "you have new
// work — pull your queue" signal. PR3 only DEDUPS (per work_item_id, mirroring
// wake message dedup) + logs + acks — the actual session inject ("check your
// queue") + the agent's pull-loop land together in PR4. WorkItemID is the
// per-WI idempotency/dedup key.
type workAvailablePayload struct {
	AgentID string `json:"agent_id"`
	TaskID  string `json:"task_id"`
}

// conversePayload decodes an "agent.converse" command (v2.7 #185). Mirrors
// environment/service.converseCommandPayload. NON-WorkItem: a DM/channel message
// injected into the running session so the agent replies via the post_message
// MCP tool (conversation_id is the reply target + the read-state cursor).
type conversePayload struct {
	AgentID        string `json:"agent_id"`
	ConversationID string `json:"conversation_id"`
	ConvKind       string `json:"conv_kind"`
	ConvName       string `json:"conv_name"`
	SenderRef      string `json:"sender_ref"`
	SenderDisplay  string `json:"sender_display"`
	MessageID      string `json:"message_id"`
	MessageText    string `json:"message_text"`
	// RootMessageID (F4): thread root of the triggering message (empty if top-level).
	// When set, the agent was @mentioned INSIDE a thread → its reply must land in the
	// same thread (the brief tells it to pass parent_message_id).
	RootMessageID string `json:"root_message_id,omitempty"`
	// AttachmentCount (v2.10.0 [T74]): how many attachments the triggering message
	// carries. >0 → the brief tells the agent a human sent file(s) (e.g. a
	// screenshot) and to call get_my_unread → download_file to view them.
	AttachmentCount int `json:"attachment_count,omitempty"`
	// OwnerRef (T250/T254): the source conversation's owner_ref. For a pm:// owner
	// chat it is pm://plans|issues|tasks|projects/{id}; buildConverseBrief resolves
	// it through the OwnerContext table (internal/conversation) to tell the agent
	// WHICH object the message belongs to (with ConvName carrying the env-resolved
	// name/title) so it can disambiguate "this {kind}" across concurrent chats.
	// Empty for DM; id://organizations/{org} for a channel (not id-anchored).
	OwnerRef string `json:"owner_ref,omitempty"`
}

// AgentControllerConfig parameterises the controller.
type AgentControllerConfig struct {
	// Reporter posts RESULT feedback to the center. Required.
	Reporter feedbackReporter
	// ToolCaller reaches the center agent-tools endpoints (complete_task / block_task
	// / post_message) for the W2 executor writeback (the orchestrator's sole-writer
	// result sink). Optional: nil ⇒ the concurrent-execution Monitor degrades to
	// reap-and-free-slot with no center writeback (W1 behaviour). *AdminClient
	// satisfies it (CallAgentTool).
	ToolCaller agentToolCaller
	// WorkerID is this daemon's worker id (for the agent home layout + mcp env).
	WorkerID string
	// AdminURL is the admin endpoint the per-agent mcp-host dials (AC_MCP_ADMIN_URL).
	AdminURL string
	// WorkerToken is the worker bearer passed to the mcp-host (AC_MCP_WORKER_TOKEN).
	WorkerToken string
	// ServerFingerprint is the pinned cert fingerprint for tcp:// admin URLs
	// (AC_MCP_SERVER_FINGERPRINT).
	ServerFingerprint string
	// BinaryPath is the agent-center binary the mcp-host server is launched as
	// (command for the --mcp-config entry; also the claude binary override is
	// separate). Empty → "agent-center".
	BinaryPath string
	// ClaudeBinary overrides the claude binary path (empty → "claude" on PATH).
	ClaudeBinary string
	// CodexBinary overrides the codex binary path (empty → "codex" on PATH). Used
	// only for cli=codex agents (the CodexSession path).
	CodexBinary string
	// AgentHomeBase is the runtime home root. Per-agent home resolves to
	// AgentHomeBase/workers/{worker_id}/agents/{agent_id}/ (C1 OQ7 layout).
	// Required for start/reset (mcp-config + workspace live under it).
	AgentHomeBase string
	// Logger receives one-line ops messages. Nil → silent.
	Logger func(msg string)
	// StopGrace is the graceful-stop window forwarded to each SupervisorSession
	// (Stop → StopSupervisor SIGTERM grace).
	StopGrace time.Duration

	// DisableUsageReport turns OFF the per-turn report_usage hook (v2.15.0 I28/F2).
	// Default (zero value, false) = reporting ON. This is the ops kill-switch: the
	// hook is best-effort/non-blocking, but if it ever becomes noisy, flipping it
	// (AGENT_CENTER_DISABLE_USAGE_REPORT=1) stops new reports immediately.
	DisableUsageReport bool

	// Resumer queries the center for this worker's boot-resume state (s4b boot
	// reconcile). Nil → ReconcileOnBoot is a no-op (additive/dormant). The daemon's
	// *AdminClient satisfies resumeStateQuerier.
	Resumer resumeStateQuerier
	// ResumeNudge is injected into a RELAUNCHED agent's session that has an ACTIVE
	// WorkItem, so the interrupted task continues (claude --session-id resumes the
	// conversation, but whether it auto-continues the interrupted turn or needs a
	// push is GATE-7 real-claude territory). Empty → DefaultResumeNudge. NEVER
	// injected on re-attach (claude is alive and mid-turn — a nudge would corrupt it).
	ResumeNudge string

	// Now is the clock seam (unit tests inject a deterministic clock to assert the
	// self-heal backoff curve / cap / reset). Nil → time.Now.
	Now func() time.Time
	// Self-heal (mid-run crash recovery) tuning; 0 → defaults (backoff 1→2→4→8→16s,
	// cap 30s, maxAttempts 5, healthy-reset window 60s). @oopslink/PM + the product
	// manual are authoritative for the production values.
	SelfHealMaxAttempts int
	SelfHealBackoffBase time.Duration
	SelfHealBackoffCap  time.Duration
	SelfHealResetWindow time.Duration

	// LeaseRenewEvery is the cadence of the process-alive lease auto-renew (T456 /
	// issue-21ba5b78 I30 P0 #1): OnTick renews the execution lease for every live
	// session's current task at most this often, decoupled from the agent's LLM turn,
	// so a long build/test never lets the lease lapse. 0 → DefaultLeaseRenewEvery. It
	// MUST stay well under the server's DefaultExecutionLeaseTTL so a renew lands long
	// before the lease would lapse.
	LeaseRenewEvery time.Duration

	// Rate-limit auto-recovery tuning (issue: LLM 服务端限流自动恢复); 0 → defaults
	// (default 60s when claude gives no window, floor 5s, cap 1h). When an LLM
	// server-side rate-limit ends a turn, the controller schedules an automatic
	// resume after the window clears (using claude's retry_after / resets_at when
	// present) instead of abandoning the in-flight work. See rate_limit.go.
	RateLimitDefaultBackoff time.Duration
	RateLimitMinBackoff     time.Duration
	RateLimitMaxBackoff     time.Duration

	// Transient-API-error auto-retry tuning (T475: "API Error: Connection closed
	// mid-response"); 0 → defaults (base 2s, doubling per attempt, cap 60s, max 5
	// retries). When a turn ends in a transient API/connection error (vs an ordinary
	// failure), the controller schedules a bounded, exponentially backed-off resume
	// to re-drive the SAME work instead of abandoning it. See api_error.go.
	APIErrorBackoffBase time.Duration
	APIErrorBackoffCap  time.Duration
	APIErrorMaxRetries  int

	// TaskDirManager manages per-task execution directories. Nil → task
	// directory management disabled (backwards-compatible).
	TaskDirManager *taskexec.DirManager
	// TaskVerifier checks task assignment with Center for boot reconcile.
	// Nil → boot task reconcile skipped.
	TaskVerifier taskexec.TaskVerifier
	// GCInterval is the minimum interval between GC sweeps (design §11.3).
	// 0 → default 1 hour. GC is only active when TaskDirManager is non-nil.
	GCInterval time.Duration

	// SegmentMaxBytes is the per-task event-stream segment roll threshold
	// (design §8.1): once events.current.jsonl crosses it AND Center has acked the
	// whole segment, onEvent rolls it into events.{seq}.jsonl.gz. 0 →
	// taskexec.DefaultSegmentMaxBytes (8 MiB). A completed task always force-seals
	// its segment regardless of this threshold. Active only when TaskDirManager is
	// non-nil. See taskevents.go.
	SegmentMaxBytes int64

	// TaskLogMaxBytes is the per-task task.log rotation cap (design §3 / W4): the
	// active tasks/{id}/task.log rotates aside to task.log.1 once a write would
	// push it past this. 0 → tasklog.DefaultMaxBytes (10 MiB). Active only when
	// TaskDirManager is non-nil. See taskevents.go.
	TaskLogMaxBytes int64

	// starter is the session factory (test seam, PM s3b-2b). Unexported so ONLY
	// same-package _test.go can override it with a fake — production callers cannot
	// set it, so NewAgentController always defaults it to the real supervisor-spawn
	// adapter. This is the test seam that keeps the controller LOGIC unit-testable
	// without a real spawn while guaranteeing production wires only the real
	// *SupervisorSession (grep-clean ownership).
	starter sessionStarter
	// codexStarter is the cli=codex session factory (test seam, same contract as
	// starter). Production = startCodexSessionAdapter (real codex exec session).
	codexStarter codexSessionStarter
}

// managedAgent tracks one live (or recently-live) agent session. Phase 0b: the
// per-agent SESSION state moved into agentruntime.SessionState (shared with the
// LocalRuntime by pointer under the SHARED mutex); managedAgent keeps only the
// daemon-owned handles (runtime + executor面 + the pull-model work_available dedup +
// the reconcile-replay version).
type managedAgent struct {
	agentID string

	// runtime is the per-agent execution面 (docs §4.0). Notify* / Start / Stop /
	// Tick route through it. Concrete type (the daemon needs lifecycle methods
	// beyond the Runtime interface). Guarded by AgentController.mu.
	runtime *agentruntime.LocalRuntime

	// state is the SHARED per-agent session state (the SAME *SessionState the
	// runtime holds). nil for a stub managedAgent (recordVersion / recordWorkAvail
	// before a session exists). Every field is guarded by AgentController.mu.
	state *agentruntime.SessionState

	// (Phase 0c) the per-agent executor engine moved OFF managedAgent onto the
	// runtime (LocalRuntime.exec). Presence is queried via runtime.HasExecutor() and
	// installed via runtime.AttachExecutor(ee).

	// appliedVersion is the highest reconcile version applied (replay guard).
	appliedVersion int

	// workAvailSeen / workAvailOrder is the bounded agent.work_available coalesce set
	// (pull model, 0c). FIFO eviction at wakeDedupCap. Guarded by AgentController.mu.
	workAvailSeen  map[string]struct{}
	workAvailOrder []string
}

// live reports whether this managedAgent has a live session. Caller must hold c.mu.
func (ma *managedAgent) live() bool {
	return ma != nil && ma.state != nil && ma.state.Session != nil
}

// AgentController implements CommandHandler. State is a map of agentID →
// managedAgent guarded by mu. Safe for the single-threaded ControlLoop caller;
// the mutex also guards against the session OnExit/OnEvent callbacks (which run
// on the session's reader goroutine) mutating shared state concurrently.
type AgentController struct {
	cfg AgentControllerConfig

	mu     sync.Mutex
	agents map[string]*managedAgent

	// bg tracks best-effort background goroutines spawned off the event-pump on a
	// clean turn-end (notably MarkCompletedTurn, which writes session.instance into
	// the agent home). Production stays fully async — these are NEVER awaited on the
	// hot path — but Shutdown drains bg after detaching the sessions so a caller that
	// tears down its agent-home directory (every t.TempDir()-based test) is
	// guaranteed no goroutine is still writing into it (T672: the async home write
	// raced t.TempDir() RemoveAll → "directory not empty"). Add is only ever called
	// from the pump goroutine, which Detach joins before Wait — so no Add races Wait.
	bg sync.WaitGroup

	// selfHeal is the daemon-level mid-run crash-recovery survival store (the
	// decide/record logic lives in agentruntime.SelfHealStore). It SURVIVES the
	// managedAgent delete on crash. Guarded by mu (the SAME shared mutex the store
	// holds). Both onExit (via the runtime) and OnTick reach this SAME instance.
	selfHeal *agentruntime.SelfHealStore

	// nextLeaseRenewAt gates the T456 process-alive lease auto-renew sweep so it runs
	// at most every cfg.LeaseRenewEvery even though OnTick fires on the (sub-second)
	// poll cadence. Guarded by mu. See lease_renew.go.
	nextLeaseRenewAt time.Time

	// lastGCAt is the last time a GC sweep completed. Used by maybeRunGC to
	// throttle to at most once per cfg.GCInterval. See gc_timer.go.
	lastGCAt time.Time

	// lastExecWatchdogAt throttles the executor watchdog/orphan-poll sweep
	// (maybeRunExecutorWatchdog) to at most once per defaultExecutorWatchdogInterval.
	// Guarded by mu. See concurrent_exec.go (W3).
	lastExecWatchdogAt time.Time

	// recoveredExec records which agents have already had their executor crash
	// recovery run in THIS daemon process (W3). Crash recovery (scan executors/ +
	// re-adopt orphans) runs exactly ONCE per agent per process — at the first engine
	// attach after a (re)start — because a later in-process engine rebuild's running
	// executors are this process's own children, not orphans. Guarded by mu.
	recoveredExec map[string]bool

	// execConfig caches the concurrency-relevant reconcile config (CLI + the F3
	// model-routing + executor-cap fields buildExecutorEngine reads) per agent, so the
	// executor engine can be RE-ATTACHED on EVERY bring-up — not just the reconcile(
	// running) command that maybeAttachExecutorEngine fires on. A concurrency-enabled
	// agent brought up via boot-reconcile (worker restart) or self-heal relaunch gets
	// NO fresh reconcile command, so without this its ma.exec stays nil and
	// work_available silently falls back to the single-active nudge path — concurrency
	// degrades to single-active after ANY restart/crash. Populated by
	// maybeAttachExecutorEngine (reconcile path) AND seeded from the center resume-state
	// on boot (so it survives a full worker process restart). Read by
	// reattachExecutorEngineFromCache, called after every relaunch (bootReapRelaunch).
	// Guarded by mu.
	execConfig map[string]reconcilePayload

	// eventWriter is the stateless per-task event-stream sink (W3): onEvent appends
	// each in-flight task's stream events to tasks/{id}/events.current.jsonl through
	// it, acks them to events.offset, and rolls archived segments. Stateless, so a
	// single shared instance is safe across agents. See taskevents.go.
	eventWriter *taskexec.EventStreamWriter
}

// compile-time: AgentController is a CommandHandler.
var _ CommandHandler = (*AgentController)(nil)

// NewAgentController constructs the controller. Reporter is required; the session
// starter defaults to the production real-supervisor-spawn adapter (only
// same-package tests can override it with a fake).
func NewAgentController(cfg AgentControllerConfig) (*AgentController, error) {
	if cfg.Reporter == nil {
		return nil, errors.New("agent_controller: reporter required")
	}
	if cfg.starter == nil {
		cfg.starter = startSupervisorSessionAdapter
	}
	if cfg.codexStarter == nil {
		cfg.codexStarter = startCodexSessionAdapter
	}
	if cfg.LeaseRenewEvery <= 0 {
		cfg.LeaseRenewEvery = DefaultLeaseRenewEvery
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string) {}
	}
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		cfg.BinaryPath = "agent-center"
	}
	c := &AgentController{
		cfg:           cfg,
		agents:        map[string]*managedAgent{},
		recoveredExec: map[string]bool{},
		execConfig:    map[string]reconcilePayload{},
		eventWriter:   taskexec.NewEventStreamWriter(),
	}
	// The self-heal survival store shares the controller's mutex + clock + logger
	// (decide/record logic in agentruntime; store stays daemon-level so it survives
	// the managedAgent delete onExit does on a crash).
	c.selfHeal = agentruntime.NewSelfHealStore(&c.mu, agentruntime.SelfHealParams{
		MaxAttempts: cfg.SelfHealMaxAttempts,
		BackoffBase: cfg.SelfHealBackoffBase,
		BackoffCap:  cfg.SelfHealBackoffCap,
		ResetWindow: cfg.SelfHealResetWindow,
	}, c.log)
	return c, nil
}

// now returns the controller clock (test seam; defaults to time.Now).
func (c *AgentController) now() time.Time {
	if c.cfg.Now != nil {
		return c.cfg.Now()
	}
	return time.Now()
}

func (c *AgentController) log(format string, args ...any) {
	c.cfg.Logger(fmt.Sprintf("[worker] agent_controller: "+format, args...))
}

// Handle dispatches a single control command. Returning nil advances the ack
// cursor; returning an error keeps the command un-acked for retry.
func (c *AgentController) Handle(ctx context.Context, cmd ControlCommand) error {
	switch cmd.CommandType {
	case cmdTypeAgentReconcile:
		var pl reconcilePayload
		if err := json.Unmarshal([]byte(cmd.Payload), &pl); err != nil {
			// A malformed payload is not retryable — log + ack so the cursor
			// advances (a retry would just fail again forever).
			c.log("reconcile decode (offset=%d): %v — skipping", cmd.Offset, err)
			return nil
		}
		return c.reconcile(ctx, pl)
	case cmdTypeAgentWork:
		var pl workPayload
		if err := json.Unmarshal([]byte(cmd.Payload), &pl); err != nil {
			c.log("work decode (offset=%d): %v — skipping", cmd.Offset, err)
			return nil
		}
		return c.routeWork(ctx, pl)
	case cmdTypeAgentWake:
		var pl wakePayload
		if err := json.Unmarshal([]byte(cmd.Payload), &pl); err != nil {
			c.log("wake decode (offset=%d): %v — skipping", cmd.Offset, err)
			return nil
		}
		return c.routeWake(ctx, pl)
	case cmdTypeAgentConverse:
		var pl conversePayload
		if err := json.Unmarshal([]byte(cmd.Payload), &pl); err != nil {
			c.log("converse decode (offset=%d): %v — skipping", cmd.Offset, err)
			return nil
		}
		return c.routeConverse(ctx, pl)
	case cmdTypeWorkAvailable:
		var pl workAvailablePayload
		if err := json.Unmarshal([]byte(cmd.Payload), &pl); err != nil {
			c.log("work_available decode (offset=%d): %v — skipping", cmd.Offset, err)
			return nil
		}
		return c.workAvailable(ctx, pl)
	case cmdTypeRuntimeFs:
		var pl runtimefs.Command
		if err := json.Unmarshal([]byte(cmd.Payload), &pl); err != nil {
			c.log("runtime_fs decode (offset=%d): %v — skipping", cmd.Offset, err)
			return nil
		}
		return c.runtimeFs(ctx, pl)
	default:
		// Unknown command type: log + ack (don't wedge the cursor on a command
		// this controller version doesn't understand).
		c.log("unknown command_type=%q offset=%d — skipping", cmd.CommandType, cmd.Offset)
		return nil
	}
}

// reconcile drives the real process toward the desired lifecycle. Version
// idempotency: a reconcile whose version is <= the last-applied version is a
// replay (e.g. after reconnect) and is a NO-OP — we never restart on a replay.
func (c *AgentController) reconcile(ctx context.Context, pl reconcilePayload) error {
	if strings.TrimSpace(pl.AgentID) == "" {
		c.log("reconcile missing agent_id — skipping")
		return nil
	}

	c.mu.Lock()
	ma := c.agents[pl.AgentID]
	if ma != nil && pl.Version <= ma.appliedVersion {
		// Already applied (or older) — replay. Do NOT restart.
		c.mu.Unlock()
		c.log("reconcile agent=%s version=%d <= applied=%d — replay no-op",
			pl.AgentID, pl.Version, ma.appliedVersion)
		return nil
	}
	c.mu.Unlock()

	switch pl.DesiredLifecycle {
	case "running":
		return c.reconcileRunning(ctx, pl)
	case "stopped", "stopping":
		return c.reconcileStop(ctx, pl)
	case "resetting":
		return c.reconcileReset(ctx, pl)
	case "error":
		// The controller does not push an agent to error; error is feedback the
		// controller emits (OnExit crash path), not a desired state to enact.
		c.log("reconcile agent=%s desired=error — no-op (controller does not enact error)", pl.AgentID)
		c.recordVersion(pl.AgentID, pl.Version)
		return nil
	default:
		c.log("reconcile agent=%s unknown desired_lifecycle=%q — skipping", pl.AgentID, pl.DesiredLifecycle)
		c.recordVersion(pl.AgentID, pl.Version)
		return nil
	}
}

// reconcileRunning ensures a live session exists. If none, start one. If one
// exists and this is a version bump, restart (stop old + start new) — per
// ADR-0049 a Restart bumps the version, and the version-idempotency guard above
// already rejected pure replays, so reaching here with a live session means a
// real restart intent.
func (c *AgentController) reconcileRunning(ctx context.Context, pl reconcilePayload) error {
	// A command-driven (re)start is the intentional/manual path — clear any self-heal
	// crash accounting and UN-LATCH a circuit-broken (terminal-failed) agent (operator
	// restart is the way out of terminal-failed).
	c.clearSelfHeal(pl.AgentID)

	c.mu.Lock()
	ma := c.agents[pl.AgentID]
	hasLive := ma.live()
	c.mu.Unlock()

	if hasLive {
		c.log("reconcile agent=%s running version-bump=%d — restarting", pl.AgentID, pl.Version)
		// Restart: stop the old instance (expected stop, but we are about to
		// replace it so we suppress the lifecycle report — a restart should NOT
		// emit a "stopped" feedback that would settle the agent's lifecycle).
		c.stopViaRuntime(ctx, pl.AgentID, false /*reportLifecycle*/)
	}

	// forkResume=false: an intent-driven start/restart is NOT a crash recovery. A
	// fresh start has no prior session; a restart stop-SIGTERMed the old claude
	// (lock released), so a plain resume of the same session-id is correct. Only the
	// Mode-B crash-relaunch paths (bootReapRelaunch) fork (the killed claude's lock
	// is still held).
	if err := c.bringUpSession(ctx, startSpecOf(pl)); err != nil {
		// Start failure IS retryable (transient FS / launch error) — return the
		// error so the command stays un-acked and is retried next tick.
		return fmt.Errorf("agent_controller: start agent=%s: %w", pl.AgentID, err)
	}

	// W1: attach the concurrent-execution engine when the agent opts in (profile
	// concurrency). Best-effort — a build failure logs and falls back to the legacy
	// inject path rather than failing the (already-running) session. The codex path
	// uses a different runtime and is excluded. Replaces any prior engine on restart.
	c.maybeAttachExecutorEngine(ctx, pl)
	return nil
}

// maybeAttachExecutorEngine builds + attaches the per-agent executor engine when
// concurrency is enabled for the agent (W1, PD decision 2). No-op otherwise, so the
// default single-claude inject path is byte-for-byte unchanged.
//
// W3: the FIRST time this process attaches an agent's engine — i.e. right after a
// daemon (re)start — it runs executor crash recovery (scan executors/ + re-adopt
// orphans into the watchdog), so a kill+restart loses no in-flight executor and
// double-launches none. The recoveredExec guard makes this run once per agent per
// process; a later in-process rebuild skips it (those executors are our children).
func (c *AgentController) maybeAttachExecutorEngine(ctx context.Context, pl reconcilePayload) {
	if !concurrencyEnabled(pl) || pl.CLI == cliCodex {
		return
	}
	// Cache the config so a later boot-reconcile / self-heal relaunch (neither of which
	// gets a fresh reconcile command) can RE-ATTACH the engine from it — otherwise
	// ma.exec stays nil after a restart and concurrency silently degrades to single-active.
	c.mu.Lock()
	c.execConfig[pl.AgentID] = pl
	rt := c.runtimeForLocked(pl.AgentID)
	c.mu.Unlock()

	// The engine installs onto the agent's LocalRuntime (rt.AttachExecutor). Without a
	// runtime there is nothing to attach to (a stub managedAgent with no session) —
	// leave it for the next bring-up's reattach-from-cache.
	if rt == nil {
		c.log("agent=%s executor engine: no runtime yet (falling back to inject until bring-up)", pl.AgentID)
		return
	}

	home, _, _, perr := c.agentPaths(pl.AgentID)
	if perr != nil {
		c.log("agent=%s executor engine paths: %v (falling back to inject)", pl.AgentID, perr)
		return
	}
	ee, err := rt.BuildExecutorEngine(home, execConfigOf(pl))
	if err != nil {
		c.log("agent=%s build executor engine: %v (falling back to inject)", pl.AgentID, err)
		return
	}
	rt.AttachExecutor(ee)

	c.mu.Lock()
	firstAttach := !c.recoveredExec[pl.AgentID]
	c.recoveredExec[pl.AgentID] = true
	c.mu.Unlock()
	c.log("agent=%s concurrent-execution enabled (max=%d, executors=%d)", pl.AgentID, pl.MaxConcurrentTasks, len(pl.AllowedExecutors))

	// First attach this process → recover orphans from a prior process (design §12).
	// The recovery-once-per-agent-per-process guard (recoveredExec) stays DAEMON-level
	// so a later in-process rebuild does NOT re-scan (would double-finalize/double-adopt).
	if firstAttach {
		_ = rt.Recover(ctx)
	}
}

// runtimeForLocked returns the agent's runtime (or nil). Caller MUST hold c.mu.
func (c *AgentController) runtimeForLocked(agentID string) *agentruntime.LocalRuntime {
	ma := c.agents[agentID]
	if ma == nil {
		return nil
	}
	return ma.runtime
}

// reattachExecutorEngineFromCache re-attaches the per-agent executor engine after a
// relaunch (boot-reconcile / self-heal — neither carries a fresh reconcile command)
// using the config cached by maybeAttachExecutorEngine or seeded from the center
// resume-state on boot. This is what keeps a concurrency-enabled agent CONCURRENT
// across restarts/crashes: without it, ma.exec on the freshly-relaunched managedAgent
// stays nil and work_available silently falls back to the single-active nudge path.
// No-op for an agent with no cached concurrency config (a default single-active agent).
func (c *AgentController) reattachExecutorEngineFromCache(ctx context.Context, agentID string) {
	pl, ok := c.cachedExecConfig(agentID)
	if !ok || !concurrencyEnabled(pl) {
		return
	}
	c.maybeAttachExecutorEngine(ctx, pl)
}

// cachedExecConfig returns the per-agent cached reconcile config (set by
// maybeAttachExecutorEngine or seeded from the center resume-state on boot), if any.
func (c *AgentController) cachedExecConfig(agentID string) (reconcilePayload, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pl, ok := c.execConfig[agentID]
	return pl, ok
}

// seedExecConfig stores a reconcile config for an agent without attaching the engine
// — used on boot to seed the cache from the center resume-state so the subsequent
// bootReapRelaunch can re-attach (the worker process restarted, so the in-memory
// cache from a prior reconcile is gone). No-op for a non-concurrency config.
func (c *AgentController) seedExecConfig(pl reconcilePayload) {
	if !concurrencyEnabled(pl) {
		return
	}
	c.mu.Lock()
	c.execConfig[pl.AgentID] = pl
	c.mu.Unlock()
}

// reconcileStop stops the session and reports lifecycle "stopped" exactly once.
func (c *AgentController) reconcileStop(ctx context.Context, pl reconcilePayload) error {
	c.clearSelfHeal(pl.AgentID) // desired-stopped → no self-heal relaunch
	c.recordVersion(pl.AgentID, pl.Version)
	c.stopViaRuntime(ctx, pl.AgentID, true /*reportLifecycle*/)
	return nil
}

// reconcileReset runs the clean-slate RESET chain (s3b-2b), the WHOLE sequence
// under the agent's home lock so it cannot interleave with a probe/spawn from
// another daemon (cross-daemon coherence; PM): SIGTERM the old supervisor →ⓦipe
// the per-scope dirs (STRICTLY contained) → BUMP the durable epoch (idempotent on
// reconcile version) → settle lifecycle "stopped". It does NOT auto-restart; the
// next reconcile(running) reads the BUMPED epoch and spawns a fresh claude
// session-id = the clean slate.
//
// The home lock is LOCK_NB: if another holder (a concurrent daemon op) has it, we
// return an error so the ControlLoop re-pulls the reset next tick rather than
// interleaving a half reset. The epoch bump is idempotent on pl.Version, so a
// re-pulled reset is safe.
func (c *AgentController) reconcileReset(ctx context.Context, pl reconcilePayload) error {
	c.clearSelfHeal(pl.AgentID) // reset = manual clean-slate → un-latch any terminal-failed
	c.recordVersion(pl.AgentID, pl.Version)

	home, _, _, err := c.agentPaths(pl.AgentID)
	if err != nil {
		// Cannot resolve the home (missing config) — settle the lifecycle so the
		// agent does not hang in "resetting"; nothing to wipe/bump without a home.
		c.log("reset agent=%s resolve home: %v", pl.AgentID, err)
		c.reportLifecycleOnce(ctx, pl.AgentID, "stopped", "")
		return nil
	}

	// Acquire the home lock for the WHOLE reset chain (no interleave with a
	// concurrent daemon's probe/relaunch on this agent).
	release, err := supervisormanager.AcquireHomeLock(home)
	if err != nil {
		// Another holder — retry next tick (the bump is version-idempotent).
		return fmt.Errorf("agent_controller: reset agent=%s acquire home lock: %w", pl.AgentID, err)
	}
	defer release()

	// 1. SIGTERM the old supervisor (expected stop) — suppress the lifecycle
	//    report until AFTER the wipe+bump so we never settle "stopped" early.
	c.stopViaRuntime(ctx, pl.AgentID, false /*reportLifecycle*/)

	// 2. Wipe the per-scope dirs under the agent home (contained).
	if err := c.cleanReset(pl.AgentID, pl.ResetScope); err != nil {
		// A containment violation / FS error: log it but still continue to the
		// epoch bump + settle (the process IS stopped). Returning an error would
		// just retry the (now process-less) reset forever; cleanup is best-effort.
		c.log("reset agent=%s scope=%q cleanup: %v", pl.AgentID, pl.ResetScope, err)
	}

	// 3. Bump the durable epoch (idempotent on pl.Version) → the next start derives
	//    a NEW claude session-id = a clean slate. Best-effort: a bump failure is
	//    logged, not fatal to the settle (a failed bump leaves the old epoch, which
	//    a later reconcile re-attempts; the wipe already cleared state).
	if st, berr := supervisormanager.BumpEpochForReset(home, pl.Version); berr != nil {
		c.log("reset agent=%s bump epoch: %v", pl.AgentID, berr)
	} else {
		c.log("reset agent=%s scope=%q epoch→%d (clean slate on next start)", pl.AgentID, pl.ResetScope, st.Epoch)
	}

	// 4. Settle resetting → stopped via the lifecycle feedback (MarkAgentStopped).
	c.reportLifecycleOnce(ctx, pl.AgentID, "stopped", "")
	return nil
}

// work ensures the agent has a running session, injects the brief, and reports
// the WorkItem active. Race policy (documented): if there is NO running session
// when work arrives, this is a delivery race — the reconcile(running) command
// for this agent should arrive (it bumped the version first). We return an ERROR
// so the ControlLoop re-pulls this work command next tick, by which point the
// session should be up. This is the lower-surprise option: we never silently
// drop work, and we never start an un-reconciled session (start is the
// reconcile's job — keeping a single source of truth for lifecycle).
// runtimeFor returns the per-agent runtime for an existing managedAgent, or nil when
// none exists (a signal arriving before reconcile started the session). Guarded by mu.
func (c *AgentController) runtimeFor(agentID string) *agentruntime.LocalRuntime {
	c.mu.Lock()
	defer c.mu.Unlock()
	ma := c.agents[agentID]
	if ma == nil {
		return nil
	}
	return ma.runtime
}

// routeWork dispatches an agent.work command through the per-agent runtime's
// NotifyWork, which now owns the exec-vs-session decision (Phase 0c): a
// concurrency-enabled runtime (r.exec != nil) forks an executor; otherwise it injects
// the brief into the resident session. The no-running-session error surface is
// preserved inside NotifyWork (byte-identical message).
func (c *AgentController) routeWork(ctx context.Context, pl workPayload) error {
	if strings.TrimSpace(pl.AgentID) == "" {
		c.log("work missing agent_id — skipping")
		return nil
	}
	rt := c.runtimeFor(pl.AgentID)
	if rt == nil {
		return fmt.Errorf("agent_controller: work for agent=%s but no running session (retry after reconcile)", pl.AgentID)
	}
	return rt.NotifyWork(ctx, workRequestOf(pl))
}

// routeWake dispatches an agent.wake command through the per-agent runtime.
func (c *AgentController) routeWake(ctx context.Context, pl wakePayload) error {
	if strings.TrimSpace(pl.AgentID) == "" {
		c.log("wake missing agent_id — skipping")
		return nil
	}
	rt := c.runtimeFor(pl.AgentID)
	if rt == nil {
		return fmt.Errorf("agent_controller: wake for agent=%s but no running session (retry after reconcile)", pl.AgentID)
	}
	return rt.NotifyWake(ctx, wakeRequestOf(pl))
}

// routeConverse dispatches an agent.converse command through the per-agent runtime.
func (c *AgentController) routeConverse(ctx context.Context, pl conversePayload) error {
	if strings.TrimSpace(pl.AgentID) == "" {
		c.log("converse missing agent_id — skipping")
		return nil
	}
	rt := c.runtimeFor(pl.AgentID)
	if rt == nil {
		return fmt.Errorf("agent_controller: converse for agent=%s but no running session (retry after reconcile)", pl.AgentID)
	}
	return rt.NotifyConverse(ctx, converseRequestOf(pl))
}

// recordWorkAvail notes a work_item_id under the per-agent agent.work_available
// coalesce set (v2.8.1 #278 D PR3). Returns true if NEWLY recorded, false if it
// was already seen (a coalesced re-emit/flap/replay). Mirrors recordWake (lazy
// managedAgent create + FIFO eviction at wakeDedupCap).
func (c *AgentController) recordWorkAvail(agentID, taskID string) bool {
	if taskID == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ma := c.agents[agentID]
	if ma == nil {
		ma = &managedAgent{agentID: agentID}
		c.agents[agentID] = ma
	}
	if ma.workAvailSeen == nil {
		ma.workAvailSeen = make(map[string]struct{}, wakeDedupCap)
	}
	if _, ok := ma.workAvailSeen[taskID]; ok {
		return false
	}
	ma.workAvailSeen[taskID] = struct{}{}
	ma.workAvailOrder = append(ma.workAvailOrder, taskID)
	for len(ma.workAvailOrder) > wakeDedupCap {
		oldest := ma.workAvailOrder[0]
		ma.workAvailOrder = ma.workAvailOrder[1:]
		delete(ma.workAvailSeen, oldest)
	}
	return true
}

// workAvailable handles the agent.work_available WAKE command (v2.8.1 #278 D PR3,
// PD-locked option b). It ONLY coalesces (per work_item_id) + logs + acks — it
// does NOT inject. Rationale: in the dual-track window (PR3–PR6) the old
// agent.work push still injects the brief + reports the WorkItem active, so the
// agent is already driven; a pull nudge here would be redundant and could nudge a
// not-yet-pull-driven agent into a premature start_work (benign 409 but activity
// noise that would muddy PR4's run-real pull-loop verification). The actual nudge
// inject + the agent's pull-driven loop land together in PR4 (clean cut). Acking
// (return nil) keeps the control-stream cursor advancing — never wedges the loop.
func (c *AgentController) workAvailable(ctx context.Context, pl workAvailablePayload) error {
	if strings.TrimSpace(pl.AgentID) == "" {
		c.log("work_available missing agent_id — skipping")
		return nil
	}
	c.mu.Lock()
	ma := c.agents[pl.AgentID]
	var sess agentSession
	var rt *agentruntime.LocalRuntime
	if ma != nil {
		if ma.state != nil {
			sess = ma.state.Session
		}
		rt = ma.runtime
	}
	c.mu.Unlock()

	// W4a / F1 (issue I55): a CONCURRENCY-ENABLED agent (executor engine attached to
	// its runtime — opt-in, non-codex) forks an isolated executor for the queued task
	// instead of nudging the resident claude. This is the LIVE producer that makes
	// executor concurrency fire in production. MUTUALLY EXCLUSIVE with the nudge/relaunch
	// path below: we fork (or leave queued) and ALWAYS short-circuit return — never also
	// Inject the pull nudge — so the executor and the resident session can't both drive
	// the same task (防双跑). The executor is independent of the resident session, so
	// this runs whether or not a session is live. Best-effort + non-wedging:
	// SpawnExecutor logs every failure and the wake is always acked.
	if rt != nil && rt.HasExecutor() {
		_, _ = rt.SpawnExecutor(ctx, agentruntime.SpawnRequest{TaskID: pl.TaskID})
		return nil
	}

	// Defensive: a concurrency-enabled agent reaching this point with NO executor
	// engine means its engine was not (re)attached on the current bring-up — it would
	// silently fall through to the single-active nudge below and degrade concurrency
	// without a trace. reattachExecutorEngineFromCache is called on every relaunch, so
	// this should not happen; if it does, re-attach now (last-ditch self-repair) and
	// log loudly so the regression is visible instead of silent.
	if cfg, ok := c.cachedExecConfig(pl.AgentID); ok && concurrencyEnabled(cfg) {
		c.log("work_available agent=%s: concurrency-enabled but no executor engine attached — re-attaching (degradation guard)", pl.AgentID)
		c.reattachExecutorEngineFromCache(ctx, pl.AgentID)
		c.mu.Lock()
		rt = c.runtimeForLocked(pl.AgentID)
		c.mu.Unlock()
		if rt != nil && rt.HasExecutor() {
			_, _ = rt.SpawnExecutor(ctx, agentruntime.SpawnRequest{TaskID: pl.TaskID})
			return nil
		}
	}

	// T335: a queued WorkItem arrived but this agent has NO live session
	// (crashed-and-circuit-broken, idle-downed, or otherwise dead). The old code
	// silently ACK-dropped the wake here, so a down agent's dispatched work stayed
	// queued FOREVER: dispatch emits only agent.work_available — never a paired
	// reconcile(running) — and nothing else pulls up a dead session on new work (the
	// only session-starters are boot-reconcile and onExit self-heal, neither of which
	// a wake triggers). Instead, drive the SAME proven per-agent boot-reconcile for
	// THIS agent (probe local supervisor × center desired state → reattach/relaunch)
	// so the dead session comes back; its pull loop then drains the queued WorkItems
	// (the agent's system prompt list_my_tasks-on-boot), self-healing the dropped
	// wake. Run BEFORE the coalesce dedup so a re-emitted wake keeps retrying the
	// bring-up while the agent is down (the dedup only suppresses a redundant nudge
	// on a LIVE session). Best-effort + non-wedging: always ack (return nil) so the
	// single-cursor control loop is never blocked behind a down agent.
	//
	// SAFETY (single-goroutine startSession invariant): workAvailable is invoked only
	// from the ControlLoop executor goroutine — the same goroutine that runs OnTick
	// self-heal and boot-reconcile — so reusing the session-start path here is never
	// concurrent with another startSession (ControlLoop §-1 CONCURRENCY).
	if sess == nil {
		c.relaunchForWake(ctx, pl.AgentID, pl.TaskID)
		return nil
	}

	// Live session: coalesce per work_item_id (mirror wake dedup) so reemit/flap/
	// replay don't spam nudges. A coalesced re-emit is a silent no-op.
	if !c.recordWorkAvail(pl.AgentID, pl.TaskID) {
		return nil
	}
	// v2.8.1 #278 D PR4a: NUDGE the agent to run its pull loop. The loop
	// instructions live in the agent's persistent system prompt
	// (claudestream.AgentWorkQueueSystemPrompt), so this is just a short wake — the
	// agent reacts per its system prompt (finish current task, then list_my_tasks →
	// start_task the next item).
	if err := sess.Inject(ctx, workAvailableNudge); err != nil {
		// Benign: the work stays queued; the agent pulls on its next loop. Log + ack.
		c.log("work_available agent=%s nudge inject: %v", pl.AgentID, err)
	}
	return nil
}

// relaunchForWake brings a DOWN agent's session back up when an agent.work_available
// wake arrives but no live session exists (T335 — "派了不起跑"). It reuses the
// per-agent boot-reconcile path (probe local supervisor × center desired state →
// reattach a survivor / relaunch a dead one) so a queued WorkItem can never sit
// forever behind a dead session. Best-effort and NON-WEDGING: every failure is
// logged, never returned — the caller acks the wake regardless so the single-cursor
// control loop is never blocked.
//
// Single-thread: invoked only from workAvailable, which runs on the ControlLoop
// executor goroutine (the same single-threaded caller that boot-reconcile and OnTick
// self-heal use), so the reused session-start path is never concurrent with another
// startSession.
func (c *AgentController) relaunchForWake(ctx context.Context, agentID, taskID string) {
	if c.cfg.Resumer == nil {
		// No resumer wired (dormant / pre-cutover / unit tests without a Resumer) → we
		// cannot learn the agent's desired state, so fall back to the legacy behavior:
		// leave the item queued; the agent pulls it on its next daemon-boot reconcile.
		c.log("work_available agent=%s work_item=%s — no running session and no resumer; queued, agent pulls on next boot", agentID, taskID)
		return
	}
	state, err := c.cfg.Resumer.ResumeState(ctx, c.cfg.WorkerID)
	if err != nil {
		c.log("work_available agent=%s relaunch: resume-state worker=%s: %v — left queued", agentID, c.cfg.WorkerID, err)
		return
	}
	for _, ra := range state.Agents {
		if strings.TrimSpace(ra.AgentID) != agentID {
			continue
		}
		if ra.DesiredLifecycle != "running" {
			// The center does NOT want this agent running (stopped/stopping/error) — a
			// queued WorkItem under a non-running agent is the rollback/reset path's job,
			// not ours. Leave it; do not resurrect a deliberately-stopped agent.
			c.log("work_available agent=%s desired=%s — not relaunching (queued)", agentID, ra.DesiredLifecycle)
			return
		}
		c.log("work_available agent=%s work_item=%s — no running session; relaunching to drain queued work", agentID, taskID)
		c.reconcileAgentOnBoot(ctx, agentID, toCenterRecord(ra), ra.Version)
		return
	}
	// The center's resume-state has no record of this agent on this worker — nothing
	// to relaunch against (a stale/cross-worker wake). Leave it queued.
	c.log("work_available agent=%s — no center record in resume-state; left queued", agentID)
}

// workAvailableNudge is the short wake injected on agent.work_available (v2.8.1
// #278 D PR4a). The full pull-loop behavior is the persistent system prompt; this
// only nudges the agent to run that loop when new work arrives.
const workAvailableNudge = "📥 New work is available in your queue. When you reach a stopping point on your current task, run your work loop: call list_my_tasks, then start_task the next item. (Need a tool you don't see — e.g. get_issue to read a task's spec from its source issue? It's deferred, not missing: run search_tools first before assuming it's absent or blocking.)"

// agentCLIMarkerFile is the per-agent-home file recording the agent's execution
// cli, written at codex start so boot-recovery can route the agent to the right
// path WITHOUT re-deriving the cli from the center (which does not carry it on the
// boot resume-set). Absent → the claude/supervisor path (the default).
const agentCLIMarkerFile = "agent.cli"

// readAgentCLIMarker returns the persisted cli for an agent home, or "" if no
// marker exists (the claude/supervisor default).
func readAgentCLIMarker(home string) string {
	b, err := os.ReadFile(filepath.Join(home, agentCLIMarkerFile))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// recordVersion advances appliedVersion for agentID, creating a stub
// managedAgent (no session) if none exists. Used by stop/reset/no-op desired
// states so a later replay of the same version is recognised as already-applied.
func (c *AgentController) recordVersion(agentID string, version int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ma := c.agents[agentID]
	if ma == nil {
		ma = &managedAgent{agentID: agentID}
		c.agents[agentID] = ma
	}
	if version > ma.appliedVersion {
		ma.appliedVersion = version
	}
}

// ---------------------------------------------------------------------------
// Agent home layout + reset cleanup (containment-guarded).
// ---------------------------------------------------------------------------

// agentPaths resolves the per-agent home, tasksDir, and plansDir under the
// runtime home layout: AgentHomeBase/agents/{agent_id}/ with tasks/ and plans/
// subdirs. Returns (home, tasksDir, plansDir, error). Design §3: workspace is
// replaced by tasks/ + plans/.
//
// v2.7 #179 + #209: AgentHomeBase is ALREADY worker-scoped — it resolves to the
// worker state dir <sqlite_dir> (= <prefix>/workers/<wid>/var), so the per-agent
// home is the FLAT <prefix>/workers/<wid>/var/agents/<aid>/. #179 removed a
// re-appended "workers/<wid>" double-nesting here; #209 removed the "agent-homes"
// wrapper that used to sit between var/ and agents/ (both redundant segments that
// also helped overflow the macOS sun_path limit, see #178). The layout MUST stay
// in lockstep with boot_reconcile's home scan (same base, same join) or
// boot-reconcile can't find supervisors → reattach breaks.
func (c *AgentController) agentPaths(agentID string) (home, tasksDir, plansDir string, err error) {
	if strings.TrimSpace(c.cfg.AgentHomeBase) == "" {
		return "", "", "", errors.New("agent_controller: agent_home_base required")
	}
	if strings.TrimSpace(c.cfg.WorkerID) == "" {
		return "", "", "", errors.New("agent_controller: worker_id required")
	}
	if strings.TrimSpace(agentID) == "" {
		return "", "", "", errors.New("agent_controller: agent_id required")
	}
	home = filepath.Join(c.cfg.AgentHomeBase, "agents", agentID)
	tasksDir = filepath.Join(home, "tasks")
	plansDir = filepath.Join(home, "plans")
	return home, tasksDir, plansDir, nil
}

// cleanReset wipes the dirs implied by resetScope UNDER the agent home. STRICT
// containment: every target is resolved + clean'd and must sit inside the agent
// home (home itself or home + os.PathSeparator), mirroring the file_transfer
// containment guard. A target that would escape is REFUSED (returned as an
// error; nothing is deleted).
//
// Scopes (ADR-0049 reset_scope):
//   - "memory"            → wipe <home>/memory
//   - "workspace"         → wipe <home>/tasks + <home>/plans (design §3.1)
//   - "all" / "" (default) → wipe memory + tasks + plans + session.instance (design §3.1)
func (c *AgentController) cleanReset(agentID, resetScope string) error {
	home, tasksDir, plansDir, err := c.agentPaths(agentID)
	if err != nil {
		return err
	}
	memory := filepath.Join(home, "memory")

	var targets []string
	switch strings.ToLower(strings.TrimSpace(resetScope)) {
	case "memory":
		targets = []string{memory}
	case "workspace":
		// Design §3.1: workspace resets tasks/ + plans/
		targets = []string{tasksDir, plansDir}
	case "", "all":
		targets = []string{memory, tasksDir, plansDir}
	default:
		c.log("reset agent=%s unknown scope=%q — defaulting to all", agentID, resetScope)
		targets = []string{memory, tasksDir, plansDir}
	}

	for _, t := range targets {
		if err := c.wipeContained(home, t); err != nil {
			return err
		}
	}

	// Design §3.1: "all" scope also removes session.instance (lease file).
	if resetScope == "" || strings.EqualFold(resetScope, "all") {
		instPath := filepath.Join(home, sessioninstance.InstanceFileName)
		if rmErr := os.Remove(instPath); rmErr != nil && !os.IsNotExist(rmErr) {
			c.log("reset agent=%s remove session.instance: %v", agentID, rmErr)
		}
	}

	c.log("reset agent=%s scope=%q wiped %d dir(s) under %s", agentID, resetScope, len(targets), home)
	return nil
}

// wipeContained removes target only if it is strictly contained under home
// (home itself is never removed). Containment is checked on the cleaned absolute
// paths via filepath.Rel — a result starting with ".." (or "..") escapes and is
// REFUSED. This is the hard reset-containment guard (never delete outside the
// agent home).
func (c *AgentController) wipeContained(home, target string) error {
	absHome, err := filepath.Abs(home)
	if err != nil {
		return fmt.Errorf("agent_controller: abs home: %w", err)
	}
	absHome = filepath.Clean(absHome)

	absTarget, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("agent_controller: abs target: %w", err)
	}
	absTarget = filepath.Clean(absTarget)

	// Refuse home itself (a reset must wipe a SUBDIR, never the whole home).
	if absTarget == absHome {
		return fmt.Errorf("agent_controller: refusing to wipe agent home itself %q", absHome)
	}
	rel, err := filepath.Rel(absHome, absTarget)
	if err != nil {
		return fmt.Errorf("agent_controller: rel %q under %q: %w", absTarget, absHome, err)
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("agent_controller: reset target %q escapes agent home %q (refused)", absTarget, absHome)
	}

	if err := os.RemoveAll(absTarget); err != nil {
		return fmt.Errorf("agent_controller: wipe %q: %w", absTarget, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Shutdown.
// ---------------------------------------------------------------------------

// Shutdown is the daemon-shutdown SURVIVAL path (s3b-2b, the @oopslink red-line):
// a worker-daemon stop/restart must NOT kill the agents' claude processes. It
// DETACHES every live session — closes the supervisor socket WITHOUT signalling —
// so the supervisor + claude keep running, owned by init, ready for the next
// daemon to re-attach. It reports NO lifecycle feedback (the agents remain
// desired-running). Each session is marked `detaching` first so its OnExit(nil)
// is recognised as a survival detach, NOT a crash. No goroutine leaks: each
// Detach joins the session's event-pump.
func (c *AgentController) Shutdown(ctx context.Context) {
	c.mu.Lock()
	rts := make([]*agentruntime.LocalRuntime, 0, len(c.agents))
	for _, ma := range c.agents {
		if ma.runtime != nil && ma.live() {
			rts = append(rts, ma.runtime)
		}
	}
	c.mu.Unlock()

	for _, rt := range rts {
		// Detach marks the session detaching (so its OnExit(nil) is a survival
		// detach, not a crash) then closes the socket without signalling; claude
		// survives, and Detach joins the pump.
		rt.Detach()
	}
	// Detach joined every event-pump above, so no NEW clean-turn goroutine can be
	// spawned past this point (no further c.bg.Add). Drain the ones already in
	// flight (e.g. MarkCompletedTurn writing session.instance) so a caller that
	// removes the agent home right after Shutdown — every t.TempDir()-based test —
	// never races a lingering home write (T672). Production: these are fast local
	// atomic writes, so the drain adds no meaningful shutdown latency.
	c.bg.Wait()
}

// Stop is an alias for Shutdown using a background context (convenience).
func (c *AgentController) Stop() { c.Shutdown(context.Background()) }
