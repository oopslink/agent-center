package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// reconcileRuntime builds a LocalRuntime wired for the option-b reconcile backstop:
// a scripted inflight-task caller, a recording fake session, a controllable clock,
// and a fresh durable pending store on a temp path.
func reconcileRuntime(t *testing.T, caller ToolCaller, clk *advClock) (*LocalRuntime, *fakeSession, *pendingStore) {
	t.Helper()
	fs := &fakeSession{}
	st := &SessionState{}
	st.Session = fs
	r := NewLocalRuntime(LocalRuntimeConfig{
		AgentID:    "agent-1",
		Now:        clk.now,
		ToolCaller: func() ToolCaller { return caller },
	}, st)
	store := newPendingStore(filepath.Join(t.TempDir(), "pending.json"))
	r.pending = store
	return r, fs, store
}

// runningResp is a list_my_inflight_tasks response with the given task ids all running.
func runningResp(ids ...string) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, `{"task_id":"`+id+`","title":"x","status":"running"}`)
	}
	return `{"tasks":[` + strings.Join(parts, ",") + `]}`
}

// TestReconcile_DropWhenJudged: a pending judgment whose task is no longer in the
// RUNNING set (the supervisor completed/blocked it) is dropped, with no nudge.
func TestReconcile_DropWhenJudged(t *testing.T) {
	clk := &advClock{t: time.Unix(1_700_000_000, 0)}
	// center reports a DIFFERENT task running — our pending task is gone (judged).
	caller := &scriptedInflightCaller{resp: runningResp("other-task")}
	r, fs, store := reconcileRuntime(t, caller, clk)
	store.record("task-1", "judge me", clk.now())

	r.reconcilePendingJudgments(context.Background(), clk.now())

	if store.len() != 0 {
		t.Fatalf("pending len = %d, want 0 (judged → dropped)", store.len())
	}
	if len(fs.msgs()) != 0 {
		t.Fatalf("injections = %v, want none (judged task is not nudged)", fs.msgs())
	}
}

// TestReconcile_NudgeAfterGrace: a still-running pending judgment past the grace
// window is re-injected (nudge), and the nudge count is bumped.
func TestReconcile_NudgeAfterGrace(t *testing.T) {
	clk := &advClock{t: time.Unix(1_700_000_000, 0)}
	caller := &scriptedInflightCaller{resp: runningResp("task-1")}
	r, fs, store := reconcileRuntime(t, caller, clk)
	store.record("task-1", "JUDGE the delivery for task-1", clk.now())

	// Within grace → no nudge yet.
	r.reconcilePendingJudgments(context.Background(), clk.now())
	if n := len(fs.msgs()); n != 0 {
		t.Fatalf("injections within grace = %d, want 0", n)
	}

	// Past grace + past the reconcile rate-limit → one nudge, re-injecting the prompt.
	clk.advance(pendingNudgeInterval + time.Minute)
	r.reconcilePendingJudgments(context.Background(), clk.now())

	msgs := fs.msgs()
	if len(msgs) != 1 {
		t.Fatalf("injections = %d, want 1 nudge: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "JUDGE the delivery for task-1") {
		t.Fatalf("nudge = %q, want the recorded judgment prompt re-injected", msgs[0])
	}
	if got := store.snapshot()[0].NudgeCount; got != 1 {
		t.Fatalf("nudge count = %d, want 1", got)
	}
}

// TestReconcile_EscalateThenSilent: after the nudge budget is exhausted the reconcile
// sends ONE final escalation nudge, marks the entry escalated (kept for boot-recovery),
// and never nudges again — and NEVER writes task status from Go.
func TestReconcile_EscalateThenSilent(t *testing.T) {
	clk := &advClock{t: time.Unix(1_700_000_000, 0)}
	caller := &scriptedInflightCaller{resp: runningResp("task-1")}
	r, fs, store := reconcileRuntime(t, caller, clk)
	store.record("task-1", "judge me", clk.now())
	// Simulate the budget already spent.
	for i := 0; i < pendingMaxNudges; i++ {
		store.bumpNudge("task-1")
	}

	// Advance well past the (now larger) grace window and reconcile → final escalation.
	clk.advance(time.Duration(pendingMaxNudges+2) * pendingNudgeInterval)
	r.reconcilePendingJudgments(context.Background(), clk.now())

	msgs := fs.msgs()
	if len(msgs) != 1 {
		t.Fatalf("injections = %d, want 1 escalation nudge: %v", len(msgs), msgs)
	}
	if !strings.Contains(msgs[0], "[reminder]") || !strings.Contains(msgs[0], "input_required") {
		t.Fatalf("escalation nudge = %q, want a [reminder] telling the supervisor to resolve/block_task(input_required)", msgs[0])
	}
	snap := store.snapshot()
	if len(snap) != 1 || !snap[0].Escalated {
		t.Fatalf("entry = %+v, want kept + escalated (boot-recovery)", snap)
	}

	// A later reconcile must NOT nudge again (escalated → silent).
	clk.advance(time.Duration(pendingMaxNudges+2) * pendingNudgeInterval)
	r.reconcilePendingJudgments(context.Background(), clk.now())
	if n := len(fs.msgs()); n != 1 {
		t.Fatalf("injections after escalation = %d, want still 1 (silent)", n)
	}
}

// TestReconcile_ReadErrorIsTransient: a center read error must NOT drop pending
// entries (never assume terminal on a failed read).
func TestReconcile_ReadErrorIsTransient(t *testing.T) {
	clk := &advClock{t: time.Unix(1_700_000_000, 0)}
	caller := &scriptedInflightCaller{err: context.DeadlineExceeded}
	r, _, store := reconcileRuntime(t, caller, clk)
	store.record("task-1", "judge me", clk.now())

	r.reconcilePendingJudgments(context.Background(), clk.now())

	if store.len() != 1 {
		t.Fatalf("pending len = %d, want 1 (read error is transient, entry kept)", store.len())
	}
}
