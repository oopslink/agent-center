package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// fakeGCRepo records the cutoff each call saw and drains a fixed backlog in batches,
// so the service-level tests can assert retention math + batched-drain looping without
// a real DB.
type fakeGCRepo struct {
	backlog    int // acked+old rows still to delete
	lastCutoff time.Time
	calls      int
}

func (r *fakeGCRepo) DeleteAckedBefore(_ context.Context, cutoff time.Time, limit int) (int64, error) {
	r.calls++
	r.lastCutoff = cutoff
	if r.backlog <= 0 {
		return 0, nil
	}
	n := limit
	if n > r.backlog {
		n = r.backlog
	}
	r.backlog -= n
	return int64(n), nil
}

func TestControlEventGC_Tick_CutoffIsNowMinusRetention(t *testing.T) {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFakeClock(now)
	repo := &fakeGCRepo{backlog: 0}
	gc := NewControlEventGC(repo, clk, 3*24*time.Hour, time.Hour, nil)

	if _, err := gc.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	want := now.Add(-3 * 24 * time.Hour)
	if !repo.lastCutoff.Equal(want) {
		t.Fatalf("cutoff = %s, want now-3d = %s", repo.lastCutoff, want)
	}
}

func TestControlEventGC_Tick_ConfigurableRetention(t *testing.T) {
	now := time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)
	clk := clock.NewFakeClock(now)
	repo := &fakeGCRepo{}
	// A 1-hour retention (env-overridden) must flow straight into the cutoff.
	gc := NewControlEventGC(repo, clk, time.Hour, time.Hour, nil)
	if _, err := gc.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if want := now.Add(-time.Hour); !repo.lastCutoff.Equal(want) {
		t.Fatalf("cutoff = %s, want now-1h = %s", repo.lastCutoff, want)
	}
}

func TestControlEventGC_Tick_DrainsInBatches(t *testing.T) {
	clk := clock.NewFakeClock(time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC))
	// 1200 rows, default batch 500 → 500 + 500 + 200 across 3 repo calls, all in one Tick.
	repo := &fakeGCRepo{backlog: 1200}
	gc := NewControlEventGC(repo, clk, 0, 0, nil)

	total, err := gc.Tick(context.Background())
	if err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if total != 1200 {
		t.Fatalf("total pruned = %d, want 1200", total)
	}
	if repo.calls != 3 {
		t.Fatalf("repo calls = %d, want 3 (500+500+200)", repo.calls)
	}
	if repo.backlog != 0 {
		t.Fatalf("backlog should be drained, got %d", repo.backlog)
	}
}

func TestControlEventGC_Defaults(t *testing.T) {
	gc := NewControlEventGC(&fakeGCRepo{}, nil, 0, 0, nil)
	if gc.retention != DefaultControlEventRetention {
		t.Fatalf("retention default = %s, want %s", gc.retention, DefaultControlEventRetention)
	}
	if gc.interval != DefaultControlEventGCInterval {
		t.Fatalf("interval default = %s, want %s", gc.interval, DefaultControlEventGCInterval)
	}
	if DefaultControlEventRetention != 3*24*time.Hour {
		t.Fatalf("retention default must be 3 days (oopslink ruling), got %s", DefaultControlEventRetention)
	}
}

// A drained backlog stops after one empty call (n < batch) — no spin.
func TestControlEventGC_Tick_StopsWhenEmpty(t *testing.T) {
	clk := clock.NewFakeClock(time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC))
	repo := &fakeGCRepo{backlog: 0}
	gc := NewControlEventGC(repo, clk, 0, 0, nil)
	if _, err := gc.Tick(context.Background()); err != nil {
		t.Fatalf("Tick: %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("repo calls = %d, want 1 (single empty pass)", repo.calls)
	}
}
