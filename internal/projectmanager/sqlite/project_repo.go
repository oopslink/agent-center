// Package sqlite implements the ProjectManager BC repositories (v2.7 B1,
// ADR-0046). Tables use a pm_ prefix (see migration 0041) to coexist with the
// legacy Workforce projects table until B3.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// --- shared helpers ---------------------------------------------------------

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

// nullInt maps 0 → SQL NULL (v2.7.1 #245: org_number is "the number, or absent"
// for rows predating allocation); any non-zero value stores as-is.
func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return int64(n)
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique")
}

// --- ProjectRepo ------------------------------------------------------------

// ProjectRepo implements pm.ProjectRepository.
type ProjectRepo struct{ db *sql.DB }

// NewProjectRepo constructs the repo.
func NewProjectRepo(db *sql.DB) *ProjectRepo { return &ProjectRepo{db: db} }

func (r *ProjectRepo) Save(ctx context.Context, p *pm.Project) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_projects (id, organization_id, name, description, status, created_by, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?)`,
		string(p.ID()), p.OrganizationID(), p.Name(), nullString(p.Description()),
		string(p.Status()), string(p.CreatedBy()), ts(p.CreatedAt()), ts(p.UpdatedAt()), p.Version())
	if isUnique(err) {
		return pm.ErrProjectExists
	}
	return err
}

func (r *ProjectRepo) Update(ctx context.Context, p *pm.Project) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE pm_projects SET name=?, description=?, status=?, updated_at=?, version=? WHERE id=?`,
		p.Name(), nullString(p.Description()), string(p.Status()), ts(p.UpdatedAt()), p.Version(), string(p.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrProjectNotFound
	}
	return nil
}

func (r *ProjectRepo) FindByID(ctx context.Context, id pm.ProjectID) (*pm.Project, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, projectSelect+` WHERE id = ?`, string(id))
	p, err := scanProject(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrProjectNotFound
	}
	return p, err
}

func (r *ProjectRepo) ListByOrg(ctx context.Context, orgID string) ([]*pm.Project, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, projectSelect+` WHERE organization_id = ? ORDER BY created_at, id`, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Project
	for rows.Next() {
		p, err := scanProject(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ListAll returns ALL projects across ALL organizations (operator-global,
// no org filter), stable-ordered (created_at, id). Mirrors ListByOrg without
// the WHERE clause. Operator-scoped: callers must be operator-only (CLI
// project list / admin project find-all). v2.7 #131 PR-3.
func (r *ProjectRepo) ListAll(ctx context.Context) ([]*pm.Project, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, projectSelect+` ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.Project
	for rows.Next() {
		p, err := scanProject(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

const projectSelect = `SELECT id, organization_id, name, description, status, created_by, created_at, updated_at, version FROM pm_projects`

func scanProject(scan func(...any) error) (*pm.Project, error) {
	var (
		id, org, name, status, createdBy, createdAt, updatedAt string
		desc                                                   sql.NullString
		version                                                int
	)
	if err := scan(&id, &org, &name, &desc, &status, &createdBy, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return pm.RehydrateProject(pm.RehydrateProjectInput{
		ID: pm.ProjectID(id), OrganizationID: org, Name: name, Description: desc.String,
		Status: pm.ProjectStatus(status), CreatedBy: pm.IdentityRef(createdBy),
		CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
	})
}

// --- ProjectMemberRepo ------------------------------------------------------

// ProjectMemberRepo implements pm.ProjectMemberRepository.
type ProjectMemberRepo struct{ db *sql.DB }

// NewProjectMemberRepo constructs the repo.
func NewProjectMemberRepo(db *sql.DB) *ProjectMemberRepo { return &ProjectMemberRepo{db: db} }

func (r *ProjectMemberRepo) Save(ctx context.Context, m *pm.ProjectMember) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO pm_project_members (id, project_id, identity_id, role, added_by, created_at) VALUES (?,?,?,?,?,?)`,
		string(m.ID()), string(m.ProjectID()), string(m.IdentityID()), string(m.Role()),
		string(m.AddedBy()), ts(m.CreatedAt()))
	if isUnique(err) {
		return pm.ErrMemberExists
	}
	return err
}

func (r *ProjectMemberRepo) FindByID(ctx context.Context, id pm.MemberID) (*pm.ProjectMember, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, memberSelect+` WHERE id = ?`, string(id))
	m, err := scanMember(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrMemberNotFound
	}
	return m, err
}

func (r *ProjectMemberRepo) FindByProjectAndIdentity(ctx context.Context, projectID pm.ProjectID, identityID pm.IdentityRef) (*pm.ProjectMember, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, memberSelect+` WHERE project_id = ? AND identity_id = ?`, string(projectID), string(identityID))
	m, err := scanMember(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, pm.ErrMemberNotFound
	}
	return m, err
}

func (r *ProjectMemberRepo) ListByProject(ctx context.Context, projectID pm.ProjectID) ([]*pm.ProjectMember, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, memberSelect+` WHERE project_id = ? ORDER BY created_at, id`, string(projectID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*pm.ProjectMember
	for rows.Next() {
		m, err := scanMember(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func (r *ProjectMemberRepo) Delete(ctx context.Context, id pm.MemberID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx, `DELETE FROM pm_project_members WHERE id = ?`, string(id))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return pm.ErrMemberNotFound
	}
	return nil
}

const memberSelect = `SELECT id, project_id, identity_id, role, added_by, created_at FROM pm_project_members`

func scanMember(scan func(...any) error) (*pm.ProjectMember, error) {
	var id, projectID, identityID, role, addedBy, createdAt string
	if err := scan(&id, &projectID, &identityID, &role, &addedBy, &createdAt); err != nil {
		return nil, err
	}
	return pm.RehydrateProjectMember(pm.NewProjectMemberInput{
		ID: pm.MemberID(id), ProjectID: pm.ProjectID(projectID), IdentityID: pm.IdentityRef(identityID),
		Role: pm.ProjectMemberRole(role), AddedBy: pm.IdentityRef(addedBy), CreatedAt: parseTime(createdAt),
	})
}

var (
	_ pm.ProjectRepository       = (*ProjectRepo)(nil)
	_ pm.ProjectMemberRepository = (*ProjectMemberRepo)(nil)
)
