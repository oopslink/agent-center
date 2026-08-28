package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

type ProgressControlRepo struct{ db *sql.DB }

func NewProgressControlRepo(db *sql.DB) *ProgressControlRepo { return &ProgressControlRepo{db: db} }

func encodeStrings(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func decodeStrings(s string) []string {
	var out []string
	if s == "" {
		return nil
	}
	_ = json.Unmarshal([]byte(s), &out)
	return out
}

func ms(d time.Duration) int64 {
	if d <= 0 {
		return 0
	}
	return d.Milliseconds()
}

func dur(ms int64) time.Duration {
	if ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func (r *ProgressControlRepo) RecordWake(ctx context.Context, w pm.ProgressWake) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_progress_wakes
		(id, plan_id, task_id, node_id, owner_ref, owner_display, reason, status, idempotency_key, requested_at, delivered_at, acknowledged_at, ack_fact_ref, ack_deadline, max_hold_duration_ms, escalation_level, next_escalation_at, organization_owner_ref)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		w.ID, string(w.PlanID), string(w.TaskID), w.NodeID, string(w.OwnerRef), w.OwnerDisplay, w.Reason, w.Status, w.IdempotencyKey,
		ts(w.RequestedAt), tsPtr(&w.DeliveredAt), tsPtr(&w.AcknowledgedAt), w.AckFactRef, ts(w.AckDeadline), ms(w.MaxHoldDuration), w.EscalationLevel,
		tsPtr(&w.NextEscalationAt), w.OrganizationOwnerRef)
	if isUnique(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *ProgressControlRepo) MarkWakeDelivered(ctx context.Context, wakeID string, at time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `UPDATE pm_progress_wakes SET status=?, delivered_at=? WHERE id=? AND delivered_at=''`,
		pm.ProgressWakeDelivered, ts(at), wakeID)
	return err
}

func (r *ProgressControlRepo) AcknowledgeWake(ctx context.Context, wakeID string, actor pm.IdentityRef, at time.Time, factRef string) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `UPDATE pm_progress_wakes SET status=?, acknowledged_at=?, ack_fact_ref=? WHERE id=? AND owner_ref=? AND acknowledged_at=''`,
		pm.ProgressWakeAcknowledged, ts(at), factRef, wakeID, string(actor))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil || n != 1 {
		return false, err
	}
	w, ok, err := r.findWake(ctx, wakeID)
	if err != nil || !ok {
		return false, err
	}
	if _, err := r.ReleaseHoldsByReason(ctx, string(pm.ProgressObligationAckWake), "obl:"+wakeID, actor, factRef, at); err != nil {
		return false, err
	}
	if _, err := r.ResolveOpenObligationsByFact(ctx, w.PlanID, w.TaskID, actor, factRef, at); err != nil {
		return false, err
	}
	if _, err := r.ResolveOpenIncidentsBySource(ctx, w.PlanID, w.TaskID, wakeID, factRef, at); err != nil {
		return false, err
	}
	return true, nil
}

func (r *ProgressControlRepo) findWake(ctx context.Context, wakeID string) (pm.ProgressWake, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, `SELECT id, plan_id, task_id, node_id, owner_ref, owner_display, reason, status, idempotency_key, requested_at, delivered_at, acknowledged_at, ack_fact_ref, ack_deadline, max_hold_duration_ms, escalation_level, next_escalation_at, organization_owner_ref
		FROM pm_progress_wakes WHERE id=?`, wakeID)
	w, err := scanWake(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return pm.ProgressWake{}, false, nil
	}
	return w, err == nil, err
}

