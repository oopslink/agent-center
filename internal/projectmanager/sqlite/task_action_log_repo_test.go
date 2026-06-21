package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// dbSetup opens a fresh in-memory DB migrated to head. Used by the v2.14.0 F2
// repo tests, which need their own repos (the shared setup() predates them).
func dbSetup(t *testing.T) (context.Context, *sql.DB) {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return context.Background(), d
}

// TestTaskActionLogRepo_AppendListRoundTrip covers the v2.14.0 I14 §7.3 action-log
// repo: ULID minting for empty-ID entries, stable (occurred_at,id) order on read,
// per-task isolation, the agent_started interactions count, and empty-input no-op.
func TestTaskActionLogRepo_AppendListRoundTrip(t *testing.T) {
	ctx, d := dbSetup(t)
	repo := NewTaskActionLogRepo(d, idgen.NewGenerator(clock.SystemClock{}))

	// Empty input is a no-op (no error, nothing inserted).
	if err := repo.Append(ctx, "T1", nil); err != nil {
		t.Fatalf("empty Append should be a no-op: %v", err)
	}

	// Append three entries with EMPTY ids (the aggregate never mints) at distinct
	// times, plus one with a caller-supplied id that must be preserved.
	base := time.Date(2026, 6, 22, 1, 0, 0, 0, time.UTC)
	logs := []pm.TaskActionLog{
		{OccurredAt: base, Action: pm.TaskActionAssigned, ActorRef: "user:pd", AgentRef: "agent:c"},
		{OccurredAt: base.Add(time.Second), Action: pm.TaskActionAgentStarted, ActorRef: "agent:c", AgentRef: "agent:c"},
		{ID: "fixed-id-xyz", OccurredAt: base.Add(2 * time.Second), Action: pm.TaskActionBlocked, ActorRef: "agent:c", AgentRef: "agent:c", Note: "[obstacle] needs token"},
		{OccurredAt: base.Add(3 * time.Second), Action: pm.TaskActionAgentStarted, ActorRef: "agent:c", AgentRef: "agent:c"},
	}
	if err := repo.Append(ctx, "T1", logs); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// A different task's entry must not bleed into T1's list.
	if err := repo.Append(ctx, "T2", []pm.TaskActionLog{{OccurredAt: base, Action: pm.TaskActionCompleted, ActorRef: "agent:z"}}); err != nil {
		t.Fatalf("Append T2: %v", err)
	}

	got, err := repo.ListByTask(ctx, "T1")
	if err != nil {
		t.Fatalf("ListByTask: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("ListByTask(T1) len = %d, want 4 (T2 must not leak)", len(got))
	}
	// Stable order by (occurred_at, id).
	wantActions := []pm.TaskAction{pm.TaskActionAssigned, pm.TaskActionAgentStarted, pm.TaskActionBlocked, pm.TaskActionAgentStarted}
	for i, w := range wantActions {
		if got[i].Action != w {
			t.Fatalf("entry %d action = %s, want %s (order)", i, got[i].Action, w)
		}
	}
	// Empty-ID entries were assigned ULIDs; the caller-supplied id is preserved.
	for i, lg := range got {
		if lg.ID == "" {
			t.Fatalf("entry %d kept an empty ID — repo must mint one", i)
		}
	}
	if got[2].ID != "fixed-id-xyz" {
		t.Fatalf("caller-supplied id not preserved: %s", got[2].ID)
	}
	// Field fidelity on the blocked entry.
	if got[2].Note != "[obstacle] needs token" || got[2].ActorRef != "agent:c" || got[2].AgentRef != "agent:c" {
		t.Fatalf("blocked entry fields lost: %+v", got[2])
	}
	if !got[0].OccurredAt.Equal(base) {
		t.Fatalf("occurred_at round-trip lost: got %v want %v", got[0].OccurredAt, base)
	}
	// §九 interactions count = COUNT(action='agent_started').
	var started int
	for _, lg := range got {
		if lg.Action == pm.TaskActionAgentStarted {
			started++
		}
	}
	if started != 2 {
		t.Fatalf("agent_started count = %d, want 2", started)
	}
	// Unknown task → empty, not an error.
	if l, err := repo.ListByTask(ctx, "nope"); err != nil || len(l) != 0 {
		t.Fatalf("ListByTask(nope) = %+v, %v", l, err)
	}
}

// TestTaskRepo_BlockLeaseRoundTrip proves the F2 TaskRepo round-trip of the three
// new pm_tasks columns: blocked_reason_type, blocked_comment, execution_lease_expires_at.
func TestTaskRepo_BlockLeaseRoundTrip(t *testing.T) {
	ctx, d := dbSetup(t)
	tr := NewTaskRepo(d)
	t0 := time.Date(2026, 6, 22, 1, 0, 0, 0, time.UTC)

	tk, _ := pm.NewTask(pm.NewTaskInput{ID: "T1", ProjectID: "P1", Title: "do", CreatedBy: "user:a", CreatedAt: t0})
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}
	// Assign → Start → RenewLease: lease must persist; type/comment empty.
	_ = tk.Assign("agent:c", t0)
	_ = tk.Start(t0)
	if err := tk.RenewLease(30*time.Minute, t0); err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	if err := tr.Update(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, _ := tr.FindByID(ctx, "T1")
	if got.ExecutionLeaseExpiresAt() == nil || !got.ExecutionLeaseExpiresAt().Equal(t0.Add(30*time.Minute)) {
		t.Fatalf("lease not round-tripped: %v", got.ExecutionLeaseExpiresAt())
	}
	if got.BlockedReasonType() != "" || got.BlockedComment() != "" {
		t.Fatalf("unexpected block annotation on a running task: type=%q comment=%q", got.BlockedReasonType(), got.BlockedComment())
	}
	// Block(input_required): reason+type persist, lease cleared.
	_ = got.Block("confirm target branch", pm.BlockReasonInputRequired, "agent:c", t0)
	if err := tr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := tr.FindByID(ctx, "T1")
	if re.BlockedReason() != "confirm target branch" || re.BlockedReasonType() != pm.BlockReasonInputRequired {
		t.Fatalf("block annotation lost: reason=%q type=%q", re.BlockedReason(), re.BlockedReasonType())
	}
	if re.ExecutionLeaseExpiresAt() != nil {
		t.Fatalf("Block must clear the lease, got %v", re.ExecutionLeaseExpiresAt())
	}
	// Unblock(comment): comment persists, reason/type cleared.
	_ = re.Unblock("approved", "user:owner", t0)
	if err := tr.Update(ctx, re); err != nil {
		t.Fatal(err)
	}
	fin, _ := tr.FindByID(ctx, "T1")
	if fin.BlockedComment() != "approved" || fin.BlockedReason() != "" || fin.BlockedReasonType() != "" {
		t.Fatalf("unblock round-trip wrong: comment=%q reason=%q type=%q", fin.BlockedComment(), fin.BlockedReason(), fin.BlockedReasonType())
	}
}
