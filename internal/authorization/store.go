package authorization

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
)

type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) exec(ctx context.Context) (persistence.SQLExecutor, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("authorization store: nil db")
	}
	return persistence.ExecutorFromCtx(ctx, s.db)
}

func (s *Store) ListDefinitions(ctx context.Context) ([]PermissionDefinition, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx, `SELECT key, category, resource_kinds_json, actions_json, legacy_sources_json
		FROM permission_definitions ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PermissionDefinition
	for rows.Next() {
		var d PermissionDefinition
		var kinds, actions, sources string
		if err := rows.Scan(&d.Key, &d.Category, &kinds, &actions, &sources); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(kinds), &d.ResourceKinds); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(actions), &d.Actions); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(sources), &d.LegacySources); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) getRole(ctx context.Context, id string) (Role, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return Role{}, err
	}
	row := exec.QueryRowContext(ctx, `SELECT id, org_id, kind, name, description, created_by, created_at, updated_at, version
		FROM authorization_roles WHERE id = ? AND revoked_at IS NULL`, strings.TrimSpace(id))
	return scanRole(row.Scan)
}

func (s *Store) findCustomRoleByName(ctx context.Context, orgID, name string) (Role, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return Role{}, err
	}
	row := exec.QueryRowContext(ctx, `SELECT id, org_id, kind, name, description, created_by, created_at, updated_at, version
		FROM authorization_roles
		WHERE org_id = ? AND name = ? AND kind = 'custom' AND revoked_at IS NULL`,
		strings.TrimSpace(orgID), strings.TrimSpace(name))
	return scanRole(row.Scan)
}

func (s *Store) upsertCustomRole(ctx context.Context, role Role, now time.Time) (Role, string, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return Role{}, "", err
	}
	role.OrgID = strings.TrimSpace(role.OrgID)
	role.Name = strings.TrimSpace(role.Name)
	if role.OrgID == "" || role.Name == "" || strings.TrimSpace(role.CreatedBy) == "" {
		return Role{}, "", fmt.Errorf("%w: custom role requires org_id, name and created_by", ErrInvalid)
	}
	if role.ID == "" {
		existing, err := s.findCustomRoleByName(ctx, role.OrgID, role.Name)
		if err == nil {
			role.ID = existing.ID
		} else if !errors.Is(err, ErrRoleNotFound) {
			return Role{}, "", err
		}
	}
	ts := now.UTC().Format(time.RFC3339Nano)
	if role.ID != "" {
		existing, err := s.getRole(ctx, role.ID)
		if err == nil {
			if existing.Kind != "custom" {
				return Role{}, "", ErrSystemRoleImmutable
			}
			if existing.OrgID != role.OrgID {
				return Role{}, "", fmt.Errorf("%w: role org mismatch", ErrInvalid)
			}
			if _, err := exec.ExecContext(ctx, `UPDATE authorization_roles
				SET name = ?, description = ?, updated_at = ?, version = version + 1
				WHERE id = ? AND kind = 'custom' AND revoked_at IS NULL`,
				role.Name, role.Description, ts, role.ID); err != nil {
				return Role{}, "", err
			}
			updated, err := s.getRole(ctx, role.ID)
			return updated, "updated", err
		}
		if !errors.Is(err, ErrRoleNotFound) {
			return Role{}, "", err
		}
	}
	if role.ID == "" {
		role.ID = "role-" + shortHash(role.OrgID+"|"+role.Name+"|"+ts)
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO authorization_roles
		(id, org_id, kind, name, description, created_by, created_at, updated_at, version)
		VALUES (?, ?, 'custom', ?, ?, ?, ?, ?, 1)`,
		role.ID, role.OrgID, role.Name, role.Description, role.CreatedBy, ts, ts)
	if err != nil {
		return Role{}, "", err
	}
	created, err := s.getRole(ctx, role.ID)
	return created, "created", err
}

