package sse

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBus_SubscribeUnsubscribe(t *testing.T) {
	b := NewBus()
	if err := b.Subscribe("u1", "c1"); err != nil {
		t.Fatal(err)
	}
	if !b.IsSubscribed("u1", "c1") {
		t.Fatal()
	}
	if err := b.Unsubscribe("u1", "c1"); err != nil {
		t.Fatal(err)
	}
	if b.IsSubscribed("u1", "c1") {
		t.Fatal()
	}
}

func TestBus_SubscribeBadArgs(t *testing.T) {
	b := NewBus()
	if err := b.Subscribe("", "c1"); err == nil {
		t.Fatal()
	}
	if err := b.Subscribe("u1", ""); err == nil {
		t.Fatal()
	}
	if err := b.Unsubscribe("", "c1"); err == nil {
		t.Fatal()
	}
	if err := b.Unsubscribe("u1", ""); err == nil {
		t.Fatal()
	}
}

func TestBus_UnsubscribeIdempotent(t *testing.T) {
	b := NewBus()
	if err := b.Unsubscribe("u1", "c-no"); err != nil {
		t.Fatalf("idempotent unsubscribe: %v", err)
	}
}

func TestBus_Publish_RoutesByConversationID(t *testing.T) {
	b := NewBus()
	b.heartbeat = 10 * time.Millisecond
	_ = b.Subscribe("u1", "c1")
	// SSE handler picks up via channel — we test routing here via the
	// matches helper (avoids HTTP wiring complexity for the unit test).
	ev := Event{EventType: "conversation.message_added", ConversationID: "c1"}
	if !b.matches("u1", ev) {
		t.Fatal("u1 should match c1")
	}
	if b.matches("u2", ev) {
		t.Fatal("u2 not subscribed")
	}
	systemEv := Event{EventType: "agent.state_changed", ConversationID: ""}
	if !b.matches("u1", systemEv) {
		t.Fatal("system-wide event should match every connection")
	}
}

func TestBus_RingBuffer_DropsOldest(t *testing.T) {
	r := newRingBuffer(3)
	for i := 0; i < 5; i++ {
		r.append(Event{EventType: "x"})
	}
	if got := r.len(); got != 3 {
		t.Fatalf("expected 3, got %d", got)
	}
	since := r.since(2)
	if len(since) != 3 {
		t.Fatalf("expected 3 since-id=2, got %d", len(since))
	}
}

func TestBus_RingBuffer_SinceAll(t *testing.T) {
	r := newRingBuffer(5)
	for i := 0; i < 3; i++ {
		r.append(Event{EventType: "x"})
	}
	since := r.since(0)
	if len(since) != 3 {
		t.Fatalf("expected 3, got %d", len(since))
	}
}

func TestServeHTTP_StreamsEventAndHeartbeat(t *testing.T) {
	b := NewBus()
	b.heartbeat = 50 * time.Millisecond
	_ = b.Subscribe("u1", "c1")

	srv := httptest.NewServer(b)
	defer srv.Close()

	// Connect.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	type frame struct {
		body string
		err  error
	}
	ch := make(chan frame, 4)

	req, _ := httptest.NewRequest("GET", srv.URL+"?user_id=u1", nil), 0
	_ = req
	_ = ch
	// Use std net/http client to handle the streaming connect.
	go func() {
		resp, err := httpGetStream(ctx, srv.URL+"?user_id=u1", "")
		if err != nil {
			ch <- frame{err: err}
			return
		}
		defer resp.Close()
		buf := make([]byte, 4096)
		// First read picks up the first heartbeat or event.
		for {
			n, rerr := resp.Read(buf)
			if rerr != nil {
				ch <- frame{err: rerr}
				return
			}
			if n > 0 {
				ch <- frame{body: string(buf[:n])}
				return
			}
		}
	}()

	// Wait for the subscriber to register.
	deadline := time.Now().Add(2 * time.Second)
	for b.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if b.SubscriberCount() == 0 {
		t.Fatal("subscriber didn't register")
	}

	// Publish an event for c1.
	b.Publish(Event{EventType: "conversation.message_added", ConversationID: "c1"})

	select {
	case f := <-ch:
		if f.err != nil {
			t.Fatalf("read: %v", f.err)
		}
		if !strings.Contains(f.body, "conversation.message_added") &&
			!strings.Contains(f.body, ": ping") {
			t.Fatalf("unexpected body: %q", f.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}

func TestServeHTTP_RequiresUserID(t *testing.T) {
	b := NewBus()
	srv := httptest.NewServer(b)
	defer srv.Close()
	resp, err := httpGet(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 400 {
		t.Fatalf("got %d want 400", resp.StatusCode)
	}
}

func TestBus_Shutdown_ClosesSubscribers(t *testing.T) {
	b := NewBus()
	b.heartbeat = 50 * time.Millisecond
	srv := httptest.NewServer(b)
	defer srv.Close()
	_ = b.Subscribe("u1", "c1")
	go func() {
		_, _ = httpGetStream(context.Background(), srv.URL+"?user_id=u1", "")
	}()
	deadline := time.Now().Add(2 * time.Second)
	for b.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if b.SubscriberCount() == 0 {
		t.Fatal("subscriber missing")
	}
	if err := b.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if b.SubscriberCount() != 0 {
		t.Fatalf("expected 0, got %d", b.SubscriberCount())
	}
}
