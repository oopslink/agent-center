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

	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/mcphost"
	"github.com/oopslink/agent-center/internal/supervisormanager"
)

// agentSession is the NARROW control surface the AgentController needs from one
// agent's session (v2.7 D2-f s3b-2b). The whole point of the supervisor model is
// that the SUPERVISOR solely owns claude, so this interface exposes only socket-
// mediated control — Inject (→ claude stdin), Stop (terminate the supervisor),
// Detach (daemon-shutdown SURVIVAL: drop the socket, keep claude alive) — never a
// process handle the controller could exec/kill directly.
//
// 🔴 OWNERSHIP INVARIANT (PM s3b-2b condition): PRODUCTION code wires ONLY the real
// *SupervisorSession (via startSupervisorSessionAdapter → StartSupervisorSession →
// supervisormanager.SpawnSupervisor; claude's parent is the supervisor, never the
// daemon). The interface exists so controller LOGIC (reconcile/work/wake/onExit
// three-state) is unit-testable with a lightweight fake — but that fake lives ONLY
// in _test.go and MUST NEVER appear in a production path. The interface is NOT a
// backdoor to direct-exec claude: grep-clean ownership = no direct claude exec on
// any production path; the test fake is a test artifact, not a session.
type agentSession interface {
	// Inject writes msg to claude's held-open stdin over the supervisor socket.
	// Returns ErrSessionClosed once Stop/Detach has begun.
	Inject(ctx context.Context, msg string) error
	// Stop is the EXPLICIT-terminate path: SIGTERM the supervisor (which stops
	// claude + exits), then join the event-pump. Fires OnExit exactly once.
	Stop(ctx context.Context) error
	// Detach is the daemon-shutdown SURVIVAL path: close the socket WITHOUT
	// signalling, so the supervisor + claude keep running for a future re-attach.
	// Fires OnExit(nil) exactly once.
	Detach()
}

// compile-time: the real *SupervisorSession is an agentSession (the ONLY
// production impl). A test fake also satisfies it but lives in _test.go.
var _ agentSession = (*SupervisorSession)(nil)

// sessionStarter is the factory the controller uses to start a session. Production
// = startSupervisorSessionAdapter (real supervisor spawn). Tests inject a fake
// starter that returns a fake agentSession (controller-logic unit tests, no real
// spawn). Injected via the unexported AgentControllerConfig.starter field, which
// only same-package _test.go can set — so production ALWAYS gets the real adapter.
type sessionStarter func(ctx context.Context, cfg SupervisorSessionConfig) (agentSession, error)

// startSupervisorSessionAdapter is the PRODUCTION session starter: it spawns the
// real persistent supervisor (which solely owns claude). The explicit nil-on-error
// return avoids the typed-nil-interface gotcha (a nil *SupervisorSession wrapped in
// a non-nil agentSession).
func startSupervisorSessionAdapter(ctx context.Context, cfg SupervisorSessionConfig) (agentSession, error) {
	s, err := StartSupervisorSession(ctx, cfg)
	if err != nil {
		return nil, err
	}
	return s, nil
}

// Command types (mirror the projector constants — kept local so the controller
// does not import the Environment/PM service packages).
const (
	cmdTypeAgentReconcile = "agent.reconcile"
	cmdTypeAgentWork      = "agent.work"
	cmdTypeAgentWake      = "agent.wake"
	cmdTypeAgentConverse  = "agent.converse" // v2.7 #185: DM/channel message → inject (no WorkItem)
	cmdTypeWorkAvailable  = "agent.work_available" // v2.8.1 #278 D pull-model WAKE (PR2 emit / PR3 handle)
)

// wakeDedupCap bounds the per-agent set of already-injected wake message IDs
// (D2-e-i Q3 dedup). At-least-once delivery + reconnect replay can re-deliver the
// same agent.wake command; the cap keeps a recent window so a replay within it is
// recognised and NOT re-injected. The window only needs to outlast a reconnect
// burst — older IDs evicting is acceptable (a very stale replay would at worst
// re-inject once). The set is also dropped entirely on session restart (the
// managedAgent is recreated), which is fine: a fresh session has no prior context
// to duplicate against.
const wakeDedupCap = 256

// mcpServerName is the `mcpServers` map key for the per-agent worker mcp-host
// server in the generated --mcp-config document.
const mcpServerName = "agent-center"

// reconcilePayload decodes an "agent.reconcile" command payload. Matches
// internal/environment/service/agent_control_projector.go reconcileCommandPayload.
type reconcilePayload struct {
	AgentID          string `json:"agent_id"`
	DesiredLifecycle string `json:"desired_lifecycle"`
	Model            string `json:"model,omitempty"`
	Version          int    `json:"version"`
	ResetScope       string `json:"reset_scope,omitempty"`
}

