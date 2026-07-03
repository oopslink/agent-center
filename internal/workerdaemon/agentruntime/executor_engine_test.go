package agentruntime

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/workerdaemon/executor"
	"github.com/oopslink/agent-center/internal/workerdaemon/orchestrator"
)

func TestFirstNonEmptyLine(t *testing.T) {
	if got := firstNonEmptyLine("\n  \n first line\nsecond"); got != "first line" {
		t.Errorf("got %q, want 'first line'", got)
	}
	if got := firstNonEmptyLine("   \n  "); got != "" {
		t.Errorf("blank got %q, want ''", got)
	}
	long := strings.Repeat("x", 200)
	if got := firstNonEmptyLine(long); len(got) != 120 {
		t.Errorf("long line len = %d, want capped 120", len(got))
	}
}

func TestFuncClock_Now(t *testing.T) {
	fixed := time.Unix(1700000000, 0)
	if got := (funcClock{now: func() time.Time { return fixed }}).Now(); !got.Equal(fixed) {
		t.Errorf("funcClock with fn = %v, want %v", got, fixed)
	}
	if (funcClock{}).Now().IsZero() {
		t.Error("funcClock with nil now should fall back to time.Now")
	}
}

// SnapshotConcurrency must enumerate the live this-process executors AND merge the
// adopted orphans (deduped), so active never under-reports after a restart.
func TestSnapshotConcurrency_LiveAndOrphanMerged(t *testing.T) {
	_, ee, _ := engineForAgent(t, "agent-snap")

	launched, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
		TaskID: "task-1", TaskRef: "task-1", Goal: executor.Goal{Title: "do it"},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	defer func() { _ = launched.Handle.Wait() }()

	ee.addOrphan("executor-orphan", 4242)

	snaps := ee.SnapshotConcurrency()
	if len(snaps) != 2 {
		t.Fatalf("got %d snapshots, want 2 (1 live + 1 orphan): %+v", len(snaps), snaps)
	}
	byID := map[string]concurrency.ExecutorSnapshot{}
	for _, s := range snaps {
		byID[s.ExecutorID] = s
	}
	live, ok := byID[launched.ExecutorID]
	if !ok {
		t.Fatalf("live executor %s missing from snapshot", launched.ExecutorID)
	}
	if live.TaskID != "task-1" {
		t.Errorf("live.TaskID = %q, want task-1", live.TaskID)
	}
	if live.CLI != "claude-code" || live.Model == "" {
		t.Errorf("live cli/model = %q/%q, want claude-code/<non-empty>", live.CLI, live.Model)
	}
	if live.StartedAt.IsZero() {
		t.Error("live.StartedAt should be stamped at spawn")
	}
	if live.State == concurrency.StateOrphan {
		t.Error("live executor must not be state=orphan")
	}
	orphan, ok := byID["executor-orphan"]
	if !ok {
		t.Fatal("orphan executor missing from snapshot")
	}
	if orphan.State != concurrency.StateOrphan || orphan.PID != 4242 {
		t.Errorf("orphan = %+v, want state=orphan pid=4242", orphan)
	}
}

// An orphan that is ALSO a live handle must not be double-counted (dedup by id).
func TestSnapshotConcurrency_DedupsOrphanAlsoLive(t *testing.T) {
	_, ee, _ := engineForAgent(t, "agent-dedup")
	launched, err := ee.engine.HandleWork(context.Background(), orchestrator.WorkItem{
		TaskID: "t", TaskRef: "t", Goal: executor.Goal{Title: "g"},
	})
	if err != nil {
		t.Fatalf("HandleWork: %v", err)
	}
	defer func() { _ = launched.Handle.Wait() }()

	ee.addOrphan(launched.ExecutorID, launched.Handle.PID)
	snaps := ee.SnapshotConcurrency()
	if len(snaps) != 1 {
		t.Fatalf("got %d snapshots, want 1 (orphan deduped against the live handle)", len(snaps))
	}
	if snaps[0].State == concurrency.StateOrphan {
		t.Error("the live handle should win over the orphan entry (not state=orphan)")
	}
}
