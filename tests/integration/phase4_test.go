package integration

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/escalator"
	"github.com/oopslink/agent-center/internal/observability/query"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

// TestPhase4_Escalator_DedupAcrossScans verifies the threshold+dedup logic.
func TestPhase4_Escalator_DedupAcrossScans(t *testing.T) {
	db, _ := persistence.Open(t.TempDir() + "/test.db")
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	svc := escalator.NewService(er, sink, clk, escalator.Config{Threshold: 11, Window: 24 * time.Hour})

	// Emit 11 unknown_event_seen events
	for i := 0; i < 11; i++ {
		_, _ = sink.Emit(context.Background(), observability.EmitCommand{
			EventType: escalator.EventTypeUnknownSeen,
			Actor:     "worker:W-1",
			Payload: map[string]any{
				"adapter_name": "claude-code", "cli_type_field": "new_thing",
				"reason": "first_seen", "message": "x",
			},
		})
	}
	res, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Triggered != 1 {
		t.Fatalf("first scan should trigger 1, got %+v", res)
	}
	// Inject 5 more same group; second scan must NOT trigger again (dedup).
	for i := 0; i < 5; i++ {
		_, _ = sink.Emit(context.Background(), observability.EmitCommand{
			EventType: escalator.EventTypeUnknownSeen,
			Actor:     "worker:W-1",
			Payload: map[string]any{
				"adapter_name": "claude-code", "cli_type_field": "new_thing",
				"reason": "first_seen", "message": "x",
			},
		})
	}
	res, err = svc.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Skipped != 1 {
		t.Fatalf("second scan should skip (dedup), got %+v", res)
	}
	escalated := observability.EventType(escalator.EventTypeEscalated)
	evs, _ := er.Find(context.Background(), observability.EventQueryFilter{EventType: &escalated})
	if len(evs) != 1 {
		t.Fatalf("expected exactly 1 escalation, got %d", len(evs))
	}
}

// TestPhase4_Events_CursorPagination_1000Rows pageinates through 1000 rows
// and asserts no dup / no gap.
func TestPhase4_Events_CursorPagination_1000Rows(t *testing.T) {
	db, _ := persistence.Open(t.TempDir() + "/test.db")
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	const total = 1000
	for i := 0; i < total; i++ {
		clk.Advance(time.Millisecond)
		_, _ = sink.Emit(context.Background(), observability.EmitCommand{
			EventType: "task.created", Actor: "user:t",
		})
	}
	deps := query.Deps{Events: er}
	svc := query.NewService(deps)
	seen := map[string]bool{}
	cursor := ""
	for pages := 0; pages < 20; pages++ {
		res, err := svc.Query(context.Background(), "events", query.QueryFilter{Limit: 100, Cursor: cursor})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Items) == 0 {
			break
		}
		for _, it := range res.Items {
			id := it.(map[string]any)["id"].(string)
			if seen[id] {
				t.Fatalf("dup id %s page %d", id, pages)
			}
			seen[id] = true
		}
		if res.NextCursor == "" {
			break
		}
		cursor = res.NextCursor
	}
	if len(seen) != total {
		t.Fatalf("expected %d unique events, got %d", total, len(seen))
	}
}
