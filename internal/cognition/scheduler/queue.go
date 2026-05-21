package scheduler

import (
	"errors"
	"sync"
)

// ErrQueueFull is returned by SpawnQueue.Enqueue when the global cap is
// reached.
var ErrQueueFull = errors.New("scheduler: spawn queue full")

// InMemoryQueue is a thread-safe FIFO SpawnQueue with a global cap.
// Implements SpawnQueue.
type InMemoryQueue struct {
	mu    sync.Mutex
	items []InvocationRequest
	cap   int
}

// NewInMemoryQueue constructs a queue with maxLen as the global cap.
func NewInMemoryQueue(maxLen int) *InMemoryQueue {
	if maxLen <= 0 {
		maxLen = 5
	}
	return &InMemoryQueue{cap: maxLen}
}

// Enqueue appends a request; returns ErrQueueFull when at capacity.
func (q *InMemoryQueue) Enqueue(req InvocationRequest) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) >= q.cap {
		return ErrQueueFull
	}
	q.items = append(q.items, req)
	return nil
}

// Dequeue pops the head; returns ok=false when empty.
func (q *InMemoryQueue) Dequeue() (InvocationRequest, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return InvocationRequest{}, false
	}
	item := q.items[0]
	q.items = q.items[1:]
	return item, true
}

// Len returns the current queue length.
func (q *InMemoryQueue) Len() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.items)
}
