package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/airuntime"
)

func ReadCatalog(ctx context.Context, db *sql.DB, org string) (airuntime.Catalog, error) {
	c := airuntime.Catalog{OrgID: org, CLIs: []airuntime.CLIDefinition{}, Models: []airuntime.ModelDefinition{}, Profiles: []airuntime.RuntimeProfile{}}
	row := db.QueryRowContext(ctx, `SELECT revision,default_profile_id FROM ai_runtime_catalogs WHERE org_id=?`, org)
	if err := row.Scan(&c.Revision, &c.DefaultProfileID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return c, nil
		}
		return c, err
	}
	rows, err := db.QueryContext(ctx, `SELECT id,key,display_name,executable,version_constraint,required_features_json,parameter_schema_json,enabled,system,created_at,updated_at FROM ai_runtime_clis WHERE org_id=? ORDER BY key`, org)
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
	if err := rows.Close(); err != nil {
		return c, err
	}

	rows, err = db.QueryContext(ctx, `SELECT id,runtime_key,model_id,display_name,compatible_cli_keys_json,default_parameters_json,enabled,context_window,input_cost,output_cost,tier,created_at,updated_at FROM pm_model_catalog WHERE org_id=? ORDER BY runtime_key`, org)
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
	if err := rows.Close(); err != nil {
		return c, err
	}

	rows, err = db.QueryContext(ctx, `SELECT id,key,name,description,cli_key,model_key,parameters_json,enabled,created_at,updated_at FROM ai_runtime_profiles WHERE org_id=? ORDER BY key`, org)
	if err != nil {
		return c, err
	}
	for rows.Next() {
		var x airuntime.RuntimeProfile
		var params, created, updated string
		var enabled int
		if err := rows.Scan(&x.ID, &x.Key, &x.Name, &x.Description, &x.CLIKey, &x.ModelKey, &params, &enabled, &created, &updated); err != nil {
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
	return c, rows.Close()
}

func ReadLegacyMigrationObjects(ctx context.Context, db *sql.DB, org string) ([]airuntime.MigrationObject, error) {
	if strings.TrimSpace(org) == "" {
		return nil, errors.New("org is required")
	}
	var out []airuntime.MigrationObject
	agents, err := readAgentRuntimeObjects(ctx, db, org)
	if err != nil {
		return nil, err
	}
	out = append(out, agents...)
	roles, err := readTeamRoleRuntimeObjects(ctx, db, org)
	if err != nil {
		return nil, err
	}
	out = append(out, roles...)
	tasks, err := readTaskModelRuntimeObjects(ctx, db, org)
	if err != nil {
		return nil, err
	}
	out = append(out, tasks...)
	return out, nil
}

func readAgentRuntimeObjects(ctx context.Context, db *sql.DB, org string) ([]airuntime.MigrationObject, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT id, organization_id, COALESCE(cli,''), COALESCE(model,''),
		       COALESCE(reasoning,''), COALESCE(mode,''), COALESCE(provider,''),
		       COALESCE(orchestrator_model,''), COALESCE(default_executor_model,''),
		       COALESCE(allowed_executors,'[]')
		FROM agents
		WHERE organization_id=?
		ORDER BY id`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []airuntime.MigrationObject
	for rows.Next() {
		var id, orgID, cli, model, reasoning, mode, provider, orchestratorModel, defaultExecutorModel, allowedRaw string
		if err := rows.Scan(&id, &orgID, &cli, &model, &reasoning, &mode, &provider, &orchestratorModel, &defaultExecutorModel, &allowedRaw); err != nil {
			return nil, err
		}
		params := map[string]any{}
		if strings.TrimSpace(reasoning) != "" {
			params["reasoning"] = strings.TrimSpace(reasoning)
		}
		if strings.TrimSpace(mode) != "" {
			params["mode"] = strings.TrimSpace(mode)
		}
		if strings.TrimSpace(provider) != "" {
			params["provider"] = strings.TrimSpace(provider)
		}
		if strings.TrimSpace(cli) != "" || strings.TrimSpace(model) != "" {
			out = append(out, airuntime.MigrationObject{
				OrgID: orgID, ObjectType: "agent_supervisor", ObjectID: id,
				Legacy: airuntime.LegacyRuntime{CLI: cli, Model: model}, Parameters: params,
			})
		}
		if strings.TrimSpace(orchestratorModel) != "" {
			out = append(out, airuntime.MigrationObject{
				OrgID: orgID, ObjectType: "agent_orchestrator", ObjectID: id,
				Legacy: airuntime.LegacyRuntime{CLI: cli, Model: orchestratorModel},
			})
		}
		if strings.TrimSpace(defaultExecutorModel) != "" {
			out = append(out, airuntime.MigrationObject{
				OrgID: orgID, ObjectType: "agent_default_executor", ObjectID: id,
				Legacy: airuntime.LegacyRuntime{CLI: cli, Model: defaultExecutorModel},
			})
		}
		var execs []struct {
			CLI   string `json:"cli"`
			Model string `json:"model"`
		}
		if strings.TrimSpace(allowedRaw) != "" {
			if err := json.Unmarshal([]byte(allowedRaw), &execs); err != nil {
				return nil, fmt.Errorf("agent %s allowed_executors: %w", id, err)
			}
		}
		for i, x := range execs {
			out = append(out, airuntime.MigrationObject{
				OrgID: orgID, ObjectType: "agent_executor_candidate", ObjectID: fmt.Sprintf("%s#%d", id, i),
				Legacy: airuntime.LegacyRuntime{CLI: x.CLI, Model: x.Model},
			})
		}
	}
	return out, rows.Err()
}

func readTeamRoleRuntimeObjects(ctx context.Context, db *sql.DB, org string) ([]airuntime.MigrationObject, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT tr.team_id, t.org_id, tr.role, COALESCE(tr.cli,''), COALESCE(tr.model,'')
		FROM team_roles tr
		JOIN teams t ON t.id=tr.team_id
		WHERE t.org_id=? AND (tr.cli<>'' OR tr.model<>'')
		ORDER BY tr.team_id, tr.role`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []airuntime.MigrationObject
	for rows.Next() {
		var teamID, orgID, role, cli, model string
		if err := rows.Scan(&teamID, &orgID, &role, &cli, &model); err != nil {
			return nil, err
		}
		out = append(out, airuntime.MigrationObject{
			OrgID: orgID, ObjectType: "team_role", ObjectID: teamID + ":" + role,
			Legacy: airuntime.LegacyRuntime{CLI: cli, Model: model},
		})
	}
	return out, rows.Err()
}

func readTaskModelRuntimeObjects(ctx context.Context, db *sql.DB, org string) ([]airuntime.MigrationObject, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT task.id, project.organization_id, COALESCE(task.model,''), COALESCE(agent.cli,'')
		FROM pm_tasks task
		JOIN pm_projects project ON project.id=task.project_id
		LEFT JOIN agents agent
		  ON task.assignee='agent:' || agent.id
		  OR task.assignee='agent:' || agent.identity_member_id
		WHERE project.organization_id=? AND COALESCE(task.model,'')<>''
		ORDER BY task.id`, org)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []airuntime.MigrationObject
	for rows.Next() {
		var taskID, orgID, model, cli string
		if err := rows.Scan(&taskID, &orgID, &model, &cli); err != nil {
			return nil, err
		}
		out = append(out, airuntime.MigrationObject{
			OrgID: orgID, ObjectType: "task_model_override", ObjectID: taskID,
			Legacy: airuntime.LegacyRuntime{CLI: cli, Model: model},
		})
	}
	return out, rows.Err()
}
