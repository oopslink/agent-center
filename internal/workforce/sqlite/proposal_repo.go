package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// ProposalRepo implements workforce.WorkerProjectProposalRepository.
type ProposalRepo struct {
	db *sql.DB
}

// NewProposalRepo constructs the repository.
func NewProposalRepo(db *sql.DB) *ProposalRepo {
	return &ProposalRepo{db: db}
}

// Save inserts a new proposal.
func (r *ProposalRepo) Save(ctx context.Context, p *workforce.WorkerProjectProposal) error {
	if p == nil {
		return errors.New("proposal repo: nil proposal")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	meta, err := json.Marshal(p.CandidateMetadata())
	if err != nil {
		return fmt.Errorf("marshal candidate_metadata: %w", err)
	}
	const stmt = `INSERT INTO worker_project_proposals (
		id, worker_id, candidate_path, suggested_project_id,
		candidate_metadata, status, proposed_at, reviewed_at,
		reviewed_by_identity_id, resulting_mapping_id, created_at, updated_at, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(p.ID()),
		string(p.WorkerID()),
		p.CandidatePath(),
		string(p.SuggestedProjectID()),
		string(meta),
		string(p.Status()),
		p.ProposedAt().Format(time.RFC3339Nano),
		nullTimePtr(p.ReviewedAt()),
		nullString(p.ReviewedByIdentityID()),
		nullString(string(p.ResultingMappingID())),
		p.CreatedAt().Format(time.RFC3339Nano),
		p.UpdatedAt().Format(time.RFC3339Nano),
		p.Version(),
	)
	if err != nil {
		if IsUniqueConstraint(err) {
			// Either id-clash or (worker_id, candidate_path, pending)
			// partial-unique clash. Caller treats as "already exists".
			return workforce.ErrProposalAlreadyExists
		}
		return err
	}
	return nil
}

// Update writes the entire proposal back using CAS on version.
func (r *ProposalRepo) Update(ctx context.Context, p *workforce.WorkerProjectProposal) error {
	if p == nil {
		return errors.New("proposal repo: nil proposal")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	meta, err := json.Marshal(p.CandidateMetadata())
	if err != nil {
		return fmt.Errorf("marshal candidate_metadata: %w", err)
	}
	const stmt = `UPDATE worker_project_proposals
		SET candidate_metadata = ?, status = ?, reviewed_at = ?,
		    reviewed_by_identity_id = ?, resulting_mapping_id = ?,
		    updated_at = ?, version = ?
		WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		string(meta), string(p.Status()), nullTimePtr(p.ReviewedAt()),
		nullString(p.ReviewedByIdentityID()), nullString(string(p.ResultingMappingID())),
		p.UpdatedAt().Format(time.RFC3339Nano), p.Version(),
		string(p.ID()), p.Version()-1,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var c int
		row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM worker_project_proposals WHERE id = ?`, string(p.ID()))
		if err := row.Scan(&c); err != nil {
			return err
		}
		if c == 0 {
			return workforce.ErrProposalNotFound
		}
		return workforce.ErrProposalVersionConflict
	}
	return nil
}

// FindByID returns a Proposal; ErrProposalNotFound if absent.
func (r *ProposalRepo) FindByID(ctx context.Context, id workforce.ProposalID) (*workforce.WorkerProjectProposal, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, proposalSelect+` WHERE id = ?`, string(id))
	p, err := scanProposal(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrProposalNotFound
	}
	return p, err
}

// FindByWorkerID returns all proposals for a worker, optionally filtered
// by status.
func (r *ProposalRepo) FindByWorkerID(ctx context.Context, workerID workforce.WorkerID, statuses ...workforce.ProposalStatus) ([]*workforce.WorkerProjectProposal, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	args := []any{string(workerID)}
	q := proposalSelect + ` WHERE worker_id = ?`
	if len(statuses) > 0 {
		ph := make([]string, len(statuses))
		for i, s := range statuses {
			ph[i] = "?"
			args = append(args, string(s))
		}
		q += ` AND status IN (` + strings.Join(ph, ",") + `)`
	}
	q += ` ORDER BY proposed_at`
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProposals(rows)
}

// FindPending returns all pending proposals across all workers.
func (r *ProposalRepo) FindPending(ctx context.Context) ([]*workforce.WorkerProjectProposal, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx,
		proposalSelect+` WHERE status = ? ORDER BY proposed_at`,
		string(workforce.ProposalPending))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanProposals(rows)
}

// FindByCandidatePath finds the active proposal for (worker, candidate_path);
// returns ErrProposalNotFound when no row matches.
func (r *ProposalRepo) FindByCandidatePath(ctx context.Context, workerID workforce.WorkerID, candidatePath string) (*workforce.WorkerProjectProposal, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx,
		proposalSelect+` WHERE worker_id = ? AND candidate_path = ? AND status = ? LIMIT 1`,
		string(workerID), candidatePath, string(workforce.ProposalPending))
	p, err := scanProposal(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrProposalNotFound
	}
	return p, err
}

const proposalSelect = `SELECT id, worker_id, candidate_path, suggested_project_id,
	candidate_metadata, status, proposed_at, reviewed_at, reviewed_by_identity_id,
	resulting_mapping_id, created_at, updated_at, version
	FROM worker_project_proposals`

func scanProposals(rows *sql.Rows) ([]*workforce.WorkerProjectProposal, error) {
	var out []*workforce.WorkerProjectProposal
	for rows.Next() {
		p, err := scanProposal(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func scanProposal(scan func(...any) error) (*workforce.WorkerProjectProposal, error) {
	var (
		id, workerID, candidatePath, suggestedProjectID string
		metaJSON                                        string
		status                                          string
		proposedAt                                      string
		reviewedAt                                      sql.NullString
		reviewedBy, resultingMappingID                  sql.NullString
		createdAt, updatedAt                            string
		version                                         int
	)
	if err := scan(&id, &workerID, &candidatePath, &suggestedProjectID,
		&metaJSON, &status, &proposedAt, &reviewedAt, &reviewedBy, &resultingMappingID,
		&createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	var meta workforce.CandidateMetadata
	if metaJSON != "" && metaJSON != "{}" {
		if err := json.Unmarshal([]byte(metaJSON), &meta); err != nil {
			return nil, fmt.Errorf("unmarshal candidate_metadata: %w", err)
		}
	}
	proposed, err := time.Parse(time.RFC3339Nano, proposedAt)
	if err != nil {
		return nil, fmt.Errorf("parse proposed_at: %w", err)
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, err
	}
	reviewed, err := parseNullTime(reviewedAt)
	if err != nil {
		return nil, err
	}
	return workforce.RehydrateWorkerProjectProposal(workforce.RehydrateProposalInput{
		ID:                   workforce.ProposalID(id),
		WorkerID:             workforce.WorkerID(workerID),
		CandidatePath:        candidatePath,
		SuggestedProjectID:   workforce.ProjectID(suggestedProjectID),
		CandidateMetadata:    meta,
		Status:               workforce.ProposalStatus(status),
		ProposedAt:           proposed,
		ReviewedAt:           reviewed,
		ReviewedByIdentityID: reviewedBy.String,
		ResultingMappingID:   workforce.MappingID(resultingMappingID.String),
		CreatedAt:            created,
		UpdatedAt:            updated,
		Version:              version,
	})
}
