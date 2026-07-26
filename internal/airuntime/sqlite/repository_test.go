package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"github.com/oopslink/agent-center/internal/airuntime"
	airuntimesql "github.com/oopslink/agent-center/internal/airuntime/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestCatalogLifecycleAndRevision(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/runtime.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := 0
	svc := airuntime.NewService(airuntimesql.NewRepository(db), func() string { n++; return fmt.Sprintf("id-%d", n) })
	ctx := context.Background()
	catalog, err := svc.Catalog(ctx, "org-a")
	if err != nil {
		t.Fatal(err)
	}
	if catalog.Revision != 0 || len(catalog.CLIs) != 2 {
		t.Fatalf("initial catalog = rev %d, clis %d", catalog.Revision, len(catalog.CLIs))
	}
	model, rev, err := svc.CreateModel(ctx, "org-a", "user:owner", 0, airuntime.ModelDefinition{Key: "gpt-5", ModelKey: "gpt-5", DisplayName: "GPT-5", CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{}, Enabled: true})
	if err != nil || rev != 1 {
		t.Fatalf("create model: rev=%d err=%v", rev, err)
	}
	profile, rev, err := svc.CreateProfile(ctx, "org-a", "user:owner", rev, airuntime.RuntimeProfile{Key: "default-coding", Name: "Default coding", CLIKey: "codex", ModelKey: model.Key, Parameters: map[string]any{}, Enabled: true})
	if err != nil || rev != 2 {
		t.Fatalf("create profile: rev=%d err=%v", rev, err)
	}
	rev, err = svc.SetDefaultProfile(ctx, "org-a", "user:owner", profile.ID, rev)
	if err != nil || rev != 3 {
		t.Fatalf("set default: rev=%d err=%v", rev, err)
	}
	model.Enabled = false
	if _, _, err := svc.UpdateModel(ctx, "org-a", "user:owner", rev, model); err == nil {
		t.Fatal("disabling a model referenced by an enabled profile must fail")
	}
	codex := catalog.CLIs[1]
	if codex.Key != "codex" {
		t.Fatalf("unexpected CLI ordering: %+v", catalog.CLIs)
	}
	codex.Enabled = false
	if _, _, err := svc.UpdateCLI(ctx, "org-a", "user:owner", rev, codex); err == nil {
		t.Fatal("disabling a CLI referenced by an enabled profile must fail")
	}
	profile.Enabled = false
	if _, _, err := svc.UpdateProfile(ctx, "org-a", "user:owner", rev, profile); err == nil {
		t.Fatal("disabling the default profile must fail")
	}
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_audit_log WHERE org_id='org-a'`).Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 3 {
		t.Fatalf("audit events=%d want 3", audits)
	}
	_, _, err = svc.CreateCLI(ctx, "org-a", "user:owner", 2, airuntime.CLIDefinition{Key: "custom", DisplayName: "Custom", Executable: "custom", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonRevisionConflict {
		t.Fatalf("stale write = %v", err)
	}
	other, err := svc.Catalog(ctx, "org-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(other.Models) != 0 || other.DefaultProfileID != "" {
		t.Fatalf("org isolation failed: %+v", other)
	}
}

func TestProfileRejectsDisabledCatalogEntries(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/runtime.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := 0
	svc := airuntime.NewService(airuntimesql.NewRepository(db), func() string { n++; return fmt.Sprintf("disabled-%d", n) })
	ctx := context.Background()
	model, rev, err := svc.CreateModel(ctx, "org", "user:a", 0, airuntime.ModelDefinition{
		Key: "disabled-model", ModelKey: "disabled-model", CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{}, Enabled: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.CreateProfile(ctx, "org", "user:a", rev, airuntime.RuntimeProfile{
		Key: "bad", Name: "Bad", CLIKey: "codex", ModelKey: model.Key, Enabled: true,
	})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonProfileDisabled {
		t.Fatalf("disabled model profile error = %v", err)
	}
}

func TestCatalogUpdatesRevalidateDependentProfiles(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/runtime.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := 0
	svc := airuntime.NewService(airuntimesql.NewRepository(db), func() string {
		n++
		return fmt.Sprintf("cascade-%d", n)
	})
	ctx := context.Background()
	catalog, err := svc.Catalog(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	var codex airuntime.CLIDefinition
	for _, cli := range catalog.CLIs {
		if cli.Key == "codex" {
			codex = cli
		}
	}
	codex.ParameterSchema = json.RawMessage(`{"type":"object","properties":{"retries":{"type":"integer","maximum":3}},"additionalProperties":false}`)
	codex, rev, err := svc.UpdateCLI(ctx, "org", "user:a", 0, codex)
	if err != nil {
		t.Fatal(err)
	}
	model, rev, err := svc.CreateModel(ctx, "org", "user:a", rev, airuntime.ModelDefinition{
		Key: "gpt", ModelKey: "gpt", CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{"retries": float64(2)}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, rev, err = svc.CreateProfile(ctx, "org", "user:a", rev, airuntime.RuntimeProfile{
		Key: "coding", Name: "Coding", CLIKey: "codex", ModelKey: model.Key,
		Parameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	tighterCLI := codex
	tighterCLI.ParameterSchema = json.RawMessage(`{"type":"object","properties":{"retries":{"type":"integer","maximum":1}},"additionalProperties":false}`)
	if _, _, err := svc.UpdateCLI(ctx, "org", "user:a", rev, tighterCLI); err == nil {
		t.Fatal("CLI schema update must reject an invalid dependent model/profile")
	}

	incompatibleModel := model
	incompatibleModel.CompatibleCLIKeys = []string{"claude-code"}
	if _, _, err := svc.UpdateModel(ctx, "org", "user:a", rev, incompatibleModel); err == nil {
		t.Fatal("model compatibility update must reject an invalid dependent profile")
	}

	invalidDefaults := model
	invalidDefaults.DefaultParameters = map[string]any{"retries": float64(4)}
	if _, _, err := svc.UpdateModel(ctx, "org", "user:a", rev, invalidDefaults); err == nil {
		t.Fatal("model defaults update must reject an invalid dependent profile")
	}
}

func TestProfileRejectsIncompatibleModel(t *testing.T) {
	db, _ := persistence.Open(t.TempDir() + "/runtime.db")
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	n := 0
	svc := airuntime.NewService(airuntimesql.NewRepository(db), func() string { n++; return fmt.Sprintf("x-%d", n) })
	ctx := context.Background()
	model, rev, err := svc.CreateModel(ctx, "org", "user:a", 0, airuntime.ModelDefinition{Key: "claude", ModelKey: "claude", CompatibleCLIKeys: []string{"claude-code"}, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = svc.CreateProfile(ctx, "org", "user:a", rev, airuntime.RuntimeProfile{Key: "bad", Name: "Bad", CLIKey: "codex", ModelKey: model.Key, Enabled: true})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonIncompatible {
		t.Fatalf("got %v", err)
	}
}