// workPayload decodes an "agent.work" command payload. Matches
// internal/projectmanager/service/work_item_projector.go workCommandPayload.
type workPayload struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
	TaskRef    string `json:"task_ref"`
	Brief      string `json:"brief"`
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
	WorkItemID     string `json:"work_item_id"`
	TaskRef        string `json:"task_ref"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	MessageText    string `json:"message_text"`
}

// workAvailablePayload decodes an "agent.work_available" (wake) command. Matches
// the projectors' workAvailablePayload (pm WorkItemProjector + env
// AgentControlProjector). v2.8.1 #278 D pull model: a per-agent "you have new
// work — pull your queue" signal. PR3 only DEDUPS (per work_item_id, mirroring
// wake message dedup) + logs + acks — the actual session inject ("check your
// queue") + the agent's pull-loop land together in PR4. WorkItemID is the
// per-WI idempotency/dedup key.
type workAvailablePayload struct {
	AgentID    string `json:"agent_id"`
	WorkItemID string `json:"work_item_id"`
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
}

// AgentControllerConfig parameterises the controller.
type AgentControllerConfig struct {
	// Reporter posts RESULT feedback to the center. Required.
	Reporter feedbackReporter
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
	// AgentHomeBase is the runtime home root. Per-agent home resolves to
	// AgentHomeBase/workers/{worker_id}/agents/{agent_id}/ (C1 OQ7 layout).
	// Required for start/reset (mcp-config + workspace live under it).
	AgentHomeBase string
	// Logger receives one-line ops messages. Nil → silent.
	Logger func(msg string)
	// StopGrace is the graceful-stop window forwarded to each SupervisorSession
	// (Stop → StopSupervisor SIGTERM grace).
	StopGrace time.Duration

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

	// starter is the session factory (test seam, PM s3b-2b). Unexported so ONLY
	// same-package _test.go can override it with a fake — production callers cannot
	// set it, so NewAgentController always defaults it to the real supervisor-spawn
	// adapter. This is the test seam that keeps the controller LOGIC unit-testable
	// without a real spawn while guaranteeing production wires only the real
	// *SupervisorSession (grep-clean ownership).
	starter sessionStarter
}

// managedAgent tracks one live (or recently-live) agent session (backed by a
// persistent supervisor in s3b-2b).
type managedAgent struct {
	agentID string
	session agentSession

	// appliedVersion is the highest reconcile version applied. A reconcile with
	// version <= appliedVersion is a replay → no-op (no restart).
	appliedVersion int

	// expectedStop records that an intentional Stop (stopped/stopping/resetting)
	// is in progress, so the session's OnExit does NOT also report a crash. The
	// reconcile/reset flow is then the sole lifecycle reporter.
	expectedStop bool

	// detaching records that a daemon-shutdown SURVIVAL detach is in progress
	// (s3b-2b). Detach closes the socket WITHOUT killing claude and fires
	// OnExit(nil); without this flag onExit would mis-report that nil exit as a
	// CRASH ("error") on every clean shutdown. detaching → onExit only logs and
	// reports NOTHING (the agent stays desired-running; its supervisor + claude
	// survive for the next daemon's s4 re-attach). SAFETY (PM): this transient
	// signal cannot mask a REAL crash — if claude actually died during shutdown,
	// the next daemon-boot s4 probe (pidfile kill-0) detects the dead process and
	// drives the mode-B relaunch. Truth is recomputed by boot-probe, not this flag.
	detaching bool

	// lifecycleOnce guards the lifecycle RESULT report for this process instance
	// so it fires EXACTLY ONCE per stop, whether the reporter is the reconcile
	// flow (expected stop) or OnExit (crash).
	lifecycleOnce sync.Once

	// wakeSeen is the bounded per-agent set of wake message IDs already injected
	// (D2-e-i Q3 dedup). wakeOrder is the insertion order used for FIFO eviction
	// when the set exceeds wakeDedupCap. Guarded by AgentController.mu. Recreated
	// (empty) on session restart along with the managedAgent.
	wakeSeen  map[string]struct{}
	wakeOrder []string

	// workAvailSeen is the bounded per-agent set of agent.work_available
	// work_item_ids already noted (v2.8.1 #278 D PR3 coalesce). The wake fires
	// per-WI at two emit points (enqueue + reemit-on-running) + flap/reconnect
	// replay, so this dedup collapses the re-emits so the daemon does not spam
	// (and, in PR4, does not re-inject the pull nudge). FIFO eviction at
	// wakeDedupCap; recreated empty on session restart. Guarded by AgentController.mu.
	workAvailSeen  map[string]struct{}
	workAvailOrder []string

	// hadWork records that work was INJECTED into this session (a WorkItem went
	// active). On an unexpected crash it drives the self-heal relaunch nudge (re-
	// drive the interrupted turn); an idle agent that crashes relaunches without a
	// nudge. Guarded by AgentController.mu.
	hadWork bool

	// currentWorkItemID is the LAST WorkItem injected into this session (work/wake),
	// used by the L2 no-silent-failure surface: when claude emits a `result` event
	// with is_error=true, onEvent fails THIS WorkItem (active→failed) so a failed
	// turn never sits silently "active". Guarded by AgentController.mu; cleared
	// (empty) on session restart with the managedAgent.
	//
	// 🕒 DEFERRED-WITH-TRIGGER (PM): this is the "last injected" WI, NOT a precise
	// per-turn correlation. The result event is delivered async by the session pump
	// (~50ms lag); if a SECOND work() injects before the first turn's result is
	// pumped, the result is mis-attributed — and the race is two-sided: result(A)
	// charged to B both wrongly fails B AND leaves A silently active (A's failure
	// never surfaces). v2.7 injects sequentially with low/﹦1 max_concurrent, so the
	// window is effectively unreachable. TRIGGER: max_concurrent>1 OR an observed
	// mis-attribution → add precise correlation (a turn-seq/token claude echoes back,
	// since the result line carries no WorkItem id). (CHANGELOG + Tester §A.)
	currentWorkItemID string

	// currentConversationID is the conversation of the LAST agent.converse inject
	// (a DM/channel turn, which has NO WorkItem). It is the converse analogue of
	// currentWorkItemID for the L2 no-silent-failure surface: when a converse turn
	// ends is_error (e.g. an invalid model → claude 404), onEvent posts a visible
	// "couldn't process the message" SYSTEM message into this conversation instead
	// of leaving the human in a silent black hole (UX Rule 9). currentWorkItemID
	// and currentConversationID are mutually exclusive — whichever context was
	// injected last is set, the other cleared. Same "last injected" imprecision +
	// deferred-with-trigger caveat as currentWorkItemID. Guarded by mu.
	currentConversationID string

	// toolNames correlates a claude tool_use_id → tool_name within a turn (v2.7.1
	// #216): the claude tool_result event carries only the tool_use_id, but the
	// Activity stream wants the tool_name on the tool_result row. Populated on each
	// tool_use, read on the matching tool_result. Reset at session-init / result so
	// it never grows unbounded across turns. Guarded by mu.
	toolNames map[string]string

	// model is the agent's configured claude --model (Profile.Model, threaded from the
	// reconcile command). Held here so a mid-run self-heal relaunch — which gets NO
	// fresh reconcile and deletes this managedAgent on crash — can carry it across the
	// crash via selfHealEntry.model and spawn the re-driven claude with the SAME model
	// (else self-heal would silently fall back to claude's default). Guarded by mu.
	model string
}

// AgentController implements CommandHandler. State is a map of agentID →
// managedAgent guarded by mu. Safe for the single-threaded ControlLoop caller;
// the mutex also guards against the session OnExit/OnEvent callbacks (which run
// on the session's reader goroutine) mutating shared state concurrently.
type AgentController struct {
	cfg AgentControllerConfig

	mu     sync.Mutex
	agents map[string]*managedAgent
	// selfHeal tracks mid-run crash recovery per agent (backoff/cap/terminal). It
	// SURVIVES the managedAgent delete on crash. Guarded by mu. See self_heal.go.
	selfHeal map[string]*selfHealEntry
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
	if cfg.Logger == nil {
		cfg.Logger = func(string) {}
	}
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		cfg.BinaryPath = "agent-center"
	}
	return &AgentController{
		cfg:      cfg,
		agents:   map[string]*managedAgent{},
		selfHeal: map[string]*selfHealEntry{},
	}, nil
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
		return c.work(ctx, pl)
	case cmdTypeAgentWake:
		var pl wakePayload
		if err := json.Unmarshal([]byte(cmd.Payload), &pl); err != nil {
			c.log("wake decode (offset=%d): %v — skipping", cmd.Offset, err)
			return nil
		}
		return c.wake(ctx, pl)
	case cmdTypeAgentConverse:
		var pl conversePayload
		if err := json.Unmarshal([]byte(cmd.Payload), &pl); err != nil {
			c.log("converse decode (offset=%d): %v — skipping", cmd.Offset, err)
			return nil
		}
		return c.converse(ctx, pl)
	case cmdTypeWorkAvailable:
		var pl workAvailablePayload
		if err := json.Unmarshal([]byte(cmd.Payload), &pl); err != nil {
			c.log("work_available decode (offset=%d): %v — skipping", cmd.Offset, err)
			return nil
		}
		return c.workAvailable(ctx, pl)
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
	hasLive := ma != nil && ma.session != nil
	c.mu.Unlock()

	if hasLive {
		c.log("reconcile agent=%s running version-bump=%d — restarting", pl.AgentID, pl.Version)
		// Restart: stop the old instance (expected stop, but we are about to
		// replace it so we suppress the lifecycle report — a restart should NOT
		// emit a "stopped" feedback that would settle the agent's lifecycle).
		c.stopSession(ctx, pl.AgentID, false /*reportLifecycle*/)
	}

	// forkResume=false: an intent-driven start/restart is NOT a crash recovery. A
	// fresh start has no prior session; a restart stop-SIGTERMed the old claude
	// (lock released), so a plain resume of the same session-id is correct. Only the
	// Mode-B crash-relaunch paths (bootReapRelaunch) fork (the killed claude's lock
	// is still held).
	if err := c.startSession(ctx, pl.AgentID, pl.Version, false /*forkResume*/, false /*resume*/, pl.Model); err != nil {
		// Start failure IS retryable (transient FS / launch error) — return the
		// error so the command stays un-acked and is retried next tick.
		return fmt.Errorf("agent_controller: start agent=%s: %w", pl.AgentID, err)
	}
	return nil
}

// reconcileStop stops the session and reports lifecycle "stopped" exactly once.
func (c *AgentController) reconcileStop(ctx context.Context, pl reconcilePayload) error {
	c.clearSelfHeal(pl.AgentID) // desired-stopped → no self-heal relaunch
	c.recordVersion(pl.AgentID, pl.Version)
	c.stopSession(ctx, pl.AgentID, true /*reportLifecycle*/)
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

	home, _, err := c.agentPaths(pl.AgentID)
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
	c.stopSession(ctx, pl.AgentID, false /*reportLifecycle*/)

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
func (c *AgentController) work(ctx context.Context, pl workPayload) error {
	if strings.TrimSpace(pl.AgentID) == "" {
		c.log("work missing agent_id — skipping")
		return nil
	}

	c.mu.Lock()
	ma := c.agents[pl.AgentID]
	var sess agentSession
	if ma != nil {
		sess = ma.session
	}
	c.mu.Unlock()

	if sess == nil {
		// No running session yet — retry after the reconcile(running) lands.
		return fmt.Errorf("agent_controller: work for agent=%s but no running session (retry after reconcile)", pl.AgentID)
	}

	if err := sess.Inject(ctx, pl.Brief); err != nil {
		if errors.Is(err, ErrSessionClosed) {
			// The session closed between the lookup and the inject — retry.
			return fmt.Errorf("agent_controller: inject agent=%s: %w", pl.AgentID, err)
		}
		return fmt.Errorf("agent_controller: inject agent=%s: %w", pl.AgentID, err)
	}

	// Work was injected (a WorkItem is now active) — mark it so an unexpected crash
	// self-heal relaunches WITH a resume nudge (re-drive the interrupted turn), and
	// record it as the in-flight WorkItem so an is_error turn surfaces against it (L2).
	c.mu.Lock()
	if cur := c.agents[pl.AgentID]; cur != nil {
		cur.hadWork = true
		if pl.WorkItemID != "" {
			cur.currentWorkItemID = pl.WorkItemID
			cur.currentConversationID = "" // work context supersedes any converse context
		}
	}
	c.mu.Unlock()

	if pl.WorkItemID != "" {
		if err := c.cfg.Reporter.ReportWorkItemState(ctx, pl.AgentID, pl.WorkItemID, "active", time.Now()); err != nil {
			// The brief was already injected; a feedback failure is transient.
			// Returning an error would re-inject the brief on retry (double work),
			// so we log + ack instead. The WorkItem state will be reconciled by a
			// later activity/feedback in D2-g if needed.
			c.log("work agent=%s report active: %v", pl.AgentID, err)
		}
	}
	return nil
}

// wake injects a message posted into the agent's TASK conversation into the
// agent's long-lived claude session and reports the WorkItem active (the OQ5
// immediate-wakeup path, waiting_input→active). It mirrors work()'s session
// lookup + no-session policy for consistency: if there is NO running session, it
// returns an ERROR so the ControlLoop re-pulls the wake next tick (the reconcile
// that starts the session bumped its version first). We never silently drop a
// wake, and we never start an un-reconciled session.
//
// Dedup (Q3): a per-agent bounded set of already-injected message IDs absorbs
// at-least-once redelivery + reconnect replay — a wake whose message_id was
// already injected for this agent is a no-op (return nil). The id is recorded
// ONLY after a successful inject (at-least-once: a failed inject retries; we
// prefer a rare duplicate over a dropped wake).
func (c *AgentController) wake(ctx context.Context, pl wakePayload) error {
	if strings.TrimSpace(pl.AgentID) == "" {
		c.log("wake missing agent_id — skipping")
		return nil
	}

	c.mu.Lock()
	ma := c.agents[pl.AgentID]
	var sess agentSession
	if ma != nil {
		sess = ma.session
		// Dedup check under the lock: a replay of an already-injected message_id
		// is a no-op.
		if pl.MessageID != "" && ma.wakeSeen != nil {
			if _, seen := ma.wakeSeen[pl.MessageID]; seen {
				c.mu.Unlock()
				c.log("wake agent=%s message=%s already injected — dedup no-op", pl.AgentID, pl.MessageID)
				return nil
			}
		}
	}
	c.mu.Unlock()

	if sess == nil {
		// No running session yet — retry after the reconcile(running) lands
		// (same policy as work()).
		return fmt.Errorf("agent_controller: wake for agent=%s but no running session (retry after reconcile)", pl.AgentID)
	}

	if err := sess.Inject(ctx, pl.MessageText); err != nil {
		// Session closed between lookup and inject (or write failed) — retry so the
		// wake is not lost (do NOT record dedup, since nothing was injected).
		return fmt.Errorf("agent_controller: wake inject agent=%s: %w", pl.AgentID, err)
	}

	// Record the message_id as injected (dedup) only after a successful inject.
	c.recordWake(pl.AgentID, pl.MessageID)

	// D2-e-ii (OQ5 method 甲): advance the agent participant's read-state cursor
	// to the newest delivered message so the NEXT batch flush (request_input →
	// agent.awaiting_input) does not re-deliver what was already injected here.
	// This applies to BOTH the e-i immediate wake (single message) and the e-ii
	// batch flush. Best-effort: a mark-seen failure is logged, not fatal (the
	// FIFO dedup set already guards crash-replay; the cursor is the batch boundary
	// and a stale cursor at worst re-delivers a duplicate the model can ignore).
	if pl.ConversationID != "" && pl.MessageID != "" {
		if err := c.cfg.Reporter.ReportMarkSeen(ctx, pl.AgentID, pl.ConversationID, pl.MessageID, time.Now()); err != nil {
			c.log("wake agent=%s mark-seen conv=%s msg=%s: %v", pl.AgentID, pl.ConversationID, pl.MessageID, err)
		}
	}

	if pl.WorkItemID != "" {
		// Record as the in-flight WorkItem so an is_error turn surfaces against it (L2).
		c.mu.Lock()
		if cur := c.agents[pl.AgentID]; cur != nil {
			cur.currentWorkItemID = pl.WorkItemID
			cur.currentConversationID = "" // work context supersedes any converse context
		}
		c.mu.Unlock()
		// waiting_input→active via the existing feedback endpoint (MarkWorkItemState
		// active drives the WorkItem AR's move to active, the Wake transition). A
		// report failure is transient: the message is already injected, so re-running
		// would double-inject — log + ack instead (mirrors work()'s policy).
		if err := c.cfg.Reporter.ReportWorkItemState(ctx, pl.AgentID, pl.WorkItemID, "active", time.Now()); err != nil {
			c.log("wake agent=%s report active: %v", pl.AgentID, err)
		}
	}
	return nil
}

// converse handles an agent.converse command (v2.7 #185): a DM/channel message
// from a human, injected into the agent's running session WITHOUT a WorkItem.
// It mirrors wake()'s session lookup + dedup + mark-seen, but builds a
// context-rich brief (who/where + how to reply) and never touches a WorkItem.
func (c *AgentController) converse(ctx context.Context, pl conversePayload) error {
	if strings.TrimSpace(pl.AgentID) == "" {
		c.log("converse missing agent_id — skipping")
		return nil
	}

	c.mu.Lock()
	ma := c.agents[pl.AgentID]
	var sess agentSession
	if ma != nil {
		sess = ma.session
		// Dedup: a replay of an already-injected message is a no-op (reuses the
		// same per-agent wakeSeen FIFO set, keyed by conversation message id).
		if pl.MessageID != "" && ma.wakeSeen != nil {
			if _, seen := ma.wakeSeen[pl.MessageID]; seen {
				c.mu.Unlock()
				c.log("converse agent=%s message=%s already injected — dedup no-op", pl.AgentID, pl.MessageID)
				return nil
			}
		}
	}
	c.mu.Unlock()

	if sess == nil {
		// Not running (shouldn't normally reach here — the projector only enqueues
		// converse for running agents + posts a system notice otherwise). Retry so
		// a converse racing a just-started session is not lost.
		return fmt.Errorf("agent_controller: converse for agent=%s but no running session (retry after reconcile)", pl.AgentID)
	}

	if err := sess.Inject(ctx, buildConverseBrief(pl)); err != nil {
		return fmt.Errorf("agent_controller: converse inject agent=%s: %w", pl.AgentID, err)
	}
	c.recordWake(pl.AgentID, pl.MessageID)

	// L2 no-silent-failure (converse): record this as the in-flight CONVERSATION
	// (no WorkItem) so an is_error turn surfaces a system message into it. Clear
	// any stale WorkItem context — a converse turn is not work.
	c.mu.Lock()
	if cur := c.agents[pl.AgentID]; cur != nil {
		cur.currentConversationID = pl.ConversationID
		cur.currentWorkItemID = ""
	}
	c.mu.Unlock()

	// Advance the agent's read-state cursor so a later batch flush doesn't
	// re-deliver this message. Best-effort (dedup set already guards replay).
	if pl.ConversationID != "" && pl.MessageID != "" {
		if err := c.cfg.Reporter.ReportMarkSeen(ctx, pl.AgentID, pl.ConversationID, pl.MessageID, time.Now()); err != nil {
			c.log("converse agent=%s mark-seen conv=%s msg=%s: %v", pl.AgentID, pl.ConversationID, pl.MessageID, err)
		}
	}
	return nil
}

// buildConverseBrief renders the stdin brief injected for an agent.converse: who
// messaged, where (DM vs channel), the text, and how to reply (post_message with
// the conversation_id). Kept deterministic + plain so claude can act on it.
func buildConverseBrief(pl conversePayload) string {
	sender := strings.TrimSpace(pl.SenderDisplay)
	if sender == "" {
		sender = strings.TrimSpace(pl.SenderRef)
	}
	var header string
	if pl.ConvKind == "channel" {
		where := strings.TrimSpace(pl.ConvName)
		if where == "" {
			where = "a channel"
		}
		header = fmt.Sprintf("[Channel #%s] %s mentioned you:", where, sender)
	} else {
		header = fmt.Sprintf("[Direct message from %s]:", sender)
	}
	return fmt.Sprintf("%s\n%s\n\n(To reply, use the post_message tool with conversation_id=%q. This is a conversation, not a task — there is no work item to complete.)",
		header, pl.MessageText, pl.ConversationID)
}

// recordWake records messageID in the agent's bounded wake-dedup set, evicting
// the oldest entry FIFO when the set exceeds wakeDedupCap. Creates a stub
// managedAgent if none exists (defensive; wake() only reaches here with a live
// session, so an entry normally exists).
func (c *AgentController) recordWake(agentID, messageID string) {
	if messageID == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	ma := c.agents[agentID]
	if ma == nil {
		ma = &managedAgent{agentID: agentID}
		c.agents[agentID] = ma
	}
	if ma.wakeSeen == nil {
		ma.wakeSeen = make(map[string]struct{}, wakeDedupCap)
	}
	if _, ok := ma.wakeSeen[messageID]; ok {
		return
	}
	ma.wakeSeen[messageID] = struct{}{}
	ma.wakeOrder = append(ma.wakeOrder, messageID)
	for len(ma.wakeOrder) > wakeDedupCap {
		oldest := ma.wakeOrder[0]
		ma.wakeOrder = ma.wakeOrder[1:]
		delete(ma.wakeSeen, oldest)
	}
}

// recordWorkAvail notes a work_item_id under the per-agent agent.work_available
// coalesce set (v2.8.1 #278 D PR3). Returns true if NEWLY recorded, false if it
// was already seen (a coalesced re-emit/flap/replay). Mirrors recordWake (lazy
// managedAgent create + FIFO eviction at wakeDedupCap).
func (c *AgentController) recordWorkAvail(agentID, workItemID string) bool {
	if workItemID == "" {
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
	if _, ok := ma.workAvailSeen[workItemID]; ok {
		return false
	}
	ma.workAvailSeen[workItemID] = struct{}{}
	ma.workAvailOrder = append(ma.workAvailOrder, workItemID)
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
func (c *AgentController) workAvailable(_ context.Context, pl workAvailablePayload) error {
	if strings.TrimSpace(pl.AgentID) == "" {
		c.log("work_available missing agent_id — skipping")
		return nil
	}
	if c.recordWorkAvail(pl.AgentID, pl.WorkItemID) {
		c.log("work_available agent=%s work_item=%s — noted (pull nudge deferred to PR4; old push still drives)",
			pl.AgentID, pl.WorkItemID)
	}
	// else: coalesced re-emit/replay — silent no-op.
	return nil
}

// startSession generates the per-agent mcp-config (written to a FILE the
// supervisor reads by path — minimal key surface), resolves the agent home +
// workspace + DURABLE reset epoch, and starts a SupervisorSession (via the
// injected starter — production = real supervisor spawn) wiring OnEvent→activity
// and OnExit→three-state coordination. Records the applied version on success.
//
// OWNERSHIP: this method NEVER execs claude. It spawns ONLY the supervisor (the
// starter → StartSupervisorSession → SpawnSupervisor); the supervisor solely owns
// claude. grep-clean: no exec.Command(claude…) on this path.
func (c *AgentController) startSession(ctx context.Context, agentID string, version int, forkResume, resume bool, model string) error {
	home, workspace, err := c.agentPaths(agentID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return fmt.Errorf("agent_controller: mkdir workspace: %w", err)
	}
	// v2.7 #182: claude runs with cwd=workspace and `--setting-sources user,project`,
	// so its "project" settings source resolves to <workspace>/.claude. Create the
	// directory as the agent's own project-config load point. It is left EMPTY —
	// agent-center never pre-fills settings/hooks here (no indirect pollution); the
	// agent (or a future feature) may drop a settings.json into it.
	if err := os.MkdirAll(filepath.Join(workspace, ".claude"), 0o700); err != nil {
		return fmt.Errorf("agent_controller: mkdir workspace/.claude: %w", err)
	}

	mcpBytes, err := mcphost.GenerateMCPConfig(mcphost.MCPConfigParams{
		ServerName:        mcpServerName,
		Command:           c.cfg.BinaryPath,
		Args:              []string{"worker", "mcp-host"},
		AgentID:           agentID,
		AdminURL:          c.cfg.AdminURL,
		WorkerToken:       c.cfg.WorkerToken,
		ServerFingerprint: c.cfg.ServerFingerprint,
		AgentRoot:         workspace,
	})
	if err != nil {
		return fmt.Errorf("agent_controller: generate mcp-config: %w", err)
	}
	// Write the mcp-config to a file under the agent home; the supervisor receives
	// only the PATH (--mcp-config-path), never the token-bearing bytes.
	mcpPath, err := writeMCPConfig(home, mcpBytes)
	if err != nil {
		return fmt.Errorf("agent_controller: write mcp-config: %w", err)
	}

	// Resolve the DURABLE reset epoch. A normal start / crash-relaunch reads the
	// CURRENT epoch (NOT 0) so it re-derives the SAME claude session-id and resumes
	// the conversation; only a reset (BumpEpochForReset) advances it. A CORRUPT
	// epoch file is surfaced as an error — we must NOT spawn at epoch 0 and
	// silently start a fresh session (the context-loss trap).
	epochState, err := supervisormanager.ReadEpoch(home)
	if err != nil {
		return fmt.Errorf("agent_controller: read epoch agent=%s: %w", agentID, err)
	}

	// Mode-B crash-relaunch FORK (v2.7 GATE-7): a hard-killed claude never releases
	// its session-id lock, so re-deriving the SAME id would hit "Session ID already
	// in use" and fail to boot (the A-seg ship-blocker). On a fork relaunch we ALWAYS
	// bump+persist the generation BEFORE spawn (per-attempt monotonic; persist-first
	// so a daemon death between bump and spawn never re-collides on the next boot-
	// reconcile) and spawn a FRESH never-locked next-gen id — this lock-avoidance is
	// unconditional for every relaunch (idle OR with-work).
	//
	// FINDING-3 (#117 part B): whether that fresh session RESUMES the prior session's
	// conversation is now decoupled from the gen-bump and gated on `resume` (==hadWork
	// — there is in-flight active work to recover):
	//   - resume==true  (hadWork): emit `--resume <prevGenId> --fork-session` so claude
	//     forks the prior conversation into the new id — the A-seg verified mid-turn
	//     resume, BYTE-IDENTICAL to before.
	//   - resume==false (idle/no-work): leave resumeFrom EMPTY → the new id starts a
	//     FRESH claude session (no --resume/--fork-session). This avoids the crash-loop
	//     where `--resume` of a no-completed-turn session makes claude immediately emit
	//     is_error subtype=error_during_execution (the IDLE-agent self-heal terminal
	//     trap). Continuity note: idle→fresh intentionally drops prior session history,
	//     which is acceptable for a session with no in-flight turn to recover.
	//
	// A NORMAL start (forkResume=false) keeps the current generation and does NOT fork —
	// a clean stop released the lock, so a plain resume of the same id is correct (and
	// the initial start is generation 0). The gen=0 byte-equivalence is untouched.
	generation := epochState.Generation
	resumeFrom := ""
	if forkResume {
		if resume {
			resumeFrom = claudestream.SessionUUIDGen(agentID, epochState.Epoch, epochState.Generation)
		}
		bumped, berr := supervisormanager.BumpGenerationForRelaunch(home)
		if berr != nil {
			return fmt.Errorf("agent_controller: bump generation agent=%s: %w", agentID, berr)
		}
		generation = bumped.Generation
	}

	// Reserve the managedAgent BEFORE launch so OnEvent/OnExit (which fire on the
	// event-pump goroutine the instant the process speaks) find their entry. The
	// session field is filled in after the starter returns.
	ma := &managedAgent{agentID: agentID, appliedVersion: version, model: model}
	c.mu.Lock()
	c.agents[agentID] = ma
	c.mu.Unlock()

	sess, err := c.cfg.starter(ctx, SupervisorSessionConfig{
		AgentID:             agentID,
		HomeDir:             home,
		MCPConfigPath:       mcpPath,
		WorkspaceDir:        workspace,
		BinaryPath:          c.cfg.BinaryPath,
		ClaudeBin:           c.cfg.ClaudeBinary,
		Model:               model,
		Epoch:               epochState.Epoch,
		Generation:          generation,
		ResumeFromSessionID: resumeFrom,
		StopGrace:           c.cfg.StopGrace,
		Logger:              c.cfg.Logger,
		OnEvent: func(ev claudestream.StreamEvent) {
			c.onEvent(agentID, ev)
		},
		OnExit: func(exitErr error) {
			c.onExit(agentID, exitErr)
		},
	})
	if err != nil {
		// Spawn failed: roll back the reservation so a retry starts clean.
		c.mu.Lock()
		if c.agents[agentID] == ma {
			delete(c.agents, agentID)
		}
		c.mu.Unlock()
		return fmt.Errorf("agent_controller: start session: %w", err)
	}

	c.mu.Lock()
	ma.session = sess
	c.mu.Unlock()
	c.log("started agent=%s version=%d epoch=%d generation=%d fork=%v resume=%v home=%s", agentID, version, epochState.Epoch, generation, forkResume, resume, home)
	return nil
}

// stopSession stops the live session for agentID (if any) — the EXPLICIT-terminate
// path: SIGTERM the supervisor (which stops claude + exits). When reportLifecycle
// is true the stop flow is the SOLE lifecycle reporter and emits "stopped" once
// (guarded by the per-instance sync.Once). expectedStop is always set so the
// session's OnExit does NOT report a crash for this intentional stop.
func (c *AgentController) stopSession(ctx context.Context, agentID string, reportLifecycle bool) {
	c.mu.Lock()
	ma := c.agents[agentID]
	if ma == nil || ma.session == nil {
		c.mu.Unlock()
		if reportLifecycle {
			// No live process, but the desired state is stopped — settle it.
			c.reportLifecycleOnce(ctx, agentID, "stopped", "")
		}
		return
	}
	ma.expectedStop = true
	sess := ma.session
	c.mu.Unlock()

	// Stop blocks until the event-pump joins + OnExit fired (no leak).
	if err := sess.Stop(ctx); err != nil {
		c.log("stop agent=%s: %v", agentID, err)
	}

	if reportLifecycle {
		c.reportLifecycleOnce(ctx, agentID, "stopped", "")
	}
}

// onEvent is the stdout→activity sink: it maps a parsed StreamEvent (the D2
// claude 2.1.156 stream-json event) to a ReportAgentActivity call. event_type =
// StreamEvent.Type ("assistant_text" | "thinking" | "tool_use" | "tool_result"
// | "system" | "result" | "rate_limit" | "unknown"); payload = the StreamEvent
// marshaled to a JSON object. It NEVER posts to a Conversation (only the
// activity endpoint). Best-effort: a feedback failure is logged, not fatal.
func (c *AgentController) onEvent(agentID string, ev StreamEvent) {
	// Stamp the in-flight WorkItem onto the activity so the observability
	// projection can aggregate tool_calls / tokens / current_activity per
	// work-item (#111: previously hardcoded ""). Read under the lock, mirroring
	// surfaceTurnFailure — this callback runs on the session reader goroutine and
	// the agents map + currentWorkItemID are mutex-guarded. Empty when idle (no
	// in-flight work), which is the pre-#111 behaviour.
	c.mu.Lock()
	var workItemRef, toolName string
	if ma := c.agents[agentID]; ma != nil {
		workItemRef = ma.currentWorkItemID
		// v2.7.1 #216: maintain the per-turn tool_use_id→tool_name correlation so the
		// tool_result activity can carry the tool_name (the claude tool_result event
		// only has the id). Reset at turn boundaries (system-init / result).
		switch ev.Type {
		case "tool_use":
			if ev.ToolUseID != "" {
				if ma.toolNames == nil {
					ma.toolNames = map[string]string{}
				}
				ma.toolNames[ev.ToolUseID] = ev.ToolName
			}
		case "tool_result":
			toolName = ma.toolNames[ev.ToolUseID]
		case "system", "result":
			ma.toolNames = nil
		}
	}
	c.mu.Unlock()

	payload, err := json.Marshal(streamActivityPayload(ev, toolName))
	if err != nil {
		c.log("activity agent=%s marshal event: %v", agentID, err)
		// Still attempt the L2 failure surface below — the activity record is
		// best-effort, but a failed turn must not be swallowed by a marshal error.
	} else if err := c.cfg.Reporter.ReportAgentActivity(
		context.Background(), agentID, activityEventType(ev), string(payload),
		workItemRef, "" /*interactionRef*/, time.Now(),
	); err != nil {
		c.log("activity agent=%s report: %v", agentID, err)
	}

	// L2 no-silent-failure: a `result` event with is_error=true means the turn
	// ENDED in failure (API/auth error, max_turns, …). The in-flight WorkItem was
	// reported "active" at inject and would otherwise sit silently active forever
	// (task stuck "running"). Surface it by failing the WorkItem (active→failed via
	// the NORMAL feedback edge — the agent is still alive, only its turn failed; NOT
	// the B3 agent-death cascade, which is for a crashed/result-less claude). The
	// failure detail is preserved in the result activity above (is_error/subtype/result).
	if ev.Type == "result" && ev.IsError {
		c.surfaceTurnFailure(agentID, ev)
	}
}

// surfaceTurnFailure fails the agent's in-flight WorkItem after an is_error turn
// (L2). It reads currentWorkItemID under the lock, then reports the failure
// outside the lock (the reporter is a network call). With no in-flight WorkItem
// (an is_error turn on an idle agent — e.g. an unsolicited error) it logs a
// VISIBLE warning rather than silently dropping it. On success it clears the
// in-flight pointer so a stray second result cannot re-fail an already-failed WI.
func (c *AgentController) surfaceTurnFailure(agentID string, ev StreamEvent) {
	c.mu.Lock()
	var wiID, convID string
	if ma := c.agents[agentID]; ma != nil {
		wiID = ma.currentWorkItemID
		convID = ma.currentConversationID
	}
	c.mu.Unlock()

	if wiID == "" {
		// No WorkItem. If the in-flight context is a CONVERSATION (agent.converse),
		// surface the failure as a VISIBLE system message in that conversation (UX
		// Rule 9 — a DM/channel turn that errored, e.g. invalid model → claude 404,
		// must not leave the human waiting in silence). Otherwise (truly idle) log.
		if convID != "" {
			c.surfaceConverseFailure(agentID, convID, ev)
			return
		}
		c.log("L2 agent=%s is_error turn with NO in-flight WorkItem (subtype=%q) — surfaced as warning, not silently dropped", agentID, ev.Subtype)
		return
	}

	if err := c.cfg.Reporter.ReportWorkItemState(
		context.Background(), agentID, wiID, "failed", time.Now(),
	); err != nil {
		// Non-silent: a report failure is logged loudly. The next activity/feedback
		// reconcile (D2-g) can still observe the failed turn in the activity stream.
		c.log("L2 agent=%s work_item=%s report failed: %v", agentID, wiID, err)
		return
	}
	c.log("L2 agent=%s work_item=%s failed (is_error turn, subtype=%q)", agentID, wiID, ev.Subtype)

	// Clear the in-flight pointer: this WorkItem is no longer active.
	c.mu.Lock()
	if ma := c.agents[agentID]; ma != nil && ma.currentWorkItemID == wiID {
		ma.currentWorkItemID = ""
	}
	c.mu.Unlock()
}

// surfaceConverseFailure posts a VISIBLE system message into the conversation of
// a failed agent.converse turn (no WorkItem) so the human sees the agent errored
// instead of waiting in silence (UX Rule 9 / #185 follow-up). Best-effort: a
// report failure is logged (the is_error is still in the activity stream). Clears
// the in-flight conversation pointer so a stray second result doesn't double-post.
func (c *AgentController) surfaceConverseFailure(agentID, convID string, ev StreamEvent) {
	if err := c.cfg.Reporter.ReportConverseError(
		context.Background(), agentID, convID, converseErrorSummary(ev), time.Now(),
	); err != nil {
		c.log("L2 agent=%s conv=%s converse-error report: %v", agentID, convID, err)
		return
	}
	c.log("L2 agent=%s conv=%s converse turn failed (is_error, subtype=%q) — system notice posted", agentID, convID, ev.Subtype)
	c.mu.Lock()
	if ma := c.agents[agentID]; ma != nil && ma.currentConversationID == convID {
		ma.currentConversationID = ""
	}
	c.mu.Unlock()
}

// converseErrorSummary builds a short, human-readable failure summary from the
// result event for the conversation system message (subtype + a bounded slice of
// the result text). The full detail remains in the result activity record.
func converseErrorSummary(ev StreamEvent) string {
	s := strings.TrimSpace(ev.Subtype)
	if s == "" {
		s = "error"
	}
	if r := strings.TrimSpace(ev.Result); r != "" {
		const max = 200
		if len(r) > max {
			r = r[:max] + "…"
		}
		s = s + ": " + r
	}
	return s
}

// streamActivityPayload builds the JSON activity payload for a StreamEvent,
// emitting only the fields relevant to its Type so the activity record is a
// meaningful, compact object (omitempty drops the rest).
func streamActivityPayload(ev StreamEvent, toolName string) map[string]any {
	p := map[string]any{"type": ev.Type}
	switch ev.Type {
	case "assistant_text", "thinking":
		p["text"] = ev.Text
	case "tool_use":
		p["tool_name"] = ev.ToolName
		p["tool_use_id"] = ev.ToolUseID
		// v2.7.1 #216: `args` is the standardized field the Activity stream reads
		// (frontend truncates it for the tool_use preview). Keep tool_input too for
		// back-compat with any existing consumer.
		if len(ev.ToolInput) > 0 {
			p["args"] = ev.ToolInput
			p["tool_input"] = ev.ToolInput
		}
	case "tool_result":
		p["tool_use_id"] = ev.ToolUseID
		// v2.7.1 #216: tool_name correlated from the matching tool_use (the claude
		// tool_result event only carries the id). Omitted when unresolved.
		if toolName != "" {
			p["tool_name"] = toolName
		}
		// `ok` = NOT is_error, parsed from the claude tool_result block content
		// (which carries an is_error flag); defaults true when absent.
		p["ok"] = !toolResultIsError(ev.ToolResult)
		if len(ev.ToolResult) > 0 {
			p["tool_result"] = ev.ToolResult
		}
	case "system":
		p["subtype"] = ev.Subtype
		// v2.7.1 #216: the session-init system line carries {model, session_id,
		// mcp_servers} — surface them as standardized fields for the system_init
		// activity (parsed from the raw line; absent fields omitted).
		if ev.Subtype == "init" {
			mergeSystemInitFields(p, ev.Raw)
		}
	case "result":
		p["subtype"] = ev.Subtype
		p["result"] = ev.Result
		p["stop_reason"] = ev.StopReason
		p["is_error"] = ev.IsError
		p["cost_usd"] = ev.CostUSD
		p["tokens_in"] = ev.TokensIn
		p["tokens_out"] = ev.TokensOut
	}
	if len(ev.Raw) > 0 {
		p["raw"] = ev.Raw
	}
	return p
}

// activityEventType maps a StreamEvent to the standardized activity event_type
// (v2.7.1 #216). The claude session-init system line becomes "system_init"; every
// other type keeps its stream value (which already matches the agent BC's
// EventType* constants). The worker stays decoupled from the agent BC (§ 0.4) — it
// emits the event_type as a STRING; these literals MUST match agent.EventType*.
func activityEventType(ev StreamEvent) string {
	if ev.Type == "system" && ev.Subtype == "init" {
		return "system_init"
	}
	return ev.Type
}

// toolResultIsError reports whether a claude tool_result content block carries an
// is_error flag (v2.7.1 #216 → payload.ok = !is_error). Best-effort: empty /
// unparseable → false (treated as ok).
func toolResultIsError(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	var probe struct {
		IsError bool `json:"is_error"`
	}
	_ = json.Unmarshal(raw, &probe)
	return probe.IsError
}

// mergeSystemInitFields extracts {model, session_id, mcp_servers} from the raw
// claude system-init line into the system_init activity payload (v2.7.1 #216).
// Absent fields are omitted; a parse failure leaves p unchanged.
func mergeSystemInitFields(p map[string]any, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var probe struct {
		Model      string          `json:"model"`
		SessionID  string          `json:"session_id"`
		MCPServers json.RawMessage `json:"mcp_servers"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return
	}
	if probe.Model != "" {
		p["model"] = probe.Model
	}
	if probe.SessionID != "" {
		p["session_id"] = probe.SessionID
	}
	if len(probe.MCPServers) > 0 {
		p["mcp_servers"] = probe.MCPServers
	}
}

