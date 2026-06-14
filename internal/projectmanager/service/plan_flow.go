package service

import (
	"context"
	"errors"

	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// ErrPlansUnavailable is returned by the Plan AppServices when no PlanRepository
// is wired (s.plans == nil). It mirrors the fail-loud guard other optional deps
// use (e.g. agent directory) so a missing dependency never silently no-ops.
var ErrPlansUnavailable = errors.New("projectmanager: plan repository unavailable — Plan operations are not wired")

// CreatePlanCommand creates a Plan (v2.9 plan orchestration, design §2). A Plan
// is project-scoped; the creator must be a project member and becomes the first
// participant of the Plan's 1:1 Conversation (§9.5).
type CreatePlanCommand struct {
	ProjectID   pm.ProjectID
	Name        string
	Description string
	TargetDate  *time.Time
	CreatedBy   pm.IdentityRef
}

// CreatePlan writes the Plan (draft) + outbox pm.plan.created. The projector
// (PlanParticipantProjector) creates the Plan's 1:1 Conversation (owner_ref
// pm://plans/{id}, kind=plan) with the creator as the first participant and
// persists the new conversation id back onto the Plan. Mirrors CreateTask: one
// local tx writes ONLY PM state + the event (OQ1); the cross-BC conversation
// create is an idempotent projection.
//
// Nil-guard: with no PlanRepository wired, returns ErrPlansUnavailable
// (fail-loud, mirroring the other optional-dep guards) rather than panicking.
func (s *Service) CreatePlan(ctx context.Context, cmd CreatePlanCommand) (pm.PlanID, error) {
	if s.plans == nil {
		return "", ErrPlansUnavailable
	}
	if err := cmd.CreatedBy.Validate(); err != nil {
		return "", err
	}
	now := s.clock.Now()
	planID := pm.PlanID(s.idgen.NewEntityID("plan"))
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, cmd.ProjectID, cmd.CreatedBy); err != nil {
			return err
		}
		// #297: reject plan-create on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, cmd.ProjectID); err != nil {
			return err
		}
		// The Plan Conversation must be stamped with the project's org so org-scoped
		// endpoints (incl. the orchestrator's @mention → agent wake, §9.5) resolve it.
		proj, perr := s.projects.FindByID(txCtx, cmd.ProjectID)
		if perr != nil {
			return perr
		}
		p, nerr := pm.NewPlan(pm.NewPlanInput{
			ID: planID, ProjectID: cmd.ProjectID, Name: cmd.Name,
			Description: cmd.Description, CreatorRef: cmd.CreatedBy,
			TargetDate: cmd.TargetDate, CreatedAt: now,
		})
		if nerr != nil {
			return nerr
		}
		if serr := s.plans.Save(txCtx, p); serr != nil {
			return serr
		}
		return s.emit(txCtx, EvtPlanCreated,
			refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(cmd.ProjectID)}),
			planEventPayload{
				PlanID: string(p.ID()), ProjectID: string(cmd.ProjectID),
				OrganizationID: proj.OrganizationID(),
				OwnerRef:       "pm://plans/" + string(p.ID()),
				CreatorRef:     string(cmd.CreatedBy),
				// ADD-ONLY (additive §9.5): the creator is the first participant.
				Participants: []string{string(cmd.CreatedBy)},
			})
	})
	if err != nil {
		return "", err
	}
	return planID, nil
}

// SelectTaskIntoPlan selects a backlog task into a draft Plan (design §2/§9.6d).
// Guards (Task ↔ Plan = 0..1, same project, draft-only):
//   - the actor must be a project member;
//   - the Plan must be in draft (ErrPlanNotDraft — the DAG/task set is editable
//     only in draft, §9.4);
//   - the task's project must equal the Plan's project (ErrPlanProjectMismatch);
//   - if the task already belongs to a DIFFERENT plan → ErrTaskInOtherPlan;
//     re-selecting into the SAME plan is a no-op.
//
// On success it sets task.plan_id and emits pm.plan.participants_changed carrying
// the task's CURRENT assignee (if any) as an ADD-ONLY participant delta so the
// orchestrator can @mention (wake) that assignee in the Plan conversation (§9.5 —
// a mention only reaches members, so the assignee MUST be a participant).
func (s *Service) SelectTaskIntoPlan(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, actor pm.IdentityRef) error {
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
		// #297: reject select-task-into-plan on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		// ADR-0047: the built-in pool is the ONE running plan that accepts new tasks
		// (that is how a task enters the claimable pool — selecting it makes it
		// dispatchable/claimable). A structured plan is still draft-only (§9.4): its
		// task-set/DAG is editable only while stopped.
		if !p.IsBuiltin() && p.Status() != pm.PlanDraft {
			return pm.ErrPlanNotDraft
		}
		if t.ProjectID() != p.ProjectID() {
			return pm.ErrPlanProjectMismatch
		}
		if cur := t.PlanID(); cur != "" && cur != planID {
			return pm.ErrTaskInOtherPlan
		}
		if t.PlanID() == planID {
			return nil // already selected into this plan — idempotent no-op
		}
		if err := t.SetPlan(planID, now); err != nil {
			return err
		}
		if err := s.tasks.Update(txCtx, t); err != nil {
			return err
		}
		// ADD-ONLY participant delta: the task's current assignee (if assigned).
		// An unassigned task adds no one (no participant to sync yet); when it is
		// later assigned, the assign path's effective-set sync covers the task
		// conversation, and a re-select/assignee change re-emits this for the plan.
		var participants []string
		if a := string(t.Assignee()); a != "" {
			participants = []string{a}
		}
		return s.emit(txCtx, EvtPlanParticipantsChanged,
			refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(p.ProjectID())}),
			planEventPayload{
				PlanID: string(p.ID()), ProjectID: string(p.ProjectID()),
				OrganizationID: "", OwnerRef: "pm://plans/" + string(p.ID()),
				Participants: participants,
			})
	})
}

