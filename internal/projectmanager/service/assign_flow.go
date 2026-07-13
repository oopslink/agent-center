package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Assign/state-flow AppServices (B2-c). All write ONLY ProjectManager state +
// an outbox event in one tx (OQ1). The cross-BC effects — creating/superseding
// AgentWorkItems and syncing Conversation participants — are handled by the
// projectors (ParticipantProjector + WorkItemProjector) consuming these events.

// AssignTask assigns (or reassigns) a Task to an identity. open→assigned, or
// re-target an already-assigned Task. On reassign the previous assignee leaves
// the effective subscriber set unless they are creator or a manual subscriber
// (falls out of EffectiveTaskSubscribers automatically). Emits pm.task.assigned
// (first assignment) or pm.task.reassigned.
func (s *Service) AssignTask(ctx context.Context, taskID pm.TaskID, assignee, actor pm.IdentityRef) error {
	if err := assignee.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject (re)assign on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		// T130 note (指派 vs 可运行): assignment is DELIBERATELY decoupled from
		// runnability. Assigning a task — even a backlog one — only records ownership
		// (and grants the agent project membership); it is a legitimate, widely-used
		// operation (e.g. assign before planning). What is gated is ENTERING RUNNING:
		// a backlog task is rejected at the open→running boundary — claim (T83) and
		// now start_work (the TaskRunGate, ErrTaskNotRunnable) — NOT at
		// assign. So a directly-assigned backlog task can sit assigned but cannot run
		// until it is added to a real plan or dispatched into the Assignment Pool.
		prev := t.Assignee()
		if err := t.Assign(assignee, now); err != nil {
			return err
		}
		// #5a (ADR-0049/0052/OQ6): when the assignee is an AGENT, grant it
		// ProjectMember so it can pass the project write-gate for its MCP tools
		// (OQ4 = agents have project-level write). Cross-org-guarded + idempotent,
		// and in THIS tx so the assignment and membership commit atomically. Human
		// (`user:`) assignees are untouched.
		if err := s.grantAgentProjectMembership(txCtx, t, assignee, now); err != nil {
			return err
		}
		// OQ13: reassigning away keeps the outgoing assignee in the Conversation
		// as a sticky manual subscriber (not the new assignee, which is effective
		// via the assignee role). Must persist before emit so the recomputed
		// effective set includes them.
		if prev != "" && prev != assignee {
			if err := s.retainAsTaskSubscriber(txCtx, t, prev, now); err != nil {
				return err
			}
		}
		// v2.18.0 W4c ③: reassigning a task that is ALREADY running+unblocked (e.g.
		// the crash-adopt / hand-off回流 path) moves its run slot onto the NEW assignee
		// — the dropped single-active index used to reject this when the new owner was
		// already at its slot. Re-check the cap for the post-reassign owner. Self-skips
		// for the common assign-of-non-running case (open/blocked/terminal).
		if err := s.enforceConcurrencyCap(txCtx, t); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		evt := EvtTaskAssigned
		if prev != "" {
			evt = EvtTaskReassigned
		}
		if err := s.emitTaskAssignEvent(txCtx, t, evt, string(prev)); err != nil {
			return err
		}
		// audit §5: first assignment vs reassignment (from→to on the assignee field).
		changeType := pm.AuditTaskAssigned
		if prev != "" {
			changeType = pm.AuditTaskReassigned
		}
		s.auditTaskAssign(txCtx, t, changeType, prev, actor)
		// BUG B (v2.9 assign-after-select): if this task is already in a plan,
		// emit pm.plan.participants_changed so the NEW assignee additively joins the
		// Plan conversation (#284). SelectTaskIntoPlan only syncs the assignee that
		// existed AT SELECT TIME; an agent assigned LATER would otherwise be left out
		// of the plan conversation → the dispatch @mention (BUG C) can't reach it.
		return s.syncPlanParticipantOnAssign(txCtx, t, assignee)
	})
}

// syncPlanParticipantOnAssign emits pm.plan.participants_changed for the task's
// plan (if any) with `assignee` as an ADD-ONLY participant delta — mirroring
// SelectTaskIntoPlan's emit (plan_flow.go) so the assign-after-select sequence
// (task selected first, agent assigned later) still adds the agent to the Plan
// conversation (#284, additive). A task not in a plan (PlanID == "") or an empty
// assignee is a no-op (nothing to sync). Runs in the AssignTask tx.
func (s *Service) syncPlanParticipantOnAssign(ctx context.Context, t *pm.Task, assignee pm.IdentityRef) error {
	planID := t.PlanID()
	if planID == "" || strings.TrimSpace(string(assignee)) == "" {
		return nil
	}
	return s.emit(ctx, EvtPlanParticipantsChanged,
		refsJSON(map[string]string{"plan_id": string(planID), "project_id": string(t.ProjectID())}),
		planEventPayload{
			PlanID: string(planID), ProjectID: string(t.ProjectID()),
			OrganizationID: "", OwnerRef: "pm://plans/" + string(planID),
			Participants: []string{string(assignee)},
		})
}

// DefaultExecutionLeaseTTL is the v2.14.0 I14 §2.5 execution-lease window granted
// on start_task and extended by each heartbeat. A running task whose lease lapses
// (the agent died without completing/blocking) is reclaimed to `open` by the
// lease-checker (Task.ExpireLease) — the replacement for the deleted
// AgentWorkItem.FailFromAgentDeath.
//
// T455: widened 15m → 5h. The 15m window was far too tight for a live agent that
// goes heads-down in a long build/run without calling heartbeat — it got reclaimed
// mid-work, and because reclaim clears the assignee + the dispatch record is
// permanent, a structured-plan node ends up stranded (claimable=false, only manual
// assign_task recovers it). A 5h window gives any realistic long-running task ample
// headroom before the lease is presumed dead, while still freeing a truly-dead
// agent's task the same day. (The deeper carry-ownership reclaim fix is tracked
// separately; this is the immediate mitigation.)
const DefaultExecutionLeaseTTL = 5 * time.Hour

