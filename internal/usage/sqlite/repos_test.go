package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/usage"
)

func tm(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func setup(t *testing.T) (context.Context, *ModelPriceRepo, *UsageEventRepo) {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return context.Background(), NewModelPriceRepo(d), NewUsageEventRepo(d)
}

// TestModelPriceSeed verifies migration 0077 seeded the four list prices and that
// the seeded Opus 4.8 numbers are exactly the expected micros.
func TestModelPriceSeed(t *testing.T) {
	ctx, prices, _ := setup(t)
	list, err := prices.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 4 {
		t.Fatalf("seed count = %d, want 4", len(list))
	}
	book := usage.NewPriceBook(list)
	opus, err := book.PriceAt("claude-opus-4-8", tm("2026-06-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if opus.InputPerMTokMicros != 5_000_000 || opus.OutputPerMTokMicros != 25_000_000 ||
		opus.CacheReadPerMTokMicros != 500_000 || opus.CacheWritePerMTokMicros != 6_250_000 {
		t.Fatalf("seeded opus price wrong: %+v", opus)
	}
	// fable is the priciest seeded model.
	fable, err := book.PriceAt("claude-fable-5", tm("2026-06-01T00:00:00Z"))
	if err != nil || fable.InputPerMTokMicros != 10_000_000 || fable.OutputPerMTokMicros != 50_000_000 {
		t.Fatalf("seeded fable price wrong: %+v, %v", fable, err)
	}
}

func TestModelPriceUpsert(t *testing.T) {
	ctx, prices, _ := setup(t)
	p := usage.ModelPrice{
		Model: "test-model", EffectiveFrom: tm("2026-06-01T00:00:00Z"),
		InputPerMTokMicros: 100, OutputPerMTokMicros: 200,
		CacheReadPerMTokMicros: 10, CacheWritePerMTokMicros: 20,
	}
	if err := prices.Upsert(ctx, p); err != nil {
		t.Fatal(err)
	}
	// Re-assert same key with corrected input price → replace, not duplicate.
	p.InputPerMTokMicros = 999
	if err := prices.Upsert(ctx, p); err != nil {
		t.Fatal(err)
	}
	book, err := prices.LoadPriceBook(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, err := book.PriceAt("test-model", tm("2026-07-01T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if got.InputPerMTokMicros != 999 {
		t.Fatalf("upsert did not replace: input = %d, want 999", got.InputPerMTokMicros)
	}
}

func TestUsageEventRoundTrip(t *testing.T) {
	ctx, _, events := setup(t)

	withTask := usage.UsageEvent{
		ID: "u1", AgentRef: "agent:a", ProjectID: "p1", TaskID: "task-1",
		Model: "claude-opus-4-8", Tokens: usage.TokenCounts{Input: 1000, Output: 500, CacheRead: 2000, CacheWrite: 100},
		CostMicros: 19125, TS: tm("2026-06-01T10:00:00Z"), Source: usage.SourceReport,
	}
	noTask := usage.UsageEvent{
		ID: "u2", AgentRef: "agent:a", ProjectID: "p1", // TaskID empty → NULL
		Model: "claude-sonnet-4-6", Tokens: usage.TokenCounts{Input: 10},
		CostMicros: 30, TS: tm("2026-06-01T11:00:00Z"), Source: usage.SourceTranscript,
	}
	otherAgent := usage.UsageEvent{
		ID: "u3", AgentRef: "agent:b", ProjectID: "p1",
		Model: "claude-haiku-4-5", Tokens: usage.TokenCounts{Input: 5},
		CostMicros: 5, TS: tm("2026-06-01T12:00:00Z"), Source: usage.SourceReport,
	}
	if err := events.Append(ctx, withTask, noTask, otherAgent); err != nil {
		t.Fatal(err)
	}

	// ListByAgent scopes to agent:a, ordered by ts; otherAgent excluded.
	got, err := events.ListByAgent(ctx, "agent:a", tm("2026-06-01T00:00:00Z"), time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "u1" || got[1].ID != "u2" {
		t.Fatalf("ListByAgent(agent:a) = %d events %v", len(got), ids(got))
	}
	// Full round-trip fidelity on the first event, incl. nullable TaskID + tokens.
	g0 := got[0]
	if g0.TaskID != "task-1" || g0.Tokens != withTask.Tokens || g0.CostMicros != 19125 ||
		g0.Source != usage.SourceReport || !g0.TS.Equal(withTask.TS) {
		t.Fatalf("round-trip mismatch: %+v", g0)
	}
	// The no-task event reads back with empty TaskID (NULL).
	if got[1].TaskID != "" {
		t.Fatalf("expected NULL task_id to read back empty, got %q", got[1].TaskID)
	}

	// Range upper bound excludes u2 (ts 11:00) when to=11:00.
	ranged, err := events.ListByAgent(ctx, "agent:a", tm("2026-06-01T00:00:00Z"), tm("2026-06-01T11:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(ranged) != 1 || ranged[0].ID != "u1" {
		t.Fatalf("ranged ListByAgent = %v, want [u1]", ids(ranged))
	}

	// ListByTask scopes to task-1.
	byTask, err := events.ListByTask(ctx, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(byTask) != 1 || byTask[0].ID != "u1" {
		t.Fatalf("ListByTask(task-1) = %v", ids(byTask))
	}
}

func TestUsageEventAppendValidates(t *testing.T) {
	ctx, _, events := setup(t)
	bad := usage.UsageEvent{ID: "x", AgentRef: "agent:a", ProjectID: "p1", Model: "m",
		TS: tm("2026-06-01T00:00:00Z"), Source: "bogus"}
	if err := events.Append(ctx, bad); err == nil {
		t.Fatal("Append accepted an event with invalid source")
	}
}

func ids(es []usage.UsageEvent) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.ID
	}
	return out
}
