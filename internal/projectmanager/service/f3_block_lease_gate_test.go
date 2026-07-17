package service

import (
	"errors"
	"strings"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// F3 (issue I14 §13.A/§13.B/§2.5) service-layer behaviors: the run-ahead dependency
// gate on Task.blockedBy, the single-active hard constraint at start, BlockTask
// reasonType validation, and the heartbeat lease renewal.

// §13.A: a structured-plan node whose blockedBy upstream is NOT yet done is NOT
// runnable (the run-ahead guard); once the upstream completes it becomes runnable.
// This is the core §13.A correction — the old gate let ANY real-plan node run.
func TestEnsureTaskRunnable_BlockedByUnfinishedUpstream_Rejected(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-dep", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	planID, err := h.svc.CreatePlan(h.ctx, CreatePlanCommand{ProjectID: pid, Name: "dag", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	h.drain(t)
	// §13.A gate test: two assigned nodes in a RUNNING plan + a depends_on edge.
	// T329: a structured-plan node is runnable only when its plan is RUNNING AND its
	// blockedBy deps are satisfied — so the plan is started here, not just drafted.
	up := h.seedAssignedTask(t, pid, planID, "upstream", "user:x")
	down := h.seedAssignedTask(t, pid, planID, "downstream", "user:x")
	// down depends_on up.
	if err := h.svc.AddPlanDependency(h.ctx, planID, down, up, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartPlan(h.ctx, planID, "user:a"); err != nil {
		t.Fatalf("StartPlan: %v", err)
	}
	h.drain(t)

	// Before the upstream is done, the downstream node is `blocked` → NOT runnable
	// (run-ahead rejected), while the upstream (no deps) IS runnable.
	if err := h.svc.EnsureTaskRunnable(h.ctx, down); !errors.Is(err, pm.ErrTaskNotRunnable) {
		t.Fatalf("downstream w/ unfinished upstream = %v, want ErrTaskNotRunnable", err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, up); err != nil {
		t.Fatalf("upstream (no deps) runnable = %v, want nil", err)
	}

	// Complete the upstream → the downstream's blockedBy dependency is satisfied →
	// it becomes runnable.
	if err := h.svc.SetTaskStatus(h.ctx, up, pm.TaskCompleted, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.EnsureTaskRunnable(h.ctx, down); err != nil {
		t.Fatalf("downstream after upstream completed = %v, want nil", err)
	}
}

// §13.B/§13.F-①: single-active for a DEFAULT agent — it may run at most ONE
// non-blocked task at a time. Starting a second running task for the same default
// agent is rejected with pm.ErrAgentHasActiveTask. Pre-v2.18 this was the
// idx_pm_tasks_one_active_per_agent UNIQUE index (migration 0072); v2.18.0 W4c
// dropped that index (0084) and moved the check to the application-layer
// ≤max_concurrent cap (Service.enforceConcurrencyCap), with the default cap of 1
// preserving exactly this single-active behaviour (no regression). Holding several
// CLAIMED (open) pool tasks is fine — only the run is capped.
func TestStartTask_SingleActivePerAgent(t *testing.T) {
	h := planAdvanceSetup(t)
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-sa", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	pool := findBuiltinPlan(t, h, pid)
	addMember(t, h, pid, "agent:w1")
	mk := func(title string) pm.TaskID {
		tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: title, CreatedBy: "user:a"})
		if err != nil {
			t.Fatal(err)
		}
		if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), tid, "user:a"); err != nil {
			t.Fatal(err)
		}
		return tid
	}
	t1, t2 := mk("one"), mk("two")
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatal(err)
	}
	// Claim both (claim→open): holding two claimed pool tasks is allowed.
	if err := h.svc.ClaimPoolTask(h.ctx, t1, "agent:w1"); err != nil {
		t.Fatalf("claim t1: %v", err)
	}
	if err := h.svc.ClaimPoolTask(h.ctx, t2, "agent:w1"); err != nil {
		t.Fatalf("claim t2 (holding two open is fine): %v", err)
	}
	// Start the first → running (one active slot taken).
	if err := h.svc.StartTask(h.ctx, t1, "agent:w1"); err != nil {
		t.Fatalf("start t1: %v", err)
	}
	// Start the second for the SAME agent → single-active violation.
	if err := h.svc.StartTask(h.ctx, t2, "agent:w1"); !errors.Is(err, pm.ErrAgentHasActiveTask) {
		t.Fatalf("start t2 = %v, want ErrAgentHasActiveTask", err)
	}
	// Once the first completes, the slot frees and the second can run.
	if err := h.svc.SetTaskStatus(h.ctx, t1, pm.TaskCompleted, "agent:w1"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.StartTask(h.ctx, t2, "agent:w1"); err != nil {
		t.Fatalf("start t2 after t1 done = %v, want nil", err)
	}
}

