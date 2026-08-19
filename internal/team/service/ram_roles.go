package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/team"
)

var (
	ErrRAMRoleMappingConflict = errors.New("team: RAM role mapping version conflict")
	ErrRAMRoleNotFound        = errors.New("team: RAM role not found in organization")
)

type ReplaceRAMRoleMappingInput struct {
	ActorRef        string
	RAMRoleIDs      []string
	ExpectedVersion int
}

func (s *Service) GetRAMRoleMapping(ctx context.Context, teamID team.TeamID, role string) (team.RAMRoleMapping, error) {
	role = strings.TrimSpace(role)
	if err := s.requireDeclaredRole(ctx, s.db, teamID, role); err != nil {
		return team.RAMRoleMapping{}, err
	}
	return loadRAMRoleMapping(ctx, s.db, teamID, role)
}

func (s *Service) PreviewRAMRoleMapping(ctx context.Context, teamID team.TeamID, role string, roleIDs []string) (team.RAMRoleMappingImpact, error) {
	role = strings.TrimSpace(role)
	next := normalizeRoleIDs(roleIDs)
	if err := s.validateRAMRoles(ctx, s.db, teamID, role, next); err != nil {
		return team.RAMRoleMappingImpact{}, err
	}
	current, err := loadRAMRoleMapping(ctx, s.db, teamID, role)
	if err != nil {
		return team.RAMRoleMappingImpact{}, err
	}
	impact := team.RAMRoleMappingImpact{TeamID: teamID, TeamRole: role, CurrentRoleIDs: current.RAMRoleIDs, NextRoleIDs: next, Version: current.Version}
	impact.AddedRoleIDs, impact.RemovedRoleIDs = diffRoleIDs(current.RAMRoleIDs, next)
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_members WHERE team_id=? AND role=?`, teamID.String(), role).Scan(&impact.MemberCount); err != nil {
		return team.RAMRoleMappingImpact{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT project_id FROM team_projects WHERE team_id=? ORDER BY project_id`, teamID.String())
	if err != nil {
		return team.RAMRoleMappingImpact{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return team.RAMRoleMappingImpact{}, err
		}
		impact.ProjectIDs = append(impact.ProjectIDs, id)
	}
	return impact, rows.Err()
}

