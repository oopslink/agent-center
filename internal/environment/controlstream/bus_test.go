package controlstream

import (
	"strconv"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/environment"
)

func mkEvent(t *testing.T, workerID string, offset int64, key string) *environment.WorkerControlEvent {
	t.Helper()
	e, err := environment.NewWorkerControlEvent(environment.NewWorkerControlEventInput{
		ID: "evt-" + key, WorkerID: environment.WorkerID(workerID), Offset: offset,
		IdempotencyKey: key, CommandType: "agent.start", Payload: `{"x":1}`,
		CreatedAt: time.Unix(1_700_000_000, 0).UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func recv(t *testing.T, ch <-chan Command) (Command, bool) {
	t.Helper()
	select {
	case c, ok := <-ch:
		return c, ok
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for command")
		return Command{}, false
	}
}

func TestBus_SubscribePublishDeliversToWorker(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe("W1")
	defer sub.Close()

	b.Publish(mkEvent(t, "W1", 1, "k1"))
	got, _ := recv(t, sub.Ch)
	if got.Offset != 1 || got.IdempotencyKey != "k1" || got.CommandType != "agent.start" {
		t.Fatalf("unexpected delivered command: %+v", got)
	}
}

func TestBus_RoutesByWorkerID(t *testing.T) {
	b := NewBus()
	s1 := b.Subscribe("W1")
	defer s1.Close()
	s2 := b.Subscribe("W2")
	defer s2.Close()

	b.Publish(mkEvent(t, "W2", 1, "k1"))

	// W2 receives it.
	got, _ := recv(t, s2.Ch)
	if got.Offset != 1 {
		t.Fatalf("W2 want offset 1, got %d", got.Offset)
	}
	// W1 must NOT receive it.
	select {
	case c := <-s1.Ch:
		t.Fatalf("W1 must not receive W2's command, got %+v", c)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestBus_OneSubscriberPerWorker_NewReplacesOld(t *testing.T) {
	b := NewBus()
	s1 := b.Subscribe("W1")
	s2 := b.Subscribe("W1") // replaces s1
	defer s2.Close()

	if b.SubscriberCount() != 1 {
		t.Fatalf("want 1 subscriber per worker, got %d", b.SubscriberCount())
	}
	// s1's done channel is closed (booted).
	select {
	case <-s1.done:
	case <-time.After(time.Second):
		t.Fatal("old subscriber should have been booted")
	}
	b.Publish(mkEvent(t, "W1", 1, "k1"))
	got, _ := recv(t, s2.Ch)
	if got.Offset != 1 {
		t.Fatalf("new subscriber should receive, got %+v", got)
	}
}

func TestBus_RingbufferReplayAndEvict(t *testing.T) {
	b := NewBus()
	b.ringSize = 3 // shrink for the test
	sub := b.Subscribe("W1")
	defer sub.Close()

	// Publish 5 commands; ring holds the last 3 (offsets 3,4,5).
	for i := int64(1); i <= 5; i++ {
		b.Publish(mkEvent(t, "W1", i, key(i)))
		recv(t, sub.Ch) // drain live channel so it never blocks
	}
	replay := sub.RingSince(0)
	if len(replay) != 3 || replay[0].Offset != 3 || replay[2].Offset != 5 {
		t.Fatalf("ring after evict want offsets [3,4,5], got %+v", replay)
	}
	// RingSince is offset-driven.
	if got := sub.RingSince(4); len(got) != 1 || got[0].Offset != 5 {
		t.Fatalf("RingSince(4) want [5], got %+v", got)
	}
	if min := sub.RingMinOffset(); min != 3 {
		t.Fatalf("RingMinOffset want 3, got %d", min)
	}
}

func TestBus_RingDedupsDuplicateOffset(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe("W1")
	defer sub.Close()
	b.Publish(mkEvent(t, "W1", 1, "k1"))
	recv(t, sub.Ch)
	b.Publish(mkEvent(t, "W1", 1, "k1")) // at-least-once re-publish of same offset
	if got := sub.RingSince(0); len(got) != 1 {
		t.Fatalf("ring must dedup same-offset publish, got %d entries", len(got))
	}
}

func TestBus_PublishWithNoSubscriberDoesNotBlock_RingStillRecords(t *testing.T) {
	b := NewBus()
	// No subscriber yet. Publish must not block and must record into the ring so
	// the next connect's overlap check sees it (missed-publish recovery).
	b.Publish(mkEvent(t, "W1", 1, "k1"))
	sub := b.Subscribe("W1")
	defer sub.Close()
	if got := sub.RingSince(0); len(got) != 1 || got[0].Offset != 1 {
		t.Fatalf("ring should retain pre-subscribe publish, got %+v", got)
	}
}

func TestBus_PublishFullChannelDropsNotBlocks(t *testing.T) {
	b := NewBus()
	sub := b.Subscribe("W1")
	defer sub.Close()
	// Overfill beyond the 64 buffer without draining; Publish must never block.
	done := make(chan struct{})
	go func() {
		for i := int64(1); i <= 1000; i++ {
			b.Publish(mkEvent(t, "W1", i, key(i)))
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a full subscriber channel")
	}
}

func key(i int64) string {
	return "k" + strconv.FormatInt(i, 10)
}
