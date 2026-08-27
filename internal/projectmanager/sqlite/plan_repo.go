package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// planIDPlaceholders builds the `?,?,...` placeholder list and matching []any
// args for a `WHERE plan_id IN (...)` batch query over the given plan ids.
func planIDPlaceholders(planIDs []pm.PlanID) (string, []any) {
	ph := make([]string, len(planIDs))
	args := make([]any, len(planIDs))
	for i, id := range planIDs {
		ph[i] = "?"
		args[i] = string(id)
	}
	return strings.Join(ph, ","), args
}

// --- PlanRepo ---------------------------------------------------------------

// PlanRepo implements pm.PlanRepository (v2.9 #283): the Plan aggregate plus its
// per-Plan depends_on execution-DAG edges. AddDependency enforces the acyclic +
// no-self-edge invariant before persisting; the DAG is 1:1-scoped to one Plan
// (§9.8). No node_status is read or written — node status is derived (§9.2).
type PlanRepo struct{ db *sql.DB }

// NewPlanRepo constructs the repo.
func NewPlanRepo(db *sql.DB) *PlanRepo { return &PlanRepo{db: db} }

// tsPtr formats an optional timestamp: nil → "" (the schema default for "no
// target date"), else RFC3339Nano (mirrors the task status_changed_at ” convention).
func tsPtr(t *time.Time) string {
	if t == nil || t.IsZero() {
		return ""
	}
	return ts(*t)
}

// parseTimePtr parses an optional stored timestamp: "" → nil, else a *time.Time.
func parseTimePtr(s string) *time.Time {
	if s == "" {
		return nil
	}
	t := parseTime(s)
	if t.IsZero() {
		return nil
	}
	return &t
}

func (r *PlanRepo) Save(ctx context.Context, p *pm.Plan) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_plans (id, project_id, name, description, status, creator_ref, conversation_id, target_date, is_builtin, org_number, created_at, updated_at, version, graph_id, active_generation_id, archived_at, archived_by)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(p.ID()), string(p.ProjectID()), p.Name(), p.Description(),
		string(p.Status()), string(p.CreatorRef()), p.ConversationID(), tsPtr(p.TargetDate()),
		boolToInt(p.IsBuiltin()), p.OrgNumber(),
		ts(p.CreatedAt()), ts(p.UpdatedAt()), p.Version(), p.GraphID(), string(p.ActiveGenerationID()), tsPtr(p.ArchivedAt()), string(p.ArchivedBy()))
	if isUnique(err) {
		return pm.ErrPlanExists
	}
	return err
}

func (r *PlanRepo) Update(ctx context.Context, p *pm.Plan) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_plans SET name=?, description=?, status=?, conversation_id=?, target_date=?, is_builtin=?, updated_at=?, version=?, graph_id=?, active_generation_id=?, archived_at=?, archived_by=? WHERE id=?`,
		p.Name(), p.Description(), string(p.Status()), p.ConversationID(), tsPtr(p.TargetDate()),
		boolToInt(p.IsBuiltin()), ts(p.UpdatedAt()), p.Version(), p.GraphID(), string(p.ActiveGenerationID()), tsPtr(p.ArchivedAt()), string(p.ArchivedBy()), string(p.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrPlanNotFound
	}
	return nil
}

func (r *PlanRepo) FindByID(ctx context.Context, id pm.PlanID) (*pm.Plan, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, planSelect+` WHERE id = ?`, string(id))
	p, err := scanPlan(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrPlanNotFound
	}
	return p, err
}

func (r *PlanRepo) ListByProject(ctx context.Context, projectID pm.ProjectID) ([]*pm.Plan, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, planSelect+` WHERE project_id = ? ORDER BY created_at, id`, string(projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Plan
	for rows.Next() {
		p, err := scanPlan(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListRunningPlans returns every Plan in status `running` across ALL projects
// (global, no project filter), stable-ordered (created_at, id). It backs the
// v2.9 P2-3 reconciliation sweep (the global background safety net).
func (r *PlanRepo) ListRunningPlans(ctx context.Context) ([]*pm.Plan, error) {
	return r.ListPlansByStatus(ctx, pm.PlanRunning)
}

func (r *PlanRepo) ListPlansByStatus(ctx context.Context, status pm.PlanStatus) ([]*pm.Plan, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, planSelect+` WHERE status = ? ORDER BY created_at, id`, string(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Plan
	for rows.Next() {
		p, err := scanPlan(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (r *PlanRepo) Delete(ctx context.Context, id pm.PlanID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `DELETE FROM pm_plans WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrPlanNotFound
	}
	return nil
}