// StartTask moves an assigned Task to running (the explicit "picked up/started"
// transition — needed before block/complete are reachable) and GRANTS the execution
// lease (v2.14.0 I14/F3 §2.5; the agent's heartbeat extends it, the lease-checker
// reclaims it on lapse).
//
// §13.A run-ahead gate: the dependency gate (EnsureTaskRunnable, now rewritten to the
// Task.blockedBy model) is enforced on the AGENT start path — start_work, via the
// agentsvc.TaskRunGate port — which is where an agent actually starts a dispatched
// task. StartTask itself is the owner/console "mark running" action (handlers_pm.go)
// and stays ungated, matching its pre-F3 behavior. The open→running UPDATE is where
// the §13.B single-active UNIQUE index bites: if the agent already has a running,
// non-blocked task the repo returns pm.ErrAgentHasActiveTask (one running task/agent).
func (s *Service) StartTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject start on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status()
		if err := t.Start(now); err != nil {
			return err
		}
		// §2.5: grant the execution lease on start.
		if err := t.RenewLease(DefaultExecutionLeaseTTL, now); err != nil {
			return err
		}
		// v2.18.0 W4c: the application-layer ≤max_concurrent run-slot cap that
		// replaced the dropped single-active UNIQUE index (migration 0072→0084).
		// MUST run in THIS tx, before Update — race-safety is the whole-tx replay.
		if err := s.enforceConcurrencyCap(txCtx, t); err != nil {
			return err // pm.ErrAgentHasActiveTask when the agent is at its run-slot cap
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		if err := s.emitTaskStateChanged(txCtx, t, prevStatus, ""); err != nil {
			return err
		}
		// audit §5: StartTask has its own tx (not via taskStateOp) — record here.
		s.auditTaskStatusChange(txCtx, t, prevStatus, actor)
		return nil
	})
}

// enforceConcurrencyCap is the application-layer run-slot guard that REPLACES the
// per-agent single-active UNIQUE index (migration 0072, dropped by 0084) — a UNIQUE
// index can only express ≤1, never the per-agent ≤max_concurrent_tasks this feature
// needs (v2.18.0 W4c, issue-b8687f2a). It MUST be called inside the transition tx,
// after the in-memory state mutation (so t already carries the post-transition
// assignee/status/blocked_reason) and immediately before s.tasks.Update — every
// task→running entry point routes through it (start_task, unblock→running,
// owner SetStatus→running, reassign-of-running), so no path can re-introduce a
// second active task behind the dropped index.
//
// Predicate = the dropped index's WHERE verbatim: the guard applies ONLY when the
// post-transition row occupies a RUN SLOT (status='running' AND blocked_reason
// empty). A task that is leaving running, or staying blocked, frees/never-takes a
// slot and is exempt (return nil). The cap is the assignee's EffectiveConcurrencyCap
// (1 for a default/non-agent assignee — single-active, no regression; the agent's
// EffectiveMaxConcurrentTasks when concurrency is opted in). It then counts the
// assignee's OTHER live run-slots (excluding this task) and rejects with
// pm.ErrAgentHasActiveTask when admitting this one would exceed the cap.
//
// RACE-SAFETY (dual-engine, W4c #3): on SQLite the count read + the Update write
// share this tx; a concurrent committed start invalidates the read snapshot, SQLite
// raises BUSY_SNAPSHOT(517), and persistence.RunInTx replays the WHOLE tx so the
// count is re-read fresh — N+1 concurrent starts therefore admit exactly N and the
// (N+1)th re-reads count==cap and is cleanly rejected (no cap breakthrough). This is
// the same race-safe idiom as the claim holding-cap (ClaimPoolTask), and needs no
// BEGIN IMMEDIATE. On a Postgres backend the identical invariant is taken with a
// `SELECT count(*) ... FOR UPDATE` row lock in the same tx (serializing the
// concurrent counters) rather than snapshot-replay; the count query and guard are
// written to port cleanly to either. Only SQLite is wired today (the repo has no PG
// driver), so the SQLite path is the implemented+tested one and the PG path is the
// documented equivalent.
func (s *Service) enforceConcurrencyCap(ctx context.Context, t *pm.Task) error {
	// Only a row that WILL occupy a run slot is capped (dropped-index predicate).
	if t.Status() != pm.TaskRunning || t.BlockedReason() != "" {
		return nil
	}
	assignee := t.Assignee()
	if assignee == "" {
		return nil // unassigned: nothing owns a slot to cap
	}
	slotCap := s.concurrencyCapOf(ctx, assignee)
	others, err := s.tasks.CountRunningUnblockedByAssignee(ctx, assignee, t.ID())
	if err != nil {
		return err
	}
	if others+1 > slotCap {
		return pm.ErrAgentHasActiveTask
	}
	return nil
}

// concurrencyCapOf resolves the run-slot cap for an assignee. An AGENT assignee's
// cap comes from the AgentDirectory (single source: Profile.EffectiveConcurrencyCap,
// shared with the daemon gate); a non-agent (`user:`) assignee, a nil directory, or
// any directory error fall back to 1 — single-active, matching the dropped index
// which applied to every assignee. Fail-safe: a directory hiccup can only ever make
// the cap STRICTER (1), never leak extra slots.
func (s *Service) concurrencyCapOf(ctx context.Context, assignee pm.IdentityRef) int {
	if s.agentDir == nil || !strings.HasPrefix(string(assignee), "agent:") {
		return 1
	}
	slotCap, err := s.agentDir.ConcurrencyCapOfAgent(ctx, strings.TrimPrefix(string(assignee), "agent:"))
	if err != nil || slotCap < 1 {
		return 1
	}
	return slotCap
}

// CountRunningTasks reports how many RUN SLOTS the assignee currently occupies
// (running, non-blocked tasks) — the observability counterpart of the ≤N cap
// (v2.18.0 W4c #4): callers compare it against the agent's EffectiveConcurrencyCap
// to see "running R / cap N". A blocked task is a legal pause and is NOT counted.
func (s *Service) CountRunningTasks(ctx context.Context, assignee pm.IdentityRef) (int, error) {
	return s.tasks.CountRunningUnblockedByAssignee(ctx, assignee, "")
}

