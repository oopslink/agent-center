package airuntime

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPreviewApplyTokenBindsAllClaimsAndExpires(t *testing.T) {
	repo := &resolveRepo{catalog: Catalog{OrgID: "org-a", Revision: 7}}
	svc := NewServiceWithValidationKey(repo, func() string { return "id" }, []byte("0123456789abcdef0123456789abcdef"))
	now := time.Date(2026, 7, 27, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	doc := ExportDocument{SchemaVersion: ExportVersion, Kind: ExportKind, Runtime: ExportCatalog{}}
	preview, err := svc.PreviewImport(context.Background(), "org-a", PreviewRequest{Strategy: StrategyMerge, Document: doc})
	if err != nil {
		t.Fatal(err)
	}
	if preview.ValidationToken == "" || preview.DocumentSHA256 == "" || !preview.ExpiresAt.Equal(now.Add(10*time.Minute)) {
		t.Fatalf("preview token metadata = %+v", preview)
	}

	tests := []struct {
		name string
		org  string
		req  ApplyRequest
		edit func()
		want Reason
	}{
		{
			name: "tamper", org: "org-a",
			req:  ApplyRequest{Strategy: StrategyMerge, Document: doc, ValidationToken: preview.ValidationToken + "x"},
			want: ReasonImportTokenInvalid,
		},
		{
			name: "organization", org: "org-b",
			req:  ApplyRequest{Strategy: StrategyMerge, Document: doc, ValidationToken: preview.ValidationToken},
			want: ReasonImportConflict,
		},
		{
			name: "strategy", org: "org-a",
			req:  ApplyRequest{Strategy: StrategyReplace, Document: doc, ValidationToken: preview.ValidationToken},
			want: ReasonImportConflict,
		},
		{
			name: "document", org: "org-a",
			req: func() ApplyRequest {
				changed := doc
				changed.Runtime.CLIs = []ExportCLI{{
					Key: "changed", DisplayName: "Changed", Executable: "changed",
					ParameterSchema: []byte(`{"type":"object"}`), Enabled: true,
				}}
				return ApplyRequest{Strategy: StrategyMerge, Document: changed, ValidationToken: preview.ValidationToken}
			}(),
			want: ReasonImportConflict,
		},
		{
			name: "revision", org: "org-a",
			req:  ApplyRequest{Strategy: StrategyMerge, Document: doc, ValidationToken: preview.ValidationToken},
			edit: func() { repo.catalog.Revision = 8 },
			want: ReasonRevisionConflict,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo.catalog.Revision = 7
			if tc.edit != nil {
				tc.edit()
			}
			_, err := svc.ApplyImport(context.Background(), tc.org, "actor", tc.req)
			var runtimeErr *Error
			if !errors.As(err, &runtimeErr) || runtimeErr.Reason != tc.want {
				t.Fatalf("error=%v want reason=%s", err, tc.want)
			}
		})
	}

	now = now.Add(11 * time.Minute)
	_, err = svc.ApplyImport(context.Background(), "org-a", "actor", ApplyRequest{
		Strategy: StrategyMerge, Document: doc, ValidationToken: preview.ValidationToken,
	})
	var runtimeErr *Error
	if !errors.As(err, &runtimeErr) || runtimeErr.Reason != ReasonImportTokenInvalid {
		t.Fatalf("expired token error=%v", err)
	}
}

func TestFilterExportDependencyClosureAndPartialWarning(t *testing.T) {
	catalog := ExportCatalog{
		CLIs: []ExportCLI{
			{Key: "codex"}, {Key: "other"},
		},
		Models: []ExportModel{
			{Key: "gpt", CompatibleCLIKeys: []string{"codex"}},
			{Key: "other", CompatibleCLIKeys: []string{"other"}},
		},
	}
	got, warnings, err := filterExport(catalog, ExportOptions{
		Scope: ExportScopeModel, ModelKeys: []string{"gpt"}, IncludeDependencies: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(warnings) != 0 || len(got.Models) != 1 || len(got.CLIs) != 1 ||
		got.Models[0].Key != "gpt" || got.CLIs[0].Key != "codex" {
		t.Fatalf("dependency closure=%+v warnings=%v", got, warnings)
	}
	partial, warnings, err := filterExport(catalog, ExportOptions{
		Scope: ExportScopeModel, ModelKeys: []string{"gpt"}, IncludeDependencies: false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(partial.Models) != 1 || len(partial.CLIs) != 0 || len(warnings) != 1 {
		t.Fatalf("partial=%+v warnings=%v", partial, warnings)
	}
}
