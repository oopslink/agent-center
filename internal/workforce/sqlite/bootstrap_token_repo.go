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

// BootstrapTokenRepo is the SQLite implementation of
// workforce.BootstrapTokenRepository (ADR-0023 § 2).
type BootstrapTokenRepo struct {
	db *sql.DB
}

// NewBootstrapTokenRepo constructs the repository.
func NewBootstrapTokenRepo(db *sql.DB) *BootstrapTokenRepo {
	return &BootstrapTokenRepo{db: db}
}

const bootstrapTokenSelect = `SELECT id, worker_id, value_hash, status,
	created_at, expires_at, used_at, revoked_at, revoked_reason, revoked_message, created_by
	FROM bootstrap_tokens`

// Save inserts a freshly-issued token. Returns ErrBootstrapTokenAlreadyExists
// on PK conflict, ErrBootstrapTokenValueHashConflict on value_hash collision,
// ErrBootstrapTokenActiveExists on `unique active per worker` violation.
func (r *BootstrapTokenRepo) Save(ctx context.Context, t *workforce.BootstrapToken) error {
	if t == nil {
		return errors.New("bootstrap token repo: nil token")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO bootstrap_tokens (
		id, worker_id, value_hash, status,
		created_at, expires_at, used_at, revoked_at, revoked_reason, revoked_message, created_by
	) VALUES (?,?,?,?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		string(t.ID()),
		string(t.WorkerID()),
		t.ValueHash(),
		string(t.Status()),
		t.CreatedAt().Format(time.RFC3339Nano),
		t.ExpiresAt().Format(time.RFC3339Nano),
		nullTimePtr(t.UsedAt()),
		nullTimePtr(t.RevokedAt()),
		nullString(string(t.RevokedReason())),
		nullString(t.RevokedMessage()),
		t.CreatedBy(),
	)
	if err != nil {
		if IsUniqueConstraint(err) {
			// SQLite reports the failing constraint as `table.column`; we
			// match by column-name suffix to disambiguate.
			msg := err.Error()
			switch {
			case containsAny(msg, "bootstrap_tokens.value_hash"):
				return workforce.ErrBootstrapTokenValueHashConflict
			case containsAny(msg, "bootstrap_tokens.worker_id"):
				// only the partial unique index `WHERE status='active'`
				// constrains worker_id.
				return workforce.ErrBootstrapTokenActiveExists
			case containsAny(msg, "bootstrap_tokens.id"):
				return workforce.ErrBootstrapTokenAlreadyExists
			default:
				return workforce.ErrBootstrapTokenAlreadyExists
			}
		}
		return err
	}
	return nil
}

// FindByID returns a token by PK.
func (r *BootstrapTokenRepo) FindByID(ctx context.Context, id workforce.BootstrapTokenID) (*workforce.BootstrapToken, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, bootstrapTokenSelect+` WHERE id = ?`, string(id))
	t, err := scanBootstrapToken(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrBootstrapTokenNotFound
	}
	return t, err
}

// FindByValueHash is the exchange-path lookup.
func (r *BootstrapTokenRepo) FindByValueHash(ctx context.Context, hash string) (*workforce.BootstrapToken, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, bootstrapTokenSelect+` WHERE value_hash = ?`, hash)
	t, err := scanBootstrapToken(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrBootstrapTokenNotFound
	}
	return t, err
}

// FindByWorkerID returns tokens by worker, optionally filtered by status.
func (r *BootstrapTokenRepo) FindByWorkerID(ctx context.Context, workerID workforce.WorkerID, statuses ...workforce.BootstrapTokenStatus) ([]*workforce.BootstrapToken, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	q := bootstrapTokenSelect + ` WHERE worker_id = ?`
	args := []any{string(workerID)}
	if len(statuses) > 0 {
		placeholders := ""
		for i, s := range statuses {
			if i > 0 {
				placeholders += ","
			}
			placeholders += "?"
			args = append(args, string(s))
		}
		q += ` AND status IN (` + placeholders + `)`
	}
	q += ` ORDER BY created_at DESC`
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBootstrapTokens(rows)
}

