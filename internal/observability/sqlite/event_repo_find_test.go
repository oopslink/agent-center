package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
)

func seed(t *testing.T, repo *EventRepo, n int, baseTime time.Time, mut func(i int, in *observability.NewEventInput)) []*observability.Event {
	t.Helper()
	out := make([]*observability.Event, n)
	clk := clock.NewFakeClock(baseTime)
	gen := idgen.NewGenerator(clk)
	for i := 0; i < n; i++ {
		clk.Set(baseTime.Add(time.Duration(i) * time.Millisecond))
		in := observability.NewEventInput{
			ID:         observability.EventID(gen.NewULID()),
			OccurredAt: clk.Now(),
			Seq:        repo.NextSeq(),
			EventType:  observability.EventType("task.created"),
			Actor:      observability.Actor("user:hayang"),
			Payload:    map[string]any{},
		}
		mut(i, &in)
		e, err := observability.NewEvent(in)
		if err != nil {
			t.Fatal(err)
		}
		if err := repo.Append(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		out[i] = e
	}
	return out
}

func TestEventRepo_Find_TypePrefixMatch(t *testing.T) {
	db := openTestDB(t)
	repo := newRepo(t, db)
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	seed(t, repo, 1, base, func(i int, in *observability.NewEventInput) { in.EventType = "task.created" })
	seed(t, repo, 1, base.Add(time.Second), func(i int, in *observability.NewEventInput) { in.EventType = "task.dispatched" })
	seed(t, repo, 1, base.Add(2*time.Second), func(i int, in *observability.NewEventInput) { in.EventType = "issue.opened" })
	prefix := "task."
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{EventTypePrefix: &prefix})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events for prefix %q, got %d", prefix, len(got))
	}
}

func TestEventRepo_Find_CursorPagination_NoDupesNoGaps(t *testing.T) {
	db := openTestDB(t)
	repo := newRepo(t, db)
	const total = 1000
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	seed(t, repo, total, base, func(i int, in *observability.NewEventInput) {})
	var cursor *observability.EventID
	seen := map[string]bool{}
	pages := 0
	for {
		pages++
		f := observability.EventQueryFilter{Limit: 100}
		if cursor != nil {
			f.Cursor = cursor
		}
		got, err := repo.Find(context.Background(), f)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) == 0 {
			break
		}
		for _, e := range got {
			if seen[string(e.ID())] {
				t.Fatalf("duplicate id %s on page %d", e.ID(), pages)
			}
			seen[string(e.ID())] = true
		}
		last := got[len(got)-1].ID()
		cursor = &last
		if len(got) < 100 {
			break
		}
		if pages > 20 {
			t.Fatal("too many pages")
		}
	}
	if len(seen) != total {
		t.Fatalf("expected %d events, saw %d", total, len(seen))
	}
}

func TestEventRepo_Find_SinceUntilWindow(t *testing.T) {
	db := openTestDB(t)
	repo := newRepo(t, db)
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	seed(t, repo, 5, base, func(i int, in *observability.NewEventInput) {})
	since := base.Add(2 * time.Millisecond)
	until := base.Add(4 * time.Millisecond)
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{Since: &since, Until: &until})
	if err != nil {
		t.Fatal(err)
	}
	// since inclusive (i=2,3), until exclusive (excludes i=4) → 2 events
	if len(got) != 2 {
		t.Fatalf("expected 2, got %d", len(got))
	}
}

func TestEventRepo_Find_ActorFilter(t *testing.T) {
	db := openTestDB(t)
	repo := newRepo(t, db)
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	seed(t, repo, 1, base, func(i int, in *observability.NewEventInput) { in.Actor = "supervisor:I-1" })
	seed(t, repo, 1, base.Add(time.Second), func(i int, in *observability.NewEventInput) { in.Actor = "user:hayang" })
	want := "supervisor:I-1"
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{Actor: &want})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Actor() != observability.Actor(want) {
		t.Fatalf("actor filter mismatch: %+v", got)
	}
}

func TestEventRepo_Find_LimitTooLarge(t *testing.T) {
	db := openTestDB(t)
	repo := newRepo(t, db)
	_, err := repo.Find(context.Background(), observability.EventQueryFilter{Limit: observability.MaxEventQueryLimit + 1})
	if !errors.Is(err, observability.ErrEventQueryLimitTooLarge) {
		t.Fatalf("expected ErrEventQueryLimitTooLarge, got %v", err)
	}
}

func TestEventRepo_Find_RefsConjunction(t *testing.T) {
	db := openTestDB(t)
	repo := newRepo(t, db)
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	seed(t, repo, 1, base, func(i int, in *observability.NewEventInput) {
		in.Refs = observability.EventRefs{TaskID: "T-42", WorkerID: "W-1"}
	})
	seed(t, repo, 1, base.Add(time.Second), func(i int, in *observability.NewEventInput) {
		in.Refs = observability.EventRefs{TaskID: "T-42", WorkerID: "W-2"}
	})
	seed(t, repo, 1, base.Add(2*time.Second), func(i int, in *observability.NewEventInput) {
		in.Refs = observability.EventRefs{TaskID: "T-99"}
	})
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{Refs: observability.EventRefsFilter{TaskID: "T-42", WorkerID: "W-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if got[0].Refs().WorkerID != "W-1" {
		t.Fatal("worker_id mismatch")
	}
}

func TestEventRepo_Find_DecisionAndCorrelation(t *testing.T) {
	db := openTestDB(t)
	repo := newRepo(t, db)
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	seed(t, repo, 1, base, func(i int, in *observability.NewEventInput) {
		in.DecisionID = "D-1"
		in.CorrelationID = "C-1"
	})
	seed(t, repo, 1, base.Add(time.Second), func(i int, in *observability.NewEventInput) {})
	dID := "D-1"
	cID := "C-1"
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{DecisionID: &dID})
	if err != nil || len(got) != 1 {
		t.Fatalf("DecisionID filter: len=%d err=%v", len(got), err)
	}
	got, err = repo.Find(context.Background(), observability.EventQueryFilter{CorrelationID: &cID})
	if err != nil || len(got) != 1 {
		t.Fatalf("CorrelationID filter: len=%d err=%v", len(got), err)
	}
}

func TestEventRepo_Find_DefaultLimit_Applies(t *testing.T) {
	db := openTestDB(t)
	repo := newRepo(t, db)
	base := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	seed(t, repo, 150, base, func(i int, in *observability.NewEventInput) {})
	got, err := repo.Find(context.Background(), observability.EventQueryFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != observability.DefaultEventQueryLimit {
		t.Fatalf("default limit not applied: got %d", len(got))
	}
}
