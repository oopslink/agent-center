// Package sqlite implements the Team BC repository backed by SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/team"
)

// Repo implements team.Repository.
type Repo struct {
	db *sql.DB
}

// NewRepo constructs the SQLite-backed team.Repository.
func NewRepo(db *sql.DB) *Repo { return &Repo{db: db} }

var _ team.Repository = (*Repo)(nil)

const tsLayout = time.RFC3339Nano

// ---- Team -------------------------------------------------------------------

// CreateTeam inserts the team row plus its declared roles. Callers wrap this in
// persistence.RunInTx so the team + roles land atomically.
func (r *Repo) CreateTeam(ctx context.Context, t *team.Team) error {
	if t == nil {
		return errors.New("team repo: nil team")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO teams (id, org_id, name, description, created_at, updated_at, version)
		VALUES (?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		t.ID().String(), t.OrgID(), t.Name(), t.Description(),
		t.CreatedAt().UTC().Format(tsLayout), t.UpdatedAt().UTC().Format(tsLayout), t.Version(),
	)
	if err != nil {
		if persistence.IsUniqueViolation(err) {
			return team.ErrTeamNameTaken
		}
		return err
	}
	for _, rc := range t.Roles() {
		if err := insertRole(ctx, exec, t.ID(), rc, t.CreatedAt()); err != nil {
			return err
		}
	}
	return nil
}

func insertRole(ctx context.Context, exec persistence.SQLExecutor, id team.TeamID, rc team.RoleConfig, now time.Time) error {
	tags, err := json.Marshal(rc.CapabilityTags)
	if err != nil {
		return fmt.Errorf("marshal capability_tags: %w", err)
	}
	if len(rc.CapabilityTags) == 0 {
		tags = []byte("[]")
	}
	const stmt = `INSERT INTO team_roles (team_id, role, cli, model, capability_tags, max_concurrency, created_at)
		VALUES (?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		id.String(), rc.Role, rc.CLI, rc.Model, string(tags), rc.MaxConcurrency,
		now.UTC().Format(tsLayout),
	)
	return err
}

// UpdateTeam persists name/description/version for an existing team.
func (r *Repo) UpdateTeam(ctx context.Context, t *team.Team) error {
	if t == nil {
		return errors.New("team repo: nil team")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `UPDATE teams SET name=?, description=?, updated_at=?, version=? WHERE id=?`
	res, err := exec.ExecContext(ctx, stmt,
		t.Name(), t.Description(), t.UpdatedAt().UTC().Format(tsLayout), t.Version(), t.ID().String(),
	)
	if err != nil {
		if persistence.IsUniqueViolation(err) {
			return team.ErrTeamNameTaken
		}
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return team.ErrTeamNotFound
	}
	return nil
}

// DeleteTeam removes the team; FK ON DELETE CASCADE clears roles/members/
// projects. Idempotent.
func (r *Repo) DeleteTeam(ctx context.Context, id team.TeamID) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx, `DELETE FROM teams WHERE id=?`, id.String())
	return err
}

// GetTeam loads the team and its declared roles.
func (r *Repo) GetTeam(ctx context.Context, id team.TeamID) (*team.Team, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	var (
		orgID, name, desc  string
		createdAt, updated string
		version            int
	)
	row := exec.QueryRowContext(ctx,
		`SELECT org_id, name, description, created_at, updated_at, version FROM teams WHERE id=?`,
		id.String())
	if err := row.Scan(&orgID, &name, &desc, &createdAt, &updated, &version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, team.ErrTeamNotFound
		}
		return nil, err
	}
	roles, err := r.loadRoles(ctx, exec, id)
	if err != nil {
		return nil, err
	}
	ct, err := time.Parse(tsLayout, createdAt)
	if err != nil {
		return nil, fmt.Errorf("parse created_at: %w", err)
	}
	ut, err := time.Parse(tsLayout, updated)
	if err != nil {
		return nil, fmt.Errorf("parse updated_at: %w", err)
	}
	return team.Rehydrate(team.RehydrateInput{
		ID: id, OrgID: orgID, Name: name, Description: desc,
		Roles: roles, CreatedAt: ct, UpdatedAt: ut, Version: version,
	}), nil
}

func (r *Repo) loadRoles(ctx context.Context, exec persistence.SQLExecutor, id team.TeamID) ([]team.RoleConfig, error) {
	rows, err := exec.QueryContext(ctx,
		`SELECT role, cli, model, capability_tags, max_concurrency FROM team_roles WHERE team_id=? ORDER BY role`,
		id.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []team.RoleConfig
	for rows.Next() {
		var (
			role, cli, model, tagsJSON string
			maxConc                    int
		)
		if err := rows.Scan(&role, &cli, &model, &tagsJSON, &maxConc); err != nil {
			return nil, err
		}
		var tags []string
		if tagsJSON != "" {
			if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
				return nil, fmt.Errorf("unmarshal capability_tags: %w", err)
			}
		}
		out = append(out, team.RoleConfig{
			Role: role, CLI: cli, Model: model,
			CapabilityTags: tags, MaxConcurrency: maxConc,
		})
	}
	return out, rows.Err()
}

// ListTeams returns teams in an org (all orgs when orgID == "").
func (r *Repo) ListTeams(ctx context.Context, orgID string) ([]*team.Team, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	var (
		rows *sql.Rows
	)
	if orgID == "" {
		rows, err = exec.QueryContext(ctx,
			`SELECT id, org_id, name, description, created_at, updated_at, version FROM teams ORDER BY created_at, id`)
	} else {
		rows, err = exec.QueryContext(ctx,
			`SELECT id, org_id, name, description, created_at, updated_at, version FROM teams WHERE org_id=? ORDER BY created_at, id`,
			orgID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []team.TeamID
	type rec struct {
		id, org, name, desc string
		created, updated    string
		version             int
	}
	var recs []rec
	for rows.Next() {
		var rc rec
		if err := rows.Scan(&rc.id, &rc.org, &rc.name, &rc.desc, &rc.created, &rc.updated, &rc.version); err != nil {
			return nil, err
		}
		recs = append(recs, rc)
		ids = append(ids, team.TeamID(rc.id))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]*team.Team, 0, len(recs))
	for i, rc := range recs {
		roles, err := r.loadRoles(ctx, exec, ids[i])
		if err != nil {
			return nil, err
		}
		ct, err := time.Parse(tsLayout, rc.created)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		ut, err := time.Parse(tsLayout, rc.updated)
		if err != nil {
			return nil, fmt.Errorf("parse updated_at: %w", err)
		}
		out = append(out, team.Rehydrate(team.RehydrateInput{
			ID: ids[i], OrgID: rc.org, Name: rc.name, Description: rc.desc,
			Roles: roles, CreatedAt: ct, UpdatedAt: ut, Version: rc.version,
		}))
	}
	return out, nil
}

// ---- Members ----------------------------------------------------------------

// AddMember inserts a membership row. The DB enforces the invariants; message
// text distinguishes the agent-exclusivity index from the (team, ref) PK.
func (r *Repo) AddMember(ctx context.Context, m *team.TeamMember) error {
	if m == nil {
		return errors.New("team repo: nil member")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	const stmt = `INSERT INTO team_members (team_id, member_ref, member_kind, role, created_at)
		VALUES (?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		m.TeamID.String(), m.Ref.String(), m.Kind.String(), m.Role,
		m.CreatedAt.UTC().Format(tsLayout),
	)
	if err != nil {
		if persistence.IsUniqueViolation(err) {
			return classifyMemberUnique(err)
		}
		if isForeignKeyViolation(err) {
			// team_id + role FK: the role was not declared for this team.
			return team.ErrRoleNotDeclared
		}
		return err
	}
	return nil
}

// classifyMemberUnique maps a UNIQUE failure to the right domain error. The
// agent-exclusivity partial index keys on member_ref alone, so its message
// mentions member_ref but not team_id; the (team_id, member_ref) PK mentions
// both.
func classifyMemberUnique(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "team_id") {
		return team.ErrMemberAlreadyInTeam
	}
	if strings.Contains(msg, "member_ref") {
		return team.ErrAgentAlreadyInTeam
	}
	return team.ErrMemberAlreadyInTeam
}

func isForeignKeyViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "FOREIGN KEY constraint failed")
}

// RemoveMember deletes a membership row.
func (r *Repo) RemoveMember(ctx context.Context, id team.TeamID, ref team.MemberRef) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	res, err := exec.ExecContext(ctx,
		`DELETE FROM team_members WHERE team_id=? AND member_ref=?`, id.String(), ref.String())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return team.ErrMemberNotFound
	}
	return nil
}

// ListMembers returns a team's members ordered by insertion time.
func (r *Repo) ListMembers(ctx context.Context, id team.TeamID) ([]*team.TeamMember, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx,
		`SELECT member_ref, member_kind, role, created_at FROM team_members WHERE team_id=? ORDER BY created_at, member_ref`,
		id.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*team.TeamMember
	for rows.Next() {
		var ref, kind, role, created string
		if err := rows.Scan(&ref, &kind, &role, &created); err != nil {
			return nil, err
		}
		ct, err := time.Parse(tsLayout, created)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		out = append(out, &team.TeamMember{
			TeamID: id, Ref: team.MemberRef(ref), Kind: team.MemberKind(kind),
			Role: role, CreatedAt: ct,
		})
	}
	return out, rows.Err()
}

// FindAgentTeam returns the team an agent currently belongs to.
func (r *Repo) FindAgentTeam(ctx context.Context, ref team.MemberRef) (team.TeamID, bool, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return "", false, err
	}
	var id string
	row := exec.QueryRowContext(ctx,
		`SELECT team_id FROM team_members WHERE member_ref=? AND member_kind=? LIMIT 1`,
		ref.String(), team.MemberKindAgent.String())
	switch err := row.Scan(&id); {
	case errors.Is(err, sql.ErrNoRows):
		return "", false, nil
	case err != nil:
		return "", false, err
	default:
		return team.TeamID(id), true, nil
	}
}