// onExit coordinates the EXACTLY-ONE lifecycle report on session exit, across the
// THREE exit kinds (s3b-2b):
//   - detaching (daemon-shutdown SURVIVAL) → Detach fired OnExit(nil) but claude
//     is STILL ALIVE under the supervisor for a future re-attach. Report NOTHING
//     (the agent stays desired-running); just log. SAFETY (PM): this cannot mask a
//     real crash — if claude actually died, the next daemon-boot s4 probe detects
//     it and drives mode-B relaunch. Truth is recomputed by boot-probe.
//   - expected stop (set by stopSession/reset) → the reconcile/reset flow is the
//     sole reporter; OnExit does NOT report (the sync.Once may already be spent,
//     and an expected stop's lifecycle is owned by the stop flow).
//   - unexpected (the supervisor/claude died while desired=running) → OnExit
//     reports "error" with the exit err, exactly once.
//
// The managedAgent entry is cleared on exit so a fresh start re-creates it.
func (c *AgentController) onExit(agentID string, exitErr error) {
	c.mu.Lock()
	ma := c.agents[agentID]
	if ma == nil {
		c.mu.Unlock()
		return
	}
	detaching := ma.detaching
	expected := ma.expectedStop
	version := ma.appliedVersion       // captured for a possible self-heal relaunch
	hadWork := ma.hadWork              // injected work → nudge on self-heal relaunch
	workItemID := ma.currentWorkItemID // in-flight WI → rebind on relaunch so a failed re-drive surfaces (L2×Mode-B)
	model := ma.model                  // agent's --model → carry across crash so self-heal re-drive uses the SAME model
	// Clear the entry: this daemon no longer tracks the session (on detach the
	// supervisor + claude survive, owned by init, for the next daemon's re-attach).
	delete(c.agents, agentID)
	c.mu.Unlock()

	if detaching {
		// Daemon-shutdown survival: claude lives on; report nothing.
		c.log("agent=%s detached (supervisor + claude survive for re-attach)", agentID)
		return
	}

	if expected {
		// The stop/reset flow owns the lifecycle report for this instance.
		c.log("agent=%s exited (expected stop)", agentID)
		return
	}

	// Unexpected crash while desired=running → report error exactly once.
	msg := ""
	if exitErr != nil {
		msg = exitErr.Error()
	} else {
		msg = "process exited unexpectedly"
	}
	c.log("agent=%s crashed: %s", agentID, msg)
	// Mid-run crash self-heal (GATE-7 Mode-B, slice B): record the crash + schedule a
	// backed-off relaunch (or circuit-break to terminal after the cap). This NEVER
	// starts a session — OnTick performs the relaunch on the ControlLoop goroutine. It
	// returns the lifecycle state to report: "error" (transient, still auto-retrying)
	// or "failed" (terminal circuit-breaker), reported once for this crash instance.
	state := c.recordCrashAndSchedule(agentID, version, hadWork, workItemID, model, msg)
	if state != "" {
		ma.lifecycleOnce.Do(func() {
			if err := c.cfg.Reporter.ReportAgentLifecycle(context.Background(), agentID, state, msg, time.Now()); err != nil {
				c.log("agent=%s report %s: %v", agentID, state, err)
			}
		})
	}
}

