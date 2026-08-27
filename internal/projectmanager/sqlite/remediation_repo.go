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

type RemediationRepo struct{ db *sql.DB }

func NewRemediationRepo(db *sql.DB) *RemediationRepo { return &RemediationRepo{db: db} }

const verdictSelect = `SELECT id, project_id, plan_id, stage_id, gate_task_id, outcome,
	evidence, reviewed_sha, subject_kind, subject_locator, subject_immutable_version, subject_execution_generation,
	subject_digest, contract_revision, authority_rank, required_checks_json, reviewed_at,
	actor_ref, idempotency_key, created_at FROM pm_gate_verdicts`

func (r *RemediationRepo) SaveVerdict(ctx context.Context, v pm.GateVerdict) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	requiredChecks, err := json.Marshal(v.Acceptance.RequiredChecks)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO pm_gate_verdicts
		(id, project_id, plan_id, stage_id, gate_task_id, outcome, evidence, reviewed_sha,
		 subject_kind, subject_locator, subject_immutable_version, subject_execution_generation,
		 subject_digest, contract_revision, authority_rank, required_checks_json, reviewed_at,
		 actor_ref, idempotency_key, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, string(v.ID), string(v.ProjectID), string(v.PlanID), string(v.StageID),
		string(v.GateTaskID), string(v.Outcome), v.Evidence, v.ReviewedSHA,
		v.Subject.Kind, v.Subject.Locator, v.Subject.ImmutableVersion, v.Subject.ExecutionGeneration,
		v.Acceptance.SubjectDigest, v.Acceptance.ContractRevision, v.Acceptance.AuthorityRank, string(requiredChecks), ts(v.Acceptance.ReviewedAt),
		string(v.ActorRef), v.IdempotencyKey, ts(v.CreatedAt))
	if isUnique(err) {
		return pm.ErrGateAlreadyVerdicted
	}
	return err
}

func scanVerdict(scan func(...any) error) (pm.GateVerdict, error) {
	var id, projectID, planID, stageID, gateTaskID, outcome, evidence, sha string
	var subjectKind, subjectLocator, subjectImmutableVersion string
	var subjectGeneration, authorityRank int
	var subjectDigest, contractRevision, requiredChecksJSON, reviewedAt, actor, key, createdAt string
	err := scan(&id, &projectID, &planID, &stageID, &gateTaskID, &outcome, &evidence, &sha,
		&subjectKind, &subjectLocator, &subjectImmutableVersion, &subjectGeneration,
		&subjectDigest, &contractRevision, &authorityRank, &requiredChecksJSON, &reviewedAt,
		&actor, &key, &createdAt)
	var checks []string
	if err == nil && requiredChecksJSON != "" {
		err = json.Unmarshal([]byte(requiredChecksJSON), &checks)
	}
	return pm.GateVerdict{ID: pm.GateVerdictID(id), ProjectID: pm.ProjectID(projectID), PlanID: pm.PlanID(planID),
		StageID: pm.StageID(stageID), GateTaskID: pm.TaskID(gateTaskID), Outcome: pm.GateVerdictOutcome(outcome),
		Evidence: evidence, ReviewedSHA: sha,
		Subject:    pm.DeliverySubject{Kind: subjectKind, Locator: subjectLocator, ImmutableVersion: subjectImmutableVersion, ExecutionGeneration: subjectGeneration},
		Acceptance: pm.Acceptance{SubjectDigest: subjectDigest, ContractRevision: contractRevision, Verdict: outcome, AuthorityRank: authorityRank, RequiredChecks: checks, ReviewedAt: parseTime(reviewedAt)},
		ActorRef:   pm.IdentityRef(actor), IdempotencyKey: key, CreatedAt: parseTime(createdAt)}, err
}

func (r *RemediationRepo) FindVerdictByGate(ctx context.Context, gateTaskID pm.TaskID) (pm.GateVerdict, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	v, err := scanVerdict(exec.QueryRowContext(ctx, verdictSelect+` WHERE gate_task_id=?`, string(gateTaskID)).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return pm.GateVerdict{}, false, nil
	}
	return v, err == nil, err
}

