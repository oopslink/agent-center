package service

import (
	"context"
	"errors"
	"strings"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// CreateProjectCommand creates a Project. The creator becomes an owner member
// in the same transaction (membership is the write-gate, OQ6). No Conversation
// is created — channels are Org-level and explicit (plan §10 OQ10).
type CreateProjectCommand struct {
	OrganizationID string
	Name           string
	Description    string
	CreatedBy      pm.IdentityRef
}

// CreateProject writes the Project + creator owner-member + outbox event.
func (s *Service) CreateProject(ctx context.Context, cmd CreateProjectCommand) (pm.ProjectID, error) {
	if err := cmd.CreatedBy.Validate(); err != nil {
		return "", err
	}
	now := s.clock.Now()
	p, err := pm.NewProject(pm.NewProjectInput{
		ID: pm.ProjectID(s.idgen.NewEntityID("project")), OrganizationID: cmd.OrganizationID,
		Name: cmd.Name, Description: cmd.Description, CreatedBy: cmd.CreatedBy, CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	member, err := pm.NewProjectMember(pm.NewProjectMemberInput{
		ID: pm.MemberID(s.idgen.NewULID()), ProjectID: p.ID(), IdentityID: cmd.CreatedBy,
		Role: pm.RoleOwner, AddedBy: cmd.CreatedBy, CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	err = s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.projects.Save(txCtx, p); err != nil {
			return err
		}
		if err := s.members.Save(txCtx, member); err != nil {
			return err
		}
		// ADR-0047: auto-create the per-project built-in "assignment pool" Plan —
		// one per project, always-started (running), FLAT (no edges), a pull/no-wake
		// dispatch pool that makes its assigned tasks claimable. Nil-safe: only when a
		// PlanRepository is wired (matches the other optional-plan paths).
		if s.plans != nil {
			if err := s.createBuiltinPlan(txCtx, p.ID(), p.OrganizationID(), now); err != nil {
				return err
			}
		}
		return s.emit(txCtx, EvtProjectCreated,
			refsJSON(map[string]string{"project_id": string(p.ID())}),
			map[string]string{"project_id": string(p.ID()), "name": p.Name(), "created_by": string(cmd.CreatedBy)})
	})
	if err != nil {
		return "", err
	}
	return p.ID(), nil
}

// createBuiltinPlan creates the per-project built-in "assignment pool" Plan
// (ADR-0047) inside the caller's tx and immediately Starts it (the pool is
// always-started so its tasks become dispatchable/claimable without a manual
// StartPlan). It is FLAT (no dependency edges) and CreatorRef "system". The repo's
// partial unique index (one builtin per project) backstops a duplicate. No outbox
// event is emitted: the pool needs no 1:1 conversation (it is a pull/no-wake pool —
// dispatch records its readiness, it never @mentions).
func (s *Service) createBuiltinPlan(ctx context.Context, projectID pm.ProjectID, orgID string, now time.Time) error {
	bp, err := pm.NewPlan(pm.NewPlanInput{
		ID:         pm.PlanID(s.idgen.NewEntityID("plan")),
		ProjectID:  projectID,
		Name:       "[Built-in]",
		CreatorRef: "system",
		Builtin:    true,
		CreatedAt:  now,
	})
	if err != nil {
		return err
	}
	// Always-started: the pool is running from birth so dispatchReadyNodes can run.
	if err := bp.Start(now); err != nil {
		return err
	}
	if err := s.plans.Save(ctx, bp); err != nil {
		return err
	}
	// ADR-0047 §-1: emit pm.plan.created so the plan-participant projector creates the
	// pool's 1:1 conversation (owner_ref pm://plans/<id>) — same path as a structured
	// plan. No human creator participant (creator_ref=system); assignees join the
	// conversation as their tasks enter the pool (via EvtPlanParticipantsChanged).
	return s.emit(ctx, EvtPlanCreated,
		refsJSON(map[string]string{"plan_id": string(bp.ID()), "project_id": string(projectID)}),
		planEventPayload{
			PlanID:         string(bp.ID()),
			ProjectID:      string(projectID),
			OrganizationID: orgID,
			OwnerRef:       "pm://plans/" + string(bp.ID()),
			CreatorRef:     "system",
		})
}

// AddProjectMemberCommand adds a member to a project (actor must be a member).
type AddProjectMemberCommand struct {
	ProjectID  pm.ProjectID
	IdentityID pm.IdentityRef
	Role       pm.ProjectMemberRole
	Actor      pm.IdentityRef
}

// AddProjectMember writes the member + outbox event.
func (s *Service) AddProjectMember(ctx context.Context, cmd AddProjectMemberCommand) (pm.MemberID, error) {
	if err := cmd.Actor.Validate(); err != nil {
		return "", err
	}
	now := s.clock.Now()
	m, err := pm.NewProjectMember(pm.NewProjectMemberInput{
		ID: pm.MemberID(s.idgen.NewULID()), ProjectID: cmd.ProjectID, IdentityID: cmd.IdentityID,
		Role: cmd.Role, AddedBy: cmd.Actor, CreatedAt: now,
	})
	if err != nil {
		return "", err
	}
	err = s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, cmd.ProjectID, cmd.Actor); err != nil {
			return err
		}
		// #297: reject member-add on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, cmd.ProjectID); err != nil {
			return err
		}
		if err := s.members.Save(txCtx, m); err != nil {
			return err
		}
		return s.emit(txCtx, EvtMemberAdded,
			refsJSON(map[string]string{"project_id": string(cmd.ProjectID)}),
			map[string]string{"project_id": string(cmd.ProjectID), "identity_id": string(cmd.IdentityID), "role": string(m.Role())})
	})
	if err != nil {
		return "", err
	}
	return m.ID(), nil
}

