package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

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
		if err := s.requireProjectMember(txCtx, p.ProjectID(), cmd.Actor); err != nil {
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
		if err := s.requireProjectMember(txCtx, p.ProjectID(), cmd.Actor); err != nil {
			return err
		}
		if err := s.plans.ResolvePlanBlockEvent(txCtx, cmd.EventID, cmd.Actor, strings.TrimSpace(cmd.ResolutionKind), strings.TrimSpace(cmd.Note), now); err != nil {
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
