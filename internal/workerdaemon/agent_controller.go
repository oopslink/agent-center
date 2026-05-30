// Package workerdaemon: AgentController is the v2.7 D2-c-ii-B control-command
// executor. It drives the long-lived ClaudeSession primitive (claude_session.go,
// D2-c-ii-A) in response to declarative control commands pulled by the
// ControlLoop, and reports RESULT feedback to the center via the D2-c-i
// /admin/environment/agent/* endpoints (the feedbackReporter seam).
//
// It implements CommandHandler (control_loop.go). It is PURELY ADDITIVE and
// stays DORMANT until D2-f: the control loop only runs when
// RuntimeConfig.ControlClient != nil, which D2-f sets. D2-c-ii-B wires this as
// RuntimeConfig.ControlHandler but leaves ControlClient nil — so the daemon's
// observable behaviour is unchanged.
//
// Command dispatch (see Handle):
//   - "agent.reconcile" → reconcile the real process to the desired lifecycle
//     (start / stop / reset), keyed by a monotonic version for replay safety.
//   - "agent.work"      → inject the work brief into the running session +
//     report the WorkItem active.
//   - unknown           → log + return nil (never wedge the ack cursor).
//
// Idempotency: returning nil from Handle advances the cumulative ack cursor;
// returning an error keeps the command un-acked so the ControlLoop re-pulls it
// next tick. The controller therefore returns nil for "already applied" replays
// (no-op) and reserves errors for genuinely transient failures it WANTS retried.
//
// Testability: the controller never touches a real claude binary — it accepts an
// injectable procLauncher (default execLauncher) handed straight to
// StartClaudeSession, so tests inject the c-ii-A fakeProc/fakeLauncher.
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

	"github.com/oopslink/agent-center/internal/mcphost"
)

