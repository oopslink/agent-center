package orchestration

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
)

const (
	WatchdogSeverityInfo     = "info"
	WatchdogSeverityWarning  = "warning"
	WatchdogSeverityCritical = "critical"

	WakeSeverityP0 = "P0"
	WakeSeverityP1 = "P1"
	WakeSeverityP2 = "P2"
	WakeSeverityP3 = "P3"
)

var (
	ErrLeaseBusy       = errors.New("orchestration: plan controller lease held by another owner")
	ErrLeaseNotFound   = errors.New("orchestration: plan controller lease not found")
	ErrLeaseExpired    = errors.New("orchestration: plan controller lease expired")
	ErrStaleFence      = errors.New("orchestration: stale fencing token")
	ErrNodeCASConflict = errors.New("orchestration: node version CAS conflict")
)

type PlanControllerLease struct {
	PlanID          string
	OwnerInstanceID string
	FencingToken    int64
	ExpiresAt       time.Time
	LastRenewedAt   time.Time
	UpdatedAt       time.Time
}

type AcquireLeaseCommand struct {
	PlanID          string
	OwnerInstanceID string
	TTL             time.Duration
}

type PlanControllerToken struct {
	PlanID          string
	OwnerInstanceID string
	FencingToken    int64
}

type FencedOutboxCommand struct {
	Token     PlanControllerToken
	EventID   string
	EventType string
	Refs      map[string]any
	Payload   map[string]any
}

type WatchdogConfig struct {
	LeaseRotationAfter time.Duration
	WatermarkLagAfter  time.Duration
	Watermarks         map[string]time.Time
}

type WatchdogObservation struct {
	ID         string
	Kind       string
	Severity   string
	PlanID     string
	Detail     map[string]any
	ObservedAt time.Time
}

type WakeRequest struct {
	ID         string
	IncidentID string
	OrgID      string
	Severity   string
	Channel    string
	PlanID     string
	Payload    map[string]any
	CreatedAt  time.Time
}

type WakeBucketKey struct {
	OrgID    string
	Severity string
	Channel  string
}

type WakeBucketCapacity struct {
	Capacity   int
	P0Reserved int
}

type WakeDelivery struct {
	ID         string
	IncidentID string
	OrgID      string
	Severity   string
	Channel    string
	PlanID     string
	Payload    map[string]any
}

type WakeOverflow struct {
	OrgID         string
	Channel       string
	Count         int
	MaxSeverity   string
	OldestAt      time.Time
	AffectedPlans []string
	UpdatedAt     time.Time
}

type wakeRow struct {
	WakeDelivery
	createdAt time.Time
}

func (s *Service) AcquirePlanLease(ctx context.Context, cmd AcquireLeaseCommand) (PlanControllerLease, error) {
	if strings.TrimSpace(cmd.PlanID) == "" || strings.TrimSpace(cmd.OwnerInstanceID) == "" || cmd.TTL <= 0 {
		return PlanControllerLease{}, ErrMissingRequiredField
	}
	var out PlanControllerLease
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		now := s.clock.Now().UTC()
		expires := now.Add(cmd.TTL).UTC()
		exec, err := persistence.ExecutorFromCtx(txCtx, s.db)
		if err != nil {
			return err
		}
		cur, err := findLease(txCtx, exec, cmd.PlanID)
		if err != nil {
			if !errors.Is(err, ErrLeaseNotFound) {
				return err
			}
			_, err := exec.ExecContext(txCtx, `INSERT INTO pm_plan_controller_leases
				(plan_id, owner_instance_id, fencing_token, expires_at, last_renewed_at, updated_at)
				VALUES (?,?,?,?,?,?)`,
				cmd.PlanID, cmd.OwnerInstanceID, 1, tsUTC(expires), tsUTC(now), tsUTC(now))
			if err != nil {
				return err
			}
			out = PlanControllerLease{PlanID: cmd.PlanID, OwnerInstanceID: cmd.OwnerInstanceID, FencingToken: 1, ExpiresAt: expires, LastRenewedAt: now, UpdatedAt: now}
			return nil
		}
		if cur.OwnerInstanceID != cmd.OwnerInstanceID && cur.ExpiresAt.After(now) {
			return ErrLeaseBusy
		}
		next := cur.FencingToken + 1
		res, err := exec.ExecContext(txCtx, `UPDATE pm_plan_controller_leases
			SET owner_instance_id=?, fencing_token=?, expires_at=?, last_renewed_at=?, updated_at=?
			WHERE plan_id=? AND owner_instance_id=? AND fencing_token=?`,
			cmd.OwnerInstanceID, next, tsUTC(expires), tsUTC(now), tsUTC(now),
			cmd.PlanID, cur.OwnerInstanceID, cur.FencingToken)
		if err != nil {
			return err
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			return ErrStaleFence
		}
		out = PlanControllerLease{PlanID: cmd.PlanID, OwnerInstanceID: cmd.OwnerInstanceID, FencingToken: next, ExpiresAt: expires, LastRenewedAt: now, UpdatedAt: now}
		return nil
	})
	return out, err
}

