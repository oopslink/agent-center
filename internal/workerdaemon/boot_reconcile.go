package workerdaemon

// boot_reconcile.go is the v2.7 D2-f s4 boot-recovery DECISION core. When a worker
// daemon (re)starts with the control path active, it must reconcile every agent's
// REAL local state (did its persistent supervisor survive the daemon restart?)
// against the CENTER's desired state (should it be running? in-flight work?), and
// for each agent take exactly one action: re-attach the survivor, relaunch a dead
// one, stop+reap an unwanted one, or leave an idle one alone.
//
// decideBootAction is the PURE heart of that reconcile (s4a): it maps
// (local probe state × center record) → a single bootAction, with NO side effects,
// so the full decision matrix is exhaustively unit-testable. The ORCHESTRATION
// that enumerates agents, probes, locks, and executes the action lives in s4b.
//
// 🔴 EXHAUSTIVENESS (PM): the decision space is the FULL Cartesian product
//
//	probe  ∈ {Reattachable, Unavailable}
//	center ∈ {running+inflight, running+no-inflight, stopped, no-record}
//
// = 8 cells, each with an EXPLICIT action (no implicit fallthrough). The matrix:
//
//	                | running+inflight | running+idle  | stopped/stopping | no-record (orphan)
//	  Reattachable  | reattach         | reattach      | stop+reap        | stop+reap (orphan)
//	  Unavailable   | reap+relaunch    | reap+relaunch | reap-only        | reap-only (dead orphan)
//
// Key calls (PM-confirmed):
//   - reattach NEVER injects a nudge (claude is alive and mid-task; a nudge would
//     corrupt the in-flight turn). Only a RELAUNCH of an agent with an ACTIVE
//     WorkItem nudges (claude resumed the session-id but may need a push).
//   - desired==stopped/stopping WINS over any in-flight WorkItem (an orphan WI under
//     a stopped agent is the rollback/reset path's job, not boot-resume's).
//   - source set = center resume-set ∪ LOCAL home enumeration: a locally-alive
//     supervisor the center has NO record of is an orphan that must be stopped —
//     only the local enumeration surfaces it (the center never lists it).
//   - Unavailable + desired-running → reap+relaunch REGARDLESS of in-flight (v2.7
//     Mode-B self-heal at boot). A desired-running agent whose supervisor is dead
//     MUST have its session relaunched (resume via the durable epoch): if we noop'd
//     an idle one, an agent.work arriving later would dead-lock forever ("no running
//     session, retry after reconcile") because the original reconcile is already
//     acked and won't replay, and work never starts a session (start is a lifecycle
//     action). Nudge iff an ACTIVE WorkItem is in flight (re-drive an interrupted
//     turn); a freshly-arriving agent.work injects its own brief on delivery. (The
//     mid-run crash case — supervisor dies while the daemon stays UP — is handled by
//     the onExit self-heal state machine, not this boot path.)

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
	"github.com/oopslink/agent-center/internal/claudestream"
	"github.com/oopslink/agent-center/internal/supervisormanager"
)

// DefaultResumeNudge is the message injected into a RELAUNCHED agent's session for
// an ACTIVE WorkItem on boot (s4b), so the interrupted task continues. FLAG
// (GATE-7): claude --session-id resumes the conversation, but whether it
// auto-continues an interrupted turn or needs an explicit nudge is unknown until
// validated against real claude — this is the single, isolated spot to correct if
// GATE-7 finds a different nudge (or none) is required. Overridable via
// AgentControllerConfig.ResumeNudge.
const DefaultResumeNudge = "Resume your current task."

// bootReconciler is the optional interface the runtime type-asserts the
// ControlHandler against to invoke boot-resume SYNCHRONOUSLY before the control
// -loop poll goroutine starts (s4b). AgentController implements it; D1's
// NoopCommandHandler does not → it is skipped (additive). Defined here so the
// controller and the runtime wiring share one contract.
type bootReconciler interface {
	ReconcileOnBoot(ctx context.Context) error
}

// compile-time: AgentController is a bootReconciler.
var _ bootReconciler = (*AgentController)(nil)

// bootActionKind enumerates the mutually-exclusive boot actions.
type bootActionKind int

