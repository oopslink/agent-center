package sse

import (
	"sync"
	"sync/atomic"
)

// ringBuffer keeps the last `size` events for Last-Event-ID replay.
type ringBuffer struct {
	mu     sync.RWMutex
	size   int
	buf    []Event
	nextID int64 // monotonic event id (atomic)
}

func newRingBuffer(size int) *ringBuffer {
	if size <= 0 {
		size = 256
	}
	return &ringBuffer{size: size, buf: make([]Event, 0, size)}
}

// append assigns a fresh monotonic id, stores the event, and returns the id.
func (r *ringBuffer) append(ev Event) int64 {
	id := atomic.AddInt64(&r.nextID, 1)
	ev.ID = id
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.buf) >= r.size {
		// Drop oldest.
		r.buf = r.buf[1:]
	}
	r.buf = append(r.buf, ev)
	return id
}

// since returns every event with id > afterID, in order.
func (r *ringBuffer) since(afterID int64) []Event {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Event, 0, 8)
	for _, ev := range r.buf {
		if ev.ID > afterID {
			out = append(out, ev)
		}
	}
	return out
}

// len returns the current buffer size (test helper).
func (r *ringBuffer) len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.buf)
}