// SoleRunningTask returns the assignee's running task IFF it has EXACTLY ONE
// running-unblocked task — otherwise (nil, nil). It is the authoritative source
// for the report_usage task-attribution fallback (issue-af03da2f / I54): a per-turn
// usage report that arrives with an empty task_id (the agent self-manages its queue
// via MCP, so the daemon never learned the current task_id) is attributed to the
// agent's running task here, where the center is the running-task authority.
//
// "Exactly one" is the deliberate guard. Zero running tasks → the turn is converse
// / idle (non-task overhead) and MUST stay unattributed (empty). MORE than one (a
// ≤max_concurrent concurrency agent, v2.18.0 W4c) → ambiguous: the center cannot
// know which of the N the tokens belong to, so it abstains rather than mis-attribute
// (the per-executor source binding is the correct fix for that path). A blocked task
// is a legal pause that frees its slot and is excluded — same RUN-SLOT predicate as
// the concurrency cap.
func (s *Service) SoleRunningTask(ctx context.Context, assignee pm.IdentityRef) (*pm.Task, error) {
	if strings.TrimSpace(string(assignee)) == "" {
		return nil, nil
	}
	running, err := s.tasks.ListRunningUnblockedByAssignee(ctx, assignee)
	if err != nil {
		return nil, err
	}
	if len(running) != 1 {
		return nil, nil // 0 = converse/idle; >1 = ambiguous — abstain either way
	}
	return running[0], nil
}

// HeartbeatTask extends the running agent's execution lease (v2.14.0 I14 §2.5 / §六,
// MCP `heartbeat`). Only the assignee may renew, and only a RUNNING, non-blocked task
// has a live lease to keep alive: a blocked task is a lease-free legal pause
// (pm.ErrTaskBlocked) and a non-running task has no execution (ErrIllegalTransition).
// It is a lease-only touch (no status change), so it emits no state_changed event —
// the renewed deadline is purely the lease-checker's input.
func (s *Service) HeartbeatTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		// Only the assignee agent heartbeats its own running task.
		if t.Assignee() != actor {
			return pm.ErrNotTaskAssignee
		}
		if err := t.RenewLease(DefaultExecutionLeaseTTL, now); err != nil {
			return err
		}
		return s.tasks.Update(txCtx, t)
	})
}

// WorkerRenewLease renews a running task's execution lease on behalf of the
// worker-daemon's process-alive auto-renew (T456 — issue-21ba5b78/I30 P0 #1). Unlike
// HeartbeatTask (driven by the agent's MCP `heartbeat`, gated on actor==assignee +
// project membership), this is the WORKER attesting "the agent process for this task
// is alive" — decoupled from the agent's LLM turn so a long build/test never lets the
// lease lapse. The worker only renews tasks it is actually running (its live
// managedAgent.currentTaskID), and a superseded session is dropped worker-side, so the
// trust boundary is the enrolled-worker auth; here we still verify the task is the
// SAME agent's running, non-blocked task before renewing (defense in depth — never
// renew another agent's or a blocked task's lease).
//
// agentRef is the assignee form the agent itself uses ("agent:<member-id>" via
// agentActor).
//
// REVOCATION SIGNAL (issue-88e32d98, P0 block-fuse): the renew is a no-op in exactly
// the cases where THIS agent's in-flight execution should STOP — the task is blocked
// (ADR-0046: block keeps status=running + a blocked_reason, so a mid-flight executor
// would otherwise keep running dangerous work), reassigned away (assignee no longer
// this agent), or terminal/archived (nothing alive to keep alive). In every such case
// it returns revoked=true with a reason so the worker can circuit-break (熔断) the
// in-flight executor at its next lease-heartbeat, instead of silently letting the
// lease lapse and the executor race on. A genuine renew returns revoked=false. reason
// is "" when not revoked; "blocked" / "reassigned" / "terminal" otherwise.
func (s *Service) WorkerRenewLease(ctx context.Context, taskID pm.TaskID, agentRef pm.IdentityRef) (revoked bool, reason string, err error) {
	now := s.clock.Now()
	txErr := s.runInTx(ctx, func(txCtx context.Context) error {
		t, ferr := s.tasks.FindByID(txCtx, taskID)
		if ferr != nil {
			return ferr
		}
		if t.IsArchived() || t.Status() != pm.TaskRunning {
			revoked, reason = true, "terminal" // nothing alive to keep alive → stop the executor
			return nil
		}
		if t.Assignee() != agentRef {
			revoked, reason = true, "reassigned" // reassigned / stale worker view → stop the executor
			return nil
		}
		// RenewLease no-ops cleanly on a blocked task (ErrTaskBlocked) — a blocked task
		// is a lease-free legal pause. A block mid-flight (P67 Ship 事故) MUST revoke so
		// the worker fuses the still-running executor rather than let it merge dead code.
		if rerr := t.RenewLease(DefaultExecutionLeaseTTL, now); rerr != nil {
			if errors.Is(rerr, pm.ErrTaskBlocked) {
				revoked, reason = true, "blocked"
				return nil
			}
			if errors.Is(rerr, pm.ErrIllegalTransition) {
				revoked, reason = true, "terminal"
				return nil
			}
			return rerr
		}
		return s.tasks.Update(txCtx, t)
	})
	if txErr != nil {
		return false, "", txErr
	}
	return revoked, reason, nil
}

// DiscardTask discards a non-terminal Task (terminal "discarded"; was CancelTask
// pre-v2.8.1, uniform 废弃 semantic).
func (s *Service) DiscardTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, actor, func(t *pm.Task, now time.Time) error {
		// T339 escape hatch: discard_task is the operator tool to conclude a leaked
		// open+archived dead task. Task.Discard() rejects an archived task
		// (ErrTaskArchived) — by design, archived is read-only — so route an archived
		// (non-terminal) task through FinalizeArchived, the one permitted terminal
		// cleanup. A live task takes the normal Discard path unchanged.
		if t.IsArchived() {
			return t.FinalizeArchived(now)
		}
		return t.Discard(now)
	}, "")
}

