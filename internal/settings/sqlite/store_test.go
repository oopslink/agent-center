package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/persistence"
)

// newStore opens a migrated in-memory DB and returns a store over it. It also
// exercises migration 0064_center_settings (the table must exist for any op to
// work).
func newStore(t *testing.T) (*Store, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 6, 20, 12, 0, 0, 0, time.UTC))
	return NewStore(db, clk), context.Background()
}

func TestStore_GetMissing(t *testing.T) {
	s, ctx := newStore(t)
	v, found, err := s.Get(ctx, "wake.max_depth")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found || v != "" {
		t.Errorf("missing key: got (%q, %v), want (\"\", false)", v, found)
	}
}

func TestStore_SetGet_Upsert(t *testing.T) {
	s, ctx := newStore(t)
	if err := s.Set(ctx, "wake.max_depth", "4"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	v, found, err := s.Get(ctx, "wake.max_depth")
	if err != nil || !found || v != "4" {
		t.Fatalf("after Set: got (%q, %v, %v), want (\"4\", true, nil)", v, found, err)
	}
	// Last write wins.
	if err := s.Set(ctx, "wake.max_depth", "6"); err != nil {
		t.Fatalf("Set#2: %v", err)
	}
	v, _, _ = s.Get(ctx, "wake.max_depth")
	if v != "6" {
		t.Errorf("upsert: got %q, want \"6\"", v)
	}
}

func TestStore_GetByPrefix(t *testing.T) {
	s, ctx := newStore(t)
	for k, v := range map[string]string{
		"wake.max_depth":    "4",
		"wake.rate_per_min": "10",
		"other.unrelated":   "x",
	} {
		if err := s.Set(ctx, k, v); err != nil {
			t.Fatalf("Set %s: %v", k, err)
		}
	}
	m, err := s.GetByPrefix(ctx, "wake.")
	if err != nil {
		t.Fatalf("GetByPrefix: %v", err)
	}
	if len(m) != 2 || m["wake.max_depth"] != "4" || m["wake.rate_per_min"] != "10" {
		t.Errorf("prefix scan: got %v, want only the two wake.* keys", m)
	}
	if _, ok := m["other.unrelated"]; ok {
		t.Errorf("prefix scan leaked a non-matching key: %v", m)
	}
}