// RemoveProjectMemberCommand removes a member from a project (v2.7 #207/#208).
type RemoveProjectMemberCommand struct {
	ProjectID  pm.ProjectID
	IdentityID pm.IdentityRef
	Actor      pm.IdentityRef
}

// RemoveProjectMember deletes the member row + emits pm.member.removed in one tx.
// authz (PD contract): the actor must be an OWNER of the project, and an owner
// member can NOT be removed (a project must always retain an owner). Returns
// ErrNotOwner (actor not owner → 403), ErrCannotRemoveOwner (target is owner →
// 409), or pm.ErrMemberNotFound (target not a member → 404).
func (s *Service) RemoveProjectMember(ctx context.Context, cmd RemoveProjectMemberCommand) error {
	if err := cmd.Actor.Validate(); err != nil {
		return err
	}
	if err := cmd.IdentityID.Validate(); err != nil {
		return err
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		// Owner-only gate (stricter than requireProjectMember): the actor must be
		// a member AND hold the owner role.
		actorM, err := s.members.FindByProjectAndIdentity(txCtx, cmd.ProjectID, cmd.Actor)
		if err != nil {
			if errors.Is(err, pm.ErrMemberNotFound) {
				return ErrNotMember
			}
			return err
		}
		if actorM.Role() != pm.RoleOwner {
			return ErrNotOwner
		}
		// Target must be a member; owners are protected (no ownerless project).
		target, err := s.members.FindByProjectAndIdentity(txCtx, cmd.ProjectID, cmd.IdentityID)
		if err != nil {
			return err // pm.ErrMemberNotFound → 404
		}
		if target.Role() == pm.RoleOwner {
			return ErrCannotRemoveOwner
		}
		// #297: reject member-remove on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, cmd.ProjectID); err != nil {
			return err
		}
		if err := s.members.Delete(txCtx, target.ID()); err != nil {
			return err
		}
		return s.emit(txCtx, EvtMemberRemoved,
			refsJSON(map[string]string{"project_id": string(cmd.ProjectID)}),
			map[string]string{"project_id": string(cmd.ProjectID), "identity_id": string(cmd.IdentityID)})
	})
}