const (
	// bootNoop: do nothing — used ONLY for an UNKNOWN/uncategorised probe state (be
	// conservative; never relaunch on a state we can't classify). A desired-running
	// agent with a dead supervisor is NOT noop'd — it is reap+relaunched (Mode-B
	// self-heal at boot, even when idle), so a later agent.work can't dead-lock on a
	// missing session.
	bootNoop bootActionKind = iota
	// bootReattach: a live, compatible supervisor for a desired-running agent —
	// re-attach to it (resume event-pump from its durable offset). NEVER nudges.
	bootReattach
	// bootReapRelaunch: the supervisor is gone/incompatible but the agent is
	// desired-running WITH in-flight work — reap any residual, then relaunch a
	// fresh supervisor (which reads the DURABLE epoch, not 0). Nudge iff an ACTIVE
	// WorkItem is in flight.
	bootReapRelaunch
	// bootStopReap: a LIVE supervisor that must NOT keep running — either the agent
	// is desired-stopped, or it is a local orphan the center has no record of.
	// Stop the supervisor + reap residual.
	bootStopReap
	// bootReapOnly: the supervisor is already gone but residual may linger and the
	// agent must NOT run (desired-stopped, or a dead orphan). Reap residual; do not
	// relaunch.
	bootReapOnly
)

func (k bootActionKind) String() string {
	switch k {
	case bootNoop:
		return "noop"
	case bootReattach:
		return "reattach"
	case bootReapRelaunch:
		return "reap_relaunch"
	case bootStopReap:
		return "stop_reap"
	case bootReapOnly:
		return "reap_only"
	default:
		return "unknown"
	}
}

// bootAction is the decision for one agent: the kind + (relaunch-only) whether to
// inject the resume nudge.
type bootAction struct {
	Kind bootActionKind
	// Nudge is meaningful ONLY for bootReapRelaunch: inject the ResumeNudge because
	// an ACTIVE WorkItem is in flight (claude resumed the session-id but the
	// interrupted turn may need a push — GATE-7 validates). Always false for every
	// other kind (notably reattach, where claude is alive and a nudge would corrupt
	// the in-flight turn).
	Nudge bool
}

// centerRecord is the center's desired view of one agent for boot reconcile (the
// s4 projection of a ResumeAgent). A nil *centerRecord means the CENTER HAS NO
// RECORD of this agent — i.e. it surfaced only from the local home enumeration
// (an orphan).
type centerRecord struct {
	// DesiredLifecycle is the center's desired lifecycle ("running" | "stopped" |
	// "stopping" | ...).
	DesiredLifecycle string
	// HasInflight is true iff the agent has ≥1 in-flight WorkItem (active ∪
	// waiting_input) — the trigger that makes a dead desired-running agent worth
	// relaunching (vs left idle).
	HasInflight bool
	// HasActive is true iff the agent has ≥1 ACTIVE WorkItem — drives the relaunch
	// nudge.
	HasActive bool
}

// wantsRunning reports whether the center desires this agent running. Anything
// other than "running" (stopped/stopping/error/empty) is treated as NOT running;
// stopped/stopping in particular WIN over in-flight work.
func (r *centerRecord) wantsRunning() bool {
	return r != nil && r.DesiredLifecycle == "running"
}

