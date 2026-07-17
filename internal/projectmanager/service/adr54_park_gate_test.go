package service

import (
	"errors"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// adr54_park_gate_test.go — the ADR-0054 (I107) service-level acceptance: a PARK must
// really stop dispatch, must free the agent, and must not be reaped as concluded work.
//
// These assert the GATES rather than the domain transitions (state_machine_adr54_test.go
// covers those). The distinction is the whole point of the issue: under ADR-0046 the
// domain was arguably self-consistent — block wrote a reason and something was supposed to
// notice — and the real failure was that every dispatch gate kept answering
// "runnable / not-freed / actionable" for a parked task. The gate answers ARE the contract.
//
// The harness (planAdvanceSetup / startedPoolTask) gives a task that is claimed, started,
// and genuinely runnable through the real built-in-pool path — so "not runnable" below
// means the park closed the gate, not that the fixture was never open.

const parkAgent = "agent:w1"

// TestADR54_ParkStopsDispatch is the ② load-bearer. EnsureTaskRunnable is the ONE
// chokepoint every re-drive funnels through (ListRunnableAgentTasks, the wake re-push, the
// 60s sweep), and a "yes" there is what forks a fresh empty-context executor onto work that
// is already delivered or deliberately paused.
func TestADR54_ParkStopsDispatch(t *testing.T) {
	t.Run("running is runnable (precondition, and the anti-over-reach guard)", func(t *testing.T) {
		h := planAdvanceSetup(t)
		_, tid := startedPoolTask(t, h, "org-1", "P", parkAgent)
		if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
			t.Fatalf("a healthy running task must stay runnable — park must not be over-broad: %v", err)
		}
	})

	t.Run("blocked", func(t *testing.T) {
		h := planAdvanceSetup(t)
		_, tid := startedPoolTask(t, h, "org-1", "P", parkAgent)
		if err := h.svc.BlockTask(h.ctx, tid, "needs a decision", pm.BlockReasonInputRequired, parkAgent); err != nil {
			t.Fatal(err)
		}
		assertParkedAndUndispatchable(t, h, tid, pm.TaskBlocked)
	})

	t.Run("delivered", func(t *testing.T) {
		h := planAdvanceSetup(t)
		_, tid := startedPoolTask(t, h, "org-1", "P", parkAgent)
		if err := h.svc.DeliverTask(h.ctx, tid, "pushed feat/x @ abc123", parkAgent); err != nil {
			t.Fatal(err)
		}
		assertParkedAndUndispatchable(t, h, tid, pm.TaskDelivered)
		// A delivery is NOT an alarm: it must not write a blocked_reason, or the Alerts rail
		// and the overdue-block escalation would page a human about healthy finished work.
		tk, _ := h.svc.GetTask(h.ctx, tid)
		if tk.BlockedReason() != "" {
			t.Fatalf("deliver must not write a blocked_reason, got %q", tk.BlockedReason())
		}
	})
}

// TestADR54_ParkFreesTheAgentsRunSlot: a parked task holds no run slot. The executor is
// gone, so pinning the agent behind a queue it cannot advance would strand a live agent —
// `delivered` frees the slot for exactly the reason `blocked` does. AgentFreedFromTask is
// the re-push trigger's predicate, so this is also what hands the agent its next task.
func TestADR54_ParkFreesTheAgentsRunSlot(t *testing.T) {
	for _, tc := range []struct {
		name string
		park func(h *planAdvanceHarness, tid pm.TaskID) error
	}{
		{"blocked", func(h *planAdvanceHarness, tid pm.TaskID) error {
			return h.svc.BlockTask(h.ctx, tid, "stuck", pm.BlockReasonObstacle, parkAgent)
		}},
		{"delivered", func(h *planAdvanceHarness, tid pm.TaskID) error {
			return h.svc.DeliverTask(h.ctx, tid, "done", parkAgent)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := planAdvanceSetup(t)
			_, tid := startedPoolTask(t, h, "org-1", "P", parkAgent)

			// A plain running task does NOT free the slot (a fresh start is not a hand-off).
			if freed, err := h.svc.AgentFreedFromTask(h.ctx, tid); err != nil || freed {
				t.Fatalf("a running task must not free the slot: freed=%v err=%v", freed, err)
			}
			if err := tc.park(h, tid); err != nil {
				t.Fatal(err)
			}
			if freed, err := h.svc.AgentFreedFromTask(h.ctx, tid); err != nil || !freed {
				t.Fatalf("a %s task must free the agent's run slot: freed=%v err=%v", tc.name, freed, err)
			}
		})
	}
}