// DeletePlan hard-deletes a Plan + its DAG state (v2.9 P3): it CASCADE-removes
// the plan's depends_on edges and dispatch records, then deletes the pm_plans row
// (all within the caller's tx via ExecutorFromCtx, so the cascade is atomic). The
// caller unloads the plan's tasks back to the backlog beforehand — tasks are NOT
// touched here. ErrPlanNotFound if no plan row existed.
func (r *PlanRepo) DeletePlan(ctx context.Context, id pm.PlanID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	if _, err := exec.ExecContext(ctx, `DELETE FROM pm_task_dependencies WHERE plan_id = ?`, string(id)); err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, `DELETE FROM pm_plan_dispatch_records WHERE plan_id = ?`, string(id)); err != nil {
		return err
	}
	// I103: cascade the plan's旁路 BlockedOn snapshots (observational, no gate reads
	// them — but they must not outlive the plan).
	if _, err := exec.ExecContext(ctx, `DELETE FROM pm_plan_blocked_on WHERE plan_id = ?`, string(id)); err != nil {
		return err
	}
	res, err := exec.ExecContext(ctx, `DELETE FROM pm_plans WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrPlanNotFound
	}
	return nil
}

// AddDependency loads the plan's existing edges, runs WouldCreateCycle (which
// rejects self-edges and cycles), then inserts. The acyclic + no-self-edge
// invariant is enforced HERE before any write (§283 acyclic red-line).
func (r *PlanRepo) AddDependency(ctx context.Context, dep pm.Dependency) error {
	existing, err := r.ListDependencies(ctx, dep.PlanID)
	if err != nil {
		return err
	}
	if err := pm.WouldCreateCycle(existing, dep); err != nil {
		return err
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	// v2.13.0 I18/B1: persist the control-flow kind/when/max_rounds ("when" is a SQL
	// keyword → quoted). kind is normalized so "" stores as the seq default.
	_, err = exec.ExecContext(ctx,
		`INSERT INTO pm_task_dependencies (plan_id, from_task_id, to_task_id, kind, "when", max_rounds) VALUES (?,?,?,?,?,?)`,
		string(dep.PlanID), string(dep.FromTaskID), string(dep.ToTaskID),
		string(pm.NormalizeEdgeKind(dep.Kind)), dep.When, dep.MaxRounds)
	return err
}

func (r *PlanRepo) RemoveDependency(ctx context.Context, dep pm.Dependency) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM pm_task_dependencies WHERE plan_id = ? AND from_task_id = ? AND to_task_id = ?`,
		string(dep.PlanID), string(dep.FromTaskID), string(dep.ToTaskID))
	return err
}

// ListDependencies returns all depends_on edges scoped to one Plan (§9.8):
// the WHERE plan_id = ? isolates one plan's DAG from every other plan's.
func (r *PlanRepo) ListDependencies(ctx context.Context, planID pm.PlanID) ([]pm.Dependency, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT plan_id, from_task_id, to_task_id, kind, "when", max_rounds FROM pm_task_dependencies WHERE plan_id = ? ORDER BY from_task_id, to_task_id`,
		string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.Dependency
	for rows.Next() {
		d, err := scanDependency(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// scanDependency reads one edge row (plan_id, from, to, kind, "when", max_rounds).
// kind is normalized so a "" / legacy row reads back as EdgeSeq (back-compat).
func scanDependency(scan func(...any) error) (pm.Dependency, error) {
	var pid, from, to, kind, when string
	var maxRounds int
	if err := scan(&pid, &from, &to, &kind, &when, &maxRounds); err != nil {
		return pm.Dependency{}, err
	}
	return pm.Dependency{
		PlanID: pm.PlanID(pid), FromTaskID: pm.TaskID(from), ToTaskID: pm.TaskID(to),
		Kind: pm.NormalizeEdgeKind(pm.EdgeKind(kind)), When: when, MaxRounds: maxRounds,
	}, nil
}

// ListDependenciesByPlans is the BATCH form of ListDependencies: ONE
// `WHERE plan_id IN (...)` query returns every given plan's depends_on edges, so a
// per-project read loads all DAGs without an N+1 loop. Each row carries plan_id so
// the caller groups in-memory. Empty planIDs → empty slice (no malformed `IN ()`).
func (r *PlanRepo) ListDependenciesByPlans(ctx context.Context, planIDs []pm.PlanID) ([]pm.Dependency, error) {
	if len(planIDs) == 0 {
		return nil, nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	in, args := planIDPlaceholders(planIDs)
	rows, err := exec.QueryContext(ctx,
		`SELECT plan_id, from_task_id, to_task_id, kind, "when", max_rounds FROM pm_task_dependencies WHERE plan_id IN (`+in+`) ORDER BY plan_id, from_task_id, to_task_id`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.Dependency
	for rows.Next() {
		d, err := scanDependency(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// --- Dispatch records (v2.9 #285, §9.3) -------------------------------------

// RecordDispatch writes the once-only {plan_id, task_id} dispatch record. It is
// idempotent on the PK: an INSERT OR IGNORE means re-running advance / event
// replay / a second upstream completing for an already-dispatched node is a
// no-op, never an error nor a second @mention (§9.3).
func (r *PlanRepo) RecordDispatch(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, at time.Time, messageID string) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT OR IGNORE INTO pm_plan_dispatch_records (plan_id, task_id, dispatched_at, dispatch_message_id) VALUES (?,?,?,?)`,
		string(planID), string(taskID), ts(at), messageID)
	return err
}

