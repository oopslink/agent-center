// Package sqlite implements the Workforce BC repositories backed by SQLite.
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

// WorkerRepo implements workforce.WorkerRepository.
type WorkerRepo struct {
	db *sql.DB
}

// NewWorkerRepo constructs the SQLite-backed WorkerRepository.
func NewWorkerRepo(db *sql.DB) *WorkerRepo {
	return &WorkerRepo{db: db}
}

// Save inserts (when version=1) or fails with ErrWorkerAlreadyExists.
// Updates that target existing rows go through UpdateStatus /
// UpdateLastHeartbeatAt — Save is for fresh insert.
func (r *WorkerRepo) Save(ctx context.Context, w *workforce.Worker) error {
	if w == nil {
		return errors.New("worker repo: nil worker")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	caps, err := w.CapabilitiesJSON()
	if err != nil {
		return fmt.Errorf("marshal capabilities: %w", err)
	}
	const stmt = `INSERT INTO workers (
		id, status, capabilities, last_heartbeat_at, working_seconds,
		enrolled_at, online_at, offline_at, offline_reason, offline_message,
		created_at, updated_at, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(w.ID()),
		string(w.Status()),
		string(caps),
		nullTimePtr(w.LastHeartbeatAt()),
		w.WorkingSeconds(),
		w.EnrolledAt().Format(time.RFC3339Nano),
		nullTimePtr(w.OnlineAt()),
		nullTimePtr(w.OfflineAt()),
		nullString(string(w.OfflineReason())),
		nullString(w.OfflineMessage()),
		w.CreatedAt().Format(time.RFC3339Nano),
		w.UpdatedAt().Format(time.RFC3339Nano),
		w.Version(),
	)
	if err != nil {
		if IsUniqueConstraint(err) {
			return workforce.ErrWorkerAlreadyExists
		}
		return err
	}
	return nil
}

// FindByID returns a Worker by id; ErrWorkerNotFound if absent.
func (r *WorkerRepo) FindByID(ctx context.Context, id workforce.WorkerID) (*workforce.Worker, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const stmt = workerSelect + ` WHERE id = ?`
	row := exec.QueryRowContext(ctx, stmt, string(id))
	w, err := scanWorker(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrWorkerNotFound
	}
	return w, err
}

// FindByStatus returns all workers with the given status.
func (r *WorkerRepo) FindByStatus(ctx context.Context, status workforce.WorkerStatus) ([]*workforce.Worker, error) {
	if !status.IsValid() {
		return nil, workforce.ErrWorkerInvalidStatus
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx, workerSelect+` WHERE status = ? ORDER BY id`, string(status))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkers(rows)
}

// FindAll lists all workers ordered by id.
func (r *WorkerRepo) FindAll(ctx context.Context) ([]*workforce.Worker, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx, workerSelect+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanWorkers(rows)
}

// UpdateStatus is the CAS path for state transitions
// (02-persistence § 4).
func (r *WorkerRepo) UpdateStatus(ctx context.Context, id workforce.WorkerID, from, to workforce.WorkerStatus, version int) error {
	if !from.IsValid() || !to.IsValid() {
		return workforce.ErrWorkerInvalidStatus
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	const stmt = `UPDATE workers
		SET status = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND status = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt, string(to), now, string(id), string(from), version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Disambiguate: is it not-found, or CAS conflict?
		return r.cassDiagnose(ctx, exec, id, workforce.ErrWorkerVersionConflict, workforce.ErrWorkerNotFound)
	}
	return nil
}

// UpdateLastHeartbeatAt is a non-CAS hot path (heartbeat is high frequency).
func (r *WorkerRepo) UpdateLastHeartbeatAt(ctx context.Context, id workforce.WorkerID, at time.Time, workingSeconds int64) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `UPDATE workers
		SET last_heartbeat_at = ?, working_seconds = ?, updated_at = ?
		WHERE id = ?`
	res, err := exec.ExecContext(ctx, stmt,
		at.UTC().Format(time.RFC3339Nano),
		workingSeconds,
		at.UTC().Format(time.RFC3339Nano),
		string(id),
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return workforce.ErrWorkerNotFound
	}
	return nil
}

func (r *WorkerRepo) cassDiagnose(ctx context.Context, exec persistence.SQLExecutor, id workforce.WorkerID, conflict, notFound error) error {
	var c int
	row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM workers WHERE id = ?`, string(id))
	if err := row.Scan(&c); err != nil {
		return err
	}
	if c == 0 {
		return notFound
	}
	return conflict
}

const workerSelect = `SELECT id, status, capabilities, last_heartbeat_at, working_seconds,
	enrolled_at, online_at, offline_at, offline_reason, offline_message,
	created_at, updated_at, version
	FROM workers`

func scanWorkers(rows *sql.Rows) ([]*workforce.Worker, error) {
	var out []*workforce.Worker
	for rows.Next() {
		w, err := scanWorker(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func scanWorker(scan func(...any) error) (*workforce.Worker, error) {
	var (
		id              string
		status          string
		capsJSON        string
		lastHeartbeatAt sql.NullString
		workingSeconds  int64
		enrolledAt      string
		onlineAt        sql.NullString
		offlineAt       sql.NullString
		offlineReason   sql.NullString
		offlineMessage  sql.NullString
		createdAt       string
		updatedAt       string
		version         int
	)
	if err := scan(&id, &status, &capsJSON, &lastHeartbeatAt, &workingSeconds,
		&enrolledAt, &onlineAt, &offlineAt, &offlineReason, &offlineMessage,
		&createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	var caps []string
	if capsJSON != "" {
		if err := json.Unmarshal([]byte(capsJSON), &caps); err != nil {
			return nil, fmt.Errorf("scan worker: capabilities: %w", err)
		}
	}
	enrolled, err := time.Parse(time.RFC3339Nano, enrolledAt)
	if err != nil {
		return nil, fmt.Errorf("scan worker: enrolled_at: %w", err)
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("scan worker: created_at: %w", err)
	}
	updated, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, fmt.Errorf("scan worker: updated_at: %w", err)
	}
	heartbeat, err := parseNullTime(lastHeartbeatAt)
	if err != nil {
		return nil, err
	}
	online, err := parseNullTime(onlineAt)
	if err != nil {
		return nil, err
	}
	offline, err := parseNullTime(offlineAt)
	if err != nil {
		return nil, err
	}
	return workforce.RehydrateWorker(workforce.RehydrateWorkerInput{
		ID:              workforce.WorkerID(id),
		Status:          workforce.WorkerStatus(status),
		Capabilities:    caps,
		LastHeartbeatAt: heartbeat,
		WorkingSeconds:  workingSeconds,
		EnrolledAt:      enrolled,
		OnlineAt:        online,
		OfflineAt:       offline,
		OfflineReason:   workforce.OfflineReason(offlineReason.String),
		OfflineMessage:  offlineMessage.String,
		CreatedAt:       created,
		UpdatedAt:       updated,
		Version:         version,
	})
}

// IsUniqueConstraint reports whether err is a SQLite UNIQUE failure.
func IsUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "constraint failed: UNIQUE")
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func parseNullTime(s sql.NullString) (*time.Time, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, s.String)
	if err != nil {
		return nil, fmt.Errorf("parse time %q: %w", s.String, err)
	}
	return &t, nil
}
