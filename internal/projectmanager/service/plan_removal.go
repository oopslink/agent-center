package service

import (
	"context"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Plan deletion and archival run their
// PM-state writes + the cross-BC conversation-cleanup event in ONE local tx
// (OQ1): the conversation hard-delete (DeletePlan) / archive (ArchivePlan) is an
// idempotent projection on EvtPlanDeleted / EvtPlanArchived (PlanParticipantProjector),
// NEVER inline — PM stays decoupled from Conversation.

// DeletePlan HARD-deletes a never-started pending Plan. In one tx it:
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
		// Hard deletion is only for never-started authoring mistakes. Once a Plan
		// has executed, discard/archive preserve its immutable history instead.
		if p.Status() != pm.PlanPending {
			return pm.ErrPlanNotPending
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
		// (b'') 2026-07-03 plan-stage-model: cascade-delete the plan's stages (a plan's
		// stages die with the plan). nil-safe (Stage-unwired constructions skip it).
		if s.stages != nil {
			if err := s.stages.DeleteByPlan(txCtx, planID); err != nil {
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

// ArchivePlan sets an orthogonal marker on a terminal Plan. It never rewrites Plan
// lifecycle or Task lifecycle; done/discarded remains the durable outcome. The actor
// must be the Plan creator, a Project owner, or an active owner of the Project's
// organization.
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
		if err := s.requirePlanCreatorOrProjectOwner(txCtx, p, actor); err != nil {
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
		if err := p.Archive(now, actor); err != nil {
			return err
		}
		if err := s.plans.Update(txCtx, p); err != nil {
			return err
		}
		// Emit the compatibility event so the conversation projection is archived.
		return s.emit(txCtx, EvtPlanArchived,
			refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(p.ProjectID())}),
			planEventPayload{
				PlanID: string(p.ID()), ProjectID: string(p.ProjectID()),
				OwnerRef: "pm://plans/" + string(p.ID()),
			})
	})
}

// DiscardPlan permanently abandons a pending/running/paused Plan. Remaining
// non-terminal member Tasks are finalized to discarded in the same transaction;
// terminal Task history is preserved. The actor must be the Plan creator, a
// Project owner, or an active owner of the Project's organization.
func (s *Service) DiscardPlan(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requirePlanCreatorOrProjectOwner(txCtx, p, actor); err != nil {
			return err
		}
		tasks, err := s.tasks.ListByPlan(txCtx, planID)
		if err != nil {
			return err
		}
		if err := p.Discard(now); err != nil {
			return err
		}
		for _, task := range tasks {
			if pm.TaskIsDone(task.Status()) {
				continue
			}
			prev := task.Status()
			if err := task.Discard(now); err != nil {
				return err
			}
			if err := s.tasks.Update(txCtx, task); err != nil {
				return err
			}
			s.auditTaskStatusChange(txCtx, task, prev, actor)
		}
		if err := s.plans.Update(txCtx, p); err != nil {
			return err
		}
		if err := s.clearPlanBlockedOn(txCtx, p.ID()); err != nil {
			return err
		}
		s.auditPlan(txCtx, p, pm.AuditPlanStopped, actor, map[string]any{"status": string(p.Status()), "discarded": true})
		return s.emitPlanLifecycle(txCtx, p, EvtPlanStopped)
	})
}