// ListDispatchRecords returns one Plan's dispatch records (§9.8 per-plan scoping).
func (r *PlanRepo) ListDispatchRecords(ctx context.Context, planID pm.PlanID) ([]pm.DispatchRecord, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT plan_id, task_id, dispatched_at, dispatch_message_id FROM pm_plan_dispatch_records WHERE plan_id = ? ORDER BY task_id`,
		string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.DispatchRecord
	for rows.Next() {
		var pid, tid, at, mid string
		if err := rows.Scan(&pid, &tid, &at, &mid); err != nil {
			return nil, err
		}
		out = append(out, pm.DispatchRecord{
			PlanID: pm.PlanID(pid), TaskID: pm.TaskID(tid),
			DispatchedAt: parseTime(at), DispatchMessageID: mid,
		})
	}
	return out, rows.Err()
}

// ListDispatchRecordsByPlans is the BATCH form of ListDispatchRecords: ONE
// `WHERE plan_id IN (...)` query returns every given plan's dispatch records, so a
// per-project read loads all dispatch state without an N+1 loop. Each row carries
// plan_id so the caller groups in-memory. Empty planIDs → empty slice.
func (r *PlanRepo) ListDispatchRecordsByPlans(ctx context.Context, planIDs []pm.PlanID) ([]pm.DispatchRecord, error) {
	if len(planIDs) == 0 {
		return nil, nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	in, args := planIDPlaceholders(planIDs)
	rows, err := exec.QueryContext(ctx,
		`SELECT plan_id, task_id, dispatched_at, dispatch_message_id FROM pm_plan_dispatch_records WHERE plan_id IN (`+in+`) ORDER BY plan_id, task_id`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.DispatchRecord
	for rows.Next() {
		var pid, tid, at, mid string
		if err := rows.Scan(&pid, &tid, &at, &mid); err != nil {
			return nil, err
		}
		out = append(out, pm.DispatchRecord{
			PlanID: pm.PlanID(pid), TaskID: pm.TaskID(tid),
			DispatchedAt: parseTime(at), DispatchMessageID: mid,
		})
	}
	return out, rows.Err()
}

// ClearDispatch deletes one node's dispatch record (creator re-run path, §9.3;
// also the B1 loopback reopen path — clearing makes a reopened node ready again).
// Deleting a non-existent record is a no-op (not an error).
func (r *PlanRepo) ClearDispatch(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM pm_plan_dispatch_records WHERE plan_id = ? AND task_id = ?`,
		string(planID), string(taskID))
	return err
}

// --- Decision outcomes (v2.13.0 I18/B1, control-flow §2.3) ------------------

// RecordDecisionOutcome upserts a decision node's outcome (latest-wins per
// plan_id,task_id): a reopened decision re-deciding overwrites its prior outcome.
// INSERT-OR-REPLACE on the PK so it is idempotent + overwrite-on-redecision.
func (r *PlanRepo) RecordDecisionOutcome(ctx context.Context, planID pm.PlanID, taskID pm.TaskID, outcome string, at time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT OR REPLACE INTO pm_plan_decision_outcomes (plan_id, task_id, outcome, decided_at) VALUES (?,?,?,?)`,
		string(planID), string(taskID), outcome, ts(at))
	return err
}

// ListDecisionOutcomes returns one Plan's recorded decision outcomes (§9.8 scoping).
func (r *PlanRepo) ListDecisionOutcomes(ctx context.Context, planID pm.PlanID) ([]pm.DecisionOutcome, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT plan_id, task_id, outcome FROM pm_plan_decision_outcomes WHERE plan_id = ? ORDER BY task_id`,
		string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.DecisionOutcome
	for rows.Next() {
		var pid, tid, oc string
		if err := rows.Scan(&pid, &tid, &oc); err != nil {
			return nil, err
		}
		out = append(out, pm.DecisionOutcome{PlanID: pm.PlanID(pid), TaskID: pm.TaskID(tid), Outcome: oc})
	}
	return out, rows.Err()
}