// CreateIssueCommand creates an Issue.
type CreateIssueCommand struct {
	ProjectID   pm.ProjectID
	Title       string
	Description string
	// Tags is the optional initial label set (v2.10.3 T170 — agent create_issue
	// may seed labels at creation, matching the editable-tags surface of
	// BatchUpdateIssue). Validated by pm.NewIssue (1..16 chars each, deduped, <=10).
	Tags      []string
	CreatedBy pm.IdentityRef
}

// CreateIssue writes the Issue + outbox pm.issue.created (the projector creates
// the issue Conversation owner_ref pm://issues/{id} + syncs participants).
func (s *Service) CreateIssue(ctx context.Context, cmd CreateIssueCommand) (pm.IssueID, error) {
	if err := cmd.CreatedBy.Validate(); err != nil {
		return "", err
	}
	now := s.clock.Now()
	issueID := pm.IssueID(s.idgen.NewEntityID("issue"))
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, cmd.ProjectID, cmd.CreatedBy); err != nil {
			return err
		}
		// #297: reject issue-create on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, cmd.ProjectID); err != nil {
			return err
		}
		proj, perr := s.projects.FindByID(txCtx, cmd.ProjectID)
		if perr != nil {
			return perr
		}
		// v2.7.1 #245: allocate the per-org I<n> number in this tx (race-safe), so
		// the issue insert + counter advance commit atomically. nil orgSeq ⇒ 0.
		orgNumber, aerr := s.allocOrgNumber(txCtx, proj.OrganizationID(), "issue")
		if aerr != nil {
			return aerr
		}
		i, ierr := pm.NewIssue(pm.NewIssueInput{
			ID: issueID, ProjectID: cmd.ProjectID, Title: cmd.Title,
			Description: cmd.Description, Tags: cmd.Tags, CreatedBy: cmd.CreatedBy, CreatedAt: now, OrgNumber: orgNumber,
		})
		if ierr != nil {
			return ierr
		}
		if err := s.issues.Save(txCtx, i); err != nil {
			return err
		}
		return s.emit(txCtx, EvtIssueCreated,
			refsJSON(map[string]string{"issue_id": string(i.ID()), "project_id": string(cmd.ProjectID)}),
			issueEventPayload{
				IssueID: string(i.ID()), ProjectID: string(cmd.ProjectID),
				OrganizationID: proj.OrganizationID(),
				OwnerRef:       "pm://issues/" + string(i.ID()), Status: string(i.Status()),
				EffectiveSubscribers: EffectiveIssueSubscribers(i, nil),
			})
	})
	if err != nil {
		return "", err
	}
	return issueID, nil
}

// CreateTaskCommand creates a Task (independent or derived from an Issue).
//
// T199/WS3: the optional Assignee + Dispatch fields collapse the former hidden
// 3-step (create_task + assign_task + add_task_to_plan) into ONE atomic call.
// Both default to the zero value, preserving the pre-T199 backlog behavior:
//   - Assignee set        → assign on create (emits pm.task.assigned → the
//     WorkItemProjector mints the queued WorkItem + wake). An AGENT assignee is
//     granted project membership (cross-org guarded), exactly like AssignTask.
//   - Dispatch == true    → select the task into the project's built-in
//     Assignment Pool and record the dispatch in the SAME tx, so it is
//     immediately claimable (unassigned) / runnable (assigned) — no reconcile
//     wait. Without Dispatch the task stays in the backlog (assignment alone is
//     decoupled from runnability, T130).
type CreateTaskCommand struct {
	ProjectID        pm.ProjectID
	Title            string
	Description      string
	DerivedFromIssue pm.IssueID
	CreatedBy        pm.IdentityRef
	Assignee         pm.IdentityRef // optional one-step assign (T199/WS3)
	Dispatch         bool           // optional one-step dispatch into the built-in pool (T199/WS3)
	// Branch/Base/SkipMergeCheck are the cycle-node git metadata (v2.13.0 I18/F2),
	// set at create by scaffold_cycle_plan; empty/false for ordinary task creates.
	Branch         string
	Base           string
	SkipMergeCheck bool
	// Role is the cycle-node role discriminator (v2.13.0 I18/F3), set at create by
	// scaffold_cycle_plan; "" for ordinary task creates. It is the bit F3's
	// Integrate-complete merge guard + F4's board key on (Dev/Review/Integrate share
	// branch/base — §4.2 — so role is the only discriminator).
	Role pm.CycleNodeRole
	// Model is the optional hard-override executor model (F3 model routing, design
	// §5 & §10); "" = unset → executor model selected from the agent's allowed/default
	// models.
	Model string
	// RequiredCapabilities is the optional capability set the task demands of an
	// executor agent (v2.18.3 BE-1); canonicalized by the domain. Empty = unrestricted.
	RequiredCapabilities []string
}

