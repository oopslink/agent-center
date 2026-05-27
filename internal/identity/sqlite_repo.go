package identity

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

// isUniqueConstraint returns true if err is a SQLite unique constraint
// violation.
func isUniqueConstraint(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// parseTime parses an RFC3339Nano string from SQLite.
func parseTime(s string) (time.Time, error) {
	return time.Parse(time.RFC3339Nano, s)
}

// parseTimePtr parses an optional time string; returns nil on empty.
func parseTimePtr(s sql.NullString) (*time.Time, error) {
	if !s.Valid || s.String == "" {
		return nil, nil
	}
	t, err := parseTime(s.String)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// nullString converts *string to sql.NullString.
func nullString(s *string) sql.NullString {
	if s == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: *s, Valid: true}
}

// nullTimeStr converts *time.Time to sql.NullString (RFC3339Nano).
func nullTimeStr(t *time.Time) sql.NullString {
	if t == nil {
		return sql.NullString{}
	}
	return sql.NullString{String: t.UTC().Format(time.RFC3339Nano), Valid: true}
}

// ============================================================
// SQLiteIdentityRepo
// ============================================================

// SQLiteIdentityRepo implements IdentityRepository.
type SQLiteIdentityRepo struct{ db *sql.DB }

// NewSQLiteIdentityRepo constructs the repo.
func NewSQLiteIdentityRepo(db *sql.DB) *SQLiteIdentityRepo {
	return &SQLiteIdentityRepo{db: db}
}

const identityInsertSQL = `
INSERT INTO identities
  (id, kind, display_name, description, account_status, passcode_hash, passcode_set_at, created_at, updated_at)
VALUES (?,?,?,?,?,?,?,?,?)`

// Save inserts a new Identity row.
func (r *SQLiteIdentityRepo) Save(ctx context.Context, id *Identity) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, identityInsertSQL,
		id.ID(), string(id.Kind()), id.DisplayName(), id.Description(),
		string(id.AccountStatus()), id.PasscodeHash(),
		nullTimeStr(id.PasscodeSetAt()),
		id.CreatedAt().Format(time.RFC3339Nano),
		id.UpdatedAt().Format(time.RFC3339Nano),
	)
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrIdentityAlreadyExists
		}
		return err
	}
	return nil
}

// Update persists mutations to an existing Identity row.
func (r *SQLiteIdentityRepo) Update(ctx context.Context, id *Identity) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const q = `
UPDATE identities SET
  display_name=?, description=?, account_status=?, passcode_hash=?,
  passcode_set_at=?, updated_at=?
WHERE id=?`
	res, err := exec.ExecContext(ctx, q,
		id.DisplayName(), id.Description(), string(id.AccountStatus()),
		id.PasscodeHash(), nullTimeStr(id.PasscodeSetAt()),
		id.UpdatedAt().Format(time.RFC3339Nano), id.ID(),
	)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrIdentityNotFound
	}
	return nil
}

// GetByID returns an Identity by its id.
func (r *SQLiteIdentityRepo) GetByID(ctx context.Context, id string) (*Identity, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, kind, display_name, description, account_status,
		passcode_hash, passcode_set_at, created_at, updated_at
		FROM identities WHERE id=?`
	row := exec.QueryRowContext(ctx, q, id)
	return scanIdentity(row)
}

// GetByDisplayName returns a user Identity by display_name.
func (r *SQLiteIdentityRepo) GetByDisplayName(ctx context.Context, name string) (*Identity, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, kind, display_name, description, account_status,
		passcode_hash, passcode_set_at, created_at, updated_at
		FROM identities WHERE display_name=? AND kind='user'`
	row := exec.QueryRowContext(ctx, q, name)
	return scanIdentity(row)
}