// ListDecisionOutcomesByPlans is the BATCH form of ListDecisionOutcomes (one
// `WHERE plan_id IN (...)` query), so a per-project read loads every plan's outcomes
// without an N+1 loop. Empty planIDs → nil.
func (r *PlanRepo) ListDecisionOutcomesByPlans(ctx context.Context, planIDs []pm.PlanID) ([]pm.DecisionOutcome, error) {
	if len(planIDs) == 0 {
		return nil, nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	in, args := planIDPlaceholders(planIDs)
	rows, err := exec.QueryContext(ctx,
		`SELECT plan_id, task_id, outcome FROM pm_plan_decision_outcomes WHERE plan_id IN (`+in+`) ORDER BY plan_id, task_id`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.DecisionOutcome
	for rows.Next() {
		var pid, tid, oc string
		if err := rows.Scan(&pid, &tid, &oc); err != nil {
			return nil, err
		}
		out = append(out, pm.DecisionOutcome{PlanID: pm.PlanID(pid), TaskID: pm.TaskID(tid), Outcome: oc})
	}
	return out, rows.Err()
}

// ClearDecisionOutcome removes a decision's recorded outcome (loopback reopen path —
// a reopened decision must re-decide). No-op if absent.
func (r *PlanRepo) ClearDecisionOutcome(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM pm_plan_decision_outcomes WHERE plan_id = ? AND task_id = ?`,
		string(planID), string(taskID))
	return err
}

// --- Loop rounds (v2.13.0 I18/B1, control-flow §4) --------------------------

// GetLoopRound returns the current completed-round count for a loopback edge
// (plan_id, from_task_id, to_task_id). 0 when no loop has fired yet.
func (r *PlanRepo) GetLoopRound(ctx context.Context, planID pm.PlanID, from, to pm.TaskID) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		`SELECT round FROM pm_plan_loop_rounds WHERE plan_id = ? AND from_task_id = ? AND to_task_id = ?`,
		string(planID), string(from), string(to))
	var round int
	switch err := row.Scan(&round); err {
	case nil:
		return round, nil
	case sql.ErrNoRows:
		return 0, nil
	default:
		return 0, err
	}
}

// IncrementLoopRound bumps (or initializes to 1) the round count for a loopback edge
// and returns the NEW round. Upsert on the PK (plan_id, from, to).
func (r *PlanRepo) IncrementLoopRound(ctx context.Context, planID pm.PlanID, from, to pm.TaskID) (int, error) {
	cur, err := r.GetLoopRound(ctx, planID, from, to)
	if err != nil {
		return 0, err
	}
	next := cur + 1
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err = exec.ExecContext(ctx,
		`INSERT OR REPLACE INTO pm_plan_loop_rounds (plan_id, from_task_id, to_task_id, round) VALUES (?,?,?,?)`,
		string(planID), string(from), string(to), next)
	if err != nil {
		return 0, err
	}
	return next, nil
}

// --- Review verdicts (v2.13.0 I18/B3, T468 / issue-f7ad5a54) ----------------

// RecordReviewVerdict upserts a Review node's structured verdict (latest-wins per
// plan_id,task_id — each round overwrites). INSERT-OR-REPLACE on the PK.
func (r *PlanRepo) RecordReviewVerdict(ctx context.Context, planID pm.PlanID, v pm.ReviewVerdict, at time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT OR REPLACE INTO pm_plan_review_verdicts (plan_id, task_id, verdict, blocking, reason, sha, round, recorded_at) VALUES (?,?,?,?,?,?,?,?)`,
		string(planID), string(v.TaskID), v.Verdict, boolToInt(v.Blocking), v.Reason, v.SHA, v.Round, ts(at))
	return err
}

// GetReviewVerdict returns one Review node's verdict (ok=false when none recorded).
func (r *PlanRepo) GetReviewVerdict(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) (pm.ReviewVerdict, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		`SELECT verdict, blocking, reason, sha, round FROM pm_plan_review_verdicts WHERE plan_id = ? AND task_id = ?`,
		string(planID), string(taskID))
	v := pm.ReviewVerdict{PlanID: planID, TaskID: taskID}
	var blocking, round int
	switch err := row.Scan(&v.Verdict, &blocking, &v.Reason, &v.SHA, &round); err {
	case nil:
		v.Blocking = blocking != 0
		v.Round = round
		return v, true, nil
	case sql.ErrNoRows:
		return pm.ReviewVerdict{}, false, nil
	default:
		return pm.ReviewVerdict{}, false, err
	}
}