func (r *ProgressControlRepo) ListExpiredUnackedWakes(ctx context.Context, now time.Time, limit int) ([]pm.ProgressWake, error) {
	if limit <= 0 {
		limit = 100
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, `SELECT id, plan_id, task_id, node_id, owner_ref, owner_display, reason, status, idempotency_key, requested_at, delivered_at, acknowledged_at, ack_fact_ref, ack_deadline, max_hold_duration_ms, escalation_level, next_escalation_at, organization_owner_ref
		FROM pm_progress_wakes WHERE acknowledged_at='' AND ack_deadline <= ? ORDER BY ack_deadline, id LIMIT ?`, ts(now), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.ProgressWake
	for rows.Next() {
		w, err := scanWake(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func scanWake(scan func(...any) error) (pm.ProgressWake, error) {
	var w pm.ProgressWake
	var planID, taskID, owner, requested, delivered, acked, ackDeadline, next string
	var maxMS int64
	if err := scan(&w.ID, &planID, &taskID, &w.NodeID, &owner, &w.OwnerDisplay, &w.Reason, &w.Status, &w.IdempotencyKey,
		&requested, &delivered, &acked, &w.AckFactRef, &ackDeadline, &maxMS, &w.EscalationLevel, &next, &w.OrganizationOwnerRef); err != nil {
		return w, err
	}
	w.PlanID, w.TaskID, w.OwnerRef = pm.PlanID(planID), pm.TaskID(taskID), pm.IdentityRef(owner)
	w.RequestedAt, w.AckDeadline, w.MaxHoldDuration = parseTime(requested), parseTime(ackDeadline), dur(maxMS)
	if t := parseTimePtr(delivered); t != nil {
		w.DeliveredAt = *t
	}
	if t := parseTimePtr(acked); t != nil {
		w.AcknowledgedAt = *t
	}
	if t := parseTimePtr(next); t != nil {
		w.NextEscalationAt = *t
	}
	return w, nil
}

func (r *ProgressControlRepo) UpsertObligation(ctx context.Context, o pm.ProgressObligation) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_progress_control_obligations
		(id, plan_id, task_id, node_id, kind, owner_ref, owner_display, deadline_at, ack_required, acked_at, escalate_to_ref, escalation_deadline_at, source_fact_refs, status, created_at, updated_at, version)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT DO UPDATE SET updated_at=excluded.updated_at, status=pm_progress_control_obligations.status,
		 deadline_at=excluded.deadline_at, source_fact_refs=excluded.source_fact_refs`,
		o.ID, string(o.PlanID), string(o.TaskID), o.NodeID, o.Kind, string(o.OwnerRef), o.OwnerDisplay, ts(o.DeadlineAt), boolToInt(o.AckRequired),
		tsPtr(o.AckedAt), o.EscalateToRef, ts(o.EscalationDeadlineAt), encodeStrings(o.SourceFactRefs), o.Status, ts(o.CreatedAt), ts(o.UpdatedAt), o.Version)
	return err == nil, err
}

func (r *ProgressControlRepo) UpsertIncident(ctx context.Context, i pm.ProgressIncident) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_progress_control_incidents
		(id, plan_id, task_id, node_id, kind, severity, owner_ref, owner_display, summary, source_ref, status, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(plan_id, task_id, kind, source_ref) DO UPDATE SET updated_at=excluded.updated_at, severity=excluded.severity`,
		i.ID, string(i.PlanID), string(i.TaskID), i.NodeID, i.Kind, i.Severity, i.OwnerRef, i.OwnerDisplay, i.Summary, i.SourceRef, i.Status, ts(i.CreatedAt), ts(i.UpdatedAt))
	return err == nil, err
}

func (r *ProgressControlRepo) UpsertHold(ctx context.Context, h pm.ProgressHold) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_progress_holds
		(id, plan_id, task_id, node_id, reason_kind, reason_id, owner_ref, owner_display, entered_at, hold_ack_deadline, max_hold_duration_ms, escalation_level, next_escalation_at, blocks_dispatch, blocks_acceptance, blocks_completion, released_at, release_fact_ref)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(plan_id, task_id, reason_kind, reason_id) DO UPDATE SET next_escalation_at=excluded.next_escalation_at, escalation_level=excluded.escalation_level`,
		h.ID, string(h.PlanID), string(h.TaskID), h.NodeID, h.ReasonKind, h.ReasonID, h.OwnerRef, h.OwnerDisplay, ts(h.EnteredAt), ts(h.HoldAckDeadline),
		ms(h.MaxHoldDuration), h.EscalationLevel, ts(h.NextEscalationAt), boolToInt(h.BlocksDispatch), boolToInt(h.BlocksAcceptance), boolToInt(h.BlocksCompletion),
		tsPtr(&h.ReleasedAt), h.ReleaseFactRef)
	return err == nil, err
}

func (r *ProgressControlRepo) ListOpenHoldsByPlan(ctx context.Context, planID pm.PlanID) ([]pm.ProgressHold, error) {
	return r.listHolds(ctx, `WHERE plan_id=? AND released_at='' ORDER BY entered_at, id`, string(planID))
}

func (r *ProgressControlRepo) ListOpenHoldsByTask(ctx context.Context, taskID pm.TaskID) ([]pm.ProgressHold, error) {
	return r.listHolds(ctx, `WHERE task_id=? AND released_at='' ORDER BY entered_at, id`, string(taskID))
}

func (r *ProgressControlRepo) ListDueHolds(ctx context.Context, now time.Time, limit int) ([]pm.ProgressHold, error) {
	if limit <= 0 {
		limit = 100
	}
	return r.listHolds(ctx, `WHERE released_at='' AND next_escalation_at!='' AND next_escalation_at <= ? ORDER BY next_escalation_at, id LIMIT ?`, ts(now), limit)
}

func (r *ProgressControlRepo) ListBreachedHolds(ctx context.Context, now time.Time, limit int) ([]pm.ProgressHold, error) {
	if limit <= 0 {
		limit = 100
	}
	return r.listHolds(ctx, `WHERE released_at='' AND max_hold_duration_ms>0 AND (julianday(entered_at) + (max_hold_duration_ms / 86400000.0)) <= julianday(?) ORDER BY entered_at, id LIMIT ?`, ts(now), limit)
}

func (r *ProgressControlRepo) listHolds(ctx context.Context, suffix string, args ...any) ([]pm.ProgressHold, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, `SELECT id, plan_id, task_id, node_id, reason_kind, reason_id, owner_ref, owner_display, entered_at, hold_ack_deadline, max_hold_duration_ms, escalation_level, next_escalation_at, blocks_dispatch, blocks_acceptance, blocks_completion, released_at, release_fact_ref FROM pm_progress_holds `+suffix, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.ProgressHold
	for rows.Next() {
		h, err := scanHold(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

func scanHold(scan func(...any) error) (pm.ProgressHold, error) {
	var h pm.ProgressHold
	var planID, taskID, entered, ack, next, released string
	var maxMS int64
	var bd, ba, bc int
	if err := scan(&h.ID, &planID, &taskID, &h.NodeID, &h.ReasonKind, &h.ReasonID, &h.OwnerRef, &h.OwnerDisplay, &entered, &ack, &maxMS, &h.EscalationLevel, &next, &bd, &ba, &bc, &released, &h.ReleaseFactRef); err != nil {
		return h, err
	}
	h.PlanID, h.TaskID = pm.PlanID(planID), pm.TaskID(taskID)
	h.EnteredAt, h.HoldAckDeadline, h.MaxHoldDuration = parseTime(entered), parseTime(ack), dur(maxMS)
	if t := parseTimePtr(next); t != nil {
		h.NextEscalationAt = *t
	}
	if t := parseTimePtr(released); t != nil {
		h.ReleasedAt = *t
	}
	h.BlocksDispatch, h.BlocksAcceptance, h.BlocksCompletion = bd != 0, ba != 0, bc != 0
	return h, nil
}

func (r *ProgressControlRepo) ReleaseHoldsByFact(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, actor pm.IdentityRef, factRef string, at time.Time) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `UPDATE pm_progress_holds SET released_at=?, release_fact_ref=? WHERE plan_id=? AND (?='' OR task_id=?) AND released_at='' AND owner_ref=?`,
		ts(at), factRef, string(planID), string(taskID), string(taskID), string(actor))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (r *ProgressControlRepo) ResolveOpenObligationsByFact(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, actor pm.IdentityRef, factRef string, at time.Time) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `UPDATE pm_progress_control_obligations
		SET status='resolved', acked_at=?, updated_at=?, source_fact_refs=?
		WHERE plan_id=? AND (?='' OR task_id=?) AND status='open' AND owner_ref=?`,
		ts(at), ts(at), encodeStrings([]string{factRef}), string(planID), string(taskID), string(taskID), string(actor))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (r *ProgressControlRepo) ResolveOpenIncidentsBySource(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, sourceRef string, factRef string, at time.Time) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `UPDATE pm_progress_control_incidents
		SET status='resolved', updated_at=?
		WHERE plan_id=? AND (?='' OR task_id=?) AND status='open' AND source_ref=?`,
		ts(at), string(planID), string(taskID), string(taskID), sourceRef)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	_ = factRef
	return int(n), err
}

func (r *ProgressControlRepo) UpsertPrerequisiteSubscription(ctx context.Context, s pm.ProgressPrerequisiteSubscription) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_progress_prerequisite_subscriptions
		(id, plan_id, decision_task_id, prerequisite_task_id, owner_ref, next_deadline_at, action, reason_fact_ref, status, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(plan_id, decision_task_id, prerequisite_task_id, reason_fact_ref) DO UPDATE SET
		 owner_ref=excluded.owner_ref, next_deadline_at=excluded.next_deadline_at, action=excluded.action`,
		s.ID, string(s.PlanID), string(s.DecisionTaskID), string(s.PrerequisiteTaskID), string(s.OwnerRef), ts(s.NextDeadlineAt), s.Action, s.ReasonFactRef, s.Status, ts(s.CreatedAt))
	return err == nil, err
}