// ResetTask is the T862 tier-3 RECOVERY reset: it returns a CONFIRMED-DEAD running task
// to the builtin pool (running→open, assignee/block/lease cleared) so a FRESH executor is
// auto-assigned, then re-dispatches via the SAME event chain a fresh pool task uses. It is
// the center half of the "worktree真没 / k8s换节点" recovery the runtime's tier-3
// (enactRecover→RecoverFresh) drives.
//
// DISTINCT from the T456 lease-nudge (NudgeOnLeaseExpiry): a lease that merely LAPSED is
// nudged (续租 + @-mention the SAME owner, status stays running) because the owner may yet
// be alive; reset is the confirmed-dead path that CHANGES owner (clears assignee → back to
// pool). The two must never be conflated — hence the guards below.
//
// MIS-FIRE GUARD (§2②): (a) the domain ResetToOpen hard-rejects a still-LIVE lease
// (ErrLeaseStillLive) — a live lease alone would just be nudged; and (b) the CALLER must
// have tier-3-confirmed the executor is dead (a live executor is nudged/adopted, never
// reset). Lapse alone is insufficient (it would be re-续租'd by the nudge sweep), so both
// hold before a running task is reclaimed.
//
// OWNER FAST-PATH (§THE-gate): the owner runtime's tier-3 confirmation arrives as
// confirmedDead=true. When the caller IS the current lease owner (actor == assignee) we
// pass bypassLease=true so guard (a) is skipped — the owner is still renewing its own
// lease, so it would NEVER lapse and the task would sit running forever waiting for a lapse
// that can't come. The ownership check keeps this safe: a STRANGER asserting confirmedDead
// still faces guard (a) (actor != assignee → bypassLease=false), and a manual reset
// (confirmedDead=false) always requires a genuinely lapsed lease. confirmedDead never
// bypasses the cap — reset×N still trips the circuit breaker below.
//
// CIRCUIT BREAKER (§2B, durable): recovery_reset_count caps consecutive resets. At
// MaxRecoveryResets the task is NOT reset again — it is BLOCKED for PD triage ("tier-3
// 恢复耗尽 reset×N 需triage"), because a reset loop is a bad-task / broken-environment
// symptom auto-recovery cannot fix. Read-cap-check-increment-reset runs in ONE tx with an
// in-tx FindByID re-read; the whole-tx replay on write conflict (RunInTx) makes concurrent
// resets converge — the loser re-reads a now-open task and its ResetToOpen fails
// ErrIllegalTransition, so exactly one +1 lands.
//
// Re-dispatch chain (all existing): ResetTask → EvtTaskStateChanged →
// AutoAssignTriggerProjector → TriggerAutoAssignForProject → autoAssignPoolTask (selects a
// fresh online+capable+free-slot agent, ClaimIfUnassigned CAS, emits EvtTaskAssigned →
// DispatchWakeProjector wakes it → fork). It emits EvtTaskStateChanged (NOT EvtTaskAssigned:
// reset CLEARED the assignee, so a DispatchWakeProjector keyed on assignee would wake 0
// people — silent no-op). ResetTask deliberately does NOT ClearPlan / touch the dispatch
// record, so the pool membership (NodeDispatched, derived from that record) survives and
// ClaimableInPool still passes. For determinism it also fires a synchronous
// TriggerAutoAssignForProject after the state change commits. Cross-agent by design
// (project-member gated, no own-task requirement).
func (s *Service) ResetTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef, confirmedDead bool) error {
	now := s.clock.Now()
	var projectID pm.ProjectID
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		// In-tx re-read = the row snapshot the cap check + reset act on; RunInTx's
		// whole-tx replay serialises concurrent resets (§2B atomicity).
		t, ferr := s.tasks.FindByID(txCtx, taskID)
		if ferr != nil {
			return ferr
		}
		if merr := s.requireProjectMember(txCtx, t.ProjectID(), actor); merr != nil {
			return merr
		}
		if merr := s.requireProjectMutable(txCtx, t.ProjectID()); merr != nil {
			return merr
		}
		projectID = t.ProjectID()
		prevStatus := t.Status()
		// Circuit breaker: budget spent → block for triage instead of resetting again.
		if t.RecoveryResetCount() >= pm.MaxRecoveryResets {
			reason := fmt.Sprintf("tier-3 recovery exhausted: reset×%d — needs PD triage (task bad → discard, or environment broken → fix + redispatch)", t.RecoveryResetCount())
			if berr := t.BlockForResetExhaustion(reason, now); berr != nil {
				return berr
			}
			if uerr := s.tasks.Update(txCtx, t); uerr != nil {
				return uerr
			}
			if ferr := s.flushActionLogs(txCtx, t); ferr != nil {
				return ferr
			}
			return s.emitTaskStateChanged(txCtx, t, prevStatus, reason)
		}
		// Under cap: reset to pool (ResetToOpen increments recovery_reset_count + gates
		// on a live lease). The owner's tier-3 confirmation (confirmedDead) skips that gate
		// ONLY for the current lease owner — a stranger cannot force-reset a slow-but-alive
		// owner, and a manual reset (confirmedDead=false) still requires a lapsed lease.
		bypassLease := confirmedDead && actor == t.Assignee()
		if rerr := t.ResetToOpen(now, bypassLease); rerr != nil {
			return rerr
		}
		if uerr := s.tasks.Update(txCtx, t); uerr != nil {
			return uerr
		}
		if ferr := s.flushActionLogs(txCtx, t); ferr != nil {
			return ferr
		}
		// issue-77d9beff ①: reset_task was the built-in pool's dead-executor recovery; a
		// STRUCTURED (graphed) plan node used to be REJECTED here (ErrResetNotPoolTask),
		// which only prevented new wedges and never recovered/cleaned the existing ones.
		// Self-heal instead: reset runs normally (running→open) and the shared reconcile
		// reopens the wedged graph node + clears its dispatch record so plan advance
		// re-dispatches it. No-op for a built-in pool member / pure backlog task.
		if herr := s.reopenStuckPlanNode(txCtx, t, "task_reset"); herr != nil {
			return herr
		}
		return s.emitTaskStateChanged(txCtx, t, prevStatus, "tier-3 recovery reset")
	})
	if err != nil {
		return err
	}
	// Determinism (§2③): re-scan the project pool synchronously so a fresh assignee is
	// matched in-line (the AutoAssignTriggerProjector also fires async off the emitted
	// event — both are CAS-safe, so the double-trigger never double-assigns). Best-effort:
	// a sweep error does not fail the reset (the periodic backstop covers it).
	if projectID != "" {
		_, _ = s.TriggerAutoAssignForProject(ctx, projectID)
	}
	return nil
}

