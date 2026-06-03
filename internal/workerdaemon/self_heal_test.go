package workerdaemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/claudestream"
)

// fakeClock is a deterministic clock seam for asserting the self-heal backoff curve /
// cap / reset without real wall-clock waits.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time          { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.t = c.t.Add(d) }

// TestDecideSelfHeal_CurveCapReset pins the PURE crash→action policy (the part Tester
// verifies at runtime via real kills): exponential backoff 1→2→4→8→16s for crashCount
// 1..5, the 6th crash circuit-breaks (failed), and a healthy run ≥ resetWindow resets
// the count. Params per @oopslink/PM (msgs c277e962 / 9fe6748e).
func TestDecideSelfHeal_CurveCapReset(t *testing.T) {
	p := selfHealParams{maxAttempts: 5, backoffBase: time.Second, backoffCap: 30 * time.Second, resetWindow: 60 * time.Second}
	base := time.Unix(1_000_000, 0)

	// Curve: crashCount 1..5 → 1,2,4,8,16s (lastRelaunchAt recent → no reset).
	want := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	for i := 0; i < 5; i++ {
		dec := decideSelfHeal(i /*prevCount*/, base /*lastRelaunch*/, base.Add(time.Second) /*now*/, p)
		if dec.failed {
			t.Fatalf("crashCount %d must not fail (cap is 5)", i+1)
		}
		if dec.crashCount != i+1 {
			t.Fatalf("crashCount = %d, want %d", dec.crashCount, i+1)
		}
		if dec.backoff != want[i] {
			t.Fatalf("backoff for crash #%d = %s, want %s", i+1, dec.backoff, want[i])
		}
	}

	// Cap: prevCount 5 → count 6 → terminal failed (the 6th crash).
	if dec := decideSelfHeal(5, base, base.Add(time.Second), p); !dec.failed || dec.crashCount != 6 {
		t.Fatalf("6th crash must circuit-break: got failed=%v count=%d", dec.failed, dec.crashCount)
	}

	// Reset: a healthy run ≥ resetWindow since the last relaunch → fresh count 1.
	if dec := decideSelfHeal(4, base, base.Add(60*time.Second), p); dec.failed || dec.crashCount != 1 || dec.backoff != time.Second {
		t.Fatalf("healthy ≥60s must reset to count 1 / 1s backoff: got failed=%v count=%d backoff=%s", dec.failed, dec.crashCount, dec.backoff)
	}

	// First-ever crash (lastRelaunchAt zero) → count 1, 1s.
	if dec := decideSelfHeal(0, time.Time{}, base, p); dec.crashCount != 1 || dec.backoff != time.Second {
		t.Fatalf("first crash → count 1 / 1s: got count=%d backoff=%s", dec.crashCount, dec.backoff)
	}
}

