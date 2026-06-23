package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/environment"
	envsql "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

// sweepHarness wires a WakeProjector over a real in-memory ControlLog with a fake
// clock and a programmable candidate source, so the debounce/backoff/give-up timing
// is exercised end-to-end against the actual AppendCommand idempotency.
type sweepHarness struct {
	proj    *WakeProjector
	control *environment.ControlLog
	clk     *clock.FakeClock
	ctx     context.Context

	cands   []SweepCandidate // current candidate set returned each tick
	gaveUp  []SweepCandidate // give-up escalations observed
	candErr error            // optional error from the candidate source
}

func newSweepHarness(t *testing.T, grace time.Duration) *sweepHarness {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	control := environment.NewControlLog(envsql.NewControlEventRepo(db), idgen.NewGenerator(clk), clk)

	h := &sweepHarness{control: control, clk: clk, ctx: context.Background()}
	h.proj = NewWakeProjector(WakeProjectorDeps{
		DB:         db,
		ControlLog: control,
		Applied:    outboxsql.NewAppliedRepo(db),
		Clock:      clk,
		SweepGrace: grace,
		SweepCandidates: func(context.Context) ([]SweepCandidate, error) {
			return h.cands, h.candErr
		},
		SweepGiveUp: func(_ context.Context, c SweepCandidate) {
			h.gaveUp = append(h.gaveUp, c)
		},
	})
	return h
}

func (h *sweepHarness) tick(t *testing.T) {
	t.Helper()
	if err := h.proj.ReconcileOnce(h.ctx); err != nil {
		t.Fatalf("ReconcileOnce: %v", err)
	}
}

func (h *sweepHarness) commands(t *testing.T, worker string) []*environment.WorkerControlEvent {
	t.Helper()
	cmds, err := h.control.CommandsAfter(h.ctx, environment.WorkerID(worker), 0)
	if err != nil {
		t.Fatalf("CommandsAfter: %v", err)
	}
	return cmds
}

// ① down+queued: a candidate that stays stuck across the grace window is nudged with
// an agent.work_available carrying its ENTITY agent id, on its worker's stream.
func TestSweep_DownQueued_NudgedAfterGrace(t *testing.T) {
	h := newSweepHarness(t, 60*time.Second)
	h.cands = []SweepCandidate{{WorkerID: "W1", AgentID: "A1", TaskID: "T1"}}

	h.tick(t) // first sighting → grace starts, no nudge
	if cmds := h.commands(t, "W1"); len(cmds) != 0 {
		t.Fatalf("first sighting must not nudge (grace), got %d commands", len(cmds))
	}

	h.clk.Advance(61 * time.Second)
	h.tick(t) // grace elapsed → nudge
	cmds := h.commands(t, "W1")
	if len(cmds) != 1 {
		t.Fatalf("want 1 nudge after grace, got %d", len(cmds))
	}
	if cmds[0].CommandType() != commandTypeWorkAvailable {
		t.Fatalf("want command type %q, got %q", commandTypeWorkAvailable, cmds[0].CommandType())
	}
	var pl sweepWakePayload
	if err := json.Unmarshal([]byte(cmds[0].Payload()), &pl); err != nil {
		t.Fatalf("payload unmarshal: %v", err)
	}
	if pl.AgentID != "A1" || pl.TaskID != "T1" {
		t.Fatalf("payload = %+v, want agent A1 / task T1", pl)
	}
}

// ④ within grace (still booting): a candidate seen for less than the grace window is
// never nudged — this is the just-dispatched / session-starting-up case.
func TestSweep_WithinGrace_NotNudged(t *testing.T) {
	h := newSweepHarness(t, 60*time.Second)
	h.cands = []SweepCandidate{{WorkerID: "W1", AgentID: "A1", TaskID: "T1"}}

	h.tick(t)
	h.clk.Advance(30 * time.Second) // still inside grace
	h.tick(t)
	if cmds := h.commands(t, "W1"); len(cmds) != 0 {
		t.Fatalf("must not nudge within grace, got %d commands", len(cmds))
	}
}

