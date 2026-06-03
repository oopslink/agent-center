package controlstream

import "sync"

// ringBuffer keeps the last `size` published commands for a single worker, for
// the connect-time catch-up/live overlap window. Entries carry the command's
// OFFSET (the source-of-truth resume cursor) — NOT a separate SSE sequence.
//
// Unlike the webconsole sse.ringBuffer (one global ring keyed by a synthetic
// monotonic id), this ring is PER-WORKER and stores commands keyed by their
// log-assigned offset, so eviction + overlap-dedup are both offset-driven.
type ringBuffer struct {
	mu   sync.RWMutex
	size int
	buf  []Command
}

func newRingBuffer(size int) *ringBuffer {
	if size <= 0 {
		size = 256
	}
	return &ringBuffer{size: size, buf: make([]Command, 0, size)}
}

// append stores a command, evicting the oldest when full. The command is
// inserted keeping the buffer ordered by offset (appends are normally already
// in offset order since the log assigns monotonically, but a late/out-of-order
// publish is placed correctly so since() stays ordered).
func (r *ringBuffer) append(cmd Command) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Find insertion point to keep offset-ascending order.
	i := len(r.buf)
	for i > 0 && r.buf[i-1].Offset > cmd.Offset {
		i--
	}
	// Drop an exact-offset duplicate already present (publish is at-least-once).
	if i > 0 && r.buf[i-1].Offset == cmd.Offset {
		return
	}
	r.buf = append(r.buf, Command{})
	copy(r.buf[i+1:], r.buf[i:])
	r.buf[i] = cmd
	if len(r.buf) > r.size {
		r.buf = r.buf[len(r.buf)-r.size:]
	}
}

// since returns every buffered command with offset strictly greater than
// afterOffset, in ascending offset order.
func (r *ringBuffer) since(afterOffset int64) []Command {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Command, 0, 8)
	for _, c := range r.buf {
		if c.Offset > afterOffset {
			out = append(out, c)
		}
	}
	return out
}

// minOffset returns the smallest offset currently buffered (0 when empty). Used
// to detect a ringbuffer gap (eviction) so catch-up falls back to the log.
func (r *ringBuffer) minOffset() int64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if len(r.buf) == 0 {
		return 0
	}
	return r.buf[0].Offset
}

// len returns the current buffer size (test helper).
func (r *ringBuffer) len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.buf)
}