// CreateTask writes the Task + outbox pm.task.created. The projector (B2-b)
// creates the task Conversation (owner_ref pm://tasks/{id}) + syncs
// participants to the effective subscriber set {creator}.
func (s *Service) CreateTask(ctx context.Context, cmd CreateTaskCommand) (pm.TaskID, error) {
	if err := cmd.CreatedBy.Validate(); err != nil {
		return "", err
	}
	now := s.clock.Now()
	taskID := pm.TaskID(s.idgen.NewEntityID("task"))
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		if err := s.requireProjectMember(txCtx, cmd.ProjectID, cmd.CreatedBy); err != nil {
			return err
		}
		// #297: reject task-create on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, cmd.ProjectID); err != nil {
			return err
		}
		// The task Conversation must be stamped with the project's org so org-scoped
		// endpoints (incl. a human replying to a waiting_input agent → wake) resolve it.
		proj, perr := s.projects.FindByID(txCtx, cmd.ProjectID)
		if perr != nil {
			return perr
		}
		// v2.7.1 #245: allocate the per-org T<n> number in this tx (race-safe). nil orgSeq ⇒ 0.
		orgNumber, aerr := s.allocOrgNumber(txCtx, proj.OrganizationID(), "task")
		if aerr != nil {
			return aerr
		}
		t, terr := pm.NewTask(pm.NewTaskInput{
			ID: taskID, ProjectID: cmd.ProjectID, Title: cmd.Title,
			Description: cmd.Description, DerivedFromIssue: cmd.DerivedFromIssue, CreatedBy: cmd.CreatedBy, CreatedAt: now, OrgNumber: orgNumber,
			Branch: cmd.Branch, Base: cmd.Base, SkipMergeCheck: cmd.SkipMergeCheck, Role: cmd.Role,
			Model: cmd.Model, RequiredCapabilities: cmd.RequiredCapabilities,
		})
		if terr != nil {
			return terr
		}
		if err := s.tasks.Save(txCtx, t); err != nil {
			return err
		}
		if err := s.emit(txCtx, EvtTaskCreated,
			refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(cmd.ProjectID)}),
			taskEventPayload{
				TaskID: string(t.ID()), ProjectID: string(cmd.ProjectID),
				OrganizationID: proj.OrganizationID(),
				OwnerRef:       "pm://tasks/" + string(t.ID()), Status: string(t.Status()),
				EffectiveSubscribers: EffectiveTaskSubscribers(t, nil),
			}); err != nil {
			return err
		}
		// T199/WS3 one-step dispatch+assign — same tx (atomic with the create, so a
		// rejected assignee/missing pool rolls back the whole operation, no orphan
		// task). Dispatch first (sets plan_id) so the subsequent assign can additively
		// join the assignee to the pool conversation.
		if cmd.Dispatch {
			if err := s.dispatchIntoBuiltinPool(txCtx, t, now); err != nil {
				return err
			}
		}
		if strings.TrimSpace(string(cmd.Assignee)) != "" {
			if err := s.assignOnCreate(txCtx, t, cmd.Assignee, now); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return taskID, nil
}

