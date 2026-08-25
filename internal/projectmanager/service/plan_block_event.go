package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func (s *Service) projectPlanBlockEvent(txCtx context.Context, t *pm.Task, reason string, reasonType pm.BlockReasonType, actor pm.IdentityRef) error {
	if s.plans == nil || t.PlanID() == "" {
		return nil
	}
	p, err := s.plans.FindByID(txCtx, t.PlanID())
	if err != nil || p.IsBuiltin() || p.ActiveGenerationID() == "" {
		return err
	}
	g, err := s.plans.FindGenerationByID(txCtx, p.ActiveGenerationID())
	if err != nil {
		return err
	}
	var nodeID string
	effective := false
	for _, n := range g.Snapshot.Tasks {
		if n.TaskID == t.ID() {
			nodeID = n.NodeID
			effective = true
			break
		}
	}
	if !effective {
		return nil
	}
	if p.OwnerRef() == "" {
		return pm.ErrPlanOwnerRequired
	}
	blockVersion := t.Version()
	key := fmt.Sprintf("plan_block:%s:%s:%s:%d", p.ID(), p.ActiveGenerationID(), t.ID(), blockVersion)
	if _, found, ferr := s.plans.FindPlanBlockEventByKey(txCtx, key); ferr != nil || found {
		return ferr
	}
	now := s.clock.Now()
	eventID := pm.PlanBlockEventID(s.idgen.NewEntityID("plan-block"))
	downstream := impactedDownstreamJSON(g.Snapshot.Edges, t.ID())
	ev, err := pm.NewPlanBlockEvent(pm.PlanBlockEvent{
		EventID: eventID, IdempotencyKey: key, PlanID: p.ID(), GenerationID: p.ActiveGenerationID(), TaskID: t.ID(), NodeID: nodeID,
		BlockVersion: blockVersion, BlockedReason: reason, ReasonType: reasonType, BlockedBy: actor, BlockedAt: now,
		Active: true, Effective: true, ImpactedDownstreamJSON: downstream, OwnerRef: p.OwnerRef(),
		NotificationState: pm.PlanBlockNotifyPending, CreatedAt: now, UpdatedAt: now,
	})
	if err != nil {
		return err
	}
	created, err := s.plans.UpsertPlanBlockEvent(txCtx, ev)
	if err != nil || !created {
		return err
	}
	p.RequireAttention(eventID, now)
	if err := s.plans.Update(txCtx, p); err != nil {
		return err
	}
	if err := s.materializeBlockedOn(txCtx, p); err != nil {
		return err
	}
	policy := p.RecoveryPolicy()
	if err := s.emit(txCtx, EvtPlanBlockOwnerWake,
		refsJSON(map[string]string{"plan_id": string(p.ID()), "task_id": string(t.ID()), "event_id": string(ev.EventID)}),
		planBlockOwnerWakePayload{
			EventID: string(ev.EventID), IdempotencyKey: ev.IdempotencyKey, PlanID: string(p.ID()), ProjectID: string(p.ProjectID()),
			GenerationID: string(p.ActiveGenerationID()), TaskID: string(t.ID()), OwnerRef: string(p.OwnerRef()),
			ConversationID: p.ConversationID(), Reason: reason, ReasonType: string(reasonType),
			RemindAfterSeconds: policy.RemindAfterSeconds, EscalateAfterSeconds: policy.EscalateAfterSeconds,
		}); err != nil {
		return err
	}
	s.auditPlan(txCtx, p, pm.AuditPlanBlockCreated, actor, map[string]any{
		"event_id": string(ev.EventID), "task_id": string(t.ID()), "generation_id": string(p.ActiveGenerationID()),
		"reason_type": string(reasonType), "reason": reason, "notification_state": string(pm.PlanBlockNotifyPending),
	})
	return nil
}

func impactedDownstreamJSON(edges []pm.PlanGenerationEdgeSnapshot, taskID pm.TaskID) string {
	children := map[pm.TaskID][]pm.TaskID{}
	for _, e := range edges {
		children[e.ToTaskID] = append(children[e.ToTaskID], e.FromTaskID)
	}
	seen := map[pm.TaskID]bool{}
	var out []string
	var walk func(pm.TaskID)
	walk = func(id pm.TaskID) {
		for _, c := range children[id] {
			if seen[c] {
				continue
			}
			seen[c] = true
			out = append(out, string(c))
			walk(c)
		}
	}
	walk(taskID)
	b, err := json.Marshal(out)
	if err != nil {
		return "[]"
	}
	return string(b)
}

