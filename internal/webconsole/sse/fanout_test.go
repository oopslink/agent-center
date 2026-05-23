package sse

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func setupFanout(t *testing.T) (*observability.EventSink, *obsqlite.EventRepo, *Bus, *EventFanout) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, fc)
	bus := NewBus()
	bus.heartbeat = 50 * time.Millisecond
	fanout := NewEventFanout(er, bus, 25*time.Millisecond)
	return sink, er, bus, fanout
}

func TestEventFanout_PublishesNewEvents(t *testing.T) {
	sink, _, bus, fanout := setupFanout(t)
	// Seed user subscription so Publish actually delivers.
	_ = bus.Subscribe("u1", "C-1")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go fanout.Run(ctx)
	time.Sleep(60 * time.Millisecond) // allow bootstrap

	// Emit an event after fanout is running.
	_, err := sink.Emit(context.Background(), observability.EmitCommand{
		EventType: "conversation.message_added",
		Refs:      observability.EventRefs{ConversationID: "C-1"},
		Actor:     observability.Actor("user:hayang"),
		Payload:   map[string]any{"x": 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Wait for the tick to fire and publish.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if bus.ring.len() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if bus.ring.len() == 0 {
		t.Fatal("fanout did not publish to bus")
	}
}

func TestEventFanout_SkipsHistoryOnBootstrap(t *testing.T) {
	sink, _, bus, fanout := setupFanout(t)
	// Emit BEFORE fanout starts — should be skipped.
	_, _ = sink.Emit(context.Background(), observability.EmitCommand{
		EventType: "system.bootstrap",
		Actor:     observability.Actor("system"),
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go fanout.Run(ctx)
	time.Sleep(120 * time.Millisecond)
	if bus.ring.len() != 0 {
		t.Fatalf("expected no published events, got %d", bus.ring.len())
	}
	// New event after bootstrap → should be published.
	_, _ = sink.Emit(context.Background(), observability.EmitCommand{
		EventType: "system.after",
		Actor:     observability.Actor("system"),
	})
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if bus.ring.len() > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if bus.ring.len() == 0 {
		t.Fatal("new event after bootstrap should publish")
	}
}

func TestEventFanout_ErrorHandlerInvoked(t *testing.T) {
	// failingRepo errors on every Find — fanout's bootstrap then tick
	// should both invoke onError.
	bus := NewBus()
	repo := &failingRepo{}
	var got atomic.Int32
	f := NewEventFanout(repo, bus, 25*time.Millisecond).WithErrorHandler(func(err error) {
		got.Add(1)
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go f.Run(ctx)
	time.Sleep(120 * time.Millisecond)
	if got.Load() == 0 {
		t.Fatal("expected error handler to fire at least once")
	}
}

func TestNewEventFanout_DefaultInterval(t *testing.T) {
	bus := NewBus()
	f := NewEventFanout(&failingRepo{}, bus, 0)
	if f.interval == 0 {
		t.Fatal("expected default interval")
	}
}

func TestEventFanout_WithErrorHandler_NilNoop(t *testing.T) {
	bus := NewBus()
	f := NewEventFanout(&failingRepo{}, bus, 0)
	original := f.onError
	f.WithErrorHandler(nil)
	if f.onError == nil {
		t.Fatal("nil handler should not overwrite")
	}
	_ = original
}

type failingRepo struct{}

func (failingRepo) Append(ctx context.Context, e *observability.Event) error {
	return errors.New("forced")
}
func (failingRepo) FindByID(ctx context.Context, id observability.EventID) (*observability.Event, error) {
	return nil, observability.ErrEventNotFound
}
func (failingRepo) Find(ctx context.Context, filter observability.EventQueryFilter) ([]*observability.Event, error) {
	return nil, errors.New("find forced")
}
