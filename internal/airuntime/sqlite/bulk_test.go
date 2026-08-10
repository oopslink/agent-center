package sqlite_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/airuntime"
	airuntimesql "github.com/oopslink/agent-center/internal/airuntime/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newBulkService(t *testing.T) (*airuntime.Service, *sql.DB) {
	t.Helper()
	db, err := persistence.Open(t.TempDir() + "/runtime.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	n := 0
	svc := airuntime.NewService(airuntimesql.NewRepository(db), func() string {
		n++
		return fmt.Sprintf("bulk-%d", n)
	})
	return svc, db
}

func importRuntime(ctx context.Context, svc *airuntime.Service, org, actor string, req airuntime.ImportRequest) (airuntime.ImportReport, error) {
	preview, err := svc.PreviewImport(ctx, org, airuntime.PreviewRequest{
		Strategy: req.Strategy,
		Document: req.Document,
	})
	if err != nil || req.DryRun {
		return preview.Report, err
	}
	if req.ExpectedRevision != 0 && preview.Report.Revision != req.ExpectedRevision {
		return airuntime.ImportReport{}, &airuntime.Error{
			Reason:  airuntime.ReasonRevisionConflict,
			Message: "catalog revision does not match expected revision",
		}
	}
	return svc.ApplyImport(ctx, org, actor, airuntime.ApplyRequest{
		Strategy:        req.Strategy,
		Document:        req.Document,
		ValidationToken: preview.ValidationToken,
	})
}