// startedPoolTask claims + starts a dispatched pool task for `agent`, returning a
// running task assigned to it (the precondition for block/heartbeat tests).
func startedPoolTask(t *testing.T, h *planAdvanceHarness, org, name, agent string) (pm.ProjectID, pm.TaskID) {
	t.Helper()
	pid, tid := dispatchedPoolTask(t, h, org, name)
	addMember(t, h, pid, pm.IdentityRef(agent))
	if err := h.svc.ClaimPoolTask(h.ctx, tid, pm.IdentityRef(agent)); err != nil {
		t.Fatalf("claim: %v", err)
	}
	if err := h.svc.StartTask(h.ctx, tid, pm.IdentityRef(agent)); err != nil {
		t.Fatalf("start: %v", err)
	}
	return pid, tid
}

// §13.A finding (BlockReasonType.IsValid enforcement): BlockTask rejects an invalid
// reasonType and accepts a valid one, persisting the type.
func TestBlockTask_ValidatesReasonType(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := startedPoolTask(t, h, "org-blk", "P", "agent:w1")

	if err := h.svc.BlockTask(h.ctx, tid, "stuck", pm.BlockReasonType("bogus"), "agent:w1"); !errors.Is(err, pm.ErrInvalidBlockReasonType) {
		t.Fatalf("block w/ invalid type = %v, want ErrInvalidBlockReasonType", err)
	}
	if err := h.svc.BlockTask(h.ctx, tid, "", pm.BlockReasonType(""), "agent:w1"); !errors.Is(err, pm.ErrInvalidBlockReasonType) {
		t.Fatalf("block w/ empty type = %v, want ErrInvalidBlockReasonType", err)
	}
	if err := h.svc.BlockTask(h.ctx, tid, "need a deploy token", pm.BlockReasonInputRequired, "agent:w1"); err != nil {
		t.Fatalf("block w/ valid type = %v, want nil", err)
	}
	got, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.BlockedReasonType() != pm.BlockReasonInputRequired {
		t.Fatalf("blockedReasonType = %q, want input_required", got.BlockedReasonType())
	}
}