// BlockTask records a stuck-reason ANNOTATION on a running task (ADR-0046: status
// stays running, no deadlock; issue I14 §2.5/§13.A). It classifies the block by
// reasonType — input_required (the agent needs a user reply) or obstacle (an external
// blocker needs owner/PM intervention) — and a non-empty reason.
//
// v2.14.0 I14/F3 (finding 01KVN… / §13.A): F1's Task.Block validates only the reason
// text, NOT the type, so this entry enforces BlockReasonType.IsValid() — any value
// other than input_required/obstacle (including "") is rejected with
// pm.ErrInvalidBlockReasonType. The Conversation input_request write for the
// input_required case is F6's HTTP/Conversation wiring (§3/§6.2), not here.
func (s *Service) BlockTask(ctx context.Context, taskID pm.TaskID, reason string, reasonType pm.BlockReasonType, actor pm.IdentityRef) error {
	if !reasonType.IsValid() {
		return pm.ErrInvalidBlockReasonType
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject block on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status()
		// agentRef is the task's own assignee: the domain Block then validates the reason
		// (an empty reason → ErrBlockReasonRequired) without the actor having to equal the
		// assignee at this layer (start_work already established the agent owns the run).
		agentRef := t.Assignee()
		if err := t.Block(reason, reasonType, agentRef, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		// §7.3: persist the "blocked" lifecycle log entry.
		if err := s.flushActionLogs(txCtx, t); err != nil {
			return err
		}
		if err := s.emitTaskStateChanged(txCtx, t, prevStatus, reason); err != nil {
			return err
		}
		// audit §5: record the block as a human-facing running→blocked status change.
		s.auditTaskBlocked(txCtx, t, reasonType, reason, actor)
		// F6 §3: an input_required block needs a USER reply → emit a SECOND event in
		// THIS tx so the TaskInputConversationProjector surfaces an interactive
		// input_request message in the task's bound Conversation (sender=assignee).
		// An obstacle block (owner/PM action, no user reply) emits NOTHING extra.
		if reasonType == pm.BlockReasonInputRequired {
			return s.emitTaskInputEvent(txCtx, EvtTaskInputRequested, taskInputEventPayload{
				TaskID:    string(t.ID()),
				ProjectID: string(t.ProjectID()),
				OwnerRef:  "pm://tasks/" + string(t.ID()),
				AgentRef:  string(agentRef),
				Reason:    reason,
			})
		}
		return nil
	})
}

// emitTaskInputEvent emits a F6 task-input event (EvtTaskInputRequested /
// EvtTaskInputReplied) inside the caller's tx, refs keyed by task+project so the
// outbox row carries the task locus.
func (s *Service) emitTaskInputEvent(ctx context.Context, evt string, payload taskInputEventPayload) error {
	return s.emit(ctx, evt,
		refsJSON(map[string]string{"task_id": payload.TaskID, "project_id": payload.ProjectID}),
		payload)
}

// UnblockTask clears a stuck (blocked_reason) annotation on a RUNNING task and
// re-dispatches it (ADR-0046). The task never left running, but its prior WorkItem
// terminated (failed) when it got stuck, so unblocking emits pm.task.assigned and
// the WorkItemProjector mints a fresh WorkItem. No-op if the task carries no
// blocked_reason (not stuck) — avoids a duplicate dispatch on an already-active task.
// UnblockTaskCommand is the F6 unblock input (§13.C / §4). Comment is the
// resolution/reply the unblocker supplies (threaded to the agent as the
// BlockedComment, and — for an input_required block — surfaced as the
// Conversation input_reply body). InputRequestMessageID (optional) threads that
// reply under the original input_request message. Actor is the unblocker (the
// user replying, or the owner resolving an obstacle).
type UnblockTaskCommand struct {
	TaskID                pm.TaskID
	Comment               string
	InputRequestMessageID string
	Actor                 pm.IdentityRef
}

func (s *Service) UnblockTask(ctx context.Context, cmd UnblockTaskCommand) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, cmd.TaskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), cmd.Actor); err != nil {
			return err
		}
		// #297: reject unblock on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		if strings.TrimSpace(t.BlockedReason()) == "" {
			return nil // not stuck → nothing to recover (idempotent, no double-dispatch)
		}
		// F6 §4: capture the block's reasonType BEFORE Unblock clears it — it decides
		// whether a Conversation input_reply must follow (input_required) or not (obstacle).
		prevReasonType := t.BlockedReasonType()
		// v2.14.0 I14 F6 (§13.C): thread the real comment (not "") so the agent reads
		// the resolution as BlockedComment; the re-dispatch (EvtTaskAssigned below) is
		// the §13.C wake.
		if err := t.Unblock(cmd.Comment, cmd.Actor, now); err != nil {
			return err
		}
		// v2.18.0 W4c ③: unblock re-enters the run-slot predicate (status stays
		// running, blocked_reason cleared → the task re-occupies a slot), so it is a
		// task→running transition the dropped single-active index used to guard. Re-check
		// the cap here — a stuck task may have been unblocked while the agent filled its
		// slots elsewhere; admitting it back must not exceed the cap.
		if err := s.enforceConcurrencyCap(txCtx, t); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		// §7.3: persist the "unblocked" lifecycle log entry.
		if err := s.flushActionLogs(txCtx, t); err != nil {
			return err
		}
		// issue-77d9beff ①: for a STRUCTURED (graphed) plan node the EvtTaskAssigned re-wake
		// below is NOT enough — the graph node is still wedged Running from the pre-block
		// attempt (the block terminated its WorkItem; the task never left running so
		// syncGraphToTasks keeps the node Running), and its stale dispatch record keeps it out
		// of graphReadySet, so unblock never actually re-dispatches it (block→unblock is a
		// name-only recovery). The shared reconcile reopens the node + clears the record so the
		// next plan advance re-dispatches it. No-op for a built-in pool / backlog task.
		if herr := s.reopenStuckPlanNode(txCtx, t, "task_unblock"); herr != nil {
			return herr
		}
		if err := s.emitTaskAssignEvent(txCtx, t, EvtTaskAssigned, ""); err != nil {
			return err
		}
		// audit §5: record the unblock as a human-facing blocked→running status change.
		s.auditTaskUnblocked(txCtx, t, cmd.Actor)
		// F6 §4: an input_required unblock IS the user's reply → emit EvtTaskInputReplied
		// in THIS tx so the TaskInputConversationProjector posts an input_reply message
		// (sender=actor, threaded under the original input_request). An obstacle unblock
		// (owner resolution, no user-facing conversation message) emits NOTHING extra.
		if prevReasonType == pm.BlockReasonInputRequired {
			return s.emitTaskInputEvent(txCtx, EvtTaskInputReplied, taskInputEventPayload{
				TaskID:                string(t.ID()),
				ProjectID:             string(t.ProjectID()),
				OwnerRef:              "pm://tasks/" + string(t.ID()),
				ActorRef:              string(cmd.Actor),
				Comment:               cmd.Comment,
				InputRequestMessageID: cmd.InputRequestMessageID,
			})
		}
		return nil
	})
}