func (r *RemediationRepo) FindVerdictByKey(ctx context.Context, key string) (pm.GateVerdict, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	v, err := scanVerdict(exec.QueryRowContext(ctx, verdictSelect+` WHERE idempotency_key=?`, key).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return pm.GateVerdict{}, false, nil
	}
	return v, err == nil, err
}

func (r *RemediationRepo) ListVerdictsByPlan(ctx context.Context, planID pm.PlanID) ([]pm.GateVerdict, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, verdictSelect+` WHERE plan_id=? ORDER BY created_at,id`, string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.GateVerdict
	for rows.Next() {
		v, err := scanVerdict(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

const continuationSelect = `SELECT id, project_id, plan_id, root_stage_id, current_stage_id,
	trigger_verdict_id, status, generation, remaining_budget, boundary_fingerprint,
	pending_proposal_id, closed_by_verdict_id, created_at, updated_at, version FROM pm_plan_continuations`

func (r *RemediationRepo) SaveContinuation(ctx context.Context, c *pm.PlanContinuation) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_plan_continuations
		(id, project_id, plan_id, root_stage_id, current_stage_id, trigger_verdict_id, status,
		 generation, remaining_budget, boundary_fingerprint, pending_proposal_id, closed_by_verdict_id,
		 created_at, updated_at, version) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, string(c.ID), string(c.ProjectID),
		string(c.PlanID), string(c.RootStageID), string(c.CurrentStageID), string(c.TriggerVerdictID), string(c.Status),
		c.Generation, c.RemainingBudget, c.BoundaryFingerprint, string(c.PendingProposalID), string(c.ClosedByVerdictID),
		ts(c.CreatedAt), ts(c.UpdatedAt), c.Version)
	if isUnique(err) {
		return pm.ErrGateAlreadyVerdicted
	}
	return err
}

func (r *RemediationRepo) UpdateContinuation(ctx context.Context, c *pm.PlanContinuation, expectedVersion int) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `UPDATE pm_plan_continuations SET current_stage_id=?, trigger_verdict_id=?, status=?,
		generation=?, remaining_budget=?, boundary_fingerprint=?, pending_proposal_id=?, closed_by_verdict_id=?,
		updated_at=?, version=? WHERE id=? AND version=?`, string(c.CurrentStageID), string(c.TriggerVerdictID), string(c.Status),
		c.Generation, c.RemainingBudget, c.BoundaryFingerprint, string(c.PendingProposalID), string(c.ClosedByVerdictID),
		ts(c.UpdatedAt), c.Version, string(c.ID), expectedVersion)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func scanContinuation(scan func(...any) error) (*pm.PlanContinuation, error) {
	c := &pm.PlanContinuation{}
	var id, projectID, planID, rootStageID, currentStageID, triggerVerdictID, status, boundary, pendingProposalID, closedBy, createdAt, updatedAt string
	err := scan(&id, &projectID, &planID, &rootStageID, &currentStageID, &triggerVerdictID, &status,
		&c.Generation, &c.RemainingBudget, &boundary, &pendingProposalID, &closedBy, &createdAt, &updatedAt, &c.Version)
	c.ID = pm.ContinuationID(id)
	c.ProjectID = pm.ProjectID(projectID)
	c.PlanID = pm.PlanID(planID)
	c.RootStageID = pm.StageID(rootStageID)
	c.CurrentStageID = pm.StageID(currentStageID)
	c.TriggerVerdictID = pm.GateVerdictID(triggerVerdictID)
	c.Status = pm.ContinuationStatus(status)
	c.BoundaryFingerprint = boundary
	c.PendingProposalID = pm.RemediationProposalID(pendingProposalID)
	c.ClosedByVerdictID = pm.GateVerdictID(closedBy)
	c.CreatedAt = parseTime(createdAt)
	c.UpdatedAt = parseTime(updatedAt)
	return c, err
}

func (r *RemediationRepo) FindContinuation(ctx context.Context, id pm.ContinuationID) (*pm.PlanContinuation, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	c, err := scanContinuation(exec.QueryRowContext(ctx, continuationSelect+` WHERE id=?`, string(id)).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrContinuationNotFound
	}
	return c, err
}

