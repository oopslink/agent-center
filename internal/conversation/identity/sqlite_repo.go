package identity

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

// SQLiteIdentityRepo implements IdentityRepository on SQLite (v2 — channel
// bindings dropped per ADR-0031 / ADR-0033).
type SQLiteIdentityRepo struct {
	db *sql.DB
}

// NewSQLiteIdentityRepo constructs the repo.
func NewSQLiteIdentityRepo(db *sql.DB) *SQLiteIdentityRepo {
	return &SQLiteIdentityRepo{db: db}
}

// Save inserts a new identity row; duplicate id → ErrIdentityAlreadyExists.
func (r *SQLiteIdentityRepo) Save(ctx context.Context, i *Identity) error {
	if i == nil {
		return errors.New("identity repo: nil identity")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO identities (id, kind, display_name, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(i.ID()), string(i.Kind()), i.DisplayName(),
		i.CreatedAt().Format(time.RFC3339Nano),
		i.UpdatedAt().Format(time.RFC3339Nano),
		i.Version(),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrIdentityAlreadyExists
		}
		return err
	}
	return nil
}

// Update CAS-updates an existing identity by version (display_name only).
func (r *SQLiteIdentityRepo) Update(ctx context.Context, i *Identity, expectedVersion int) error {
	if i == nil {
		return errors.New("identity repo: nil identity")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `UPDATE identities
		SET display_name = ?, updated_at = ?, version = version + 1
		WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		i.DisplayName(), i.UpdatedAt().Format(time.RFC3339Nano),
		string(i.ID()), expectedVersion,
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		var c int
		row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM identities WHERE id = ?`, string(i.ID()))
		if err := row.Scan(&c); err != nil {
			return err
		}
		if c == 0 {
			return ErrIdentityNotFound
		}
		return ErrIdentityVersionConflict
	}
	return nil
}

// FindByID returns the identity, or ErrIdentityNotFound.
func (r *SQLiteIdentityRepo) FindByID(ctx context.Context, id IdentityID) (*Identity, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, identitySelect+` WHERE id = ?`, string(id))
	i, err := scanIdentity(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrIdentityNotFound
	}
	return i, err
}

// Find returns identities matching filter ordered by id ASC.
func (r *SQLiteIdentityRepo) Find(ctx context.Context, filter IdentityFilter) ([]*Identity, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	sb := strings.Builder{}
	sb.WriteString(identitySelect)
	sb.WriteString(` WHERE 1=1`)
	var args []any
	if filter.Kind != nil {
		sb.WriteString(` AND kind = ?`)
		args = append(args, string(*filter.Kind))
	}
	if filter.Cursor != nil && *filter.Cursor != "" {
		sb.WriteString(` AND id > ?`)
		args = append(args, string(*filter.Cursor))
	}
	limit := filter.Limit
	if limit <= 0 {
		limit = DefaultIdentityLimit
	}
	sb.WriteString(` ORDER BY id ASC LIMIT ?`)
	args = append(args, limit)
	rows, err := exec.QueryContext(ctx, sb.String(), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Identity
	for rows.Next() {
		i, err := scanIdentity(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}

const identitySelect = `SELECT id, kind, display_name, created_at, updated_at, version FROM identities`

func scanIdentity(scan func(...any) error) (*Identity, error) {
	var (
		id, kind, displayName string
		createdAt, updatedAt  string
		version               int
	)
	if err := scan(&id, &kind, &displayName, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	ct, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	ut, err := time.Parse(time.RFC3339Nano, updatedAt)
	if err != nil {
		return nil, err
	}
	return RehydrateIdentity(RehydrateIdentityInput{
		ID:          IdentityID(id),
		Kind:        Kind(kind),
		DisplayName: displayName,
		CreatedAt:   ct,
		UpdatedAt:   ut,
		Version:     version,
	})
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "constraint failed: UNIQUE") ||
		strings.Contains(err.Error(), "constraint failed: identities.id")
}
