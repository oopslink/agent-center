// Package sqlite implements the Agent BC repository (v2.7 C1, ADR-0049).
package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
)

// AgentRepo implements agent.Repository.
type AgentRepo struct{ db *sql.DB }

// NewAgentRepo constructs the repo.
func NewAgentRepo(db *sql.DB) *AgentRepo { return &AgentRepo{db: db} }

func nullString(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func (r *AgentRepo) Save(ctx context.Context, a *agent.Agent) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	env, skills, err := marshalProfileJSON(a)
	if err != nil {
		return err
	}
	p := a.Profile()
	_, err = exec.ExecContext(ctx,
		`INSERT INTO agents (id, organization_id, name, description, model, cli, reasoning, mode, provider, env_vars, skills,
			worker_id, lifecycle, lifecycle_error, created_by, identity_member_id, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		string(a.ID()), a.OrganizationID(), p.Name, nullString(p.Description), nullString(p.Model),
		nullString(p.CLI), nullString(p.Reasoning), nullString(p.Mode), nullString(p.Provider),
		env, skills, a.WorkerID(), string(a.Lifecycle()), nullString(a.LifecycleError()),
		string(a.CreatedBy()), nullString(a.IdentityMemberID()), ts(a.CreatedAt()), ts(a.UpdatedAt()), a.Version())
	if persistence.IsUniqueViolation(err) {
		return agent.ErrAgentExists
	}
	return err
}

func (r *AgentRepo) Update(ctx context.Context, a *agent.Agent) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	env, skills, err := marshalProfileJSON(a)
	if err != nil {
		return err
	}
	p := a.Profile()
	// worker_id is intentionally NOT in the SET list — the binding is immutable.
	res, err := exec.ExecContext(ctx,
		`UPDATE agents SET name=?, description=?, model=?, cli=?, reasoning=?, mode=?, provider=?, env_vars=?, skills=?,
			lifecycle=?, lifecycle_error=?, updated_at=?, version=? WHERE id=?`,
		p.Name, nullString(p.Description), nullString(p.Model), nullString(p.CLI),
		nullString(p.Reasoning), nullString(p.Mode), nullString(p.Provider), env, skills,
		string(a.Lifecycle()), nullString(a.LifecycleError()), ts(a.UpdatedAt()), a.Version(), string(a.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return agent.ErrAgentNotFound
	}
	return nil
}

// Archive persists the v2.8 #272 soft-delete: lifecycle→archived AND clears the
// worker_id binding. This is the ONE legitimate place worker_id changes (the
// generic Update keeps it immutable, agent_repo.go above) — archive is a terminal
// release, so the worker is freed to re-bind. Idempotent at the row level
// (absent id → ErrAgentNotFound; the service guards already-archived upstream).
func (r *AgentRepo) Archive(ctx context.Context, a *agent.Agent) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE agents SET lifecycle=?, lifecycle_error='', worker_id='', updated_at=?, version=? WHERE id=?`,
		string(a.Lifecycle()), ts(a.UpdatedAt()), a.Version(), string(a.ID()))
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return agent.ErrAgentNotFound
	}
	return nil
}

func (r *AgentRepo) FindByID(ctx context.Context, id agent.AgentID) (*agent.Agent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, agentSelect+` WHERE id = ?`, string(id))
	a, err := scanAgent(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, agent.ErrAgentNotFound
	}
	return a, err
}

// FindByIdentityMemberID resolves the agent whose identity_member_id column
// equals id (v2.7 #157 / #185 FINDING-J). identity_member_id is nullable, so a
// NULL row never matches a non-empty id. One identity-member maps to one
// execution agent; ORDER BY created_at, id LIMIT 1 makes the result
// deterministic if that invariant is ever violated.
func (r *AgentRepo) FindByIdentityMemberID(ctx context.Context, identityMemberID string) (*agent.Agent, error) {
	id := strings.TrimSpace(identityMemberID)
	if id == "" {
		return nil, agent.ErrAgentNotFound
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		agentSelect+` WHERE identity_member_id = ? ORDER BY created_at, id LIMIT 1`, id)
	a, err := scanAgent(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, agent.ErrAgentNotFound
	}
	return a, err
}

