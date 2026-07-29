package airuntime_test

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/airuntime"
	airuntimesql "github.com/oopslink/agent-center/internal/airuntime/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestBundleRoundTripTokenBindingAndRevisionConflict(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/bundle.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	nextID := 0
	svc := airuntime.NewService(airuntimesql.NewRepository(db), func() string {
		nextID++
		return "bundle-" + string(rune('a'+nextID))
	})
	ctx := context.Background()
	model, rev, err := svc.CreateModel(ctx, "source", "user:admin", 0, airuntime.ModelDefinition{
		Key: "gpt", ModelKey: "gpt-5", DisplayName: "GPT", CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{"effort": "high"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	modelRevision := rev
	profile, _, err := svc.CreateProfile(ctx, "source", "user:admin", modelRevision, airuntime.RuntimeProfile{
		Key: "coding", Name: "Coding", CLIKey: "codex", ModelKey: model.Key,
		Parameters: map[string]any{"secret_token": map[string]any{"secret_ref": "secret://runtime/key"}}, Enabled: true,
	})
	if err == nil {
		t.Fatal("unmarked secret-looking parameter must be rejected by the CLI schema")
	}
	profile, rev, err = svc.CreateProfile(ctx, "source", "user:admin", modelRevision, airuntime.RuntimeProfile{
		Key: "coding", Name: "Coding", CLIKey: "codex", ModelKey: model.Key, Parameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetDefaultProfile(ctx, "source", "user:admin", profile.ID, rev); err != nil {
		t.Fatal(err)
	}
	exported, err := svc.Export(ctx, "source", "json")
	if err != nil {
		t.Fatal(err)
	}
	text := string(exported)
	for _, forbidden := range []string{`"id"`, `"created_at"`, `"updated_at"`, `"secret_token"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("export contains forbidden field %s: %s", forbidden, text)
		}
	}
	preview, err := svc.PreviewImport(ctx, "target", "merge", exported)
	if err != nil {
		t.Fatal(err)
	}
	if preview.ValidationToken == "" || preview.BundleDigest == "" || len(preview.Changes) == 0 {
		t.Fatalf("preview = %+v", preview)
	}
	applied, err := svc.ApplyImport(ctx, "target", "user:admin", "merge", exported, preview.ValidationToken)
	if err != nil {
		t.Fatal(err)
	}
	if applied.Revision != 1 || applied.DefaultProfileID == "" {
		t.Fatalf("applied = %+v", applied)
	}
	second, err := svc.Export(ctx, "target", "json")
	if err != nil {
		t.Fatal(err)
	}
	var firstJSON, secondJSON any
	if json.Unmarshal(exported, &firstJSON) != nil || json.Unmarshal(second, &secondJSON) != nil {
		t.Fatal("invalid JSON export")
	}
	firstCanonical, _ := airuntime.CanonicalJSON(firstJSON)
	secondCanonical, _ := airuntime.CanonicalJSON(secondJSON)
	if string(firstCanonical) != string(secondCanonical) {
		t.Fatalf("round trip changed semantics:\n%s\n%s", firstCanonical, secondCanonical)
	}

	stale, err := svc.PreviewImport(ctx, "target", "merge", exported)
	if err != nil {
		t.Fatal(err)
	}
	catalog, _ := svc.Catalog(ctx, "target")
	if _, _, err := svc.CreateModel(ctx, "target", "user:other", catalog.Revision, airuntime.ModelDefinition{
		Key: "other", ModelKey: "other", CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{}, Enabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyImport(ctx, "target", "user:admin", "merge", exported, stale.ValidationToken); err == nil {
		t.Fatal("stale validation token must fail after catalog revision changes")
	}
	if _, err := svc.ApplyImport(ctx, "other-org", "user:admin", "merge", exported, stale.ValidationToken); err == nil {
		t.Fatal("validation token must be bound to organization")
	}
}

func TestBundleRejectsUnknownAndUnsupportedSchemaVersion(t *testing.T) {
	db, _ := persistence.Open(t.TempDir() + "/bundle.db")
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	svc := airuntime.NewService(airuntimesql.NewRepository(db), func() string { return "id" })
	for _, raw := range []string{
		`{"schema_version":2,"kind":"agent-center-ai-runtime","runtime":{}}`,
		`{"schema_version":1,"kind":"agent-center-ai-runtime","critical_unknown":true,"runtime":{}}`,
	} {
		if _, err := svc.PreviewImport(context.Background(), "org", "merge", []byte(raw)); err == nil {
			t.Fatalf("unsupported bundle accepted: %s", raw)
		}
	}
}

func TestBundleReplaceClearsMissingDefaultAndDisablesItsProfile(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/replace.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	nextID := 0
	svc := airuntime.NewService(airuntimesql.NewRepository(db), func() string {
		nextID++
		return fmt.Sprintf("replace-%d", nextID)
	})
	ctx := context.Background()
	model, rev, err := svc.CreateModel(ctx, "org", "user:admin", 0, airuntime.ModelDefinition{
		Key: "gpt", ModelKey: "gpt", CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, rev, err := svc.CreateProfile(ctx, "org", "user:admin", rev, airuntime.RuntimeProfile{
		Key: "old-default", Name: "Old default", CLIKey: "codex", ModelKey: model.Key,
		Parameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetDefaultProfile(ctx, "org", "user:admin", profile.ID, rev); err != nil {
		t.Fatal(err)
	}

	replacement := []byte(`{
		"schema_version":1,
		"kind":"agent-center-ai-runtime",
		"runtime":{"clis":[],"models":[],"profiles":[]}
	}`)
	preview, err := svc.PreviewImport(ctx, "org", "replace", replacement)
	if err != nil {
		t.Fatal(err)
	}
	applied, err := svc.ApplyImport(ctx, "org", "user:admin", "replace", replacement, preview.ValidationToken)
	if err != nil {
		t.Fatal(err)
	}
	if applied.DefaultProfileID != "" {
		t.Fatalf("default profile = %q want cleared", applied.DefaultProfileID)
	}
	for _, candidate := range applied.Profiles {
		if candidate.ID == profile.ID && candidate.Enabled {
			t.Fatalf("missing previous default profile remained enabled: %+v", candidate)
		}
	}
}
