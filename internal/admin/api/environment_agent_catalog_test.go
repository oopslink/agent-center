package api

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// fakeCatalogRepo is a minimal pm.ModelCatalogRepository whose only meaningful method
// is ListByOrg — the join path annotateExecutorsFromCatalog exercises. The rest are
// unused stubs so the test stays focused on the T950 ② join logic (no DB, no HTTP).
type fakeCatalogRepo struct {
	byOrg   map[string][]*pm.ModelCatalogEntry
	listErr error
}

func (f *fakeCatalogRepo) ListByOrg(_ context.Context, orgID string) ([]*pm.ModelCatalogEntry, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.byOrg[orgID], nil
}
func (f *fakeCatalogRepo) Save(context.Context, *pm.ModelCatalogEntry) error   { return nil }
func (f *fakeCatalogRepo) Update(context.Context, *pm.ModelCatalogEntry) error { return nil }
func (f *fakeCatalogRepo) FindByID(context.Context, pm.ModelCatalogEntryID) (*pm.ModelCatalogEntry, error) {
	return nil, nil
}
func (f *fakeCatalogRepo) FindByModelID(context.Context, string, string) (*pm.ModelCatalogEntry, error) {
	return nil, nil
}
func (f *fakeCatalogRepo) Delete(context.Context, pm.ModelCatalogEntryID) error { return nil }
func (f *fakeCatalogRepo) ReplaceForOrg(context.Context, string, []*pm.ModelCatalogEntry) error {
	return nil
}
func (f *fakeCatalogRepo) UpsertForOrg(context.Context, string, []*pm.ModelCatalogEntry) error {
	return nil
}

func mustEntry(t *testing.T, org string, f pm.ModelCatalogFields) *pm.ModelCatalogEntry {
	t.Helper()
	e, err := pm.NewModelCatalogEntry(pm.NewModelCatalogEntryInput{
		ID: pm.ModelCatalogEntryID("mce-" + f.ModelID), OrgID: org, Fields: f,
	})
	if err != nil {
		t.Fatalf("NewModelCatalogEntry(%q): %v", f.ModelID, err)
	}
	return e
}

// A model with a catalog row gets its tier/cost/context filled; a model with NO row is
// left neutral (all zero) — never dropped. This is the annotation the difficulty judge
// reads to pick cheapest-sufficient.
func TestAnnotateExecutorsFromCatalog_HitAndMiss(t *testing.T) {
	repo := &fakeCatalogRepo{byOrg: map[string][]*pm.ModelCatalogEntry{
		"org1": {mustEntry(t, "org1", pm.ModelCatalogFields{
			ModelID: "opus", DisplayName: "Opus 4.8", InputCost: 15, OutputCost: 75,
			ContextWindow: 200000, Tier: "hardest reasoning",
		})},
	}}
	in := []agent.ExecutorProfile{
		{CLI: "claude-code", Model: "opus"},  // hit
		{CLI: "claude-code", Model: "haiku"}, // miss (not in catalog)
	}
	got := annotateExecutorsFromCatalog(context.Background(), repo, "org1", in)

	if got[0].Tier != "hardest reasoning" || got[0].InputCost != 15 || got[0].OutputCost != 75 ||
		got[0].ContextWindow != 200000 || got[0].DisplayName != "Opus 4.8" {
		t.Errorf("hit not annotated: %+v", got[0])
	}
	if got[1].Tier != "" || got[1].InputCost != 0 || got[1].ContextWindow != 0 {
		t.Errorf("miss must stay neutral, not fabricated: %+v", got[1])
	}
	if got[1].CLI != "claude-code" || got[1].Model != "haiku" {
		t.Errorf("miss must keep {cli,model}: %+v", got[1])
	}
	// Input must not be mutated (join returns copies).
	if in[0].Tier != "" {
		t.Errorf("input mutated: %+v", in[0])
	}
}

// nil repo / empty org / lookup error → pool returned unchanged (judge degrades to
// name-only routing, never a hard error).
func TestAnnotateExecutorsFromCatalog_Degrades(t *testing.T) {
	in := []agent.ExecutorProfile{{CLI: "claude-code", Model: "opus"}}

	if got := annotateExecutorsFromCatalog(context.Background(), nil, "org1", in); got[0].Tier != "" {
		t.Errorf("nil repo should not annotate: %+v", got[0])
	}
	if got := annotateExecutorsFromCatalog(context.Background(), &fakeCatalogRepo{}, "", in); got[0].Tier != "" {
		t.Errorf("empty org should not annotate: %+v", got[0])
	}
	errRepo := &fakeCatalogRepo{listErr: errors.New("db down")}
	if got := annotateExecutorsFromCatalog(context.Background(), errRepo, "org1", in); got[0].Tier != "" {
		t.Errorf("lookup error should degrade to unannotated: %+v", got[0])
	}
}

// The wire round-trip: annotations emit as JSON keys only when non-empty, so an
// unannotated pool stays byte-identical to the pre-catalog {cli,model} shape (OFF-path
// parity), while an annotated one carries the extra keys the worker decodes.
func TestExecutorProfilesToMaps_AnnotationsOmitEmpty(t *testing.T) {
	plain := executorProfilesToMaps([]agent.ExecutorProfile{{CLI: "claude-code", Model: "haiku"}})
	if len(plain) != 1 || len(plain[0]) != 2 {
		t.Fatalf("unannotated profile must serialize to exactly {cli,model}: %v", plain[0])
	}

	rich := executorProfilesToMaps([]agent.ExecutorProfile{{
		CLI: "claude-code", Model: "opus", DisplayName: "Opus", InputCost: 15,
		OutputCost: 75, ContextWindow: 200000, Tier: "hard",
	}})
	m := rich[0]
	for _, k := range []string{"display_name", "input_cost", "output_cost", "context_window", "tier"} {
		if _, ok := m[k]; !ok {
			t.Errorf("annotated profile missing wire key %q: %v", k, m)
		}
	}
}
