package airuntime

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

type migrationRepo struct {
	catalog    Catalog
	objects    []MigrationObject
	selections []ObjectSelection
	settings   map[string]string

	applied MigrationMutation
	shadow  ShadowCompareMutation
	cutover CutoverMutation
}

func (r *migrationRepo) GetCatalog(context.Context, string) (Catalog, error) { return r.catalog, nil }
func (*migrationRepo) CreateCLI(context.Context, CLIDefinition, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*migrationRepo) UpdateCLI(context.Context, CLIDefinition, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*migrationRepo) CreateModel(context.Context, ModelDefinition, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*migrationRepo) UpdateModel(context.Context, ModelDefinition, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*migrationRepo) CreateProfile(context.Context, RuntimeProfile, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*migrationRepo) UpdateProfile(context.Context, RuntimeProfile, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*migrationRepo) SetDefaultProfile(context.Context, string, string, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (r *migrationRepo) ListLegacyRuntimeObjects(context.Context, string) ([]MigrationObject, error) {
	return append([]MigrationObject(nil), r.objects...), nil
}
func (r *migrationRepo) ListObjectSelections(_ context.Context, _ string, objectType string) ([]ObjectSelection, error) {
	var out []ObjectSelection
	for _, selection := range r.selections {
		if selection.ObjectType == objectType {
			out = append(out, selection)
		}
	}
	return out, nil
}
func (r *migrationRepo) ApplyLegacyMigration(_ context.Context, m MigrationMutation, expected int64, _ AuditEvent) (int64, error) {
	if expected != r.catalog.Revision {
		return 0, &Error{Reason: ReasonRevisionConflict, Message: "catalog revision changed"}
	}
	r.applied = m
	r.catalog.Profiles = append(r.catalog.Profiles, m.Profiles...)
	r.selections = append(r.selections, m.ObjectSelections...)
	r.catalog.Revision = expected + 1
	return expected + 1, nil
}
func (r *migrationRepo) RecordShadowComparisons(_ context.Context, m ShadowCompareMutation) error {
	r.shadow = m
	return nil
}
func (r *migrationRepo) GetCutoverSettings(context.Context, string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range r.settings {
		out[k] = v
	}
	return out, nil
}
func (r *migrationRepo) ApplyCutover(_ context.Context, m CutoverMutation) error {
	if r.settings == nil {
		r.settings = map[string]string{}
	}
	for _, flag := range m.Flags {
		r.settings[flag.Key] = flag.After
	}
	r.cutover = m
	return nil
}

func migrationFixture() (*Service, *migrationRepo) {
	repo := &migrationRepo{
		settings: map[string]string{},
		catalog: Catalog{
			OrgID: "org", Revision: 7, DefaultProfileID: "prof-default",
			CLIs: []CLIDefinition{{
				ID: "cli-codex", Key: "codex", Executable: "codex", Enabled: true,
				ParameterSchema: []byte(`{"type":"object"}`),
			}},
			Models: []ModelDefinition{{
				ID: "model-gpt", Key: "gpt", ModelKey: "gpt-5", Enabled: true,
				CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{},
			}},
			Profiles: []RuntimeProfile{{
				ID: "prof-default", Key: "default-coding", Name: "Default coding",
				CLIKey: "codex", ModelKey: "gpt", Parameters: map[string]any{}, Enabled: true,
			}},
		},
		objects: []MigrationObject{
			{OrgID: "org", ObjectType: "agent", ObjectID: "agent-a", Legacy: LegacyRuntime{CLI: "codex", Model: "gpt-5"}},
			{OrgID: "org", ObjectType: "agent", ObjectID: "agent-b", Legacy: LegacyRuntime{CLI: "codex", Model: "gpt-5"}, Parameters: map[string]any{"reasoning": "high"}},
			{OrgID: "org", ObjectType: "agent", ObjectID: "agent-c", Legacy: LegacyRuntime{CLI: "codex", Model: "gpt-5"}, Parameters: map[string]any{"reasoning": "high"}},
			{OrgID: "org", ObjectType: "agent", ObjectID: "agent-d", Legacy: LegacyRuntime{CLI: "codex", Model: "gpt-5"}, Parameters: map[string]any{"reasoning": "low"}},
			{OrgID: "org", ObjectType: "agent", ObjectID: "agent-e", Legacy: LegacyRuntime{CLI: "codex", Model: "unknown"}},
		},
	}
	var n int
	svc := NewServiceWithValidationKey(repo, func() string {
		n++
		return "id-" + string(rune('a'+n))
	}, []byte("0123456789abcdef0123456789abcdef"))
	now := time.Date(2026, 8, 6, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	return svc, repo
}

func TestLegacyMigrationDryRunClassifiesWithoutOneOffProfiles(t *testing.T) {
	svc, _ := migrationFixture()
	report, err := svc.LegacyMigrationDryRun(context.Background(), "org")
	if err != nil {
		t.Fatal(err)
	}
	if report.Summary.ExactMappings != 1 || report.Summary.DeduplicatedProfiles != 1 ||
		report.Summary.DeduplicatedObjects != 2 || report.Summary.ObjectOverrides != 1 ||
		report.Summary.Unmapped != 1 || report.Summary.ProfilesToCreate != 1 {
		t.Fatalf("summary=%+v", report.Summary)
	}
	if got := report.ExactMappings[0]; got.ObjectID != "agent-a" || got.ProfileKey != "default-coding" || got.Selection.Mode != SelectionProfile {
		t.Fatalf("exact mapping=%+v", got)
	}
	dedup := report.DeduplicatedProfiles[0]
	if !strings.HasPrefix(dedup.ProfileKey, "migrated-shared-") || len(dedup.ObjectIDs) != 2 {
		t.Fatalf("dedupe profile=%+v", dedup)
	}
	override := report.ObjectOverrides[0]
	if override.ObjectID != "agent-d" || override.Selection.Mode != SelectionOverride || override.ProfileKey != "" {
		t.Fatalf("single-object runtime must remain an override, got %+v", override)
	}
	if report.Unmapped[0].ObjectID != "agent-e" || report.Unmapped[0].Reason != ReasonLegacyUnmapped {
		t.Fatalf("unmapped=%+v", report.Unmapped)
	}
	if report.PlanSHA256 == "" {
		t.Fatal("dry-run must include a stable plan digest")
	}
}

func TestApplyLegacyMigrationWritesSharedProfilesAndSelections(t *testing.T) {
	svc, repo := migrationFixture()
	dry, err := svc.LegacyMigrationDryRun(context.Background(), "org")
	if err != nil {
		t.Fatal(err)
	}
	applied, err := svc.ApplyLegacyMigration(context.Background(), "org", "user:admin", ApplyMigrationRequest{
		ExpectedRevision: 7,
		PlanSHA256:       dry.PlanSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !applied.Applied || applied.Revision != 8 || len(repo.applied.Profiles) != 1 || len(repo.applied.ObjectSelections) != 4 {
		t.Fatalf("applied=%+v mutation profiles=%d selections=%d", applied, len(repo.applied.Profiles), len(repo.applied.ObjectSelections))
	}
	if got := repo.applied.Profiles[0].ModelKey; got != "gpt" {
		t.Fatalf("shared profile model key=%q want catalog key gpt", got)
	}
	for _, selection := range repo.applied.ObjectSelections {
		if selection.ObjectID == "agent-e" {
			t.Fatal("unmapped object must not receive a generated selection")
		}
		if selection.ObjectID == "agent-d" && selection.Selection.Mode != SelectionOverride {
			t.Fatalf("agent-d selection=%+v, want object override", selection.Selection)
		}
	}
	_, err = svc.ApplyLegacyMigration(context.Background(), "org", "user:admin", ApplyMigrationRequest{
		ExpectedRevision: 7,
		PlanSHA256:       dry.PlanSHA256 + "x",
	})
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != ReasonMigrationPlanChanged {
		t.Fatalf("changed plan error=%v", err)
	}
}

func TestLegacyMigrationApplyDryRunApplyIsIdempotent(t *testing.T) {
	svc, repo := migrationFixture()
	ctx := context.Background()

	firstDryRun, err := svc.LegacyMigrationDryRun(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	firstApply, err := svc.ApplyLegacyMigration(ctx, "org", "user:admin", ApplyMigrationRequest{
		ExpectedRevision: firstDryRun.Revision,
		PlanSHA256:       firstDryRun.PlanSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !firstApply.Applied || firstApply.Revision != 8 {
		t.Fatalf("first apply=%+v", firstApply)
	}

	secondDryRun, err := svc.LegacyMigrationDryRun(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	if secondDryRun.Revision != 8 || secondDryRun.Summary.ProfilesToCreate != 0 || secondDryRun.Summary.ObjectSelectionsToWrite != 0 {
		t.Fatalf("second dry-run must plan zero mutations: %+v", secondDryRun)
	}
	beforeProfiles, beforeSelections := len(repo.catalog.Profiles), len(repo.selections)
	secondApply, err := svc.ApplyLegacyMigration(ctx, "org", "user:admin", ApplyMigrationRequest{
		ExpectedRevision: secondDryRun.Revision,
		PlanSHA256:       secondDryRun.PlanSHA256,
	})
	if err != nil {
		t.Fatal(err)
	}
	if secondApply.Applied || secondApply.Revision != secondDryRun.Revision {
		t.Fatalf("second apply must be a no-op: %+v", secondApply)
	}
	if len(repo.catalog.Profiles) != beforeProfiles || len(repo.selections) != beforeSelections {
		t.Fatalf("second apply mutated repository: profiles %d→%d selections %d→%d",
			beforeProfiles, len(repo.catalog.Profiles), beforeSelections, len(repo.selections))
	}
}

func TestShadowCompareRecordsEvidenceAndCutoverFlagsRollback(t *testing.T) {
	svc, repo := migrationFixture()
	repo.objects = repo.objects[:2]
	repo.selections = []ObjectSelection{{
		ObjectType: "agent", ObjectID: "agent-a",
		Selection: RuntimeSelection{Mode: SelectionProfile, ProfileID: "default-coding"},
	}}
	shadow, err := svc.ShadowCompare(context.Background(), "org", ShadowCompareRequest{ObjectType: "agent"})
	if err != nil {
		t.Fatal(err)
	}
	if shadow.Compared != 2 || shadow.Matched != 1 || shadow.DiffCount != 1 || len(repo.shadow.Records) != 2 {
		t.Fatalf("shadow=%+v records=%d", shadow, len(repo.shadow.Records))
	}
	if repo.shadow.Records[0].Difference.Legacy.ParametersSHA256 == "" {
		t.Fatalf("shadow evidence must store parameter digest, got %+v", repo.shadow.Records[0].Difference.Legacy)
	}

	cut, err := svc.ApplyCutover(context.Background(), "org", "user:admin", CutoverRequest{
		Stage: CutoverStageAgentScope, Enabled: true, ObjectIDs: []string{"agent-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if repo.settings["ai_runtime.org.agent_resolver"] != "allowlist" ||
		repo.settings["ai_runtime.org.agent_resolver_allowlist"] != "agent-a" ||
		len(cut.RollbackFlags) != 2 {
		t.Fatalf("cutover=%+v settings=%+v", cut, repo.settings)
	}
	snapshot, path, err := svc.ResolveObject(context.Background(), "org", ResolveObjectRequest{
		ObjectType: "agent", ObjectID: "agent-a", Legacy: LegacyRuntime{CLI: "codex", Model: "gpt-5"},
	})
	if err != nil || path != "new" || snapshot.Source != "profile" {
		t.Fatalf("allowlisted resolve snapshot=%+v path=%s err=%v", snapshot, path, err)
	}
	_, path, err = svc.ResolveObject(context.Background(), "org", ResolveObjectRequest{
		ObjectType: "agent", ObjectID: "agent-b", Legacy: LegacyRuntime{CLI: "codex", Model: "gpt-5"},
	})
	if err != nil || path != "legacy" {
		t.Fatalf("non-allowlisted resolve path=%s err=%v", path, err)
	}
	_, err = svc.ApplyCutover(context.Background(), "org", "user:admin", CutoverRequest{Stage: CutoverStageRollback})
	if err != nil {
		t.Fatal(err)
	}
	if repo.settings["ai_runtime.org.agent_resolver"] != "legacy" ||
		repo.settings["ai_runtime.org.org_default_resolver"] != "legacy" {
		t.Fatalf("rollback settings=%+v", repo.settings)
	}
}