// dispatchIntoBuiltinPool selects t into its project's built-in Assignment Pool
// and records the (pull) dispatch in the caller's tx, so the task becomes a
// NodeDispatched pool member — immediately claimable (ListClaimablePool) and
// runnable (EnsureTaskRunnable) without waiting for the reconcile loop. Mirrors
// SelectTaskIntoPlan(pool) + dispatchBuiltinPool's RecordDispatch, fused for the
// one-step create+dispatch path (T199/WS3). RecordDispatch is INSERT-OR-IGNORE on
// the PK, so a later reconcile sweep is a harmless no-op.
func (s *Service) dispatchIntoBuiltinPool(ctx context.Context, t *pm.Task, now time.Time) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	pool, err := s.requireBuiltinPool(ctx, t.ProjectID())
	if err != nil {
		return err
	}
	if err := t.SetPlan(pool.ID(), now); err != nil {
		return err
	}
	if err := s.tasks.Update(ctx, t); err != nil {
		return err
	}
	// PULL dispatch: record-only (no @mention, no work-item, no wake) — exactly
	// what dispatchBuiltinPool does for a ready pool node.
	return s.plans.RecordDispatch(ctx, pool.ID(), t.ID(), now, "")
}

// assignOnCreate assigns t to assignee in the caller's tx — the create-time
// equivalent of AssignTask: it grants an AGENT assignee project membership
// (cross-org guarded, idempotent), persists, emits pm.task.assigned (the
// WorkItemProjector mints the queued WorkItem + wake), and — if t is already in a
// plan (dispatched above) — additively joins the assignee to the plan
// conversation. T199/WS3.
func (s *Service) assignOnCreate(ctx context.Context, t *pm.Task, assignee pm.IdentityRef, now time.Time) error {
	if err := assignee.Validate(); err != nil {
		return err
	}
	if err := t.Assign(assignee, now); err != nil {
		return err
	}
	if err := s.grantAgentProjectMembership(ctx, t, assignee, now); err != nil {
		return err
	}
	if err := s.tasks.Update(ctx, t); err != nil {
		return err
	}
	if err := s.emitTaskAssignEvent(ctx, t, EvtTaskAssigned, ""); err != nil {
		return err
	}
	return s.syncPlanParticipantOnAssign(ctx, t, assignee)
}

// requireBuiltinPool returns the project's built-in Assignment Pool plan, or
// ErrBuiltinPoolMissing when none exists (ADR-0047 invariant breach). Unlike
// builtinPoolOf it surfaces the underlying repo error instead of swallowing it
// (§17), since the one-step dispatch path must fail loud.
func (s *Service) requireBuiltinPool(ctx context.Context, projectID pm.ProjectID) (*pm.Plan, error) {
	plans, err := s.plans.ListByProject(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for _, p := range plans {
		if p.IsBuiltin() {
			return p, nil
		}
	}
	return nil, ErrBuiltinPoolMissing
}

// allocOrgNumber returns the next per-org T<n>/I<n> number for entityType
// ("task"|"issue"), or 0 when no org sequence is wired (nil-safe — keeps
// pre-#245 service constructions working). v2.7.1 #245.
func (s *Service) allocOrgNumber(ctx context.Context, orgID, entityType string) (int, error) {
	if s.orgSeq == nil {
		return 0, nil
	}
	return s.orgSeq.Allocate(ctx, orgID, entityType)
}

// SubscribeTask adds a MANUAL subscriber to a Task and re-emits the effective
// set. creator/assignee are derived (not rows) — subscribing them is a no-op
// on rows but they remain effective regardless.
func (s *Service) SubscribeTask(ctx context.Context, taskID pm.TaskID, identity, actor pm.IdentityRef) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	sub, err := pm.NewTaskSubscriber(taskID, identity, actor, now)
	if err != nil {
		return err
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject subscriber-add on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		if err := s.taskSubs.Add(txCtx, sub); err != nil {
			return err
		}
		return s.emitTaskSubsChanged(txCtx, t)
	})
}

