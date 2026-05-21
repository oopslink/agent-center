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

// SQLiteIdentityRepo implements IdentityRepository on SQLite.
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
// id / kind are immutable so we never UPDATE them; this method is reserved
// for future Rename operations.
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

// SQLiteChannelBindingRepo implements ChannelBindingRepository on SQLite.
type SQLiteChannelBindingRepo struct {
	db *sql.DB
}

// NewSQLiteChannelBindingRepo constructs the repo.
func NewSQLiteChannelBindingRepo(db *sql.DB) *SQLiteChannelBindingRepo {
	return &SQLiteChannelBindingRepo{db: db}
}

// Save inserts a ChannelBinding; collisions:
//   - (channel, vendor_user_id) dup → ErrChannelBindingAlreadyExists
//   - preferred=1 conflict on (identity_id, channel) → ErrChannelBindingPreferredConflict
func (r *SQLiteChannelBindingRepo) Save(ctx context.Context, b *ChannelBinding) error {
	if b == nil {
		return errors.New("channel_binding repo: nil binding")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO channel_bindings (
		id, identity_id, channel, vendor_user_id, preferred, bound_at, created_at
	) VALUES (?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		b.ID(), string(b.IdentityID()), string(b.Channel()),
		b.VendorUserID(), boolToInt(b.Preferred()),
		b.BoundAt().Format(time.RFC3339Nano),
		b.CreatedAt().Format(time.RFC3339Nano),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			// SQLite reports the offending columns rather than the index
			// name, so we match on the column tuple. The preferred unique
			// index covers (identity_id, channel); the channel/vendor
			// unique covers (channel, vendor_user_id).
			s := err.Error()
			switch {
			case strings.Contains(s, "channel_bindings.identity_id") &&
				strings.Contains(s, "channel_bindings.channel") &&
				!strings.Contains(s, "vendor_user_id"):
				return ErrChannelBindingPreferredConflict
			case strings.Contains(s, "channel_bindings.channel") &&
				strings.Contains(s, "vendor_user_id"):
				return ErrChannelBindingAlreadyExists
			default:
				return ErrChannelBindingAlreadyExists
			}
		}
		return err
	}
	return nil
}

// FindByID returns one binding by ULID id.
func (r *SQLiteChannelBindingRepo) FindByID(ctx context.Context, id string) (*ChannelBinding, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx, bindingSelect+` WHERE id = ?`, id)
	b, err := scanBinding(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChannelBindingNotFound
	}
	return b, err
}

// FindByIdentityID returns all bindings for an identity ordered by channel.
func (r *SQLiteChannelBindingRepo) FindByIdentityID(ctx context.Context, identityID IdentityID) ([]*ChannelBinding, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx,
		bindingSelect+` WHERE identity_id = ? ORDER BY channel ASC, vendor_user_id ASC`,
		string(identityID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*ChannelBinding
	for rows.Next() {
		b, err := scanBinding(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// FindByVendorUserID reverse-looks a (channel, vendor_user_id) tuple.
func (r *SQLiteChannelBindingRepo) FindByVendorUserID(ctx context.Context, channel Channel, vendorUserID string) (*ChannelBinding, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx,
		bindingSelect+` WHERE channel = ? AND vendor_user_id = ?`,
		string(channel), vendorUserID)
	b, err := scanBinding(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChannelBindingNotFound
	}
	return b, err
}

// FindPreferred returns the binding flagged preferred for (identity, channel).
func (r *SQLiteChannelBindingRepo) FindPreferred(ctx context.Context, identityID IdentityID, channel Channel) (*ChannelBinding, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	row := exec.QueryRowContext(ctx,
		bindingSelect+` WHERE identity_id = ? AND channel = ? AND preferred = 1`,
		string(identityID), string(channel))
	b, err := scanBinding(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrChannelBindingNotFound
	}
	return b, err
}

// DeleteByIdentityAndChannel removes any binding rows matching (identity,
// channel). Returns ErrChannelBindingNotFound when no rows were deleted.
func (r *SQLiteChannelBindingRepo) DeleteByIdentityAndChannel(ctx context.Context, identityID IdentityID, channel Channel) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	res, err := exec.ExecContext(ctx,
		`DELETE FROM channel_bindings WHERE identity_id = ? AND channel = ?`,
		string(identityID), string(channel))
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrChannelBindingNotFound
	}
	return nil
}

const bindingSelect = `SELECT id, identity_id, channel, vendor_user_id, preferred, bound_at, created_at FROM channel_bindings`

func scanBinding(scan func(...any) error) (*ChannelBinding, error) {
	var (
		id, identityID, channel, vendorUserID string
		preferred                             int
		boundAt, createdAt                    string
	)
	if err := scan(&id, &identityID, &channel, &vendorUserID, &preferred, &boundAt, &createdAt); err != nil {
		return nil, err
	}
	bt, err := time.Parse(time.RFC3339Nano, boundAt)
	if err != nil {
		return nil, fmt.Errorf("parse bound_at: %w", err)
	}
	ct, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, err
	}
	return RehydrateChannelBinding(RehydrateChannelBindingInput{
		ID:           id,
		IdentityID:   IdentityID(identityID),
		Channel:      Channel(channel),
		VendorUserID: vendorUserID,
		Preferred:    preferred != 0,
		BoundAt:      bt,
		CreatedAt:    ct,
	}), nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed") ||
		strings.Contains(err.Error(), "constraint failed: UNIQUE") ||
		strings.Contains(err.Error(), "constraint failed: identities.id")
}
