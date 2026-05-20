package idgen

import (
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

func TestNewULID_FormatAndLen(t *testing.T) {
	g := NewGenerator(clock.SystemClock{})
	id := g.NewULID()
	if len(id) != 26 {
		t.Fatalf("ULID length: got %d want 26 (%s)", len(id), id)
	}
	if !IsValid(id) {
		t.Fatalf("ULID %q not parseable", id)
	}
}

func TestNewULID_UniquePerCall(t *testing.T) {
	g := NewGenerator(clock.SystemClock{})
	seen := make(map[string]struct{}, 1000)
	for i := 0; i < 1000; i++ {
		id := g.NewULID()
		if _, dup := seen[id]; dup {
			t.Fatalf("ULID dup at iteration %d: %s", i, id)
		}
		seen[id] = struct{}{}
	}
}

func TestNewULID_StrictlyMonotonicSameMillisecond(t *testing.T) {
	fixed := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	fc := clock.NewFakeClock(fixed)
	g := NewGenerator(fc)
	prev := g.NewULID()
	for i := 0; i < 100; i++ {
		cur := g.NewULID()
		if cur <= prev {
			t.Fatalf("not monotonic at i=%d: prev=%s cur=%s", i, prev, cur)
		}
		prev = cur
	}
}

func TestNewULID_TimestampMatchesClock(t *testing.T) {
	want := time.Date(2026, 5, 20, 10, 23, 0, 0, time.UTC)
	fc := clock.NewFakeClock(want)
	g := NewGenerator(fc)
	id := g.NewULID()
	got, ok := Time(id)
	if !ok {
		t.Fatalf("Time(%s) parse failed", id)
	}
	// ULID timestamp is millisecond precision.
	if got.UnixMilli() != want.UnixMilli() {
		t.Fatalf("timestamp mismatch: got %v want %v", got, want)
	}
}

func TestNewULID_ConcurrentMonotonic(t *testing.T) {
	// Fixed clock so all 100×100 IDs land in the same millisecond — exercises
	// the monotonic entropy source under contention.
	fc := clock.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g := NewGenerator(fc)
	const goroutines = 20
	const perG = 200

	results := make([][]string, goroutines)
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			ids := make([]string, perG)
			for j := 0; j < perG; j++ {
				ids[j] = g.NewULID()
			}
			results[i] = ids
		}(i)
	}
	wg.Wait()

	seen := make(map[string]struct{}, goroutines*perG)
	for _, ids := range results {
		for _, id := range ids {
			if _, dup := seen[id]; dup {
				t.Fatalf("dup id under concurrency: %s", id)
			}
			seen[id] = struct{}{}
		}
	}
}

func TestIsValid(t *testing.T) {
	cases := []struct {
		in string
		ok bool
	}{
		{"", false},
		{"not-a-ulid", false},
		{"01ARZ3NDEKTSV4RRFFQ69G5FAV", true},
		// 25 chars (one short)
		{"01ARZ3NDEKTSV4RRFFQ69G5FA", false},
	}
	for _, c := range cases {
		got := IsValid(c.in)
		if got != c.ok {
			t.Fatalf("IsValid(%q)=%v want %v", c.in, got, c.ok)
		}
	}
}

func TestMustNewULID(t *testing.T) {
	id := MustNewULID()
	if !IsValid(id) {
		t.Fatalf("MustNewULID returned invalid: %s", id)
	}
}

func TestNewGenerator_NilClockUsesSystem(t *testing.T) {
	g := NewGenerator(nil)
	id := g.NewULID()
	if !IsValid(id) {
		t.Fatalf("nil-clock generator invalid id: %s", id)
	}
}

func TestNewGeneratorWithReader_DeterministicSequence(t *testing.T) {
	fc := clock.NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	g1 := NewGeneratorWithReader(fc, DeterministicReader(42))
	g2 := NewGeneratorWithReader(fc, DeterministicReader(42))
	// Same seed → same first id.
	if a, b := g1.NewULID(), g2.NewULID(); a != b {
		t.Fatalf("deterministic seed mismatch: %s vs %s", a, b)
	}
}

func TestNewGeneratorWithReader_NilClock(t *testing.T) {
	g := NewGeneratorWithReader(nil, DeterministicReader(1))
	id := g.NewULID()
	if !IsValid(id) {
		t.Fatalf("invalid id: %s", id)
	}
}

func TestTime_InvalidReturnsFalse(t *testing.T) {
	if _, ok := Time("garbage"); ok {
		t.Fatal("expected ok=false for invalid ULID")
	}
}