// UnsubscribeTask removes a MANUAL subscriber row. If identity is the creator
// or current assignee they remain effective (role-derived) — this only deletes
// a manual row, so "can't unsubscribe while role holds" is automatic.
func (s *Service) UnsubscribeTask(ctx context.Context, taskID pm.TaskID, identity, actor pm.IdentityRef) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		t, err := s.tasks.FindByID(txCtx, taskID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, t.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject subscriber-remove on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, t.ProjectID()); err != nil {
			return err
		}
		if err := s.taskSubs.Remove(txCtx, taskID, identity); err != nil {
			return err
		}
		return s.emitTaskSubsChanged(txCtx, t)
	})
}

// emitTaskSubsChanged loads the manual rows, computes the effective set, and
// emits pm.task.subscribers_changed for the participant projector.
func (s *Service) emitTaskSubsChanged(ctx context.Context, t *pm.Task) error {
	manual, err := s.taskSubs.ListByTask(ctx, t.ID())
	if err != nil {
		return err
	}
	return s.emit(ctx, EvtTaskSubsChanged,
		refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
		taskEventPayload{
			TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
			OwnerRef: "pm://tasks/" + string(t.ID()), Assignee: string(t.Assignee()), Status: string(t.Status()),
			EffectiveSubscribers: EffectiveTaskSubscribers(t, manual),
		})
}

// SubscribeIssue / UnsubscribeIssue mirror the Task variants for Issues.
func (s *Service) SubscribeIssue(ctx context.Context, issueID pm.IssueID, identity, actor pm.IdentityRef) error {
	if err := identity.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	sub, err := pm.NewIssueSubscriber(issueID, identity, actor, now)
	if err != nil {
		return err
	}
	return s.runInTx(ctx, func(txCtx context.Context) error {
		i, err := s.issues.FindByID(txCtx, issueID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, i.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject subscriber-add on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, i.ProjectID()); err != nil {
			return err
		}
		if err := s.issueSubs.Add(txCtx, sub); err != nil {
			return err
		}
		return s.emitIssueSubsChanged(txCtx, i)
	})
}

func (s *Service) UnsubscribeIssue(ctx context.Context, issueID pm.IssueID, identity, actor pm.IdentityRef) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		i, err := s.issues.FindByID(txCtx, issueID)
		if err != nil {
			return err
		}
		if err := s.requireProjectMember(txCtx, i.ProjectID(), actor); err != nil {
			return err
		}
		// #297: reject subscriber-remove on an archived (read-only) project.
		if err := s.requireProjectMutable(txCtx, i.ProjectID()); err != nil {
			return err
		}
		if err := s.issueSubs.Remove(txCtx, issueID, identity); err != nil {
			return err
		}
		return s.emitIssueSubsChanged(txCtx, i)
	})
}

func (s *Service) emitIssueSubsChanged(ctx context.Context, i *pm.Issue) error {
	manual, err := s.issueSubs.ListByIssue(ctx, i.ID())
	if err != nil {
		return err
	}
	return s.emit(ctx, EvtIssueSubsChanged,
		refsJSON(map[string]string{"issue_id": string(i.ID()), "project_id": string(i.ProjectID())}),
		issueEventPayload{
			IssueID: string(i.ID()), ProjectID: string(i.ProjectID()),
			OwnerRef: "pm://issues/" + string(i.ID()), Status: string(i.Status()),
			EffectiveSubscribers: EffectiveIssueSubscribers(i, manual),
		})
}