func (s *Store) replaceRolePermissions(ctx context.Context, roleID string, perms []RolePermissionInput, now time.Time) error {
	exec, err := s.exec(ctx)
	if err != nil {
		return err
	}
	role, err := s.getRole(ctx, roleID)
	if err != nil {
		return err
	}
	if role.Kind != "custom" {
		return ErrSystemRoleImmutable
	}
	if _, err := exec.ExecContext(ctx, `DELETE FROM authorization_role_permissions WHERE role_id = ?`, roleID); err != nil {
		return err
	}
	ts := now.UTC().Format(time.RFC3339Nano)
	seen := map[string]struct{}{}
	for _, p := range perms {
		key := strings.TrimSpace(string(p.PermissionKey))
		kind := strings.TrimSpace(p.ResourceKind)
		if key == "" || kind == "" {
			return fmt.Errorf("%w: permission_key and resource_kind required", ErrInvalid)
		}
		dedupe := key + "\x00" + kind
		if _, ok := seen[dedupe]; ok {
			continue
		}
		seen[dedupe] = struct{}{}
		delegatable := 0
		if p.Delegatable {
			delegatable = 1
		}
		if _, err := exec.ExecContext(ctx, `INSERT INTO authorization_role_permissions
			(role_id, permission_key, resource_kind, delegatable, created_at)
			VALUES (?, ?, ?, ?, ?)`, roleID, key, kind, delegatable, ts); err != nil {
			return err
		}
	}
	_, err = exec.ExecContext(ctx, `UPDATE authorization_roles
		SET updated_at = ?, version = version + 1 WHERE id = ?`, ts, roleID)
	return err
}

