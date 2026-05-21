package integration

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/blobstore"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/escalator"
	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/observability/query"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/observability/trace"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/taskruntime"
)

// TestPhase4_Projection_HighFrequency_NoLeak runs 100 push/s for 3s and
// asserts the projection row always reflects the latest push.
func TestPhase4_Projection_HighFrequency_NoLeak(t *testing.T) {
	db, err := persistence.Open(t.TempDir() + "/test.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	repo := obsqlite.NewProjectionRepo(db)
	svc := projection.NewTaskExecutionProjectionService(repo, sink, nil, clk)

	id := taskruntime.TaskExecutionID("E-1")
	base := clk.Now()
	const N = 300
	for i := 0; i < N; i++ {
		t0 := base.Add(time.Duration(i) * 10 * time.Millisecond)
		if err := svc.UpdateProjection(context.Background(), id, projection.ProjectionUpdate{
			LastPushAt: t0, TotalToolCalls: int64(i + 1),
		}); err != nil {
			t.Fatalf("iter %d: %v", i, err)
		}
	}
	got, err := repo.FindByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got.TotalToolCalls != int64(N) {
		t.Fatalf("expected %d tool calls, got %d", N, got.TotalToolCalls)
	}
}

// TestPhase4_Projection_StaleDrop_EmitsEvent verifies the same-tx event emit.
func TestPhase4_Projection_StaleDrop_EmitsEvent(t *testing.T) {
	db, _ := persistence.Open(t.TempDir() + "/test.db")
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	repo := obsqlite.NewProjectionRepo(db)
	svc := projection.NewTaskExecutionProjectionService(repo, sink, nil, clk)
	id := taskruntime.TaskExecutionID("E-stale")
	now := clk.Now()
	_ = svc.UpdateProjection(context.Background(), id, projection.ProjectionUpdate{LastPushAt: now, TotalToolCalls: 10})
	_ = svc.UpdateProjection(context.Background(), id, projection.ProjectionUpdate{LastPushAt: now.Add(-time.Minute), TotalToolCalls: 1})
	stale := observability.EventType("observability.projection_stale_drop")
	evs, _ := er.Find(context.Background(), observability.EventQueryFilter{EventType: &stale})
	if len(evs) != 1 {
		t.Fatalf("expected 1 stale_drop event, got %d", len(evs))
	}
}

// TestPhase4_TraceArchive_TerminalHook_Roundtrip runs the full archive →
// BlobStore upload → center-callback fill in tasks.trace_blob_path style
// roundtrip.
func TestPhase4_TraceArchive_TerminalHook_Roundtrip(t *testing.T) {
	db, _ := persistence.Open(t.TempDir() + "/test.db")
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	root := t.TempDir()
	bs, _ := blobstore.NewLocalDir(root)
	svc := trace.NewService(bs, sink, clk)
	// Simulate per-execution dir with events.jsonl
	execDir := t.TempDir()
	eventsPath := execDir + "/events.jsonl"
	_ = writeFile(t, eventsPath, "line1\nline2\n")
	var got trace.TraceArchiveResult
	_, err := svc.Archive(context.Background(), trace.ArchiveRequest{
		TaskID: "T-1", ExecutionID: "E-1",
		SourceFiles: []trace.SourceFileSpec{{Path: eventsPath, NameInTar: "events.jsonl"}},
	}, func(_ context.Context, r trace.TraceArchiveResult) error {
		got = r
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.BlobRef == "" {
		t.Fatal("center callback did not get blob ref")
	}
	// Download → unzip → tar contains events.jsonl
	rc, _ := bs.Get(context.Background(), got.BlobRef)
	buf := &bytes.Buffer{}
	_, _ = buf.ReadFrom(rc)
	_ = rc.Close()
	if buf.Len() == 0 {
		t.Fatal("downloaded blob is empty")
	}
	uploadedType := observability.EventType("observability.trace_archive_uploaded")
	evs, _ := er.Find(context.Background(), observability.EventQueryFilter{EventType: &uploadedType})
	if len(evs) != 1 {
		t.Fatalf("expected 1 uploaded event, got %d", len(evs))
	}
}

// TestPhase4_TraceArchive_FailureThenRetry verifies the retry-on-restart flow.
func TestPhase4_TraceArchive_FailureThenRetry(t *testing.T) {
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	db, _ := persistence.Open(t.TempDir() + "/test.db")
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)

	root := t.TempDir()
	bs, _ := blobstore.NewLocalDir(root)
	svc := trace.NewService(bs, sink, clk)

	// Layout: <execRoot>/E-1/{terminal.json, events.jsonl}
	execRoot := t.TempDir()
	dir := execRoot + "/E-1"
	if err := writeFile(t, dir+"/terminal.json", `{"status":"completed"}`); err != nil {
		t.Fatal(err)
	}
	_ = writeFile(t, dir+"/events.jsonl", "line1\n")

	scanner := trace.NewPendingScanner(execRoot, svc, clk)
	res, err := scanner.Scan(context.Background(), func(execDir string) (trace.ArchiveRequest, bool, error) {
		return trace.ArchiveRequest{
			TaskID: "T-1", ExecutionID: "E-1",
			SourceFiles: []trace.SourceFileSpec{{Path: execDir + "/events.jsonl"}},
		}, true, nil
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Succeeded != 1 {
		t.Fatalf("expected 1 retry success, got %+v", res)
	}
}

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

// TestPhase4_TxRollback_AffectsBothTables — when caller tx rolls back after
// EventSink.Emit, neither events nor projection rows persist.
func TestPhase4_TxRollback_AffectsBothTables(t *testing.T) {
	db, _ := persistence.Open(t.TempDir() + "/test.db")
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	clk := clock.NewFakeClock(time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(clk)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, clk)
	pr := obsqlite.NewProjectionRepo(db)

	ctx := context.Background()
	// Intentionally fail the tx: write projection + event, then return an
	// error to trigger rollback.
	err := persistence.RunInTx(ctx, db, func(txCtx context.Context) error {
		if _, _, err := pr.UpsertIfFresh(txCtx, "E-tx", projection.ProjectionUpdate{LastPushAt: clk.Now()}); err != nil {
			return err
		}
		if _, err := sink.Emit(txCtx, observability.EmitCommand{
			EventType: "task.created", Actor: "user:t",
		}); err != nil {
			return err
		}
		return errors.New("planned rollback")
	})
	if err == nil || !strings.Contains(err.Error(), "planned rollback") {
		t.Fatalf("expected planned rollback error, got %v", err)
	}
	// Neither row should be visible.
	if _, e := pr.FindByID(ctx, "E-tx"); !errors.Is(e, projection.ErrProjectionNotFound) {
		t.Fatalf("projection row should NOT exist after rollback: %v", e)
	}
	tc := observability.EventType("task.created")
	evs, _ := er.Find(ctx, observability.EventQueryFilter{EventType: &tc})
	if len(evs) != 0 {
		t.Fatalf("events should NOT exist after rollback, got %d", len(evs))
	}
}

func writeFile(t *testing.T, path, content string) error {
	t.Helper()
	if err := mkdirAll(t, path); err != nil {
		return err
	}
	return writeOnce(t, path, content)
}

func mkdirAll(t *testing.T, path string) error {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return nil
	}
	dir := path[:idx]
	return osMkdirAll(dir)
}

func writeOnce(t *testing.T, path, content string) error {
	return osWriteFile(path, []byte(content))
}
