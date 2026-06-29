package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	outboxsql "github.com/oopslink/agent-center/internal/outbox/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

// fakeRepoResolver maps repo_id → url (v2.18.4 BE-1 merge-check resolver test).
type fakeRepoResolver map[string]string

func (f fakeRepoResolver) RepoURL(_ context.Context, repoID string) (string, error) {
	return f[repoID], nil
}

func coderepoRefSetup(t *testing.T, resolver CodeRepoResolver) (*Service, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Unix(1_700_000_000, 0).UTC())
	svc := New(Deps{
		DB: db, Projects: pmsql.NewProjectRepo(db), Members: pmsql.NewProjectMemberRepo(db),
		Issues: pmsql.NewIssueRepo(db), Tasks: pmsql.NewTaskRepo(db),
		TaskSubs: pmsql.NewTaskSubscriberRepo(db), IssueSubs: pmsql.NewIssueSubscriberRepo(db),
		CodeRepoRefs: pmsql.NewCodeRepoRefRepo(db), Outbox: outboxsql.NewOutboxRepo(db),
		IDGen: idgen.NewGenerator(clk), Clock: clk, CodeRepoResolver: resolver,
	})
	return svc, context.Background()
}

func mkProject(t *testing.T, svc *Service, ctx context.Context) pm.ProjectID {
	t.Helper()
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	return pid
}

