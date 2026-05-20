package observability

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
)

// fakeRepo is an in-memory EventRepository used by sink unit tests.
type fakeRepo struct {
	mu       sync.Mutex
	events   []*Event
	failNext error
}

func (r *fakeRepo) Append(ctx context.Context, e *Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failNext != nil {
		err := r.failNext
		r.failNext = nil
		return err
	}
	r.events = append(r.events, e)
	return nil
}

func (r *fakeRepo) FindByID(ctx context.Context, id EventID) (*Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.events {
		if e.ID() == id {
			return e, nil
		}
	}
	return nil, ErrEventNotFound
}

func (r *fakeRepo) Find(ctx context.Context, _ EventQueryFilter) ([]*Event, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*Event, len(r.events))
	copy(out, r.events)
	return out, nil
}

type fakeSeq struct{ n atomic.Int64 }

func (s *fakeSeq) NextSeq() int64 { return s.n.Add(1) }

func newSink() (*EventSink, *fakeRepo, *clock.FakeClock) {
	repo := &fakeRepo{}
	seq := &fakeSeq{}
	fc := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	return NewEventSink(repo, seq, idgen.NewGenerator(fc), fc), repo, fc
}

func TestEventSink_Emit_Happy(t *testing.T) {
	sink, repo, _ := newSink()
	id, err := sink.Emit(context.Background(), EmitCommand{
		EventType: "x.y",
		Actor:     "user:hayang",
		Refs:      EventRefs{WorkerID: "W-1"},
		Payload:   map[string]any{"a": 1},
	})
	if err != nil {
		t.Fatalf("Emit: %v", err)
	}
	if id == "" {
		t.Fatal("empty id")
	}
	if len(repo.events) != 1 {
		t.Fatalf("expected 1 event in repo, got %d", len(repo.events))
	}
	if repo.events[0].Type() != "x.y" {
		t.Fatal("type mismatch")
	}
}

func TestEventSink_Emit_RejectsEmptyEventType(t *testing.T) {
	sink, _, _ := newSink()
	_, err := sink.Emit(context.Background(), EmitCommand{
		EventType: "",
		Actor:     "user:hayang",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEventSink_Emit_RejectsBadActor(t *testing.T) {
	sink, _, _ := newSink()
	_, err := sink.Emit(context.Background(), EmitCommand{
		EventType: "x.y",
		Actor:     "foo:bar",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEventSink_Emit_OccurredAtDefaultsToClock(t *testing.T) {
	sink, repo, fc := newSink()
	_, err := sink.Emit(context.Background(), EmitCommand{
		EventType: "x.y",
		Actor:     "system",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !repo.events[0].OccurredAt().Equal(fc.Now()) {
		t.Fatalf("occurred_at: got %v want %v", repo.events[0].OccurredAt(), fc.Now())
	}
}

func TestEventSink_Emit_OccurredAtExplicit(t *testing.T) {
	sink, repo, _ := newSink()
	want := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	_, err := sink.Emit(context.Background(), EmitCommand{
		EventType:  "x.y",
		Actor:      "system",
		OccurredAt: want,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !repo.events[0].OccurredAt().Equal(want) {
		t.Fatal("explicit occurred_at not honored")
	}
}

func TestEventSink_Emit_PayloadReasonMessage(t *testing.T) {
	sink, _, _ := newSink()
	_, err := sink.Emit(context.Background(), EmitCommand{
		EventType: "x.y",
		Actor:     "system",
		Payload:   map[string]any{"reason": "worker_lost"},
	})
	if err == nil {
		t.Fatal("expected error for reason without message")
	}
}

func TestEventSink_Emit_SeqMonotonic(t *testing.T) {
	sink, repo, _ := newSink()
	for i := 0; i < 5; i++ {
		_, _ = sink.Emit(context.Background(), EmitCommand{
			EventType: "x.y",
			Actor:     "system",
		})
	}
	for i := 1; i < len(repo.events); i++ {
		if repo.events[i].Seq() <= repo.events[i-1].Seq() {
			t.Fatalf("seq not monotonic at i=%d: %d <= %d", i, repo.events[i].Seq(), repo.events[i-1].Seq())
		}
	}
}

func TestEventSink_Emit_RepoFailure(t *testing.T) {
	sink, repo, _ := newSink()
	repo.failNext = errors.New("boom")
	_, err := sink.Emit(context.Background(), EmitCommand{
		EventType: "x.y",
		Actor:     "system",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestEventSink_Emit_NilReceiver(t *testing.T) {
	var sink *EventSink
	_, err := sink.Emit(context.Background(), EmitCommand{
		EventType: "x.y",
		Actor:     "system",
	})
	if err == nil {
		t.Fatal("expected error for nil receiver")
	}
}

func TestEventSink_Emit_MissingDep(t *testing.T) {
	sink := &EventSink{clock: clock.SystemClock{}}
	_, err := sink.Emit(context.Background(), EmitCommand{
		EventType: "x.y",
		Actor:     "system",
	})
	if err == nil {
		t.Fatal("expected error for missing deps")
	}
}

func TestEventSink_NewWithNilClock(t *testing.T) {
	repo := &fakeRepo{}
	seq := &fakeSeq{}
	s := NewEventSink(repo, seq, idgen.NewGenerator(clock.SystemClock{}), nil)
	if _, err := s.Emit(context.Background(), EmitCommand{
		EventType: "x.y",
		Actor:     "system",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestEventSink_ConcurrentEmit(t *testing.T) {
	sink, repo, _ := newSink()
	var wg sync.WaitGroup
	const N = 50
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = sink.Emit(context.Background(), EmitCommand{
				EventType: "x.y",
				Actor:     "system",
			})
		}()
	}
	wg.Wait()
	if len(repo.events) != N {
		t.Fatalf("expected %d events, got %d", N, len(repo.events))
	}
	seen := map[int64]struct{}{}
	for _, e := range repo.events {
		if _, dup := seen[e.Seq()]; dup {
			t.Fatalf("dup seq %d", e.Seq())
		}
		seen[e.Seq()] = struct{}{}
	}
}
