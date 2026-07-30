package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/airuntime"
	"github.com/oopslink/agent-center/internal/persistence"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) GetCatalog(ctx context.Context, org string) (airuntime.Catalog, error) {
	c := airuntime.Catalog{OrgID: org, CLIs: []airuntime.CLIDefinition{}, Models: []airuntime.ModelDefinition{}, Profiles: []airuntime.RuntimeProfile{}}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	if _, err := exec.ExecContext(ctx, `INSERT OR IGNORE INTO ai_runtime_catalogs(org_id) VALUES (?)`, org); err != nil {
		return c, err
	}
	for _, seed := range []struct{ key, name, executable string }{{"codex", "Codex", "codex"}, {"claude-code", "Claude Code", "claude"}} {
		if _, err := exec.ExecContext(ctx, `INSERT OR IGNORE INTO ai_runtime_clis(id,org_id,key,display_name,executable,parameter_schema_json,enabled,system,created_at,updated_at) VALUES(?,?,?,?,?, ?,1,1,?,?)`,
			"cli-"+seed.key+"-"+org, org, seed.key, seed.name, seed.executable, `{"type":"object"}`, stamp(time.Now()), stamp(time.Now())); err != nil {
			return c, err
		}
	}
	if err := exec.QueryRowContext(ctx, `SELECT revision,default_profile_id FROM ai_runtime_catalogs WHERE org_id=?`, org).Scan(&c.Revision, &c.DefaultProfileID); err != nil {
		return c, err
	}
	rows, err := exec.QueryContext(ctx, `SELECT id,key,display_name,executable,version_constraint,required_features_json,parameter_schema_json,enabled,system,created_at,updated_at FROM ai_runtime_clis WHERE org_id=? ORDER BY key`, org)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var x airuntime.CLIDefinition
		var features, schema, created, updated string
		var enabled, system int
		if err := rows.Scan(&x.ID, &x.Key, &x.DisplayName, &x.Executable, &x.VersionConstraint, &features, &schema, &enabled, &system, &created, &updated); err != nil {
			rows.Close()
			return c, err
		}
		x.OrgID = org
		x.Enabled = enabled != 0
		x.System = system != 0
		x.ParameterSchema = json.RawMessage(schema)
		_ = json.Unmarshal([]byte(features), &x.RequiredFeatures)
		x.CreatedAt = parse(created)
		x.UpdatedAt = parse(updated)
		c.CLIs = append(c.CLIs, x)
	}
	rows.Close()
	rows, err = exec.QueryContext(ctx, `SELECT id,runtime_key,model_id,display_name,compatible_cli_keys_json,default_parameters_json,enabled,context_window,input_cost,output_cost,tier,created_at,updated_at FROM pm_model_catalog WHERE org_id=? ORDER BY runtime_key`, org)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var x airuntime.ModelDefinition
		var clis, params, created, updated string
		var enabled int
		if err := rows.Scan(&x.ID, &x.Key, &x.ModelKey, &x.DisplayName, &clis, &params, &enabled, &x.ContextWindow, &x.InputCost, &x.OutputCost, &x.Tier, &created, &updated); err != nil {
			rows.Close()
			return c, err
		}
		x.OrgID = org
		x.Enabled = enabled != 0
		_ = json.Unmarshal([]byte(clis), &x.CompatibleCLIKeys)
		_ = json.Unmarshal([]byte(params), &x.DefaultParameters)
		x.CreatedAt = parse(created)
		x.UpdatedAt = parse(updated)
		c.Models = append(c.Models, x)
	}
	rows.Close()
	rows, err = exec.QueryContext(ctx, `SELECT id,key,name,description,cli_key,model_key,parameters_json,enabled,version,created_at,updated_at FROM ai_runtime_profiles WHERE org_id=? ORDER BY key`, org)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var x airuntime.RuntimeProfile
		var params, created, updated string
		var enabled int
		if err := rows.Scan(&x.ID, &x.Key, &x.Name, &x.Description, &x.CLIKey, &x.ModelKey, &params, &enabled, &x.Version, &created, &updated); err != nil {
			rows.Close()
			return c, err
		}
		x.OrgID = org
		x.Enabled = enabled != 0
		_ = json.Unmarshal([]byte(params), &x.Parameters)
		x.CreatedAt = parse(created)
		x.UpdatedAt = parse(updated)
		c.Profiles = append(c.Profiles, x)
	}
	rows.Close()
	return c, nil
}