type AcknowledgePlanBlockCommand struct {
	EventID pm.PlanBlockEventID
	Actor   pm.IdentityRef
}

func (s *Service) ListPlanBlockEvents(ctx context.Context, planID pm.PlanID, activeOnly bool) ([]pm.PlanBlockEvent, error) {
	if s.plans == nil {
		return nil, ErrPlansUnavailable
	}
	if _, err := s.plans.FindByID(ctx, planID); err != nil {
		return nil, err
	}
	return s.plans.ListPlanBlockEvents(ctx, planID, activeOnly)
}

func (s *Service) AcknowledgePlanBlock(ctx context.Context, cmd AcknowledgePlanBlockCommand) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	if err := cmd.Actor.Validate(); err != nil {
		return err
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		ev, ok, err := s.plans.FindPlanBlockEventByID(txCtx, cmd.EventID)
		if err != nil {
			return err
		}
		if !ok || !ev.Active || !ev.Effective || !ev.ResolvedAt.IsZero() {
			return pm.ErrPlanBlockEventNotFound
		}
		p, err := s.plans.FindByID(txCtx, ev.PlanID)
		if err != nil {
			return err
		}
		if err := s.requireCurrentPlanOwner(txCtx, p, cmd.Actor, now); err != nil {
			return err
		}
		if err := s.plans.AcknowledgePlanBlockEvent(txCtx, cmd.EventID, cmd.Actor, now); err != nil {
			return err
		}
		s.auditPlan(txCtx, p, pm.AuditPlanBlockAcknowledged, cmd.Actor, map[string]any{
			"event_id": string(cmd.EventID), "task_id": string(ev.TaskID), "generation_id": string(ev.GenerationID),
		})
		return nil
	})
}

type ResolvePlanBlockCommand struct {
	EventID        pm.PlanBlockEventID
	ResolutionKind string
	Note           string
	Actor          pm.IdentityRef
}

func (s *Service) ResolvePlanBlock(ctx context.Context, cmd ResolvePlanBlockCommand) error {
	if s.plans == nil {
		return ErrPlansUnavailable
	}
	if err := cmd.Actor.Validate(); err != nil {
		return err
	}
	kind := pm.PlanBlockResolutionKind(strings.TrimSpace(cmd.ResolutionKind))
	if !kind.IsValid() {
		return pm.ErrInvalidPlanBlockResolution
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		ev, ok, err := s.plans.FindPlanBlockEventByID(txCtx, cmd.EventID)
		if err != nil {
			return err
		}
		if !ok || !ev.Active || !ev.Effective || !ev.ResolvedAt.IsZero() {
			return pm.ErrPlanBlockEventNotFound
		}
		p, err := s.plans.FindByID(txCtx, ev.PlanID)
		if err != nil {
			return err
		}
		if err := s.requireCurrentPlanOwner(txCtx, p, cmd.Actor, now); err != nil {
			return err
		}
		switch kind {
		case pm.PlanBlockResumeOriginal:
			t, err := s.tasks.FindByID(txCtx, ev.TaskID)
			if err != nil {
				return err
			}
			if strings.TrimSpace(t.BlockedReason()) == "" {
				return pm.ErrPlanBlockStillBlocked
			}
			prevReasonType := t.BlockedReasonType()
			if err := t.Unblock(cmd.Note, cmd.Actor, now); err != nil {
				return err
			}
			if err := s.enforceConcurrencyCap(txCtx, t); err != nil {
				return err
			}
			if err := s.tasks.Update(txCtx, t); err != nil {
				return err
			}
			if err := s.flushActionLogs(txCtx, t); err != nil {
				return err
			}
			if herr := s.reopenStuckPlanNode(txCtx, t, "plan_block_resume_original"); herr != nil {
				return herr
			}
			if err := s.emitTaskAssignEvent(txCtx, t, EvtTaskAssigned, ""); err != nil {
				return err
			}
			if prevReasonType == pm.BlockReasonInputRequired {
				if err := s.emitTaskInputEvent(txCtx, EvtTaskInputReplied, taskInputEventPayload{
					TaskID: string(t.ID()), ProjectID: string(t.ProjectID()), OwnerRef: "pm://tasks/" + string(t.ID()),
					ActorRef: string(cmd.Actor), Comment: cmd.Note,
				}); err != nil {
					return err
				}
			}
			s.auditTaskUnblocked(txCtx, t, cmd.Actor)
		case pm.PlanBlockReplaceWithContinuation, pm.PlanBlockBypassRemoveNode:
			return fmt.Errorf("%w: %s requires evolve_plan_generation", pm.ErrInvalidPlanBlockResolution, kind)
		case pm.PlanBlockPauseOrDiscardPlan:
			switch strings.ToLower(strings.TrimSpace(cmd.Note)) {
			case "discard":
				if err := p.Discard(now); err != nil {
					return err
				}
			default:
				if err := p.Pause(now); err != nil {
					return err
				}
			}
			if err := s.plans.Update(txCtx, p); err != nil {
				return err
			}
		}
		if err := s.plans.ResolvePlanBlockEvent(txCtx, cmd.EventID, cmd.Actor, string(kind), strings.TrimSpace(cmd.Note), now); err != nil {
			return err
		}
		remaining, err := s.plans.ListPlanBlockEvents(txCtx, p.ID(), true)
		if err != nil {
			return err
		}
		if len(remaining) == 0 {
			p.ClearAttention(now)
			if err := s.plans.Update(txCtx, p); err != nil {
				return err
			}
		}
		s.auditPlan(txCtx, p, pm.AuditPlanBlockResolved, cmd.Actor, map[string]any{
			"event_id": string(cmd.EventID), "task_id": string(ev.TaskID), "generation_id": string(ev.GenerationID),
			"resolution_kind": strings.TrimSpace(cmd.ResolutionKind), "resolution_note": strings.TrimSpace(cmd.Note),
		})
		return nil
	})
}

