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
