package sqlite_test

import (
	"bytes"
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
	model.Enabled = false
	if model, rev, err = svc.UpdateModel(ctx, "org-a", "user:owner", rev, model); err != nil || rev != 2 {
		t.Fatalf("disable model: rev=%d err=%v", rev, err)
	}
	codex := catalog.CLIs[1]
	if codex.Key != "codex" {
		t.Fatalf("unexpected CLI ordering: %+v", catalog.CLIs)
	}
	codex.Enabled = false
	if _, rev, err = svc.UpdateCLI(ctx, "org-a", "user:owner", rev, codex); err != nil || rev != 3 {
		t.Fatalf("disable CLI: rev=%d err=%v", rev, err)
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
	if len(other.Models) != 0 {
		t.Fatalf("org isolation failed: %+v", other)
	}
}

func TestModelAllowsMissingCompatibleCLIUntilRuntimeUse(t *testing.T) {
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
		Key: "missing-cli", ModelKey: "missing-cli", CompatibleCLIKeys: []string{"missing"},
		DefaultParameters: map[string]any{}, Enabled: true,
	})
	if err != nil || rev != 1 {
		t.Fatalf("create missing CLI model: rev=%d err=%v", rev, err)
	}
	resolver := airuntime.NewRuntimeResolver(airuntimesql.NewRepository(db))
	_, err = resolver.Resolve(ctx, "org", airuntime.RuntimeSelection{
		Mode: airuntime.SelectionOverride, CLIID: "missing", ModelID: model.ID,
	})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonCLINotFound {
		t.Fatalf("runtime use error = %v", err)
	}
}

func TestCatalogUpdatesRevalidateDependentModels(t *testing.T) {
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

	tighterCLI := codex
	tighterCLI.ParameterSchema = json.RawMessage(`{"type":"object","properties":{"retries":{"type":"integer","maximum":1}},"additionalProperties":false}`)
	if _, _, err := svc.UpdateCLI(ctx, "org", "user:a", rev, tighterCLI); err == nil {
		t.Fatal("CLI schema update must reject an invalid dependent model")
	}

	incompatibleModel := model
	incompatibleModel.CompatibleCLIKeys = []string{"missing"}
	model, rev, err = svc.UpdateModel(ctx, "org", "user:a", rev, incompatibleModel)
	if err != nil {
		t.Fatalf("model compatibility update with a missing CLI should remain import/edit tolerant: %v", err)
	}

	invalidDefaults := model
	invalidDefaults.CompatibleCLIKeys = []string{"codex"}
	invalidDefaults.DefaultParameters = map[string]any{"retries": float64(4)}
	if _, _, err := svc.UpdateModel(ctx, "org", "user:a", rev, invalidDefaults); err == nil {
		t.Fatal("model defaults update must reject invalid parameters")
	}
}

func TestCatalogHardDeleteRemovesEntriesAndDoesNotReseedSystemCLI(t *testing.T) {
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
		return fmt.Sprintf("delete-%d", n)
	})
	ctx := context.Background()
	catalog, err := svc.Catalog(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	model, rev, err := svc.CreateModel(ctx, "org", "user:a", catalog.Revision, airuntime.ModelDefinition{
		Key: "gpt", ModelKey: "gpt", CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	rev, err = svc.DeleteModel(ctx, "org", "user:a", model.ID, rev)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err = svc.Catalog(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Models) != 0 {
		t.Fatalf("model was not hard deleted: %+v", catalog.Models)
	}
	var codexID string
	for _, cli := range catalog.CLIs {
		if cli.Key == "codex" {
			codexID = cli.ID
		}
	}
	if codexID == "" {
		t.Fatalf("fixture missing codex CLI: %+v", catalog.CLIs)
	}
	rev, err = svc.DeleteCLI(ctx, "org", "user:a", codexID, rev)
	if err != nil {
		t.Fatal(err)
	}
	catalog, err = svc.Catalog(ctx, "org")
	if err != nil {
		t.Fatal(err)
	}
	for _, cli := range catalog.CLIs {
		if cli.Key == "codex" {
			t.Fatalf("deleted system CLI was reseeded: %+v", catalog.CLIs)
		}
	}
	if catalog.Revision != rev {
		t.Fatalf("revision=%d want %d", catalog.Revision, rev)
	}
	var audits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_audit_log WHERE org_id=? AND action='deleted'`, "org").Scan(&audits); err != nil {
		t.Fatal(err)
	}
	if audits != 2 {
		t.Fatalf("delete audits=%d want 2", audits)
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
	var compactLeft, compactRight bytes.Buffer
	if json.Compact(&compactLeft, left) != nil || json.Compact(&compactRight, right) != nil {
		return false
	}
	return bytes.Equal(compactLeft.Bytes(), compactRight.Bytes())
}
