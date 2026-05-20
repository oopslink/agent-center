package clock

import (
	"sync"
	"testing"
	"time"
)

func TestSystemClock_NowIsUTC(t *testing.T) {
	c := SystemClock{}
	got := c.Now()
	if loc := got.Location(); loc.String() != "UTC" {
		t.Fatalf("SystemClock.Now() returned non-UTC time: %v (location=%v)", got, loc)
	}
}

func TestSystemClock_NowMonotonic(t *testing.T) {
	c := SystemClock{}
	t1 := c.Now()
	t2 := c.Now()
	if t2.Before(t1) {
		t.Fatalf("SystemClock.Now() not monotonic: %v then %v", t1, t2)
	}
}

func TestFakeClock_NewSetsTime(t *testing.T) {
	want := time.Date(2026, 5, 20, 10, 23, 0, 0, time.UTC)
	c := NewFakeClock(want)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("NewFakeClock not seeded: got %v want %v", got, want)
	}
}

func TestFakeClock_ConvertsToUTC(t *testing.T) {
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	in := time.Date(2026, 5, 20, 10, 23, 0, 0, loc)
	c := NewFakeClock(in)
	if got := c.Now().Location().String(); got != "UTC" {
		t.Fatalf("FakeClock did not convert to UTC: location=%s", got)
	}
}

func TestFakeClock_Set(t *testing.T) {
	c := NewFakeClock(time.Now())
	want := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	c.Set(want)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("FakeClock.Set: got %v want %v", got, want)
	}
}

func TestFakeClock_Advance(t *testing.T) {
	base := time.Date(2026, 5, 20, 0, 0, 0, 0, time.UTC)
	c := NewFakeClock(base)
	c.Advance(time.Hour)
	want := base.Add(time.Hour)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("FakeClock.Advance: got %v want %v", got, want)
	}
}

func TestFakeClock_ConcurrentSafe(t *testing.T) {
	c := NewFakeClock(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() { defer wg.Done(); c.Advance(time.Second) }()
		go func() { defer wg.Done(); _ = c.Now() }()
	}
	wg.Wait()
	// 50 advances of 1s
	want := time.Date(2026, 1, 1, 0, 0, 50, 0, time.UTC)
	if got := c.Now(); !got.Equal(want) {
		t.Fatalf("FakeClock concurrent advance: got %v want %v", got, want)
	}
}
