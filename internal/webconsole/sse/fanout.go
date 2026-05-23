package sse

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
)

// EventFanout polls the EventRepository for new events and publishes
// them onto a Bus. Polling avoids the rollback-vs-publish race that an
// in-tx decorator hits: we only ever see events that committed.
//
// Default interval 250ms — low enough that UI feels real-time, high
// enough that 4 polls/sec doesn't strain SQLite.
type EventFanout struct {
	repo     observability.EventRepository
	bus      *Bus
	interval time.Duration
	cursor   observability.EventID
	onError  func(error)
}

// NewEventFanout wires the fanout. interval = 0 means use the default.
func NewEventFanout(repo observability.EventRepository, bus *Bus, interval time.Duration) *EventFanout {
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	return &EventFanout{repo: repo, bus: bus, interval: interval, onError: defaultOnError}
}

// WithErrorHandler installs a custom error sink (defaults to log.Printf).
func (f *EventFanout) WithErrorHandler(h func(error)) *EventFanout {
	if h != nil {
		f.onError = h
	}
	return f
}

// Run blocks until ctx is cancelled, polling new events on the
// configured interval and Publishing to the Bus.
//
// Bootstrap behaviour: on start it queries the latest event and stores
// its id as the cursor; the fanout only fires for events created
// after Run starts (we do NOT replay historical events to SSE).
func (f *EventFanout) Run(ctx context.Context) {
	if err := f.bootstrap(ctx); err != nil {
		f.onError(err)
		// Continue with empty cursor — we'll replay every existing
		// event once (acceptable on first start; small DB at center
		// startup).
	}
	ticker := time.NewTicker(f.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			f.tick(ctx)
		}
	}
}

func (f *EventFanout) bootstrap(ctx context.Context) error {
	// Find the most recent event so we don't re-fire history.
	events, err := f.repo.Find(ctx, observability.EventQueryFilter{Limit: 1})
	if err != nil {
		return err
	}
	if len(events) > 0 {
		// Find returns ASC by id; the last one is the most recent.
		f.cursor = events[len(events)-1].ID()
	}
	return nil
}

func (f *EventFanout) tick(ctx context.Context) {
	filter := observability.EventQueryFilter{Limit: 200}
	if f.cursor != "" {
		c := f.cursor
		filter.Cursor = &c
	}
	events, err := f.repo.Find(ctx, filter)
	if err != nil {
		f.onError(err)
		return
	}
	for _, e := range events {
		f.publish(e)
		f.cursor = e.ID()
	}
}

func (f *EventFanout) publish(e *observability.Event) {
	data, _ := json.Marshal(e.Payload())
	f.bus.Publish(Event{
		EventType:      string(e.Type()),
		ConversationID: e.Refs().ConversationID,
		Data:           data,
		OccurredAt:     e.OccurredAt(),
	})
}

func defaultOnError(err error) {
	log.Printf("sse fanout: %v", err)
}