// List returns all identities.
func (r *SQLiteIdentityRepo) List(ctx context.Context) ([]*Identity, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, kind, display_name, description, account_status,
		passcode_hash, passcode_set_at, created_at, updated_at
		FROM identities ORDER BY created_at`
	rows, err := exec.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Identity
	for rows.Next() {
		id, err := scanIdentityRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func scanIdentity(row *sql.Row) (*Identity, error) {
	var (
		id, kind, displayName, description, accountStatus, passcodeHash string
		passcodeSetAtStr                                                   sql.NullString
		createdAtStr, updatedAtStr                                         string
	)
	err := row.Scan(&id, &kind, &displayName, &description, &accountStatus,
		&passcodeHash, &passcodeSetAtStr, &createdAtStr, &updatedAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrIdentityNotFound
		}
		return nil, err
	}
	return buildIdentity(id, kind, displayName, description, accountStatus, passcodeHash, passcodeSetAtStr, createdAtStr, updatedAtStr)
}

func scanIdentityRow(rows *sql.Rows) (*Identity, error) {
	var (
		id, kind, displayName, description, accountStatus, passcodeHash string
		passcodeSetAtStr                                                   sql.NullString
		createdAtStr, updatedAtStr                                         string
	)
	err := rows.Scan(&id, &kind, &displayName, &description, &accountStatus,
		&passcodeHash, &passcodeSetAtStr, &createdAtStr, &updatedAtStr)
	if err != nil {
		return nil, err
	}
	return buildIdentity(id, kind, displayName, description, accountStatus, passcodeHash, passcodeSetAtStr, createdAtStr, updatedAtStr)
}

func buildIdentity(id, kind, displayName, description, accountStatus, passcodeHash string, passcodeSetAtStr sql.NullString, createdAtStr, updatedAtStr string) (*Identity, error) {
	createdAt, err := parseTime(createdAtStr)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTime(updatedAtStr)
	if err != nil {
		return nil, err
	}
	passcodeSetAt, err := parseTimePtr(passcodeSetAtStr)
	if err != nil {
		return nil, err
	}
	return RehydrateIdentity(
		id, IdentityKind(kind), displayName, description,
		AccountStatus(accountStatus), passcodeHash, passcodeSetAt,
		createdAt, updatedAt,
	), nil
}

// CountIdentities returns the total number of identities (used for bootstrap detection).
func (r *SQLiteIdentityRepo) CountIdentities(ctx context.Context) (int, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return 0, err
	}
	var n int
	row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM identities`)
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ============================================================
// SQLiteOrganizationRepo
// ============================================================

// SQLiteOrganizationRepo implements OrganizationRepository.
type SQLiteOrganizationRepo struct{ db *sql.DB }

// NewSQLiteOrganizationRepo constructs the repo.
func NewSQLiteOrganizationRepo(db *sql.DB) *SQLiteOrganizationRepo {
	return &SQLiteOrganizationRepo{db: db}
}

const orgInsertSQL = `
INSERT INTO organizations
  (id, slug, name, description, created_by_identity_id, created_at, updated_at)
VALUES (?,?,?,?,?,?,?)`

// Save inserts or updates an Organization.
func (r *SQLiteOrganizationRepo) Save(ctx context.Context, org *Organization) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	// Check if exists first.
	var exists int
	row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM organizations WHERE id=?`, org.ID())
	if err := row.Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		_, err = exec.ExecContext(ctx, orgInsertSQL,
			org.ID(), org.Slug(), org.Name(), org.Description(),
			org.CreatedByIdentityID(),
			org.CreatedAt().Format(time.RFC3339Nano),
			org.UpdatedAt().Format(time.RFC3339Nano),
		)
	} else {
		const q = `UPDATE organizations SET slug=?,name=?,description=?,updated_at=?,deleted_at=? WHERE id=?`
		_, err = exec.ExecContext(ctx, q,
			org.Slug(), org.Name(), org.Description(),
			org.UpdatedAt().Format(time.RFC3339Nano),
			nullTimeStr(org.DeletedAt()),
			org.ID(),
		)
	}
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrOrganizationSlugTaken
		}
		return err
	}
	return nil
}

// GetByID returns an Organization by id.
func (r *SQLiteOrganizationRepo) GetByID(ctx context.Context, id string) (*Organization, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, slug, name, description, created_by_identity_id,
		created_at, updated_at, deleted_at
		FROM organizations WHERE id=?`
	row := exec.QueryRowContext(ctx, q, id)
	return scanOrganization(row)
}