// decideBootAction maps (local supervisor probe state × center record) → the one
// boot action for an agent. PURE: no side effects, no I/O — the whole 8-cell
// matrix is exhaustively unit-testable. rec == nil ⇒ the center has no record
// (orphan); otherwise rec carries the desired lifecycle + in-flight flags.
//
// The two probe states partition the matrix:
//   - Reattachable: a live, compatible supervisor exists locally.
//   - Unavailable:  no live+compatible supervisor (dead / missing / incompatible).
func decideBootAction(probe supervisormanager.ProbeState, rec *centerRecord) bootAction {
	switch probe {
	case supervisormanager.Reattachable:
		// A LIVE local supervisor exists.
		switch {
		case rec == nil:
			// Orphan: locally alive but the center has no record → stop+reap.
			return bootAction{Kind: bootStopReap}
		case rec.wantsRunning():
			// Desired-running + alive → re-attach (idle or busy; NEVER nudge —
			// claude is alive). Covers BOTH running+inflight and running+idle.
			return bootAction{Kind: bootReattach}
		default:
			// Desired-stopped/stopping (or any non-running) WINS → stop the live
			// supervisor + reap, regardless of any orphan in-flight WI.
			return bootAction{Kind: bootStopReap}
		}

	case supervisormanager.Unavailable:
		// NO live+compatible supervisor locally.
		switch {
		case rec == nil:
			// Dead orphan: no center record + nothing live → reap any residual.
			return bootAction{Kind: bootReapOnly}
		case rec.wantsRunning():
			// Desired-running but the local supervisor is dead → reap residual +
			// relaunch (Mode-B self-heal at boot). startSession resumes via the
			// DURABLE session.epoch → same session-id → continues context (not clean-
			// slate). Relaunch even when NO in-flight WI is reported: a desired-running
			// agent must have a live session, else an agent.work arriving later dead-
			// locks ("no running session, retry after reconcile") — the original
			// reconcile is already acked + won't replay, and work never starts a
			// session (start is a lifecycle action). Nudge iff an ACTIVE WorkItem is in
			// flight (re-drive an interrupted turn); a freshly-arriving agent.work
			// injects its own brief on delivery.
			return bootAction{Kind: bootReapRelaunch, Nudge: rec.HasActive}
		default:
			// Desired-stopped/stopping + already gone → reap any residual.
			return bootAction{Kind: bootReapOnly}
		}

	default:
		// Unknown probe state: be conservative — do nothing (never relaunch on an
		// uncategorised state).
		return bootAction{Kind: bootNoop}
	}
}

// ReconcileOnBoot reconciles every agent on this worker after a daemon (re)start,
// JOINING the center's desired state with each agent's LOCAL supervisor probe, and
// taking exactly one action per agent (reattach / relaunch / stop+reap / reap /
// noop). It preserves worker/agent lifecycle independence: a daemon restart
// re-attaches survivors (claude never interrupted) and only relaunches the truly
// dead.
//
// MUST run SYNCHRONOUSLY before the control-loop poll goroutine starts — the
// session-start/attach paths are only safe for the single-threaded ControlLoop
// caller (the runtime wiring enforces this via the bootReconciler hook).
//
// Source set = the center's resume-set ∪ a LOCAL home enumeration: a locally-alive
// supervisor the center has NO record of is an orphan that only the local scan can
// surface (the center never lists it). Each agent's whole decision+execution runs
// under its home lock so two daemons can't both reattach/relaunch it.
//
// Per-agent errors are logged and processing continues (one bad agent never stalls
// boot). Returns an error ONLY if the center resume-state query itself fails (the
// caller logs it; the daemon does NOT crash).
func (c *AgentController) ReconcileOnBoot(ctx context.Context) error {
	if c.cfg.Resumer == nil {
		return nil // no resumer wired → dormant (additive)
	}
	state, err := c.cfg.Resumer.ResumeState(ctx, c.cfg.WorkerID)
	if err != nil {
		return fmt.Errorf("agent_controller: boot resume-state worker=%s: %w", c.cfg.WorkerID, err)
	}

	// Center desired records + versions keyed by agent id.
	centerByID := make(map[string]*centerRecord, len(state.Agents))
	centerVersion := make(map[string]int, len(state.Agents))
	for _, ra := range state.Agents {
		id := strings.TrimSpace(ra.AgentID)
		if id == "" {
			continue
		}
		centerByID[id] = toCenterRecord(ra)
		centerVersion[id] = ra.Version
	}

	// Union the center set with the LOCAL home enumeration so orphans (locally
	// alive, no center record) are surfaced.
	union := make(map[string]struct{}, len(centerByID))
	for id := range centerByID {
		union[id] = struct{}{}
	}
	localIDs, lerr := c.enumerateLocalAgents()
	if lerr != nil {
		c.log("boot-reconcile: local enumeration: %v (continuing with center set only)", lerr)
	}
	for _, id := range localIDs {
		union[id] = struct{}{}
	}

	c.log("boot-reconcile: %d center agent(s) + %d local home(s) → %d to reconcile",
		len(centerByID), len(localIDs), len(union))
	for id := range union {
		c.reconcileAgentOnBoot(ctx, id, centerByID[id], centerVersion[id])
	}
	return nil
}

