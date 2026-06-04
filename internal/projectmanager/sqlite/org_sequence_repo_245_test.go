package sqlite

import (
	"context"
	"path/filepath"
	"sort"
	"sync"
	"testing"

	"github.com/oopslink/agent-center/internal/persistence"
)

// v2.7.1 #245: OrgSequenceRepo allocates per-(org, type) monotonic numbers.

func TestOrgSequenceRepo_SequentialAndIsolated(t *testing.T) {
	ctx := context.Background()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	r := NewOrgSequenceRepo(d)

	// Sequential within (org1, task): 1, 2, 3.
	for want := 1; want <= 3; want++ {
		got, err := r.Allocate(ctx, "org1", "task")
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("org1/task alloc #%d = %d, want %d", want, got, want)
		}
	}
	// (org1, issue) is an independent counter → starts at 1.
	if got, _ := r.Allocate(ctx, "org1", "issue"); got != 1 {
		t.Fatalf("org1/issue first alloc = %d, want 1 (independent of task counter)", got)
	}
	// (org2, task) is independent of org1 → starts at 1.
	if got, _ := r.Allocate(ctx, "org2", "task"); got != 1 {
		t.Fatalf("org2/task first alloc = %d, want 1 (independent of org1)", got)
	}
	// org1/task continues at 4.
	if got, _ := r.Allocate(ctx, "org1", "task"); got != 4 {
		t.Fatalf("org1/task next = %d, want 4", got)
	}
}

// Concurrency: N goroutines allocating from the same (org, type) must produce a
// permutation of 1..N — no collision (distinct), no skip (covers the range). A
// file db (WAL + busy_timeout) serializes the writers so the UPSERT...RETURNING
// is atomic per allocation. This is the race-safety Tester stresses in round-4.
func TestOrgSequenceRepo_ConcurrentNoCollisionNoSkip(t *testing.T) {
	dir := t.TempDir()
	d, err := persistence.Open(filepath.Join(dir, "seq.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	r := NewOrgSequenceRepo(d)

	const N = 50
	results := make([]int, N)
	errs := make([]error, N)
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = r.Allocate(context.Background(), "orgC", "task")
		}(i)
	}
	wg.Wait()

	for i, e := range errs {
		if e != nil {
			t.Fatalf("goroutine %d allocate error: %v", i, e)
		}
	}
	sort.Ints(results)
	for i, got := range results {
		if got != i+1 {
			t.Fatalf("concurrent allocations are not a 1..%d permutation: sorted[%d]=%d (collision or skip)", N, i, got)
		}
	}
}