// GetBySlug returns an active (non-deleted) Organization by slug.
func (r *SQLiteOrganizationRepo) GetBySlug(ctx context.Context, slug string) (*Organization, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, slug, name, description, created_by_identity_id,
		created_at, updated_at, deleted_at
		FROM organizations WHERE slug=? AND deleted_at IS NULL`
	row := exec.QueryRowContext(ctx, q, slug)
	return scanOrganization(row)
}

// ListForIdentity returns all active organizations where identity is a member.
func (r *SQLiteOrganizationRepo) ListForIdentity(ctx context.Context, identityID string) ([]*Organization, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `
SELECT o.id, o.slug, o.name, o.description, o.created_by_identity_id,
	o.created_at, o.updated_at, o.deleted_at
FROM organizations o
JOIN members m ON m.organization_id=o.id
WHERE m.identity_id=? AND m.status='joined' AND o.deleted_at IS NULL
ORDER BY o.created_at`
	rows, err := exec.QueryContext(ctx, q, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Organization
	for rows.Next() {
		org, err := scanOrganizationRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, org)
	}
	return result, rows.Err()
}

func scanOrganization(row *sql.Row) (*Organization, error) {
	var (
		id, slug, name, description, createdBy string
		createdAtStr, updatedAtStr             string
		deletedAtStr                           sql.NullString
	)
	err := row.Scan(&id, &slug, &name, &description, &createdBy,
		&createdAtStr, &updatedAtStr, &deletedAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrOrganizationNotFound
		}
		return nil, err
	}
	return buildOrganization(id, slug, name, description, createdBy, createdAtStr, updatedAtStr, deletedAtStr)
}

func scanOrganizationRow(rows *sql.Rows) (*Organization, error) {
	var (
		id, slug, name, description, createdBy string
		createdAtStr, updatedAtStr             string
		deletedAtStr                           sql.NullString
	)
	err := rows.Scan(&id, &slug, &name, &description, &createdBy,
		&createdAtStr, &updatedAtStr, &deletedAtStr)
	if err != nil {
		return nil, err
	}
	return buildOrganization(id, slug, name, description, createdBy, createdAtStr, updatedAtStr, deletedAtStr)
}

func buildOrganization(id, slug, name, description, createdBy, createdAtStr, updatedAtStr string, deletedAtStr sql.NullString) (*Organization, error) {
	createdAt, err := parseTime(createdAtStr)
	if err != nil {
		return nil, err
	}
	updatedAt, err := parseTime(updatedAtStr)
	if err != nil {
		return nil, err
	}
	deletedAt, err := parseTimePtr(deletedAtStr)
	if err != nil {
		return nil, err
	}
	return RehydrateOrganization(id, slug, name, description, createdBy, createdAt, updatedAt, deletedAt), nil
}

// ============================================================
// SQLiteMemberRepo
// ============================================================

// SQLiteMemberRepo implements MemberRepository.
type SQLiteMemberRepo struct{ db *sql.DB }

// NewSQLiteMemberRepo constructs the repo.
func NewSQLiteMemberRepo(db *sql.DB) *SQLiteMemberRepo {
	return &SQLiteMemberRepo{db: db}
}

const memberInsertSQL = `
INSERT INTO members
  (id, organization_id, identity_id, role, status, joined_at,
   invited_by_identity_id, invited_at, disabled_at, disabled_reason)
VALUES (?,?,?,?,?,?,?,?,?,?)`

// Save inserts or updates a Member row.
func (r *SQLiteMemberRepo) Save(ctx context.Context, m *Member) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	var exists int
	row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM members WHERE id=?`, m.ID())
	if err := row.Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		_, err = exec.ExecContext(ctx, memberInsertSQL,
			m.ID(), m.OrganizationID(), m.IdentityID(),
			string(m.Role()), string(m.Status()),
			m.JoinedAt().Format(time.RFC3339Nano),
			nullString(m.InvitedByIdentityID()),
			nullTimeStr(m.InvitedAt()),
			nullTimeStr(m.DisabledAt()),
			m.DisabledReason(),
		)
	} else {
		const q = `UPDATE members SET role=?,status=?,disabled_at=?,disabled_reason=? WHERE id=?`
		_, err = exec.ExecContext(ctx, q,
			string(m.Role()), string(m.Status()),
			nullTimeStr(m.DisabledAt()), m.DisabledReason(),
			m.ID(),
		)
	}
	if err != nil {
		if isUniqueConstraint(err) {
			return ErrMemberAlreadyExists
		}
		return err
	}
	return nil
}

// GetByID returns a Member by its id.
func (r *SQLiteMemberRepo) GetByID(ctx context.Context, id string) (*Member, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, organization_id, identity_id, role, status, joined_at,
		invited_by_identity_id, invited_at, disabled_at, disabled_reason
		FROM members WHERE id=?`
	row := exec.QueryRowContext(ctx, q, id)
	return scanMember(row)
}

// GetByOrganizationAndIdentity finds the joined member for (org, identity).
func (r *SQLiteMemberRepo) GetByOrganizationAndIdentity(ctx context.Context, orgID, identityID string) (*Member, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, organization_id, identity_id, role, status, joined_at,
		invited_by_identity_id, invited_at, disabled_at, disabled_reason
		FROM members WHERE organization_id=? AND identity_id=? AND status='joined'`
	row := exec.QueryRowContext(ctx, q, orgID, identityID)
	return scanMember(row)
}

