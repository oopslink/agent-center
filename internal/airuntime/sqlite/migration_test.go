package sqlite

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/airuntime"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestLegacyMigrationSQLiteApplyIsDedupedAndAudited(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/stage5.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if err := persistence.NewMigrator(db).Up(ctx); err != nil {
		t.Fatal(err)
	}
	repo := NewRepository(db)
	var n int
	svc := airuntime.NewServiceWithValidationKey(repo, func() string {
		n++
		return "runtime-id-" + strings.Repeat("x", n)
	}, []byte("0123456789abcdef0123456789abcdef"))
	if _, _, err := svc.CreateModel(ctx, "org", "user:admin", 0, airuntime.ModelDefinition{
		Key: "gpt", ModelKey: "gpt-5", DisplayName: "GPT",
		CompatibleCLIKeys: []string{"codex"}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CreateProfile(ctx, "org", "user:admin", 1, airuntime.RuntimeProfile{
		Key: "default-coding", Name: "Default coding", CLIKey: "codex", ModelKey: "gpt",
		Parameters: map[string]any{}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	insertAgent(t, db, "agent-a", "", "")
	insertAgent(t, db, "agent-b", "high", "")
	insertAgent(t, db, "agent-c", "high", "")
	insertAgent(t, db, "agent-d", "low", "")
	insertAgent(t, db, "agent-e", "", "unknown-model")

	dry, err := svc.LegacyMigrationDryRun(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	if dry.Summary.ProfilesToCreate != 1 || dry.Summary.ObjectSelectionsToWrite != 4 || dry.Summary.Unmapped != 1 {
		t.Fatalf("dry summary=%+v", dry.Summary)
	}
	applied, err := svc.ApplyLegacyMigration(ctx, "org", "user:admin", airuntime.ApplyMigrationRequest{
		ExpectedRevision: dry.Revision,
		PlanSHA256:       dry.PlanSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.Revision != dry.Revision+1 {
		t.Fatalf("applied=%+v dry revision=%d", applied, dry.Revision)
	}
	var sharedProfiles int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_profiles WHERE org_id='org' AND key LIKE 'migrated-shared-%'`).Scan(&sharedProfiles); err != nil {
		t.Fatal(err)
	}
	if sharedProfiles != 1 {
		t.Fatalf("shared migrated profiles=%d want 1", sharedProfiles)
	}
	var selections int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_object_selections WHERE org_id='org'`).Scan(&selections); err != nil {
		t.Fatal(err)
	}
	if selections != 4 {
		t.Fatalf("object selections=%d want 4", selections)
	}
	var agentDSelection string
	if err := db.QueryRow(`SELECT selection_json FROM ai_runtime_object_selections WHERE org_id='org' AND object_id='agent-d'`).Scan(&agentDSelection); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(agentDSelection, `"mode":"override"`) {
		t.Fatalf("agent-d must stay object override, selection=%s", agentDSelection)
	}
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_audit_log WHERE org_id='org' AND action='legacy_migration_applied'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 1 {
		t.Fatalf("audit rows=%d want 1", audits)
	}
}

func insertAgent(t *testing.T, db *sql.DB, id, reasoning, model string) {
	t.Helper()
	if model == "" {
		model = "gpt-5"
	}
	_, err := db.Exec(`
		INSERT INTO agents(id, organization_id, name, model, cli, reasoning, worker_id, lifecycle, created_by, created_at, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		id, "org", id, model, "codex", nullForEmpty(reasoning), "worker-1", "stopped", "user:admin",
		"2026-08-06T00:00:00Z", "2026-08-06T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
}

func nullForEmpty(v string) any {
	if v == "" {
		return nil
	}
	return v
}
