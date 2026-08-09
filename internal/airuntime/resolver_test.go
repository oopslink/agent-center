package airuntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type resolveRepo struct{ catalog Catalog }

func (r *resolveRepo) GetCatalog(context.Context, string) (Catalog, error) { return r.catalog, nil }
func (*resolveRepo) CreateCLI(context.Context, CLIDefinition, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*resolveRepo) UpdateCLI(context.Context, CLIDefinition, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*resolveRepo) CreateModel(context.Context, ModelDefinition, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*resolveRepo) UpdateModel(context.Context, ModelDefinition, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}

func resolverFixture() (*RuntimeResolver, *resolveRepo) {
	repo := &resolveRepo{catalog: Catalog{
		OrgID: "org-1",
		CLIs: []CLIDefinition{{
			ID: "cli-codex", Key: "codex", Executable: "codex", Enabled: true,
			RequiredFeatures: []string{"json"}, ParameterSchema: json.RawMessage(`{
				"type":"object","additionalProperties":false,
				"properties":{"temperature":{"type":"number","default":0.1},"effort":{"type":"string"}}
			}`),
		}},
		Models: []ModelDefinition{{
			ID: "model-gpt", Key: "gpt", ModelKey: "gpt-5", Enabled: true,
			CompatibleCLIKeys: []string{"codex"}, DefaultParameters: map[string]any{"temperature": 0.2, "effort": "medium"},
		}},
	}}
	resolver := NewRuntimeResolver(repo)
	resolver.now = func() time.Time { return time.Unix(100, 0).UTC() }
	return resolver, repo
}

func TestRuntimeResolverPrecedenceAndSnapshotImmutability(t *testing.T) {
	resolver, repo := resolverFixture()
	override, err := resolver.Resolve(context.Background(), "org-1", RuntimeSelection{
		Mode: SelectionOverride, CLIID: "cli-codex", ModelID: "model-gpt",
		Parameters: map[string]any{"temperature": 0.7},
	})
	if err != nil {
		t.Fatal(err)
	}
	if override.Source != "override" || override.Parameters["temperature"] != 0.7 || override.Parameters["effort"] != "medium" {
		t.Fatalf("override = %+v", override)
	}
	repo.catalog.Models[0].DefaultParameters["temperature"] = 0.9
	if override.Parameters["temperature"] != 0.7 {
		t.Fatalf("snapshot changed with catalog: %#v", override.Parameters)
	}
}

func TestRuntimeResolverStructuredFailures(t *testing.T) {
	resolver, repo := resolverFixture()
	cases := []struct {
		name string
		sel  RuntimeSelection
		edit func()
		want Reason
	}{
		{"inherit default missing", RuntimeSelection{Mode: SelectionInherit}, nil, ReasonDefaultMissing},
		{"retired profile selection", RuntimeSelection{Mode: "profile"}, nil, ReasonSelectionInvalid},
		{"cli disabled", RuntimeSelection{Mode: SelectionOverride, CLIID: "codex", ModelID: "gpt"}, func() { repo.catalog.CLIs[0].Enabled = false }, ReasonCLIDisabled},
		{"model disabled", RuntimeSelection{Mode: SelectionOverride, CLIID: "codex", ModelID: "gpt"}, func() { repo.catalog.Models[0].Enabled = false }, ReasonModelDisabled},
		{"incompatible", RuntimeSelection{Mode: SelectionOverride, CLIID: "codex", ModelID: "gpt"}, func() { repo.catalog.Models[0].CompatibleCLIKeys = []string{"claude-code"} }, ReasonIncompatible},
		{"invalid parameters", RuntimeSelection{Mode: SelectionOverride, CLIID: "codex", ModelID: "gpt", Parameters: map[string]any{"unknown": true}}, nil, ReasonParametersInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver, repo = resolverFixture()
			if tc.edit != nil {
				tc.edit()
			}
			_, err := resolver.Resolve(context.Background(), "org-1", tc.sel)
			var runtimeErr *Error
			if !errors.As(err, &runtimeErr) || runtimeErr.Reason != tc.want || runtimeErr.Message == "" || runtimeErr.Details == nil {
				t.Fatalf("error = %#v, want reason %s", err, tc.want)
			}
		})
	}
}

type legacyCount map[string]int

func (c legacyCount) IncrementLegacyFallback(kind string) { c[kind]++ }

func TestLegacyAdapterPrecedenceExactMappingAndIssue(t *testing.T) {
	resolver, _ := resolverFixture()
	count := legacyCount{}
	adapter := NewLegacyAdapter(resolver, count)
	explicit := RuntimeSelection{Mode: SelectionOverride, CLIID: "cli-codex", ModelID: "model-gpt"}
	snapshot, issue, err := adapter.Resolve(context.Background(), "org-1", "agent", &explicit, LegacyRuntime{CLI: "unknown", Model: "unknown"})
	if err != nil || issue != nil || snapshot.Source != "override" || count["agent"] != 0 {
		t.Fatalf("new selection precedence: snapshot=%+v issue=%+v err=%v count=%v", snapshot, issue, err, count)
	}
	snapshot, issue, err = adapter.Resolve(context.Background(), "org-1", "agent", nil, LegacyRuntime{CLI: "codex", Model: "gpt-5"})
	if err != nil || issue != nil || snapshot.Source != "legacy" || count["agent"] != 1 {
		t.Fatalf("legacy exact map: snapshot=%+v issue=%+v err=%v count=%v", snapshot, issue, err, count)
	}
	_, issue, err = adapter.Resolve(context.Background(), "org-1", "agent", nil, LegacyRuntime{CLI: "codex", Model: "unknown-model"})
	var runtimeErr *Error
	if issue == nil || issue.Original.Model != "unknown-model" || !errors.As(err, &runtimeErr) || runtimeErr.Reason != ReasonLegacyUnmapped {
		t.Fatalf("unmapped legacy = issue=%+v err=%#v", issue, err)
	}
}