// CompleteTask moves running→completed and records the completer.
func (s *Service) CompleteTask(ctx context.Context, taskID pm.TaskID, by pm.IdentityRef) error {
	return s.CompleteTaskAfterPrecheck(ctx, taskID, by)
}

// PrecheckCompleteTask runs the external/read-only completion guards before a caller
// opens its own write transaction. Currently a no-op (the cycle-specific merge guard
// was removed); retained for API stability with callers that split precheck + commit.
func (s *Service) PrecheckCompleteTask(ctx context.Context, taskID pm.TaskID) error {
	return nil
}

// CompleteTaskAfterPrecheck moves running→completed without re-running external
// guards. Use only when PrecheckCompleteTask already passed immediately before the
// surrounding transaction. CompleteTask remains the safe default for other callers.
func (s *Service) CompleteTaskAfterPrecheck(ctx context.Context, taskID pm.TaskID, by pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, by, func(t *pm.Task, now time.Time) error { return t.Complete(by, now) }, "")
}

// primaryRepoURL resolves the project's primary code-repo URL for the F3 merge
// guard (v2.18.4 BE-1, issue-f980c8de). It picks the project's PRIMARY ref (the one
// flagged is_primary), falling back to the FIRST ref in stable created-order when no
// primary is set (pre-0087 behaviour). For the chosen ref it resolves the URL:
//   - a workspace-Repo ref (repo_id set) → the workspace coderepo.Repo's url via the
//     CodeRepoResolver port; if that yields nothing (resolver absent / repo gone) it
//     falls back to the ref's own url;
//   - a legacy url-only ref → its own url.
//
// "" when the project has no repo configured (the caller fails closed). Read-only.
func (s *Service) primaryRepoURL(ctx context.Context, projectID pm.ProjectID) (string, error) {
	refs, err := s.codeRepoRefs.ListByProject(ctx, projectID)
	if err != nil {
		return "", err
	}
	if len(refs) == 0 {
		return "", nil
	}
	chosen := refs[0]
	for _, ref := range refs {
		if ref.IsPrimary() {
			chosen = ref
			break
		}
	}
	if chosen.RepoID() != "" && s.codeRepoResolver != nil {
		if url, rerr := s.codeRepoResolver.RepoURL(ctx, chosen.RepoID()); rerr == nil && url != "" {
			return url, nil
		}
	}
	return chosen.URL(), nil
}

// retainAsTaskSubscriber persists `identity` as a sticky MANUAL subscriber so a
// person taken off a Task (unassigned / reassigned away / reopened) stays in the
// task Conversation as a subscriber until explicitly Unsubscribed (OQ13 — task
// state resets but Conversation membership is monotonic). The creator and the
// empty ref are skipped (creator is always effective); Add is INSERT OR IGNORE,
// so re-retaining an existing manual row is a no-op. Must run inside the caller's
// tx so the subsequent effective-set recompute sees the new row.
func (s *Service) retainAsTaskSubscriber(ctx context.Context, t *pm.Task, identity pm.IdentityRef, now time.Time) error {
	if identity == "" || identity == t.CreatedBy() {
		return nil
	}
	sub, err := pm.NewTaskSubscriber(t.ID(), identity, "system", now)
	if err != nil {
		return err
	}
	return s.taskSubs.Add(ctx, sub)
}

// grantAgentProjectMembership makes an AGENT assignee a ProjectMember of the
// task's project so the agent passes the project write-gate (OQ6) for its MCP
// tools (#5a, ADR-0049/0052; OQ4 = agents get project-level write). It is:
//   - AGENT-ONLY: human (`user:`) assignees and `system` are skipped (the branch
//     keys on the `agent:` prefix) — they are never granted membership here.
//   - FAIL-CLOSED: for an AGENT assignee, a missing directory (s.agentDir == nil)
//     is a hard error (pm.ErrAgentDirectoryUnavailable) — a missing dependency
//     must NEVER silently skip the cross-org guard / membership grant. The nil
//     case only preserves old behavior for human assignees (where no agent
//     authorization is involved). Production always wires the directory.
//   - CROSS-ORG GUARDED: it resolves the agent's org via the directory and the
//     project's org via the project repo; a mismatch (or an unresolvable agent)
//     is rejected with pm.ErrCrossOrgAssignee — an org member is the prerequisite
//     for project membership.
//   - IDEMPOTENT: if the agent is already a member it is a no-op (ErrMemberExists
//     from the member repo is swallowed); no duplicate row, no error.
//
// It runs inside the AssignTask tx (after t.Assign succeeds), so the assignment
// and the membership commit atomically. Membership is monotonic (OQ13-style):
// Unassign/reassign never removes it — only explicit member management does.
func (s *Service) grantAgentProjectMembership(ctx context.Context, t *pm.Task, assignee pm.IdentityRef, now time.Time) error {
	if !strings.HasPrefix(string(assignee), "agent:") {
		return nil // human / system assignees: no agent authorization to grant
	}
	if s.agentDir == nil {
		// Fail-closed: an agent assignee requires the directory to verify its org
		// (OQ6). Refuse rather than silently bypass the cross-org guard.
		return pm.ErrAgentDirectoryUnavailable
	}
	agentID := strings.TrimPrefix(string(assignee), "agent:")

	// Cross-org guard: agent's org must equal the project's org.
	p, err := s.projects.FindByID(ctx, t.ProjectID())
	if err != nil {
		return err
	}
	agentOrg, err := s.agentDir.OrgOfAgent(ctx, agentID)
	if err != nil {
		// Can't verify org (e.g. agent not found) → treat as cross-org/reject.
		return pm.ErrCrossOrgAssignee
	}
	if agentOrg != p.OrganizationID() {
		return pm.ErrCrossOrgAssignee
	}

	// Idempotent add: reuse the same member-repo insert AddProjectMember uses.
	m, err := pm.NewProjectMember(pm.NewProjectMemberInput{
		ID: pm.MemberID(s.idgen.NewULID()), ProjectID: t.ProjectID(), IdentityID: assignee,
		Role: pm.RoleMember, AddedBy: assignee, CreatedAt: now,
	})
	if err != nil {
		return err
	}
	if err := s.members.Save(ctx, m); err != nil {
		if errors.Is(err, pm.ErrMemberExists) {
			return nil // already a member → no-op
		}
		return err
	}
	return nil
}

