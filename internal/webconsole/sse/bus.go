// Package sse hosts the Web Console SSE backend (P11 § 3.3).
//
// Connection model (per Q5=B):
//   - Single user-level long EventSource per user
//   - subscribe / unsubscribe individual conversation_ids over a side
//     channel (POST /api/sse/{subscribe,unsubscribe})
//   - Heartbeat ping every 30s prevents idle close
//   - Reconnect with Last-Event-ID continues from a small in-memory
//     ringbuffer
//
// EventSink integration: fan-out happens via Publish(); the cli.App
// wires it to an observability.EventSink listener so every emitted
// domain event reaches subscribed clients.
package sse

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ssePrimePadding is a ~2KB SSE COMMENT (a line starting with ':') flushed
// immediately on connect (v2.10.1 [T104]). The W3C event-stream grammar ignores
// comment lines — EventSource never surfaces them — but the bytes force a
// buffering reverse proxy / CDN that does NOT honor `X-Accel-Buffering: no`
// (that header is nginx-only; Caddy/Cloudflare/Traefik ignore it) to flush the
// response HEAD past its buffer threshold. Without it such a proxy holds the
// head until the upstream closes, so the browser EventSource never fires
// `onopen` and the UI flaps connecting↔reconnecting forever (T104, observed on
// a domain behind a proxy while localhost — direct to the server — was fine).
var ssePrimePadding = ":" + strings.Repeat(" ", 2048) + "\n\n"

// Event is the on-wire SSE message body. event_type matches the
// observability EventType; conversation_id (optional) lets the bus
// route to subscribers.
type Event struct {
	ID             int64           `json:"id"`
	EventType      string          `json:"event_type"`
	ConversationID string          `json:"conversation_id,omitempty"`
	Data           json.RawMessage `json:"data,omitempty"`
	OccurredAt     time.Time       `json:"occurred_at"`
}

// Bus is the subscriber pool + ringbuffer. v2 single-user case has 1
// connection per user (typically 1-3 tabs at most).
type Bus struct {
	mu        sync.RWMutex
	subs      map[string]*subscriber         // userID → connection
	channels  map[string]map[string]struct{} // userID → set of conversation_ids
	ring      *ringBuffer
	heartbeat time.Duration
}

type subscriber struct {
	userID string
	ch     chan Event
	done   chan struct{}
}

// NewBus returns a fresh Bus. Heartbeat default 30s; ringBuffer default 256.
func NewBus() *Bus {
	return &Bus{
		subs:     make(map[string]*subscriber),
		channels: make(map[string]map[string]struct{}),
		ring:     newRingBuffer(256),
		// v2.10.2 [T135]: 15s (was 30s) so idle gaps stay short behind a
		// domain/CDN/local-proxy chain (Cloudflare keep-idle ~100s, but a local
		// forward proxy may read-timeout much sooner) and a dropped stream is
		// re-detected faster. The client watchdog (45s) now tolerates 3 missed
		// beats instead of 1.5.
		heartbeat: 15 * time.Second,
	}
}