// toCenterRecord projects a resume-state ResumeAgent into the boot-decision
// centerRecord. The endpoint already filters WorkItems to in-flight (active ∪
// waiting_input), so any present ⇒ HasInflight; an ACTIVE one ⇒ HasActive (drives
// the relaunch nudge).
func toCenterRecord(ra ResumeAgent) *centerRecord {
	rec := &centerRecord{
		DesiredLifecycle: ra.DesiredLifecycle,
		HasInflight:      len(ra.WorkItems) > 0,
	}
	for _, wi := range ra.WorkItems {
		if wi.Status == string(workItemStatusActive) {
			rec.HasActive = true
			break
		}
	}
	return rec
}

// workItemStatusActive is the in-flight "active" status string (matches
// agent.WorkItemActive) — kept local so this package does not import the agent BC.
const workItemStatusActive = "active"

// enumerateLocalAgents scans this worker's agent-home root for homes that have a
// supervisor.instance file — i.e. a supervisor was started there and may have
// SURVIVED a daemon restart. These are the candidates the center may NOT list
// (orphans). A missing agents dir (fresh worker) yields no ids, no error.
func (c *AgentController) enumerateLocalAgents() ([]string, error) {
	base := filepath.Join(c.cfg.AgentHomeBase, "workers", c.cfg.WorkerID, "agents")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read agents dir %q: %w", base, err)
	}
	var ids []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		instPath := filepath.Join(base, e.Name(), agentsupervisor.InstanceFileName)
		if _, statErr := os.Stat(instPath); statErr == nil {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}

// reconcileAgentOnBoot probes one agent's local supervisor, decides the action vs
// the center record, and executes it — the WHOLE sequence under the agent's home
// lock (cross-daemon: no two daemons reattach/relaunch the same agent). Per-agent
// errors are logged; this never panics the boot loop.
func (c *AgentController) reconcileAgentOnBoot(ctx context.Context, agentID string, rec *centerRecord, version int) {
	home, _, err := c.agentPaths(agentID)
	if err != nil {
		c.log("boot-reconcile agent=%s resolve home: %v — skip", agentID, err)
		return
	}

	release, lerr := supervisormanager.AcquireHomeLock(home)
	if lerr != nil {
		// Another daemon (or a concurrent op) holds the lock — skip; it owns this
		// agent's reconcile.
		c.log("boot-reconcile agent=%s home lock busy: %v — skip", agentID, lerr)
		return
	}
	defer release()

	pr, perr := supervisormanager.ProbeAgent(ctx, home)
	if perr != nil {
		c.log("boot-reconcile agent=%s probe: %v — skip", agentID, perr)
		return
	}

	action := decideBootAction(pr.State, rec)
	c.log("boot-reconcile agent=%s probe=%s desired=%s → %s", agentID, probeStateName(pr.State), desiredOf(rec), action.Kind)

	switch action.Kind {
	case bootReattach:
		c.bootReattach(ctx, agentID, home, pr, version)
	case bootReapRelaunch:
		closeProbeClient(pr) // Unavailable carries no client; defensive
		c.bootReapRelaunch(ctx, agentID, home, version, action.Nudge)
	case bootStopReap:
		c.bootStopReap(agentID, home, pr)
	case bootReapOnly:
		closeProbeClient(pr)
		if rerr := supervisormanager.ReapResidual(home); rerr != nil {
			c.log("boot-reconcile agent=%s reap-only: %v", agentID, rerr)
		}
	case bootNoop:
		closeProbeClient(pr)
		c.log("boot-reconcile agent=%s noop (idle desired-running / unknown — leave for next work)", agentID)
	}
}

