package sqlite

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestAuditLogRepo_AppendListRoundTrip covers the change-log ledger repo (0099 /
// design §4.3): ULID minting, blank-detail → '{}' normalization, newest-first order,
// per-object isolation, and field fidelity.
func TestAuditLogRepo_AppendListRoundTrip(t *testing.T) {
	ctx, d := dbSetup(t)
	repo := NewAuditLogRepo(d, idgen.NewGenerator(clock.SystemClock{}))
	base := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)

	// Three entries on task T1 at distinct times (empty ids → minted), plus a
	// caller-supplied id that must be preserved.
	entries := []pm.AuditEntry{
		{ProjectID: "P1", ObjectType: pm.AuditObjectTask, ObjectID: "T1", ChangeType: pm.AuditTaskCreated, ToValue: "open", ActorRef: "user:pd", OccurredAt: base},
		{ProjectID: "P1", ObjectType: pm.AuditObjectTask, ObjectID: "T1", ChangeType: pm.AuditTaskAssigned, Field: "assignee", ToValue: "agent:c", ActorRef: "user:pd", OccurredAt: base.Add(time.Second)},
		{ID: "fixed-audit-id", ProjectID: "P1", ObjectType: pm.AuditObjectTask, ObjectID: "T1", ChangeType: pm.AuditTaskStatusChanged, Field: "status", FromValue: "open", ToValue: "running", ActorRef: "agent:c", Detail: `{"k":"v"}`, OccurredAt: base.Add(2 * time.Second)},
	}
	for _, e := range entries {
		if err := repo.Append(ctx, e); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	// A different object's entry must not leak into T1's list.
	if err := repo.Append(ctx, pm.AuditEntry{ProjectID: "P1", ObjectType: pm.AuditObjectIssue, ObjectID: "T1", ChangeType: pm.AuditIssueCreated, ActorRef: "user:x", OccurredAt: base}); err != nil {
		t.Fatalf("Append issue: %v", err)
	}

	got, next, err := repo.ListByObject(ctx, pm.AuditObjectTask, "T1", "", 0)
	if err != nil {
		t.Fatalf("ListByObject: %v", err)
	}
	if next != "" {
		t.Fatalf("next cursor = %q, want empty (all fit one page)", next)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3 (issue-with-same-id must not leak)", len(got))
	}
	// Newest-first: status_changed (t+2) → assigned (t+1) → created (t0).
	wantChange := []pm.AuditChangeType{pm.AuditTaskStatusChanged, pm.AuditTaskAssigned, pm.AuditTaskCreated}
	for i, w := range wantChange {
		if got[i].ChangeType != w {
			t.Fatalf("entry %d change_type = %s, want %s (newest-first)", i, got[i].ChangeType, w)
		}
	}
	// Field fidelity on the status_changed entry (also: caller id preserved, detail kept).
	sc := got[0]
	if sc.ID != "fixed-audit-id" || sc.Field != "status" || sc.FromValue != "open" || sc.ToValue != "running" || sc.Detail != `{"k":"v"}` || string(sc.ActorRef) != "agent:c" {
		t.Fatalf("status_changed fields lost: %+v", sc)
	}
	if !sc.OccurredAt.Equal(base.Add(2 * time.Second)) {
		t.Fatalf("occurred_at round-trip lost: %v", sc.OccurredAt)
	}
	// Minted ids on the empty-id entries; blank detail normalized to '{}'.
	for _, e := range got {
		if e.ID == "" {
			t.Fatalf("empty id not minted: %+v", e)
		}
		if e.Detail == "" {
			t.Fatalf("blank detail not normalized to '{}': %+v", e)
		}
	}

	// Unknown object → empty, not an error.
	if l, n, err := repo.ListByObject(ctx, pm.AuditObjectPlan, "nope", "", 0); err != nil || len(l) != 0 || n != "" {
		t.Fatalf("ListByObject(nope) = %+v, %q, %v", l, n, err)
	}
}

// TestAuditLogRepo_CursorPagination proves keyset pagination is stable and complete
// across pages, including a tie on occurred_at (broken on the unique id).
func TestAuditLogRepo_CursorPagination(t *testing.T) {
	ctx, d := dbSetup(t)
	repo := NewAuditLogRepo(d, idgen.NewGenerator(clock.SystemClock{}))
	base := time.Date(2026, 7, 3, 1, 0, 0, 0, time.UTC)

	// 5 entries; two share the SAME occurred_at (tie → id breaks it).
	times := []time.Time{base, base.Add(time.Second), base.Add(2 * time.Second), base.Add(2 * time.Second), base.Add(3 * time.Second)}
	for i, ts := range times {
		if err := repo.Append(ctx, pm.AuditEntry{ProjectID: "P1", ObjectType: pm.AuditObjectTask, ObjectID: "T1", ChangeType: pm.AuditTaskStatusChanged, ToValue: string(rune('a' + i)), ActorRef: "user:x", OccurredAt: ts}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// Page size 2 → pages of 2,2,1. Walk the cursor and collect every id exactly once.
	seen := map[string]bool{}
	cursor := ""
	pages := 0
	for {
		page, next, err := repo.ListByObject(ctx, pm.AuditObjectTask, "T1", cursor, 2)
		if err != nil {
			t.Fatalf("page %d: %v", pages, err)
		}
		pages++
		if len(page) > 2 {
			t.Fatalf("page %d over limit: %d", pages, len(page))
		}
		for _, e := range page {
			if seen[e.ID] {
				t.Fatalf("duplicate id across pages: %s", e.ID)
			}
			seen[e.ID] = true
		}
		if next == "" {
			break
		}
		cursor = next
		if pages > 10 {
			t.Fatal("pagination did not terminate")
		}
	}
	if len(seen) != 5 {
		t.Fatalf("walked %d distinct entries, want 5", len(seen))
	}
	if pages != 3 {
		t.Fatalf("pages = %d, want 3 (2+2+1)", pages)
	}
}
