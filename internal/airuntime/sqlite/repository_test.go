package sqlite_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/airuntime"
	airuntimesql "github.com/oopslink/agent-center/internal/airuntime/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestAgentRuntimeSelectionRoundTripAndValidation(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/selection.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO agents(id,organization_id,name,worker_id,lifecycle,created_by,created_at,updated_at) VALUES('agent-a','org-a','A','worker-a','stopped','user:x','2026-01-01T00:00:00Z','2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	repo := airuntimesql.NewRepository(db)
	if err := repo.PutAgentSelection(context.Background(), "org-a", "agent-a", airuntime.RuntimeSelection{Mode: airuntime.SelectionInherit}, time.Unix(10, 0)); err != nil {
		t.Fatal(err)
	}
	got, ok, err := repo.GetAgentSelection(context.Background(), "org-a", "agent-a")
	if err != nil || !ok || got.Mode != airuntime.SelectionInherit {
		t.Fatalf("selection = %+v ok=%v err=%v", got, ok, err)
	}
	if err := repo.PutAgentSelection(context.Background(), "org-a", "agent-a", airuntime.RuntimeSelection{Mode: airuntime.SelectionProfile, ProfileID: "missing"}, time.Now()); err == nil {
		t.Fatal("missing explicit profile must fail closed")
	}
}

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