func (s *Service) ReplaceRAMRoleMapping(ctx context.Context, teamID team.TeamID, role string, in ReplaceRAMRoleMappingInput) (team.RAMRoleMapping, error) {
	if in.ExpectedVersion < 1 {
		return team.RAMRoleMapping{}, ErrRAMRoleMappingConflict
	}
	role = strings.TrimSpace(role)
	next := normalizeRoleIDs(in.RAMRoleIDs)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return team.RAMRoleMapping{}, err
	}
	defer tx.Rollback()
	if err := s.validateRAMRoles(ctx, tx, teamID, role, next); err != nil {
		return team.RAMRoleMapping{}, err
	}
	current, err := loadRAMRoleMapping(ctx, tx, teamID, role)
	if err != nil {
		return team.RAMRoleMapping{}, err
	}
	if current.Version != in.ExpectedVersion {
		return team.RAMRoleMapping{}, ErrRAMRoleMappingConflict
	}
	now := s.clock.Now().UTC()
	stamp := now.Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO team_role_ram_role_versions (team_id,team_role,version,updated_at,updated_by) VALUES (?,?,1,?,?)`, teamID.String(), role, stamp, in.ActorRef); err != nil {
		return team.RAMRoleMapping{}, err
	}
	res, err := tx.ExecContext(ctx, `UPDATE team_role_ram_role_versions SET version=version+1,updated_at=?,updated_by=? WHERE team_id=? AND team_role=? AND version=?`, stamp, in.ActorRef, teamID.String(), role, in.ExpectedVersion)
	if err != nil {
		return team.RAMRoleMapping{}, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return team.RAMRoleMapping{}, ErrRAMRoleMappingConflict
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM team_role_ram_role_mappings WHERE team_id=? AND team_role=?`, teamID.String(), role); err != nil {
		return team.RAMRoleMapping{}, err
	}
	for _, id := range next {
		if _, err := tx.ExecContext(ctx, `INSERT INTO team_role_ram_role_mappings (team_id,team_role,ram_role_id,created_at,created_by) VALUES (?,?,?,?,?)`, teamID.String(), role, id, stamp, in.ActorRef); err != nil {
			return team.RAMRoleMapping{}, err
		}
	}
	previousJSON, _ := json.Marshal(current.RAMRoleIDs)
	nextJSON, _ := json.Marshal(next)
	auditID := s.idgen.NewEntityID("trramaudit")
	var orgID string
	if err := tx.QueryRowContext(ctx, `SELECT org_id FROM teams WHERE id=?`, teamID.String()).Scan(&orgID); err != nil {
		return team.RAMRoleMapping{}, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO team_role_ram_role_audit_events (id,org_id,team_id,team_role,actor_ref,previous_role_ids,next_role_ids,previous_version,next_version,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, auditID, orgID, teamID.String(), role, in.ActorRef, string(previousJSON), string(nextJSON), current.Version, current.Version+1, stamp); err != nil {
		return team.RAMRoleMapping{}, err
	}
	if err := tx.Commit(); err != nil {
		return team.RAMRoleMapping{}, err
	}
	return team.RAMRoleMapping{TeamID: teamID, TeamRole: role, RAMRoleIDs: next, Version: current.Version + 1, UpdatedAt: now, UpdatedBy: in.ActorRef}, nil
}

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Service) requireDeclaredRole(ctx context.Context, q queryer, teamID team.TeamID, role string) error {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM team_roles WHERE team_id=? AND role=?`, teamID.String(), role).Scan(&n); err != nil {
		return err
	}
	if n != 1 {
		return team.ErrRoleNotDeclared
	}
	return nil
}

func (s *Service) validateRAMRoles(ctx context.Context, q queryer, teamID team.TeamID, role string, ids []string) error {
	if err := s.requireDeclaredRole(ctx, q, teamID, role); err != nil {
		return err
	}
	var orgID string
	if err := q.QueryRowContext(ctx, `SELECT org_id FROM teams WHERE id=?`, teamID.String()).Scan(&orgID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return team.ErrTeamNotFound
		}
		return err
	}
	for _, id := range ids {
		var n int
		if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM authorization_roles WHERE id=? AND revoked_at IS NULL AND org_id IN ('',?)`, id, orgID).Scan(&n); err != nil {
			return err
		}
		if n != 1 {
			return fmt.Errorf("%w: %s", ErrRAMRoleNotFound, id)
		}
	}
	return nil
}

func loadRAMRoleMapping(ctx context.Context, q queryer, teamID team.TeamID, role string) (team.RAMRoleMapping, error) {
	m := team.RAMRoleMapping{TeamID: teamID, TeamRole: role, RAMRoleIDs: []string{}, Version: 1}
	var stamp, actor string
	err := q.QueryRowContext(ctx, `SELECT version,updated_at,updated_by FROM team_role_ram_role_versions WHERE team_id=? AND team_role=?`, teamID.String(), role).Scan(&m.Version, &stamp, &actor)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return m, err
	}
	if err == nil {
		m.UpdatedAt, _ = time.Parse(time.RFC3339Nano, stamp)
		m.UpdatedBy = actor
	}
	rows, err := q.QueryContext(ctx, `SELECT ram_role_id FROM team_role_ram_role_mappings WHERE team_id=? AND team_role=? ORDER BY ram_role_id`, teamID.String(), role)
	if err != nil {
		return m, err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return m, err
		}
		m.RAMRoleIDs = append(m.RAMRoleIDs, id)
	}
	return m, rows.Err()
}

func normalizeRoleIDs(in []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
func diffRoleIDs(current, next []string) (added, removed []string) {
	c, n := map[string]struct{}{}, map[string]struct{}{}
	for _, v := range current {
		c[v] = struct{}{}
	}
	for _, v := range next {
		n[v] = struct{}{}
		if _, ok := c[v]; !ok {
			added = append(added, v)
		}
	}
	for _, v := range current {
		if _, ok := n[v]; !ok {
			removed = append(removed, v)
		}
	}
	return
}
