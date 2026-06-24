package escalator_test

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/escalator"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func setupEnv(t *testing.T) (*escalator.Service, *obsqlite.EventRepo, *observability.EventSink, *clock.FakeClock) {
	t.Helper()
	path := t.TempDir() + "/test.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	svc := escalator.NewService(er, sink, clk, escalator.Config{Threshold: 3, Window: time.Hour})
	return svc, er, sink, clk
}

func seedUnknown(t *testing.T, sink *observability.EventSink, n int, adapter, cliType string) {
	t.Helper()
	for i := 0; i < n; i++ {
		_, err := sink.Emit(context.Background(), observability.EmitCommand{
			EventType: escalator.EventTypeUnknownSeen,
			Actor:     observability.Actor("worker:W-1"),
			Payload: map[string]any{
				"adapter_name":   adapter,
				"cli_type_field": cliType,
				"sample_raw":     `{"type":"new_thing"}`,
				"reason":         "first_seen",
				"message":        "encountered an unknown event type",
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}

func countEscalated(t *testing.T, er *obsqlite.EventRepo) int {
	t.Helper()
	et := escalator.EventTypeEscalated
	evs, err := er.Find(context.Background(), observability.EventQueryFilter{EventType: &et})
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

func TestEscalator_BelowThreshold_NoEmit(t *testing.T) {
	svc, er, sink, _ := setupEnv(t)
	seedUnknown(t, sink, 2, "claude-code", "new_thing")
	if _, err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := countEscalated(t, er); c != 0 {
		t.Fatalf("below threshold should not emit; got %d", c)
	}
}

func TestEscalator_ThresholdTriggered(t *testing.T) {
	svc, er, sink, _ := setupEnv(t)
	seedUnknown(t, sink, 3, "claude-code", "new_thing")
	res, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Triggered != 1 {
		t.Fatalf("Triggered: %d", res.Triggered)
	}
	if c := countEscalated(t, er); c != 1 {
		t.Fatalf("expected 1 escalated event, got %d", c)
	}
}

func TestEscalator_Dedup_24hWindow(t *testing.T) {
	svc, er, sink, _ := setupEnv(t)
	seedUnknown(t, sink, 5, "codex", "weird")
	if _, err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	seedUnknown(t, sink, 5, "codex", "weird")
	if _, err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := countEscalated(t, er); c != 1 {
		t.Fatalf("dedup failed; got %d escalated events", c)
	}
}

func TestEscalator_Dedup_Resets_AfterDay(t *testing.T) {
	svc, er, sink, clk := setupEnv(t)
	seedUnknown(t, sink, 5, "codex", "weird")
	if _, err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk.Advance(25 * time.Hour)
	seedUnknown(t, sink, 5, "codex", "weird")
	if _, err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	// After 24h dedup expires, escalation fires again.
	if c := countEscalated(t, er); c != 2 {
		t.Fatalf("expected 2 escalated events post-24h, got %d", c)
	}
}

func TestEscalator_MissingPayloadFields_Skipped(t *testing.T) {
	svc, er, sink, _ := setupEnv(t)
	for i := 0; i < 5; i++ {
		_, _ = sink.Emit(context.Background(), observability.EmitCommand{
			EventType: escalator.EventTypeUnknownSeen,
			Actor:     observability.Actor("worker:W-1"),
			Payload:   map[string]any{"reason": "x", "message": "y"}, // no adapter / cli_type
		})
	}
	if _, err := svc.Scan(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c := countEscalated(t, er); c != 0 {
		t.Fatalf("malformed events should not escalate; got %d", c)
	}
}

func TestEscalator_MultipleGroupsSeparately(t *testing.T) {
	svc, er, sink, _ := setupEnv(t)
	seedUnknown(t, sink, 3, "claude-code", "new_a")
	seedUnknown(t, sink, 3, "claude-code", "new_b")
	seedUnknown(t, sink, 1, "codex", "new_c") // below threshold
	res, err := svc.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Triggered != 2 {
		t.Fatalf("expected 2 triggered groups, got %d", res.Triggered)
	}
	if c := countEscalated(t, er); c != 2 {
		t.Fatalf("expected 2 escalation events, got %d", c)
	}
}

func TestEscalator_Defaults_ApplyWhenZero(t *testing.T) {
	cfg := escalator.Config{}.Apply()
	if cfg.Threshold != escalator.DefaultThreshold {
		t.Fatalf("threshold default off: %d", cfg.Threshold)
	}
	if cfg.Window != escalator.DefaultWindow {
		t.Fatalf("window default off: %v", cfg.Window)
	}
	if cfg.Interval != escalator.DefaultInterval {
		t.Fatalf("interval default off: %v", cfg.Interval)
	}
}