func (s *Service) requireCurrentPlanOwner(ctx context.Context, p *pm.Plan, actor pm.IdentityRef, now time.Time) error {
	if p == nil {
		return pm.ErrPlanNotFound
	}
	if err := s.reconcilePlanOwner(ctx, p, actor, "owner_action", now); err != nil {
		return err
	}
	if p.OwnerRef() != actor {
		s.auditPlan(ctx, p, pm.AuditPlanBlockAcknowledged, actor, map[string]any{
			"denied": "not_current_owner", "owner_ref": string(p.OwnerRef()),
		})
		return pm.ErrPlanOwnerOnly
	}
	return nil
}

func (s *Service) reconcilePlanOwner(ctx context.Context, p *pm.Plan, actor pm.IdentityRef, reason string, now time.Time) error {
	if p == nil {
		return nil
	}
	if ok, err := s.validPlanOwner(ctx, p.ProjectID(), p.OwnerRef()); err != nil {
		return err
	} else if ok {
		return nil
	}
	prevOwner, prevBackup := p.OwnerRef(), p.BackupOwnerRef()
	var target pm.IdentityRef
	if prevBackup != "" {
		if ok, err := s.validPlanOwner(ctx, p.ProjectID(), prevBackup); err != nil {
			return err
		} else if ok {
			target = prevBackup
		}
	}
	if target == "" {
		members, err := s.members.ListByProject(ctx, p.ProjectID())
		if err != nil {
			return err
		}
		for _, m := range members {
			ok, err := s.validPlanOwner(ctx, p.ProjectID(), m.IdentityID())
			if err != nil {
				return err
			}
			if m.Role() == pm.RoleOwner && ok {
				target = m.IdentityID()
				break
			}
		}
	}
	if target != "" {
		if err := p.SetOwner(target, "", now); err != nil {
			return err
		}
	}
	p.RequireAttention(p.LastAttentionEventID(), now)
	if err := s.plans.Update(ctx, p); err != nil {
		return err
	}
	s.auditPlan(ctx, p, pm.AuditPlanOwnerTransferred, actor, map[string]any{
		"field": "owner_ref", "from": string(prevOwner), "to": string(target),
		"backup_from": string(prevBackup), "backup_to": "", "reason": reason,
		"attention_status": string(p.AttentionStatus()), "fail_closed": target == "",
	})
	if s.outbox != nil {
		if err := s.emit(ctx, EvtPlanOwnerAccessLost,
			refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(p.ProjectID())}),
			map[string]any{
				"plan_id": string(p.ID()), "project_id": string(p.ProjectID()),
				"previous_owner_ref": string(prevOwner), "owner_ref": string(target),
				"backup_owner_ref": string(prevBackup), "reason": reason,
				"attention_status": string(p.AttentionStatus()), "fail_closed": target == "",
			}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) validPlanOwner(ctx context.Context, projectID pm.ProjectID, ref pm.IdentityRef) (bool, error) {
	if ref == "" {
		return false, nil
	}
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		return false, err
	}
	if s.authorizer == nil {
		if _, err := s.members.FindByProjectAndIdentity(ctx, projectID, ref); err != nil {
			if errors.Is(err, pm.ErrMemberNotFound) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	ok, err := s.joinedOrgMember(ctx, p.OrganizationID(), ref)
	if err != nil || !ok {
		return ok, err
	}
	exp, err := s.authorizer.ResolveEffective(ctx, authz.CheckRequest{
		SubjectRef: authz.SubjectRef(ref),
		Transport:  authz.TransportSystem,
		Permission: "project.write",
		Resource: authz.ResourceScope{
			Kind:  "project",
			ID:    string(projectID),
			OrgID: p.OrganizationID(),
		},
		RequestID: "pm.plan.owner.reconcile",
	})
	if err != nil && !errors.Is(err, authz.ErrDenied) {
		return false, err
	}
	return exp.Decision.Allowed, nil
}

func (s *Service) joinedOrgMember(ctx context.Context, orgID string, ref pm.IdentityRef) (bool, error) {
	if s.db == nil {
		return true, nil
	}
	exec, err := persistence.ExecutorFromCtx(ctx, s.db)
	if err != nil {
		return false, err
	}
	subject := authz.SubjectRef(ref)
	if !(subject.IsUser() || subject.IsAgent()) {
		return ref == "system", nil
	}
	id := subject.BareID()
	var count int
	var q string
	if subject.IsUser() {
		q = `SELECT COUNT(*) FROM members WHERE organization_id = ? AND identity_id = ? AND status = 'joined'`
	} else {
		q = `SELECT COUNT(*) FROM members WHERE organization_id = ? AND id = ? AND status = 'joined'`
	}
	if err := exec.QueryRowContext(ctx, q, orgID, id).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Service) ReconcilePlanOwnerAccessLoss(ctx context.Context, orgID, subjectRef, reason, actorRef string) error {
	if s == nil || s.plans == nil {
		return ErrPlansUnavailable
	}
	subject := pm.IdentityRef(strings.TrimSpace(subjectRef))
	if err := subject.Validate(); err != nil {
		return err
	}
	actor := pm.IdentityRef(strings.TrimSpace(actorRef))
	if actor == "" {
		actor = pm.SystemActor("owner-access-loss")
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "owner_access_lost"
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		projects, err := s.projects.ListByOrg(txCtx, strings.TrimSpace(orgID))
		if err != nil {
			return err
		}
		for _, project := range projects {
			plans, err := s.plans.ListByProject(txCtx, project.ID())
			if err != nil {
				return err
			}
			for _, p := range plans {
				if p.OwnerRef() != subject {
					continue
				}
				if err := s.reconcilePlanOwner(txCtx, p, actor, reason, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Service) ReconcilePlanOwnersAccessLoss(ctx context.Context, orgID, reason, actorRef string) error {
	if s == nil || s.plans == nil {
		return ErrPlansUnavailable
	}
	actor := pm.IdentityRef(strings.TrimSpace(actorRef))
	if actor == "" {
		actor = pm.SystemActor("owner-access-loss")
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "owner_permission_loss"
	}
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		projects, err := s.projects.ListByOrg(txCtx, strings.TrimSpace(orgID))
		if err != nil {
			return err
		}
		for _, project := range projects {
			plans, err := s.plans.ListByProject(txCtx, project.ID())
			if err != nil {
				return err
			}
			for _, p := range plans {
				ok, err := s.validPlanOwner(txCtx, p.ProjectID(), p.OwnerRef())
				if err != nil {
					return err
				}
				if ok {
					continue
				}
				if err := s.reconcilePlanOwner(txCtx, p, actor, reason, now); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (s *Service) resolvePlanBlockForTask(txCtx context.Context, t *pm.Task, actor pm.IdentityRef, kind, note string, at time.Time) error {
	if s.plans == nil || t == nil || t.PlanID() == "" {
		return nil
	}
	p, err := s.plans.FindByID(txCtx, t.PlanID())
	if err != nil || p.ActiveGenerationID() == "" {
		return err
	}
	events, err := s.plans.ListPlanBlockEvents(txCtx, p.ID(), true)
	if err != nil {
		return err
	}
	for _, ev := range events {
		if ev.TaskID == t.ID() && ev.GenerationID == p.ActiveGenerationID() {
			if err := s.plans.ResolvePlanBlockEvent(txCtx, ev.EventID, actor, kind, note, at); err != nil {
				return err
			}
		}
	}
	remaining, err := s.plans.ListPlanBlockEvents(txCtx, p.ID(), true)
	if err != nil {
		return err
	}
	if len(remaining) == 0 {
		p.ClearAttention(at)
		if err := s.plans.Update(txCtx, p); err != nil {
			return err
		}
	}
	return nil
}