func (s *Store) rolePermissions(ctx context.Context, roleID string) ([]RolePermission, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx, `SELECT role_id, permission_key, resource_kind, delegatable
		FROM authorization_role_permissions WHERE role_id = ? ORDER BY permission_key, resource_kind`, roleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RolePermission
	for rows.Next() {
		var p RolePermission
		var delegatable int
		if err := rows.Scan(&p.RoleID, &p.PermissionKey, &p.ResourceKind, &delegatable); err != nil {
			return nil, err
		}
		p.Delegatable = delegatable == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) assignRole(ctx context.Context, in RoleAssignment, now time.Time) (RoleAssignment, string, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return RoleAssignment{}, "", err
	}
	in.OrgID = strings.TrimSpace(in.OrgID)
	in.SubjectRef = SubjectRef(strings.TrimSpace(string(in.SubjectRef)))
	in.RoleID = strings.TrimSpace(in.RoleID)
	in.ResourceKind = strings.TrimSpace(in.ResourceKind)
	in.ResourceID = strings.TrimSpace(in.ResourceID)
	if in.OrgID == "" || in.SubjectRef == "" || in.RoleID == "" || in.ResourceKind == "" || in.ResourceID == "" || strings.TrimSpace(in.CreatedBy) == "" {
		return RoleAssignment{}, "", fmt.Errorf("%w: assignment requires org, subject, role, resource and created_by", ErrInvalid)
	}
	if in.ID == "" {
		in.ID = "asgn-" + shortHash(in.OrgID+"|"+string(in.SubjectRef)+"|"+in.RoleID+"|"+in.ResourceKind+"|"+in.ResourceID)
	}
	ts := now.UTC().Format(time.RFC3339Nano)
	_, err = exec.ExecContext(ctx, `INSERT INTO authorization_role_assignments
		(id, org_id, subject_ref, role_id, resource_kind, resource_id, created_by, created_at, expires_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		in.ID, in.OrgID, in.SubjectRef, in.RoleID, in.ResourceKind, in.ResourceID, in.CreatedBy, ts, nullableTime(in.ExpiresAt))
	if err != nil {
		existing, findErr := s.findActiveAssignment(ctx, in.OrgID, in.SubjectRef, in.RoleID, in.ResourceKind, in.ResourceID)
		if findErr == nil {
			return existing, "unchanged", nil
		}
		return RoleAssignment{}, "", err
	}
	created, err := s.getAssignmentInOrg(ctx, in.OrgID, in.ID)
	return created, "created", err
}

func (s *Store) getAssignmentInOrg(ctx context.Context, orgID, id string) (RoleAssignment, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return RoleAssignment{}, err
	}
	row := exec.QueryRowContext(ctx, `SELECT id, org_id, subject_ref, role_id, resource_kind, resource_id,
		created_by, created_at, expires_at, revoked_at, revoked_by, revoked_reason, version
		FROM authorization_role_assignments WHERE org_id = ? AND id = ?`,
		strings.TrimSpace(orgID), strings.TrimSpace(id))
	return scanAssignment(row.Scan)
}

func (s *Store) findActiveAssignment(ctx context.Context, orgID string, subject SubjectRef, roleID, kind, id string) (RoleAssignment, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return RoleAssignment{}, err
	}
	row := exec.QueryRowContext(ctx, `SELECT id, org_id, subject_ref, role_id, resource_kind, resource_id,
		created_by, created_at, expires_at, revoked_at, revoked_by, revoked_reason, version
		FROM authorization_role_assignments
		WHERE org_id = ? AND subject_ref = ? AND role_id = ? AND resource_kind = ? AND resource_id = ? AND revoked_at IS NULL`,
		strings.TrimSpace(orgID), strings.TrimSpace(string(subject)), strings.TrimSpace(roleID), strings.TrimSpace(kind), strings.TrimSpace(id))
	return scanAssignment(row.Scan)
}

func (s *Store) findAssignmentForRevoke(ctx context.Context, orgID string, subject SubjectRef, roleID, kind, id string) (RoleAssignment, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return RoleAssignment{}, err
	}
	row := exec.QueryRowContext(ctx, `SELECT id, org_id, subject_ref, role_id, resource_kind, resource_id,
		created_by, created_at, expires_at, revoked_at, revoked_by, revoked_reason, version
		FROM authorization_role_assignments
		WHERE org_id = ? AND subject_ref = ? AND role_id = ? AND resource_kind = ? AND resource_id = ?
		ORDER BY CASE WHEN revoked_at IS NULL THEN 0 ELSE 1 END, created_at DESC, id DESC
		LIMIT 1`,
		strings.TrimSpace(orgID), strings.TrimSpace(string(subject)), strings.TrimSpace(roleID), strings.TrimSpace(kind), strings.TrimSpace(id))
	return scanAssignment(row.Scan)
}

func (s *Store) activeAssignmentsFor(ctx context.Context, orgID string, subject SubjectRef, kind, id string) ([]RoleAssignment, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx, `SELECT id, org_id, subject_ref, role_id, resource_kind, resource_id,
		created_by, created_at, expires_at, revoked_at, revoked_by, revoked_reason, version
		FROM authorization_role_assignments
		WHERE org_id = ? AND subject_ref = ? AND resource_kind = ? AND resource_id = ? AND revoked_at IS NULL
		ORDER BY created_at, id`, orgID, subject, kind, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RoleAssignment
	for rows.Next() {
		a, err := scanAssignment(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (s *Store) revokeAssignment(ctx context.Context, in RevokeInput, actor SubjectRef, orgID string, now time.Time) (RoleAssignment, string, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return RoleAssignment{}, "", err
	}
	a, err := s.assignmentForRevoke(ctx, orgID, in)
	if err != nil {
		return RoleAssignment{}, "", err
	}
	if a.OrgID != strings.TrimSpace(orgID) {
		return RoleAssignment{}, "", ErrAssignmentNotFound
	}
	if a.RevokedAt != nil {
		return a, "unchanged", nil
	}
	ts := now.UTC().Format(time.RFC3339Nano)
	result, err := exec.ExecContext(ctx, `UPDATE authorization_role_assignments
		SET revoked_at = ?, revoked_by = ?, revoked_reason = ?, version = version + 1
		WHERE org_id = ? AND id = ? AND revoked_at IS NULL`,
		ts, actor, strings.TrimSpace(in.Reason), strings.TrimSpace(orgID), a.ID)
	if err != nil {
		return RoleAssignment{}, "", err
	}
	if rows, err := result.RowsAffected(); err == nil && rows == 0 {
		current, findErr := s.getAssignmentInOrg(ctx, orgID, a.ID)
		if findErr != nil {
			return RoleAssignment{}, "", findErr
		}
		if current.RevokedAt != nil {
			return current, "unchanged", nil
		}
		return RoleAssignment{}, "", ErrConflict
	}
	revoked, err := s.getAssignmentInOrg(ctx, orgID, a.ID)
	return revoked, "revoked", err
}

func (s *Store) assignmentForRevoke(ctx context.Context, orgID string, in RevokeInput) (RoleAssignment, error) {
	orgID = strings.TrimSpace(orgID)
	if orgID == "" {
		return RoleAssignment{}, fmt.Errorf("%w: org_id required", ErrInvalid)
	}
	if assignmentID := strings.TrimSpace(in.AssignmentID); assignmentID != "" {
		return s.getAssignmentInOrg(ctx, orgID, assignmentID)
	}
	kind, id := in.Resource.Key()
	subject := SubjectRef(strings.TrimSpace(string(in.SubjectRef)))
	roleID := strings.TrimSpace(in.RoleID)
	if subject == "" || roleID == "" || kind == "" || id == "" {
		return RoleAssignment{}, fmt.Errorf("%w: revoke requires assignment_id or subject_ref, role_id and resource", ErrInvalid)
	}
	return s.findAssignmentForRevoke(ctx, orgID, subject, roleID, kind, id)
}

func (s *Store) beginIdempotency(ctx context.Context, key, actor, operation, requestHash string, now time.Time) (string, bool, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return "", false, err
	}
	key = strings.TrimSpace(key)
	if key == "" {
		return "", false, ErrIdempotencyRequired
	}
	var existingHash, response sql.NullString
	row := exec.QueryRowContext(ctx, `SELECT request_hash, response_json
		FROM authorization_idempotency_keys WHERE idempotency_key = ?`, key)
	switch err := row.Scan(&existingHash, &response); {
	case err == nil:
		if existingHash.String != requestHash {
			return "", false, ErrIdempotencyConflict
		}
		if response.Valid && response.String != "" {
			return response.String, true, nil
		}
		return "", true, fmt.Errorf("%w: idempotency request still pending", ErrConflict)
	case errors.Is(err, sql.ErrNoRows):
	default:
		return "", false, err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO authorization_idempotency_keys
		(idempotency_key, actor_ref, operation, request_hash, status, created_at)
		VALUES (?, ?, ?, ?, 'pending', ?)`, key, actor, operation, requestHash, now.UTC().Format(time.RFC3339Nano))
	return "", false, err
}