// Subscribe registers a userID's interest in a conversation. The user
// must already have an active SSE connection (or will get events once
// they connect — subscription persists across reconnects).
func (b *Bus) Subscribe(userID, conversationID string) error {
	if userID == "" || conversationID == "" {
		return errors.New("sse: user_id and conversation_id required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.channels[userID] == nil {
		b.channels[userID] = make(map[string]struct{})
	}
	b.channels[userID][conversationID] = struct{}{}
	return nil
}

// Unsubscribe removes a userID's interest. Idempotent.
func (b *Bus) Unsubscribe(userID, conversationID string) error {
	if userID == "" || conversationID == "" {
		return errors.New("sse: user_id and conversation_id required")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if set, ok := b.channels[userID]; ok {
		delete(set, conversationID)
	}
	return nil
}

// IsSubscribed reports membership (used by tests + fan-out).
func (b *Bus) IsSubscribed(userID, conversationID string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	set, ok := b.channels[userID]
	if !ok {
		return false
	}
	_, present := set[conversationID]
	return present
}

// Publish fans out an event to every subscriber interested in its
// conversation_id. If conversation_id is empty, fans out to every
// connected subscriber (system-wide notification — e.g. agent state
// change). The event is also appended to the ringbuffer for reconnect
// replay.
func (b *Bus) Publish(ev Event) {
	if ev.OccurredAt.IsZero() {
		ev.OccurredAt = time.Now().UTC()
	}
	ev.ID = b.ring.append(ev)
	b.mu.RLock()
	defer b.mu.RUnlock()
	for userID, sub := range b.subs {
		if ev.ConversationID != "" {
			set := b.channels[userID]
			if _, ok := set[ev.ConversationID]; !ok {
				continue
			}
		}
		select {
		case sub.ch <- ev:
		case <-sub.done:
		default:
			// Channel full; drop. Reconnect with Last-Event-ID will
			// catch up via the ringbuffer.
		}
	}
}

// ServeHTTP implements the EventSource endpoint. Each user gets ONE
// connection at a time (a new connect replaces the old one).
func (b *Bus) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		userID = r.Header.Get("X-User-Id")
	}
	if userID == "" {
		http.Error(w, "user_id required (query param or X-User-Id header)", http.StatusBadRequest)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	// no-transform (v2.10.1 [T104]): tell intermediaries (Cloudflare et al.) NOT
	// to compress/transform the stream — proxy compression buffers SSE. Pairs with
	// the nginx-only X-Accel-Buffering below + the ssePrimePadding flush.
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering (nginx-only header)

	// v2.7 #172 (acceptance FINDING-A): flush the response head immediately
	// on connect. Go's net/http only sends status+headers on the first
	// Write/Flush; before this, a fresh connection (no Last-Event-ID to
	// replay below) had no flush until the first ~30s heartbeat, so the
	// browser EventSource onopen — and the UI "connecting"→"live" flip —
	// was delayed a full heartbeat on every page load. (Root cause behind
	// the original #153 "connecting" report, which still reproduced on a
	// healthy center.)
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	// v2.10.1 [T104]: prime the stream with a ~2KB ignored SSE comment so a
	// buffering proxy/CDN (Cloudflare, observed) releases the response head
	// immediately → EventSource fires onopen instead of flapping connecting↔
	// reconnecting. EventSource ignores comment lines, so the client sees nothing.
	fmt.Fprint(w, ssePrimePadding)
	flusher.Flush()

	// v2.10.2 [T135]: hint the EventSource native reconnect backoff (a `retry:`
	// line) so a transient drop behind the domain/CDN/proxy chain reconnects on a
	// sane fixed delay rather than the UA default.
	fmt.Fprint(w, "retry: 3000\n\n")
	flusher.Flush()

	// Replay missed events when Last-Event-ID present. Header is the
	// standard channel (native browser EventSource passes it on auto-
	// reconnect); ?last_event_id=N query param is the fallback used by
	// manual reconnect paths (frontend useSSE hook) since EventSource
	// constructor cannot set custom headers.
	lastEventID := r.Header.Get("Last-Event-ID")
	if lastEventID == "" {
		lastEventID = r.URL.Query().Get("last_event_id")
	}
	if lastEventID != "" {
		if afterID, err := strconv.ParseInt(lastEventID, 10, 64); err == nil {
			for _, ev := range b.ring.since(afterID) {
				if !b.matches(userID, ev) {
					continue
				}
				writeSSE(w, ev)
			}
			flusher.Flush()
		}
	}

	// v2.10.2 [T135]: send an immediate heartbeat data frame right after connect
	// (and any replay) so the client receives a real `onmessage` within
	// milliseconds — its status flips to "open" and its liveness watchdog is
	// primed at once. Without it, a fresh connection with NO missed events to
	// replay stayed silent until the first heartbeat tick (up to one full
	// interval), so every (re)connect behind the domain/proxy chain showed an
	// "connecting" window that read as the flicker T135 reports. No `id:` line →
	// lastEventId is unchanged; the client dispatch table treats it as a no-op.
	fmt.Fprint(w, "data: {\"event_type\":\"sse.heartbeat\"}\n\n")
	flusher.Flush()

	// Install the subscriber (replaces any existing connection for this user).
	sub := &subscriber{
		userID: userID,
		ch:     make(chan Event, 64),
		done:   make(chan struct{}),
	}
	b.mu.Lock()
	if old, ok := b.subs[userID]; ok {
		close(old.done)
	}
	b.subs[userID] = sub
	b.mu.Unlock()
	defer func() {
		b.mu.Lock()
		if b.subs[userID] == sub {
			delete(b.subs, userID)
		}
		b.mu.Unlock()
	}()

	ticker := time.NewTicker(b.heartbeat)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-sub.done:
			return
		case ev := <-sub.ch:
			writeSSE(w, ev)
			flusher.Flush()
		case <-ticker.C:
			// v2.5.13 fix (#71): heartbeat as a real data message so the
			// browser fires onmessage on the client, which is what the
			// frontend watchdog uses to confirm liveness. The previous
			// `: ping` comment line kept the TCP socket warm but did NOT
			// fire onmessage (per W3C EventSource spec — comments are
			// dropped), so the client's 30s no-event watchdog always
			// expired and forced a reconnect every cycle. Sending a real
			// data frame (no `id:` line so lastEventId is unchanged)
			// resets the watchdog and falls into the dispatch table's
			// default no-op branch.
			fmt.Fprint(w, "data: {\"event_type\":\"sse.heartbeat\"}\n\n")
			flusher.Flush()
		}
	}
}

// matches reports whether an event should be delivered to userID.
func (b *Bus) matches(userID string, ev Event) bool {
	if ev.ConversationID == "" {
		return true // system-wide
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	set, ok := b.channels[userID]
	if !ok {
		return false
	}
	_, present := set[ev.ConversationID]
	return present
}

// writeSSE writes a single Event in the W3C text/event-stream format.
//
// v2.4-D-X1 fix B6/B7: emit ONLY `id:` and `data:` — no `event:`
// line. Browsers route typed events (where the `event:` field is set)
// via addEventListener(<type>, ...) instead of onmessage. useSSE only
// listens on onmessage, so adding `event:` silently dropped every
// SSE message on real browsers. The event_type stays inside the JSON
// payload (Event.EventType), which is what dispatchToQueryClient
// already switches on.
func writeSSE(w http.ResponseWriter, ev Event) {
	body, _ := json.Marshal(ev)
	fmt.Fprintf(w, "id: %d\ndata: %s\n\n", ev.ID, body)
}

// SubscriberCount returns the count of active connections (test helper).
func (b *Bus) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subs)
}

// Shutdown closes all subscriber channels. Idempotent.
func (b *Bus) Shutdown(ctx context.Context) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	for userID, sub := range b.subs {
		close(sub.done)
		delete(b.subs, userID)
	}
	return nil
}
