package agentadapter

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newSink(t *testing.T) (*observability.EventSink, *obsqlite.EventRepo) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 21, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	return observability.NewEventSink(er, er, gen, clk), er
}

func mkUnknown(cliType string) AgentTraceEvent {
	return AgentTraceEvent{
		Type:    EventUnknown,
		CliType: cliType,
		Raw:     json.RawMessage(`{"type":"` + cliType + `"}`),
	}
}

func TestReporter_DedupePerExecution(t *testing.T) {
	sink, er := newSink(t)
	r := NewUnknownEventReporter(ReporterConfig{WarningThresholdPerExecution: 100})
	if err := r.Report(context.Background(), sink, "claude-code", "E-1", mkUnknown("future_thing")); err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background(), sink, "claude-code", "E-1", mkUnknown("future_thing")); err != nil {
		t.Fatal(err)
	}
	events, _ := er.Find(context.Background(), observability.EventQueryFilter{Limit: 100})
	count := 0
	for _, e := range events {
		if e.Type() == "agent_adapter.unknown_event_seen" {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("dedupe: expected 1 unknown_event_seen, got %d", count)
	}
}

func TestReporter_DifferentCliTypesEmitSeparately(t *testing.T) {
	sink, er := newSink(t)
	r := NewUnknownEventReporter(ReporterConfig{WarningThresholdPerExecution: 100})
	if err := r.Report(context.Background(), sink, "claude-code", "E-1", mkUnknown("a")); err != nil {
		t.Fatal(err)
	}
	if err := r.Report(context.Background(), sink, "claude-code", "E-1", mkUnknown("b")); err != nil {
		t.Fatal(err)
	}
	events, _ := er.Find(context.Background(), observability.EventQueryFilter{Limit: 100})
	count := 0
	for _, e := range events {
		if e.Type() == "agent_adapter.unknown_event_seen" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("expected 2 events, got %d", count)
	}
}

func TestReporter_ThresholdEmitsWarning(t *testing.T) {
	sink, er := newSink(t)
	r := NewUnknownEventReporter(ReporterConfig{WarningThresholdPerExecution: 2})
	for i, ct := range []string{"a", "b", "c", "d"} {
		_ = i
		if err := r.Report(context.Background(), sink, "claude-code", "E-1", mkUnknown(ct)); err != nil {
			t.Fatal(err)
		}
	}
	events, _ := er.Find(context.Background(), observability.EventQueryFilter{Limit: 100})
	warnings := 0
	for _, e := range events {
		if e.Type() == "task_execution.warning" {
			warnings++
		}
	}
	if warnings != 1 {
		t.Fatalf("warnings: expected 1, got %d", warnings)
	}
}

func TestReporter_ParseFailureThreshold(t *testing.T) {
	r := NewUnknownEventReporter(ReporterConfig{ParseFailFailureThreshold: 3})
	if r.ReportParseFailure("E-1") {
		t.Fatal("first call should not fail")
	}
	r.ReportParseFailure("E-1")
	if !r.ReportParseFailure("E-1") {
		t.Fatal("third call should hit threshold")
	}
}

func TestReporter_Reset(t *testing.T) {
	sink, _ := newSink(t)
	r := NewUnknownEventReporter(ReporterConfig{WarningThresholdPerExecution: 1})
	_ = r.Report(context.Background(), sink, "claude-code", "E-1", mkUnknown("a"))
	r.Reset()
	if len(r.seen) != 0 {
		t.Fatal("expected reset")
	}
}

func TestDefaultReporterConfig(t *testing.T) {
	cfg := DefaultReporterConfig()
	if cfg.WarningThresholdPerExecution == 0 || cfg.ParseFailFailureThreshold == 0 {
		t.Fatal("defaults not set")
	}
}