func (s *Service) RenewPlanLease(ctx context.Context, token PlanControllerToken, ttl time.Duration) (PlanControllerLease, error) {
	if ttl <= 0 {
		return PlanControllerLease{}, ErrMissingRequiredField
	}
	var out PlanControllerLease
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		now := s.clock.Now().UTC()
		expires := now.Add(ttl).UTC()
		exec, err := persistence.ExecutorFromCtx(txCtx, s.db)
		if err != nil {
			return err
		}
		res, err := exec.ExecContext(txCtx, `UPDATE pm_plan_controller_leases
			SET expires_at=?, last_renewed_at=?, updated_at=?
			WHERE plan_id=? AND owner_instance_id=? AND fencing_token=? AND expires_at > ?`,
			tsUTC(expires), tsUTC(now), tsUTC(now), token.PlanID, token.OwnerInstanceID, token.FencingToken, tsUTC(now))
		if err != nil {
			return err
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			return ErrStaleFence
		}
		out = PlanControllerLease{PlanID: token.PlanID, OwnerInstanceID: token.OwnerInstanceID, FencingToken: token.FencingToken, ExpiresAt: expires, LastRenewedAt: now, UpdatedAt: now}
		return nil
	})
	return out, err
}

func (s *Service) FencedUpdateNode(ctx context.Context, token PlanControllerToken, nodeID NodeID, mutate func(*Node) error) error {
	if mutate == nil {
		return ErrMissingRequiredField
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.validateFence(txCtx, token); err != nil {
			return err
		}
		n, err := s.nodes.FindByID(txCtx, nodeID)
		if err != nil {
			return err
		}
		planID, err := s.planIDForGraph(txCtx, n.GraphID())
		if err != nil {
			return err
		}
		if planID != token.PlanID {
			return ErrStaleFence
		}
		before := n.Version()
		if err := mutate(n); err != nil {
			return err
		}
		exec, err := persistence.ExecutorFromCtx(txCtx, s.db)
		if err != nil {
			return err
		}
		res, err := exec.ExecContext(txCtx, `UPDATE pm_graph_nodes
			SET title=?, status=?, outcome=?, metadata=?, action_logs=?, updated_at=?, version=?
			WHERE id=? AND version=?`,
			n.Title(), string(n.Status()), n.Outcome(), n.MetadataJSON(), n.ActionLogsJSON(), tsUTC(n.UpdatedAt()), n.Version(), string(n.ID()), before)
		if err != nil {
			return err
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			return ErrNodeCASConflict
		}
		return nil
	})
}

func (s *Service) FencedAppendOutbox(ctx context.Context, cmd FencedOutboxCommand) error {
	if strings.TrimSpace(cmd.EventID) == "" || strings.TrimSpace(cmd.EventType) == "" {
		return ErrMissingRequiredField
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.validateFence(txCtx, cmd.Token); err != nil {
			return err
		}
		refs := copyMap(cmd.Refs)
		if refs == nil {
			refs = map[string]any{}
		}
		refs["plan_id"] = cmd.Token.PlanID
		refs["fencing_token"] = cmd.Token.FencingToken
		e := outbox.Event{
			ID:        cmd.EventID,
			EventType: cmd.EventType,
			Refs:      mustJSON(refs),
			Payload:   mustJSON(cmd.Payload),
			CreatedAt: s.clock.Now().UTC(),
		}
		return s.outboxAppend(txCtx, e)
	})
}