// §2.5/§六: HeartbeatTask renews a running task's lease; only the assignee may do it;
// a blocked task (a lease-free legal pause) is rejected with ErrTaskBlocked.
func TestHeartbeatTask_RenewsAndGuards(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := startedPoolTask(t, h, "org-hb", "P", "agent:w1")

	// Non-assignee cannot heartbeat.
	addMember(t, h, mustProjectOf(t, h, tid), "agent:other")
	if err := h.svc.HeartbeatTask(h.ctx, tid, "agent:other"); !errors.Is(err, pm.ErrNotTaskAssignee) {
		t.Fatalf("heartbeat by non-assignee = %v, want ErrNotTaskAssignee", err)
	}

	// Assignee renews the lease (running, non-blocked).
	before, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if before.ExecutionLeaseExpiresAt() == nil {
		t.Fatal("expected a lease set by StartTask")
	}
	if err := h.svc.HeartbeatTask(h.ctx, tid, "agent:w1"); err != nil {
		t.Fatalf("heartbeat by assignee = %v, want nil", err)
	}

	// A blocked task holds no lease → heartbeat is rejected.
	if err := h.svc.BlockTask(h.ctx, tid, "waiting on owner", pm.BlockReasonObstacle, "agent:w1"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.HeartbeatTask(h.ctx, tid, "agent:w1"); !errors.Is(err, pm.ErrTaskBlocked) {
		t.Fatalf("heartbeat on blocked task = %v, want ErrTaskBlocked", err)
	}
}

// T456 (issue-21ba5b78/I30): a lapsed lease is NUDGED, not reclaimed — the task stays
// running with the SAME assignee, the lease is renewed (so it is not re-nudged every
// sweep), and a lease_nudge log + EvtTaskLeaseExpiredNudge are produced. This replaces
// the old reclaim (open + assignee cleared) that silently orphaned an agent's work.
func TestLeaseChecker_NudgesExpiredLease(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := startedPoolTask(t, h, "org-lease", "P", "agent:w1")

	// Before the lease lapses, nothing is nudged.
	if n, err := h.svc.NudgeExpiredLeases(h.ctx); err != nil || n != 0 {
		t.Fatalf("nudge before expiry = (%d,%v), want (0,nil)", n, err)
	}
	// Advance past the lease TTL → the stalled agent's task is nudged (NOT reclaimed).
	h.clk.Advance(DefaultExecutionLeaseTTL + time.Minute)
	n, err := h.svc.NudgeExpiredLeases(h.ctx)
	if err != nil || n != 1 {
		t.Fatalf("nudge after expiry = (%d,%v), want (1,nil)", n, err)
	}
	got, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	// The anti-orphan invariant: still running, same assignee, lease renewed (not cleared).
	if got.Status() != pm.TaskRunning {
		t.Fatalf("status=%s, want running (nudged, never reclaimed)", got.Status())
	}
	if got.Assignee() != "agent:w1" {
		t.Fatalf("assignee=%q, want unchanged agent:w1 (owner never changes)", got.Assignee())
	}
	exp := got.ExecutionLeaseExpiresAt()
	if exp == nil || !exp.After(h.clk.Now()) {
		t.Fatalf("lease should be RENEWED (future), got %v at now=%v", exp, h.clk.Now())
	}
	// A lease_nudge action-log entry was persisted (attributed to system, agent=assignee).
	logs, err := h.actionLogs.ListByTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	var sawNudge bool
	for _, lg := range logs {
		if lg.Action == pm.TaskActionLeaseNudged {
			sawNudge = true
			if lg.ActorRef != "system" || lg.AgentRef != "agent:w1" {
				t.Fatalf("lease_nudge log wrong: %+v", lg)
			}
		}
	}
	if !sawNudge {
		t.Fatalf("expected a lease_nudge action-log entry, logs=%+v", logs)
	}
	// Renewing the lease rate-limits re-nudges: an immediate second sweep nudges nothing.
	if n, _ := h.svc.NudgeExpiredLeases(h.ctx); n != 0 {
		t.Fatalf("second sweep = %d, want 0 (lease renewed by the nudge)", n)
	}
}

// A heartbeat within the TTL keeps the lease alive, so the checker does NOT nudge a
// task whose agent is still alive (the long-build/test acceptance case).
func TestLeaseChecker_HeartbeatPreventsNudge(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := startedPoolTask(t, h, "org-lease2", "P", "agent:w1")

	// Advance to just before expiry, then heartbeat → lease extended by a fresh TTL.
	h.clk.Advance(DefaultExecutionLeaseTTL - time.Minute)
	if err := h.svc.HeartbeatTask(h.ctx, tid, "agent:w1"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	// Advance a little (well under the renewed TTL) → still alive, not nudged.
	h.clk.Advance(2 * time.Minute)
	if n, err := h.svc.NudgeExpiredLeases(h.ctx); err != nil || n != 0 {
		t.Fatalf("nudge after heartbeat = (%d,%v), want (0,nil) (lease renewed)", n, err)
	}
	got, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != pm.TaskRunning {
		t.Fatalf("status=%s, want running (heartbeat kept it alive)", got.Status())
	}
}

// WorkerRenewLease (T456 P0 #1): the worker's process-alive renew keeps a live
// session's task lease alive without the agent calling heartbeat, and is a safe no-op
// for a stale/foreign assignee.
func TestWorkerRenewLease(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := startedPoolTask(t, h, "org-wrl", "P", "agent:w1")

	// A foreign agentRef must NOT renew the task's lease.
	before, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if revoked, reason, err := h.svc.WorkerRenewLease(h.ctx, tid, "agent:someone-else"); err != nil || !revoked || reason != "reassigned" {
		t.Fatalf("WorkerRenewLease(foreign) = (revoked=%v reason=%q err=%v), want (true,\"reassigned\",nil) no-renew", revoked, reason, err)
	}
	mid, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ExecutionLeaseExpiresAt().Equal(*mid.ExecutionLeaseExpiresAt()) {
		t.Fatalf("foreign renew must not touch the lease: %v -> %v",
			before.ExecutionLeaseExpiresAt(), mid.ExecutionLeaseExpiresAt())
	}

	// The assignee's worker renews → lease pushed out to now+TTL, keeping it alive past
	// what would have been the original expiry.
	h.clk.Advance(DefaultExecutionLeaseTTL - time.Minute)
	if revoked, _, err := h.svc.WorkerRenewLease(h.ctx, tid, "agent:w1"); err != nil || revoked {
		t.Fatalf("WorkerRenewLease(assignee) = (revoked=%v err=%v), want a live renew", revoked, err)
	}
	h.clk.Advance(2 * time.Minute) // past the ORIGINAL expiry, within the renewed TTL
	if n, err := h.svc.NudgeExpiredLeases(h.ctx); err != nil || n != 0 {
		t.Fatalf("nudge after worker-renew = (%d,%v), want (0,nil)", n, err)
	}
	got, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != pm.TaskRunning || got.Assignee() != "agent:w1" {
		t.Fatalf("status=%s assignee=%q, want running/agent:w1", got.Status(), got.Assignee())
	}
}

// WorkerRenewLease_BlockedRevokes (issue-88e32d98 P0 block-fuse): a task blocked
// mid-flight (ADR-0046: status stays running, blocked_reason set) makes the worker's
// renew return revoked="blocked" — the signal the worker uses to circuit-break the
// still-running executor instead of letting it race on to a dangerous action.
func TestWorkerRenewLease_BlockedRevokes(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := startedPoolTask(t, h, "org-wrl-blk", "P", "agent:w1")

	// Sanity: a live task renews cleanly (not revoked).
	if revoked, _, err := h.svc.WorkerRenewLease(h.ctx, tid, "agent:w1"); err != nil || revoked {
		t.Fatalf("pre-block renew = (revoked=%v err=%v), want a live renew", revoked, err)
	}

	if err := h.svc.BlockTask(h.ctx, tid, "waiting on owner", pm.BlockReasonObstacle, "agent:w1"); err != nil {
		t.Fatalf("BlockTask: %v", err)
	}

	// The block must revoke — this is the熔断 signal (P67 Ship 事故: the executor kept
	// running through a block and merged dead code).
	revoked, reason, err := h.svc.WorkerRenewLease(h.ctx, tid, "agent:w1")
	if err != nil {
		t.Fatalf("WorkerRenewLease(blocked) err = %v", err)
	}
	if !revoked || reason != "blocked" {
		t.Fatalf("WorkerRenewLease(blocked) = (revoked=%v reason=%q), want (true,\"blocked\")", revoked, reason)
	}
}

// §7.3: the log-producing flows persist the append-only lifecycle log to
// pm_task_action_logs. Blocking then unblocking a task records both entries.
func TestActionLog_BlockUnblockPersisted(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := startedPoolTask(t, h, "org-log", "P", "agent:w1")

	if err := h.svc.BlockTask(h.ctx, tid, "need a token", pm.BlockReasonObstacle, "agent:w1"); err != nil {
		t.Fatal(err)
	}
	if err := h.svc.UnblockTask(h.ctx, UnblockTaskCommand{TaskID: tid, Actor: "agent:w1"}); err != nil {
		t.Fatal(err)
	}
	logs, err := h.actionLogs.ListByTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	var sawBlocked, sawUnblocked bool
	for _, lg := range logs {
		switch lg.Action {
		case pm.TaskActionBlocked:
			sawBlocked = true
		case pm.TaskActionUnblocked:
			sawUnblocked = true
		}
	}
	if !sawBlocked || !sawUnblocked {
		t.Fatalf("action logs = %+v, want both blocked and unblocked entries", logs)
	}
}

// §13.D: the overdue-blocked reminder emits ONE reminder per block episode once the
// block outlives the threshold, never reclaims the task, and re-arms after unblock.
func TestOverdueBlockedReminder_EmitsOncePerEpisode(t *testing.T) {
	h := planAdvanceSetup(t)
	_, tid := startedPoolTask(t, h, "org-od", "P", "agent:w1")
	if err := h.svc.BlockTask(h.ctx, tid, "waiting on owner", pm.BlockReasonObstacle, "agent:w1"); err != nil {
		t.Fatal(err)
	}
	chk := NewOverdueBlockedReminder(h.svc, h.clk, time.Hour, time.Minute, nil)

	// Not overdue yet → no reminder.
	if n, err := chk.Tick(h.ctx); err != nil || n != 0 {
		t.Fatalf("tick before threshold = (%d,%v), want (0,nil)", n, err)
	}
	// Past the threshold → exactly one reminder.
	h.clk.Advance(time.Hour + time.Minute)
	if n, err := chk.Tick(h.ctx); err != nil || n != 1 {
		t.Fatalf("tick after threshold = (%d,%v), want (1,nil)", n, err)
	}
	// Latched: a second sweep does not re-remind the same episode.
	if n, _ := chk.Tick(h.ctx); n != 0 {
		t.Fatalf("second tick = %d, want 0 (latched)", n)
	}
	// The reminder NEVER recovers a blocked task (§13.D: not auto-recovered) — it only
	// reminds. ADR-0054: the task must stay PARKED (status=blocked) with its reason intact;
	// the reminder must not un-park it, and (the ADR-0054 regression this guards) the sweep
	// must still FIND it now that a blocked task no longer sits in `running`.
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.Status() != pm.TaskBlocked || strings.TrimSpace(got.BlockedReason()) == "" {
		t.Fatalf("task should stay parked (blocked) with its reason, got status=%s blocked=%q", got.Status(), got.BlockedReason())
	}

	// Unblock → latch pruned; a NEW block episode re-arms the reminder.
	if err := h.svc.UnblockTask(h.ctx, UnblockTaskCommand{TaskID: tid, Actor: "agent:w1"}); err != nil {
		t.Fatal(err)
	}
	if n, _ := chk.Tick(h.ctx); n != 0 {
		t.Fatalf("tick after unblock = %d, want 0 (not blocked)", n)
	}
	if err := h.svc.BlockTask(h.ctx, tid, "stuck again", pm.BlockReasonObstacle, "agent:w1"); err != nil {
		t.Fatal(err)
	}
	h.clk.Advance(time.Hour + time.Minute)
	if n, _ := chk.Tick(h.ctx); n != 1 {
		t.Fatalf("tick after re-block+overdue = %d, want 1 (fresh episode)", n)
	}
}

// mustProjectOf returns the project id of a task (test helper).
func mustProjectOf(t *testing.T, h *planAdvanceHarness, tid pm.TaskID) pm.ProjectID {
	t.Helper()
	task, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	return task.ProjectID()
}
