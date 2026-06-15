package service

import (
	"context"
	"errors"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Plan Shared Findings AppServices (v2.10, ADR-0053 — the DeLM "shared verified
// context" minimal slice). An agent records a compact FINDING (gist) back to its
// Plan, bound to the SOURCE task it executed; downstream/sibling agents read the
// findings (injected into dispatch by dispatchReadyNodes, or via ListPlanFindings)
// to build on prior progress instead of re-discovering it. Each AppService writes
// ONLY pm state + an outbox event in one tx (OQ1).

// ErrFindingsUnavailable is returned when no PlanFindingRepository is wired
// (s.findings == nil) — fail-loud, mirroring ErrPlansUnavailable.
var ErrFindingsUnavailable = errors.New("projectmanager: plan finding repository unavailable — finding operations are not wired")

// RecordFindingCommand captures the args for RecordFinding.
type RecordFindingCommand struct {
	PlanID    pm.PlanID
	TaskID    pm.TaskID
	AuthorRef pm.IdentityRef
	Kind      pm.PlanFindingKind
	Content   string
}

// RecordFinding admits a finding into a Plan's shared context (ADR-0053). The v1
// ADMISSION gate is EVIDENCE ATTRIBUTION (decision 2), not LLM verification:
//
//	(1) the plan exists;
//	(2) the project is mutable (not archived — recording is a mutation, §297);
//	(3) the actor is a member of the plan's project (requireProjectMember);
//	(4) the source task exists and BELONGS to this plan (ErrFindingTaskNotInPlan);
//	(5) the actor IS the source task's assignee (ErrFindingNotTaskAssignee) — you
//	    can only gist what you actually executed.
//
// On success it constructs the finding (with project_id derived from the plan),
// persists it, and emits pm.plan_finding.recorded — all in one tx.
func (s *Service) RecordFinding(ctx context.Context, cmd RecordFindingCommand) (pm.PlanFindingID, error) {
	if s.findings == nil {
		return "", ErrFindingsUnavailable
	}
	if s.plans == nil {
		return "", ErrPlansUnavailable
	}
	if err := cmd.AuthorRef.Validate(); err != nil {
		return "", err
	}
	now := s.clock.Now()
	id := pm.PlanFindingID(s.idgen.NewULID())
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		p, err := s.plans.FindByID(txCtx, cmd.PlanID)
		if err != nil {
			return err // pm.ErrPlanNotFound when missing
		}
		// (3) actor must be a project member; (2) project must be mutable.
		if err := s.requireProjectMember(txCtx, p.ProjectID(), cmd.AuthorRef); err != nil {
			return err
		}
		if err := s.requireProjectMutable(txCtx, p.ProjectID()); err != nil {
			return err
		}
		// (4) source task exists + belongs to this plan.
		t, err := s.tasks.FindByID(txCtx, cmd.TaskID)
		if err != nil {
			return err // pm.ErrTaskNotFound when missing
		}
		if t.PlanID() != cmd.PlanID {
			return pm.ErrFindingTaskNotInPlan
		}
		// (4b) defense-in-depth (no-FK, review #3): the source task must also be in
		// the plan's PROJECT. SelectTaskIntoPlan already guarantees this, but an
		// inconsistent task row must not let an agent ground a finding in another
		// project's task while it is stored under this plan's project.
		if t.ProjectID() != p.ProjectID() {
			return pm.ErrFindingTaskNotInPlan
		}
		// (5) ADMISSION: the actor must be the source task's assignee.
		if t.Assignee() != cmd.AuthorRef {
			return pm.ErrFindingNotTaskAssignee
		}
		f, err := pm.NewPlanFinding(pm.NewPlanFindingInput{
			ID: id, PlanID: cmd.PlanID, TaskID: cmd.TaskID, ProjectID: p.ProjectID(),
			AuthorRef: cmd.AuthorRef, Kind: cmd.Kind, Content: cmd.Content, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		if err := s.findings.Save(txCtx, f); err != nil {
			return err
		}
		proj, err := s.projects.FindByID(txCtx, p.ProjectID())
		if err != nil {
			return err
		}
		return s.emit(txCtx, EvtPlanFindingRecorded,
			refsJSON(map[string]string{
				"finding_id": string(id), "plan_id": string(cmd.PlanID),
				"task_id": string(cmd.TaskID), "project_id": string(p.ProjectID()),
			}),
			planFindingEventPayload{
				FindingID: string(id), PlanID: string(cmd.PlanID), TaskID: string(cmd.TaskID),
				ProjectID: string(p.ProjectID()), OrganizationID: proj.OrganizationID(),
				AuthorRef: string(cmd.AuthorRef), Kind: string(cmd.Kind),
			})
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// ListPlanFindings returns a Plan's findings (oldest-first), the read path behind
// the list_findings tool and the pull-pool's "read shared context" need. It is
// ACTOR-AWARE (review #2): the actor must be a member of the plan's project, and a
// missing plan yields ErrPlanNotFound — a worker-bound agent cannot read another
// project's shared findings just by knowing/guessing a plan_id. Legitimate readers
// (a plan-task assignee) are project members via AssignTask, so they still pass.
func (s *Service) ListPlanFindings(ctx context.Context, planID pm.PlanID, actor pm.IdentityRef) ([]*pm.PlanFinding, error) {
	if s.findings == nil {
		return nil, ErrFindingsUnavailable
	}
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	p, err := s.plans.FindByID(ctx, planID)
	if err != nil {
		return nil, err // pm.ErrPlanNotFound when missing
	}
	if err := s.requireProjectMember(ctx, p.ProjectID(), actor); err != nil {
		return nil, err
	}
	return s.findings.ListByPlan(ctx, planID)
}

// RetractFinding removes a finding (ADR-0053: findings are immutable; to change one
// you retract + re-record). The actor must be the finding's AUTHOR or a project
// OWNER (ErrFindingForbidden otherwise). Emits pm.plan_finding.retracted.
func (s *Service) RetractFinding(ctx context.Context, findingID pm.PlanFindingID, actor pm.IdentityRef) error {
	if s.findings == nil {
		return ErrFindingsUnavailable
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		f, err := s.findings.FindByID(txCtx, findingID)
		if err != nil {
			return err // pm.ErrPlanFindingNotFound when missing
		}
		if err := s.requireFindingRetractor(txCtx, f, actor); err != nil {
			return err
		}
		if err := s.findings.Delete(txCtx, findingID); err != nil {
			return err
		}
		return s.emit(txCtx, EvtPlanFindingRetracted,
			refsJSON(map[string]string{
				"finding_id": string(findingID), "plan_id": string(f.PlanID()),
				"project_id": string(f.ProjectID()),
			}),
			planFindingRetractPayload{
				FindingID: string(findingID), PlanID: string(f.PlanID()),
				ProjectID: string(f.ProjectID()), RetractedBy: string(actor),
			})
	})
}

// requireFindingRetractor enforces the retract authz: the actor is the finding's
// author, OR a project owner. A non-member / non-owner non-author → ErrFindingForbidden.
func (s *Service) requireFindingRetractor(ctx context.Context, f *pm.PlanFinding, actor pm.IdentityRef) error {
	if actor == f.AuthorRef() {
		return nil
	}
	m, err := s.members.FindByProjectAndIdentity(ctx, f.ProjectID(), actor)
	if err != nil {
		if errors.Is(err, pm.ErrMemberNotFound) {
			return pm.ErrFindingForbidden
		}
		return err
	}
	if m.Role() != pm.RoleOwner {
		return pm.ErrFindingForbidden
	}
	return nil
}

// planFindingEventPayload is the JSON payload for pm.plan_finding.recorded.
type planFindingEventPayload struct {
	FindingID      string `json:"finding_id"`
	PlanID         string `json:"plan_id"`
	TaskID         string `json:"task_id"`
	ProjectID      string `json:"project_id"`
	OrganizationID string `json:"organization_id"`
	AuthorRef      string `json:"author_ref"`
	Kind           string `json:"kind"`
}

// planFindingRetractPayload is the JSON payload for pm.plan_finding.retracted.
type planFindingRetractPayload struct {
	FindingID   string `json:"finding_id"`
	PlanID      string `json:"plan_id"`
	ProjectID   string `json:"project_id"`
	RetractedBy string `json:"retracted_by"`
}