// ---- Projects ---------------------------------------------------------------

// AssociateProject links a project to a team.
func (r *Repo) AssociateProject(ctx context.Context, id team.TeamID, projectID string) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	_, err = exec.ExecContext(ctx,
		`INSERT INTO team_projects (team_id, project_id, created_at) VALUES (?,?,?)`,
		id.String(), projectID, time.Now().UTC().Format(tsLayout))
	if err != nil {
		if persistence.IsUniqueViolation(err) {
			return team.ErrProjectAlreadyAssociated
		}
		return err
	}
	return nil
}

// DisassociateProject unlinks a project from a team. ErrProjectNotAssociated
// when the link is absent (symmetry with RemoveMember's not-found contract).
func (r *Repo) DisassociateProject(ctx context.Context, id team.TeamID, projectID string) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	res, err := exec.ExecContext(ctx,
		`DELETE FROM team_projects WHERE team_id=? AND project_id=?`, id.String(), projectID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return team.ErrProjectNotAssociated
	}
	return nil
}

// ListProjects returns a team's associated projects.
func (r *Repo) ListProjects(ctx context.Context, id team.TeamID) ([]*team.TeamProject, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	rows, err := exec.QueryContext(ctx,
		`SELECT project_id, created_at FROM team_projects WHERE team_id=? ORDER BY created_at, project_id`,
		id.String())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*team.TeamProject
	for rows.Next() {
		var pid, created string
		if err := rows.Scan(&pid, &created); err != nil {
			return nil, err
		}
		ct, err := time.Parse(tsLayout, created)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		out = append(out, &team.TeamProject{TeamID: id, ProjectID: pid, CreatedAt: ct})
	}
	return out, rows.Err()
}