func (s *Service) ProcessControllerInboxOnce(ctx context.Context, token PlanControllerToken, eventID string, fn func(context.Context) error) (bool, error) {
	if strings.TrimSpace(eventID) == "" || fn == nil {
		return false, ErrMissingRequiredField
	}
	processed := false
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		if err := s.validateFence(txCtx, token); err != nil {
			return err
		}
		exec, err := persistence.ExecutorFromCtx(txCtx, s.db)
		if err != nil {
			return err
		}
		res, err := exec.ExecContext(txCtx, `INSERT OR IGNORE INTO pm_plan_controller_inbox
			(event_id, plan_id, fencing_token, processed_at) VALUES (?,?,?,?)`,
			eventID, token.PlanID, token.FencingToken, tsUTC(s.clock.Now()))
		if err != nil {
			return err
		}
		if rows, _ := res.RowsAffected(); rows == 0 {
			return nil
		}
		if err := fn(txCtx); err != nil {
			return err
		}
		processed = true
		return nil
	})
	return processed, err
}

func (s *Service) RunPlanControllerWatchdog(ctx context.Context, cfg WatchdogConfig) ([]WatchdogObservation, error) {
	now := s.clock.Now().UTC()
	var observations []WatchdogObservation
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		exec, err := persistence.ExecutorFromCtx(txCtx, s.db)
		if err != nil {
			return err
		}
		rows, err := exec.QueryContext(txCtx, `SELECT g.plan_id, l.owner_instance_id, l.fencing_token, l.expires_at, l.last_renewed_at
			FROM pm_graphs g LEFT JOIN pm_plan_controller_leases l ON l.plan_id = g.plan_id
			WHERE g.status = ?`, string(GraphRunning))
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var planID string
			var owner sql.NullString
			var token sql.NullInt64
			var expires, renewed sql.NullString
			if err := rows.Scan(&planID, &owner, &token, &expires, &renewed); err != nil {
				return err
			}
			if !owner.Valid || !expires.Valid || !parseTS(expires.String).After(now) {
				observations = append(observations, WatchdogObservation{
					ID: s.idgen.NewULID(), Kind: "shard_without_owner", Severity: WatchdogSeverityCritical,
					PlanID: planID, Detail: map[string]any{"owner_instance_id": "", "expired_at": expires.String}, ObservedAt: now,
				})
				continue
			}
			if cfg.LeaseRotationAfter > 0 && now.Sub(parseTS(renewed.String)) > cfg.LeaseRotationAfter {
				observations = append(observations, WatchdogObservation{
					ID: s.idgen.NewULID(), Kind: "non_rotating_lease", Severity: WatchdogSeverityWarning,
					PlanID: planID, Detail: map[string]any{"owner_instance_id": owner.String, "fencing_token": token.Int64}, ObservedAt: now,
				})
			}
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if cfg.WatermarkLagAfter > 0 {
			for name, watermark := range cfg.Watermarks {
				if now.Sub(watermark.UTC()) > cfg.WatermarkLagAfter {
					observations = append(observations, WatchdogObservation{
						ID: s.idgen.NewULID(), Kind: "reconcile_watermark_lag", Severity: WatchdogSeverityWarning,
						Detail: map[string]any{"watermark": name, "lag_ms": now.Sub(watermark.UTC()).Milliseconds()}, ObservedAt: now,
					})
				}
			}
		}
		for _, ob := range observations {
			if _, err := exec.ExecContext(txCtx, `INSERT INTO pm_watchdog_observations
				(id, kind, severity, plan_id, detail, observed_at) VALUES (?,?,?,?,?,?)`,
				ob.ID, ob.Kind, ob.Severity, ob.PlanID, mustJSON(ob.Detail), tsUTC(ob.ObservedAt)); err != nil {
				return err
			}
		}
		return nil
	})
	return observations, err
}

