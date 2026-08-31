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

type shadowReadinessRecord struct {
	Mode            EnforcementMode
	WindowStartedAt time.Time
	WindowEndedAt   time.Time
	Transports      []string
	Checks          int64
	Mismatches      int64
	LegacyOnly      int64
	EquivalentOnly  int64
	Ready           bool
	Reason          string
}

type shadowAuditCoverage struct {
	Checks        int64
	Mismatches    int64
	Transports    map[string]bool
	CoveragePairs map[string]bool
}

const reservedAccessGrantRoleNamePrefix = "Access grant"

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func normalizeRoleKindVisibility(kind, visibility string) (string, string, error) {
	kind = strings.TrimSpace(kind)
	visibility = strings.TrimSpace(visibility)
	if kind == "" {
		kind = "custom"
	}
	if visibility == "" {
		if kind == "managed" {
			visibility = "internal"
		} else {
			visibility = "reusable"
		}
	}
	switch kind {
	case "system", "custom":
		if visibility != "reusable" {
			return "", "", fmt.Errorf("%w: reusable roles must use system/custom kind", ErrInvalid)
		}
	case "managed":
		if visibility != "internal" {
			return "", "", fmt.Errorf("%w: managed roles must be internal", ErrInvalid)
		}
	default:
		return "", "", fmt.Errorf("%w: role kind must be system, custom, or managed", ErrInvalid)
	}
	return kind, visibility, nil
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

func (s *Store) persistShadowReadiness(ctx context.Context, rec shadowReadinessRecord) error {
	exec, err := s.exec(ctx)
	if err != nil {
		return err
	}
	if rec.WindowStartedAt.IsZero() {
		rec.WindowStartedAt = time.Now().UTC()
	}
	if rec.WindowEndedAt.IsZero() {
		rec.WindowEndedAt = rec.WindowStartedAt
	}
	rawTransports, err := json.Marshal(rec.Transports)
	if err != nil {
		return err
	}
	ready := 0
	if rec.Ready {
		ready = 1
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO authorization_shadow_readiness
		(id, mode, window_started_at, window_ended_at, transports_json, checks, mismatches, legacy_only, equivalent_only, ready, reason, updated_at)
		VALUES ('current', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			mode = excluded.mode,
			window_started_at = CASE
				WHEN authorization_shadow_readiness.window_started_at = '' THEN excluded.window_started_at
				ELSE authorization_shadow_readiness.window_started_at
			END,
			window_ended_at = excluded.window_ended_at,
			transports_json = excluded.transports_json,
			checks = excluded.checks,
			mismatches = excluded.mismatches,
			legacy_only = excluded.legacy_only,
			equivalent_only = excluded.equivalent_only,
			ready = excluded.ready,
			reason = excluded.reason,
			updated_at = excluded.updated_at`,
		rec.Mode, rec.WindowStartedAt.UTC().Format(time.RFC3339Nano), rec.WindowEndedAt.UTC().Format(time.RFC3339Nano),
		string(rawTransports), rec.Checks, rec.Mismatches, rec.LegacyOnly, rec.EquivalentOnly, ready, rec.Reason,
		time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) getShadowReadiness(ctx context.Context) (shadowReadinessRecord, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return shadowReadinessRecord{}, err
	}
	var rec shadowReadinessRecord
	var started, ended, rawTransports string
	var ready int
	err = exec.QueryRowContext(ctx, `SELECT mode, window_started_at, window_ended_at, transports_json,
		checks, mismatches, legacy_only, equivalent_only, ready, reason
		FROM authorization_shadow_readiness WHERE id = 'current'`).Scan(&rec.Mode, &started, &ended, &rawTransports,
		&rec.Checks, &rec.Mismatches, &rec.LegacyOnly, &rec.EquivalentOnly, &ready, &rec.Reason)
	if err != nil {
		return shadowReadinessRecord{}, err
	}
	if started != "" {
		if rec.WindowStartedAt, err = time.Parse(time.RFC3339Nano, started); err != nil {
			return shadowReadinessRecord{}, err
		}
	}
	if ended != "" {
		if rec.WindowEndedAt, err = time.Parse(time.RFC3339Nano, ended); err != nil {
			return shadowReadinessRecord{}, err
		}
	}
	if err := json.Unmarshal([]byte(rawTransports), &rec.Transports); err != nil {
		return shadowReadinessRecord{}, err
	}
	rec.Ready = ready == 1
	return rec, nil
}

func (s *Store) shadowAuditCoverage(ctx context.Context, started, ended time.Time) (shadowAuditCoverage, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return shadowAuditCoverage{}, err
	}
	rows, err := exec.QueryContext(ctx, `SELECT payload_json, created_at
		FROM authorization_audit_events
		WHERE event_type = 'authorization.shadow.compare'
		ORDER BY created_at, id`)
	if err != nil {
		return shadowAuditCoverage{}, err
	}
	defer rows.Close()
	out := shadowAuditCoverage{Transports: map[string]bool{}, CoveragePairs: map[string]bool{}}
	for rows.Next() {
		var raw, createdRaw string
		if err := rows.Scan(&raw, &createdRaw); err != nil {
			return shadowAuditCoverage{}, err
		}
		created := parseDBTime(createdRaw)
		if !started.IsZero() && created.Before(started) {
			continue
		}
		if !ended.IsZero() && created.After(ended) {
			continue
		}
		var payload struct {
			Transport    string `json:"transport"`
			Permission   string `json:"permission"`
			ResourceKind string `json:"resource_kind"`
			Mismatch     bool   `json:"mismatch"`
		}
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			return shadowAuditCoverage{}, err
		}
		out.Checks++
		if payload.Mismatch {
			out.Mismatches++
		}
		if strings.TrimSpace(payload.Transport) != "" {
			out.Transports[payload.Transport] = true
		}
		if strings.TrimSpace(payload.Permission) != "" && strings.TrimSpace(payload.ResourceKind) != "" {
			out.CoveragePairs[payload.ResourceKind+":"+payload.Permission] = true
		}
	}
	return out, rows.Err()
}

func (s *Store) getRole(ctx context.Context, id string) (Role, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return Role{}, err
	}
	row := exec.QueryRowContext(ctx, `SELECT id, org_id, kind, COALESCE(NULLIF(visibility, ''), 'reusable'), name, description, created_by, created_at, updated_at, version
		FROM authorization_roles WHERE id = ? AND revoked_at IS NULL`, strings.TrimSpace(id))
	return scanRole(row.Scan)
}

func (s *Store) findCustomRoleByName(ctx context.Context, orgID, name string) (Role, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return Role{}, err
	}
	row := exec.QueryRowContext(ctx, `SELECT id, org_id, kind, COALESCE(NULLIF(visibility, ''), 'reusable'), name, description, created_by, created_at, updated_at, version
		FROM authorization_roles
		WHERE org_id = ? AND name = ? AND kind = 'custom' AND COALESCE(NULLIF(visibility, ''), 'reusable') = 'reusable' AND revoked_at IS NULL`,
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
	kind, visibility, err := normalizeRoleKindVisibility(role.Kind, role.Visibility)
	if err != nil {
		return Role{}, "", err
	}
	if kind == "system" {
		return Role{}, "", ErrSystemRoleImmutable
	}
	if kind == "custom" && visibility == "reusable" && strings.HasPrefix(role.Name, reservedAccessGrantRoleNamePrefix) {
		return Role{}, "", fmt.Errorf("%w: %q prefix is reserved for managed internal roles", ErrInvalid, reservedAccessGrantRoleNamePrefix)
	}
	role.Kind = kind
	role.Visibility = visibility
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
			if existing.Kind == "system" {
				return Role{}, "", ErrSystemRoleImmutable
			}
			if existing.OrgID != role.OrgID {
				return Role{}, "", fmt.Errorf("%w: role org mismatch", ErrInvalid)
			}
			if existing.Kind != role.Kind || existing.Visibility != role.Visibility {
				return Role{}, "", fmt.Errorf("%w: role kind/visibility mismatch", ErrInvalid)
			}
			if _, err := exec.ExecContext(ctx, `UPDATE authorization_roles
				SET name = ?, description = ?, updated_at = ?, version = version + 1
				WHERE id = ? AND kind = ? AND visibility = ? AND revoked_at IS NULL`,
				role.Name, role.Description, ts, role.ID, role.Kind, role.Visibility); err != nil {
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
		(id, org_id, kind, visibility, name, description, created_by, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)`,
		role.ID, role.OrgID, role.Kind, role.Visibility, role.Name, role.Description, role.CreatedBy, ts, ts)
	if err != nil {
		return Role{}, "", err
	}
	created, err := s.getRole(ctx, role.ID)
	return created, "created", err
}

func (s *Store) upsertManagedInternalRole(ctx context.Context, orgID string, permission PermissionKey, resourceKind string, now time.Time) (Role, string, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return Role{}, "", err
	}
	orgID = strings.TrimSpace(orgID)
	permission = PermissionKey(strings.TrimSpace(string(permission)))
	resourceKind = strings.TrimSpace(resourceKind)
	if orgID == "" || permission == "" || resourceKind == "" {
		return Role{}, "", fmt.Errorf("%w: managed role requires org, permission and resource kind", ErrInvalid)
	}
	if !PermissionDefinedForResource(permission, resourceKind) {
		return Role{}, "", fmt.Errorf("%w: %s for %s", ErrPermissionUndefined, permission, resourceKind)
	}
	id, err := s.managedDirectRoleID(ctx, orgID, permission, resourceKind)
	if err != nil {
		return Role{}, "", err
	}
	ts := now.UTC().Format(time.RFC3339Nano)
	status := "created"
	if existing, err := s.getRole(ctx, id); err == nil {
		if existing.OrgID != orgID {
			return Role{}, "", fmt.Errorf("%w: managed role org mismatch", ErrInvalid)
		}
		if existing.Kind != "managed" || existing.Visibility != "internal" {
			var mapped int
			if err := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_role_ram_role_mappings WHERE ram_role_id = ?`, id).Scan(&mapped); err != nil {
				return Role{}, "", err
			}
			if mapped > 0 {
				return Role{}, "", fmt.Errorf("%w: role-access backing role is referenced by team roles", ErrConflict)
			}
			if _, err := exec.ExecContext(ctx, `UPDATE authorization_roles
				SET kind = 'managed', visibility = 'internal', name = ?, updated_at = ?, version = version + 1
				WHERE id = ? AND org_id = ? AND revoked_at IS NULL`,
				"Managed direct grant "+string(permission)+" on "+resourceKind, ts, id, orgID); err != nil {
				return Role{}, "", err
			}
			status = "updated"
		} else {
			status = "unchanged"
		}
	} else if errors.Is(err, ErrRoleNotFound) {
		if _, err := exec.ExecContext(ctx, `INSERT INTO authorization_roles
			(id, org_id, kind, visibility, stable_key, scope_kind, name, description, created_by, created_at, updated_at, version)
			VALUES (?, ?, 'managed', 'internal', ?, ?, ?, ?, 'system', ?, ?, 1)`,
			id, orgID, id, resourceKind, "Managed direct grant "+string(permission)+" on "+resourceKind, "Internal carrier for direct permission grants.", ts, ts); err != nil {
			return Role{}, "", err
		}
	} else {
		return Role{}, "", err
	}
	if err := s.replaceRolePermissions(ctx, id, []RolePermissionInput{{PermissionKey: permission, ResourceKind: resourceKind}}, now); err != nil {
		return Role{}, "", err
	}
	role, err := s.getRole(ctx, id)
	if status == "unchanged" {
		status = "set"
	}
	return role, status, err
}

func (s *Store) managedDirectRoleID(ctx context.Context, orgID string, permission PermissionKey, resourceKind string) (string, error) {
	legacyID := legacyManagedDirectRoleID(permission, resourceKind)
	existing, err := s.getRole(ctx, legacyID)
	if err == nil {
		if existing.OrgID == orgID {
			return legacyID, nil
		}
		return orgScopedManagedDirectRoleID(orgID, permission, resourceKind), nil
	}
	if !errors.Is(err, ErrRoleNotFound) {
		return "", err
	}
	return legacyID, nil
}

func legacyManagedDirectRoleID(permission PermissionKey, resourceKind string) string {
	sum := sha256.Sum256([]byte(string(permission) + "|" + resourceKind))
	return "role-access-" + hex.EncodeToString(sum[:])[:16]
}

func orgScopedManagedDirectRoleID(orgID string, permission PermissionKey, resourceKind string) string {
	sum := sha256.Sum256([]byte(orgID + "|" + string(permission) + "|" + resourceKind))
	return "role-access-" + hex.EncodeToString(sum[:])[:16]
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
	if role.Kind == "system" {
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
	return s.activeAssignmentsForResourceIDs(ctx, orgID, subject, kind, []string{id})
}

func (s *Store) activeAssignmentsForResourceIDs(ctx context.Context, orgID string, subject SubjectRef, kind string, ids []string) ([]RoleAssignment, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return nil, err
	}
	cleanIDs := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleanIDs = append(cleanIDs, id)
	}
	if len(cleanIDs) == 0 {
		return nil, nil
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(cleanIDs)), ",")
	args := []any{orgID, subject, strings.TrimSpace(kind)}
	for _, id := range cleanIDs {
		args = append(args, id)
	}
	rows, err := exec.QueryContext(ctx, `SELECT id, org_id, subject_ref, role_id, resource_kind, resource_id,
		created_by, created_at, expires_at, revoked_at, revoked_by, revoked_reason, version
		FROM authorization_role_assignments
		WHERE org_id = ? AND subject_ref = ? AND resource_kind = ? AND resource_id IN (`+placeholders+`) AND revoked_at IS NULL
		ORDER BY CASE resource_id
			WHEN ? THEN 0
			ELSE 1
		END, created_at, id`, append(args, cleanIDs[0])...)
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
	query := `UPDATE authorization_role_assignments
		SET revoked_at = ?, revoked_by = ?, revoked_reason = ?, version = version + 1
		WHERE org_id = ? AND id = ? AND revoked_at IS NULL`
	args := []any{ts, actor, strings.TrimSpace(in.Reason), strings.TrimSpace(orgID), a.ID}
	if in.ExpectedVersion > 0 {
		query += ` AND version = ?`
		args = append(args, in.ExpectedVersion)
	}
	result, err := exec.ExecContext(ctx, query, args...)
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

type revokePreviewRecord struct {
	PreviewID   string
	TokenHash   string
	ActorRef    SubjectRef
	OrgID       string
	SubjectHash string
	RequestHash string
	RequestJSON string
	Status      string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

func (s *Store) saveRevokePreview(ctx context.Context, rec revokePreviewRecord) error {
	exec, err := s.exec(ctx)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `INSERT INTO authorization_revoke_previews
		(preview_id, token_hash, actor_ref, org_id, subject_hash, request_hash, request_json, status, created_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
		rec.PreviewID, rec.TokenHash, rec.ActorRef, rec.OrgID, rec.SubjectHash, rec.RequestHash, rec.RequestJSON,
		rec.CreatedAt.UTC().Format(time.RFC3339Nano), rec.ExpiresAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) getRevokePreview(ctx context.Context, previewID string) (revokePreviewRecord, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return revokePreviewRecord{}, err
	}
	var rec revokePreviewRecord
	var created, expires string
	row := exec.QueryRowContext(ctx, `SELECT preview_id, token_hash, actor_ref, org_id, subject_hash, request_hash,
			request_json, status, created_at, expires_at
		FROM authorization_revoke_previews WHERE preview_id = ?`, strings.TrimSpace(previewID))
	if err := row.Scan(&rec.PreviewID, &rec.TokenHash, &rec.ActorRef, &rec.OrgID, &rec.SubjectHash, &rec.RequestHash, &rec.RequestJSON, &rec.Status, &created, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return revokePreviewRecord{}, ErrNotFound
		}
		return revokePreviewRecord{}, err
	}
	rec.CreatedAt = parseDBTime(created)
	rec.ExpiresAt = parseDBTime(expires)
	return rec, nil
}

func (s *Store) consumeRevokePreview(ctx context.Context, previewID string, now time.Time) error {
	exec, err := s.exec(ctx)
	if err != nil {
		return err
	}
	res, err := exec.ExecContext(ctx, `UPDATE authorization_revoke_previews
		SET status = 'confirmed', confirmed_at = ?
		WHERE preview_id = ? AND status = 'pending'`,
		now.UTC().Format(time.RFC3339Nano), strings.TrimSpace(previewID))
	if err != nil {
		return err
	}
	if rows, err := res.RowsAffected(); err == nil && rows == 0 {
		return ErrPreviewRejected
	}
	return nil
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

func (s *Store) listAuditEventsForSubject(ctx context.Context, subject SubjectRef, limit int) ([]AuditEvent, error) {
	exec, err := s.exec(ctx)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := exec.QueryContext(ctx, `SELECT id, event_type, actor_ref, subject_ref, permission_key,
			resource_kind, resource_id, role_id, assignment_id, request_id, payload_json, created_at
		FROM authorization_audit_events
		WHERE subject_ref = ? OR actor_ref = ?
		ORDER BY created_at DESC, id DESC
		LIMIT ?`, strings.TrimSpace(string(subject)), strings.TrimSpace(string(subject)), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AuditEvent
	for rows.Next() {
		var e AuditEvent
		var payload string
		var created string
		if err := rows.Scan(
			&e.ID,
			&e.EventType,
			&e.ActorRef,
			&e.SubjectRef,
			&e.PermissionKey,
			&e.ResourceKind,
			&e.ResourceID,
			&e.RoleID,
			&e.AssignmentID,
			&e.RequestID,
			&payload,
			&created,
		); err != nil {
			return nil, err
		}
		if strings.TrimSpace(payload) != "" {
			var p map[string]any
			if err := json.Unmarshal([]byte(payload), &p); err == nil {
				e.Payload = p
			}
		}
		e.CreatedAt = parseDBTime(created)
		out = append(out, e)
	}
	return out, rows.Err()
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
	if err := scan(&r.ID, &r.OrgID, &r.Kind, &r.Visibility, &r.Name, &r.Description, &r.CreatedBy, &created, &updated, &r.Version); err != nil {
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
