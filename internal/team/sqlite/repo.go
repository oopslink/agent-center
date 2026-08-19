// Package sqlite implements the Team BC repository backed by SQLite.
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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
	if err := replaceMemoryPolicy(ctx, exec, t.ID(), t.MemoryPolicy(), t.CreatedAt()); err != nil {
		return err
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
	requirements, err := json.Marshal(rc.AccessRequirements)
	if err != nil {
		return fmt.Errorf("marshal access_requirements: %w", err)
	}
	if len(rc.AccessRequirements) == 0 {
		requirements = []byte("[]")
	}
	const stmt = `INSERT INTO team_roles (team_id, role, cli, model, capability_tags, max_concurrency, created_at, access_requirements)
		VALUES (?,?,?,?,?,?,?,?)`
	_, err = exec.ExecContext(ctx, stmt,
		id.String(), rc.Role, rc.CLI, rc.Model, string(tags), rc.MaxConcurrency,
		now.UTC().Format(tsLayout), string(requirements),
	)
	if err != nil {
		return err
	}
	return replaceRoleRAMRoles(ctx, exec, id, rc.Role, rc.RAMRoleKeys, now, "system")
}

func replaceRoleRAMRoles(ctx context.Context, exec persistence.SQLExecutor, teamID team.TeamID, role string, keys []string, now time.Time, actor string) error {
	previous := []string{}
	rows, err := exec.QueryContext(ctx, `SELECT ram_role_id FROM team_role_ram_role_mappings WHERE team_id=? AND team_role=? ORDER BY ram_role_id`, teamID.String(), role)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		previous = append(previous, id)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	previousVersion := 0
	err = exec.QueryRowContext(ctx, `SELECT version FROM team_role_ram_role_versions WHERE team_id=? AND team_role=?`, teamID.String(), role).Scan(&previousVersion)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if _, err := exec.ExecContext(ctx, `DELETE FROM team_role_ram_role_mappings WHERE team_id=? AND team_role=?`, teamID.String(), role); err != nil {
		return err
	}
	seen := map[string]struct{}{}
	next := []string{}
	for _, key := range keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		var roleID string
		err := exec.QueryRowContext(ctx, `SELECT ar.id FROM authorization_roles ar JOIN teams t ON t.id=? WHERE ar.name=? AND ar.revoked_at IS NULL AND ar.org_id IN ('',t.org_id) ORDER BY CASE WHEN ar.org_id=t.org_id THEN 0 ELSE 1 END LIMIT 1`, teamID.String(), key).Scan(&roleID)
		if errors.Is(err, sql.ErrNoRows) {
			return fmt.Errorf("%w: %q", team.ErrRAMRoleKeyNotFound, key)
		}
		if err != nil {
			return err
		}
		if _, err := exec.ExecContext(ctx, `INSERT INTO team_role_ram_role_mappings (team_id,team_role,ram_role_id,created_at,created_by) VALUES (?,?,?,?,?)`, teamID.String(), role, roleID, now.UTC().Format(tsLayout), actor); err != nil {
			return err
		}
		next = append(next, roleID)
	}
	sort.Strings(next)
	stamp := now.UTC().Format(tsLayout)
	if _, err := exec.ExecContext(ctx, `INSERT INTO team_role_ram_role_versions (team_id,team_role,version,updated_at,updated_by) VALUES (?,?,1,?,?) ON CONFLICT(team_id,team_role) DO UPDATE SET version=version+1,updated_at=excluded.updated_at,updated_by=excluded.updated_by`, teamID.String(), role, stamp, actor); err != nil {
		return err
	}
	previousJSON, _ := json.Marshal(previous)
	nextJSON, _ := json.Marshal(next)
	if string(previousJSON) == string(nextJSON) {
		return nil
	}
	var orgID string
	if err := exec.QueryRowContext(ctx, `SELECT org_id FROM teams WHERE id=?`, teamID.String()).Scan(&orgID); err != nil {
		return err
	}
	nextVersion := previousVersion + 1
	if _, err := exec.ExecContext(ctx, `INSERT INTO team_role_ram_role_audit_events (id,org_id,team_id,team_role,actor_ref,previous_role_ids,next_role_ids,previous_version,next_version,created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, fmt.Sprintf("trram-%s-%s-%d-%d", teamID.String(), role, now.UnixNano(), nextVersion), orgID, teamID.String(), role, actor, string(previousJSON), string(nextJSON), previousVersion, nextVersion, stamp); err != nil {
		return err
	}
	return nil
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

func (r *Repo) ReplaceRoles(ctx context.Context, t *team.Team) error {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	roles := t.Roles()
	for _, rc := range roles {
		tags, err := json.Marshal(rc.CapabilityTags)
		if err != nil {
			return fmt.Errorf("marshal capability_tags: %w", err)
		}
		requirements, err := json.Marshal(rc.AccessRequirements)
		if err != nil {
			return fmt.Errorf("marshal access_requirements: %w", err)
		}
		_, err = exec.ExecContext(ctx, `INSERT INTO team_roles
			(team_id, role, cli, model, capability_tags, max_concurrency, created_at, access_requirements)
			VALUES (?,?,?,?,?,?,?,?) ON CONFLICT(team_id, role) DO UPDATE SET
			cli=excluded.cli, model=excluded.model, capability_tags=excluded.capability_tags,
			max_concurrency=excluded.max_concurrency, access_requirements=excluded.access_requirements`, t.ID().String(), rc.Role, rc.CLI,
			rc.Model, string(tags), rc.MaxConcurrency, t.UpdatedAt().UTC().Format(tsLayout), string(requirements))
		if err != nil {
			return err
		}
		if err := replaceRoleRAMRoles(ctx, exec, t.ID(), rc.Role, rc.RAMRoleKeys, t.UpdatedAt(), "system"); err != nil {
			return err
		}
	}
	if len(roles) == 0 {
		_, err = exec.ExecContext(ctx, `DELETE FROM team_roles WHERE team_id=?`, t.ID().String())
		return err
	}
	args := []any{t.ID().String()}
	marks := make([]string, 0, len(roles))
	for _, rc := range roles {
		marks = append(marks, "?")
		args = append(args, rc.Role)
	}
	_, err = exec.ExecContext(ctx, `DELETE FROM team_roles WHERE team_id=? AND role NOT IN (`+strings.Join(marks, ",")+`)`, args...)
	return err
}

// SetMemoryPolicy persists a team's controlled Team Memory write policy.
func (r *Repo) SetMemoryPolicy(ctx context.Context, t *team.Team) error {
	if t == nil {
		return errors.New("team repo: nil team")
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return err
	}
	if err := replaceMemoryPolicy(ctx, exec, t.ID(), t.MemoryPolicy(), t.UpdatedAt()); err != nil {
		return err
	}
	const stmt = `UPDATE teams SET updated_at=?, version=? WHERE id=?`
	res, err := exec.ExecContext(ctx, stmt, t.UpdatedAt().UTC().Format(tsLayout), t.Version(), t.ID().String())
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return team.ErrTeamNotFound
	}
	return nil
}

func replaceMemoryPolicy(ctx context.Context, exec persistence.SQLExecutor, id team.TeamID, policy team.TeamMemoryPolicy, now time.Time) error {
	normalized, err := policy.Normalize()
	if err != nil {
		return err
	}
	ts := now.UTC().Format(tsLayout)
	if _, err := exec.ExecContext(ctx, `INSERT INTO team_memory_policies (team_id, mode, updated_at)
		VALUES (?,?,?) ON CONFLICT(team_id) DO UPDATE SET mode=excluded.mode, updated_at=excluded.updated_at`,
		id.String(), string(normalized.Mode), ts); err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, `DELETE FROM team_memory_policy_curators WHERE team_id=?`, id.String()); err != nil {
		return err
	}
	for _, ref := range normalized.CuratorAgentRefs {
		if _, err := exec.ExecContext(ctx, `INSERT INTO team_memory_policy_curators (team_id, agent_ref, created_at)
			VALUES (?,?,?)`, id.String(), ref.String(), ts); err != nil {
			return err
		}
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
	policy, err := r.loadMemoryPolicy(ctx, exec, id)
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
		Roles: roles, MemoryPolicy: policy, CreatedAt: ct, UpdatedAt: ut, Version: version,
	}), nil
}

func (r *Repo) loadRoles(ctx context.Context, exec persistence.SQLExecutor, id team.TeamID) ([]team.RoleConfig, error) {
	rows, err := exec.QueryContext(ctx,
		`SELECT role, cli, model, capability_tags, max_concurrency, COALESCE(access_requirements, '[]') FROM team_roles WHERE team_id=? ORDER BY role`,
		id.String())
	if err != nil {
		return nil, err
	}
	var out []team.RoleConfig
	for rows.Next() {
		var (
			role, cli, model, tagsJSON string
			maxConc                    int
			requirementsJSON           string
		)
		if err := rows.Scan(&role, &cli, &model, &tagsJSON, &maxConc, &requirementsJSON); err != nil {
			return nil, err
		}
		var tags []string
		if tagsJSON != "" {
			if err := json.Unmarshal([]byte(tagsJSON), &tags); err != nil {
				return nil, fmt.Errorf("unmarshal capability_tags: %w", err)
			}
		}
		var requirements []string
		if requirementsJSON != "" {
			if err := json.Unmarshal([]byte(requirementsJSON), &requirements); err != nil {
				return nil, fmt.Errorf("unmarshal access_requirements: %w", err)
			}
		}
		out = append(out, team.RoleConfig{
			Role: role, CLI: cli, Model: model,
			CapabilityTags: tags, AccessRequirements: requirements, MaxConcurrency: maxConc,
		})
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, err
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for i := range out {
		keys, err := loadRAMRoleKeys(ctx, exec, id, out[i].Role)
		if err != nil {
			return nil, err
		}
		out[i].RAMRoleKeys = keys
	}
	return out, nil
}

func loadRAMRoleKeys(ctx context.Context, exec persistence.SQLExecutor, id team.TeamID, role string) ([]string, error) {
	rows, err := exec.QueryContext(ctx, `SELECT ar.name FROM team_role_ram_role_mappings m JOIN authorization_roles ar ON ar.id=m.ram_role_id WHERE m.team_id=? AND m.team_role=? ORDER BY ar.name`, id.String(), role)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		out = append(out, key)
	}
	return out, rows.Err()
}

// GetMemoryPolicy loads the Team Memory policy for id, defaulting when no
// explicit row has been written yet.
func (r *Repo) GetMemoryPolicy(ctx context.Context, id team.TeamID) (team.TeamMemoryPolicy, error) {
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return team.TeamMemoryPolicy{}, err
	}
	if _, err := r.GetTeam(ctx, id); err != nil {
		return team.TeamMemoryPolicy{}, err
	}
	return r.loadMemoryPolicy(ctx, exec, id)
}

func (r *Repo) loadMemoryPolicy(ctx context.Context, exec persistence.SQLExecutor, id team.TeamID) (team.TeamMemoryPolicy, error) {
	mode := string(team.TeamMemoryProposalOnly)
	row := exec.QueryRowContext(ctx, `SELECT mode FROM team_memory_policies WHERE team_id=?`, id.String())
	switch err := row.Scan(&mode); {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return team.TeamMemoryPolicy{}, err
	}
	rows, err := exec.QueryContext(ctx, `SELECT agent_ref FROM team_memory_policy_curators WHERE team_id=? ORDER BY agent_ref`, id.String())
	if err != nil {
		return team.TeamMemoryPolicy{}, err
	}
	defer rows.Close()
	var refs []team.MemberRef
	for rows.Next() {
		var ref string
		if err := rows.Scan(&ref); err != nil {
			return team.TeamMemoryPolicy{}, err
		}
		refs = append(refs, team.MemberRef(ref))
	}
	if err := rows.Err(); err != nil {
		return team.TeamMemoryPolicy{}, err
	}
	return team.TeamMemoryPolicy{Mode: team.TeamMemoryPolicyMode(mode), CuratorAgentRefs: refs}.Normalize()
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
		policy, err := r.loadMemoryPolicy(ctx, exec, ids[i])
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
			Roles: roles, MemoryPolicy: policy, CreatedAt: ct, UpdatedAt: ut, Version: rc.version,
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
		if strings.Contains(err.Error(), "agent already in another team") {
			return team.ErrAgentAlreadyInTeam
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
// current PK is (team_id, member_ref, role), so a duplicate unique failure means
// the same member already has that role in the team. Cross-team agent exclusivity
// is enforced by a trigger and mapped in AddMember.
func classifyMemberUnique(err error) error {
	msg := err.Error()
	if strings.Contains(msg, "team_id") {
		return team.ErrMemberAlreadyInTeam
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
	if _, err := exec.ExecContext(ctx,
		`DELETE FROM team_memory_policy_curators WHERE team_id=? AND agent_ref=?`, id.String(), ref.String()); err != nil {
		return err
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

// ListMembersByTeams returns the members of ALL the given teams in ONE batched
// IN(...) read — the query behind the directory's membership rollup, which would
// otherwise pay one read per team (N+1).
func (r *Repo) ListMembersByTeams(ctx context.Context, ids []team.TeamID) ([]*team.TeamMember, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	exec, err := persistence.ExecutorFromCtx(ctx, r.db)
	if err != nil {
		return nil, err
	}
	ph := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args[i] = id.String()
	}
	rows, err := exec.QueryContext(ctx,
		`SELECT team_id, member_ref, member_kind, role, created_at FROM team_members
		 WHERE team_id IN (`+strings.Join(ph, ",")+`) ORDER BY team_id, created_at, member_ref`,
		args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*team.TeamMember
	for rows.Next() {
		var teamID, ref, kind, role, created string
		if err := rows.Scan(&teamID, &ref, &kind, &role, &created); err != nil {
			return nil, err
		}
		ct, err := time.Parse(tsLayout, created)
		if err != nil {
			return nil, fmt.Errorf("parse created_at: %w", err)
		}
		out = append(out, &team.TeamMember{
			TeamID: team.TeamID(teamID), Ref: team.MemberRef(ref), Kind: team.MemberKind(kind),
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