func (s *Service) EnqueueWake(ctx context.Context, req WakeRequest) error {
	if strings.TrimSpace(req.IncidentID) == "" || strings.TrimSpace(req.OrgID) == "" || strings.TrimSpace(req.Channel) == "" {
		return ErrMissingRequiredField
	}
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		exec, err := persistence.ExecutorFromCtx(txCtx, s.db)
		if err != nil {
			return err
		}
		id := req.ID
		if id == "" {
			id = s.idgen.NewULID()
		}
		at := req.CreatedAt.UTC()
		if at.IsZero() {
			at = s.clock.Now().UTC()
		}
		_, err = exec.ExecContext(txCtx, `INSERT OR IGNORE INTO pm_wake_queue
			(id, incident_id, org_id, severity, channel, plan_id, payload, status, created_at)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			id, req.IncidentID, req.OrgID, normalizeSeverity(req.Severity), req.Channel, req.PlanID, mustJSON(req.Payload), "pending", tsUTC(at))
		return err
	})
}

func (s *Service) DrainWakeTokens(ctx context.Context, caps map[WakeBucketKey]WakeBucketCapacity) ([]WakeDelivery, []WakeOverflow, error) {
	now := s.clock.Now().UTC()
	var delivered []WakeDelivery
	var overflows []WakeOverflow
	err := persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		exec, err := persistence.ExecutorFromCtx(txCtx, s.db)
		if err != nil {
			return err
		}
		rows, err := exec.QueryContext(txCtx, `SELECT id, incident_id, org_id, severity, channel, plan_id, payload, created_at
			FROM pm_wake_queue WHERE status = 'pending'
			ORDER BY org_id, channel, CASE severity WHEN 'P0' THEN 0 WHEN 'P1' THEN 1 WHEN 'P2' THEN 2 ELSE 3 END, created_at, id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		var pending []wakeRow
		for rows.Next() {
			var r wakeRow
			var payload, created string
			if err := rows.Scan(&r.ID, &r.IncidentID, &r.OrgID, &r.Severity, &r.Channel, &r.PlanID, &payload, &created); err != nil {
				return err
			}
			r.Payload = unmarshalMap(payload)
			r.createdAt = parseTS(created)
			pending = append(pending, r)
		}
		if err := rows.Err(); err != nil {
			return err
		}
		usedTotal := map[string]int{}
		usedNonP0 := map[string]int{}
		overflowByBucket := map[string][]wakeRow{}
		for _, r := range pending {
			key := WakeBucketKey{OrgID: r.OrgID, Severity: r.Severity, Channel: r.Channel}
			base := r.OrgID + "\x00" + r.Channel
			capacity := caps[key]
			if capacity.Capacity <= 0 {
				capacity = caps[WakeBucketKey{OrgID: r.OrgID, Severity: "*", Channel: r.Channel}]
			}
			if capacity.Capacity <= 0 {
				overflowByBucket[base] = append(overflowByBucket[base], r)
				continue
			}
			canSend := usedTotal[base] < capacity.Capacity
			if r.Severity != WakeSeverityP0 {
				nonP0Limit := capacity.Capacity - capacity.P0Reserved
				if nonP0Limit < 0 {
					nonP0Limit = 0
				}
				canSend = canSend && usedNonP0[base] < nonP0Limit
			}
			if !canSend {
				overflowByBucket[base] = append(overflowByBucket[base], r)
				continue
			}
			if _, err := exec.ExecContext(txCtx, `UPDATE pm_wake_queue SET status='delivered', delivered_at=? WHERE id=? AND status='pending'`, tsUTC(now), r.ID); err != nil {
				return err
			}
			usedTotal[base]++
			if r.Severity != WakeSeverityP0 {
				usedNonP0[base]++
			}
			delivered = append(delivered, r.WakeDelivery)
		}
		for _, rows := range overflowByBucket {
			ob, err := s.markWakeOverflow(txCtx, exec, rows, now)
			if err != nil {
				return err
			}
			overflows = append(overflows, ob)
		}
		return nil
	})
	return delivered, overflows, err
}

func (s *Service) ResumeWakeOverflow(ctx context.Context, orgID, channel string) error {
	return persistence.RunInTx(ctx, s.db, func(txCtx context.Context) error {
		exec, err := persistence.ExecutorFromCtx(txCtx, s.db)
		if err != nil {
			return err
		}
		if _, err := exec.ExecContext(txCtx, `UPDATE pm_wake_queue SET status='pending', overflowed_at=NULL
			WHERE org_id=? AND channel=? AND status='overflow'`, orgID, channel); err != nil {
			return err
		}
		_, err = exec.ExecContext(txCtx, `DELETE FROM pm_wake_overflows WHERE org_id=? AND channel=?`, orgID, channel)
		return err
	})
}

func (s *Service) validateFence(ctx context.Context, token PlanControllerToken) error {
	exec, err := persistence.ExecutorFromCtx(ctx, s.db)
	if err != nil {
		return err
	}
	lease, err := findLease(ctx, exec, token.PlanID)
	if err != nil {
		return err
	}
	if lease.OwnerInstanceID != token.OwnerInstanceID || lease.FencingToken != token.FencingToken {
		return ErrStaleFence
	}
	if !lease.ExpiresAt.After(s.clock.Now().UTC()) {
		return ErrLeaseExpired
	}
	return nil
}

