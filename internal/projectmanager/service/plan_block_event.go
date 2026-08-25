package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

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
	msgID, notifyState := s.notifyPlanOwnerOfBlock(txCtx, p, ev, t)
	if notifyState != ev.NotificationState {
		_ = s.plans.UpdatePlanBlockEventNotification(txCtx, ev.EventID, notifyState, now)
	}
	policy := p.RecoveryPolicy()
	if err := s.emit(txCtx, EvtPlanBlockOwnerWake,
		refsJSON(map[string]string{"plan_id": string(p.ID()), "task_id": string(t.ID()), "event_id": string(ev.EventID)}),
		planBlockOwnerWakePayload{
			EventID: string(ev.EventID), IdempotencyKey: ev.IdempotencyKey, PlanID: string(p.ID()), ProjectID: string(p.ProjectID()),
			GenerationID: string(p.ActiveGenerationID()), TaskID: string(t.ID()), OwnerRef: string(p.OwnerRef()),
			ConversationID: p.ConversationID(), MessageID: msgID, Reason: reason, ReasonType: string(reasonType),
			RemindAfterSeconds: policy.RemindAfterSeconds, EscalateAfterSeconds: policy.EscalateAfterSeconds,
		}); err != nil {
		return err
	}
	s.auditPlan(txCtx, p, pm.AuditPlanBlockCreated, actor, map[string]any{
		"event_id": string(ev.EventID), "task_id": string(t.ID()), "generation_id": string(p.ActiveGenerationID()),
		"reason_type": string(reasonType), "reason": reason, "notification_state": string(notifyState),
	})
	return nil
}

func (s *Service) notifyPlanOwnerOfBlock(ctx context.Context, p *pm.Plan, ev *pm.PlanBlockEvent, t *pm.Task) (string, pm.PlanBlockNotificationState) {
	if s.planDispatcher == nil || strings.TrimSpace(p.ConversationID()) == "" {
		return "", pm.PlanBlockNotifyFailed
	}
	content := fmt.Sprintf("plan attention required: task %s is blocked [%s]: %s", taskRefToken(t), ev.ReasonType, ev.BlockedReason)
	msgID, err := s.planDispatcher.PostMention(ctx, p.ConversationID(), string(p.OwnerRef()), content)
	if err != nil {
		return "", pm.PlanBlockNotifyFailed
	}
	return msgID, pm.PlanBlockNotifySent
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
