package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// MappingRepo implements workforce.WorkerProjectMappingRepository.
type MappingRepo struct {
	db *sql.DB
}

// NewMappingRepo constructs the repository.
func NewMappingRepo(db *sql.DB) *MappingRepo {
	return &MappingRepo{db: db}
}

// Save inserts a new mapping. Unique active constraint on (worker_id,
// project_id) means re-saving an active mapping for the same pair returns
// ErrMappingAlreadyActive.
func (r *MappingRepo) Save(ctx context.Context, m *workforce.WorkerProjectMapping) error {
	if m == nil {
		return errors.New("mapping repo: nil mapping")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `INSERT INTO worker_project_mappings (
		id, worker_id, project_id, base_path, source_proposal_id, status,
		invalidate_reason, invalidate_message, added_at, invalidated_at,
		created_at, updated_at, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := exec.ExecContext(ctx, stmt,
		string(m.ID()),
		string(m.WorkerID()),
		string(m.ProjectID()),
		m.BasePath(),
		nullString(string(m.SourceProposalID())),
		string(m.Status()),
		nullString(string(m.InvalidateReason())),
		nullString(m.InvalidateMessage()),
		m.AddedAt().Format(time.RFC3339Nano),
		nullTimePtr(m.InvalidatedAt()),
		m.CreatedAt().Format(time.RFC3339Nano),
		m.UpdatedAt().Format(time.RFC3339Nano),
		m.Version(),
	)
	if err != nil {
		if IsUniqueConstraint(err) {
			// Could be id-clash or active-pair-clash; both → ErrMappingAlreadyActive
			// (caller treats them the same: stop, don't continue).
			return workforce.ErrMappingAlreadyActive
		}
		return err
	}
	return nil
}

// FindByID returns a mapping; ErrMappingNotFound if absent.
func (r *MappingRepo) FindByID(ctx context.Context, id workforce.MappingID) (*workforce.WorkerProjectMapping, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, mappingSelect+` WHERE id = ?`, string(id))
	m, err := scanMapping(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrMappingNotFound
	}
	return m, err
}

// FindByWorkerID lists all mappings for a worker.
func (r *MappingRepo) FindByWorkerID(ctx context.Context, workerID workforce.WorkerID) ([]*workforce.WorkerProjectMapping, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, mappingSelect+` WHERE worker_id = ? ORDER BY added_at`, string(workerID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMappings(rows)
}

// FindByProjectID lists all mappings for a project.
func (r *MappingRepo) FindByProjectID(ctx context.Context, projectID workforce.ProjectID) ([]*workforce.WorkerProjectMapping, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, mappingSelect+` WHERE project_id = ? ORDER BY added_at`, string(projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanMappings(rows)
}

// FindByWorkerAndProject returns the **active** mapping for the pair, or
// ErrMappingNotFound. Invalidated mappings are not returned by this lookup
// (callers needing them use FindByWorkerID + filter).
func (r *MappingRepo) FindByWorkerAndProject(ctx context.Context, workerID workforce.WorkerID, projectID workforce.ProjectID) (*workforce.WorkerProjectMapping, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		mappingSelect+` WHERE worker_id = ? AND project_id = ? AND status = ? LIMIT 1`,
		string(workerID), string(projectID), string(workforce.MappingActive))
	m, err := scanMapping(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrMappingNotFound
	}
	return m, err
}

// Invalidate transitions mapping to invalidated state.
func (r *MappingRepo) Invalidate(ctx context.Context, id workforce.MappingID, reason workforce.InvalidateReason, message string, at time.Time) error {
	if !reason.IsValid() {
		return fmt.Errorf("mapping repo: invalid reason %q", reason)
	}
	if message == "" {
		return errors.New("mapping repo: message required")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	now := at.UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE worker_project_mappings
		SET status = ?, invalidate_reason = ?, invalidate_message = ?,
		    invalidated_at = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND status = ?`
	res, err := exec.ExecContext(ctx, stmt,
		string(workforce.MappingInvalidated), string(reason), message,
		now, now, string(id), string(workforce.MappingActive))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		// Disambiguate not-found vs already-inactive.
		var count int
		row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM worker_project_mappings WHERE id = ?`, string(id))
		if err := row.Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			return workforce.ErrMappingNotFound
		}
		return workforce.ErrMappingNotActive
	}
	return nil
}

// CountActiveByProjectID is used by Project deletion (workforce/02 § 5.4)
// to enforce "no active mapping" precondition.
func (r *MappingRepo) CountActiveByProjectID(ctx context.Context, projectID workforce.ProjectID) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM worker_project_mappings WHERE project_id = ? AND status = ?`,
		string(projectID), string(workforce.MappingActive))
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

const mappingSelect = `SELECT id, worker_id, project_id, base_path, source_proposal_id, status,
	invalidate_reason, invalidate_message, added_at, invalidated_at,
	created_at, updated_at, version
	FROM worker_project_mappings`

func scanMappings(rows *sql.Rows) ([]*workforce.WorkerProjectMapping, error) {
	var out []*workforce.WorkerProjectMapping
	for rows.Next() {
		m, err := scanMapping(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func scanMapping(scan func(...any) error) (*workforce.WorkerProjectMapping, error) {
	var (
		id, workerID, projectID, basePath, status string
		sourceProposalID                          sql.NullString
		invalReason, invalMessage                 sql.NullString
		addedAt                                   string
		invalidatedAt                             sql.NullString
		createdAt, updatedAt                      string
		version                                   int
	)
	if err := scan(&id, &workerID, &projectID, &basePath, &sourceProposalID, &status,
		&invalReason, &invalMessage, &addedAt, &invalidatedAt, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	added, err := time.Parse(time.RFC3339Nano, addedAt)
	if err != nil {
		return nil, fmt.Errorf("parse added_at: %w", err)
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, err
	}
	invalAt, err := parseNullTime(invalidatedAt)
	if err != nil {
		return nil, err
	}
	return workforce.RehydrateWorkerProjectMapping(workforce.RehydrateMappingInput{
		ID:                workforce.MappingID(id),
		WorkerID:          workforce.WorkerID(workerID),
		ProjectID:         workforce.ProjectID(projectID),
		BasePath:          basePath,
		SourceProposalID:  workforce.ProposalID(sourceProposalID.String),
		Status:            workforce.MappingStatus(status),
		InvalidateReason:  workforce.InvalidateReason(invalReason.String),
		InvalidateMessage: invalMessage.String,
		AddedAt:           added,
		InvalidatedAt:     invalAt,
		CreatedAt:         created,
		UpdatedAt:         updated,
		Version:           version,
	})
}