// ListReviewVerdicts returns one Plan's recorded review verdicts (PD read path).
func (r *PlanRepo) ListReviewVerdicts(ctx context.Context, planID pm.PlanID) ([]pm.ReviewVerdict, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT task_id, verdict, blocking, reason, sha, round FROM pm_plan_review_verdicts WHERE plan_id = ? ORDER BY task_id`,
		string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.ReviewVerdict
	for rows.Next() {
		v := pm.ReviewVerdict{PlanID: planID}
		var blocking, round int
		if err := rows.Scan(&v.TaskID, &v.Verdict, &blocking, &v.Reason, &v.SHA, &round); err != nil {
			return nil, err
		}
		v.Blocking = blocking != 0
		v.Round = round
		out = append(out, v)
	}
	return out, rows.Err()
}

// --- Stage gate extra-round request ledger ---------------------------------

func (r *PlanRepo) RecordStageGateReopenRequest(ctx context.Context, req pm.StageGateReopenRequest) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_stage_gate_reopen_requests
		 (plan_id, stage_id, idempotency_key, actor_ref, reason, prior_gate_task_id, prior_round, new_round, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		string(req.PlanID), string(req.StageID), req.IdempotencyKey, string(req.ActorRef), req.Reason,
		string(req.PriorGateTaskID), req.PriorRound, req.NewRound, ts(req.CreatedAt))
	if isUnique(err) {
		return false, nil
	}
	return err == nil, err
}

func (r *PlanRepo) GetStageGateReopenRequest(ctx context.Context, planID pm.PlanID, stageID pm.StageID, key string) (pm.StageGateReopenRequest, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		`SELECT actor_ref, reason, prior_gate_task_id, prior_round, new_round, created_at
		 FROM pm_stage_gate_reopen_requests
		 WHERE plan_id = ? AND stage_id = ? AND idempotency_key = ?`,
		string(planID), string(stageID), key)
	req := pm.StageGateReopenRequest{PlanID: planID, StageID: stageID, IdempotencyKey: key}
	var actorRef, gateTaskID, createdAt string
	switch err := row.Scan(&actorRef, &req.Reason, &gateTaskID, &req.PriorRound, &req.NewRound, &createdAt); err {
	case nil:
		req.ActorRef = pm.IdentityRef(actorRef)
		req.PriorGateTaskID = pm.TaskID(gateTaskID)
		req.CreatedAt = parseTime(createdAt)
		return req, true, nil
	case sql.ErrNoRows:
		return pm.StageGateReopenRequest{}, false, nil
	default:
		return pm.StageGateReopenRequest{}, false, err
	}
}

// --- BlockedOn snapshots (I103 §1) -----------------------------------------

// encodeWaitKeys serializes the wait_keys id list to a stored JSON array. A nil/empty
// list stores "" (the schema default), decoded back to nil — so an empty round-trip is
// byte-stable (the idempotent materialize never sees a spurious [] vs nil diff).
func encodeWaitKeys(keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	b, err := json.Marshal(keys)
	if err != nil {
		return ""
	}
	return string(b)
}

// decodeWaitKeys parses the stored JSON array back to the id list ("" → nil).
func decodeWaitKeys(s string) []string {
	if s == "" {
		return nil
	}
	var keys []string
	if err := json.Unmarshal([]byte(s), &keys); err != nil {
		return nil
	}
	return keys
}

// UpsertBlockedOn writes/refreshes one node's BlockedOn slot (single-slot latest-wins
// per plan_id,task_id). INSERT-OR-REPLACE on the PK — the service computes the
// preserved fields (waited_since / probe fields) before calling, so this is a plain
// whole-row overwrite.
func (r *PlanRepo) UpsertBlockedOn(ctx context.Context, b pm.BlockedOn) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT OR REPLACE INTO pm_plan_blocked_on
		 (plan_id, task_id, node_id, wait_type, wait_keys, trigger_condition, waited_since, deadline, on_timeout, last_probe_at, probe_count)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		string(b.PlanID), string(b.TaskID), b.NodeID, string(b.WaitType),
		encodeWaitKeys(b.WaitKeys), b.TriggerCondition, ts(b.WaitedSince),
		tsPtr(&b.Deadline), b.OnTimeout, tsPtr(&b.LastProbeAt), b.ProbeCount)
	return err
}

// ClearBlockedOn deletes one node's BlockedOn slot (the node entered ready/running/
// terminal). Idempotent: deleting an absent row is not an error.
func (r *PlanRepo) ClearBlockedOn(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`DELETE FROM pm_plan_blocked_on WHERE plan_id = ? AND task_id = ?`,
		string(planID), string(taskID))
	return err
}

// blockedOnSelect selects a full BlockedOn row (task_id leading). scanBlockedOn reads
// this exact column order.
const blockedOnSelect = `SELECT task_id, node_id, wait_type, wait_keys, trigger_condition, waited_since, deadline, on_timeout, last_probe_at, probe_count FROM pm_plan_blocked_on`

// scanBlockedOn scans one `task_id, <cols>` row into a BlockedOn carrying planID.
func scanBlockedOn(scan func(...any) error, planID pm.PlanID) (pm.BlockedOn, error) {
	var (
		taskID, nodeID, waitType, waitKeys, trigger, waitedSince, deadline, onTimeout, lastProbe string
		probeCount                                                                               int
	)
	if err := scan(&taskID, &nodeID, &waitType, &waitKeys, &trigger, &waitedSince, &deadline, &onTimeout, &lastProbe, &probeCount); err != nil {
		return pm.BlockedOn{}, err
	}
	b := pm.BlockedOn{
		NodeID:           nodeID,
		TaskID:           pm.TaskID(taskID),
		PlanID:           planID,
		WaitType:         pm.WaitType(waitType),
		WaitKeys:         decodeWaitKeys(waitKeys),
		TriggerCondition: trigger,
		WaitedSince:      parseTime(waitedSince),
		OnTimeout:        onTimeout,
		ProbeCount:       probeCount,
	}
	if d := parseTimePtr(deadline); d != nil {
		b.Deadline = *d
	}
	if lp := parseTimePtr(lastProbe); lp != nil {
		b.LastProbeAt = *lp
	}
	return b, nil
}

// GetBlockedOn returns one node's BlockedOn slot (ok=false when none recorded — so the
// materialize can distinguish a first materialize from a refresh).
func (r *PlanRepo) GetBlockedOn(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) (pm.BlockedOn, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		blockedOnSelect+` WHERE plan_id = ? AND task_id = ?`, string(planID), string(taskID))
	b, err := scanBlockedOn(row.Scan, planID)
	switch err {
	case nil:
		return b, true, nil
	case sql.ErrNoRows:
		return pm.BlockedOn{}, false, nil
	default:
		return pm.BlockedOn{}, false, err
	}
}

// ListBlockedOn returns one plan's BlockedOn snapshots, stable-ordered (task_id).
func (r *PlanRepo) ListBlockedOn(ctx context.Context, planID pm.PlanID) ([]pm.BlockedOn, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		blockedOnSelect+` WHERE plan_id = ? ORDER BY task_id`, string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.BlockedOn
	for rows.Next() {
		b, serr := scanBlockedOn(rows.Scan, planID)
		if serr != nil {
			return nil, serr
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// --- Progress control S2A ----------------------------------------------------

func (r *PlanRepo) SaveProgressObservation(ctx context.Context, v pm.ObservationVector) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	src, err := json.Marshal(v.SourceRevisions)
	if err != nil {
		return err
	}
	facts, err := json.Marshal(v.Facts)
	if err != nil {
		return err
	}
	cov, err := json.Marshal(v.Coverage)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO pm_progress_observations
		(id, plan_id, task_id, node_id, decision, quality, as_of, evaluated_at,
		 source_revisions_json, facts_json, suspect_key, suspect_cycles,
		 progress_contract, progress_contract_defaulted, uncovered_progress_window_seconds,
		 coverage_json, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID, string(v.PlanID), string(v.TaskID), v.NodeID, string(v.Decision), string(v.Quality),
		ts(v.AsOf), ts(v.EvaluatedAt), string(src), string(facts), v.SuspectKey, v.SuspectCycles,
		string(v.ProgressContract), boolToInt(v.ProgressContractDefaulted), v.UncoveredProgressWindowSeconds,
		string(cov), ts(v.EvaluatedAt))
	return err
}

func (r *PlanRepo) LatestProgressObservation(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) (pm.ObservationVector, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, progressObservationSelect+
		` WHERE plan_id = ? AND task_id = ? ORDER BY evaluated_at DESC, id DESC LIMIT 1`, string(planID), string(taskID))
	v, err := scanProgressObservation(row.Scan)
	switch err {
	case nil:
		return v, true, nil
	case sql.ErrNoRows:
		return pm.ObservationVector{}, false, nil
	default:
		return pm.ObservationVector{}, false, err
	}
}

const progressObservationSelect = `SELECT id, plan_id, task_id, node_id, decision, quality,
	as_of, evaluated_at, source_revisions_json, facts_json, suspect_key, suspect_cycles,
	progress_contract, progress_contract_defaulted, uncovered_progress_window_seconds, coverage_json
	FROM pm_progress_observations`

func scanProgressObservation(scan func(...any) error) (pm.ObservationVector, error) {
	var id, planID, taskID, nodeID, decision, quality, asOf, evaluatedAt, srcJSON, factsJSON, suspectKey, contract, covJSON string
	var suspectCycles, contractDefaulted int
	var uncovered int64
	if err := scan(&id, &planID, &taskID, &nodeID, &decision, &quality, &asOf, &evaluatedAt,
		&srcJSON, &factsJSON, &suspectKey, &suspectCycles, &contract, &contractDefaulted, &uncovered, &covJSON); err != nil {
		return pm.ObservationVector{}, err
	}
	var src []pm.ObservationSource
	if srcJSON != "" {
		if err := json.Unmarshal([]byte(srcJSON), &src); err != nil {
			return pm.ObservationVector{}, err
		}
	}
	var facts []pm.ProgressFact
	if factsJSON != "" {
		if err := json.Unmarshal([]byte(factsJSON), &facts); err != nil {
			return pm.ObservationVector{}, err
		}
	}
	var cov pm.ProgressCoverage
	if covJSON != "" {
		if err := json.Unmarshal([]byte(covJSON), &cov); err != nil {
			return pm.ObservationVector{}, err
		}
	}
	return pm.ObservationVector{
		ID: id, PlanID: pm.PlanID(planID), TaskID: pm.TaskID(taskID), NodeID: nodeID,
		Decision: pm.ProgressDecision(decision), Quality: pm.ProgressQuality(quality),
		AsOf: parseTime(asOf), EvaluatedAt: parseTime(evaluatedAt), SourceRevisions: src, Facts: facts,
		SuspectKey: suspectKey, SuspectCycles: suspectCycles, ProgressContract: pm.DeliveryContract(contract),
		ProgressContractDefaulted: contractDefaulted != 0, UncoveredProgressWindowSeconds: uncovered, Coverage: cov,
	}, nil
}

func (r *PlanRepo) UpsertProgressObligation(ctx context.Context, o pm.ProgressObligation) error {
	return r.upsertProgressResponsibility(ctx, "pm_progress_obligations", progressObligationArgs(o)...)
}

func (r *PlanRepo) UpsertProgressIncident(ctx context.Context, i pm.ProgressIncident) error {
	return r.upsertProgressResponsibility(ctx, "pm_progress_incidents", progressIncidentArgs(i)...)
}

func (r *PlanRepo) upsertProgressResponsibility(ctx context.Context, table string, args ...any) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO `+table+`
		(id, plan_id, task_id, node_id, kind, owner_ref, owner_display, deadline_at,
		 ack_required, acked_at, escalate_to_ref, escalation_deadline_at,
		 source_fact_refs_json, episode_key, status, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		 ON CONFLICT(plan_id, task_id, kind, episode_key) DO UPDATE SET
		 owner_ref=excluded.owner_ref,
		 owner_display=excluded.owner_display,
		 deadline_at=excluded.deadline_at,
		 ack_required=excluded.ack_required,
		 acked_at=excluded.acked_at,
		 escalate_to_ref=excluded.escalate_to_ref,
		 escalation_deadline_at=excluded.escalation_deadline_at,
		 source_fact_refs_json=excluded.source_fact_refs_json,
		 status=excluded.status,
		 updated_at=excluded.updated_at,
		 version=`+table+`.version+1`, args...)
	return err
}