func TestCodeRepoReference_AddSetPrimaryRemove(t *testing.T) {
	svc, ctx := coderepoRefSetup(t, nil)
	pid := mkProject(t, svc, ctx)

	// Add two workspace-Repo references; the first as primary.
	r1, err := svc.AddCodeRepoReference(ctx, AddCodeRepoReferenceCommand{ProjectID: pid, RepoID: "repo-A", Label: "A", IsPrimary: true, Actor: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := svc.AddCodeRepoReference(ctx, AddCodeRepoReferenceCommand{ProjectID: pid, RepoID: "repo-B", Label: "B", Actor: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	assertPrimary(t, svc, ctx, pid, r1)

	// Promote r2 → it becomes the sole primary (r1 demoted).
	if err := svc.SetPrimaryCodeRepo(ctx, pid, r2, "user:a"); err != nil {
		t.Fatal(err)
	}
	assertPrimary(t, svc, ctx, pid, r2)

	// A url-only ref (no repo_id) is allowed.
	if _, err := svc.AddCodeRepoReference(ctx, AddCodeRepoReferenceCommand{ProjectID: pid, URL: "https://x/legacy", Actor: "user:a"}); err != nil {
		t.Fatalf("url-only ref add: %v", err)
	}
	// A ref with neither url nor repo_id is rejected.
	if _, err := svc.AddCodeRepoReference(ctx, AddCodeRepoReferenceCommand{ProjectID: pid, Actor: "user:a"}); err == nil {
		t.Fatal("ref with neither url nor repo_id must be rejected")
	}

	// Remove r1.
	if err := svc.RemoveCodeRepoReference(ctx, pid, r1, "user:a"); err != nil {
		t.Fatal(err)
	}
	refs, _ := svc.ListCodeRepos(ctx, pid)
	for _, ref := range refs {
		if ref.ID() == r1 {
			t.Fatal("r1 should be removed")
		}
	}
}

func assertPrimary(t *testing.T, svc *Service, ctx context.Context, pid pm.ProjectID, wantPrimary string) {
	t.Helper()
	refs, err := svc.ListCodeRepos(ctx, pid)
	if err != nil {
		t.Fatal(err)
	}
	primaries := 0
	for _, ref := range refs {
		if ref.IsPrimary() {
			primaries++
			if ref.ID() != wantPrimary {
				t.Fatalf("primary = %s, want %s", ref.ID(), wantPrimary)
			}
		}
	}
	if primaries != 1 {
		t.Fatalf("primary count = %d, want exactly 1", primaries)
	}
}

// White-box: primaryRepoURL follows the primary ref's repo_id → workspace Repo url
// via the resolver, falls back to a url-only ref, and to the first ref when no
// primary is set.
func TestPrimaryRepoURL_ResolvesViaReference(t *testing.T) {
	resolver := fakeRepoResolver{"repo-A": "https://ws/app-A", "repo-B": "https://ws/app-B"}
	svc, ctx := coderepoRefSetup(t, resolver)
	pid := mkProject(t, svc, ctx)

	// No refs → "".
	if url, _ := svc.primaryRepoURL(ctx, pid); url != "" {
		t.Fatalf("no refs → %q, want empty", url)
	}

	// First ref (no primary yet) is used; repo-A resolves to its workspace url.
	_, _ = svc.AddCodeRepoReference(ctx, AddCodeRepoReferenceCommand{ProjectID: pid, RepoID: "repo-A", Actor: "user:a"})
	if url, _ := svc.primaryRepoURL(ctx, pid); url != "https://ws/app-A" {
		t.Fatalf("single ref → %q, want https://ws/app-A", url)
	}

	// Add repo-B as PRIMARY → resolver returns its url even though A was first.
	_, _ = svc.AddCodeRepoReference(ctx, AddCodeRepoReferenceCommand{ProjectID: pid, RepoID: "repo-B", IsPrimary: true, Actor: "user:a"})
	if url, _ := svc.primaryRepoURL(ctx, pid); url != "https://ws/app-B" {
		t.Fatalf("primary repo-B → %q, want https://ws/app-B", url)
	}

	// A primary ref whose repo is unknown to the resolver falls back to its own url.
	pid2 := mkProject(t, svc, ctx)
	_, _ = svc.AddCodeRepoReference(ctx, AddCodeRepoReferenceCommand{ProjectID: pid2, RepoID: "gone", URL: "https://fallback/url", IsPrimary: true, Actor: "user:a"})
	if url, _ := svc.primaryRepoURL(ctx, pid2); url != "https://fallback/url" {
		t.Fatalf("unresolvable primary → %q, want fallback url", url)
	}
}

// A legacy url-only ref resolves to its own url even when no resolver is wired.
func TestPrimaryRepoURL_LegacyUrlOnly(t *testing.T) {
	svc, ctx := coderepoRefSetup(t, nil)
	pid := mkProject(t, svc, ctx)
	_, _ = svc.AddCodeRepoReference(ctx, AddCodeRepoReferenceCommand{ProjectID: pid, URL: "https://legacy/url", Actor: "user:a"})
	if url, _ := svc.primaryRepoURL(ctx, pid); url != "https://legacy/url" {
		t.Fatalf("legacy url-only → %q, want https://legacy/url", url)
	}
}

// A ref belonging to ANOTHER project is never mutated/removed across projects.
func TestCodeRepoReference_CrossProjectGuard(t *testing.T) {
	svc, ctx := coderepoRefSetup(t, nil)
	pidA := mkProject(t, svc, ctx)
	pidB := mkProject(t, svc, ctx)
	refA, err := svc.AddCodeRepoReference(ctx, AddCodeRepoReferenceCommand{ProjectID: pidA, RepoID: "r", Actor: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	// Removing refA via project B is rejected (not leaked across projects).
	if err := svc.RemoveCodeRepoReference(ctx, pidB, refA, "user:a"); err != pm.ErrCodeRepoRefNotFound {
		t.Fatalf("cross-project remove = %v, want ErrCodeRepoRefNotFound", err)
	}
	if err := svc.SetPrimaryCodeRepo(ctx, pidB, refA, "user:a"); err != pm.ErrCodeRepoRefNotFound {
		t.Fatalf("cross-project set-primary = %v, want ErrCodeRepoRefNotFound", err)
	}
	// A non-member actor is rejected by the membership gate.
	if err := svc.RemoveCodeRepoReference(ctx, pidA, refA, "user:stranger"); err == nil {
		t.Fatal("non-member remove must be rejected")
	}
}
