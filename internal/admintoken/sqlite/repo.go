// Package sqlite implements the admintoken Repository against SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/persistence"
)

// Repo is the SQLite-backed admintoken.Repository.
type Repo struct {
	db *sql.DB
}

// New constructs the repo.
func New(db *sql.DB) *Repo { return &Repo{db: db} }

const tokenSelect = `SELECT id, owner, scopes_json, value_hash,
		created_at, created_by, revoked_at, revoked_by, revoked_reason,
		last_used_at, version, is_enroll, expires_at, used_at
	FROM admin_tokens`

// Save inserts a new row.
func (r *Repo) Save(ctx context.Context, t *admintoken.AdminToken) error {
	if t == nil {
		return errors.New("admin token repo: nil token")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	scopesJSON, err := encodeScopes(t.Scopes())
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO admin_tokens
		(id, owner, scopes_json, value_hash, created_at, created_by,
		 revoked_at, revoked_by, revoked_reason, last_used_at, version,
		 is_enroll, expires_at, used_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	isEnrollInt := 0
	if t.IsEnroll() {
		isEnrollInt = 1
	}
	if _, err := exec.ExecContext(ctx, stmt,
		string(t.ID()), string(t.Owner()), scopesJSON, t.ValueHash(),
		t.CreatedAt().Format(time.RFC3339Nano), t.CreatedBy(),
		nullTimePtr(t.RevokedAt()), nullString(t.RevokedBy()), nullString(t.RevokedReason()),
		nullTimePtr(t.LastUsedAt()), t.Version(),
		isEnrollInt, nullTimePtr(t.ExpiresAt()), nullTimePtr(t.UsedAt()),
	); err != nil {
		if isUnique(err) {
			return admintoken.ErrTokenAlreadyExists
		}
		return err
	}
	return nil
}

// FindByID returns a row by PK.
func (r *Repo) FindByID(ctx context.Context, id admintoken.TokenID) (*admintoken.AdminToken, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, tokenSelect+` WHERE id = ?`, string(id))
	t, err := scanToken(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, admintoken.ErrTokenNotFound
	}
	return t, err
}

// FindByHash looks up via the value_hash unique index.
func (r *Repo) FindByHash(ctx context.Context, valueHash []byte) (*admintoken.AdminToken, error) {
	if len(valueHash) == 0 {
		return nil, admintoken.ErrTokenNotFound
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, tokenSelect+` WHERE value_hash = ?`, valueHash)
	t, err := scanToken(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, admintoken.ErrTokenNotFound
	}
	return t, err
}

// FindAll returns every row, ordered created_at desc.
func (r *Repo) FindAll(ctx context.Context) ([]*admintoken.AdminToken, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, tokenSelect+` ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*admintoken.AdminToken, 0)
	for rows.Next() {
		t, err := scanToken(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// FindByOwner returns tokens owned by a single principal.
func (r *Repo) FindByOwner(ctx context.Context, owner admintoken.Owner) ([]*admintoken.AdminToken, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, tokenSelect+` WHERE owner = ? ORDER BY created_at DESC`, string(owner))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]*admintoken.AdminToken, 0)
	for rows.Next() {
		t, err := scanToken(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Revoke writes the revoked fields with CAS on version.
func (r *Repo) Revoke(ctx context.Context, id admintoken.TokenID, by, reason string, expectedVersion int) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `UPDATE admin_tokens
		SET revoked_at = ?, revoked_by = ?, revoked_reason = ?, version = version + 1
		WHERE id = ? AND version = ? AND revoked_at IS NULL`
	res, err := exec.ExecContext(ctx, stmt,
		time.Now().UTC().Format(time.RFC3339Nano),
		nullString(by), nullString(reason),
		string(id), expectedVersion,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	// Differentiate between "not found", "already revoked", and "version
	// drift" with a precise re-read.
	row := exec.QueryRowContext(ctx, `SELECT version, revoked_at FROM admin_tokens WHERE id = ?`, string(id))
	var v int
	var rev sql.NullString
	if err := row.Scan(&v, &rev); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return admintoken.ErrTokenNotFound
		}
		return err
	}
	if rev.Valid && rev.String != "" {
		return admintoken.ErrTokenRevoked
	}
	return admintoken.ErrTokenVersionConflict
}

// UpdateLastUsedAt is best-effort. We never block the calling path on
// failure — middleware swallows the error.
func (r *Repo) UpdateLastUsedAt(ctx context.Context, id admintoken.TokenID, atRFC3339Nano string) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`UPDATE admin_tokens SET last_used_at = ? WHERE id = ?`,
		atRFC3339Nano, string(id),
	)
	return err
}

// ConsumeEnrollToken atomically marks an enroll token as used. Returns
// ErrTokenConsumed if already consumed (CAS via used_at IS NULL guard),
// or ErrTokenNotFound if no row matches. v2.4-D-A3 (task #37).
func (r *Repo) ConsumeEnrollToken(ctx context.Context, id admintoken.TokenID, atRFC3339Nano string) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `UPDATE admin_tokens
		SET used_at = ?, version = version + 1
		WHERE id = ? AND is_enroll = 1 AND used_at IS NULL`
	res, err := exec.ExecContext(ctx, stmt, atRFC3339Nano, string(id))
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	// 0 rows affected — distinguish "not found" vs "already consumed".
	row := exec.QueryRowContext(ctx, `SELECT is_enroll, used_at FROM admin_tokens WHERE id = ?`, string(id))
	var isEnroll int
	var usedAt sql.NullString
	if err := row.Scan(&isEnroll, &usedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return admintoken.ErrTokenNotFound
		}
		return err
	}
	if isEnroll == 0 {
		// Not an enroll token at all — odd, but treat as not-found
		// from this method's contract (callers shouldn't call Consume
		// on non-enroll tokens).
		return admintoken.ErrTokenNotFound
	}
	if usedAt.Valid && usedAt.String != "" {
		return admintoken.ErrTokenConsumed
	}
	return admintoken.ErrTokenVersionConflict
}

// =============================================================================
// helpers
// =============================================================================

func encodeScopes(scopes []admintoken.Scope) (string, error) {
	if len(scopes) == 0 {
		return "[]", nil
	}
	strs := make([]string, len(scopes))
	for i, s := range scopes {
		strs[i] = string(s)
	}
	b, err := json.Marshal(strs)
	if err != nil {
		return "", fmt.Errorf("admin token repo: encode scopes: %w", err)
	}
	return string(b), nil
}

func decodeScopes(s string) ([]admintoken.Scope, error) {
	if s == "" {
		return nil, nil
	}
	var raw []string
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("admin token repo: decode scopes: %w", err)
	}
	out := make([]admintoken.Scope, len(raw))
	for i, x := range raw {
		out[i] = admintoken.Scope(x)
	}
	return out, nil
}

type scanFn func(...any) error

func scanToken(scan scanFn) (*admintoken.AdminToken, error) {
	var (
		id, owner, scopesJSON, createdAt, createdBy string
		valueHash                                   []byte
		revokedAt, revokedBy, revokedReason         sql.NullString
		lastUsedAt                                  sql.NullString
		version                                     int
		isEnrollInt                                 int
		expiresAt, usedAt                           sql.NullString
	)
	if err := scan(&id, &owner, &scopesJSON, &valueHash,
		&createdAt, &createdBy, &revokedAt, &revokedBy, &revokedReason,
		&lastUsedAt, &version, &isEnrollInt, &expiresAt, &usedAt); err != nil {
		return nil, err
	}
	scopes, err := decodeScopes(scopesJSON)
	if err != nil {
		return nil, err
	}
	created, err := parseTime(createdAt)
	if err != nil {
		return nil, err
	}
	revoked, err := parseNullTime(revokedAt)
	if err != nil {
		return nil, err
	}
	used, err := parseNullTime(lastUsedAt)
	if err != nil {
		return nil, err
	}
	expires, err := parseNullTime(expiresAt)
	if err != nil {
		return nil, err
	}
	usedAtT, err := parseNullTime(usedAt)
	if err != nil {
		return nil, err
	}
	return admintoken.Rehydrate(admintoken.RehydrateInput{
		ID:            admintoken.TokenID(id),
		Owner:         admintoken.Owner(owner),
		Scopes:        scopes,
		ValueHash:     valueHash,
		CreatedAt:     created,
		CreatedBy:     createdBy,
		RevokedAt:     revoked,
		RevokedBy:     revokedBy.String,
		RevokedReason: revokedReason.String,
		LastUsedAt:    used,
		Version:       version,
		IsEnroll:      isEnrollInt != 0,
		ExpiresAt:     expires,
		UsedAt:        usedAtT,
	}), nil
}

func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
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

func isUnique(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}