// ListByOrganization returns all members of an organization.
func (r *SQLiteMemberRepo) ListByOrganization(ctx context.Context, orgID string) ([]*Member, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, organization_id, identity_id, role, status, joined_at,
		invited_by_identity_id, invited_at, disabled_at, disabled_reason
		FROM members WHERE organization_id=? ORDER BY joined_at`
	rows, err := exec.QueryContext(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Member
	for rows.Next() {
		m, err := scanMemberRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// CountActiveOwners returns the count of joined owner members in an org.
func (r *SQLiteMemberRepo) CountActiveOwners(ctx context.Context, orgID string) (int, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return 0, err
	}
	var n int
	row := exec.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM members WHERE organization_id=? AND role='owner' AND status='joined'`, orgID)
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func scanMember(row *sql.Row) (*Member, error) {
	var (
		id, orgID, identityID, role, status string
		joinedAtStr                          string
		invitedByStr                         sql.NullString
		invitedAtStr, disabledAtStr          sql.NullString
		disabledReason                       string
	)
	err := row.Scan(&id, &orgID, &identityID, &role, &status, &joinedAtStr,
		&invitedByStr, &invitedAtStr, &disabledAtStr, &disabledReason)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrMemberNotFound
		}
		return nil, err
	}
	return buildMember(id, orgID, identityID, role, status, joinedAtStr, invitedByStr, invitedAtStr, disabledAtStr, disabledReason)
}

func scanMemberRow(rows *sql.Rows) (*Member, error) {
	var (
		id, orgID, identityID, role, status string
		joinedAtStr                          string
		invitedByStr                         sql.NullString
		invitedAtStr, disabledAtStr          sql.NullString
		disabledReason                       string
	)
	err := rows.Scan(&id, &orgID, &identityID, &role, &status, &joinedAtStr,
		&invitedByStr, &invitedAtStr, &disabledAtStr, &disabledReason)
	if err != nil {
		return nil, err
	}
	return buildMember(id, orgID, identityID, role, status, joinedAtStr, invitedByStr, invitedAtStr, disabledAtStr, disabledReason)
}

func buildMember(id, orgID, identityID, role, status, joinedAtStr string, invitedByStr, invitedAtStr, disabledAtStr sql.NullString, disabledReason string) (*Member, error) {
	joinedAt, err := parseTime(joinedAtStr)
	if err != nil {
		return nil, err
	}
	invitedAt, err := parseTimePtr(invitedAtStr)
	if err != nil {
		return nil, err
	}
	disabledAt, err := parseTimePtr(disabledAtStr)
	if err != nil {
		return nil, err
	}
	var invitedBy *string
	if invitedByStr.Valid && invitedByStr.String != "" {
		s := invitedByStr.String
		invitedBy = &s
	}
	return RehydrateMember(id, orgID, identityID,
		MemberRole(role), MemberStatus(status),
		joinedAt, invitedBy, invitedAt, disabledAt, disabledReason,
	), nil
}

// ============================================================
// SQLiteInvitationRepo
// ============================================================

// SQLiteInvitationRepo implements InvitationRepository.
type SQLiteInvitationRepo struct{ db *sql.DB }

// NewSQLiteInvitationRepo constructs the repo.
func NewSQLiteInvitationRepo(db *sql.DB) *SQLiteInvitationRepo {
	return &SQLiteInvitationRepo{db: db}
}

// Save inserts or updates an Invitation.
func (r *SQLiteInvitationRepo) Save(ctx context.Context, inv *Invitation) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	var exists int
	row := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM invitations WHERE id=?`, inv.ID())
	if err := row.Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		const q = `INSERT INTO invitations
  (id, organization_id, invitee_handle, role_to_grant, invited_by_identity_id,
   status, token, created_at, expires_at)
