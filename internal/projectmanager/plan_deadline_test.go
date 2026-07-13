package projectmanager

import (
	"testing"
	"time"
)

var tBase = time.Unix(1_700_000_000, 0).UTC()

func TestTimeoutAction_IsValid(t *testing.T) {
	for _, a := range []TimeoutAction{TimeoutReprobe, TimeoutEscalate, TimeoutRouteToHandler} {
		if !a.IsValid() {
			t.Errorf("%q should be valid", a)
		}
	}
	for _, a := range []TimeoutAction{"", "bogus", "REPROBE"} {
		if TimeoutAction(a).IsValid() {
			t.Errorf("%q should be invalid", a)
		}
	}
}

// TestDeadlineFor_OverrideAndDefault covers the resolution order: a per-type override
// wins over the default; an un-overridden type falls through to the default.
func TestDeadlineFor_OverrideAndDefault(t *testing.T) {
	p := DeadlinePolicy{
		Default: WaitDeadline{Timeout: time.Hour, OnTimeout: TimeoutReprobe},
		ByWaitType: map[WaitType]WaitDeadline{
			WaitAcceptanceVerdict: {Timeout: 2 * time.Hour, OnTimeout: TimeoutEscalate},
		},
	}
	// Overridden type → the override's timeout + action.
	dl, act, ok := p.DeadlineFor(WaitAcceptanceVerdict, tBase)
	if !ok || !dl.Equal(tBase.Add(2*time.Hour)) || act != TimeoutEscalate {
		t.Fatalf("override = (%v, %v, %v), want (%v, escalate, true)", dl, act, ok, tBase.Add(2*time.Hour))
	}
	// Un-overridden type → the default's timeout + action.
	dl, act, ok = p.DeadlineFor(WaitUpstreamCompletion, tBase)
	if !ok || !dl.Equal(tBase.Add(time.Hour)) || act != TimeoutReprobe {
		t.Fatalf("default = (%v, %v, %v), want (%v, reprobe, true)", dl, act, ok, tBase.Add(time.Hour))
	}
}

// TestDeadlineFor_Disabled covers every path that assigns NO deadline (ok=false).
func TestDeadlineFor_Disabled(t *testing.T) {
	// Per-type override with a non-positive timeout explicitly disables the type even
	// when the default would assign one.
	p := DeadlinePolicy{
		Default:    WaitDeadline{Timeout: time.Hour, OnTimeout: TimeoutReprobe},
		ByWaitType: map[WaitType]WaitDeadline{WaitHumanDecision: {Timeout: 0}},
	}
	if _, _, ok := p.DeadlineFor(WaitHumanDecision, tBase); ok {
		t.Fatal("override Timeout=0 should disable the deadline (ok=false)")
	}
	// Zero policy is inert — no default, no override → nothing assigned.
	var inert DeadlinePolicy
	if _, _, ok := inert.DeadlineFor(WaitUpstreamCompletion, tBase); ok {
		t.Fatal("zero policy should assign no deadline (inert)")
	}
	// A zero waited_since cannot anchor a deadline.
	if _, _, ok := p.DeadlineFor(WaitUpstreamCompletion, time.Time{}); ok {
		t.Fatal("zero waited_since should assign no deadline")
	}
}

// TestDeadlineFor_DefaultsActionToEscalate: a configured deadline whose action is empty
// or invalid falls back to escalate — the safe verb (record, never auto-decide).
func TestDeadlineFor_DefaultsActionToEscalate(t *testing.T) {
	for _, bad := range []TimeoutAction{"", "nonsense"} {
		p := DeadlinePolicy{Default: WaitDeadline{Timeout: time.Hour, OnTimeout: bad}}
		_, act, ok := p.DeadlineFor(WaitTimeoutOnly, tBase)
		if !ok || act != TimeoutEscalate {
			t.Fatalf("action %q → (%v, %v), want (escalate, true)", bad, act, ok)
		}
	}
}

// TestDeadlineFor_NoDrift: for a FIXED (type, waited_since) the deadline is stable — the
// no-drift guarantee the reconcile relies on (waited_since preserved ⇒ same deadline).
func TestDeadlineFor_NoDrift(t *testing.T) {
	p := DefaultDeadlinePolicy()
	dl1, _, ok1 := p.DeadlineFor(WaitUpstreamCompletion, tBase)
	dl2, _, ok2 := p.DeadlineFor(WaitUpstreamCompletion, tBase)
	if !ok1 || !ok2 || !dl1.Equal(dl2) {
		t.Fatalf("deadline drifted: %v vs %v", dl1, dl2)
	}
}

// TestDefaultDeadlinePolicy_Sane asserts the production policy assigns a valid action +
// a future deadline for every wait_type (no type is silently un-covered).
func TestDefaultDeadlinePolicy_Sane(t *testing.T) {
	p := DefaultDeadlinePolicy()
	for _, wt := range []WaitType{
		WaitUpstreamCompletion, WaitAcceptanceVerdict, WaitStageBarrier,
		WaitHumanDecision, WaitExternalEvent, WaitExecutorLiveness, WaitTimeoutOnly,
	} {
		dl, act, ok := p.DeadlineFor(wt, tBase)
		if !ok {
			t.Errorf("%q: default policy assigns no deadline", wt)
			continue
		}
		if !act.IsValid() {
			t.Errorf("%q: action %q invalid", wt, act)
		}
		if !dl.After(tBase) {
			t.Errorf("%q: deadline %v not after base", wt, dl)
		}
	}
	if p.ProbeBackoff <= 0 {
		t.Error("default ProbeBackoff should be positive")
	}
}