// bootReattach re-attaches to a live survivor from its durable offset (the
// supervisor's baseOffset == the daemon's last-acked offset). NO nudge — claude is
// alive and may be mid-turn. The ProbeResult owns the open client; the reattach
// session takes it over.
func (c *AgentController) bootReattach(ctx context.Context, agentID, home string, pr supervisormanager.ProbeResult, version int) {
	ref := supervisormanager.RefFromProbe(home, pr)
	if ref == nil || ref.Client == nil {
		c.log("boot-reconcile agent=%s reattach: probe carried no live ref/client — skip", agentID)
		closeProbeClient(pr)
		return
	}

	// Reserve the managedAgent BEFORE the event-pump starts (OnEvent/OnExit fire on
	// the pump goroutine).
	ma := &managedAgent{agentID: agentID, appliedVersion: version}
	c.mu.Lock()
	c.agents[agentID] = ma
	c.mu.Unlock()

	sess, err := ReattachSupervisorSession(ctx, ref, ref.Client,
		func(ev claudestream.StreamEvent) { c.onEvent(agentID, ev) },
		func(exitErr error) { c.onExit(agentID, exitErr) },
		c.cfg.Logger,
		pr.Hello.BaseOffset,
	)
	if err != nil {
		c.mu.Lock()
		if c.agents[agentID] == ma {
			delete(c.agents, agentID)
		}
		c.mu.Unlock()
		c.log("boot-reconcile agent=%s reattach: %v", agentID, err)
		return
	}
	c.mu.Lock()
	ma.session = sess
	c.mu.Unlock()
	c.log("boot-reconcile agent=%s RE-ATTACHED from offset=%d (no nudge — claude alive)", agentID, pr.Hello.BaseOffset)
}

// bootReapRelaunch reaps any residual then starts a fresh supervisor (which reads
// the DURABLE epoch via ReadEpoch — resuming the SAME claude session-id, never 0).
// Injects the resume nudge ONLY when an ACTIVE WorkItem is in flight.
func (c *AgentController) bootReapRelaunch(ctx context.Context, agentID, home string, version int, nudge bool) {
	if rerr := supervisormanager.ReapResidual(home); rerr != nil {
		c.log("boot-reconcile agent=%s reap before relaunch: %v", agentID, rerr)
	}
	// startSession reads the durable epoch + spawns the supervisor. We already hold
	// the home lock (boot reconcile), so no double-lock.
	if err := c.startSession(ctx, agentID, version); err != nil {
		c.log("boot-reconcile agent=%s relaunch: %v — skip", agentID, err)
		return
	}
	c.log("boot-reconcile agent=%s RELAUNCHED version=%d (nudge=%v)", agentID, version, nudge)
	if !nudge {
		return
	}
	msg := c.cfg.ResumeNudge
	if strings.TrimSpace(msg) == "" {
		msg = DefaultResumeNudge
	}
	c.mu.Lock()
	ma := c.agents[agentID]
	var sess agentSession
	if ma != nil {
		sess = ma.session
	}
	c.mu.Unlock()
	if sess != nil {
		if err := sess.Inject(ctx, msg); err != nil {
			c.log("boot-reconcile agent=%s resume-nudge inject: %v", agentID, err)
		}
	}
}

// bootStopReap stops a LIVE supervisor (desired-stopped, or a local orphan the
// center has no record of) + reaps residual. StopSupervisor SIGTERMs the
// supervisor and closes the attach client; ReapResidual then guarantees no
// leftover claude.
func (c *AgentController) bootStopReap(agentID, home string, pr supervisormanager.ProbeResult) {
	ref := supervisormanager.RefFromProbe(home, pr)
	if ref != nil {
		if serr := supervisormanager.StopSupervisor(ref, c.cfg.StopGrace); serr != nil {
			c.log("boot-reconcile agent=%s stop supervisor: %v", agentID, serr)
		}
	} else {
		closeProbeClient(pr)
	}
	if rerr := supervisormanager.ReapResidual(home); rerr != nil {
		c.log("boot-reconcile agent=%s reap after stop: %v", agentID, rerr)
	}
}

// closeProbeClient closes a probe's attach client when we are NOT handing it to a
// reattach session (so the connection is not leaked).
func closeProbeClient(pr supervisormanager.ProbeResult) {
	if pr.Client != nil {
		_ = pr.Client.Close()
	}
}

// desiredOf renders the center desired lifecycle for logging (nil rec = orphan).
func desiredOf(rec *centerRecord) string {
	if rec == nil {
		return "(no-center-record)"
	}
	return rec.DesiredLifecycle
}

// probeStateName renders a ProbeState for logging.
func probeStateName(s supervisormanager.ProbeState) string {
	switch s {
	case supervisormanager.Reattachable:
		return "reattachable"
	case supervisormanager.Unavailable:
		return "unavailable"
	default:
		return "unknown"
	}
}
