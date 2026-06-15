package sse

import (
	"context"
	"net/http"
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
		// v2.10.1 [T104]: the stream now opens with a ~2KB priming COMMENT
		// (`:` line, ignored per the EventSource spec). Keep reading past it —
		// like a real client — until a real `data:` frame (heartbeat or event)
		// arrives; returning on the first read would surface only the padding
		// and disconnect before the test publishes.
		var acc strings.Builder
		for {
			n, rerr := resp.Read(buf)
			if rerr != nil {
				ch <- frame{err: rerr}
				return
			}
			if n > 0 {
				acc.Write(buf[:n])
				if strings.Contains(acc.String(), "data:") {
					ch <- frame{body: acc.String()}
					return
				}
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
		// v2.5.13 (#71): heartbeat is now a real data frame
		// (`sse.heartbeat` event_type, no id line) instead of the
		// `: ping` comment, so the client onmessage fires and the
		// watchdog resets. Either the real event or the heartbeat
		// frame is acceptable here.
		if !strings.Contains(f.body, "conversation.message_added") &&
			!strings.Contains(f.body, "sse.heartbeat") {
			t.Fatalf("unexpected body: %q", f.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for SSE event")
	}
}

// v2.10.1 [T104]: behind a buffering proxy/CDN (Cloudflare, observed) the
// EventSource never fired onopen — the proxy held the response head. The fix
// primes the stream with a ~2KB ignored SSE comment (flushed immediately) +
// `no-transform` so the head is released and the proxy won't compress/buffer.
func TestServeHTTP_T104_PrimesStreamForBufferingProxy(t *testing.T) {
	b := NewBus()
	srv := httptest.NewServer(b)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"?user_id=u1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	// no-transform tells Cloudflare et al. NOT to compress/transform the stream
	// (compression buffers SSE); X-Accel-Buffering covers nginx.
	if cc := resp.Header.Get("Cache-Control"); !strings.Contains(cc, "no-transform") {
		t.Errorf("Cache-Control = %q, want it to contain no-transform", cc)
	}
	if got := resp.Header.Get("X-Accel-Buffering"); got != "no" {
		t.Errorf("X-Accel-Buffering = %q, want no", got)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	// The stream opens with the ~2KB priming COMMENT (a ':' line) BEFORE any data
	// frame, so a buffering proxy releases the head immediately.
	buf := make([]byte, 4096)
	n, rerr := resp.Body.Read(buf)
	if rerr != nil {
		t.Fatalf("read: %v", rerr)
	}
	first := string(buf[:n])
	if !strings.HasPrefix(first, ":") {
		t.Errorf("first chunk must start with the priming comment ':' (got %d bytes, prefix %q)", n, safePrefix(first, 40))
	}
	if strings.Contains(first, "data:") {
		t.Errorf("priming comment must precede any data frame (got prefix %q)", safePrefix(first, 80))
	}
	if n < 2000 {
		t.Errorf("priming padding too small (%d bytes), want ~2KB", n)
	}
}

func safePrefix(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
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

func TestServeHTTP_LastEventID_QueryFallback(t *testing.T) {
	// Manual reconnect path: EventSource constructor cannot set headers,
	// so the frontend passes Last-Event-ID via `?last_event_id=N`. Verify
	// the bus replays from the ringbuffer using the query value.
	b := NewBus()
	b.heartbeat = 50 * time.Millisecond
	_ = b.Subscribe("u1", "c1")
	// Pre-seed two events into the ringbuffer (IDs 1, 2).
	b.Publish(Event{EventType: "first", ConversationID: "c1"})
	b.Publish(Event{EventType: "second", ConversationID: "c1"})

	srv := httptest.NewServer(b)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, err := httpGetStream(ctx, srv.URL+"?user_id=u1&last_event_id=1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Close()
	buf := make([]byte, 4096)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		n, rerr := resp.Read(buf)
		if rerr != nil {
			break
		}
		body := string(buf[:n])
		// v2.4-D-X1 fix: SSE wire no longer emits an `event:` line
		// (typed events were silently dropped on real browsers). The
		// event_type lives inside the JSON payload now.
		if strings.Contains(body, `"event_type":"second"`) {
			return // good — replayed the second event (id > 1)
		}
		if strings.Contains(body, `"event_type":"first"`) {
			t.Fatalf("query-param Last-Event-ID should have skipped id=1; got %s", body)
		}
	}
	t.Fatal("did not see replayed event within deadline")
}

// v2.5.13 (#71): the heartbeat frame must be a real data message
// (so EventSource fires onmessage on the client) and must NOT carry
// an `id:` field (so lastEventId stays anchored to the last real
// event in the ringbuffer). Asserts the on-wire shape.
func TestServeHTTP_HeartbeatIsRealDataMessageWithoutID(t *testing.T) {
	b := NewBus()
	b.heartbeat = 30 * time.Millisecond
	_ = b.Subscribe("u1", "c1")

	srv := httptest.NewServer(b)
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	resp, err := httpGetStream(ctx, srv.URL+"?user_id=u1", "")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Close()

	buf := make([]byte, 4096)
	deadline := time.Now().Add(2 * time.Second)
	var seen string
	for time.Now().Before(deadline) {
		n, rerr := resp.Read(buf)
		if rerr != nil {
			break
		}
		seen += string(buf[:n])
		if strings.Contains(seen, "sse.heartbeat") {
			break
		}
	}
	if !strings.Contains(seen, `data: {"event_type":"sse.heartbeat"}`) {
		t.Fatalf("expected heartbeat data frame, got: %q", seen)
	}
	// The heartbeat frame must not carry an `id:` line — the bus
	// reserves IDs for ringbuffer-replayable events.
	for _, line := range strings.Split(seen, "\n\n") {
		if !strings.Contains(line, "sse.heartbeat") {
			continue
		}
		if strings.Contains(line, "id:") {
			t.Fatalf("heartbeat frame must not carry id: line, got: %q", line)
		}
	}
}

// v2.7 #172 (acceptance FINDING-A): a fresh SSE connection (no
// Last-Event-ID to replay) must flush the response head immediately on
// connect, so the browser EventSource fires onopen — and the UI flips
// "connecting"→"live" — right away instead of waiting for the first
// heartbeat. Regression: the only flush on connect lived in the replay
// branch, so a bare connect didn't send status+headers until the first
// (~30s) heartbeat, surfacing as a persistent "connecting" on every page
// load (root cause behind the original #153 report on a healthy center).
//
// Test mechanism: with a deliberately long heartbeat, http Client.Do
// returns only once the response head is received. Without the fix Do
// blocks until the ctx deadline; with it, the head arrives in well under
// one heartbeat.
func TestServeHTTP_FlushesHeadImmediatelyOnConnect(t *testing.T) {
	b := NewBus()
	b.heartbeat = 30 * time.Second // long on purpose: the head must not depend on it

	srv := httptest.NewServer(b)
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL+"?user_id=u1", nil)
	if err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	resp, err := http.DefaultClient.Do(req)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("connect head should arrive promptly, got error after %v: %v", elapsed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want status 200, got %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("want Content-Type text/event-stream, got %q", ct)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("response head took %v (≈ a heartbeat) — not flushed on connect (regression)", elapsed)
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