func (s *Service) planIDForGraph(ctx context.Context, graphID GraphID) (string, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, s.db)
	if err != nil {
		return "", err
	}
	var planID string
	if err := exec.QueryRowContext(ctx, `SELECT plan_id FROM pm_graphs WHERE id=?`, string(graphID)).Scan(&planID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", ErrGraphNotFound
		}
		return "", err
	}
	return planID, nil
}

func (s *Service) outboxAppend(ctx context.Context, e outbox.Event) error {
	exec, err := persistence.ExecutorFromCtx(ctx, s.db)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT OR IGNORE INTO outbox_events
		(id, event_type, refs, payload, created_at, processed_at) VALUES (?,?,?,?,?,NULL)`,
		e.ID, string(e.EventType), e.Refs, e.Payload, tsUTC(e.CreatedAt))
	return err
}

func (s *Service) markWakeOverflow(ctx context.Context, exec persistence.SQLExecutor, rows []wakeRow, now time.Time) (WakeOverflow, error) {
	plans := map[string]struct{}{}
	oldest := rows[0].createdAt
	maxSeverity := rows[0].Severity
	for _, r := range rows {
		if r.PlanID != "" {
			plans[r.PlanID] = struct{}{}
		}
		if r.createdAt.Before(oldest) {
			oldest = r.createdAt
		}
		if severityRank(r.Severity) < severityRank(maxSeverity) {
			maxSeverity = r.Severity
		}
		if _, err := exec.ExecContext(ctx, `UPDATE pm_wake_queue SET status='overflow', overflowed_at=? WHERE id=? AND status='pending'`, tsUTC(now), r.ID); err != nil {
			return WakeOverflow{}, err
		}
	}
	affected := make([]string, 0, len(plans))
	for p := range plans {
		affected = append(affected, p)
	}
	sort.Strings(affected)
	ob := WakeOverflow{
		OrgID: rows[0].OrgID, Channel: rows[0].Channel, Count: len(rows),
		MaxSeverity: maxSeverity, OldestAt: oldest, AffectedPlans: affected, UpdatedAt: now,
	}
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_wake_overflows
		(org_id, channel, count, max_severity, oldest_at, affected_plans, updated_at)
		VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(org_id, channel) DO UPDATE SET
			count=excluded.count, max_severity=excluded.max_severity, oldest_at=excluded.oldest_at,
			affected_plans=excluded.affected_plans, updated_at=excluded.updated_at`,
		ob.OrgID, ob.Channel, ob.Count, ob.MaxSeverity, tsUTC(ob.OldestAt), mustJSON(ob.AffectedPlans), tsUTC(ob.UpdatedAt))
	return ob, err
}

func findLease(ctx context.Context, exec persistence.SQLExecutor, planID string) (PlanControllerLease, error) {
	var l PlanControllerLease
	var expires, renewed, updated string
	err := exec.QueryRowContext(ctx, `SELECT plan_id, owner_instance_id, fencing_token, expires_at, last_renewed_at, updated_at
		FROM pm_plan_controller_leases WHERE plan_id=?`, planID).
		Scan(&l.PlanID, &l.OwnerInstanceID, &l.FencingToken, &expires, &renewed, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return PlanControllerLease{}, ErrLeaseNotFound
	}
	if err != nil {
		return PlanControllerLease{}, err
	}
	l.ExpiresAt = parseTS(expires)
	l.LastRenewedAt = parseTS(renewed)
	l.UpdatedAt = parseTS(updated)
	return l, nil
}

func normalizeSeverity(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	switch s {
	case WakeSeverityP0, WakeSeverityP1, WakeSeverityP2, WakeSeverityP3:
		return s
	default:
		return WakeSeverityP3
	}
}

func severityRank(s string) int {
	switch normalizeSeverity(s) {
	case WakeSeverityP0:
		return 0
	case WakeSeverityP1:
		return 1
	case WakeSeverityP2:
		return 2
	default:
		return 3
	}
}

func copyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func mustJSON(v any) string {
	if v == nil {
		return "{}"
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("orchestration: json marshal failed: %v", err))
	}
	return string(b)
}

func unmarshalMap(s string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(s), &out); err != nil || out == nil {
		return map[string]any{}
	}
	return out
}

func tsUTC(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTS(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t.UTC()
}
