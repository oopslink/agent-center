package sqlite_test

import (
	"context"
	"testing"

	airuntimesql "github.com/oopslink/agent-center/internal/airuntime/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestReadCatalogDoesNotSeedMissingOrg(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/runtime.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := airuntimesql.ReadCatalog(context.Background(), db, "org-missing"); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_catalogs WHERE org_id='org-missing'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("read-only catalog seeded %d rows", count)
	}
}

func TestReadLegacyMigrationObjectsEnumeratesRuntimeSurfaces(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/runtime.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO agents
			(id, organization_id, name, model, cli, reasoning, orchestrator_model,
			 default_executor_model, allowed_executors, worker_id, lifecycle, created_by,
			 identity_member_id, created_at, updated_at)
		VALUES
			('agent-a','org-a','Agent A','gpt-5','codex','high','gpt-router',
			 'gpt-exec','[{"cli":"codex","model":"gpt-5"},{"cli":"claude-code","model":"sonnet-5"}]',
			 'worker-a','stopped','user:owner','member-a','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
		INSERT INTO teams(id, org_id, name, created_at, updated_at)
		VALUES('team-a','org-a','Team A','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
		INSERT INTO team_roles(team_id, role, cli, model, capability_tags, max_concurrency, created_at)
		VALUES('team-a','coder','claude-code','sonnet-5','[]',1,'2026-01-01T00:00:00Z');
		INSERT INTO pm_projects(id, organization_id, name, status, created_by, created_at, updated_at)
		VALUES('proj-a','org-a','Project A','active','user:owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
		INSERT INTO pm_tasks(id, project_id, title, status, assignee, model, created_by, created_at, updated_at)
		VALUES('task-a','proj-a','Task A','open','agent:member-a','gpt-5','user:owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
	got, err := airuntimesql.ReadLegacyMigrationObjects(context.Background(), db, "org-a")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, object := range got {
		seen[object.ObjectType+"/"+object.ObjectID] = true
	}
	for _, want := range []string{
		"agent_supervisor/agent-a",
		"agent_orchestrator/agent-a",
		"agent_default_executor/agent-a",
		"agent_executor_candidate/agent-a#0",
		"agent_executor_candidate/agent-a#1",
		"team_role/team-a:coder",
		"task_model_override/task-a",
	} {
		if !seen[want] {
			t.Fatalf("missing %s in objects: %+v", want, got)
		}
	}
}
