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

func seededDocument(t *testing.T, svc *airuntime.Service, org string) airuntime.ExportDocument {
	t.Helper()
	ctx := context.Background()
	model, rev, err := svc.CreateModel(ctx, org, "user:owner", 0, airuntime.ModelDefinition{
		Key: "gpt", ModelKey: "gpt-5", DisplayName: "GPT", CompatibleCLIKeys: []string{"codex"},
		DefaultParameters: map[string]any{"temperature": 0.2}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	profile, rev, err := svc.CreateProfile(ctx, org, "user:owner", rev, airuntime.RuntimeProfile{
		Key: "coding", Name: "Coding", CLIKey: "codex", ModelKey: model.Key,
		Parameters: map[string]any{"temperature": 0.1}, Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SetDefaultProfile(ctx, org, "user:owner", profile.ID, rev); err != nil {
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
	report, err := svc.Import(context.Background(), "org-target", "user:owner", airuntime.ImportRequest{
		ExpectedRevision: 0, ConflictStrategy: airuntime.ConflictOverwrite, Document: source,
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
	source.SourceRevision, target.SourceRevision = 0, 0
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
	valid := airuntime.ExportDocument{Kind: airuntime.ExportKind, Version: airuntime.ExportVersion, Catalog: airuntime.ExportCatalog{}}
	cases := []struct {
		name string
		doc  airuntime.ExportDocument
		want airuntime.Reason
	}{
		{"malformed kind", airuntime.ExportDocument{Version: 1}, airuntime.ReasonImportMalformed},
		{"unsupported version", airuntime.ExportDocument{Kind: airuntime.ExportKind, Version: 99}, airuntime.ReasonImportVersionUnsupported},
		{"invalid key", func() airuntime.ExportDocument {
			d := valid
			d.Catalog.CLIs = []airuntime.ExportCLI{{Key: "BAD KEY", DisplayName: "Bad", Executable: "bad", ParameterSchema: json.RawMessage(`{"type":"object"}`)}}
			return d
		}(), airuntime.ReasonImportInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Import(context.Background(), "org", "user:owner", airuntime.ImportRequest{ConflictStrategy: airuntime.ConflictReject, Document: tc.doc})
			var runtimeErr *airuntime.Error
			if !errors.As(err, &runtimeErr) || runtimeErr.Reason != tc.want {
				t.Fatalf("error = %v want %s", err, tc.want)
			}
		})
	}
	assertCatalogState(t, db, "org", 0, 0)
}

func TestBulkImportRejectsReferentialIntegrityAndSchemaFailuresAtomically(t *testing.T) {
	svc, db := newBulkService(t)
	doc := airuntime.ExportDocument{
		Kind: airuntime.ExportKind, Version: airuntime.ExportVersion,
		Catalog: airuntime.ExportCatalog{
			CLIs: []airuntime.ExportCLI{
				{Key: "valid", DisplayName: "Valid", Executable: "valid", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true},
				{Key: "bad-schema", DisplayName: "Bad", Executable: "bad", ParameterSchema: json.RawMessage(`{"type":"object","required":"wrong"}`), Enabled: true},
			},
			Models: []airuntime.ExportModel{{Key: "orphan", ModelKey: "orphan", CompatibleCLIKeys: []string{"missing"}, DefaultParameters: map[string]any{}, Enabled: true}},
		},
	}
	report, err := svc.Import(context.Background(), "org", "user:owner", airuntime.ImportRequest{ConflictStrategy: airuntime.ConflictReject, Document: doc})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonImportInvalid {
		t.Fatalf("error = %v", err)
	}
	if len(report.Diagnostics) < 2 {
		t.Fatalf("diagnostics = %+v", report.Diagnostics)
	}
	assertCatalogState(t, db, "org", 0, 0)
	cat, _ := svc.Catalog(context.Background(), "org")
	if _, ok := cliByKeyForTest(cat.CLIs)["valid"]; ok {
		t.Fatal("valid prefix was partially persisted")
	}
}

func TestBulkImportConflictPolicies(t *testing.T) {
	for _, strategy := range []airuntime.ConflictStrategy{airuntime.ConflictReject, airuntime.ConflictSkip, airuntime.ConflictOverwrite} {
		t.Run(string(strategy), func(t *testing.T) {
			svc, db := newBulkService(t)
			doc := airuntime.ExportDocument{Kind: airuntime.ExportKind, Version: airuntime.ExportVersion, Catalog: airuntime.ExportCatalog{
				CLIs: []airuntime.ExportCLI{{Key: "codex", DisplayName: "Imported Codex", Executable: "codex-new", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true}},
			}}
			report, err := svc.Import(context.Background(), "org", "user:owner", airuntime.ImportRequest{ConflictStrategy: strategy, Document: doc})
			switch strategy {
			case airuntime.ConflictReject:
				var runtimeErr *airuntime.Error
				if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonImportConflict {
					t.Fatalf("reject error = %v", err)
				}
				assertCatalogState(t, db, "org", 0, 0)
			case airuntime.ConflictSkip:
				if err != nil || report.Applied || report.Items[0].Action != "skip" {
					t.Fatalf("skip report=%+v err=%v", report, err)
				}
				assertCatalogState(t, db, "org", 0, 0)
			case airuntime.ConflictOverwrite:
				if err != nil || !report.Applied || report.Items[0].Action != "overwrite" {
					t.Fatalf("overwrite report=%+v err=%v", report, err)
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
	doc := airuntime.ExportDocument{Kind: airuntime.ExportKind, Version: airuntime.ExportVersion, Catalog: airuntime.ExportCatalog{
		CLIs: []airuntime.ExportCLI{{Key: "custom", DisplayName: "Custom", Executable: "custom", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true}},
	}}
	report, err := svc.Import(context.Background(), "org", "user:owner", airuntime.ImportRequest{DryRun: true, ConflictStrategy: airuntime.ConflictReject, Document: doc})
	if err != nil || report.Applied || len(report.Items) != 1 {
		t.Fatalf("dry run report=%+v err=%v", report, err)
	}
	assertCatalogState(t, db, "org", 0, 0)
	if _, _, err := svc.CreateCLI(context.Background(), "org", "user:owner", 0, airuntime.CLIDefinition{Key: "other", DisplayName: "Other", Executable: "other", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	_, err = svc.Import(context.Background(), "org", "user:owner", airuntime.ImportRequest{ExpectedRevision: 0, ConflictStrategy: airuntime.ConflictReject, Document: doc})
	var runtimeErr *airuntime.Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != airuntime.ReasonRevisionConflict {
		t.Fatalf("revision error = %v", err)
	}
	assertCatalogState(t, db, "org", 1, 1)
}

func TestBulkImportRepositoryErrorRollsBackEntireMutation(t *testing.T) {
	_, db := newBulkService(t)
	svc := airuntime.NewService(airuntimesql.NewRepository(db), func() string { return "duplicate-id" })
	doc := airuntime.ExportDocument{Kind: airuntime.ExportKind, Version: airuntime.ExportVersion, Catalog: airuntime.ExportCatalog{
		CLIs: []airuntime.ExportCLI{
			{Key: "first", DisplayName: "First", Executable: "first", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true},
			{Key: "second", DisplayName: "Second", Executable: "second", ParameterSchema: json.RawMessage(`{"type":"object"}`), Enabled: true},
		},
	}}
	if _, err := svc.Import(context.Background(), "org", "user:owner", airuntime.ImportRequest{ConflictStrategy: airuntime.ConflictReject, Document: doc}); err == nil {
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
	params := a.Catalog.Models[0].DefaultParameters
	if params["api_key"] != airuntime.RedactedValue || params["nested"].(map[string]any)["access_token"] != airuntime.RedactedValue {
		t.Fatalf("parameters not redacted: %#v", params)
	}
	b, err := svc.Export(context.Background(), "org-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Catalog.Models) != 0 {
		t.Fatalf("cross-org model leak: %+v", b.Catalog.Models)
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
