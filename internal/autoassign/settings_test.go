package autoassign

import (
	"context"
	"testing"
)

// memStore is an in-memory settings.Store for the accessor tests.
type memStore map[string]string

func (m memStore) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := m[key]
	return v, ok, nil
}
func (m memStore) GetByPrefix(_ context.Context, prefix string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range m {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out[k] = v
		}
	}
	return out, nil
}
func (m memStore) Set(_ context.Context, key, value string) error {
	m[key] = value
	return nil
}

func TestAutoAssignEnabled_Defaults(t *testing.T) {
	ctx := context.Background()
	store := memStore{}

	// Absent → default ON (decision 1).
	if on, err := Enabled(ctx, store, "p1"); err != nil || !on {
		t.Fatalf("absent → (%v, %v), want (true, nil)", on, err)
	}
	// nil store → default ON (a missing backend must not disable the feature).
	if on, err := Enabled(ctx, nil, "p1"); err != nil || !on {
		t.Fatalf("nil store → (%v, %v), want (true, nil)", on, err)
	}

	// SetEnabled(false) → off; round-trips and is project-scoped.
	if err := SetEnabled(ctx, store, "p1", false); err != nil {
		t.Fatal(err)
	}
	if on, _ := Enabled(ctx, store, "p1"); on {
		t.Fatal("p1 should be disabled after SetEnabled(false)")
	}
	if on, _ := Enabled(ctx, store, "p2"); !on {
		t.Fatal("p2 (untouched) should still default ON — switch is per-project")
	}
	if store[EnabledKey("p1")] != "false" {
		t.Fatalf("stored value = %q, want \"false\"", store[EnabledKey("p1")])
	}

	// SetEnabled(true) → back ON.
	if err := SetEnabled(ctx, store, "p1", true); err != nil {
		t.Fatal(err)
	}
	if on, _ := Enabled(ctx, store, "p1"); !on {
		t.Fatal("p1 should be enabled after SetEnabled(true)")
	}
}
