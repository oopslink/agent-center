package workerdaemon

import (
	"testing"

	"github.com/oopslink/agent-center/internal/supervisormanager"
)

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
			name:     "unavailable + running+IDLE (no in-flight WI) → NOOP (do not relaunch idle)",
			probe:    supervisormanager.Unavailable,
			rec:      run("running", false, false),
			wantKind: bootNoop,
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
