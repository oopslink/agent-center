package airuntime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type resolveRepo struct {
	catalog       Catalog
	catalogErr    error
	frozen        RuntimeSnapshot
	frozenOK      bool
	catalogReads  int
	snapshotReads int
	selection     RuntimeSelection
	selectionOK   bool
	freezeCount   int
}

func (r *resolveRepo) GetAgentSelection(context.Context, string, string) (RuntimeSelection, bool, error) {
	return r.selection, r.selectionOK, nil
}
func (*resolveRepo) PutAgentSelection(context.Context, string, string, RuntimeSelection, time.Time) error {
	return errors.New("unused")
}

func (r *resolveRepo) GetCatalog(context.Context, string) (Catalog, error) {
	r.catalogReads++
	return r.catalog, r.catalogErr
}
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
func (*resolveRepo) CreateProfile(context.Context, RuntimeProfile, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*resolveRepo) UpdateProfile(context.Context, RuntimeProfile, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*resolveRepo) SetDefaultProfile(context.Context, string, string, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (*resolveRepo) ApplyCatalog(context.Context, Catalog, int64, AuditEvent) (int64, error) {
	return 0, errors.New("unused")
}
func (r *resolveRepo) FreezeExecutionSnapshot(_ context.Context, _, _ string, snapshot RuntimeSnapshot) (RuntimeSnapshot, bool, error) {
	r.freezeCount++
	return snapshot, true, nil
}
func (r *resolveRepo) GetExecutionSnapshot(context.Context, string, string) (RuntimeSnapshot, bool, error) {
	r.snapshotReads++
	return r.frozen, r.frozenOK, nil
}

func resolverFixture() (*RuntimeResolver, *resolveRepo) {
	repo := &resolveRepo{catalog: Catalog{
		OrgID: "org-1", Revision: 7, DefaultProfileID: "profile-default",
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
		Profiles: []RuntimeProfile{{
			ID: "profile-default", Key: "default", CLIKey: "codex", ModelKey: "gpt",
			Enabled: true, Version: 3, Parameters: map[string]any{"effort": "high"},
		}},
	}}
	resolver := NewRuntimeResolver(repo)
	resolver.now = func() time.Time { return time.Unix(100, 0).UTC() }
	return resolver, repo
}

func TestExecutionFreezerReadsFrozenSnapshotBeforeMutableCatalog(t *testing.T) {
	want := RuntimeSnapshot{SchemaVersion: 1, CatalogRevision: 7, ModelKey: "gpt-5"}
	repo := &resolveRepo{
		catalogErr: errors.New("mutable catalog unavailable"),
		frozen:     want,
		frozenOK:   true,
	}
	if err := NewExecutionFreezer(repo).EnsureExecution(context.Background(), "org-1", "execution-1"); err != nil {
		t.Fatalf("continuation consulted mutable catalog: %v", err)
	}
	if repo.snapshotReads != 1 || repo.catalogReads != 0 {
		t.Fatalf("reads snapshot=%d catalog=%d, want 1/0", repo.snapshotReads, repo.catalogReads)
	}
}

func TestExecutionFreezerUsesPersistedAgentSelection(t *testing.T) {
	_, repo := resolverFixture()
	repo.selection = RuntimeSelection{Mode: SelectionOverride, CLIID: "cli-codex", ModelID: "model-gpt", Parameters: map[string]any{"effort": "low"}}
	repo.selectionOK = true
	if err := NewExecutionFreezer(repo).EnsureExecution(context.Background(), "org-1", "execution-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	if repo.snapshotReads != 2 || repo.catalogReads != 2 {
		t.Fatalf("snapshot/catalog reads = %d/%d, want 2/2", repo.snapshotReads, repo.catalogReads)
	}
}

func TestShadowExecutionFreezerDoesNotWriteSnapshot(t *testing.T) {
	_, repo := resolverFixture()
	var logs []string
	shadow := NewShadowExecutionFreezer(repo, func(format string, _ ...any) { logs = append(logs, format) })
	if err := shadow.EnsureExecution(context.Background(), "org-1", "execution-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	if repo.freezeCount != 0 || len(logs) != 1 {
		t.Fatalf("shadow freezeCount=%d logs=%v, want no write and one diff log", repo.freezeCount, logs)
	}
}

func TestRuntimeResolverPrecedenceAndSnapshotImmutability(t *testing.T) {
	resolver, repo := resolverFixture()
	snapshot, err := resolver.Resolve(context.Background(), "org-1", RuntimeSelection{Mode: SelectionInherit})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Source != "org_default" || snapshot.ProfileID != "profile-default" || snapshot.ModelKey != "gpt-5" {
		t.Fatalf("snapshot provenance = %+v", snapshot)
	}
	if snapshot.CatalogRevision != 7 || snapshot.ProfileKey != "default" || snapshot.ProfileVersion != 3 || snapshot.ParametersDigest == "" {
		t.Fatalf("snapshot version provenance = %+v", snapshot)
	}
	if snapshot.Parameters["temperature"] != 0.2 || snapshot.Parameters["effort"] != "high" {
		t.Fatalf("merged parameters = %#v", snapshot.Parameters)
	}
	repo.catalog.Profiles[0].Parameters["effort"] = "low"
	repo.catalog.Models[0].DefaultParameters["temperature"] = 0.9
	if snapshot.Parameters["effort"] != "high" || snapshot.Parameters["temperature"] != 0.2 {
		t.Fatalf("snapshot changed with catalog: %#v", snapshot.Parameters)
	}

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
}

func TestRuntimeResolverStructuredFailures(t *testing.T) {
	resolver, repo := resolverFixture()
	cases := []struct {
		name string
		sel  RuntimeSelection
		edit func()
		want Reason
	}{
		{"profile missing", RuntimeSelection{Mode: SelectionProfile, ProfileID: "missing"}, nil, ReasonProfileNotFound},
		{"cli disabled", RuntimeSelection{Mode: SelectionInherit}, func() { repo.catalog.CLIs[0].Enabled = false }, ReasonCLIDisabled},
		{"model disabled", RuntimeSelection{Mode: SelectionInherit}, func() { repo.catalog.Models[0].Enabled = false }, ReasonModelDisabled},
		{"incompatible", RuntimeSelection{Mode: SelectionInherit}, func() { repo.catalog.Models[0].CompatibleCLIKeys = []string{"claude-code"} }, ReasonIncompatible},
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
	explicit := RuntimeSelection{Mode: SelectionProfile, ProfileID: "profile-default"}
	snapshot, issue, err := adapter.Resolve(context.Background(), "org-1", "agent", &explicit, LegacyRuntime{CLI: "unknown", Model: "unknown"})
	if err != nil || issue != nil || snapshot.Source != "profile" || count["agent"] != 0 {
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