// Command types (mirror the projector constants — kept local so the controller
// does not import the Environment/PM service packages).
const (
	cmdTypeAgentReconcile = "agent.reconcile"
	cmdTypeAgentWork      = "agent.work"
	cmdTypeAgentWake      = "agent.wake"
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
type wakePayload struct {
	AgentID     string `json:"agent_id"`
	WorkItemID  string `json:"work_item_id"`
	TaskRef     string `json:"task_ref"`
	MessageID   string `json:"message_id"`
	MessageText string `json:"message_text"`
}

// AgentControllerConfig parameterises the controller.
type AgentControllerConfig struct {
	// Reporter posts RESULT feedback to the center. Required.
	Reporter feedbackReporter
	// Launcher starts the claude process. Nil → execLauncher (production).
	Launcher procLauncher
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
	// StopGrace is the graceful-stop window forwarded to each ClaudeSession.
	StopGrace time.Duration
}

// managedAgent tracks one live (or recently-live) agent process.
type managedAgent struct {
	agentID string
	session *ClaudeSession

	// appliedVersion is the highest reconcile version applied. A reconcile with
	// version <= appliedVersion is a replay → no-op (no restart).
	appliedVersion int

	// expectedStop records that an intentional Stop (stopped/stopping/resetting)
	// is in progress, so the session's OnExit does NOT also report a crash. The
	// reconcile/reset flow is then the sole lifecycle reporter.
	expectedStop bool

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
}

// AgentController implements CommandHandler. State is a map of agentID →
// managedAgent guarded by mu. Safe for the single-threaded ControlLoop caller;
// the mutex also guards against the session OnExit/OnEvent callbacks (which run
// on the session's reader goroutine) mutating shared state concurrently.
type AgentController struct {
	cfg AgentControllerConfig

	mu     sync.Mutex
	agents map[string]*managedAgent
}

// compile-time: AgentController is a CommandHandler.
var _ CommandHandler = (*AgentController)(nil)

// NewAgentController constructs the controller. Reporter is required; a nil
// Launcher defaults to the production execLauncher.
func NewAgentController(cfg AgentControllerConfig) (*AgentController, error) {
	if cfg.Reporter == nil {
		return nil, errors.New("agent_controller: reporter required")
	}
	if cfg.Launcher == nil {
		cfg.Launcher = execLauncher{}
	}
	if cfg.Logger == nil {
		cfg.Logger = func(string) {}
	}
	if strings.TrimSpace(cfg.BinaryPath) == "" {
		cfg.BinaryPath = "agent-center"
	}
	return &AgentController{
		cfg:    cfg,
		agents: map[string]*managedAgent{},
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
	c.mu.Lock()
	ma := c.agents[pl.AgentID]
	hasLive := ma != nil && ma.session != nil
	c.mu.Unlock()

	if hasLive {
		c.log("reconcile agent=%s running version-bump=%d — restarting", pl.AgentID, pl.Version)
		// Restart: stop the old instance (expected stop, but we are about to
		// replace it so we suppress the lifecycle report — a restart should NOT
		// emit a "stopped" feedback that would settle the agent's lifecycle).
		c.stopSession(ctx, pl.AgentID, true /*graceful*/, false /*reportLifecycle*/)
	}

	if err := c.startSession(ctx, pl.AgentID, pl.Version); err != nil {
		// Start failure IS retryable (transient FS / launch error) — return the
		// error so the command stays un-acked and is retried next tick.
		return fmt.Errorf("agent_controller: start agent=%s: %w", pl.AgentID, err)
	}
	return nil
}

// reconcileStop stops the session and reports lifecycle "stopped" exactly once.
func (c *AgentController) reconcileStop(ctx context.Context, pl reconcilePayload) error {
	c.recordVersion(pl.AgentID, pl.Version)
	c.stopSession(ctx, pl.AgentID, true /*graceful*/, true /*reportLifecycle*/)
	return nil
}

// reconcileReset stops the session, wipes the per-scope dirs under the agent
// home (STRICTLY contained), then reports lifecycle "stopped". Does NOT auto-
// restart (the next intent change drives a fresh start).
func (c *AgentController) reconcileReset(ctx context.Context, pl reconcilePayload) error {
	c.recordVersion(pl.AgentID, pl.Version)
	// Stop first (expected) — but defer the lifecycle report until AFTER the
	// cleanup so a clean is not reported as stopped before the wipe runs.
	c.stopSession(ctx, pl.AgentID, true /*graceful*/, false /*reportLifecycle*/)

	if err := c.cleanReset(pl.AgentID, pl.ResetScope); err != nil {
		// A containment violation / FS error: log it but still settle the
		// lifecycle (the process IS stopped). Returning an error here would just
		// retry the (now process-less) reset forever; the cleanup is best-effort.
		c.log("reset agent=%s scope=%q cleanup: %v", pl.AgentID, pl.ResetScope, err)
	}

	// Settle resetting → stopped via the lifecycle feedback (MarkAgentStopped).
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
	var sess *ClaudeSession
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
	var sess *ClaudeSession
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

	if pl.WorkItemID != "" {
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

// startSession generates the per-agent mcp-config, resolves the agent home +
// workspace, and starts a ClaudeSession wiring OnEvent→activity and
// OnExit→crash/clean coordination. Records the applied version on success.
func (c *AgentController) startSession(ctx context.Context, agentID string, version int) error {
	home, workspace, err := c.agentPaths(agentID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(workspace, 0o700); err != nil {
		return fmt.Errorf("agent_controller: mkdir workspace: %w", err)
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

	// Reserve the managedAgent BEFORE launch so OnEvent/OnExit (which fire on the
	// reader goroutine the instant the process speaks) find their entry. The
	// session field is filled in after StartClaudeSession returns.
	ma := &managedAgent{agentID: agentID, appliedVersion: version}
	c.mu.Lock()
	c.agents[agentID] = ma
	c.mu.Unlock()

	sess, err := StartClaudeSession(ctx, ClaudeSessionConfig{
		AgentID:        agentID,
		HomeDir:        home,
		WorkspaceDir:   workspace,
		Launcher:       c.cfg.Launcher,
		MCPConfigBytes: mcpBytes,
		Binary:         c.cfg.ClaudeBinary,
		StopGrace:      c.cfg.StopGrace,
		Logger:         c.cfg.Logger,
		OnEvent: func(ev StreamEvent) {
			c.onEvent(agentID, ev)
		},
		OnExit: func(exitErr error) {
			c.onExit(agentID, exitErr)
		},
	})
	if err != nil {
		// Launch failed: roll back the reservation so a retry starts clean.
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
	c.log("started agent=%s version=%d home=%s", agentID, version, home)
	return nil
}

// stopSession stops the live session for agentID (if any). When reportLifecycle
// is true the stop flow is the SOLE lifecycle reporter and emits "stopped" once
// (guarded by the per-instance sync.Once). expectedStop is always set so the
// session's OnExit does NOT report a crash for this intentional stop.
func (c *AgentController) stopSession(ctx context.Context, agentID string, graceful, reportLifecycle bool) {
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

	// Stop blocks until the reader goroutine joins + OnExit fired (no leak).
	if err := sess.Stop(ctx, graceful); err != nil {
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
	payload, err := json.Marshal(streamActivityPayload(ev))
	if err != nil {
		c.log("activity agent=%s marshal event: %v", agentID, err)
		return
	}
	if err := c.cfg.Reporter.ReportAgentActivity(
		context.Background(), agentID, ev.Type, string(payload),
		"" /*workItemRef*/, "" /*interactionRef*/, time.Now(),
	); err != nil {
		c.log("activity agent=%s report: %v", agentID, err)
	}
}

// streamActivityPayload builds the JSON activity payload for a StreamEvent,
// emitting only the fields relevant to its Type so the activity record is a
// meaningful, compact object (omitempty drops the rest).
func streamActivityPayload(ev StreamEvent) map[string]any {
	p := map[string]any{"type": ev.Type}
	switch ev.Type {
	case "assistant_text", "thinking":
		p["text"] = ev.Text
	case "tool_use":
		p["tool_name"] = ev.ToolName
		p["tool_use_id"] = ev.ToolUseID
		if len(ev.ToolInput) > 0 {
			p["tool_input"] = ev.ToolInput
		}
	case "tool_result":
		p["tool_use_id"] = ev.ToolUseID
		if len(ev.ToolResult) > 0 {
			p["tool_result"] = ev.ToolResult
		}
	case "system":
		p["subtype"] = ev.Subtype
	case "result":
		p["subtype"] = ev.Subtype
		p["result"] = ev.Result
		p["stop_reason"] = ev.StopReason
		p["cost_usd"] = ev.CostUSD
		p["tokens_in"] = ev.TokensIn
		p["tokens_out"] = ev.TokensOut
	}
	if len(ev.Raw) > 0 {
		p["raw"] = ev.Raw
	}
	return p
}

// onExit coordinates the EXACTLY-ONE lifecycle report on process exit:
//   - expected stop (set by stopSession/reset) → the reconcile/reset flow is the
//     sole reporter; OnExit does NOT report (the sync.Once may already be spent,
//     and even if not, an expected stop's lifecycle is owned by the stop flow).
//   - unexpected (process crashed while desired=running) → OnExit reports
//     "error" with the exit err, exactly once.
//
// The managedAgent entry is cleared on exit so a fresh start re-creates it.
func (c *AgentController) onExit(agentID string, exitErr error) {
	c.mu.Lock()
	ma := c.agents[agentID]
	if ma == nil {
		c.mu.Unlock()
		return
	}
	expected := ma.expectedStop
	// Clear the entry: the process is gone.
	delete(c.agents, agentID)
	c.mu.Unlock()

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
	ma.lifecycleOnce.Do(func() {
		if err := c.cfg.Reporter.ReportAgentLifecycle(context.Background(), agentID, "error", msg, time.Now()); err != nil {
			c.log("agent=%s report error: %v", agentID, err)
		}
	})
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

// agentPaths resolves the per-agent home + workspace under the C1 OQ7 runtime
// home layout: AgentHomeBase/workers/{worker_id}/agents/{agent_id}/ with a
// workspace/ subdir. Returns (home, workspace, error).
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
	home = filepath.Join(c.cfg.AgentHomeBase, "workers", c.cfg.WorkerID, "agents", agentID)
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

// Shutdown stops all live sessions for a clean daemon shutdown. It does NOT
// report lifecycle feedback (a daemon shutdown is not an agent-intent stop — the
// agents remain desired-running and will be reconciled on the next daemon boot).
// No goroutine leaks: each session's Stop joins its reader goroutine.
func (c *AgentController) Shutdown(ctx context.Context) {
	c.mu.Lock()
	sessions := make([]*ClaudeSession, 0, len(c.agents))
	for _, ma := range c.agents {
		if ma.session != nil {
			ma.expectedStop = true
			sessions = append(sessions, ma.session)
		}
	}
	c.mu.Unlock()

	for _, s := range sessions {
		if err := s.Stop(ctx, true); err != nil {
			c.log("shutdown stop: %v", err)
		}
	}
}

// Stop is an alias for Shutdown using a background context (convenience).
func (c *AgentController) Stop() { c.Shutdown(context.Background()) }
