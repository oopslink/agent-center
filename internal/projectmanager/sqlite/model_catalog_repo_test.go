package sqlite

import (
	"context"
	"testing"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func newCatalogRepo(t *testing.T) (context.Context, *ModelCatalogRepo) {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return context.Background(), NewModelCatalogRepo(d)
}

func mkEntry(t *testing.T, id, org, modelID string, in, out float64, ctx int, tier string) *pm.ModelCatalogEntry {
	t.Helper()
	e, err := pm.NewModelCatalogEntry(pm.NewModelCatalogEntryInput{
		ID: pm.ModelCatalogEntryID(id), OrgID: org, CreatedBy: "user:a", CreatedAt: t0,
		Fields: pm.ModelCatalogFields{ModelID: modelID, DisplayName: modelID, InputCost: in, OutputCost: out, ContextWindow: ctx, Tier: tier},
	})
	if err != nil {
		t.Fatalf("new entry: %v", err)
	}
	return e
}

func TestModelCatalogRepo_RoundTrip(t *testing.T) {
	ctx, r := newCatalogRepo(t)
	e := mkEntry(t, "mdl-1", "org-1", "opus", 15, 75, 200000, "hardest tasks")
	if err := r.Save(ctx, e); err != nil {
		t.Fatal(err)
	}
	// (org, model_id) unique.
	dup := mkEntry(t, "mdl-2", "org-1", "opus", 1, 1, 1, "x")
	if err := r.Save(ctx, dup); err != pm.ErrModelCatalogEntryExists {
		t.Fatalf("dup model_id: want ErrModelCatalogEntryExists, got %v", err)
	}
	// same model_id in a DIFFERENT org is fine.
	if err := r.Save(ctx, mkEntry(t, "mdl-3", "org-2", "opus", 1, 1, 1, "y")); err != nil {
		t.Fatalf("cross-org same model_id: %v", err)
	}
	got, err := r.FindByModelID(ctx, "org-1", "opus")
	if err != nil || got.InputCost() != 15 || got.OutputCost() != 75 || got.ContextWindow() != 200000 || got.Tier() != "hardest tasks" {
		t.Fatalf("find: %+v err=%v", got, err)
	}
	// update.
	if err := got.Update(pm.ModelCatalogFields{ModelID: "opus", DisplayName: "Opus", InputCost: 10, OutputCost: 50, ContextWindow: 100000, Tier: "hard"}, t0); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := r.FindByID(ctx, "mdl-1")
	if re.InputCost() != 10 || re.Tier() != "hard" || re.Version() != 2 {
		t.Fatalf("after update: %+v", re)
	}
	// list is org-scoped (org-1 has 1, org-2 has 1).
	l1, _ := r.ListByOrg(ctx, "org-1")
	if len(l1) != 1 {
		t.Fatalf("org-1 list = %d, want 1", len(l1))
	}
	// delete.
	if err := r.Delete(ctx, "mdl-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.FindByID(ctx, "mdl-1"); err != pm.ErrModelCatalogEntryNotFound {
		t.Fatalf("after delete: want NotFound, got %v", err)
	}
}

func TestModelCatalogRepo_ImportUpsertVsReplace(t *testing.T) {
	ctx, r := newCatalogRepo(t)
	// seed two.
	_ = r.Save(ctx, mkEntry(t, "s1", "org-1", "a", 1, 1, 1, "t-a"))
	_ = r.Save(ctx, mkEntry(t, "s2", "org-1", "b", 2, 2, 2, "t-b"))

	// UPSERT: update "a" (new cost) + add "c"; "b" untouched → org has a,b,c.
	up := []*pm.ModelCatalogEntry{
		mkEntry(t, "u1", "org-1", "a", 99, 99, 99, "t-a2"),
		mkEntry(t, "u2", "org-1", "c", 3, 3, 3, "t-c"),
	}
	if err := r.UpsertForOrg(ctx, "org-1", up); err != nil {
		t.Fatal(err)
	}
	l, _ := r.ListByOrg(ctx, "org-1")
	if len(l) != 3 {
		t.Fatalf("after upsert: %d entries, want 3 (a,b,c)", len(l))
	}
	a, _ := r.FindByModelID(ctx, "org-1", "a")
	if a.InputCost() != 99 || a.Version() != 2 {
		t.Fatalf("upsert-updated a: cost=%v ver=%d, want 99/2", a.InputCost(), a.Version())
	}

	// REPLACE: swap the whole catalog for just "d" → org has ONLY d.
	if err := r.ReplaceForOrg(ctx, "org-1", []*pm.ModelCatalogEntry{mkEntry(t, "r1", "org-1", "d", 4, 4, 4, "t-d")}); err != nil {
		t.Fatal(err)
	}
	l2, _ := r.ListByOrg(ctx, "org-1")
	if len(l2) != 1 || l2[0].ModelID() != "d" {
		t.Fatalf("after replace: %v, want only [d]", l2)
	}
}
