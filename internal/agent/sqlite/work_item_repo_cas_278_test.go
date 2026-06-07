package sqlite

import (
	"context"
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/agent"
)

// v2.8.1 #278 D PR4: UpdateCAS optimistic lock — the agent-write-vs-reconciler-
// release race guard. The writer that commits second (its loaded version stale)
// loses with ErrWorkItemReassigned; a missing row → ErrWorkItemNotFound.
func TestWorkItemRepo_UpdateCAS_VersionConflict(t *testing.T) {
	wr, _ := newWIDB(t)
	ctx := context.Background()

	w, _ := agent.NewWorkItem(agent.NewWorkItemInput{ID: "WI-cas", AgentID: "A1", TaskRef: "pm://tasks/T1", CreatedAt: t0})
	if err := wr.Save(ctx, w); err != nil { // persisted at version 1
		t.Fatal(err)
	}

	// Two readers load the SAME version (1) — the race setup.
	a, _ := wr.FindByID(ctx, "WI-cas")
	b, _ := wr.FindByID(ctx, "WI-cas")
	if a.Version() != 1 || b.Version() != 1 {
		t.Fatalf("expected both loaded at version 1, got a=%d b=%d", a.Version(), b.Version())
	}

	// Winner: transition + CAS against the loaded version (1) → succeeds, DB → version 2.
	if err := a.Activate(t0); err != nil {
		t.Fatal(err)
	}
	if err := wr.UpdateCAS(ctx, a, 1); err != nil {
		t.Fatalf("first CAS (expected=1) must succeed, got %v", err)
	}

	// Loser: transition from its STALE copy (loaded version 1) + CAS(expected=1) →
	// DB is now version 2 → 0 rows → ErrWorkItemReassigned (row exists, moved).
	if err := b.Cancel(t0); err != nil {
		t.Fatal(err)
	}
	if err := wr.UpdateCAS(ctx, b, 1); !errors.Is(err, agent.ErrWorkItemReassigned) {
		t.Fatalf("second CAS (stale expected=1) must lose → ErrWorkItemReassigned, got %v", err)
	}

	// Sanity: the winner's transition stuck (status=active), not the loser's cancel.
	cur, _ := wr.FindByID(ctx, "WI-cas")
	if cur.Status() != agent.WorkItemActive {
		t.Fatalf("winner's transition must persist: status=%s want active", cur.Status())
	}

	// Missing row → ErrWorkItemNotFound (not reassigned).
	ghost, _ := agent.NewWorkItem(agent.NewWorkItemInput{ID: "WI-ghost", AgentID: "A1", TaskRef: "pm://tasks/T1", CreatedAt: t0})
	_ = ghost.Activate(t0)
	if err := wr.UpdateCAS(ctx, ghost, 1); !errors.Is(err, agent.ErrWorkItemNotFound) {
		t.Fatalf("CAS on missing row → ErrWorkItemNotFound, got %v", err)
	}
}