// FindActiveByWorkerForUpdate is the reissue-path concurrent guard. SQLite's
// transaction isolation already serialises writes; in a future Postgres
// backend this would emit `FOR UPDATE`. For SQLite we rely on the unique
// partial index `uniq_bootstrap_tokens_active_per_worker` as the ultimate
// guard against concurrent reissues.
func (r *BootstrapTokenRepo) FindActiveByWorkerForUpdate(ctx context.Context, workerID workforce.WorkerID) (*workforce.BootstrapToken, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx,
		bootstrapTokenSelect+` WHERE worker_id = ? AND status = 'active'`,
		string(workerID))
	t, err := scanBootstrapToken(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrBootstrapTokenNotFound
	}
	return t, err
}

// UpdateStatus persists a state transition using `from` as pre-image guard.
func (r *BootstrapTokenRepo) UpdateStatus(ctx context.Context, t *workforce.BootstrapToken, from workforce.BootstrapTokenStatus) error {
	if t == nil {
		return errors.New("bootstrap token repo: nil token")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `UPDATE bootstrap_tokens
		SET status = ?, used_at = ?, revoked_at = ?, revoked_reason = ?, revoked_message = ?
		WHERE id = ? AND status = ?`
	res, err := exec.ExecContext(ctx, stmt,
		string(t.Status()),
		nullTimePtr(t.UsedAt()),
		nullTimePtr(t.RevokedAt()),
		nullString(string(t.RevokedReason())),
		nullString(t.RevokedMessage()),
		string(t.ID()),
		string(from),
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Disambiguate not-found vs status mismatch.
		var c int
		if scanErr := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM bootstrap_tokens WHERE id = ?`, string(t.ID())).Scan(&c); scanErr != nil {
			return scanErr
		}
		if c == 0 {
			return workforce.ErrBootstrapTokenNotFound
		}
		return workforce.ErrBootstrapTokenStatusConflict
	}
	return nil
}

// FindExpired returns active tokens past TTL (scanner path).
func (r *BootstrapTokenRepo) FindExpired(ctx context.Context, before time.Time) ([]*workforce.BootstrapToken, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx,
		bootstrapTokenSelect+` WHERE status = 'active' AND expires_at <= ?`,
		before.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanBootstrapTokens(rows)
}

func scanBootstrapTokens(rows *sql.Rows) ([]*workforce.BootstrapToken, error) {
	var out []*workforce.BootstrapToken
	for rows.Next() {
		t, err := scanBootstrapToken(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func scanBootstrapToken(scan func(...any) error) (*workforce.BootstrapToken, error) {
	var (
		id             string
		workerID       string
		valueHash      string
		status         string
		createdAt      string
		expiresAt      string
		usedAt         sql.NullString
		revokedAt      sql.NullString
		revokedReason  sql.NullString
		revokedMessage sql.NullString
		createdBy      string
	)
	if err := scan(&id, &workerID, &valueHash, &status,
		&createdAt, &expiresAt, &usedAt, &revokedAt, &revokedReason, &revokedMessage, &createdBy); err != nil {
		return nil, err
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("scan bootstrap token: created_at: %w", err)
	}
	expires, err := time.Parse(time.RFC3339Nano, expiresAt)
	if err != nil {
		return nil, fmt.Errorf("scan bootstrap token: expires_at: %w", err)
	}
	used, err := parseNullTime(usedAt)
	if err != nil {
		return nil, err
	}
	revoked, err := parseNullTime(revokedAt)
	if err != nil {
		return nil, err
	}
	return workforce.RehydrateBootstrapToken(workforce.RehydrateBootstrapTokenInput{
		ID:             workforce.BootstrapTokenID(id),
		WorkerID:       workforce.WorkerID(workerID),
		ValueHash:      valueHash,
		Status:         workforce.BootstrapTokenStatus(status),
		CreatedAt:      created,
		ExpiresAt:      expires,
		UsedAt:         used,
		RevokedAt:      revoked,
		RevokedReason:  workforce.BootstrapTokenRevokedReason(revokedReason.String),
		RevokedMessage: revokedMessage.String,
		CreatedBy:      createdBy,
	})
}

// containsAny is a tiny helper to keep the SQLite error string matching local
// to this file (the matchers are SQLite-specific).
func containsAny(haystack string, needles ...string) bool {
	for _, n := range needles {
		if n != "" && (len(haystack) >= len(n)) && indexOf(haystack, n) != -1 {
			return true
		}
	}
	return false
}

// indexOf reimplements strings.Contains without importing strings here (file
// already has plenty of deps); cheap helper for the constraint-name match.
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
