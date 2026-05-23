// Package sqlite implements the SecretManagement repositories backed by SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/secretmgmt"
)

// UserSecretRepo is the SQLite-backed UserSecretRepository.
type UserSecretRepo struct {
	db *sql.DB
}

// NewUserSecretRepo constructs the repo.
func NewUserSecretRepo(db *sql.DB) *UserSecretRepo {
	return &UserSecretRepo{db: db}
}

const userSecretSelect = `SELECT id, name, kind, value_ciphertext, value_nonce, state,
	created_at, created_by, last_used_at, rotated_at, revoked_at, revoked_by, revoked_reason, revoked_message, version
	FROM user_secrets`

// Save inserts a fresh row.
func (r *UserSecretRepo) Save(ctx context.Context, s *secretmgmt.UserSecret) error {
	if s == nil {
		return errors.New("user secret repo: nil secret")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `INSERT INTO user_secrets (
		id, name, kind, value_ciphertext, value_nonce, state,
		created_at, created_by, last_used_at, rotated_at, revoked_at, revoked_by, revoked_reason, revoked_message, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := exec.ExecContext(ctx, stmt,
		string(s.ID()),
		s.Name(),
		string(s.Kind()),
		s.Ciphertext(),
		s.Nonce(),
		string(s.State()),
		s.CreatedAt().Format(time.RFC3339Nano),
		s.CreatedBy(),
		nullTimePtr(s.LastUsedAt()),
		nullTimePtr(s.RotatedAt()),
		nullTimePtr(s.RevokedAt()),
		nullString(s.RevokedBy()),
		nullString(string(s.RevokedReason())),
		nullString(s.RevokedMessage()),
		s.Version(),
	)
	if err != nil {
		if isUnique(err) {
			msg := err.Error()
			if strings.Contains(msg, "user_secrets.name") {
				return secretmgmt.ErrUserSecretNameTaken
			}
			return secretmgmt.ErrUserSecretAlreadyExists
		}
		return err
	}
	return nil
}

// FindByID returns a secret by PK.
func (r *UserSecretRepo) FindByID(ctx context.Context, id secretmgmt.UserSecretID) (*secretmgmt.UserSecret, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, userSecretSelect+` WHERE id = ?`, string(id))
	s, err := scanUserSecret(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, secretmgmt.ErrUserSecretNotFound
	}
	return s, err
}

// FindByName returns a secret by globally unique name.
func (r *UserSecretRepo) FindByName(ctx context.Context, name string) (*secretmgmt.UserSecret, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, userSecretSelect+` WHERE name = ?`, name)
	s, err := scanUserSecret(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, secretmgmt.ErrUserSecretNotFound
	}
	return s, err
}

// FindAll lists with optional kind / state filters.
func (r *UserSecretRepo) FindAll(ctx context.Context, filter secretmgmt.UserSecretFilter) ([]*secretmgmt.UserSecret, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	q := userSecretSelect + ` WHERE 1=1`
	args := []any{}
	if filter.Kind != nil {
		q += ` AND kind = ?`
		args = append(args, string(*filter.Kind))
	}
	if filter.State != nil {
		q += ` AND state = ?`
		args = append(args, string(*filter.State))
	}
	q += ` ORDER BY created_at ASC`
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*secretmgmt.UserSecret
	for rows.Next() {
		s, err := scanUserSecret(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// UpdateValue — CAS replace of ciphertext + nonce + rotated_at.
func (r *UserSecretRepo) UpdateValue(ctx context.Context, id secretmgmt.UserSecretID, ciphertext, nonce []byte, rotatedAt time.Time, version int) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `UPDATE user_secrets
		SET value_ciphertext = ?, value_nonce = ?, rotated_at = ?, version = version + 1
		WHERE id = ? AND version = ? AND state = 'active'`
	res, err := exec.ExecContext(ctx, stmt,
		ciphertext, nonce, rotatedAt.UTC().Format(time.RFC3339Nano),
		string(id), version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return diagnoseUserSecretUpdate(ctx, exec, id, version)
	}
	return nil
}

// UpdateState — CAS transition (active → revoked).
func (r *UserSecretRepo) UpdateState(ctx context.Context, id secretmgmt.UserSecretID, from, to secretmgmt.UserSecretState, at time.Time, by string, reason secretmgmt.UserSecretRevokedReason, message string, version int) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `UPDATE user_secrets
		SET state = ?, revoked_at = ?, revoked_by = ?, revoked_reason = ?, revoked_message = ?, version = version + 1
		WHERE id = ? AND state = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		string(to),
		at.UTC().Format(time.RFC3339Nano),
		by,
		string(reason),
		message,
		string(id), string(from), version,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return diagnoseUserSecretUpdate(ctx, exec, id, version)
	}
	return nil
}