func (r *Repository) CreateCLI(ctx context.Context, x airuntime.CLIDefinition, expected int64, a airuntime.AuditEvent) (int64, error) {
	return r.write(ctx, x.OrgID, expected, a, func(exec persistence.SQLExecutor) error {
		features, _ := json.Marshal(x.RequiredFeatures)
		_, err := exec.ExecContext(ctx, `INSERT INTO ai_runtime_clis(id,org_id,key,display_name,executable,version_constraint,required_features_json,parameter_schema_json,enabled,system,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, x.ID, x.OrgID, x.Key, x.DisplayName, x.Executable, x.VersionConstraint, string(features), string(x.ParameterSchema), boolInt(x.Enabled), boolInt(x.System), stamp(x.CreatedAt), stamp(x.UpdatedAt))
		return err
	})
}
func (r *Repository) UpdateCLI(ctx context.Context, x airuntime.CLIDefinition, expected int64, a airuntime.AuditEvent) (int64, error) {
	return r.write(ctx, x.OrgID, expected, a, func(exec persistence.SQLExecutor) error {
		features, _ := json.Marshal(x.RequiredFeatures)
		res, err := exec.ExecContext(ctx, `UPDATE ai_runtime_clis SET display_name=?,executable=?,version_constraint=?,required_features_json=?,parameter_schema_json=?,enabled=?,updated_at=? WHERE id=? AND org_id=?`, x.DisplayName, x.Executable, x.VersionConstraint, string(features), string(x.ParameterSchema), boolInt(x.Enabled), stamp(x.UpdatedAt), x.ID, x.OrgID)
		if err == nil {
			n, _ := res.RowsAffected()
			if n == 0 {
				return airuntime.ErrNotFound
			}
		}
		return err
	})
}
func (r *Repository) CreateModel(ctx context.Context, x airuntime.ModelDefinition, expected int64, a airuntime.AuditEvent) (int64, error) {
	return r.write(ctx, x.OrgID, expected, a, func(exec persistence.SQLExecutor) error {
		clis, _ := json.Marshal(x.CompatibleCLIKeys)
		params, _ := json.Marshal(x.DefaultParameters)
		_, err := exec.ExecContext(ctx, `INSERT INTO pm_model_catalog(id,org_id,model_id,display_name,input_cost,output_cost,context_window,tier,created_by,created_at,updated_at,version,runtime_key,compatible_cli_keys_json,default_parameters_json,enabled) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, x.ID, x.OrgID, x.ModelKey, x.DisplayName, x.InputCost, x.OutputCost, x.ContextWindow, x.Tier, a.Actor, stamp(x.CreatedAt), stamp(x.UpdatedAt), 1, x.Key, string(clis), string(params), boolInt(x.Enabled))
		return err
	})
}
func (r *Repository) UpdateModel(ctx context.Context, x airuntime.ModelDefinition, expected int64, a airuntime.AuditEvent) (int64, error) {
	return r.write(ctx, x.OrgID, expected, a, func(exec persistence.SQLExecutor) error {
		clis, _ := json.Marshal(x.CompatibleCLIKeys)
		params, _ := json.Marshal(x.DefaultParameters)
		res, err := exec.ExecContext(ctx, `UPDATE pm_model_catalog SET model_id=?,display_name=?,compatible_cli_keys_json=?,default_parameters_json=?,enabled=?,context_window=?,input_cost=?,output_cost=?,tier=?,updated_at=?,version=version+1 WHERE id=? AND org_id=?`, x.ModelKey, x.DisplayName, string(clis), string(params), boolInt(x.Enabled), x.ContextWindow, x.InputCost, x.OutputCost, x.Tier, stamp(x.UpdatedAt), x.ID, x.OrgID)
		if err == nil {
			n, _ := res.RowsAffected()
			if n == 0 {
				return airuntime.ErrNotFound
			}
		}
		return err
	})
}
func (r *Repository) CreateProfile(ctx context.Context, x airuntime.RuntimeProfile, expected int64, a airuntime.AuditEvent) (int64, error) {
	return r.write(ctx, x.OrgID, expected, a, func(exec persistence.SQLExecutor) error {
		params, _ := json.Marshal(x.Parameters)
		_, err := exec.ExecContext(ctx, `INSERT INTO ai_runtime_profiles(id,org_id,key,name,description,cli_key,model_key,parameters_json,enabled,version,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, x.ID, x.OrgID, x.Key, x.Name, x.Description, x.CLIKey, x.ModelKey, string(params), boolInt(x.Enabled), 1, stamp(x.CreatedAt), stamp(x.UpdatedAt))
		return err
	})
}
func (r *Repository) UpdateProfile(ctx context.Context, x airuntime.RuntimeProfile, expected int64, a airuntime.AuditEvent) (int64, error) {
	return r.write(ctx, x.OrgID, expected, a, func(exec persistence.SQLExecutor) error {
		params, _ := json.Marshal(x.Parameters)
		res, err := exec.ExecContext(ctx, `UPDATE ai_runtime_profiles SET name=?,description=?,cli_key=?,model_key=?,parameters_json=?,enabled=?,version=version+1,updated_at=? WHERE id=? AND org_id=?`, x.Name, x.Description, x.CLIKey, x.ModelKey, string(params), boolInt(x.Enabled), stamp(x.UpdatedAt), x.ID, x.OrgID)
		if err == nil {
			n, _ := res.RowsAffected()
			if n == 0 {
				return airuntime.ErrNotFound
			}
		}
		return err
	})
}
func (r *Repository) SetDefaultProfile(ctx context.Context, org, id string, expected int64, a airuntime.AuditEvent) (int64, error) {
	return r.write(ctx, org, expected, a, func(exec persistence.SQLExecutor) error {
		_, err := exec.ExecContext(ctx, `UPDATE ai_runtime_catalogs SET default_profile_id=? WHERE org_id=?`, id, org)
		return err
	})
}

func (r *Repository) ApplyCatalog(ctx context.Context, catalog airuntime.Catalog, expected int64, a airuntime.AuditEvent) (int64, error) {
	return r.write(ctx, catalog.OrgID, expected, a, func(exec persistence.SQLExecutor) error {
		for _, x := range catalog.CLIs {
			features, _ := json.Marshal(x.RequiredFeatures)
			_, err := exec.ExecContext(ctx, `
				INSERT INTO ai_runtime_clis(id,org_id,key,display_name,executable,version_constraint,required_features_json,parameter_schema_json,enabled,system,created_at,updated_at)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(org_id,key) DO UPDATE SET display_name=excluded.display_name,executable=excluded.executable,
				version_constraint=excluded.version_constraint,required_features_json=excluded.required_features_json,
				parameter_schema_json=excluded.parameter_schema_json,enabled=excluded.enabled,updated_at=excluded.updated_at
			`, x.ID, catalog.OrgID, x.Key, x.DisplayName, x.Executable, x.VersionConstraint, string(features), string(x.ParameterSchema), boolInt(x.Enabled), boolInt(x.System), stamp(x.CreatedAt), stamp(x.UpdatedAt))
			if err != nil {
				return err
			}
		}
		for _, x := range catalog.Models {
			clis, _ := json.Marshal(x.CompatibleCLIKeys)
			params, _ := json.Marshal(x.DefaultParameters)
			_, err := exec.ExecContext(ctx, `
				INSERT INTO pm_model_catalog(id,org_id,model_id,display_name,input_cost,output_cost,context_window,tier,created_by,created_at,updated_at,version,runtime_key,compatible_cli_keys_json,default_parameters_json,enabled)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(org_id,runtime_key) DO UPDATE SET model_id=excluded.model_id,display_name=excluded.display_name,
				input_cost=excluded.input_cost,output_cost=excluded.output_cost,context_window=excluded.context_window,tier=excluded.tier,
				updated_at=excluded.updated_at,version=pm_model_catalog.version+1,compatible_cli_keys_json=excluded.compatible_cli_keys_json,
				default_parameters_json=excluded.default_parameters_json,enabled=excluded.enabled
			`, x.ID, catalog.OrgID, x.ModelKey, x.DisplayName, x.InputCost, x.OutputCost, x.ContextWindow, x.Tier, a.Actor, stamp(x.CreatedAt), stamp(x.UpdatedAt), 1, x.Key, string(clis), string(params), boolInt(x.Enabled))
			if err != nil {
				return err
			}
		}
		for _, x := range catalog.Profiles {
			params, _ := json.Marshal(x.Parameters)
			_, err := exec.ExecContext(ctx, `
				INSERT INTO ai_runtime_profiles(id,org_id,key,name,description,cli_key,model_key,parameters_json,enabled,version,created_at,updated_at)
				VALUES(?,?,?,?,?,?,?,?,?,?,?,?)
				ON CONFLICT(org_id,key) DO UPDATE SET name=excluded.name,description=excluded.description,cli_key=excluded.cli_key,
				model_key=excluded.model_key,parameters_json=excluded.parameters_json,enabled=excluded.enabled,
				version=excluded.version,updated_at=excluded.updated_at
			`, x.ID, catalog.OrgID, x.Key, x.Name, x.Description, x.CLIKey, x.ModelKey, string(params), boolInt(x.Enabled), x.Version, stamp(x.CreatedAt), stamp(x.UpdatedAt))
			if err != nil {
				return err
			}
		}
		_, err := exec.ExecContext(ctx, `UPDATE ai_runtime_catalogs SET default_profile_id=? WHERE org_id=?`, catalog.DefaultProfileID, catalog.OrgID)
		return err
	})
}

func (r *Repository) FreezeExecutionSnapshot(ctx context.Context, orgID, executionID string, snapshot airuntime.RuntimeSnapshot) (airuntime.RuntimeSnapshot, bool, error) {
	data, err := airuntime.CanonicalJSON(snapshot)
	if err != nil {
		return airuntime.RuntimeSnapshot{}, false, err
	}
	var stored airuntime.RuntimeSnapshot
	created := false
	err = persistence.RunInTx(ctx, r.db, func(txctx context.Context) error {
		exec, _ := persistence.ExecutorFromCtx(txctx, r.db)
		res, err := exec.ExecContext(txctx, `
			INSERT OR IGNORE INTO ai_runtime_execution_snapshots(execution_id,org_id,snapshot_json,created_at)
			VALUES(?,?,?,?)
		`, executionID, orgID, string(data), stamp(snapshot.ResolvedAt))
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		created = n == 1
		var raw, storedOrg string
		if err := exec.QueryRowContext(txctx, `SELECT org_id,snapshot_json FROM ai_runtime_execution_snapshots WHERE execution_id=?`, executionID).Scan(&storedOrg, &raw); err != nil {
			return err
		}
		if storedOrg != orgID {
			return fmt.Errorf("execution snapshot belongs to another organization")
		}
		return json.Unmarshal([]byte(raw), &stored)
	})
	return stored, created, err
}

func (r *Repository) GetExecutionSnapshot(ctx context.Context, orgID, executionID string) (airuntime.RuntimeSnapshot, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var storedOrg, raw string
	err := exec.QueryRowContext(ctx, `SELECT org_id,snapshot_json FROM ai_runtime_execution_snapshots WHERE execution_id=?`, executionID).Scan(&storedOrg, &raw)
	if err == sql.ErrNoRows {
		return airuntime.RuntimeSnapshot{}, false, nil
	}
	if err != nil {
		return airuntime.RuntimeSnapshot{}, false, err
	}
	if storedOrg != orgID {
		return airuntime.RuntimeSnapshot{}, false, fmt.Errorf("execution snapshot belongs to another organization")
	}
	var snapshot airuntime.RuntimeSnapshot
	if err := json.Unmarshal([]byte(raw), &snapshot); err != nil {
		return airuntime.RuntimeSnapshot{}, false, err
	}
	return snapshot, true, nil
}

func (r *Repository) GetAgentSelection(ctx context.Context, orgID, agentID string) (airuntime.RuntimeSelection, bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	var raw string
	err := exec.QueryRowContext(ctx, `SELECT selection_json FROM ai_runtime_agent_selections WHERE org_id=? AND agent_id=?`, orgID, agentID).Scan(&raw)
	if err == sql.ErrNoRows {
		return airuntime.RuntimeSelection{}, false, nil
	}
	if err != nil {
		return airuntime.RuntimeSelection{}, false, err
	}
	var selection airuntime.RuntimeSelection
	if err := json.Unmarshal([]byte(raw), &selection); err != nil {
		return airuntime.RuntimeSelection{}, false, err
	}
	return selection, true, nil
}

func (r *Repository) PutAgentSelection(ctx context.Context, orgID, agentID string, selection airuntime.RuntimeSelection, at time.Time) error {
	mode := strings.TrimSpace(selection.Mode)
	if mode == "" {
		mode = airuntime.SelectionInherit
		selection.Mode = mode
	}
	if mode != airuntime.SelectionInherit {
		if _, err := airuntime.NewRuntimeResolver(r).Resolve(ctx, orgID, selection); err != nil {
			return err
		}
	}
	raw, err := airuntime.CanonicalJSON(selection)
	if err != nil {
		return err
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err = exec.ExecContext(ctx, `INSERT INTO ai_runtime_agent_selections(org_id,agent_id,selection_json,updated_at)
		VALUES(?,?,?,?) ON CONFLICT(agent_id) DO UPDATE SET org_id=excluded.org_id,selection_json=excluded.selection_json,updated_at=excluded.updated_at`,
		orgID, agentID, string(raw), stamp(at))
	return err
}

func (r *Repository) write(ctx context.Context, org string, expected int64, a airuntime.AuditEvent, change func(persistence.SQLExecutor) error) (int64, error) {
	var revision int64
	err := persistence.RunInTx(ctx, r.db, func(txctx context.Context) error {
		exec, _ := persistence.ExecutorFromCtx(txctx, r.db)
		if _, err := exec.ExecContext(txctx, `INSERT OR IGNORE INTO ai_runtime_catalogs(org_id) VALUES (?)`, org); err != nil {
			return err
		}
		res, err := exec.ExecContext(txctx, `UPDATE ai_runtime_catalogs SET revision=revision+1 WHERE org_id=? AND revision=?`, org, expected)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			return &airuntime.Error{Reason: airuntime.ReasonRevisionConflict, Message: "catalog revision changed", Details: map[string]any{"expected_revision": expected}}
		}
		if err := change(exec); err != nil {
			return mapConstraint(err)
		}
		revision = expected + 1
		a.Revision = revision
		_, err = exec.ExecContext(txctx, `INSERT INTO ai_runtime_audit_log(id,org_id,actor,entity_type,entity_key,action,before_json,after_json,revision,occurred_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, a.ID, org, a.Actor, a.EntityType, a.EntityKey, a.Action, string(a.Before), string(a.After), revision, stamp(a.OccurredAt))
		return err
	})
	return revision, err
}
func mapConstraint(err error) error {
	if err != nil && (contains(err.Error(), "UNIQUE") || contains(err.Error(), "constraint")) {
		return fmt.Errorf("ai runtime key already exists: %w", err)
	}
	return err
}
func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func parse(v string) time.Time { t, _ := time.Parse(time.RFC3339Nano, v); return t }

var _ airuntime.Repository = (*Repository)(nil)