func progressObligationArgs(o pm.ProgressObligation) []any {
	refs, _ := json.Marshal(o.SourceFactRefs)
	return []any{o.ID, string(o.PlanID), string(o.TaskID), o.NodeID, string(o.Kind), string(o.OwnerRef),
		o.OwnerDisplay, ts(o.DeadlineAt), boolToInt(o.AckRequired), tsPtr(o.AckedAt),
		string(o.EscalateToRef), ts(o.EscalationDeadlineAt), string(refs), o.EpisodeKey,
		string(o.Status), ts(o.CreatedAt), ts(o.UpdatedAt), o.Version}
}

func progressIncidentArgs(i pm.ProgressIncident) []any {
	refs, _ := json.Marshal(i.SourceFactRefs)
	return []any{i.ID, string(i.PlanID), string(i.TaskID), i.NodeID, string(i.Kind), string(i.OwnerRef),
		i.OwnerDisplay, ts(i.DeadlineAt), boolToInt(i.AckRequired), tsPtr(i.AckedAt),
		string(i.EscalateToRef), ts(i.EscalationDeadlineAt), string(refs), i.EpisodeKey,
		string(i.Status), ts(i.CreatedAt), ts(i.UpdatedAt), i.Version}
}

func (r *PlanRepo) ListOpenProgressObligations(ctx context.Context, planID pm.PlanID) ([]pm.ProgressObligation, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, progressResponsibilitySelect("pm_progress_obligations")+
		` WHERE plan_id = ? AND status = ? ORDER BY deadline_at, task_id, kind`, string(planID), string(pm.ResponsibilityOpen))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.ProgressObligation
	for rows.Next() {
		o, err := scanProgressObligation(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func (r *PlanRepo) ListOpenProgressIncidents(ctx context.Context, planID pm.PlanID) ([]pm.ProgressIncident, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, progressResponsibilitySelect("pm_progress_incidents")+
		` WHERE plan_id = ? AND status = ? ORDER BY deadline_at, task_id, kind`, string(planID), string(pm.ResponsibilityOpen))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.ProgressIncident
	for rows.Next() {
		i, err := scanProgressIncident(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

func progressResponsibilitySelect(table string) string {
	return `SELECT id, plan_id, task_id, node_id, kind, owner_ref, owner_display, deadline_at,
		ack_required, acked_at, escalate_to_ref, escalation_deadline_at, source_fact_refs_json,
		episode_key, status, created_at, updated_at, version FROM ` + table
}

func scanProgressObligation(scan func(...any) error) (pm.ProgressObligation, error) {
	var base progressRespRow
	if err := scanProgressResp(&base, scan); err != nil {
		return pm.ProgressObligation{}, err
	}
	return pm.ProgressObligation{
		ID: base.id, PlanID: pm.PlanID(base.planID), TaskID: pm.TaskID(base.taskID), NodeID: base.nodeID,
		Kind: pm.ProgressObligationKind(base.kind), OwnerRef: pm.IdentityRef(base.ownerRef), OwnerDisplay: base.ownerDisplay,
		DeadlineAt: base.deadlineAt, AckRequired: base.ackRequired, AckedAt: base.ackedAt,
		EscalateToRef: pm.IdentityRef(base.escalateToRef), EscalationDeadlineAt: base.escalationDeadlineAt,
		SourceFactRefs: base.sourceFactRefs, EpisodeKey: base.episodeKey, Status: pm.ResponsibilityStatus(base.status),
		CreatedAt: base.createdAt, UpdatedAt: base.updatedAt, Version: base.version,
	}, nil
}

func scanProgressIncident(scan func(...any) error) (pm.ProgressIncident, error) {
	var base progressRespRow
	if err := scanProgressResp(&base, scan); err != nil {
		return pm.ProgressIncident{}, err
	}
	return pm.ProgressIncident{
		ID: base.id, PlanID: pm.PlanID(base.planID), TaskID: pm.TaskID(base.taskID), NodeID: base.nodeID,
		Kind: pm.ProgressIncidentKind(base.kind), OwnerRef: pm.IdentityRef(base.ownerRef), OwnerDisplay: base.ownerDisplay,
		DeadlineAt: base.deadlineAt, AckRequired: base.ackRequired, AckedAt: base.ackedAt,
		EscalateToRef: pm.IdentityRef(base.escalateToRef), EscalationDeadlineAt: base.escalationDeadlineAt,
		SourceFactRefs: base.sourceFactRefs, EpisodeKey: base.episodeKey, Status: pm.ResponsibilityStatus(base.status),
		CreatedAt: base.createdAt, UpdatedAt: base.updatedAt, Version: base.version,
	}, nil
}

type progressRespRow struct {
	id, planID, taskID, nodeID, kind, ownerRef, ownerDisplay, escalateToRef, refsJSON, episodeKey, status string
	deadlineAt, escalationDeadlineAt, createdAt, updatedAt                                                time.Time
	ackRequired                                                                                           bool
	ackedAt                                                                                               *time.Time
	sourceFactRefs                                                                                        []string
	version                                                                                               int
}

func scanProgressResp(out *progressRespRow, scan func(...any) error) error {
	var deadline, acked, escalation, created, updated string
	var ackReq int
	if err := scan(&out.id, &out.planID, &out.taskID, &out.nodeID, &out.kind, &out.ownerRef, &out.ownerDisplay,
		&deadline, &ackReq, &acked, &out.escalateToRef, &escalation, &out.refsJSON, &out.episodeKey, &out.status,
		&created, &updated, &out.version); err != nil {
		return err
	}
	out.deadlineAt = parseTime(deadline)
	out.ackRequired = ackReq != 0
	out.ackedAt = parseTimePtr(acked)
	out.escalationDeadlineAt = parseTime(escalation)
	out.createdAt = parseTime(created)
	out.updatedAt = parseTime(updated)
	if out.refsJSON != "" {
		if err := json.Unmarshal([]byte(out.refsJSON), &out.sourceFactRefs); err != nil {
			return err
		}
	}
	return nil
}

// --- Plan generations --------------------------------------------------------

func (r *PlanRepo) SaveGeneration(ctx context.Context, g *pm.PlanGeneration) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	diff, err := json.Marshal(g.Diff)
	if err != nil {
		return err
	}
	snapshot, err := json.Marshal(g.Snapshot)
	if err != nil {
		return err
	}
	dispatched, err := json.Marshal(g.DispatchedTaskIDs)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx,
		`INSERT INTO pm_plan_generations
		 (id, plan_id, parent_generation_id, reason, evidence, creator_ref, diff_json,
		  snapshot_json, idempotency_key, request_fingerprint, dispatched_task_ids_json, created_at)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(g.ID), string(g.PlanID), string(g.ParentGenerationID), g.Reason, g.Evidence,
		string(g.CreatorRef), string(diff), string(snapshot), g.IdempotencyKey,
		g.RequestFingerprint, string(dispatched), ts(g.CreatedAt))
	if isUnique(err) {
		return pm.ErrPlanGenerationExists
	}
	return err
}

const generationSelect = `SELECT id, plan_id, parent_generation_id, reason, evidence, creator_ref,
	diff_json, snapshot_json, idempotency_key, request_fingerprint, dispatched_task_ids_json, created_at
	FROM pm_plan_generations`

func scanGeneration(scan func(...any) error) (*pm.PlanGeneration, error) {
	var id, planID, parentID, reason, evidence, creator, diffJSON, snapshotJSON, key, fp, dispatchedJSON, createdAt string
	if err := scan(&id, &planID, &parentID, &reason, &evidence, &creator, &diffJSON, &snapshotJSON, &key, &fp, &dispatchedJSON, &createdAt); err != nil {
		return nil, err
	}
	var diff pm.PlanGenerationDiff
	if err := json.Unmarshal([]byte(diffJSON), &diff); err != nil {
		return nil, err
	}
	var snapshot pm.PlanGenerationSnapshot
	if err := json.Unmarshal([]byte(snapshotJSON), &snapshot); err != nil {
		return nil, err
	}
	var dispatched []pm.TaskID
	if strings.TrimSpace(dispatchedJSON) != "" {
		if err := json.Unmarshal([]byte(dispatchedJSON), &dispatched); err != nil {
			return nil, err
		}
	}
	return pm.NewPlanGeneration(pm.PlanGeneration{
		ID:                 pm.PlanGenerationID(id),
		PlanID:             pm.PlanID(planID),
		ParentGenerationID: pm.PlanGenerationID(parentID),
		Reason:             reason,
		Evidence:           evidence,
		CreatorRef:         pm.IdentityRef(creator),
		Diff:               diff,
		Snapshot:           snapshot,
		IdempotencyKey:     key,
		RequestFingerprint: fp,
		DispatchedTaskIDs:  dispatched,
		CreatedAt:          parseTime(createdAt),
	})
}

func (r *PlanRepo) FindGenerationByID(ctx context.Context, id pm.PlanGenerationID) (*pm.PlanGeneration, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	g, err := scanGeneration(exec.QueryRowContext(ctx, generationSelect+` WHERE id = ?`, string(id)).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrPlanGenerationNotFound
	}
	return g, err
}

func (r *PlanRepo) FindGenerationByIdempotencyKey(ctx context.Context, planID pm.PlanID, key string) (*pm.PlanGeneration, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	g, err := scanGeneration(exec.QueryRowContext(ctx,
		generationSelect+` WHERE plan_id = ? AND idempotency_key = ?`, string(planID), key).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return g, err == nil, err
}

func (r *PlanRepo) ActivateGeneration(ctx context.Context, planID pm.PlanID, generationID pm.PlanGenerationID, expectedVersion, nextVersion int, at time.Time) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_plans SET active_generation_id = ?, version = ?, updated_at = ? WHERE id = ? AND version = ?`,
		string(generationID), nextVersion, ts(at), string(planID), expectedVersion)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

const planSelect = `SELECT id, project_id, name, description, status, creator_ref, conversation_id, target_date, is_builtin, org_number, created_at, updated_at, version, graph_id, active_generation_id, archived_at, archived_by FROM pm_plans`

// boolToInt maps a Go bool to SQLite's 0/1 integer storage convention.
func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func scanPlan(scan func(...any) error) (*pm.Plan, error) {
	var (
		id, projectID, name, description, status, creatorRef, conversationID, targetDate, createdAt, updatedAt string
		isBuiltin                                                                                              int
		orgNumber                                                                                              sql.NullInt64
		version                                                                                                int
		graphID, activeGenerationID, archivedAt, archivedBy                                                    string
	)
	if err := scan(&id, &projectID, &name, &description, &status, &creatorRef, &conversationID, &targetDate, &isBuiltin, &orgNumber, &createdAt, &updatedAt, &version, &graphID, &activeGenerationID, &archivedAt, &archivedBy); err != nil {
		return nil, err
	}
	return pm.RehydratePlan(pm.RehydratePlanInput{
		ID: pm.PlanID(id), ProjectID: pm.ProjectID(projectID), Name: name, Description: description,
		Status: pm.PlanStatus(status), CreatorRef: pm.IdentityRef(creatorRef), ConversationID: conversationID,
		TargetDate:         parseTimePtr(targetDate),
		Builtin:            isBuiltin != 0,
		OrgNumber:          int(orgNumber.Int64),
		GraphID:            graphID,
		ActiveGenerationID: pm.PlanGenerationID(activeGenerationID),
		ArchivedAt:         parseTimePtr(archivedAt), ArchivedBy: pm.IdentityRef(archivedBy),
		CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
	})
}

var _ pm.PlanRepository = (*PlanRepo)(nil)