// UnassignTask moves assigned→open and clears the assignee (the explicit "drop
// the assignment" verb). Per OQ13 the outgoing assignee is RETAINED as a sticky
// manual subscriber (stays in the Conversation, downgraded to subscriber; only
// an explicit Unsubscribe removes them). The live AgentWorkItem is canceled by
// the WorkItemProjector consuming the resulting state_changed→open (canceling
// the work attempt is independent of keeping the subscription).
func (s *Service) UnassignTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject unassign on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status() // Unassign is metadata-only; status unchanged.
		prev := t.Assignee()
		if err := t.Unassign(now); err != nil {
			return err
		}
		if err := s.retainAsTaskSubscriber(txCtx, t, prev, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		if err := s.emitTaskStateChanged(txCtx, t, prevStatus, ""); err != nil {
			return err
		}
		// audit §5: record the unassign (only when there WAS an assignee to drop).
		if prev != "" {
			s.auditTaskAssign(txCtx, t, pm.AuditTaskUnassigned, prev, actor)
		}
		return nil
	})
}

// ReopenTask moves a completed/verified Task back to open in one step (internally
// completed/verified→reopened→open), clearing assignment + completion truth so a
// subsequent assign starts a fresh work segment. Per OQ13 the prior assignee +
// completer are RETAINED as sticky manual subscribers (they did the work → stay
// informed after reopen). No live WorkItem to cancel (the Task was done).
func (s *Service) ReopenTask(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject reopen on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status() // before the reopen chain (completed/verified).
		prevAssignee, prevCompleter := t.Assignee(), t.CompletedBy()
		if err := t.Reopen(now); err != nil {
			return err
		}
		if err := t.ToOpenFromReopened(now); err != nil {
			return err
		}
		if err := s.retainAsTaskSubscriber(txCtx, t, prevAssignee, now); err != nil {
			return err
		}
		if err := s.retainAsTaskSubscriber(txCtx, t, prevCompleter, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		if err := s.emitTaskStateChanged(txCtx, t, prevStatus, ""); err != nil {
			return err
		}
		// audit §5: ReopenTask has its own tx (not taskStateOp) — record the reopen.
		s.auditTaskStatusChange(txCtx, t, prevStatus, actor)
		return nil
	})
}

// taskStateOp is the shared "load → gate → mutate → persist → emit
// state_changed" path for status-only transitions.
func (s *Service) taskStateOp(ctx context.Context, taskID pm.TaskID, actor pm.IdentityRef, mutate func(*pm.Task, time.Time) error, reason string) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject any status transition (Start/Discard/Block/Complete/Verify/
		// SetStatus — every taskStateOp caller) on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status() // snapshot BEFORE the transition (Discard/SetStatus/...).
		if err := mutate(t, now); err != nil {
			return err
		}
		// v2.18.0 W4c ③: an owner SetStatus→running (the free status-override menu) is
		// also a task→running transition the dropped single-active index used to guard.
		// The cap self-skips for any non-running / blocked / terminal mutate (Block,
		// Complete, Discard), so this only bites when the override actually fills a slot.
		if err := s.enforceConcurrencyCap(txCtx, t); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		// §7.3: persist any lifecycle log the mutate appended (e.g. Block → "blocked").
		if err := s.flushActionLogs(txCtx, t); err != nil {
			return err
		}
		if err := s.emitTaskStateChanged(txCtx, t, prevStatus, reason); err != nil {
			return err
		}
		// audit §5: record the status transition (shared闸 for Discard/SetStatus/
		// Complete/Reopen/Reset — every taskStateOp caller). No-op when status didn't move.
		s.auditTaskStatusChange(txCtx, t, prevStatus, actor)
		// T464: if this transition just concluded a derived task and that makes ALL of
		// its issue's derived tasks terminal, nudge the issue owner to review + close.
		return s.maybeNotifyIssueDerivedTasksDone(txCtx, t, prevStatus)
	})
}

// SetTaskStatus sets the Task to any VALID status with NO adjacency enforcement
// (v2.8.1 @oopslink: task state = the agent's self-reported progress; the center
// does not gate workflow transitions — the Change-status menu offers the full
// enum). Project-member gated; emits pm.task.state_changed (generic) so the
// participant projector + downstream stay in sync. The typed transitions
// (Start/Complete/Block/...) remain for the agent's structured self-reports.
func (s *Service) SetTaskStatus(ctx context.Context, taskID pm.TaskID, target pm.TaskStatus, actor pm.IdentityRef) error {
	return s.taskStateOp(ctx, taskID, actor, func(t *pm.Task, now time.Time) error {
		return t.SetStatus(target, now)
	}, "")
}