VALUES (?,?,?,?,?,?,?,?,?)`
		_, err = exec.ExecContext(ctx, q,
			inv.ID(), inv.OrganizationID(), inv.InviteeHandle(), string(inv.RoleToGrant()),
			inv.InvitedByIdentityID(), string(inv.Status()), inv.Token(),
			inv.CreatedAt().Format(time.RFC3339Nano),
			inv.ExpiresAt().Format(time.RFC3339Nano),
		)
	} else {
		const q = `UPDATE invitations SET status=?,accepted_by_identity_id=?,accepted_at=? WHERE id=?`
		_, err = exec.ExecContext(ctx, q,
			string(inv.Status()),
			nullString(inv.AcceptedByIdentityID()),
			nullTimeStr(inv.AcceptedAt()),
			inv.ID(),
		)
	}
	return err
}

// GetByID returns an Invitation by id.
func (r *SQLiteInvitationRepo) GetByID(ctx context.Context, id string) (*Invitation, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, organization_id, invitee_handle, role_to_grant,
		invited_by_identity_id, status, token, created_at, expires_at,
		accepted_by_identity_id, accepted_at
		FROM invitations WHERE id=?`
	row := exec.QueryRowContext(ctx, q, id)
	return scanInvitation(row)
}

// GetByToken returns an Invitation by its one-time token.
func (r *SQLiteInvitationRepo) GetByToken(ctx context.Context, token string) (*Invitation, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, organization_id, invitee_handle, role_to_grant,
		invited_by_identity_id, status, token, created_at, expires_at,
		accepted_by_identity_id, accepted_at
		FROM invitations WHERE token=?`
	row := exec.QueryRowContext(ctx, q, token)
	return scanInvitation(row)
}

// ListByOrganization returns all invitations for an org.
func (r *SQLiteInvitationRepo) ListByOrganization(ctx context.Context, orgID string) ([]*Invitation, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	const q = `SELECT id, organization_id, invitee_handle, role_to_grant,
		invited_by_identity_id, status, token, created_at, expires_at,
		accepted_by_identity_id, accepted_at
		FROM invitations WHERE organization_id=? ORDER BY created_at`
	rows, err := exec.QueryContext(ctx, q, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []*Invitation
	for rows.Next() {
		inv, err := scanInvitationRow(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, inv)
	}
	return result, rows.Err()
}

func scanInvitation(row *sql.Row) (*Invitation, error) {
	var (
		id, orgID, inviteeHandle, roleToGrant, invitedBy, status, token string
		createdAtStr, expiresAtStr                                        string
		acceptedByStr                                                      sql.NullString
		acceptedAtStr                                                      sql.NullString
	)
	err := row.Scan(&id, &orgID, &inviteeHandle, &roleToGrant, &invitedBy,
		&status, &token, &createdAtStr, &expiresAtStr, &acceptedByStr, &acceptedAtStr)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrInvitationNotFound
		}
		return nil, err
	}
	return buildInvitation(id, orgID, inviteeHandle, roleToGrant, invitedBy, status, token, createdAtStr, expiresAtStr, acceptedByStr, acceptedAtStr)
}

func scanInvitationRow(rows *sql.Rows) (*Invitation, error) {
	var (
		id, orgID, inviteeHandle, roleToGrant, invitedBy, status, token string
		createdAtStr, expiresAtStr                                        string
		acceptedByStr                                                      sql.NullString
		acceptedAtStr                                                      sql.NullString
	)
	err := rows.Scan(&id, &orgID, &inviteeHandle, &roleToGrant, &invitedBy,
		&status, &token, &createdAtStr, &expiresAtStr, &acceptedByStr, &acceptedAtStr)
	if err != nil {
		return nil, err
	}
	return buildInvitation(id, orgID, inviteeHandle, roleToGrant, invitedBy, status, token, createdAtStr, expiresAtStr, acceptedByStr, acceptedAtStr)
}

func buildInvitation(id, orgID, inviteeHandle, roleToGrant, invitedBy, status, token, createdAtStr, expiresAtStr string, acceptedByStr, acceptedAtStr sql.NullString) (*Invitation, error) {
	createdAt, err := parseTime(createdAtStr)
	if err != nil {
		return nil, err
	}
	expiresAt, err := parseTime(expiresAtStr)
	if err != nil {
		return nil, err
	}
	acceptedAt, err := parseTimePtr(acceptedAtStr)
	if err != nil {
		return nil, err
	}
	var acceptedBy *string
	if acceptedByStr.Valid && acceptedByStr.String != "" {
		s := acceptedByStr.String
		acceptedBy = &s
	}
	return RehydrateInvitation(
		id, orgID, inviteeHandle, MemberRole(roleToGrant), invitedBy,
		InvitationStatus(status), token, createdAt, expiresAt, acceptedBy, acceptedAt,
	), nil
}
