package api

import (
	"net/http/httptest"
	"testing"
	"time"
)

// TestTimeFilter_TzSafe — the v2.8.1 work-items time-range filter compares the
// FE's absolute RFC3339 instants (local date + tz offset) against UTC-stored
// timestamps, so a GMT+8 "today" range has NO off-by-one at the day boundary.
func TestTimeFilter_TzSafe(t *testing.T) {
	// GMT+8 "today" (2026-06-08) → [00:00+08:00, 23:59:59+08:00]
	// == [2026-06-07T16:00:00Z, 2026-06-08T15:59:59Z].
	r := httptest.NewRequest("GET",
		"/api/tasks?created_after=2026-06-08T00:00:00%2B08:00&created_before=2026-06-08T23:59:59%2B08:00", nil)
	tf, err := parseTimeFilter(r)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		ts   time.Time
		want bool
	}{
		{"midday UTC within GMT+8 today", time.Date(2026, 6, 8, 10, 0, 0, 0, time.UTC), true},
		{"inclusive lower bound (07T16:00Z)", time.Date(2026, 6, 7, 16, 0, 0, 0, time.UTC), true},
		{"the day before in GMT+8 (07T10:00Z=18:00+08 on 7th)", time.Date(2026, 6, 7, 10, 0, 0, 0, time.UTC), false},
		{"after the window (08T16:00Z=00:00+08 on 9th)", time.Date(2026, 6, 8, 16, 0, 0, 0, time.UTC), false},
	}
	for _, c := range cases {
		if got := tf.passes(c.ts, c.ts); got != c.want {
			t.Errorf("%s: passes=%v want %v", c.name, got, c.want)
		}
	}
}

func TestTimeFilter_BadFormatIs400(t *testing.T) {
	// A bare date (not RFC3339) must error → 400, never silently ignored (which
	// would over-return rows).
	r := httptest.NewRequest("GET", "/api/tasks?updated_after=2026-06-08", nil)
	if _, err := parseTimeFilter(r); err == nil {
		t.Fatal("bare date must be rejected (RFC3339 required)")
	}
}

func TestTimeFilter_UnsetPassesAll(t *testing.T) {
	r := httptest.NewRequest("GET", "/api/tasks", nil)
	tf, err := parseTimeFilter(r)
	if err != nil {
		t.Fatal(err)
	}
	ts := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if !tf.passes(ts, ts) {
		t.Error("no time filter → every row passes")
	}
}