func (s *Store) completeIdempotency(ctx context.Context, key string, response []byte, now time.Time) error {
	exec, err := s.exec(ctx)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `UPDATE authorization_idempotency_keys
		SET response_json = ?, status = 'completed', completed_at = ?
		WHERE idempotency_key = ?`, string(response), now.UTC().Format(time.RFC3339Nano), strings.TrimSpace(key))
	return err
}

func (s *Store) appendAudit(ctx context.Context, e auditEvent) error {
	exec, err := s.exec(ctx)
	if err != nil {
		return err
	}
	payload := "{}"
	if e.Payload != nil {
		b, err := json.Marshal(e.Payload)
		if err != nil {
			return err
		}
		payload = string(b)
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO authorization_audit_events
		(id, event_type, actor_ref, subject_ref, permission_key, resource_kind, resource_id,
		 role_id, assignment_id, request_id, payload_json, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.ID, e.EventType, e.ActorRef, e.SubjectRef, e.PermissionKey, e.ResourceKind, e.ResourceID,
		e.RoleID, e.AssignmentID, e.RequestID, payload, e.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

type auditEvent struct {
	ID            string
	EventType     string
	ActorRef      SubjectRef
	SubjectRef    SubjectRef
	PermissionKey PermissionKey
	ResourceKind  string
	ResourceID    string
	RoleID        string
	AssignmentID  string
	RequestID     string
	Payload       map[string]any
	CreatedAt     time.Time
}

func scanRole(scan func(dest ...any) error) (Role, error) {
	var r Role
	var created, updated string
	if err := scan(&r.ID, &r.OrgID, &r.Kind, &r.Name, &r.Description, &r.CreatedBy, &created, &updated, &r.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Role{}, ErrRoleNotFound
		}
		return Role{}, err
	}
	r.CreatedAt = parseDBTime(created)
	r.UpdatedAt = parseDBTime(updated)
	return r, nil
}

func scanAssignment(scan func(dest ...any) error) (RoleAssignment, error) {
	var a RoleAssignment
	var created string
	var expires, revoked sql.NullString
	if err := scan(&a.ID, &a.OrgID, &a.SubjectRef, &a.RoleID, &a.ResourceKind, &a.ResourceID,
		&a.CreatedBy, &created, &expires, &revoked, &a.RevokedBy, &a.RevokedReason, &a.Version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return RoleAssignment{}, ErrAssignmentNotFound
		}
		return RoleAssignment{}, err
	}
	a.CreatedAt = parseDBTime(created)
	if expires.Valid && expires.String != "" {
		t := parseDBTime(expires.String)
		a.ExpiresAt = &t
	}
	if revoked.Valid && revoked.String != "" {
		t := parseDBTime(revoked.String)
		a.RevokedAt = &t
	}
	return a, nil
}

func nullableTime(v *time.Time) any {
	if v == nil {
		return nil
	}
	return v.UTC().Format(time.RFC3339Nano)
}

func parseDBTime(v string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, v); err == nil {
		return t.UTC()
	}
	if t, err := time.Parse("2006-01-02 15:04:05", v); err == nil {
		return t.UTC()
	}
	return time.Time{}
}

func shortHash(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:])[:8]
}
