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

// AgentInstanceRepo implements workforce.AgentInstanceRepository.
type AgentInstanceRepo struct {
	db *sql.DB
}

// NewAgentInstanceRepo constructs the repository.
func NewAgentInstanceRepo(db *sql.DB) *AgentInstanceRepo {
	return &AgentInstanceRepo{db: db}
}

const agentInstanceSelect = `SELECT id, name, agent_cli, worker_id, config, max_concurrent,
	state, is_builtin, identity_id, organization_id, kind, created_at, archived_at, archived_reason, archived_message, version
	FROM agent_instances`

// Save inserts a fresh row.
func (r *AgentInstanceRepo) Save(ctx context.Context, a *workforce.AgentInstance) error {
	if a == nil {
		return errors.New("agent instance repo: nil instance")
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	isBuiltin := 0
	if a.IsBuiltin() {
		isBuiltin = 1
	}
	const stmt = `INSERT INTO agent_instances (
		id, name, agent_cli, worker_id, config, max_concurrent,
		state, is_builtin, identity_id, organization_id, kind,
		created_at, archived_at, archived_reason, archived_message, version
	) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`
	_, err := exec.ExecContext(ctx, stmt,
		string(a.ID()),
		a.Name(),
		a.AgentCLI(),
		nullWorkerID(a.WorkerID()),
		a.Config(),
		nullInt(a.MaxConcurrent()),
		string(a.State()),
		isBuiltin,
		a.IdentityID(),
		a.OrganizationID(),
		a.Kind(),
		a.CreatedAt().Format(time.RFC3339Nano),
		nullTimePtr(a.ArchivedAt()),
		nullString(string(a.ArchivedReason())),
		nullString(a.ArchivedMessage()),
		a.Version(),
	)
	if err != nil {
		if IsUniqueConstraint(err) {
			msg := err.Error()
			switch {
			case containsAny(msg, "agent_instances.name"):
				return workforce.ErrAgentInstanceNameTaken
			case containsAny(msg, "agent_instances.id"):
				return workforce.ErrAgentInstanceAlreadyExists
			default:
				return workforce.ErrAgentInstanceAlreadyExists
			}
		}
		return err
	}
	return nil
}

// FindByID returns a row by PK.
func (r *AgentInstanceRepo) FindByID(ctx context.Context, id workforce.AgentInstanceID) (*workforce.AgentInstance, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, agentInstanceSelect+` WHERE id = ?`, string(id))
	a, err := scanAgentInstance(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrAgentInstanceNotFound
	}
	return a, err
}

// FindByName returns a row by globally unique name.
func (r *AgentInstanceRepo) FindByName(ctx context.Context, name string) (*workforce.AgentInstance, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, agentInstanceSelect+` WHERE name = ?`, name)
	a, err := scanAgentInstance(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, workforce.ErrAgentInstanceNotFound
	}
	return a, err
}

