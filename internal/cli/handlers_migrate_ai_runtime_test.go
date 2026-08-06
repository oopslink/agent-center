package cli

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/airuntime"
	"github.com/oopslink/agent-center/internal/persistence"
)

func seedAIRuntimeDryRunDB(t *testing.T, dbPath string) {
	t.Helper()
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		INSERT INTO ai_runtime_catalogs(org_id, revision, default_profile_id)
		VALUES('org-a', 3, 'profile-gpt');
		INSERT INTO ai_runtime_clis
			(id, org_id, key, display_name, executable, parameter_schema_json, enabled, system, created_at, updated_at)
		VALUES
			('cli-codex','org-a','codex','Codex','codex','{"type":"object"}',1,1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
		INSERT INTO pm_model_catalog
			(id, org_id, model_id, display_name, created_by, created_at, updated_at, version,
			 runtime_key, compatible_cli_keys_json, default_parameters_json, enabled)
		VALUES
			('model-gpt','org-a','gpt-5','GPT-5','user:owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z',1,
			 'gpt','["codex"]','{}',1);
		INSERT INTO ai_runtime_profiles
			(id, org_id, key, name, cli_key, model_key, parameters_json, enabled, created_at, updated_at)
		VALUES
			('profile-gpt','org-a','default-codex','Default Codex','codex','gpt','{}',1,'2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
		INSERT INTO agents
			(id, organization_id, name, model, cli, worker_id, lifecycle, created_by, created_at, updated_at)
		VALUES
			('agent-a','org-a','Agent A','gpt-5','codex','worker-a','stopped','user:owner','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z');
	`)
	if err != nil {
		t.Fatal(err)
	}
}

func TestMigrateAIRuntimeDryRunJSONAndReadOnly(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "runtime.db")
	cfgPath := filepath.Join(dir, "cfg.yaml")
	writeMigrateCfg(t, cfgPath, dbPath)
	seedAIRuntimeDryRunDB(t, dbPath)

	cmd := MigrateAIRuntimeCommand()
	stdout, stderr, code := runHandler(t, cmd, []string{
		"--config=" + cfgPath, "--org=org-a", "--dry-run", "--format=json",
	})
	if code != ExitOK {
		t.Fatalf("code=%d stderr=%s stdout=%s", code, stderr, stdout)
	}
	var report airuntime.MigrationDryRunReport
	if err := json.Unmarshal([]byte(stdout), &report); err != nil {
		t.Fatalf("json report: %v\n%s", err, stdout)
	}
	if report.Counts[airuntime.MigrationCategoryExactProfile] != 1 || report.TotalObjects != 1 {
		t.Fatalf("report = %+v", report)
	}
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_audit_log WHERE org_id='org-a'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 0 {
		t.Fatalf("dry-run wrote audit rows: %d", audits)
	}
}

func TestMigrateAIRuntimeRequiresDryRun(t *testing.T) {
	cmd := MigrateAIRuntimeCommand()
	_, stderr, code := runHandler(t, cmd, []string{"--org=org-a"})
	if code != ExitUsage || !strings.Contains(stderr, "requires --dry-run") {
		t.Fatalf("code=%d stderr=%s", code, stderr)
	}
}
