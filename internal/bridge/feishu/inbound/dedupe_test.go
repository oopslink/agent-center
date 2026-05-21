package inbound

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

func TestDedupe_FirstThenRepeat(t *testing.T) {
	c := clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC))
	d := NewDedupe(0, 0, c)
	if d.SeenBefore("ref-1") {
		t.Fatal("first seen returned true")
	}
	if !d.SeenBefore("ref-1") {
		t.Fatal("second seen returned false")
	}
	if d.Size() != 1 {
		t.Errorf("size: %d", d.Size())
	}
}

func TestDedupe_EmptyRefNeverDedupes(t *testing.T) {
	d := NewDedupe(0, 0, nil)
	if d.SeenBefore("") {
		t.Fatal("empty ref should not be deduped")
	}
	if d.SeenBefore("") {
		t.Fatal("empty ref still should not dedupe second time")
	}
}

func TestDedupe_TTLExpiry(t *testing.T) {
	c := clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC))
	d := NewDedupe(1*time.Minute, 0, c)
	d.SeenBefore("ref-1")
	c.Advance(2 * time.Minute)
	if d.SeenBefore("ref-1") {
		t.Fatal("expected ref-1 to have expired after window")
	}
}

func TestDedupe_EvictsBeyondCap(t *testing.T) {
	c := clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC))
	d := NewDedupe(0, 3, c)
	for _, ref := range []string{"a", "b", "c", "d"} {
		d.SeenBefore(ref)
	}
	if d.Size() != 3 {
		t.Errorf("expected eviction to keep size=3, got %d", d.Size())
	}
	// Probe "b" and "c" first (still cached). Re-probing also refreshes
	// their FIFO position, so we MUST check them before the
	// "a was evicted" assertion (which itself touches the cache).
	if !d.SeenBefore("b") {
		t.Error("'b' should still be present")
	}
	if !d.SeenBefore("c") {
		t.Error("'c' should still be present")
	}
	if !d.SeenBefore("d") {
		t.Error("'d' should still be present")
	}
	// Now confirm "a" was evicted (re-insertion is a side effect we accept).
	if d.SeenBefore("a") {
		t.Error("'a' should have been evicted")
	}
}

func TestDedupe_HitRefreshesTimestamp(t *testing.T) {
	c := clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC))
	d := NewDedupe(2*time.Minute, 0, c)
	d.SeenBefore("ref-1")
	c.Advance(1 * time.Minute)
	if !d.SeenBefore("ref-1") {
		t.Fatal("ref-1 should still be deduped within window")
	}
	c.Advance(90 * time.Second)
	// Total elapsed is 2m30s but since first hit refreshed at 1m,
	// the window from refresh has only elapsed 1m30s — still deduped.
	if !d.SeenBefore("ref-1") {
		t.Fatal("refreshed entry should still be deduped")
	}
}