// FindAll lists with optional filters.
func (r *AgentInstanceRepo) FindAll(ctx context.Context, filter workforce.AgentInstanceFilter) ([]*workforce.AgentInstance, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	q := agentInstanceSelect + ` WHERE 1=1`
	args := []any{}
	if filter.WorkerID != nil {
		q += ` AND worker_id = ?`
		args = append(args, string(*filter.WorkerID))
	}
	if filter.State != nil {
		q += ` AND state = ?`
		args = append(args, string(*filter.State))
	}
	if filter.IsBuiltin != nil {
		v := 0
		if *filter.IsBuiltin {
			v = 1
		}
		q += ` AND is_builtin = ?`
		args = append(args, v)
	}
	if filter.OrganizationID != "" {
		q += ` AND organization_id = ?`
		args = append(args, filter.OrganizationID)
	}
	q += ` ORDER BY created_at ASC`
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*workforce.AgentInstance
	for rows.Next() {
		a, err := scanAgentInstance(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpdateState — CAS transition.
func (r *AgentInstanceRepo) UpdateState(ctx context.Context, id workforce.AgentInstanceID, from, to workforce.AgentInstanceState, version int) error {
	if !from.IsValid() || !to.IsValid() {
		return fmt.Errorf("agent instance repo: invalid state from=%s to=%s", from, to)
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `UPDATE agent_instances
		SET state = ?, version = version + 1
		WHERE id = ? AND state = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt, string(to), string(id), string(from), version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Disambiguate not-found vs CAS.
		var c int
		if scanErr := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_instances WHERE id = ?`, string(id)).Scan(&c); scanErr != nil {
			return scanErr
		}
		if c == 0 {
			return workforce.ErrAgentInstanceNotFound
		}
		return workforce.ErrAgentInstanceVersionConflict
	}
	return nil
}

// UpdateConfig — CAS update of config + optional max_concurrent.
func (r *AgentInstanceRepo) UpdateConfig(ctx context.Context, id workforce.AgentInstanceID, config string, maxConcurrent *int, version int) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `UPDATE agent_instances
		SET config = ?, max_concurrent = ?, version = version + 1
		WHERE id = ? AND version = ?`
	res, err := exec.ExecContext(ctx, stmt, config, nullInt(maxConcurrent), string(id), version)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		var c int
		if scanErr := exec.QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_instances WHERE id = ?`, string(id)).Scan(&c); scanErr != nil {
			return scanErr
		}
		if c == 0 {
			return workforce.ErrAgentInstanceNotFound
		}
		return workforce.ErrAgentInstanceVersionConflict
	}
	return nil
}

// Archive — CAS transition to archived state + records archive metadata.
func (r *AgentInstanceRepo) Archive(ctx context.Context, id workforce.AgentInstanceID, at time.Time, reason workforce.AgentInstanceArchivedReason, message string, version int) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `UPDATE agent_instances
		SET state = 'archived', archived_at = ?, archived_reason = ?, archived_message = ?, version = version + 1
		WHERE id = ? AND state = 'idle' AND is_builtin = 0 AND version = ?`
	res, err := exec.ExecContext(ctx, stmt,
		at.UTC().Format(time.RFC3339Nano),
		string(reason),
		message,
		string(id),
		version,
	)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		// Disambiguate.
		row := exec.QueryRowContext(ctx, `SELECT state, is_builtin, version FROM agent_instances WHERE id = ?`, string(id))
		var (
			st        string
			isBuiltin int
			ver       int
		)
		if scanErr := row.Scan(&st, &isBuiltin, &ver); scanErr != nil {
			if errors.Is(scanErr, sql.ErrNoRows) {
				return workforce.ErrAgentInstanceNotFound
			}
			return scanErr
		}
		if isBuiltin == 1 {
			return workforce.ErrAgentInstanceIsBuiltin
		}
		if workforce.AgentInstanceState(st) == workforce.AgentInstanceArchived {
			return workforce.ErrAgentInstanceArchived
		}
		if ver != version {
			return workforce.ErrAgentInstanceVersionConflict
		}
		return fmt.Errorf("agent instance repo: cannot archive from state %s", st)
	}
	return nil
}

// CountActiveExecutions counts non-terminal task_executions for this agent.
// Returns 0 if the column is NULL (e.g. P8 transitional state).
func (r *AgentInstanceRepo) CountActiveExecutions(ctx context.Context, id workforce.AgentInstanceID) (int, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `SELECT COUNT(*) FROM task_executions
		WHERE agent_instance_id = ? AND status IN ('submitted', 'working', 'input_required')`
	var c int
	if err := exec.QueryRowContext(ctx, stmt, string(id)).Scan(&c); err != nil {
		return 0, err
	}
	return c, nil
}

// BulkUpdateStateByWorker transitions all agents on workerID from `from` → `to`.
// Returns the affected row count. version field is not used; bumps version+1
// for each affected row.
func (r *AgentInstanceRepo) BulkUpdateStateByWorker(ctx context.Context, workerID workforce.WorkerID, from, to workforce.AgentInstanceState) (int, error) {
	if !from.IsValid() || !to.IsValid() {
		return 0, fmt.Errorf("agent instance repo: invalid bulk state from=%s to=%s", from, to)
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	const stmt = `UPDATE agent_instances
		SET state = ?, version = version + 1
		WHERE worker_id = ? AND state = ?`
	res, err := exec.ExecContext(ctx, stmt, string(to), string(workerID), string(from))
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	return int(n), nil
}

func scanAgentInstance(scan func(...any) error) (*workforce.AgentInstance, error) {
	var (
		id              string
		name            string
		agentCLI        string
		workerID        sql.NullString
		config          string
		maxConcurrent   sql.NullInt64
		state           string
		isBuiltin       int
		identityID      string
		organizationID  string
		kind            string
		createdAt       string
		archivedAt      sql.NullString
		archivedReason  sql.NullString
		archivedMessage sql.NullString
		version         int
	)
	if err := scan(&id, &name, &agentCLI, &workerID, &config, &maxConcurrent,
		&state, &isBuiltin, &identityID, &organizationID, &kind,
		&createdAt, &archivedAt, &archivedReason, &archivedMessage, &version); err != nil {
		return nil, err
	}
	created, err := time.Parse(time.RFC3339Nano, createdAt)
	if err != nil {
		return nil, fmt.Errorf("scan agent instance: created_at: %w", err)
	}
	archived, err := parseNullTime(archivedAt)
	if err != nil {
		return nil, err
	}
	var workerIDPtr *workforce.WorkerID
	if workerID.Valid {
		v := workforce.WorkerID(workerID.String)
		workerIDPtr = &v
	}
	var maxConcurrentPtr *int
	if maxConcurrent.Valid {
		v := int(maxConcurrent.Int64)
		maxConcurrentPtr = &v
	}
	return workforce.RehydrateAgentInstance(workforce.RehydrateAgentInstanceInput{
		ID:              workforce.AgentInstanceID(id),
		Name:            name,
		AgentCLI:        agentCLI,
		WorkerID:        workerIDPtr,
		Config:          config,
		MaxConcurrent:   maxConcurrentPtr,
		State:           workforce.AgentInstanceState(state),
		IsBuiltin:       isBuiltin == 1,
		IdentityID:      identityID,
		OrganizationID:  organizationID,
		Kind:            kind,
		CreatedAt:       created,
		ArchivedAt:      archived,
		ArchivedReason:  workforce.AgentInstanceArchivedReason(archivedReason.String),
		ArchivedMessage: archivedMessage.String,
		Version:         version,
	})
}

func nullWorkerID(p *workforce.WorkerID) any {
	if p == nil {
		return nil
	}
	return string(*p)
}

func nullInt(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
