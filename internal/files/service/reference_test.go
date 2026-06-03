package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/files"
	filesqlite "github.com/oopslink/agent-center/internal/files/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

// newRefService builds a Service wired with an in-memory DB + migrated repos,
// sufficient for exercising the reference-CRUD methods and the reachability
// primitive (the transfer-flow deps are left nil — unused here).
func newRefService(t *testing.T) (*Service, files.FileReferenceRepository, *clock.FakeClock) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	clk := clock.NewFakeClock(time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC))
	refs := filesqlite.NewFileReferenceRepo(db)
	svc := New(Deps{
		DB:         db,
		References: refs,
		IDGen:      idgen.NewGenerator(clk),
		Clock:      clk,
	})
	return svc, refs, clk
}

func mustURI(t *testing.T) files.FileURI {
	t.Helper()
	uri, err := files.NewFileURI(idgen.MustNewULID())
	if err != nil {
		t.Fatal(err)
	}
	return uri
}

func TestAddReference_Persists(t *testing.T) {
	svc, repo, _ := newRefService(t)
	ctx := context.Background()
	uri := mustURI(t)

	ref, err := svc.AddReference(ctx, AddReferenceCmd{
		FileURI:     uri,
		Scope:       files.ScopeTask,
		ScopeID:     "task-1",
		Filename:    "report.pdf",
		MimeType:    "application/pdf",
		SizeBytes:   123,
		DisplayName: "Report",
		CreatedBy:   "user:x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID == "" || !ref.IsLive() {
		t.Fatalf("AddReference returned bad ref: %+v", ref)
	}
	if !ref.CreatedAt.Equal(time.Date(2026, 5, 29, 12, 0, 0, 0, time.UTC)) {
		t.Fatalf("CreatedAt not from clock: %v", ref.CreatedAt)
	}

	// FindByScope returns it.
	byScope, err := repo.FindByScope(ctx, files.ScopeTask, "task-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(byScope) != 1 || byScope[0].ID != ref.ID {
		t.Fatalf("FindByScope = %+v; want the new ref", byScope)
	}

	// FindByURI returns it.
	byURI, err := repo.FindByURI(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	if len(byURI) != 1 || byURI[0].ID != ref.ID {
		t.Fatalf("FindByURI = %+v; want the new ref", byURI)
	}

	// CountLiveByURI == 1.
	n, err := repo.CountLiveByURI(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("CountLiveByURI = %d; want 1", n)
	}

	// ListReferences passthrough.
	list, err := svc.ListReferences(ctx, uri)
	if err != nil || len(list) != 1 || list[0].ID != ref.ID {
		t.Fatalf("ListReferences = %+v, %v", list, err)
	}
}

func TestSoftDeleteReference_DropsLive(t *testing.T) {
	svc, repo, _ := newRefService(t)
	ctx := context.Background()
	uri := mustURI(t)

	ref, err := svc.AddReference(ctx, AddReferenceCmd{
		FileURI:   uri,
		Scope:     files.ScopeTask,
		ScopeID:   "task-1",
		CreatedBy: "user:x",
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := svc.SoftDeleteReference(ctx, ref.ID); err != nil {
		t.Fatal(err)
	}

	n, err := repo.CountLiveByURI(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("CountLiveByURI after soft-delete = %d; want 0", n)
	}

	byURI, err := repo.FindByURI(ctx, uri)
	if err != nil {
		t.Fatal(err)
	}
	if len(byURI) != 0 {
		t.Fatalf("FindByURI after soft-delete = %+v; want none", byURI)
	}

	// Idempotent: a second soft-delete is a no-op (no error).
	if err := svc.SoftDeleteReference(ctx, ref.ID); err != nil {
		t.Fatalf("second SoftDeleteReference: %v", err)
	}
}

func TestReachable(t *testing.T) {
	ctx := context.Background()

	taskScope := ScopeRef{Scope: files.ScopeTask, ScopeID: "task-1"}
	projScope := ScopeRef{Scope: files.ScopeProject, ScopeID: "proj-1"}
	otherScope := ScopeRef{Scope: files.ScopeIssue, ScopeID: "issue-9"}

	t.Run("live ref in caller scopes -> true", func(t *testing.T) {
		svc, _, _ := newRefService(t)
		uri := mustURI(t)
		if _, err := svc.AddReference(ctx, AddReferenceCmd{
			FileURI: uri, Scope: taskScope.Scope, ScopeID: taskScope.ScopeID, CreatedBy: "user:x",
		}); err != nil {
			t.Fatal(err)
		}
		ok, err := svc.Reachable(ctx, uri, []ScopeRef{taskScope})
		if err != nil || !ok {
			t.Fatalf("Reachable = %v, %v; want true", ok, err)
		}
	})

	t.Run("soft-deleted ref does NOT grant reachability", func(t *testing.T) {
		svc, _, _ := newRefService(t)
		uri := mustURI(t)
		ref, err := svc.AddReference(ctx, AddReferenceCmd{
			FileURI: uri, Scope: taskScope.Scope, ScopeID: taskScope.ScopeID, CreatedBy: "user:x",
		})
		if err != nil {
			t.Fatal(err)
		}
		// Was reachable while live.
		if ok, _ := svc.Reachable(ctx, uri, []ScopeRef{taskScope}); !ok {
			t.Fatal("precondition: live ref should be reachable")
		}
		// After soft-delete the SAME scope no longer reaches the blob.
		if err := svc.SoftDeleteReference(ctx, ref.ID); err != nil {
			t.Fatal(err)
		}
		ok, err := svc.Reachable(ctx, uri, []ScopeRef{taskScope})
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("Reachable = true after soft-delete; want false (soft-deleted ref must not grant reachability)")
		}
	})

	t.Run("live ref not in caller scopes -> false", func(t *testing.T) {
		svc, _, _ := newRefService(t)
		uri := mustURI(t)
		if _, err := svc.AddReference(ctx, AddReferenceCmd{
			FileURI: uri, Scope: taskScope.Scope, ScopeID: taskScope.ScopeID, CreatedBy: "user:x",
		}); err != nil {
			t.Fatal(err)
		}
		ok, err := svc.Reachable(ctx, uri, []ScopeRef{otherScope})
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("Reachable = true; want false (scope not in caller scopes)")
		}
	})

	t.Run("empty caller scopes -> false", func(t *testing.T) {
		svc, _, _ := newRefService(t)
		uri := mustURI(t)
		if _, err := svc.AddReference(ctx, AddReferenceCmd{
			FileURI: uri, Scope: taskScope.Scope, ScopeID: taskScope.ScopeID, CreatedBy: "user:x",
		}); err != nil {
			t.Fatal(err)
		}
		ok, err := svc.Reachable(ctx, uri, nil)
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("Reachable = true for empty caller scopes; want false")
		}
	})

	t.Run("multiple refs, one matching -> true", func(t *testing.T) {
		svc, _, _ := newRefService(t)
		uri := mustURI(t)
		// Two placements of the same blob: project + task.
		if _, err := svc.AddReference(ctx, AddReferenceCmd{
			FileURI: uri, Scope: projScope.Scope, ScopeID: projScope.ScopeID, CreatedBy: "user:x",
		}); err != nil {
			t.Fatal(err)
		}
		if _, err := svc.AddReference(ctx, AddReferenceCmd{
			FileURI: uri, Scope: taskScope.Scope, ScopeID: taskScope.ScopeID, CreatedBy: "user:x",
		}); err != nil {
			t.Fatal(err)
		}
		// Caller only has the task scope; the matching ref grants reachability.
		ok, err := svc.Reachable(ctx, uri, []ScopeRef{taskScope, otherScope})
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			t.Fatal("Reachable = false; want true (one of multiple refs matches)")
		}
	})

	t.Run("no live ref at all -> false", func(t *testing.T) {
		svc, _, _ := newRefService(t)
		uri := mustURI(t)
		ok, err := svc.Reachable(ctx, uri, []ScopeRef{taskScope})
		if err != nil {
			t.Fatal(err)
		}
		if ok {
			t.Fatal("Reachable = true with no refs; want false")
		}
	})
}
