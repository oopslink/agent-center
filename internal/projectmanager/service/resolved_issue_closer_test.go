package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func resolvedCloserFixture(t *testing.T) (*Service, *clock.FakeClock, context.Context, pm.ProjectID, pm.IssueID) {
	t.Helper()
	svc, _, ctx := setup(t)
	fc, ok := svc.clock.(*clock.FakeClock)
	if !ok {
		t.Fatalf("test service clock = %T, want *clock.FakeClock", svc.clock)
	}
	pid, err := svc.CreateProject(ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	iid, err := svc.CreateIssue(ctx, CreateIssueCommand{ProjectID: pid, Title: "bug", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	return svc, fc, ctx, pid, iid
}

func TestCloseResolvedIssues_ClosesAfterThreeDays(t *testing.T) {
	svc, fc, ctx, _, iid := resolvedCloserFixture(t)

	fc.Advance(time.Hour)
	resolvedAt := fc.Now()
	if err := svc.SetIssueStatus(ctx, iid, pm.IssueResolved, "user:a"); err != nil {
		t.Fatal(err)
	}
	fc.Set(resolvedAt.Add(72*time.Hour + time.Second))
	n, err := svc.CloseResolvedIssues(ctx, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("closed = %d, want 1", n)
	}
	got, err := svc.issues.FindByID(ctx, iid)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status() != pm.IssueClosed {
		t.Fatalf("status = %s, want closed", got.Status())
	}
	if !got.StatusChangedAt().Equal(fc.Now()) {
		t.Fatalf("closed statusChangedAt = %v, want %v", got.StatusChangedAt(), fc.Now())
	}
}

func TestCloseResolvedIssues_WaitsForGracePeriod(t *testing.T) {
	svc, fc, ctx, _, iid := resolvedCloserFixture(t)

	fc.Advance(time.Hour)
	resolvedAt := fc.Now()
	if err := svc.SetIssueStatus(ctx, iid, pm.IssueResolved, "user:a"); err != nil {
		t.Fatal(err)
	}
	fc.Set(resolvedAt.Add(72*time.Hour - time.Second))
	n, err := svc.CloseResolvedIssues(ctx, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("closed = %d, want 0 before grace period", n)
	}
	got, _ := svc.issues.FindByID(ctx, iid)
	if got.Status() != pm.IssueResolved {
		t.Fatalf("status = %s, want resolved", got.Status())
	}
}

func TestCloseResolvedIssues_ReopenedIssueNotClosed(t *testing.T) {
	svc, fc, ctx, _, iid := resolvedCloserFixture(t)

	if err := svc.SetIssueStatus(ctx, iid, pm.IssueResolved, "user:a"); err != nil {
		t.Fatal(err)
	}
	fc.Advance(time.Hour)
	if err := svc.SetIssueStatus(ctx, iid, pm.IssueReopened, "user:a"); err != nil {
		t.Fatal(err)
	}
	fc.Advance(100 * time.Hour)
	n, err := svc.CloseResolvedIssues(ctx, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("closed = %d, want 0 for reopened issue", n)
	}
	got, _ := svc.issues.FindByID(ctx, iid)
	if got.Status() != pm.IssueReopened {
		t.Fatalf("status = %s, want reopened", got.Status())
	}
}

func TestCloseResolvedIssues_Idempotent(t *testing.T) {
	svc, fc, ctx, _, iid := resolvedCloserFixture(t)

	if err := svc.SetIssueStatus(ctx, iid, pm.IssueResolved, "user:a"); err != nil {
		t.Fatal(err)
	}
	fc.Advance(73 * time.Hour)
	if n, err := svc.CloseResolvedIssues(ctx, 72*time.Hour); err != nil || n != 1 {
		t.Fatalf("first close: n=%d err=%v, want 1 nil", n, err)
	}
	if n, err := svc.CloseResolvedIssues(ctx, 72*time.Hour); err != nil || n != 0 {
		t.Fatalf("second close: n=%d err=%v, want 0 nil", n, err)
	}
}

func TestCloseResolvedIssues_SkipsArchivedProject(t *testing.T) {
	svc, fc, ctx, pid, iid := resolvedCloserFixture(t)

	if err := svc.SetIssueStatus(ctx, iid, pm.IssueResolved, "user:a"); err != nil {
		t.Fatal(err)
	}
	if err := svc.ArchiveProject(ctx, pid, "user:a"); err != nil {
		t.Fatal(err)
	}
	fc.Advance(73 * time.Hour)
	n, err := svc.CloseResolvedIssues(ctx, 72*time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("closed = %d, want 0 for archived project", n)
	}
	got, _ := svc.issues.FindByID(ctx, iid)
	if got.Status() != pm.IssueResolved {
		t.Fatalf("status = %s, want resolved", got.Status())
	}
}