// Delete hard-removes the agent row (v2.7 #197). Idempotent — absent id affects
// 0 rows and returns nil. The worker_id binding column goes with the row.
func (r *AgentRepo) Delete(ctx context.Context, id agent.AgentID) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx, `DELETE FROM agents WHERE id = ?`, string(id))
	return err
}

func (r *AgentRepo) ListByOrg(ctx context.Context, orgID string) ([]*agent.Agent, error) {
	return r.list(ctx, agentSelect+` WHERE organization_id = ? ORDER BY created_at, id`, orgID)
}

func (r *AgentRepo) ListByWorker(ctx context.Context, workerID string) ([]*agent.Agent, error) {
	return r.list(ctx, agentSelect+` WHERE worker_id = ? ORDER BY created_at, id`, workerID)
}

// ClearWorkerBindings unbinds every agent of a worker (worker_id → "") in one bulk
// update, bumping version + updated_at. v2.8.1 force-delete: the worker is
// force-removed and its agents become worker-less (retained, re-bindable — NOT
// archived). Returns the number of agents unbound. The second legitimate place
// worker_id changes (the other is Archive); the generic Update keeps it immutable.
func (r *AgentRepo) ClearWorkerBindings(ctx context.Context, workerID string, at time.Time) (int, error) {
	if strings.TrimSpace(workerID) == "" {
		return 0, nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE agents SET worker_id='', updated_at=?, version=version+1 WHERE worker_id=?`,
		ts(at), workerID)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

func (r *AgentRepo) list(ctx context.Context, q, arg string) ([]*agent.Agent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, q, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*agent.Agent
	for rows.Next() {
		a, err := scanAgent(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

const agentSelect = `SELECT id, organization_id, name, description, model, cli, reasoning, mode, provider, env_vars, skills,
	worker_id, lifecycle, lifecycle_error, created_by, identity_member_id, created_at, updated_at, version FROM agents`

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func marshalProfileJSON(a *agent.Agent) (env string, skills string, err error) {
	p := a.Profile()
	ev := p.EnvVars
	if ev == nil {
		ev = map[string]string{}
	}
	eb, err := json.Marshal(ev)
	if err != nil {
		return "", "", err
	}
	sk := a.Skills()
	if sk == nil {
		sk = []string{}
	}
	sb, err := json.Marshal(sk)
	if err != nil {
		return "", "", err
	}
	return string(eb), string(sb), nil
}

func scanAgent(scan func(...any) error) (*agent.Agent, error) {
	var (
		id, org, name, workerID, lifecycle, createdBy, createdAt, updatedAt string
		desc, model, cli, lifecycleErr, identityMemberID                    sql.NullString
		reasoning, mode, provider                                           sql.NullString
		envJSON, skillsJSON                                                 string
		version                                                             int
	)
	if err := scan(&id, &org, &name, &desc, &model, &cli, &reasoning, &mode, &provider, &envJSON, &skillsJSON,
		&workerID, &lifecycle, &lifecycleErr, &createdBy, &identityMemberID, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	var env map[string]string
	if err := json.Unmarshal([]byte(envJSON), &env); err != nil {
		return nil, err
	}
	var skills []string
	if err := json.Unmarshal([]byte(skillsJSON), &skills); err != nil {
		return nil, err
	}
	return agent.RehydrateAgent(agent.RehydrateAgentInput{
		ID: agent.AgentID(id), OrganizationID: org,
		Profile: agent.Profile{
			Name: name, Description: desc.String, Model: model.String, CLI: cli.String,
			Reasoning: reasoning.String, Mode: mode.String, Provider: provider.String, EnvVars: env,
		},
		Skills:  skills, WorkerID: workerID,
		Lifecycle: agent.AgentLifecycle(lifecycle), LifecycleError: lifecycleErr.String,
		CreatedBy:        agent.IdentityRef(createdBy),
		IdentityMemberID: identityMemberID.String,
		CreatedAt:        parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
	})
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

var _ agent.Repository = (*AgentRepo)(nil)
