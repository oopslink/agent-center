package workerdaemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agentsupervisor"
	"github.com/oopslink/agent-center/internal/supervisormanager"
)

// fakeResumer is a TEST-ONLY resumeStateQuerier returning a canned ResumeState.
type fakeResumer struct {
	state ResumeState
	err   error
}

func (f *fakeResumer) ResumeState(_ context.Context, _ string) (ResumeState, error) {
	return f.state, f.err
}

// writeBootInstance plants a minimal valid supervisor.instance in home so
// enumerateLocalAgents counts it + ProbeAgent reads it. The pids are dead and no
// socket is served → ProbeAgent finds the process gone → Unavailable{dead} (a dead
// orphan / survivor that didn't actually survive).
func writeBootInstance(t *testing.T, home, agentID string) {
	t.Helper()
	if err := os.MkdirAll(home, 0o700); err != nil {
		t.Fatal(err)
	}
	rec := map[string]any{
		"instance_id":    "inst-" + agentID,
		"agent_id":       agentID,
		"supervisor_pid": 999999, // dead pid (never the test process)
		"child_pid":      999998,
		"started_at":     time.Now().Format(time.RFC3339Nano),
	}
	b, _ := json.Marshal(rec)
	if err := os.WriteFile(filepath.Join(home, agentsupervisor.InstanceFileName), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestReconcileOnBoot_SourceUnionAndRouting proves the boot orchestration (s4b):
// source set = center resume-set ∪ LOCAL home enumeration (orphan discovery), and
// each agent routes to the correct action. No real supervisors: agents with no
// local supervisor probe Unavailable, so this exercises relaunch (+nudge) / noop /
// reap-only without a real spawn (the fake session starter stands in; real
// reattach/relaunch round-trips are Tester's GATE).
func TestReconcileOnBoot_SourceUnionAndRouting(t *testing.T) {
	base := t.TempDir()
	var logMu sync.Mutex
	var logs []string
	logger := func(m string) { logMu.Lock(); defer logMu.Unlock(); logs = append(logs, m) }

	rs := &recordingStarter{}
	resumer := &fakeResumer{state: ResumeState{Agents: []ResumeAgent{
		// running + ACTIVE in-flight → reapRelaunch + nudge.
		{AgentID: "ag-relaunch", DesiredLifecycle: "running", Version: 7,
			WorkItems: []ResumeWorkItem{{WorkItemID: "wi-1", Status: "active"}}},
		// running + NO in-flight → reap+relaunch (Mode-B self-heal at boot), NO nudge.
		{AgentID: "ag-idle", DesiredLifecycle: "running", Version: 3},
		// stopped → reapOnly.
		{AgentID: "ag-stopped", DesiredLifecycle: "stopped", Version: 2},
	}}}

	c, err := NewAgentController(AgentControllerConfig{
		Reporter: &recordingReporter{}, WorkerID: "w-1",
		AdminURL: "unix:/tmp/a.sock", WorkerToken: "t", BinaryPath: "agent-center",
		AgentHomeBase: base, StopGrace: 20 * time.Millisecond,
		Resumer: resumer, Logger: logger,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.cfg.starter = rs.start

	// Plant a LOCAL orphan: a home with a supervisor.instance but NOT in the center
	// set — only the local enumeration can surface it.
	orphanHome := filepath.Join(base, "workers", "w-1", "agents", "ag-orphan")
	writeBootInstance(t, orphanHome, "ag-orphan")

	if err := c.ReconcileOnBoot(context.Background()); err != nil {
		t.Fatalf("ReconcileOnBoot: %v", err)
	}

	// TWO sessions started: ag-relaunch (ACTIVE in-flight → relaunch + nudge) AND
	// ag-idle (desired-running but idle → Mode-B self-heal relaunch at boot, NO
	// nudge). ag-stopped / ag-orphan must NOT start a session.
	if rs.count() != 2 {
		t.Fatalf("want exactly 2 relaunch sessions (ag-relaunch + ag-idle), got %d", rs.count())
	}
	byAgent := map[string]*fakeSession{}
	for _, s := range rs.all() {
		byAgent[s.cfg.AgentID] = s
	}
	if byAgent["ag-relaunch"] == nil {
		t.Fatal("ag-relaunch (active in-flight) must relaunch")
	}
	if byAgent["ag-idle"] == nil {
		t.Fatal("ag-idle (idle desired-running) must relaunch — Mode-B self-heal at boot")
	}
	if byAgent["ag-stopped"] != nil || byAgent["ag-orphan"] != nil {
		t.Fatal("stopped / orphan must NOT start a session")
	}
	// ag-relaunch got the resume nudge exactly once (ACTIVE WorkItem); ag-idle did NOT.
	if msgs := byAgent["ag-relaunch"].injectedMsgs(); len(msgs) != 1 || msgs[0] != DefaultResumeNudge {
		t.Fatalf("ag-relaunch must inject the resume nudge once, got %v", msgs)
	}
	if msgs := byAgent["ag-idle"].injectedMsgs(); len(msgs) != 0 {
		t.Fatalf("ag-idle (idle relaunch) must NOT nudge, got %v", msgs)
	}

	// The orphan was DISCOVERED via local enumeration (not in the center set) and
	// reconciled — proven by its boot-reconcile log line.
	joined := strings.Join(logs, "\n")
	for _, id := range []string{"ag-relaunch", "ag-idle", "ag-stopped", "ag-orphan"} {
		if !strings.Contains(joined, "agent="+id) {
			t.Fatalf("agent %s was not reconciled; logs:\n%s", id, joined)
		}
	}
	// The orphan specifically routed to reap_only (no center record + dead).
	if !strings.Contains(joined, "agent=ag-orphan") || !strings.Contains(joined, "(no-center-record)") {
		t.Fatalf("orphan not routed as no-center-record; logs:\n%s", joined)
	}
}

// TestReconcileOnBoot_NoResumerDormant proves the boot reconcile is a no-op when no
// Resumer is wired (additive/dormant — the pre-cutover default).
func TestReconcileOnBoot_NoResumerDormant(t *testing.T) {
	rs := &recordingStarter{}
	c, err := NewAgentController(AgentControllerConfig{
		Reporter: &recordingReporter{}, WorkerID: "w-1", AgentHomeBase: t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.cfg.starter = rs.start
	if err := c.ReconcileOnBoot(context.Background()); err != nil {
		t.Fatalf("dormant ReconcileOnBoot must be a no-op, got %v", err)
	}
	if rs.count() != 0 {
		t.Fatalf("no Resumer → no sessions, got %d", rs.count())
	}
}

// TestReconcileOnBoot_RelaunchWaitingInputNoNudge proves a relaunch with only a
// WAITING_INPUT in-flight WorkItem (no active) does NOT inject a nudge — the
// session being up is enough; the center's wake cadence delivers.
func TestReconcileOnBoot_RelaunchWaitingInputNoNudge(t *testing.T) {
	rs := &recordingStarter{}
	resumer := &fakeResumer{state: ResumeState{Agents: []ResumeAgent{
		{AgentID: "ag-wait", DesiredLifecycle: "running", Version: 4,
			WorkItems: []ResumeWorkItem{{WorkItemID: "wi-w", Status: "waiting_input"}}},
	}}}
	c, err := NewAgentController(AgentControllerConfig{
		Reporter: &recordingReporter{}, WorkerID: "w-1",
		AdminURL: "unix:/tmp/a.sock", WorkerToken: "t", BinaryPath: "agent-center",
		AgentHomeBase: t.TempDir(), Resumer: resumer,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.cfg.starter = rs.start
	if err := c.ReconcileOnBoot(context.Background()); err != nil {
		t.Fatalf("ReconcileOnBoot: %v", err)
	}
	if rs.count() != 1 {
		t.Fatalf("waiting_input agent must relaunch (session up for wake), got %d sessions", rs.count())
	}
	if msgs := rs.last().injectedMsgs(); len(msgs) != 0 {
		t.Fatalf("waiting_input relaunch must NOT nudge, got %v", msgs)
	}
}

// TestDecideBootAction_FullCartesianProduct exhaustively pins EVERY cell of the
// decision matrix (PM: probe × center full Cartesian product, explicit action per
// cell, no implicit fallthrough). The decision space is:
//
//	probe  ∈ {Reattachable, Unavailable}
//	center ∈ {running+inflight, running+idle, stopped, stopping, no-record}
//
// Each row below is one cell with its REQUIRED action + nudge.
func TestDecideBootAction_FullCartesianProduct(t *testing.T) {
	run := func(desired string, inflight, active bool) *centerRecord {
		return &centerRecord{DesiredLifecycle: desired, HasInflight: inflight, HasActive: active}
	}

	cases := []struct {
		name      string
		probe     supervisormanager.ProbeState
		rec       *centerRecord
		wantKind  bootActionKind
		wantNudge bool
	}{
		// ---- Reattachable (a LIVE, compatible local supervisor) ----
		{
			name:     "reattachable + running+inflight → reattach (no nudge: claude alive)",
			probe:    supervisormanager.Reattachable,
			rec:      run("running", true, true),
			wantKind: bootReattach,
		},
		{
			name:     "reattachable + running+idle → reattach (keep live desired-running agent)",
			probe:    supervisormanager.Reattachable,
			rec:      run("running", false, false),
			wantKind: bootReattach,
		},
		{
			name:     "reattachable + stopped → stop+reap (desired-stopped WINS over live)",
			probe:    supervisormanager.Reattachable,
			rec:      run("stopped", false, false),
			wantKind: bootStopReap,
		},
		{
			name:     "reattachable + stopping → stop+reap",
			probe:    supervisormanager.Reattachable,
			rec:      run("stopping", false, false),
			wantKind: bootStopReap,
		},
		{
			name:     "reattachable + stopped WITH orphan in-flight WI → stop+reap (stopped still wins)",
			probe:    supervisormanager.Reattachable,
			rec:      run("stopped", true, true),
			wantKind: bootStopReap,
		},
		{
			name:     "reattachable + no-center-record → stop+reap (local orphan)",
			probe:    supervisormanager.Reattachable,
			rec:      nil,
			wantKind: bootStopReap,
		},

		// ---- Unavailable (no live+compatible supervisor: dead/missing/incompatible) ----
		{
			name:      "unavailable + running+inflight+active → reap+relaunch WITH nudge",
			probe:     supervisormanager.Unavailable,
			rec:       run("running", true, true),
			wantKind:  bootReapRelaunch,
			wantNudge: true,
		},
		{
			name:     "unavailable + running+inflight (waiting_input only, no active) → reap+relaunch NO nudge",
			probe:    supervisormanager.Unavailable,
			rec:      run("running", true, false),
			wantKind: bootReapRelaunch,
			// HasActive=false → no nudge (a waiting_input agent needs the session up,
			// not a nudge).
			wantNudge: false,
		},
		{
			name:     "unavailable + running+IDLE (no in-flight WI) → reap+relaunch (Mode-B self-heal at boot; resume, no nudge)",
			probe:    supervisormanager.Unavailable,
			rec:      run("running", false, false),
			wantKind: bootReapRelaunch,
			// Relaunch the dead session even when idle (else a later agent.work dead-
			// locks on no-session). No nudge: HasActive=false → nothing to re-drive;
			// an arriving agent.work injects its own brief.
			wantNudge: false,
		},
		{
			name:     "unavailable + stopped → reap-only (dead + should-stop)",
			probe:    supervisormanager.Unavailable,
			rec:      run("stopped", false, false),
			wantKind: bootReapOnly,
		},
		{
			name:     "unavailable + stopping → reap-only",
			probe:    supervisormanager.Unavailable,
			rec:      run("stopping", false, false),
			wantKind: bootReapOnly,
		},
		{
			name:     "unavailable + no-center-record → reap-only (dead orphan)",
			probe:    supervisormanager.Unavailable,
			rec:      nil,
			wantKind: bootReapOnly,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideBootAction(tc.probe, tc.rec)
			if got.Kind != tc.wantKind {
				t.Fatalf("kind = %s, want %s", got.Kind, tc.wantKind)
			}
			if got.Nudge != tc.wantNudge {
				t.Fatalf("nudge = %v, want %v", got.Nudge, tc.wantNudge)
			}
			// Nudge is meaningful ONLY for reap+relaunch — never set for any other
			// kind (notably reattach, where claude is alive).
			if got.Kind != bootReapRelaunch && got.Nudge {
				t.Fatalf("nudge must never be set for kind %s", got.Kind)
			}
		})
	}
}

// TestDecideBootAction_NudgeOnlyOnRelaunch guards the key correctness invariant
// across the whole matrix: a nudge is emitted ONLY by a reap+relaunch of an agent
// with an ACTIVE WorkItem — reattach (claude alive, mid-turn) NEVER nudges.
func TestDecideBootAction_NudgeOnlyOnRelaunch(t *testing.T) {
	// Reattach of an agent that DOES have an active WI must still NOT nudge.
	a := decideBootAction(supervisormanager.Reattachable, &centerRecord{DesiredLifecycle: "running", HasInflight: true, HasActive: true})
	if a.Kind != bootReattach || a.Nudge {
		t.Fatalf("reattach with active WI must not nudge: %+v", a)
	}
	// Relaunch with an active WI nudges; without, it does not.
	withActive := decideBootAction(supervisormanager.Unavailable, &centerRecord{DesiredLifecycle: "running", HasInflight: true, HasActive: true})
	if withActive.Kind != bootReapRelaunch || !withActive.Nudge {
		t.Fatalf("relaunch with active WI must nudge: %+v", withActive)
	}
	noActive := decideBootAction(supervisormanager.Unavailable, &centerRecord{DesiredLifecycle: "running", HasInflight: true, HasActive: false})
	if noActive.Kind != bootReapRelaunch || noActive.Nudge {
		t.Fatalf("relaunch without active WI must not nudge: %+v", noActive)
	}
}