// RemoveTaskFromPlan removes a task from its Plan (back to the backlog). It
// clears task.plan_id AND removes every depends_on edge that references the task
// in this plan (a removed node can have no edges, §9.8). It is ADDITIVE-safe: it
// does NOT remove the task's assignee from the Plan conversation participants
// (§9.5 — participants are monotonic; preserve history access, don't yank
// mid-plan). No pm.plan.participants_changed is emitted (nothing is added).
func (s *Service) RemoveTaskFromPlan(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, actor pm.IdentityRef) error {
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
		// #297: reject remove-task-from-plan on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		// §9.4: the plan's task-set + DAG are editable only in draft. Removing a
		// node from a running/done plan would break the executing DAG — mirror the
		// gate on SelectTaskIntoPlan / Add+RemovePlanDependency (add/remove symmetry).
		if p.Status() != pm.PlanDraft {
			return pm.ErrPlanNotDraft
		}
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		// Remove any depends_on edges referencing this task in this plan first, so
		// the DAG never references a task no longer in the plan (§9.8).
		edges, err := s.plans.ListDependencies(txCtx, planID)
		if err != nil {
			return err
		}
		for _, e := range edges {
			if e.FromTaskID == taskID || e.ToTaskID == taskID {
				if rerr := s.plans.RemoveDependency(txCtx, e); rerr != nil {
					return rerr
				}
			}
		}
		if err := t.ClearPlan(now); err != nil {
			return err
		}
		return s.tasks.Update(txCtx, t)
	})
}

// UpdatePlanCommand renames / re-goals / re-targets a DRAFT Plan (§9.4 — edits
// only in draft). Absent (nil) fields are left unchanged; TargetDate is a
// **TargetDateSet** + value so "" can distinguish "clear" from "unchanged".
type UpdatePlanCommand struct {
	PlanID        pm.PlanID
	Name          *string
	Description   *string
	TargetDateSet bool
	TargetDate    *time.Time
	Actor         pm.IdentityRef
}

// UpdatePlan applies a dirty-only patch to a draft Plan (§9.4). The actor must be
// a project member; a non-draft Plan is rejected (ErrPlanNotDraft).
func (s *Service) UpdatePlan(ctx context.Context, cmd UpdatePlanCommand) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, cmd.PlanID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), cmd.Actor); err != nil {
			return err
		}
		// #297: reject plan edit on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		if p.Status() != pm.PlanDraft {
			return pm.ErrPlanNotDraft
		}
		if cmd.Name != nil {
			if err := p.Rename(*cmd.Name, now); err != nil {
				return err
			}
		}
		if cmd.Description != nil {
			p.SetDescription(*cmd.Description, now)
		}
		if cmd.TargetDateSet {
			p.SetTargetDate(cmd.TargetDate, now)
		}
		return s.plans.Update(txCtx, p)
	})
}

// AddPlanDependency adds a depends_on edge (from_task depends_on to_task) to a
// DRAFT Plan's DAG (§9.4 — edits only in draft). The repo's AddDependency
// rejects self-edges + cycles (acyclic invariant, §9.6a) before persisting. Both
// tasks must already be selected into the Plan. The actor must be a project member.
func (s *Service) AddPlanDependency(ctx context.Context, planID pm.PlanID, fromTaskID, toTaskID pm.TaskID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject add-dependency on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		// ADR-0047: the built-in pool is a FLAT pool — dependency edges are never
		// allowed (it is always running, never draft; this guard documents WHY).
		if p.IsBuiltin() {
			return pm.ErrBuiltinPlanNoEdges
		}
		if p.Status() != pm.PlanDraft {
			return pm.ErrPlanNotDraft
		}
		// Both endpoints must be tasks selected into THIS plan (§9.8 scoping).
		for _, id := range []pm.TaskID{fromTaskID, toTaskID} {
			t, terr := s.tasks.FindByID(txCtx, id)
			if terr != nil {
				return terr
			}
			if t.PlanID() != planID {
				return pm.ErrPlanProjectMismatch
			}
		}
		return s.plans.AddDependency(txCtx, pm.Dependency{PlanID: planID, FromTaskID: fromTaskID, ToTaskID: toTaskID})
	})
}

// RemovePlanDependency removes a depends_on edge from a DRAFT Plan's DAG (§9.4).
// Idempotent: removing a non-existent edge is a no-op. Actor must be a member.
func (s *Service) RemovePlanDependency(ctx context.Context, planID pm.PlanID, fromTaskID, toTaskID pm.TaskID, actor pm.IdentityRef) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, planID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, p.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject remove-dependency on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		if p.Status() != pm.PlanDraft {
			return pm.ErrPlanNotDraft
		}
		return s.plans.RemoveDependency(txCtx, pm.Dependency{PlanID: planID, FromTaskID: fromTaskID, ToTaskID: toTaskID})
	})
}
