// Package controlstream is the center-side SSE down-push bus for worker control
// commands (v2.7 D5 slice-1). It mirrors the webconsole sse.Bus pattern
// (subscriber map + per-route ringbuffer + heartbeat + offset/Last-Event-ID
// replay) but is adapted for the Worker control channel:
//
//   - Routed by workerID (NOT conversation_id): 1 worker = exactly 1 subscriber.
//   - Ringbuffer entries carry the full command INCLUDING its log-assigned
//     OFFSET — the resume cursor is the offset, not a synthetic SSE seq.
//   - It rides the SAME WorkerControlEvent log: the bus is a low-latency
//     down-push of what AppendCommand just committed; the poll endpoint +
//     reconnect-by-offset catch-up remain the at-least-once backstop.
//
// Placement rationale (cycle-check): this package imports internal/environment
// (for *WorkerControlEvent / WorkerID), and internal/environment injects the bus
// only via the environment.CommandPublisher INTERFACE — environment does NOT
// import controlstream. internal/admin/api imports controlstream (subscribe +
// serve) and environment (already). Import direction is one-way
// (controlstream → environment, admin/api → controlstream), so there is no cycle.
//
// The daemon stream CLIENT is a later slice; this is the CENTER side only.
package controlstream

import (
	"context"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
)

// Command is the on-wire down-push payload for one control command. It carries
// the OFFSET (resume source-of-truth) plus the same fields the poll endpoint's
// controlEventMap projects, so the daemon tracks OFFSET (not the SSE seq).
type Command struct {
	Offset         int64  `json:"offset"`
	ID             string `json:"id"`
	IdempotencyKey string `json:"idempotency_key"`
	CommandType    string `json:"command_type"`
	Payload        string `json:"payload"`
	CreatedAt      string `json:"created_at"` // RFC3339Nano, matches the poll projection.
}

// CommandFromEvent projects a domain WorkerControlEvent to the wire Command.
func CommandFromEvent(e *environment.WorkerControlEvent) Command {
	return Command{
		Offset:         e.Offset(),
		ID:             e.ID(),
		IdempotencyKey: e.IdempotencyKey(),
		CommandType:    e.CommandType(),
		Payload:        e.Payload(),
		CreatedAt:      e.CreatedAt().Format(time.RFC3339Nano),
	}
}

// DefaultHeartbeat matches the webconsole SSE cadence (30s data-frame).
const DefaultHeartbeat = 30 * time.Second

// defaultRingSize bounds the per-worker live overlap buffer. A reconnecting
// worker that fell further behind than this catches up via the log (CommandsAfter),
// so this only sizes the catch-up/live race window — small is fine.
const defaultRingSize = 256

// Subscription is one worker's live feed. Commands published for the worker AFTER
// Subscribe arrive on Ch (best-effort, buffered). Ring exposes the per-worker
// overlap buffer the endpoint uses to dedup the catch-up/live race by offset.
// Close releases the subscription.
type Subscription struct {
	WorkerID environment.WorkerID
	Ch       <-chan Command
	bus      *Bus
	ch       chan Command
	done     chan struct{}
	ring     *ringBuffer
}

// RingSince returns the per-worker buffered commands with offset > afterOffset,
// offset-ordered. The endpoint uses this together with the log catch-up to bridge
// the gap between "log max at connect" and the first live command, deduping by
// offset.
func (s *Subscription) RingSince(afterOffset int64) []Command { return s.ring.since(afterOffset) }

// RingMinOffset is the smallest offset still buffered (0 when empty).
func (s *Subscription) RingMinOffset() int64 { return s.ring.minOffset() }

// Heartbeat is the bus heartbeat interval (the endpoint owns the ticker).
func (s *Subscription) Heartbeat() time.Duration { return s.bus.heartbeat }

// Done is closed when the subscription is replaced (a newer connect for the same
// worker) or the bus shuts down. The endpoint selects on it to exit cleanly.
func (s *Subscription) Done() <-chan struct{} { return s.done }

// Close unregisters the subscription. Idempotent.
func (s *Subscription) Close() { s.bus.unsubscribe(s) }