// reportLifecycleOnce emits a lifecycle RESULT exactly once per managed instance
// (guarded by the per-instance sync.Once). When the entry is already gone (the
// process exited first), it still emits — the once is then keyed to a transient
// managedAgent, which is acceptable for the no-process-settle path.
func (c *AgentController) reportLifecycleOnce(ctx context.Context, agentID, state, errMsg string) {
	c.mu.Lock()
	ma := c.agents[agentID]
	c.mu.Unlock()

	emit := func() {
		if err := c.cfg.Reporter.ReportAgentLifecycle(ctx, agentID, state, errMsg, time.Now()); err != nil {
			c.log("agent=%s report %s: %v", agentID, state, err)
		}
	}
	if ma != nil {
		ma.lifecycleOnce.Do(emit)
		return
	}
	emit()
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

// agentPaths resolves the per-agent home + workspace under the runtime home
// layout: AgentHomeBase/agents/{agent_id}/ with a workspace/ subdir. Returns
// (home, workspace, error).
//
// v2.7 #179 + #209: AgentHomeBase is ALREADY worker-scoped — it resolves to the
// worker state dir <sqlite_dir> (= <prefix>/workers/<wid>/var), so the per-agent
// home is the FLAT <prefix>/workers/<wid>/var/agents/<aid>/. #179 removed a
// re-appended "workers/<wid>" double-nesting here; #209 removed the "agent-homes"
// wrapper that used to sit between var/ and agents/ (both redundant segments that
// also helped overflow the macOS sun_path limit, see #178). The layout MUST stay
// in lockstep with boot_reconcile's home scan (same base, same join) or
// boot-reconcile can't find supervisors → reattach breaks.
func (c *AgentController) agentPaths(agentID string) (home, workspace string, err error) {
	if strings.TrimSpace(c.cfg.AgentHomeBase) == "" {
		return "", "", errors.New("agent_controller: agent_home_base required")
	}
	if strings.TrimSpace(c.cfg.WorkerID) == "" {
		return "", "", errors.New("agent_controller: worker_id required")
	}
	if strings.TrimSpace(agentID) == "" {
		return "", "", errors.New("agent_controller: agent_id required")
	}
	home = filepath.Join(c.cfg.AgentHomeBase, "agents", agentID)
	workspace = filepath.Join(home, "workspace")
	return home, workspace, nil
}

// cleanReset wipes the dirs implied by resetScope UNDER the agent home. STRICT
// containment: every target is resolved + clean'd and must sit inside the agent
// home (home itself or home + os.PathSeparator), mirroring the file_transfer
// containment guard. A target that would escape is REFUSED (returned as an
// error; nothing is deleted).
//
// Scopes (ADR-0049 reset_scope):
//   - "memory"            → wipe <home>/memory
//   - "workspace"         → wipe <home>/workspace
//   - "all" / "" (default) → wipe both memory + workspace
func (c *AgentController) cleanReset(agentID, resetScope string) error {
	home, workspace, err := c.agentPaths(agentID)
	if err != nil {
		return err
	}
	memory := filepath.Join(home, "memory")

	var targets []string
	switch strings.ToLower(strings.TrimSpace(resetScope)) {
	case "memory":
		targets = []string{memory}
	case "workspace":
		targets = []string{workspace}
	case "", "all":
		targets = []string{memory, workspace}
	default:
		c.log("reset agent=%s unknown scope=%q — defaulting to all", agentID, resetScope)
		targets = []string{memory, workspace}
	}

	for _, t := range targets {
		if err := c.wipeContained(home, t); err != nil {
			return err
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
	sessions := make([]agentSession, 0, len(c.agents))
	for _, ma := range c.agents {
		if ma.session != nil {
			ma.detaching = true
			sessions = append(sessions, ma.session)
		}
	}
	c.mu.Unlock()

	for _, s := range sessions {
		// Detach is no-signal + joins the pump; claude survives.
		s.Detach()
	}
}

// Stop is an alias for Shutdown using a background context (convenience).
func (c *AgentController) Stop() { c.Shutdown(context.Background()) }