func (r *ProgressControlRepo) ListOpenPrerequisiteSubscriptions(ctx context.Context, now time.Time, limit int) ([]pm.ProgressPrerequisiteSubscription, error) {
	if limit <= 0 {
		limit = 100
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, `SELECT id, plan_id, decision_task_id, prerequisite_task_id, owner_ref,
		next_deadline_at, action, reason_fact_ref, status, created_at, resolved_at, decision_fact_ref
		FROM pm_progress_prerequisite_subscriptions WHERE status='open'
		ORDER BY next_deadline_at, id LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.ProgressPrerequisiteSubscription
	for rows.Next() {
		var s pm.ProgressPrerequisiteSubscription
		var planID, decisionID, prerequisiteID, owner, deadline, created, resolved string
		if err := rows.Scan(&s.ID, &planID, &decisionID, &prerequisiteID, &owner, &deadline, &s.Action, &s.ReasonFactRef, &s.Status, &created, &resolved, &s.DecisionFactRef); err != nil {
			return nil, err
		}
		s.PlanID, s.DecisionTaskID, s.PrerequisiteTaskID, s.OwnerRef = pm.PlanID(planID), pm.TaskID(decisionID), pm.TaskID(prerequisiteID), pm.IdentityRef(owner)
		s.NextDeadlineAt, s.CreatedAt = parseTime(deadline), parseTime(created)
		if t := parseTimePtr(resolved); t != nil {
			s.ResolvedAt = *t
		}
		out = append(out, s)
	}
	_ = now // deadline is surfaced to the cockpit; satisfaction drives evaluation.
	return out, rows.Err()
}

func (r *ProgressControlRepo) ResolvePrerequisiteSubscription(ctx context.Context, id, factRef string, at time.Time) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `UPDATE pm_progress_prerequisite_subscriptions
		SET status='resolved', resolved_at=?, decision_fact_ref=? WHERE id=? AND status='open'`, ts(at), factRef, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (r *ProgressControlRepo) ReleaseHoldsByReason(ctx context.Context, reasonKind, reasonID string, actor pm.IdentityRef, factRef string, at time.Time) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `UPDATE pm_progress_holds SET released_at=?, release_fact_ref=? WHERE reason_kind=? AND reason_id=? AND released_at='' AND owner_ref=?`,
		ts(at), factRef, reasonKind, reasonID, string(actor))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	return int(n), err
}

func (r *ProgressControlRepo) RecordEscalation(ctx context.Context, e pm.ProgressEscalation) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_progress_escalations
		(id, plan_id, task_id, node_id, obligation_id, hold_id, kind, severity, escalate_to_ref, deadline_at, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		e.ID, string(e.PlanID), string(e.TaskID), e.NodeID, e.ObligationID, e.HoldID, e.Kind, e.Severity, e.EscalateToRef, ts(e.DeadlineAt), ts(e.CreatedAt))
	if isUnique(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *ProgressControlRepo) SnapshotPlan(ctx context.Context, planID pm.PlanID, asOf time.Time) (pm.ProgressControlSnapshot, error) {
	holds, err := r.ListOpenHoldsByPlan(ctx, planID)
	if err != nil {
		return pm.ProgressControlSnapshot{}, err
	}
	obligations, err := r.listOpenObligations(ctx, planID)
	if err != nil {
		return pm.ProgressControlSnapshot{}, err
	}
	incidents, err := r.listOpenIncidents(ctx, planID)
	if err != nil {
		return pm.ProgressControlSnapshot{}, err
	}
	s := pm.ProgressControlSnapshot{AsOf: asOf, Decision: pm.ProgressDecisionVerified, OpenHolds: holds, OpenObligations: obligations, OpenIncidents: incidents}
	if len(holds) > 0 || len(obligations) > 0 || len(incidents) > 0 {
		s.Decision = pm.ProgressDecisionBound
	}
	return s, nil
}

func (r *ProgressControlRepo) listOpenObligations(ctx context.Context, planID pm.PlanID) ([]pm.ProgressObligation, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, `SELECT id, task_id, node_id, kind, owner_ref, owner_display, deadline_at, ack_required, acked_at, escalate_to_ref, escalation_deadline_at, source_fact_refs, status, created_at, updated_at, version
		FROM pm_progress_control_obligations WHERE plan_id=? AND status='open' ORDER BY deadline_at, id`, string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.ProgressObligation
	for rows.Next() {
		var o pm.ProgressObligation
		var taskID, owner, deadline, acked, escalationDeadline, refs, created, updated string
		var ackRequired int
		if err := rows.Scan(&o.ID, &taskID, &o.NodeID, &o.Kind, &owner, &o.OwnerDisplay, &deadline, &ackRequired, &acked, &o.EscalateToRef, &escalationDeadline, &refs, &o.Status, &created, &updated, &o.Version); err != nil {
			return nil, err
		}
		o.PlanID = planID
		o.TaskID = pm.TaskID(taskID)
		o.OwnerRef = pm.IdentityRef(owner)
		o.DeadlineAt = parseTime(deadline)
		o.AckRequired = ackRequired != 0
		if t := parseTimePtr(acked); t != nil {
			o.AckedAt = t
		}
		o.EscalationDeadlineAt = parseTime(escalationDeadline)
		o.SourceFactRefs = decodeStrings(refs)
		o.CreatedAt, o.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *ProgressControlRepo) listOpenIncidents(ctx context.Context, planID pm.PlanID) ([]pm.ProgressIncident, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, `SELECT id, task_id, node_id, kind, severity, owner_ref, owner_display, summary, source_ref, status, created_at, updated_at
		FROM pm_progress_control_incidents WHERE plan_id=? AND status='open' ORDER BY created_at, id`, string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.ProgressIncident
	for rows.Next() {
		var i pm.ProgressIncident
		var taskID, created, updated string
		if err := rows.Scan(&i.ID, &taskID, &i.NodeID, &i.Kind, &i.Severity, &i.OwnerRef, &i.OwnerDisplay, &i.Summary, &i.SourceRef, &i.Status, &created, &updated); err != nil {
			return nil, err
		}
		i.PlanID = planID
		i.TaskID = pm.TaskID(taskID)
		i.CreatedAt, i.UpdatedAt = parseTime(created), parseTime(updated)
		out = append(out, i)
	}
	return out, rows.Err()
}

var _ pm.ProgressControlRepository = (*ProgressControlRepo)(nil)

var _ = errors.Is