func seededDocument(t *testing.T, svc *airuntime.Service, org string) airuntime.ExportDocument {
	t.Helper()
	ctx := context.Background()
	_, _, err := svc.CreateModel(ctx, org, "user:owner", 0, airuntime.ModelDefinition{
		Key: "gpt", ModelKey: "gpt-5", DisplayName: "GPT", CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{"temperature": 0.2}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := svc.Export(ctx, org)
	if err != nil {
		t.Fatal(err)
	}
	return doc
}

func TestBulkImportRoundTripAndDeterministicOrdering(t *testing.T) {
	svc, _ := newBulkService(t)
	source := seededDocument(t, svc, "org-source")
	report, err := importRuntime(context.Background(), svc, "org-target", "user:owner", airuntime.ImportRequest{
		ExpectedRevision: 0, Strategy: airuntime.StrategyMerge, Document: source,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.Revision != 1 {
		t.Fatalf("report = %+v", report)
	}
	target, err := svc.Export(context.Background(), "org-target")
	if err != nil {
		t.Fatal(err)
	}
	source.ExportedAt = target.ExportedAt
	if !reflect.DeepEqual(source, target) {
		a, _ := json.MarshalIndent(source, "", "  ")
		b, _ := json.MarshalIndent(target, "", "  ")
		t.Fatalf("round trip differs\nsource=%s\ntarget=%s", a, b)
	}
	for i := 1; i < len(report.Items); i++ {
		prev, next := report.Items[i-1], report.Items[i]
		if prev.EntityType > next.EntityType || (prev.EntityType == next.EntityType && prev.Key > next.Key) {
			t.Fatalf("report not deterministic: %+v", report.Items)
		}
	}
}

func TestBulkImportRejectsMalformedUnsupportedAndInvalidDocuments(t *testing.T) {
	svc, db := newBulkService(t)
	valid := airuntime.ExportDocument{Kind: airuntime.ExportKind, SchemaVersion: airuntime.ExportVersion, Runtime: airuntime.ExportCatalog{}}
	cases := []struct {
		name string
		doc  airuntime.ExportDocument
		want airuntime.Reason
	}{
		{"malformed kind", airuntime.ExportDocument{SchemaVersion: 1}, airuntime.ReasonImportMalformed},
		{"unsupported version", airuntime.ExportDocument{Kind: airuntime.ExportKind, SchemaVersion: 99}, airuntime.ReasonImportVersionUnsupported},
		{"invalid key", func() airuntime.ExportDocument {
			d := valid
			d.Runtime.CLIs = []airuntime.ExportCLI{{Key: "BAD KEY", DisplayName: "Bad", Executable: "bad", ParameterSchema: json.RawMessage(`{"type":"object"}`)}}
			return d
		}(), airuntime.ReasonImportInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := importRuntime(context.Background(), svc, "org", "user:owner", airuntime.ImportRequest{Strategy: airuntime.StrategyMerge, Document: tc.doc})
			var runtimeErr *airuntime.Error
			if !errors.As(err, &runtimeErr) || runtimeErr.Reason != tc.want {
				t.Fatalf("error = %v want %s", err, tc.want)
			}
		})
	}
	assertCatalogState(t, db, "org", 0, 0)
}

func TestBulkImportRejectsSchemaFailuresAtomically(t *testing.T) {
	svc, db := newBulkService(t)
	doc := airuntime.ExportDocument{
		Kind: airuntime.ExportKind, SchemaVersion: airuntime.ExportVersion,
		Runtime: airuntime.ExportCatalog{
			CLIs: []airuntime.ExportCLI{
				{Key: "valid", DisplayName: "Valid", Executable: "valid", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true},
				{Key: "bad-schema", DisplayName: "Bad", Executable: "bad", ParameterSchema: json.RawMessage(`{"type":"object","required":"wrong"}`), Enabled: true},
			},
			Models: []airuntime.ExportModel{{Key: "orphan", ModelKey: "orphan", CompatibleCLIKeys: []string{"missing"}, DefaultParameters: map[string]any{}, Enabled: true}},
		},
	}
	report, err := importRuntime(context.Background(), svc, "org", "user:owner", airuntime.ImportRequest{Strategy: airuntime.StrategyMerge, Document: doc})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonImportInvalid {
		t.Fatalf("error = %v", err)
	}
	if len(report.Diagnostics) < 1 {
		t.Fatalf("diagnostics = %+v", report.Diagnostics)
	}
	assertCatalogState(t, db, "org", 0, 0)
	cat, _ := svc.Catalog(context.Background(), "org")
	if _, ok := cliByKeyForTest(cat.CLIs)["valid"]; ok {
		t.Fatal("valid prefix was partially persisted")
	}
}

func TestBulkImportAllowsModelsReferencingMissingCLIsUntilRuntimeUse(t *testing.T) {
	svc, db := newBulkService(t)
	doc := airuntime.ExportDocument{
		Kind: airuntime.ExportKind, SchemaVersion: airuntime.ExportVersion,
		Runtime: airuntime.ExportCatalog{
			Models: []airuntime.ExportModel{{
				Key: "future", ModelKey: "future-model", DisplayName: "Future Model",
				CompatibleCLIKeys: []string{"future-cli"}, DefaultParameters: map[string]any{}, Enabled: true,
			}},
		},
	}
	report, err := importRuntime(context.Background(), svc, "org", "user:owner", airuntime.ImportRequest{Strategy: airuntime.StrategyMerge, Document: doc})
	if err != nil || !report.Applied {
		t.Fatalf("report=%+v err=%v", report, err)
	}
	cat, err := svc.Catalog(context.Background(), "org")
	if err != nil {
		t.Fatal(err)
	}
	var imported *airuntime.ModelDefinition
	for i := range cat.Models {
		if cat.Models[i].Key == "future" {
			imported = &cat.Models[i]
			break
		}
	}
	if imported == nil || !reflect.DeepEqual(imported.CompatibleCLIKeys, []string{"future-cli"}) {
		t.Fatalf("missing-CLI model was not imported as-is: %+v", cat.Models)
	}
	resolver := airuntime.NewRuntimeResolver(airuntimesql.NewRepository(db))
	_, err = resolver.Resolve(context.Background(), "org", airuntime.RuntimeSelection{
		Mode: airuntime.SelectionOverride, CLIID: "future-cli", ModelID: "future",
	})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonCLINotFound {
		t.Fatalf("runtime use error=%v", err)
	}
}

func TestBulkImportStrategies(t *testing.T) {
	for _, strategy := range []airuntime.ImportStrategy{airuntime.StrategyMerge, airuntime.StrategyCreate} {
		t.Run(string(strategy), func(t *testing.T) {
			svc, db := newBulkService(t)
			doc := airuntime.ExportDocument{Kind: airuntime.ExportKind, SchemaVersion: airuntime.ExportVersion, Runtime: airuntime.ExportCatalog{
				CLIs: []airuntime.ExportCLI{{Key: "codex", DisplayName: "Imported Codex", Executable: "codex-new", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true}},
			}}
			report, err := importRuntime(context.Background(), svc, "org", "user:owner", airuntime.ImportRequest{Strategy: strategy, Document: doc})
			switch strategy {
			case airuntime.StrategyCreate:
				if err != nil || report.Applied || report.Items[0].Action != "unchanged" {
					t.Fatalf("create_only report=%+v err=%v", report, err)
				}
				assertCatalogState(t, db, "org", 0, 0)
			case airuntime.StrategyMerge:
				if err != nil || !report.Applied || report.Items[0].Action != "update" {
					t.Fatalf("merge report=%+v err=%v", report, err)
				}
				assertCatalogState(t, db, "org", 1, 1)
				cat, _ := svc.Catalog(context.Background(), "org")
				if cliByKeyForTest(cat.CLIs)["codex"].Executable != "codex-new" {
					t.Fatal("overwrite did not update CLI")
				}
			}
		})
	}
}

func TestBulkImportDryRunAndRevisionConflictHaveZeroMutation(t *testing.T) {
	svc, db := newBulkService(t)
	doc := airuntime.ExportDocument{Kind: airuntime.ExportKind, SchemaVersion: airuntime.ExportVersion, Runtime: airuntime.ExportCatalog{
		CLIs: []airuntime.ExportCLI{{Key: "custom", DisplayName: "Custom", Executable: "custom", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true}},
	}}
	report, err := importRuntime(context.Background(), svc, "org", "user:owner", airuntime.ImportRequest{DryRun: true, Strategy: airuntime.StrategyMerge, Document: doc})
	if err != nil || report.Applied || len(report.Items) != 1 {
		t.Fatalf("dry run report=%+v err=%v", report, err)
	}
	assertCatalogState(t, db, "org", 0, 0)
	preview, err := svc.PreviewImport(context.Background(), "org", airuntime.PreviewRequest{
		Strategy: airuntime.StrategyMerge,
		Document: doc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.CreateCLI(context.Background(), "org", "user:owner", 0, airuntime.CLIDefinition{Key: "other", DisplayName: "Other", Executable: "other", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.ApplyImport(context.Background(), "org", "user:owner", airuntime.ApplyRequest{
		Strategy:        airuntime.StrategyMerge,
		Document:        doc,
		ValidationToken: preview.ValidationToken,
	})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonRevisionConflict {
		t.Fatalf("revision error = %v", err)
	}
	assertCatalogState(t, db, "org", 1, 1)
}

func TestBulkImportRepositoryErrorRollsBackEntireMutation(t *testing.T) {
	_, db := newBulkService(t)
	svc := airuntime.NewService(airuntimesql.NewRepository(db), func() string { return "duplicate-id" })
	doc := airuntime.ExportDocument{Kind: airuntime.ExportKind, SchemaVersion: airuntime.ExportVersion, Runtime: airuntime.ExportCatalog{
		CLIs: []airuntime.ExportCLI{
			{Key: "first", DisplayName: "First", Executable: "first", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true},
			{Key: "second", DisplayName: "Second", Executable: "second", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true},
		},
	}}
	preview, err := svc.PreviewImport(context.Background(), "org", airuntime.PreviewRequest{
		Strategy: airuntime.StrategyMerge, Document: doc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyImport(context.Background(), "org", "user:owner", airuntime.ApplyRequest{
		Strategy: airuntime.StrategyMerge, Document: doc, ValidationToken: preview.ValidationToken,
	}); err == nil {
		t.Fatal("expected duplicate generated ID to fail")
	}
	assertCatalogState(t, db, "org", 0, 0)
	cat, err := svc.Catalog(context.Background(), "org")
	if err != nil {
		t.Fatal(err)
	}
	byKey := cliByKeyForTest(cat.CLIs)
	if _, ok := byKey["first"]; ok {
		t.Fatal("first row survived failed bulk transaction")
	}
	if _, ok := byKey["second"]; ok {
		t.Fatal("second row survived failed bulk transaction")
	}
}

func TestBulkExportRedactsSensitiveFieldsAndIsolatesOrganizations(t *testing.T) {
	svc, _ := newBulkService(t)
	_, rev, err := svc.CreateModel(context.Background(), "org-a", "user:owner", 0, airuntime.ModelDefinition{
		Key: "secure", ModelKey: "secure", CompatibleCLIKeys: []string{"codex"}, Enabled: true,
		DefaultParameters: map[string]any{"api_key": "secret-value", "nested": map[string]any{"access_token": "token-value", "temperature": 0.2}},
	})
	if err != nil || rev != 1 {
		t.Fatal(err)
	}
	a, err := svc.Export(context.Background(), "org-a")
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(a)
	if strings.Contains(string(raw), "secret-value") || strings.Contains(string(raw), "token-value") || strings.Contains(string(raw), "org-a") || strings.Contains(string(raw), "user:owner") {
		t.Fatalf("sensitive or org data leaked: %s", raw)
	}
	params := a.Runtime.Models[0].DefaultParameters
	if params["api_key"] != airuntime.RedactedValue || params["nested"].(map[string]any)["access_token"] != airuntime.RedactedValue {
		t.Fatalf("parameters not redacted: %#v", params)
	}
	b, err := svc.Export(context.Background(), "org-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Runtime.Models) != 0 {
		t.Fatalf("cross-org model leak: %+v", b.Runtime.Models)
	}
}

func TestBulkImportReplaceDisablesMissingEntriesWithinOrganization(t *testing.T) {
	svc, _ := newBulkService(t)
	doc := seededDocument(t, svc, "org-a")
	_, revision, err := svc.CreateCLI(context.Background(), "org-a", "user:owner", 1, airuntime.CLIDefinition{
		Key: "extra", DisplayName: "Extra", Executable: "extra", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, otherRevision, err := svc.CreateCLI(context.Background(), "org-b", "user:owner", 0, airuntime.CLIDefinition{
		Key: "extra", DisplayName: "Other Extra", Executable: "other-extra", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}

	doc.Runtime.CLIs = []airuntime.ExportCLI{doc.Runtime.CLIs[1]} // codex only; omit claude-code and extra.
	report, err := importRuntime(context.Background(), svc, "org-a", "user:owner", airuntime.ImportRequest{
		ExpectedRevision: revision,
		Strategy:         airuntime.StrategyReplace,
		Document:         doc,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Applied || report.Revision != revision+1 {
		t.Fatalf("replace report=%+v", report)
	}
	a, err := svc.Catalog(context.Background(), "org-a")
	if err != nil {
		t.Fatal(err)
	}
	clis := cliByKeyForTest(a.CLIs)
	if clis["extra"].Enabled || clis["claude-code"].Enabled || !clis["codex"].Enabled {
		t.Fatalf("replace CLI states=%+v", clis)
	}
	b, err := svc.Catalog(context.Background(), "org-b")
	if err != nil {
		t.Fatal(err)
	}
	if !cliByKeyForTest(b.CLIs)["extra"].Enabled || b.Revision != otherRevision {
		t.Fatalf("replace crossed organization boundary: %+v", b)
	}
}

func TestBulkImportPreservesExportedRedactedSecretsAndRejectsMissingValues(t *testing.T) {
	svc, db := newBulkService(t)
	_, revision, err := svc.CreateModel(context.Background(), "org-a", "user:owner", 0, airuntime.ModelDefinition{
		Key: "secure", ModelKey: "secure", DisplayName: "Secure", CompatibleCLIKeys: []string{"codex"}, Enabled: true,
		DefaultParameters: map[string]any{
			"api_key": "keep-me",
			"nested":  map[string]any{"access_token": "keep-nested", "temperature": 0.2},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := svc.Export(context.Background(), "org-a")
	if err != nil {
		t.Fatal(err)
	}
	doc.Runtime.Models[0].DisplayName = "Updated Secure"

	dryRun, err := importRuntime(context.Background(), svc, "org-a", "user:owner", airuntime.ImportRequest{
		ExpectedRevision: revision, DryRun: true, Strategy: airuntime.StrategyMerge, Document: doc,
	})
	if err != nil || dryRun.Applied {
		t.Fatalf("redacted dry-run report=%+v err=%v", dryRun, err)
	}
	assertCatalogState(t, db, "org-a", revision, 1)

	applied, err := importRuntime(context.Background(), svc, "org-a", "user:owner", airuntime.ImportRequest{
		ExpectedRevision: revision, Strategy: airuntime.StrategyMerge, Document: doc,
	})
	if err != nil || !applied.Applied {
		t.Fatalf("redacted apply report=%+v err=%v", applied, err)
	}
	cat, err := svc.Catalog(context.Background(), "org-a")
	if err != nil {
		t.Fatal(err)
	}
	params := cat.Models[0].DefaultParameters
	if params["api_key"] != "keep-me" || params["nested"].(map[string]any)["access_token"] != "keep-nested" {
		t.Fatalf("redacted placeholders were persisted or lost: %#v", params)
	}

	rejected, err := importRuntime(context.Background(), svc, "org-b", "user:owner", airuntime.ImportRequest{
		ExpectedRevision: 0, Strategy: airuntime.StrategyMerge, Document: doc,
	})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonImportInvalid {
		t.Fatalf("missing redacted secret error=%v report=%+v", err, rejected)
	}
	assertCatalogState(t, db, "org-b", 0, 0)
}

func TestPreviewApplyPreservesRedactedSecretsInsideArrays(t *testing.T) {
	svc, _ := newBulkService(t)
	_, _, err := svc.CreateModel(context.Background(), "org-a", "user:owner", 0, airuntime.ModelDefinition{
		Key: "secure-array", ModelKey: "secure-array", DisplayName: "Secure Array",
		CompatibleCLIKeys: []string{"codex"}, Enabled: true,
		DefaultParameters: map[string]any{
			"providers": []any{
				map[string]any{"api_key": "keep-first"},
				map[string]any{"nested": []any{map[string]any{"access_token": "keep-second"}}},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := svc.Export(context.Background(), "org-a")
	if err != nil {
		t.Fatal(err)
	}
	doc.Runtime.Models[0].DisplayName = "Updated"
	preview, err := svc.PreviewImport(context.Background(), "org-a", airuntime.PreviewRequest{
		Strategy: airuntime.StrategyMerge, Document: doc,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.ApplyImport(context.Background(), "org-a", "user:owner", airuntime.ApplyRequest{
		Strategy: airuntime.StrategyMerge, Document: doc, ValidationToken: preview.ValidationToken,
	})
	if err != nil || !report.Applied {
		t.Fatalf("apply report=%+v err=%v", report, err)
	}
	catalog, err := svc.Catalog(context.Background(), "org-a")
	if err != nil {
		t.Fatal(err)
	}
	providers := catalog.Models[0].DefaultParameters["providers"].([]any)
	if providers[0].(map[string]any)["api_key"] != "keep-first" ||
		providers[1].(map[string]any)["nested"].([]any)[0].(map[string]any)["access_token"] != "keep-second" {
		t.Fatalf("array secrets were not restored: %#v", providers)
	}

	_, err = svc.PreviewImport(context.Background(), "org-b", airuntime.PreviewRequest{
		Strategy: airuntime.StrategyMerge, Document: doc,
	})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonImportInvalid {
		t.Fatalf("missing array secret error=%v", err)
	}
}

func TestCreateOnlyDoesNotChangeExistingCatalogEntries(t *testing.T) {
	svc, db := newBulkService(t)
	doc := seededDocument(t, svc, "org")
	catalog, err := svc.Catalog(context.Background(), "org")
	if err != nil {
		t.Fatal(err)
	}
	_, revision, err := svc.CreateModel(context.Background(), "org", "user:owner", catalog.Revision, airuntime.ModelDefinition{
		Key: "other", ModelKey: "other", DisplayName: "Other", CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	doc, err = svc.Export(context.Background(), "org")
	if err != nil {
		t.Fatal(err)
	}
	for i := range doc.Runtime.Models {
		if doc.Runtime.Models[i].Key == "other" {
			doc.Runtime.Models[i].DisplayName = "Other updated"
		}
	}
	preview, err := svc.PreviewImport(context.Background(), "org", airuntime.PreviewRequest{
		Strategy: airuntime.StrategyCreate, Document: doc,
	})
	if err != nil {
		t.Fatal(err)
	}
	report, err := svc.ApplyImport(context.Background(), "org", "user:owner", airuntime.ApplyRequest{
		Strategy: airuntime.StrategyCreate, Document: doc, ValidationToken: preview.ValidationToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Applied {
		t.Fatalf("create_only unexpectedly applied: %+v", report)
	}
	var displayName string
	if err := db.QueryRow(`SELECT display_name FROM pm_model_catalog WHERE org_id=? AND runtime_key=?`, "org", "other").Scan(&displayName); err != nil {
		t.Fatal(err)
	}
	if displayName != "Other" || revision == 0 {
		t.Fatalf("existing model changed under create_only: %s", displayName)
	}
}

func assertCatalogState(t *testing.T, db *sql.DB, org string, revision int64, audits int) {
	t.Helper()
	var gotRevision int64
	if err := db.QueryRow(`SELECT revision FROM ai_runtime_catalogs WHERE org_id=?`, org).Scan(&gotRevision); err != nil {
		t.Fatal(err)
	}
	if gotRevision != revision {
		t.Fatalf("revision=%d want %d", gotRevision, revision)
	}
	var gotAudits int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ai_runtime_audit_log WHERE org_id=?`, org).Scan(&gotAudits); err != nil {
		t.Fatal(err)
	}
	if gotAudits != audits {
		t.Fatalf("audits=%d want %d", gotAudits, audits)
	}
}

func cliByKeyForTest(xs []airuntime.CLIDefinition) map[string]airuntime.CLIDefinition {
	out := map[string]airuntime.CLIDefinition{}
	for _, x := range xs {
		out[x.Key] = x
	}
	return out
}