// Bus is the per-worker subscriber pool. Each worker has at most one live
// subscriber (a new Subscribe replaces the old one, mirroring sse.Bus). A
// per-worker ringbuffer backs the catch-up/live overlap window AND survives
// across reconnects (so a publish landing between connections is still in the
// ring for the next connect's overlap check, complementing the log).
type Bus struct {
	mu        sync.RWMutex
	subs      map[environment.WorkerID]*Subscription
	rings     map[environment.WorkerID]*ringBuffer
	heartbeat time.Duration
	ringSize  int
}

// NewBus returns a fresh Bus with the default heartbeat + ring size.
func NewBus() *Bus {
	return &Bus{
		subs:      make(map[environment.WorkerID]*Subscription),
		rings:     make(map[environment.WorkerID]*ringBuffer),
		heartbeat: DefaultHeartbeat,
		ringSize:  defaultRingSize,
	}
}

// ringFor returns (creating if needed) the per-worker ringbuffer. Caller holds
// b.mu.
func (b *Bus) ringFor(workerID environment.WorkerID) *ringBuffer {
	r := b.rings[workerID]
	if r == nil {
		r = newRingBuffer(b.ringSize)
		b.rings[workerID] = r
	}
	return r
}

// Publish fans a command out to the worker's live subscriber (if connected) and
// records it in the worker's ringbuffer for the catch-up/live overlap window.
// BEST-EFFORT: a full/absent subscriber channel is dropped (the poll fallback +
// reconnect catch-up recovers it). Satisfies environment.CommandPublisher.
func (b *Bus) Publish(e *environment.WorkerControlEvent) {
	if e == nil {
		return
	}
	cmd := CommandFromEvent(e)
	b.mu.RLock()
	defer b.mu.RUnlock()
	// Record into the ring FIRST so a subscriber connecting concurrently (and
	// reading RingSince) cannot miss it.
	b.ringFor(e.WorkerID()).append(cmd)
	sub := b.subs[e.WorkerID()]
	if sub == nil {
		return
	}
	select {
	case sub.ch <- cmd:
	case <-sub.done:
	default:
		// Channel full; drop. The reconnect-by-offset catch-up (CommandsAfter)
		// re-delivers — at-least-once is held by the LOG, not the bus.
	}
}

// Subscribe registers (or replaces) the live subscriber for a worker and returns
// its Subscription. The endpoint MUST Subscribe BEFORE reading the log catch-up
// so no command published in the race window is lost: a command appended after
// the catch-up snapshot lands on Ch (and in the ring), and the endpoint dedups
// the catch-up/live overlap by offset.
func (b *Bus) Subscribe(workerID environment.WorkerID) *Subscription {
	b.mu.Lock()
	defer b.mu.Unlock()
	if old, ok := b.subs[workerID]; ok {
		close(old.done) // boot the prior connection for this worker.
		delete(b.subs, workerID)
	}
	ch := make(chan Command, 64)
	sub := &Subscription{
		WorkerID: workerID,
		Ch:       ch,
		bus:      b,
		ch:       ch,
		done:     make(chan struct{}),
		ring:     b.ringFor(workerID),
	}
	b.subs[workerID] = sub
	return sub
}

// unsubscribe removes a subscription if it is still the active one for its
// worker. Idempotent. The ring is intentionally retained for the next connect's
// overlap window.
func (b *Bus) unsubscribe(sub *Subscription) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if cur, ok := b.subs[sub.WorkerID]; ok && cur == sub {
		select {
		case <-sub.done:
		default:
			close(sub.done)
		}
		delete(b.subs, sub.WorkerID)
	}
}

// SetHeartbeat overrides the heartbeat interval (used by tests + boot tuning).
// Affects subscriptions created AFTER the call.
func (b *Bus) SetHeartbeat(d time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if d > 0 {
		b.heartbeat = d
	}
}

// SubscriberCount returns the active subscriber count (test helper).
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

// Shutdown closes every live subscriber. Idempotent.
func (b *Bus) Shutdown(_ context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, sub := range b.subs {
		select {
		case <-sub.done:
		default:
			close(sub.done)
		}
		delete(b.subs, id)
	}
	return nil
}

// Compile-time: Bus satisfies the log's optional publish hook.
var _ environment.CommandPublisher = (*Bus)(nil)
