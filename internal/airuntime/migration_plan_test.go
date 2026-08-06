package airuntime

import (
	"strings"
	"testing"
	"time"
)

func migrationCatalogFixture() Catalog {
	return Catalog{
		OrgID: "org-1", Revision: 42,
		CLIs: []CLIDefinition{
			{ID: "cli-codex", Key: "codex", Executable: "codex", Enabled: true},
			{ID: "cli-claude", Key: "claude-code", Executable: "claude", Enabled: true},
		},
		Models: []ModelDefinition{
			{ID: "model-gpt", Key: "gpt", ModelKey: "gpt-5", CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{}, Enabled: true},
			{ID: "model-sonnet", Key: "sonnet", ModelKey: "sonnet-5", CompatibleCLIKeys: []string{"claude-code"}, DefaultParameters: map[string]any{}, Enabled: true},
			{ID: "model-opus", Key: "opus", ModelKey: "opus-4", CompatibleCLIKeys: []string{"claude-code"}, DefaultParameters: map[string]any{}, Enabled: true},
		},
		Profiles: []RuntimeProfile{
			{ID: "profile-gpt", Key: "default-codex", Name: "Default Codex", CLIKey: "codex", ModelKey: "gpt", Parameters: map[string]any{}, Enabled: true},
		},
	}
}

func TestMigrationPlannerDryRunClassifiesFourReports(t *testing.T) {
	planner := NewMigrationPlanner()
	planner.now = func() time.Time { return time.Date(2026, 8, 6, 1, 2, 3, 0, time.UTC) }
	report, err := planner.DryRun(migrationCatalogFixture(), []MigrationObject{
		{OrgID: "org-1", ObjectType: "agent_supervisor", ObjectID: "agent-exact", Legacy: LegacyRuntime{CLI: "codex", Model: "gpt-5"}},
		{OrgID: "org-1", ObjectType: "team_role", ObjectID: "team-a:coder", Legacy: LegacyRuntime{CLI: "claude-code", Model: "sonnet-5"}},
		{OrgID: "org-1", ObjectType: "agent_executor_candidate", ObjectID: "agent-a#0", Legacy: LegacyRuntime{CLI: "claude-code", Model: "sonnet-5"}},
		{OrgID: "org-1", ObjectType: "agent_default_executor", ObjectID: "agent-single", Legacy: LegacyRuntime{CLI: "claude-code", Model: "opus-4"}},
		{OrgID: "org-1", ObjectType: "task_model_override", ObjectID: "task-no-cli", Legacy: LegacyRuntime{Model: "gpt-5"}},
		{OrgID: "org-1", ObjectType: "agent_supervisor", ObjectID: "agent-unknown", Legacy: LegacyRuntime{CLI: "codex", Model: "unknown"}},
	}, ResolverStageNewResolverCanary)
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || report.TotalObjects != 6 || report.CatalogRevision != 42 {
		t.Fatalf("report header = %+v", report)
	}
	if report.Counts[MigrationCategoryExactProfile] != 1 ||
		report.Counts[MigrationCategoryDeduplicated] != 2 ||
		report.Counts[MigrationCategoryObjectOverride] != 1 ||
		report.Counts[MigrationCategoryUnmapped] != 2 {
		t.Fatalf("counts = %+v", report.Counts)
	}
	if len(report.ExactProfiles) != 1 || report.ExactProfiles[0].ProfileKey != "default-codex" {
		t.Fatalf("exact profiles = %+v", report.ExactProfiles)
	}
	if len(report.DeduplicatedProfiles) != 1 || len(report.DeduplicatedProfiles[0].Objects) != 2 {
		t.Fatalf("dedup profiles = %+v", report.DeduplicatedProfiles)
	}
	if strings.HasPrefix(report.DeduplicatedProfiles[0].ProposedKey, "migrated-") {
		t.Fatalf("dedup proposed one-time migrated profile: %+v", report.DeduplicatedProfiles[0])
	}
	if len(report.ObjectOverrides) != 1 || report.ObjectOverrides[0].Selection.Mode != SelectionOverride {
		t.Fatalf("overrides = %+v", report.ObjectOverrides)
	}
	if len(report.Unmapped) != 2 {
		t.Fatalf("unmapped = %+v", report.Unmapped)
	}
	for _, u := range report.Unmapped {
		if strings.HasPrefix(u.Object.ObjectID, "migrated-") {
			t.Fatalf("unmapped object generated migrated profile-like id: %+v", u)
		}
	}
	if report.IdempotencyDigestSHA256 == "" {
		t.Fatal("missing idempotency digest")
	}
	if got := report.CutoverEvidence[len(report.CutoverEvidence)-1].Stage; got != ResolverStageNewResolverCanary {
		t.Fatalf("cutover evidence stops at %s", got)
	}
}

func TestMigrationPlannerDryRunIsIdempotentIgnoringGeneratedAt(t *testing.T) {
	planner := NewMigrationPlanner()
	at := time.Date(2026, 8, 6, 1, 0, 0, 0, time.UTC)
	planner.now = func() time.Time { return at }
	objects := []MigrationObject{
		{OrgID: "org-1", ObjectType: "agent_supervisor", ObjectID: "agent-a", Legacy: LegacyRuntime{CLI: "codex", Model: "gpt-5"}},
	}
	first, err := planner.DryRun(migrationCatalogFixture(), objects, ResolverStageShadowCompare)
	if err != nil {
		t.Fatal(err)
	}
	planner.now = func() time.Time { return at.Add(time.Hour) }
	second, err := planner.DryRun(migrationCatalogFixture(), objects, ResolverStageShadowCompare)
	if err != nil {
		t.Fatal(err)
	}
	if first.IdempotencyDigestSHA256 != second.IdempotencyDigestSHA256 {
		t.Fatalf("digest drift: %s != %s", first.IdempotencyDigestSHA256, second.IdempotencyDigestSHA256)
	}
}