// recovery: an agent that drops out of the candidate set (acquired a running task) is
// pruned, so a later re-entry restarts the grace clock from scratch rather than
// nudging immediately.
func TestSweep_Recovery_RestartsGrace(t *testing.T) {
	h := newSweepHarness(t, 60*time.Second)
	h.cands = []SweepCandidate{{WorkerID: "W1", AgentID: "A1", TaskID: "T1"}}
	h.tick(t)
	h.clk.Advance(61 * time.Second)
	h.tick(t) // nudge #1
	if got := len(h.commands(t, "W1")); got != 1 {
		t.Fatalf("want 1 nudge, got %d", got)
	}

	// Recovered: no longer a candidate. Next tick prunes its state.
	h.cands = nil
	h.clk.Advance(61 * time.Second)
	h.tick(t)

	// Re-enters stuck: must serve grace again, NOT nudge immediately.
	h.cands = []SweepCandidate{{WorkerID: "W1", AgentID: "A1", TaskID: "T1"}}
	h.clk.Advance(61 * time.Second)
	h.tick(t)
	if got := len(h.commands(t, "W1")); got != 1 {
		t.Fatalf("re-entry must restart grace (still 1 total nudge), got %d", got)
	}
}

// ⑤ epoch key: two ticks that both nudge the same (agent,task) produce DISTINCT
// idempotency keys, so neither is folded away by the ControlLog UNIQUE(worker,key)
// dedup — each tick's wake genuinely reaches the worker.
func TestSweep_DistinctEpochKeysPerTick(t *testing.T) {
	h := newSweepHarness(t, 60*time.Second)
	h.cands = []SweepCandidate{{WorkerID: "W1", AgentID: "A1", TaskID: "T1"}}

	h.tick(t)                       // sighting
	h.clk.Advance(61 * time.Second) // past grace
	h.tick(t)                       // nudge #1 (emitCount 0→1, backoff = grace)
	h.clk.Advance(61 * time.Second) // past the grace-sized backoff
	h.tick(t)                       // nudge #2

	cmds := h.commands(t, "W1")
	if len(cmds) != 2 {
		t.Fatalf("want 2 distinct nudges across two ticks, got %d", len(cmds))
	}
	if cmds[0].IdempotencyKey() == cmds[1].IdempotencyKey() {
		t.Fatalf("epoch keys must differ across ticks, both = %q", cmds[0].IdempotencyKey())
	}
}

// ⑥ give-up: an agent that stays stuck despite the full nudge budget is escalated
// exactly once and then goes silent — the bound on ControlLog for a never-recoverable
// desired-running agent.
func TestSweep_GiveUpAfterCap(t *testing.T) {
	h := newSweepHarness(t, 60*time.Second)
	h.cands = []SweepCandidate{{WorkerID: "W1", AgentID: "A1", TaskID: "T1"}}

	// Drive many ticks, always well past any backoff, so the only thing that stops the
	// nudging is the give-up cap.
	for i := 0; i < 40; i++ {
		h.clk.Advance(10 * time.Minute) // exceeds sweepMaxBackoff every step
		h.tick(t)
	}

	if got := len(h.commands(t, "W1")); got != sweepMaxEmits {
		t.Fatalf("want exactly sweepMaxEmits(%d) nudges then silence, got %d", sweepMaxEmits, got)
	}
	if len(h.gaveUp) != 1 {
		t.Fatalf("want exactly one give-up escalation, got %d", len(h.gaveUp))
	}
	if h.gaveUp[0].AgentID != "A1" {
		t.Fatalf("give-up escalation for wrong agent: %+v", h.gaveUp[0])
	}
}

// candidate source error path: see TestSweep_CandidateSourceError below.

// candidate source error is surfaced (the loop logs it and retries next tick) and
// enqueues nothing.
func TestSweep_CandidateSourceError(t *testing.T) {
	h := newSweepHarness(t, 60*time.Second)
	h.candErr = context.DeadlineExceeded
	if err := h.proj.ReconcileOnce(h.ctx); err == nil {
		t.Fatal("want the candidate-source error surfaced, got nil")
	}
	if cmds := h.commands(t, "W1"); len(cmds) != 0 {
		t.Fatalf("error path must enqueue nothing, got %d", len(cmds))
	}
}

// dormant: with no SweepCandidates wired, ReconcileOnce is a graceful no-op (the
// post-F7 behavior is preserved).
func TestSweep_NilCandidates_Dormant(t *testing.T) {
	p := NewWakeProjector(WakeProjectorDeps{})
	if err := p.ReconcileOnce(context.Background()); err != nil {
		t.Fatalf("dormant ReconcileOnce must be a no-op, got %v", err)
	}
}
