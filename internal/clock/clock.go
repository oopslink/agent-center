// Package clock provides a Clock interface for time injection.
//
// Per conventions § 14.x: time is injectable so tests don't sleep.
package clock

import (
	"sync"
	"time"
)

// Clock returns the current time. Inject into services to control time in
// tests (FakeClock). Production code uses SystemClock.
type Clock interface {
	Now() time.Time
}

// SystemClock returns time.Now().UTC().
type SystemClock struct{}

// Now returns the current UTC time.
func (SystemClock) Now() time.Time {
	return time.Now().UTC()
}

// FakeClock returns a configurable time; safe for concurrent use.
type FakeClock struct {
	mu sync.Mutex
	t  time.Time
}

// NewFakeClock seeds a FakeClock to the given time.
func NewFakeClock(t time.Time) *FakeClock {
	return &FakeClock{t: t.UTC()}
}

// Now returns the current fake time.
func (c *FakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

// Set overrides the fake time.
func (c *FakeClock) Set(t time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = t.UTC()
}

// Advance moves the fake clock forward by d.
func (c *FakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d).UTC()
}
