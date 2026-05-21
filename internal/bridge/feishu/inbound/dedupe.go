package inbound

import (
	"container/list"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// DedupeWindow is the default TTL for vendor_msg_ref entries.
const DedupeWindow = 10 * time.Minute

// DedupeCacheSize is the default soft cap for in-memory dedupe entries.
// Once exceeded, the oldest entries are evicted independent of TTL.
const DedupeCacheSize = 4096

// Dedupe is the FeishuInboundDedup domain service (plan-7 § 1.5).
// Bridge BC invariant 6: same vendor_msg_ref must not be written to the
// domain twice. The cache lives in-memory only; restart recovery relies
// on the underlying domain state being idempotent (e.g.
// conversation.message_added Append catches the row-level UNIQUE
// (vendor_msg_ref) constraint).
type Dedupe struct {
	window time.Duration
	cap    int
	clock  clock.Clock

	mu     sync.Mutex
	index  map[string]*list.Element // vendor_msg_ref → entry
	order  *list.List               // FIFO for eviction
}

type dedupeEntry struct {
	ref string
	ts  time.Time
}

// NewDedupe constructs a Dedupe cache with the given window/cap (zero =
// defaults).
func NewDedupe(window time.Duration, cacheCap int, clk clock.Clock) *Dedupe {
	if window <= 0 {
		window = DedupeWindow
	}
	if cacheCap <= 0 {
		cacheCap = DedupeCacheSize
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Dedupe{
		window: window,
		cap:    cacheCap,
		clock:  clk,
		index:  map[string]*list.Element{},
		order:  list.New(),
	}
}

// SeenBefore reports whether the given vendor_msg_ref has been seen in
// the window. If not, it records the ref and returns false. Empty refs
// are never deduped (they cause an immediate `bridge.parse_failed`
// upstream).
func (d *Dedupe) SeenBefore(ref string) bool {
	if ref == "" {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.clock.Now()
	d.evictExpiredLocked(now)
	if el, ok := d.index[ref]; ok {
		entry := el.Value.(*dedupeEntry)
		// Refresh ts on hit (we only care about "seen recently").
		entry.ts = now
		d.order.MoveToBack(el)
		return true
	}
	d.evictOversizeLocked()
	el := d.order.PushBack(&dedupeEntry{ref: ref, ts: now})
	d.index[ref] = el
	return false
}

// Size returns the current number of cached refs.
func (d *Dedupe) Size() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.index)
}

func (d *Dedupe) evictExpiredLocked(now time.Time) {
	cutoff := now.Add(-d.window)
	for {
		front := d.order.Front()
		if front == nil {
			return
		}
		entry := front.Value.(*dedupeEntry)
		if !entry.ts.Before(cutoff) {
			return
		}
		d.order.Remove(front)
		delete(d.index, entry.ref)
	}
}

func (d *Dedupe) evictOversizeLocked() {
	for len(d.index) >= d.cap {
		front := d.order.Front()
		if front == nil {
			return
		}
		entry := front.Value.(*dedupeEntry)
		d.order.Remove(front)
		delete(d.index, entry.ref)
	}
}
