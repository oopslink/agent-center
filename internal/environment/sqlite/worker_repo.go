// Package sqlite implements the Environment BC repositories (v2.7 D1,
// ADR-0050). Tables (env_workers, worker_control_events) land in migration
// 0044. This BC stands ALONGSIDE the legacy workforce.Worker until the D2
// cutover (#107).
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	env "github.com/oopslink/agent-center/internal/environment"
	"github.com/oopslink/agent-center/internal/persistence"
)

// --- shared helpers ---------------------------------------------------------

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(s string) time.Time {
	if strings.TrimSpace(s) == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// isUnique reports whether err is a sqlite UNIQUE-constraint violation.
func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}

// --- WorkerRepo -------------------------------------------------------------

// WorkerRepo implements env.WorkerRepository.
type WorkerRepo struct{ db *sql.DB }

// NewWorkerRepo constructs the repo.
func NewWorkerRepo(db *sql.DB) *WorkerRepo { return &WorkerRepo{db: db} }

func (r *WorkerRepo) Save(ctx context.Context, w *env.Worker) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO env_workers
		 (id, organization_id, name, status, last_acked_offset, last_heartbeat_at, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		string(w.ID()), w.OrganizationID(), nullString(w.Name()), string(w.Status()),
		w.LastAckedOffset(), nullString(ts(w.LastHeartbeatAt())),
		ts(w.CreatedAt()), ts(w.UpdatedAt()), w.Version())
	if isUnique(err) {
		return env.ErrWorkerExists
	}
	return err
}

func (r *WorkerRepo) Update(ctx context.Context, w *env.Worker) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE env_workers SET name=?, status=?, last_acked_offset=?, last_heartbeat_at=?, updated_at=?, version=?
		 WHERE id=?`,
		nullString(w.Name()), string(w.Status()), w.LastAckedOffset(),
		nullString(ts(w.LastHeartbeatAt())), ts(w.UpdatedAt()), w.Version(), string(w.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return env.ErrWorkerNotFound
	}
	return nil
}

func (r *WorkerRepo) FindByID(ctx context.Context, id env.WorkerID) (*env.Worker, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, workerSelect+` WHERE id = ?`, string(id))
	w, err := scanWorker(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, env.ErrWorkerNotFound
	}
	return w, err
}

func (r *WorkerRepo) ListByOrg(ctx context.Context, orgID string) ([]*env.Worker, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, workerSelect+` WHERE organization_id = ? ORDER BY created_at, id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*env.Worker
	for rows.Next() {
		w, err := scanWorker(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

const workerSelect = `SELECT id, organization_id, name, status, last_acked_offset, last_heartbeat_at, created_at, updated_at, version FROM env_workers`

func scanWorker(scan func(...any) error) (*env.Worker, error) {
	var (
		id, org, status, createdAt, updatedAt string
		name, lastHeartbeatAt                 sql.NullString
		lastAckedOffset                       int64
		version                               int
	)
	if err := scan(&id, &org, &name, &status, &lastAckedOffset, &lastHeartbeatAt, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return env.RehydrateWorker(env.RehydrateWorkerInput{
		ID: env.WorkerID(id), OrganizationID: org, Name: name.String,
		Status: env.WorkerStatus(status), LastAckedOffset: lastAckedOffset,
		LastHeartbeatAt: parseTime(lastHeartbeatAt.String),
		CreatedAt:       parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
	})
}

var _ env.WorkerRepository = (*WorkerRepo)(nil)
