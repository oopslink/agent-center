package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// v2.9 P3 Stage B — Plan deletion + archival (with cascade Task archival). Both
// guard against a RUNNING plan (ErrPlanRunning — stop it first) and run their
// PM-state writes + the cross-BC conversation-cleanup event in ONE local tx
// (OQ1): the conversation hard-delete (DeletePlan) / archive (ArchivePlan) is an
// idempotent projection on EvtPlanDeleted / EvtPlanArchived (PlanParticipantProjector),
// NEVER inline — PM stays decoupled from Conversation.

// DeletePlan HARD-deletes a non-running Plan (design §; "删会话"). It is rejected
// for a RUNNING plan (ErrPlanRunning — stop it first). In one tx it:
//
//	(a) UNLOADs every task in the plan back to the backlog (task.plan_id="") — tasks
//	    are NOT deleted, they return to the project backlog (reuses the
//	    RemoveTaskFromPlan mechanism: clear plan_id; archived tasks are skipped since
//	    ClearPlan would reject them, but a plan being deleted has live tasks);
//	(b) deletes the plan's depends_on edges + dispatch records + the plan row
//	    (repo.DeletePlan cascades all three);
//	(c) emits pm.plan.deleted so the projector hard-deletes the plan's 1:1
//	    Conversation (reverse of pm.plan.created which creates it).
//
// The actor must be a project member.
func (s *Service) DeletePlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject plan delete on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		// ADR-0047: the built-in pool cannot be deleted on its own (it lives + dies
		// with its project). Check BEFORE the running guard so the error is the
		// specific ErrBuiltinPlanImmutable (the pool is always running, so the running
		// guard would otherwise mask it with ErrPlanRunning).
		if p.IsBuiltin() {
			return pm.ErrBuiltinPlanImmutable
		}
		// Guard: a running plan cannot be deleted — stop it first.
		if p.Status() == pm.PlanRunning {
			return pm.ErrPlanRunning
		}
		// (a) UNLOAD the plan's tasks back to the backlog (plan_id="").
		tasks, err := s.tasks.ListByPlan(txCtx, planID)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			if err := t.ClearPlan(now); err != nil {
				return err
			}
			if err := s.tasks.Update(txCtx, t); err != nil {
				return err
			}
		}
		// (b) cascade-delete deps + dispatch records + the plan row.
		if err := s.plans.DeletePlan(txCtx, planID); err != nil {
			return err
		}
		// (b') v2.10 cascade-delete the plan's shared findings (ADR-0053): a plan's
		// findings die with the plan. nil-safe (pre-v2.10 constructions skip it).
		if s.findings != nil {
			if err := s.findings.DeleteByPlan(txCtx, planID); err != nil {
				return err
			}
		}
		// (c) emit pm.plan.deleted → projector hard-deletes the plan conversation.
		return s.emit(txCtx, EvtPlanDeleted,
			refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(p.ProjectID())}),
			planEventPayload{
				PlanID: string(p.ID()), ProjectID: string(p.ProjectID()),
				OwnerRef: "pm://plans/" + string(p.ID()),
			})
	})
}

// ArchivePlan archives a non-running Plan AND CASCADE-archives its tasks (v2.9
// P3, irreversible). It is rejected for a RUNNING plan (ErrPlanRunning) and for an
// already-archived plan (ErrPlanArchived, mirroring Conversation.Archive). In one
// tx it:
//
//	(a) plan.Archive(now) → plans.Update (draft/done → archived, terminal);
//	(b) CASCADE: each task in the plan → task.Archive(now, actor) → tasks.Update.
//	    Archival is ORTHOGONAL — the task's status is preserved (a verified task
//	    stays verified, etc.). A task already archived is left as-is (idempotent skip);
//	(c) emits pm.plan.archived so the projector archives the plan's 1:1 Conversation
//	    for consistency.
//
// The actor must be a project member; actor is recorded as archived_by on each task.
func (s *Service) ArchivePlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject plan archive on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		// ADR-0047: the user-facing ArchivePlan rejects the built-in pool — it is
		// archived ONLY as part of its project's cascade (which calls the domain
		// Plan.Archive directly), never on its own. Check before the running guard so
		// the error is the specific ErrBuiltinPlanImmutable.
		if p.IsBuiltin() {
			return pm.ErrBuiltinPlanImmutable
		}
		// v2.9 #299 (@oopslink): reject archiving a plan that still has any member
		// task in the RUNNING state — after stop, a draft plan may still carry an
		// in-flight running task, and archiving would orphan it. (Distinct from
		// p.Archive's ErrPlanRunning, which guards the PLAN's own running status.)
		tasks, err := s.tasks.ListByPlan(txCtx, planID)
		if err != nil {
			return err
		}
		for _, t := range tasks {
			if t.Status() == pm.TaskRunning {
				return pm.ErrPlanHasRunningTasks
			}
		}
		// (a) archive the plan (rejects running → ErrPlanRunning, already-archived →
		// ErrPlanArchived).
		if err := p.Archive(now); err != nil {
			return err
		}
		if err := s.plans.Update(txCtx, p); err != nil {
			return err
		}
		// (b) CASCADE: archive every (not-yet-archived) task in the plan. Archive is
		// orthogonal (status preserved), so FIRST finalize any NON-terminal task to
		// discarded (T339): a skipped/abandoned escape node stays `open`, and archiving
		// it orthogonally would leave an open+archived task that leaks into the board /
		// list_tasks(open) yet is locked against any later transition. Finalizing before
		// Archive() closes that hole; a completed/discarded task keeps its real outcome.
		// (Reuses the list fetched for the running-task check.)
		for _, t := range tasks {
			if t.IsArchived() {
				continue // idempotent: already archived
			}
			if err := t.FinalizeForArchive(now); err != nil {
				return err
			}
			if err := t.Archive(now, actor); err != nil {
				return err
			}
			if err := s.tasks.Update(txCtx, t); err != nil {
				return err
			}
		}
		// (c) emit pm.plan.archived → projector archives the plan conversation.
		return s.emit(txCtx, EvtPlanArchived,
			refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(p.ProjectID())}),
			planEventPayload{
				PlanID: string(p.ID()), ProjectID: string(p.ProjectID()),
				OwnerRef: "pm://plans/" + string(p.ID()),
			})
	})
}
