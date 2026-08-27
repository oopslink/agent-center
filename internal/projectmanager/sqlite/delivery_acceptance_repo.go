package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

type DeliveryAcceptanceRepo struct{ db *sql.DB }

func NewDeliveryAcceptanceRepo(db *sql.DB) *DeliveryAcceptanceRepo {
	return &DeliveryAcceptanceRepo{db: db}
}

func (r *DeliveryAcceptanceRepo) SaveDeliverySubject(ctx context.Context, s pm.DeliverySubject) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_delivery_subjects
		(id, subject_type, plan_id, task_id, node_id, execution_id, repo_id, remote, branch, base_sha,
		 candidate_sha, candidate_ref, pushed_remote, delivery_contract_hash, acceptance_contract_hash, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		s.ID, string(s.SubjectType), string(s.PlanID), string(s.TaskID), s.NodeID, s.ExecutionID, s.RepoID,
		s.Remote, s.Branch, s.BaseSHA, s.CandidateSHA, s.CandidateRef, s.PushedRemote,
		s.DeliveryContractHash, s.AcceptanceContractHash, ts(s.CreatedAt))
	return err
}

const deliverySubjectSelect = `SELECT id, subject_type, plan_id, task_id, node_id, execution_id, repo_id,
	remote, branch, base_sha, candidate_sha, candidate_ref, pushed_remote, delivery_contract_hash,
	acceptance_contract_hash, created_at FROM pm_delivery_subjects`

func scanDeliverySubject(scan func(...any) error) (pm.DeliverySubject, error) {
	var s pm.DeliverySubject
	var typ, planID, taskID, createdAt string
	err := scan(&s.ID, &typ, &planID, &taskID, &s.NodeID, &s.ExecutionID, &s.RepoID,
		&s.Remote, &s.Branch, &s.BaseSHA, &s.CandidateSHA, &s.CandidateRef, &s.PushedRemote,
		&s.DeliveryContractHash, &s.AcceptanceContractHash, &createdAt)
	s.SubjectType = pm.DeliverySubjectType(typ)
	s.PlanID = pm.PlanID(planID)
	s.TaskID = pm.TaskID(taskID)
	s.CreatedAt = parseTime(createdAt)
	return s, err
}

func (r *DeliveryAcceptanceRepo) FindDeliverySubject(ctx context.Context, id string) (pm.DeliverySubject, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	s, err := scanDeliverySubject(exec.QueryRowContext(ctx, deliverySubjectSelect+` WHERE id=?`, id).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return pm.DeliverySubject{}, false, nil
	}
	return s, err == nil, err
}

func (r *DeliveryAcceptanceRepo) FindLatestDeliverySubjectByTask(ctx context.Context, planID pm.PlanID, taskID pm.TaskID) (pm.DeliverySubject, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	s, err := scanDeliverySubject(exec.QueryRowContext(ctx, deliverySubjectSelect+
		` WHERE plan_id=? AND task_id=? ORDER BY created_at DESC, id DESC LIMIT 1`, string(planID), string(taskID)).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return pm.DeliverySubject{}, false, nil
	}
	return s, err == nil, err
}

func (r *DeliveryAcceptanceRepo) SaveAcceptance(ctx context.Context, a pm.Acceptance) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `INSERT INTO pm_acceptances
		(id, subject_id, subject_digest, plan_id, task_id, gate_task_id, contract_hash, verdict, actor_ref,
		 authority_rank, authority_source, evidence_ref, evidence_sha, findings_json, created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.SubjectID, a.SubjectDigest, string(a.PlanID), string(a.TaskID), string(a.GateTaskID),
		a.ContractHash, string(a.Verdict), string(a.ActorRef), a.AuthorityRank, a.AuthoritySource,
		a.EvidenceRef, a.EvidenceSHA, a.FindingsJSON, ts(a.CreatedAt))
	return err
}

const acceptanceSelect = `SELECT id, subject_id, subject_digest, plan_id, task_id, gate_task_id, contract_hash,
	verdict, actor_ref, authority_rank, authority_source, evidence_ref, evidence_sha, findings_json, created_at FROM pm_acceptances`

func scanAcceptance(scan func(...any) error) (pm.Acceptance, error) {
	var a pm.Acceptance
	var planID, taskID, gateTaskID, verdict, actor, createdAt string
	err := scan(&a.ID, &a.SubjectID, &a.SubjectDigest, &planID, &taskID, &gateTaskID, &a.ContractHash,
		&verdict, &actor, &a.AuthorityRank, &a.AuthoritySource, &a.EvidenceRef, &a.EvidenceSHA,
		&a.FindingsJSON, &createdAt)
	a.PlanID = pm.PlanID(planID)
	a.TaskID = pm.TaskID(taskID)
	a.GateTaskID = pm.TaskID(gateTaskID)
	a.Verdict = pm.AcceptanceVerdict(verdict)
	a.ActorRef = pm.IdentityRef(actor)
	a.CreatedAt = parseTime(createdAt)
	return a, err
}

func (r *DeliveryAcceptanceRepo) FindEffectiveAcceptance(ctx context.Context, subjectID string, contractHash string) (pm.Acceptance, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	a, err := scanAcceptance(exec.QueryRowContext(ctx, acceptanceSelect+
		` WHERE subject_id=? AND contract_hash=? ORDER BY authority_rank DESC, created_at DESC, id DESC LIMIT 1`, subjectID, contractHash).Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return pm.Acceptance{}, false, nil
	}
	return a, err == nil, err
}

func (r *DeliveryAcceptanceRepo) ListAcceptances(ctx context.Context, subjectID string, contractHash string) ([]pm.Acceptance, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, acceptanceSelect+
		` WHERE subject_id=? AND contract_hash=? ORDER BY created_at, id`, subjectID, contractHash)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.Acceptance
	for rows.Next() {
		a, err := scanAcceptance(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

var _ pm.DeliveryAcceptanceRepository = (*DeliveryAcceptanceRepo)(nil)