// TestSelfHeal_OnTickRelaunchesAfterBackoff proves the marshaling: a recorded crash
// schedules a backed-off relaunch that OnTick (the ControlLoop single-threaded hook)
// performs only AFTER the backoff elapses, exactly once, with a resume nudge (hadWork).
func TestSelfHeal_OnTickRelaunchesAfterBackoff(t *testing.T) {
	base := t.TempDir()
	// AcquireHomeLock + startSession operate under the agent home; create it.
	home := filepath.Join(base, "agents", "ag-1")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	rs := &recordingStarter{}
	c, err := NewAgentController(AgentControllerConfig{
		Reporter: &recordingReporter{}, WorkerID: "w-1", AdminURL: "unix:/tmp/a.sock",
		WorkerToken: "t", BinaryPath: "agent-center", AgentHomeBase: base, Now: clock.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.cfg.starter = rs.start

	// Crash with active work → schedules a relaunch at now+1s (crashCount 1).
	c.recordCrashAndSchedule("ag-1", 7 /*version*/, true /*hadWork*/, "wi-7" /*workItemID*/, "bad-model-x" /*model*/, "boom")

	// Before the 1s backoff elapses → OnTick must NOT relaunch.
	c.OnTick(context.Background())
	if rs.count() != 0 {
		t.Fatalf("relaunch fired before backoff elapsed: got %d", rs.count())
	}

	// Advance past the backoff → OnTick relaunches once, with the resume nudge.
	clock.advance(2 * time.Second)
	c.OnTick(context.Background())
	if rs.count() != 1 {
		t.Fatalf("want exactly 1 relaunch after backoff, got %d", rs.count())
	}
	if got := rs.last().cfg.AgentID; got != "ag-1" {
		t.Fatalf("relaunched wrong agent: %s", got)
	}
	if msgs := rs.last().injectedMsgs(); len(msgs) != 1 || msgs[0] != DefaultResumeNudge {
		t.Fatalf("self-heal relaunch with active work must nudge once, got %v", msgs)
	}
	// GATE-7 Mode-B FORK: the crash-relaunch must fork into a FRESH generation (gen
	// bumped 0→1, persisted) and --resume from the killed session's id (gen 0), so it
	// never re-collides with the held session-id lock. (Initial epoch 0 / gen 0.)
	if got := rs.last().cfg.Generation; got != 1 {
		t.Fatalf("self-heal relaunch must fork to generation 1, got %d", got)
	}
	if got, want := rs.last().cfg.ResumeFromSessionID, claudestream.SessionUUIDGen("ag-1", 0, 0); got != want {
		t.Fatalf("fork must --resume the prior (gen-0) session-id %q, got %q", want, got)
	}
	// L2×Mode-B: the in-flight WorkItem id (captured at crash) must be REBOUND onto
	// the relaunched managedAgent's currentWorkItemID — the SAME field
	// surfaceTurnFailure reads — so a failed re-drive turn can fail the original WI
	// instead of leaving it silently active.
	c.mu.Lock()
	reboundWI := ""
	if ma := c.agents["ag-1"]; ma != nil {
		reboundWI = ma.currentWorkItemID
	}
	c.mu.Unlock()
	if reboundWI != "wi-7" {
		t.Fatalf("relaunch must rebind currentWorkItemID to the in-flight WI %q (L2×Mode-B), got %q", "wi-7", reboundWI)
	}
	// Model-crash-survival: the self-heal relaunch must spawn with the SAME model
	// (carried across the crash via selfHealEntry.model), not fall back to claude's
	// default — otherwise a mid-run crash would silently change the agent's model AND
	// the bad-model re-drive (Tester's rebind induction) couldn't produce is_error.
	if got := rs.last().cfg.Model; got != "bad-model-x" {
		t.Fatalf("self-heal relaunch must carry the agent model across the crash, got %q want %q", got, "bad-model-x")
	}

	// Idempotent: a further OnTick with no new crash does NOT re-relaunch.
	clock.advance(2 * time.Second)
	c.OnTick(context.Background())
	if rs.count() != 1 {
		t.Fatalf("OnTick re-fired a consumed relaunch: got %d", rs.count())
	}
}

// TestSelfHeal_IdleRelaunchFreshNoResume proves FINDING-3 (#117 part B) at the
// startSession arg-assembly level: a self-heal relaunch of an IDLE agent (hadWork ==
// nudge == false) STILL bumps the generation to a fresh never-locked id (lock-
// avoidance, unchanged) but spawns it WITHOUT --resume (ResumeFromSessionID empty) so
// claude starts fresh — no `--resume`-of-a-no-completed-turn-session
// error_during_execution crash-loop. Contrast: TestSelfHeal_OnTickRelaunchesAfterBackoff
// pins the hadWork==true path (gen 1 + ResumeFromSessionID = the gen-0 id), which must
// remain byte-identical.
func TestSelfHeal_IdleRelaunchFreshNoResume(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "agents", "ag-1")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	rs := &recordingStarter{}
	c, err := NewAgentController(AgentControllerConfig{
		Reporter: &recordingReporter{}, WorkerID: "w-1", AdminURL: "unix:/tmp/a.sock",
		WorkerToken: "t", BinaryPath: "agent-center", AgentHomeBase: base, Now: clock.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.cfg.starter = rs.start

	// Crash of an IDLE agent (hadWork=false, no in-flight WorkItem) → schedules a
	// relaunch at now+1s.
	c.recordCrashAndSchedule("ag-1", 3 /*version*/, false /*hadWork*/, "" /*workItemID*/, "" /*model*/, "idle-crash")

	clock.advance(2 * time.Second)
	c.OnTick(context.Background())
	if rs.count() != 1 {
		t.Fatalf("want exactly 1 idle relaunch after backoff, got %d", rs.count())
	}

	// Lock-avoidance UNCHANGED: the generation is still bumped 0→1 (fresh never-locked id).
	if got := rs.last().cfg.Generation; got != 1 {
		t.Fatalf("idle relaunch must STILL fork to generation 1 (lock-avoidance), got %d", got)
	}
	// FINDING-3: but the fresh id starts FRESH — NO --resume (empty ResumeFromSessionID).
	if got := rs.last().cfg.ResumeFromSessionID; got != "" {
		t.Fatalf("idle relaunch must NOT resume (fresh session, no error_during_execution); ResumeFromSessionID=%q want empty", got)
	}
	// And an idle relaunch injects no nudge (nothing to re-drive).
	if msgs := rs.last().injectedMsgs(); len(msgs) != 0 {
		t.Fatalf("idle relaunch must NOT nudge, got %v", msgs)
	}
}

// TestSelfHeal_RelaunchFailCircuitBreaks proves FINDING-3 (#117 part A): a relaunch
// that FAILS to come up is no longer silent-limbo. selfHealRelaunch captures
// bootReapRelaunch's error, LOUD-logs it, and feeds it through the SAME state machine
// as a crash — so repeated relaunch-fails count toward the cap and eventually circuit-
// break to terminal (failed=true, Fleet-visible), instead of stalling forever after
// nextRelaunchAt was consumed.
func TestSelfHeal_RelaunchFailCircuitBreaks(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "agents", "ag-1")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	rs := &recordingStarter{}
	rep := &recordingReporter{}
	var logMu sync.Mutex
	var logs []string
	c, err := NewAgentController(AgentControllerConfig{
		Reporter: rep, WorkerID: "w-1", AdminURL: "unix:/tmp/a.sock",
		WorkerToken: "t", BinaryPath: "agent-center", AgentHomeBase: base, Now: clock.now,
		Logger: func(m string) { logMu.Lock(); defer logMu.Unlock(); logs = append(logs, m) },
	})
	if err != nil {
		t.Fatal(err)
	}
	c.cfg.starter = rs.start

	// One real crash arms the first relaunch (crashCount 1).
	c.recordCrashAndSchedule("ag-1", 9 /*version*/, false /*hadWork*/, "", "", "boom")

	// Each relaunch attempt FAILS to come up (startSession returns an error). The cap
	// is the default 5: relaunch-fails advance crashCount 2..5 (transient "error"),
	// the 6th decision (crashCount 6) circuit-breaks to terminal "failed". We drive
	// ticks until the entry latches failed (bounded — must terminate, not limbo).
	prevCount := 0
	for i := 0; i < 20; i++ {
		c.mu.Lock()
		e := c.selfHeal["ag-1"]
		if e != nil && e.failed {
			c.mu.Unlock()
			break
		}
		prevCount = 0
		if e != nil {
			prevCount = e.crashCount
		}
		c.mu.Unlock()

		rs.mu.Lock()
		rs.nextErr = errSelfHealRelaunchTest // the relaunch fails to come up
		rs.mu.Unlock()

		clock.advance(time.Minute) // past any backoff
		c.OnTick(context.Background())

		// Each failed relaunch must advance the count (no silent-limbo / no stall).
		c.mu.Lock()
		e = c.selfHeal["ag-1"]
		newCount := 0
		if e != nil {
			newCount = e.crashCount
		}
		c.mu.Unlock()
		if !(e != nil && e.failed) && newCount <= prevCount {
			t.Fatalf("relaunch-fail must advance the crash count toward the cap (was %d, now %d) — not silent-limbo", prevCount, newCount)
		}
	}

	c.mu.Lock()
	e := c.selfHeal["ag-1"]
	failed := e != nil && e.failed
	c.mu.Unlock()
	if !failed {
		t.Fatal("repeated relaunch-fails must circuit-break to terminal failed (bounded, not limbo)")
	}

	// LOUD-surface: the distinct relaunch-fail log line must have fired.
	logMu.Lock()
	sawFailLog := false
	for _, m := range logs {
		if strings.Contains(m, "FAILED to come up") {
			sawFailLog = true
			break
		}
	}
	logMu.Unlock()
	if !sawFailLog {
		t.Fatal("relaunch-fail must LOUD-log a distinct 'FAILED to come up' line")
	}

	// Terminal "failed" lifecycle must have been reported (Fleet-visible).
	sawFailed := false
	for _, l := range rep.lifecycleCalls() {
		if l.agentID == "ag-1" && l.state == "failed" {
			sawFailed = true
			break
		}
	}
	if !sawFailed {
		t.Fatal("terminal circuit-break must report lifecycle 'failed' (Fleet-visible)")
	}
}

var errSelfHealRelaunchTest = errSentinel("supervisor did not come up within 15s")

// errSentinel is a tiny error type for the relaunch-fail test (no fmt import churn).
type errSentinel string

func (e errSentinel) Error() string { return string(e) }

// TestSelfHeal_CircuitBreaksAndClearUnlatches proves the cap → terminal failed (no
// more auto relaunch) and that a command-driven clearSelfHeal un-latches it (manual
// recovery).
func TestSelfHeal_CircuitBreaksAndClearUnlatches(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "agents", "ag-1")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	rs := &recordingStarter{}
	c, err := NewAgentController(AgentControllerConfig{
		Reporter: &recordingReporter{}, WorkerID: "w-1", AdminURL: "unix:/tmp/a.sock",
		WorkerToken: "t", BinaryPath: "agent-center", AgentHomeBase: base, Now: clock.now,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.cfg.starter = rs.start

	// 6 consecutive crashes (no relaunch between → lastRelaunchAt stays zero → no
	// reset → counts 1..6): crashes 1-5 report transient "error", the 6th circuit-
	// breaks and reports terminal "failed".
	for i := 0; i < 6; i++ {
		state := c.recordCrashAndSchedule("ag-1", 1, false, "" /*workItemID*/, "" /*model*/, "boom")
		want := "error"
		if i == 5 {
			want = "failed"
		}
		if state != want {
			t.Fatalf("crash #%d: report state = %q, want %q", i+1, state, want)
		}
	}
	c.mu.Lock()
	e := c.selfHeal["ag-1"]
	failed := e != nil && e.failed
	c.mu.Unlock()
	if !failed {
		t.Fatal("6 consecutive crashes must circuit-break to terminal failed")
	}

	// A failed agent is NEVER auto-relaunched, even long after.
	clock.advance(time.Hour)
	c.OnTick(context.Background())
	if rs.count() != 0 {
		t.Fatalf("terminal-failed agent must not auto-relaunch, got %d", rs.count())
	}

	// clearSelfHeal (command-driven reset/restart) un-latches it.
	c.clearSelfHeal("ag-1")
	c.mu.Lock()
	_, present := c.selfHeal["ag-1"]
	c.mu.Unlock()
	if present {
		t.Fatal("clearSelfHeal must drop the entry (un-latch terminal-failed)")
	}
}