func (r *RemediationRepo) FindOpenContinuationByStage(ctx context.Context, stageID pm.StageID) (*pm.PlanContinuation, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	c, err := scanContinuation(exec.QueryRowContext(ctx, continuationSelect+` WHERE current_stage_id=? AND status!='closed' ORDER BY created_at DESC LIMIT 1`, string(stageID)).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	return c, err == nil, err
}

func (r *RemediationRepo) ListContinuationsByPlan(ctx context.Context, planID pm.PlanID) ([]*pm.PlanContinuation, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, continuationSelect+` WHERE plan_id=? ORDER BY created_at,id`, string(planID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.PlanContinuation
	for rows.Next() {
		c, err := scanContinuation(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *RemediationRepo) SaveProposal(ctx context.Context, p pm.RemediationProposal) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	payload, err := json.Marshal(p.Payload)
	if err != nil {
		return err
	}
	diagnostics, err := json.Marshal(p.Diagnostics)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO pm_remediation_proposals
		(id, project_id, plan_id, continuation_id, trigger_verdict_id, idempotency_key,
		 based_on_plan_version, boundary_fingerprint, payload_json, status, diagnostics_json,
		 created_by, created_at, committed_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, string(p.ID), string(p.ProjectID),
		string(p.PlanID), string(p.ContinuationID), string(p.TriggerVerdictID), p.IdempotencyKey, p.BasedOnPlanVersion,
		p.BoundaryFingerprint, string(payload), p.Status, string(diagnostics), string(p.CreatedBy), ts(p.CreatedAt), poolTS(p.CommittedAt))
	if isUnique(err) {
		return pm.ErrRemediationProposalExists
	}
	return err
}

func (r *RemediationRepo) UpdateProposalStatus(ctx context.Context, id pm.RemediationProposalID, status string, diagnostics []string, committedAt time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	b, _ := json.Marshal(diagnostics)
	_, err := exec.ExecContext(ctx, `UPDATE pm_remediation_proposals SET status=?, diagnostics_json=?, committed_at=? WHERE id=?`, status, string(b), poolTS(committedAt), string(id))
	return err
}

func scanProposal(scan func(...any) error) (pm.RemediationProposal, error) {
	var p pm.RemediationProposal
	var id, projectID, planID, continuationID, triggerVerdictID, payload, status, diagnostics, createdBy, createdAt, committedAt string
	err := scan(&id, &projectID, &planID, &continuationID, &triggerVerdictID, &p.IdempotencyKey,
		&p.BasedOnPlanVersion, &p.BoundaryFingerprint, &payload, &status, &diagnostics, &createdBy, &createdAt, &committedAt)
	p.ID = pm.RemediationProposalID(id)
	p.ProjectID = pm.ProjectID(projectID)
	p.PlanID = pm.PlanID(planID)
	p.ContinuationID = pm.ContinuationID(continuationID)
	p.TriggerVerdictID = pm.GateVerdictID(triggerVerdictID)
	p.Status = status
	p.CreatedBy = pm.IdentityRef(createdBy)
	p.CreatedAt = parseTime(createdAt)
	p.CommittedAt = parseTime(committedAt)
	_ = json.Unmarshal([]byte(payload), &p.Payload)
	_ = json.Unmarshal([]byte(diagnostics), &p.Diagnostics)
	return p, err
}

func (r *RemediationRepo) FindProposalByKey(ctx context.Context, key string) (pm.RemediationProposal, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const selectSQL = `SELECT id, project_id, plan_id, continuation_id, trigger_verdict_id,
		idempotency_key, based_on_plan_version, boundary_fingerprint, payload_json, status,
		diagnostics_json, created_by, created_at, committed_at FROM pm_remediation_proposals`
	p, err := scanProposal(exec.QueryRowContext(ctx, selectSQL+` WHERE idempotency_key=?`, key).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return pm.RemediationProposal{}, false, nil
	}
	return p, err == nil, err
}

var _ pm.RemediationRepository = (*RemediationRepo)(nil)