// BatchTaskPatch is the set of optionally-updated fields for BatchUpdateTask. A
// nil pointer means "leave unchanged"; a non-nil pointer applies the field
// (v2.8.1 edit-task #278). For Assignee, "" means Unassign. Title/Description
// are also accepted so the bare task PATCH stays a superset of the prior
// metadata-only PATCH (single atomic tx for the whole edit).
type BatchTaskPatch struct {
	Status      *string
	Assignee    *string
	Tags        *[]string
	Title       *string
	Description *string
	// DerivedFromIssue (T192): nil = unchanged; "" = clear the link; a non-empty id
	// (re)links — validated to exist + same project, like UpdateTask.
	DerivedFromIssue *pm.IssueID
	// RequiredCapabilities (v2.18.3 BE-1): nil = unchanged; non-nil replaces the set
	// (empty slice clears it → unrestricted). Canonicalized by the domain.
	RequiredCapabilities *[]string
}

// BatchUpdateTask applies any subset of {status, assignee, tags} to a Task in a
// SINGLE tx — all-or-none (if any field's mutation errors, the tx rolls back and
// nothing is applied). Project-member gated. Emits pm.task.state_changed so the
// participant projector + downstream stay in sync (a tags-only edit still bumps
// version + re-emits, which is harmless/idempotent for the effective set).
func (s *Service) BatchUpdateTask(ctx context.Context, taskID pm.TaskID, patch BatchTaskPatch, actor pm.IdentityRef) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject batch task edit on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		prevStatus := t.Status()     // snapshot BEFORE patch.Status applies (if any).
		prevAssignee := t.Assignee() // snapshot BEFORE patch.Assignee applies (if any).
		if patch.Title != nil {
			if err := t.Rename(*patch.Title, now); err != nil {
				return err
			}
		}
		if patch.Description != nil {
			if err := t.SetDescription(*patch.Description, now); err != nil {
				return err
			}
		}
		if patch.Status != nil {
			if err := t.SetStatus(pm.TaskStatus(*patch.Status), now); err != nil {
				return err
			}
		}
		if patch.Assignee != nil {
			if *patch.Assignee == "" {
				if err := t.Unassign(now); err != nil {
					return err
				}
			} else if err := t.Assign(pm.IdentityRef(*patch.Assignee), now); err != nil {
				return err
			}
		}
		if patch.Tags != nil {
			if err := t.SetTags(*patch.Tags, now); err != nil {
				return err
			}
		}
		if patch.RequiredCapabilities != nil {
			if err := t.SetRequiredCapabilities(*patch.RequiredCapabilities, now); err != nil {
				return err
			}
		}
		if patch.DerivedFromIssue != nil {
			if err := s.applyDerivedFromIssue(txCtx, t, *patch.DerivedFromIssue, now); err != nil {
				return err
			}
		}
		// v2.18.0 W4c ③: a batch edit can set status→running AND/OR reassign in one
		// shot — either lands a (possibly new) assignee in a run slot, the transition
		// the dropped single-active index used to guard. Self-skips unless the result
		// is running+unblocked.
		if err := s.enforceConcurrencyCap(txCtx, t); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		if err := s.emitTaskStateChanged(txCtx, t, prevStatus, ""); err != nil {
			return err
		}
		// audit §5: BatchUpdateTask inlines its own tx (bypasses taskStateOp) and can
		// change status AND/OR assignee in one shot — record each that actually moved.
		s.auditTaskStatusChange(txCtx, t, prevStatus, actor)
		if t.Assignee() != prevAssignee {
			switch {
			case t.Assignee() == "":
				s.auditTaskAssign(txCtx, t, pm.AuditTaskUnassigned, prevAssignee, actor)
			case prevAssignee == "":
				s.auditTaskAssign(txCtx, t, pm.AuditTaskAssigned, prevAssignee, actor)
			default:
				s.auditTaskAssign(txCtx, t, pm.AuditTaskReassigned, prevAssignee, actor)
			}
		}
		return nil
	})
}

// emitTaskAssignEvent emits an assign/reassign event carrying the current
// assignee, previous assignee, and the recomputed effective subscriber set.
func (s *Service) emitTaskAssignEvent(ctx context.Context, t *pm.Task, evt, previous string) error {
	manual, err := s.taskSubs.ListByTask(ctx, t.ID())
	if err != nil {
		return err
	}
	return s.emit(ctx, evt,
		refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
		taskEventPayload{
			TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
			OwnerRef: "pm://tasks/" + string(t.ID()), Assignee: string(t.Assignee()),
			PreviousAssignee: previous, Status: string(t.Status()),
			EffectiveSubscribers: EffectiveTaskSubscribers(t, manual),
		})
}

// emitTaskStateChanged emits pm.task.state_changed carrying the recomputed
// effective subscriber set. A state change can move the effective set — most
// notably unassign/reopen, which clear the assignee so the prior assignee must
// leave the task Conversation. The ParticipantProjector consumes this and
// rewrites participants to the effective set (set semantics → idempotent for
// state changes that don't move the set, e.g. start/block/complete).
//
// prevStatus is the task's status captured by the caller BEFORE the transition
// this emit reports (the task is already transitioned by the time we run, so the
// caller MUST snapshot prev). It rides the payload as PrevStatus so the P2-2
// failure handler can notify ONLY on the →failed transition (#re-discard edge).
// For emits that follow no status transition (assign/reassign/unassign tags-only
// edits), the caller passes the current status — prev == now means "not a
// transition", which is never a failure transition anyway.
func (s *Service) emitTaskStateChanged(ctx context.Context, t *pm.Task, prevStatus pm.TaskStatus, reason string) error {
	manual, err := s.taskSubs.ListByTask(ctx, t.ID())
	if err != nil {
		return err
	}
	return s.emit(ctx, EvtTaskStateChanged,
		refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
		taskEventPayload{
			TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
			OwnerRef: "pm://tasks/" + string(t.ID()), Assignee: string(t.Assignee()),
			Status: string(t.Status()), PrevStatus: string(prevStatus), Reason: reason,
			EffectiveSubscribers: EffectiveTaskSubscribers(t, manual),
		})
}
