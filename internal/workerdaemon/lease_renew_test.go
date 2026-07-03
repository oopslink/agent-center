package workerdaemon

import (
	"context"
	"testing"
	"time"
)

// T456 P0 #1: OnTick renews the execution lease for a LIVE session's current task —
// the process-alive auto-renew that keeps a long build/test from letting the lease
// lapse, decoupled from the agent's LLM turn.
func TestLeaseRenew_OnTickRenewsLiveSessionTask(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, rep, _ := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	// Bring up a live session, then mark it as running task-1.
	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	withAgentState(c, "agent-1", func() { c.agents["agent-1"].state.CurrentTaskID = "task-1" })

	c.OnTick(context.Background())

	got := rep.leaseRenewCalls()
	if len(got) != 1 || got[0].agentID != "agent-1" || got[0].taskID != "task-1" {
		t.Fatalf("want one renew of agent-1/task-1, got %+v", got)
	}
}

// The sweep skips sessions with NO current task (idle/converse-only) and managed
// agents with NO live session (dead — the self-heal path owns that recovery, not the
// lease renew). A superseded old session is dropped the same way: it is not in the
// current agents map.
func TestLeaseRenew_SkipsIdleAndDeadSessions(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, rep, _ := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	// Live session but NO current task → skip.
	if err := c.Handle(context.Background(), reconcileCmd(t, "idle-ag", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	// A managed agent with a current task but a DEAD session (session nil) → skip.
	st := c.installTestAgent("dead-ag")
	withAgentState(c, "dead-ag", func() { st.CurrentTaskID = "task-x" })

	c.OnTick(context.Background())

	if got := rep.leaseRenewCalls(); len(got) != 0 {
		t.Fatalf("idle + dead sessions must not be renewed, got %+v", got)
	}
}

// A session being intentionally torn down (expectedStop / detaching) is NOT renewed —
// its lease is allowed to lapse.
func TestLeaseRenew_SkipsStoppingSession(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, rep, _ := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	withAgentState(c, "agent-1", func() {
		c.agents["agent-1"].state.CurrentTaskID = "task-1"
		c.agents["agent-1"].state.ExpectedStop = true
	})

	c.OnTick(context.Background())
	if got := rep.leaseRenewCalls(); len(got) != 0 {
		t.Fatalf("a stopping session must not be renewed, got %+v", got)
	}
}

// The sweep is rate-limited to cfg.LeaseRenewEvery: a second OnTick within the window
// does NOT re-renew; advancing past the window renews again.
func TestLeaseRenew_RateLimited(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_000_000, 0)}
	c, rep, _ := newTestController(t, t.TempDir())
	c.cfg.Now = clock.now
	c.cfg.LeaseRenewEvery = 60 * time.Second
	defer c.Shutdown(context.Background())

	if err := c.Handle(context.Background(), reconcileCmd(t, "agent-1", "running", 1, "", 1)); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	withAgentState(c, "agent-1", func() { c.agents["agent-1"].state.CurrentTaskID = "task-1" })

	c.OnTick(context.Background()) // renews
	c.OnTick(context.Background()) // within the window → no renew
	if got := rep.leaseRenewCalls(); len(got) != 1 {
		t.Fatalf("second tick within the window must not re-renew, got %d", len(got))
	}

	clock.t = clock.t.Add(61 * time.Second)
	c.OnTick(context.Background()) // window elapsed → renews again
	if got := rep.leaseRenewCalls(); len(got) != 2 {
		t.Fatalf("after the window a renew must fire again, got %d", len(got))
	}
}
