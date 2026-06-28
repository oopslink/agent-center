package executor

import (
	"errors"
	"testing"
)

func TestPool_Adopt_TakesSlotWithoutSpawning(t *testing.T) {
	pool, git := newTestPool(t, 2, nil)
	if err := pool.Adopt("recovered-1"); err != nil {
		t.Fatalf("Adopt: %v", err)
	}
	if pool.Active() != 1 {
		t.Errorf("Active = %d, want 1", pool.Active())
	}
	// Adopt must NOT spawn / provision: no git worktree calls happened.
	if len(git.args) != 0 {
		t.Errorf("Adopt must not provision a worktree, git args = %v", git.args)
	}
	// A handle-less reservation is not surfaced as a live Handle.
	if len(pool.Handles()) != 0 {
		t.Errorf("Handles = %d, want 0 (adopted is handle-less)", len(pool.Handles()))
	}
	// ...but it still frees normally.
	if !pool.Release("recovered-1") {
		t.Error("Release must free an adopted slot")
	}
}

func TestPool_Adopt_DuplicateAndCapacity(t *testing.T) {
	pool, _ := newTestPool(t, 1, nil)
	if err := pool.Adopt("a"); err != nil {
		t.Fatalf("Adopt a: %v", err)
	}
	if err := pool.Adopt("a"); !errors.Is(err, ErrAlreadyActive) {
		t.Errorf("re-adopt err = %v, want ErrAlreadyActive", err)
	}
	if err := pool.Adopt("b"); !errors.Is(err, ErrAtCapacity) {
		t.Errorf("over-cap adopt err = %v, want ErrAtCapacity", err)
	}
}

func TestPool_Adopt_InvalidID(t *testing.T) {
	pool, _ := newTestPool(t, 2, nil)
	if err := pool.Adopt("bad/id"); err == nil {
		t.Error("invalid id must be rejected")
	}
	if pool.Active() != 0 {
		t.Errorf("rejected adopt must not occupy a slot, Active = %d", pool.Active())
	}
}