// UpdateLastUsedAt — non-CAS hot path.
func (r *UserSecretRepo) UpdateLastUsedAt(ctx context.Context, id secretmgmt.UserSecretID, at time.Time) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `UPDATE user_secrets SET last_used_at = ? WHERE id = ?`
	res, err := exec.ExecContext(ctx, stmt, at.UTC().Format(time.RFC3339Nano), string(id))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return secretmgmt.ErrUserSecretNotFound
	}
	return nil
}

func diagnoseUserSecretUpdate(ctx context.Context, exec persistence.SQLExecutor, id secretmgmt.UserSecretID, version int) error {
	row := exec.QueryRowContext(ctx, `SELECT state, version FROM user_secrets WHERE id = ?`, string(id))
	var (
		st  string
		ver int
	)
	if err := row.Scan(&st, &ver); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return secretmgmt.ErrUserSecretNotFound
		}
		return err
	}
	if secretmgmt.UserSecretState(st) == secretmgmt.UserSecretRevoked {
		return secretmgmt.ErrUserSecretRevoked
	}
	if ver != version {
		return secretmgmt.ErrUserSecretVersionConflict
	}
	return fmt.Errorf("secretmgmt: update no-op for id=%s state=%s version=%d", id, st, ver)
}

func scanUserSecret(scan func(...any) error) (*secretmgmt.UserSecret, error) {
	var (
		id             string
		name           string
		kind           string
		ciphertext     []byte
		nonce          []byte
		state          string
		createdAt      string
		createdBy      string
		lastUsedAt     sql.NullString
		rotatedAt      sql.NullString
		revokedAt      sql.NullString
		revokedBy      sql.NullString
		revokedReason  sql.NullString
		revokedMessage sql.NullString
		version        int
	)
	if err := scan(&id, &name, &kind, &ciphertext, &nonce, &state,
		&createdAt, &createdBy, &lastUsedAt, &rotatedAt, &revokedAt, &revokedBy, &revokedReason, &revokedMessage, &version); err != nil {
		return nil, err
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("scan user secret: created_at: %w", err)
	}
	lastUsed, err := parseNullTime(lastUsedAt)
	if err != nil {
		return nil, err
	}
	rotated, err := parseNullTime(rotatedAt)
	if err != nil {
		return nil, err
	}
	revoked, err := parseNullTime(revokedAt)
	if err != nil {
		return nil, err
	}
	return secretmgmt.RehydrateUserSecret(secretmgmt.RehydrateUserSecretInput{
		ID:             secretmgmt.UserSecretID(id),
		Name:           name,
		Kind:           secretmgmt.UserSecretKind(kind),
		Ciphertext:     ciphertext,
		Nonce:          nonce,
		State:          secretmgmt.UserSecretState(state),
		CreatedAt:      created,
		CreatedBy:      createdBy,
		LastUsedAt:     lastUsed,
		RotatedAt:      rotated,
		RevokedAt:      revoked,
		RevokedBy:      revokedBy.String,
		RevokedReason:  secretmgmt.UserSecretRevokedReason(revokedReason.String),
		RevokedMessage: revokedMessage.String,
		Version:        version,
	})
}

func isUnique(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed")
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
		return nil, err
	}
	return &t, nil
}