func TestExecutionSnapshotIsByteStableAcrossCatalogChangeAndRetry(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/runtime.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	next := 0
	repo := airuntimesql.NewRepository(db)
	svc := airuntime.NewService(repo, func() string {
		next++
		return fmt.Sprintf("freeze-%d", next)
	})
	resolver := airuntime.NewRuntimeResolver(repo)
	ctx := context.Background()
	model, rev, err := svc.CreateModel(ctx, "org", "user:a", 0, airuntime.ModelDefinition{
		Key: "gpt", ModelKey: "gpt-5", CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{"effort": "medium"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, rev, err := svc.CreateProfile(ctx, "org", "user:a", rev, airuntime.RuntimeProfile{
		Key: "coding", Name: "Coding", CLIKey: "codex", ModelKey: model.Key,
		Parameters: map[string]any{"effort": "high"}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rev, err = svc.SetDefaultProfile(ctx, "org", "user:a", profile.ID, rev)
	if err != nil {
		t.Fatal(err)
	}
	first, created, err := resolver.ResolveExecution(ctx, "org", "execution-1", airuntime.RuntimeSelection{Mode: airuntime.SelectionInherit})
	if err != nil || !created {
		t.Fatalf("first freeze: created=%v err=%v", created, err)
	}
	firstBytes, _ := airuntime.CanonicalJSON(first)

	profile.Parameters["effort"] = "low"
	if _, _, err := svc.UpdateProfile(ctx, "org", "user:a", rev, profile); err != nil {
		t.Fatal(err)
	}
	retry, created, err := resolver.ResolveExecution(ctx, "org", "execution-1", airuntime.RuntimeSelection{Mode: airuntime.SelectionOverride, CLIID: "missing", ModelID: "missing"})
	if err != nil || created {
		t.Fatalf("retry existing snapshot: created=%v err=%v", created, err)
	}
	retryBytes, _ := airuntime.CanonicalJSON(retry)
	if string(firstBytes) != string(retryBytes) {
		t.Fatalf("snapshot changed across retry/catalog mutation:\n%s\n%s", firstBytes, retryBytes)
	}
	fresh, created, err := resolver.ResolveExecution(ctx, "org", "execution-2", airuntime.RuntimeSelection{Mode: airuntime.SelectionInherit})
	if err != nil || !created {
		t.Fatalf("new execution: created=%v err=%v", created, err)
	}
	if fresh.Parameters["effort"] != "low" || fresh.ProfileVersion == first.ProfileVersion {
		t.Fatalf("new execution did not observe new profile version: first=%+v fresh=%+v", first, fresh)
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

func TestCatalogUpdateAuditsPreserveBeforeAndAfter(t *testing.T) {
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
		return fmt.Sprintf("audit-%d", n)
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

	originalSchema := append(json.RawMessage(nil), codex.ParameterSchema...)
	updatedSchema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"config":{
				"type":"object",
				"properties":{"retries":{"type":"integer","maximum":5}},
				"required":["retries"],
				"additionalProperties":false
			}
		},
		"required":["config"],
		"additionalProperties":false
	}`)
	codex.ParameterSchema = updatedSchema
	codex, rev, err := svc.UpdateCLI(ctx, "org", "user:a", 0, codex)
	if err != nil {
		t.Fatal(err)
	}
	var cliBeforeJSON, cliAfterJSON string
	if err := db.QueryRow(`
		SELECT before_json, after_json
		FROM ai_runtime_audit_log
		WHERE org_id=? AND entity_type='cli' AND entity_key=? AND action='updated'
		ORDER BY revision DESC LIMIT 1
	`, "org", codex.Key).Scan(&cliBeforeJSON, &cliAfterJSON); err != nil {
		t.Fatal(err)
	}
	var cliBefore, cliAfter airuntime.CLIDefinition
	if err := json.Unmarshal([]byte(cliBeforeJSON), &cliBefore); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(cliAfterJSON), &cliAfter); err != nil {
		t.Fatal(err)
	}
	if !equalJSON(cliBefore.ParameterSchema, originalSchema) {
		t.Fatalf("CLI audit before schema = %s want %s", cliBefore.ParameterSchema, originalSchema)
	}
	if !equalJSON(cliAfter.ParameterSchema, updatedSchema) {
		t.Fatalf("CLI audit after schema = %s want %s", cliAfter.ParameterSchema, updatedSchema)
	}

	model, rev, err := svc.CreateModel(ctx, "org", "user:a", rev, airuntime.ModelDefinition{
		Key: "gpt", ModelKey: "gpt", CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{"config": map[string]any{"retries": float64(2)}},
		Enabled:           true,
	})
	if err != nil {
		t.Fatal(err)
	}
	model.DefaultParameters = map[string]any{"config": map[string]any{"retries": float64(4)}}
	model, rev, err = svc.UpdateModel(ctx, "org", "user:a", rev, model)
	if err != nil {
		t.Fatal(err)
	}
	var modelBeforeJSON, modelAfterJSON string
	if err := db.QueryRow(`
		SELECT before_json, after_json
		FROM ai_runtime_audit_log
		WHERE org_id=? AND entity_type='model' AND entity_key=? AND action='updated'
		ORDER BY revision DESC LIMIT 1
	`, "org", model.Key).Scan(&modelBeforeJSON, &modelAfterJSON); err != nil {
		t.Fatal(err)
	}
	var modelBefore, modelAfter airuntime.ModelDefinition
	if err := json.Unmarshal([]byte(modelBeforeJSON), &modelBefore); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(modelAfterJSON), &modelAfter); err != nil {
		t.Fatal(err)
	}
	beforeRetries := modelBefore.DefaultParameters["config"].(map[string]any)["retries"]
	afterRetries := modelAfter.DefaultParameters["config"].(map[string]any)["retries"]
	if beforeRetries != float64(2) || afterRetries != float64(4) {
		t.Fatalf("Model audit retries before=%v after=%v", beforeRetries, afterRetries)
	}
}

func TestCatalogUpdateValidationFailuresDoNotPersistOrAudit(t *testing.T) {
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
		return fmt.Sprintf("failed-%d", n)
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
	codex.ParameterSchema = json.RawMessage(`{
		"type":"object",
		"properties":{"config":{"type":"object","properties":{"retries":{"type":"integer","maximum":5}},"required":["retries"],"additionalProperties":false}},
		"required":["config"],
		"additionalProperties":false
	}`)
	codex, rev, err := svc.UpdateCLI(ctx, "org", "user:a", 0, codex)
	if err != nil {
		t.Fatal(err)
	}
	model, rev, err := svc.CreateModel(ctx, "org", "user:a", rev, airuntime.ModelDefinition{
		Key: "gpt", ModelKey: "gpt", CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{"config": map[string]any{"retries": float64(4)}},
		Enabled:           true,
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
	var auditsBefore int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_audit_log WHERE org_id=?`, "org").Scan(&auditsBefore); err != nil {
		t.Fatal(err)
	}

	invalidCLI := codex
	invalidCLI.ParameterSchema = json.RawMessage(`{
		"type":"object",
		"properties":{"config":{"type":"object","properties":{"retries":{"type":"integer","maximum":3}},"required":["retries"],"additionalProperties":false}},
		"required":["config"],
		"additionalProperties":false
	}`)
	if _, _, err := svc.UpdateCLI(ctx, "org", "user:a", rev, invalidCLI); err == nil {
		t.Fatal("expected nested CLI schema validation failure")
	}
	invalidModel := model
	invalidModel.DefaultParameters = map[string]any{"config": map[string]any{"retries": float64(6)}}
	if _, _, err := svc.UpdateModel(ctx, "org", "user:a", rev, invalidModel); err == nil {
		t.Fatal("expected nested model arguments validation failure")
	}

	after, err := svc.Catalog(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	if after.Revision != rev {
		t.Fatalf("revision after failed updates = %d want %d", after.Revision, rev)
	}
	var storedCLI airuntime.CLIDefinition
	for _, cli := range after.CLIs {
		if cli.ID == codex.ID {
			storedCLI = cli
		}
	}
	var storedModel airuntime.ModelDefinition
	for _, candidate := range after.Models {
		if candidate.ID == model.ID {
			storedModel = candidate
		}
	}
	if !equalJSON(storedCLI.ParameterSchema, codex.ParameterSchema) {
		t.Fatalf("failed CLI update persisted schema: %s", storedCLI.ParameterSchema)
	}
	storedRetries := storedModel.DefaultParameters["config"].(map[string]any)["retries"]
	if storedRetries != float64(4) {
		t.Fatalf("failed Model update persisted retries=%v", storedRetries)
	}
	var auditsAfter int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_audit_log WHERE org_id=?`, "org").Scan(&auditsAfter); err != nil {
		t.Fatal(err)
	}
	if auditsAfter != auditsBefore {
		t.Fatalf("audit rows after failed updates = %d want %d", auditsAfter, auditsBefore)
	}
}

func equalJSON(left, right []byte) bool {
	var leftValue, rightValue any
	if json.Unmarshal(left, &leftValue) != nil || json.Unmarshal(right, &rightValue) != nil {
		return false
	}
	return reflect.DeepEqual(leftValue, rightValue)
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