// TestADR54_ParkedIsStillActiveWork is the 命门-1 guard at the service level: a parked task
// must NOT be treated as concluded. It stays non-terminal and stays in the agent's
// assigned/active set — dropping it would make the board look emptier than it is and hide
// exactly the tasks that need a human.
func TestADR54_ParkedIsStillActiveWork(t *testing.T) {
	for _, tc := range []struct {
		name string
		park func(h *planAdvanceHarness, tid pm.TaskID) error
	}{
		{"blocked", func(h *planAdvanceHarness, tid pm.TaskID) error {
			return h.svc.BlockTask(h.ctx, tid, "stuck", pm.BlockReasonObstacle, parkAgent)
		}},
		{"delivered", func(h *planAdvanceHarness, tid pm.TaskID) error {
			return h.svc.DeliverTask(h.ctx, tid, "done", parkAgent)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := planAdvanceSetup(t)
			_, tid := startedPoolTask(t, h, "org-1", "P", parkAgent)
			if err := tc.park(h, tid); err != nil {
				t.Fatal(err)
			}

			tk, _ := h.svc.GetTask(h.ctx, tid)
			if tk.Status().IsTerminal() {
				t.Fatalf("a %s task must not be terminal (that is the false green / reap ADR-0054 exists to abolish)", tc.name)
			}
			active, err := h.svc.ListAssignedAgentTasks(h.ctx, parkAgent)
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, a := range active {
				if a.ID() == tid {
					found = true
				}
			}
			if !found {
				t.Fatalf("a %s task must stay in the agent's assigned/active set — it is active work, not history", tc.name)
			}
		})
	}
}

// TestADR54_DeliveredAcceptanceExits: the delivered state's two service-level exits. Accept
// (CompleteTask) concludes it; reject (ReworkTask) returns it to its assignee as running
// AND re-opens the dispatch gate. Together they are why `delivered` cannot strand work.
func TestADR54_DeliveredAcceptanceExits(t *testing.T) {
	t.Run("accept", func(t *testing.T) {
		h := planAdvanceSetup(t)
		_, tid := startedPoolTask(t, h, "org-1", "P", parkAgent)
		if err := h.svc.DeliverTask(h.ctx, tid, "done", parkAgent); err != nil {
			t.Fatal(err)
		}
		if err := h.svc.CompleteTask(h.ctx, tid, parkAgent); err != nil {
			t.Fatalf("complete_task must ACCEPT a delivered task: %v", err)
		}
		tk, _ := h.svc.GetTask(h.ctx, tid)
		if tk.Status() != pm.TaskCompleted {
			t.Fatalf("accept → completed, got %s", tk.Status())
		}
	})

	t.Run("reject returns it to the assignee and re-opens dispatch", func(t *testing.T) {
		h := planAdvanceSetup(t)
		_, tid := startedPoolTask(t, h, "org-1", "P", parkAgent)
		if err := h.svc.DeliverTask(h.ctx, tid, "done", parkAgent); err != nil {
			t.Fatal(err)
		}
		if err := h.svc.ReworkTask(h.ctx, tid, "tests fail on CI", parkAgent); err != nil {
			t.Fatalf("rework_task must REJECT a delivered task: %v", err)
		}
		tk, _ := h.svc.GetTask(h.ctx, tid)
		if tk.Status() != pm.TaskRunning {
			t.Fatalf("reject → running, got %s", tk.Status())
		}
		if tk.Assignee() != pm.IdentityRef(parkAgent) {
			t.Fatalf("reject must keep the assignee (the work returns to THEM), got %q", tk.Assignee())
		}
		if tk.BlockedComment() != "tests fail on CI" {
			t.Fatalf("reject must hand the note back via blocked_comment, got %q", tk.BlockedComment())
		}
		// A reject must not strand the task: it is dispatchable again.
		if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
			t.Fatalf("a reworked task must be runnable again, got %v", err)
		}
	})
}

// TestADR54_UnblockRestoresDispatch closes the ② loop: park stops dispatch, and unblock —
// the recovery door — restores it. A park that could not be undone would just be the T16
// deadlock ADR-0046 removed, wearing a new hat.
func TestADR54_UnblockRestoresDispatch(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := startedPoolTask(t, h, "org-1", "P", parkAgent)
	if err := h.svc.BlockTask(h.ctx, tid, "needs key", pm.BlockReasonObstacle, parkAgent); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("precondition: a blocked task must not be runnable, got %v", err)
	}
	if err := h.svc.UnblockTask(h.ctx, UnblockTaskCommand{TaskID: tid, Actor: parkAgent}); err != nil {
		t.Fatal(err)
	}
	tk, _ := h.svc.GetTask(h.ctx, tid)
	if tk.Status() != pm.TaskRunning {
		t.Fatalf("unblock must un-park to running, got %s", tk.Status())
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, tid); err != nil {
		t.Fatalf("an unblocked task must be runnable again, got %v", err)
	}
}

func assertParkedAndUndispatchable(t *testing.T, h *planAdvanceHarness, tid pm.TaskID, want pm.TaskStatus) {
	t.Helper()
	tk, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status() != want {
		t.Fatalf("status = %s, want %s", tk.Status(), want)
	}
	// THE gate: every re-drive path funnels through EnsureTaskRunnable.
	if rerr := h.svc.EnsureTaskRunnable(h.ctx, tid); !errors.Is(rerr, pm.ErrTaskNotRunnable) {
		t.Fatalf("a %s task must NOT be runnable (a yes here forks a fresh executor onto parked work), got %v", want, rerr)
	}
	// The agent-facing "what can I start now" feed must not offer it either — a second,
	// independent gate, deliberately redundant with the one above.
	runnable, err := h.svc.ListRunnableAgentTasks(h.ctx, parkAgent)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runnable {
		if r.ID() == tid {
			t.Fatalf("a %s task must not appear in the agent's runnable feed", want)
		}
	}
}
