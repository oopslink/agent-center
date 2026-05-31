package query_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/observability/query"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/workforce"
	wforce "github.com/oopslink/agent-center/internal/workforce"
)

func TestFleetSnapshot_FourSegments_HappyPath(t *testing.T) {
	env := newQEnv(t)
	// Seed: 1 worker online + 1 issue + 1 live work item (which seeds its pm
	// task T-1). The legacy execution is kept only as inert data the fleet no
	// longer reads (proj-A: fleet reads work-item projections).
	env.seedExecution(t, "E-1", "T-1", "W-1", execution.StatusInputRequired)
	env.seedWorker(t, "W-1", workforce.WorkerOnline)
	env.seedPMIssue(t, "I-1", "proj", "discuss", pm.IssueOpen)
	// projection
	if _, _, err := env.deps.Projection.UpsertIfFresh(context.Background(), "E-1", projection.ProjectionUpdate{LastPushAt: env.clk.Now(), CurrentActivity: "edit"}); err != nil {
		t.Fatal(err)
	}
	// v2.7 #107: the fleet "executions" segment now reads live work-item projections.
	env.seedLiveWorkItem(t, "WI-1", "AG-1", "T-1", "proj", "org-1", "active")
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{})
	if len(snap.Warnings) != 0 {
		t.Fatalf("warnings: %v", snap.Warnings)
	}
	if len(snap.WorkItems) != 1 {
		t.Fatalf("work_items: %d", len(snap.WorkItems))
	}
	if snap.WorkItems[0].WorkItemID != "WI-1" || snap.WorkItems[0].CurrentActivity != "edit" || snap.WorkItems[0].TotalToolCalls != 2 {
		t.Fatalf("work item fields not joined: %+v", snap.WorkItems[0])
	}
	if len(snap.Workers) != 1 {
		t.Fatalf("workers: %d", len(snap.Workers))
	}
	if snap.Workers[0].ActiveCount != 1 {
		t.Fatalf("active_count: %d", snap.Workers[0].ActiveCount)
	}
	if len(snap.PendingIssues) != 1 {
		t.Fatalf("pending_issues: %d", len(snap.PendingIssues))
	}
}

func TestFleetSnapshot_ProjectFilter(t *testing.T) {
	env := newQEnv(t)
	env.seedLiveWorkItem(t, "WI-A", "AG-A", "T-1", "proj-a", "org-1", "active")
	env.seedLiveWorkItem(t, "WI-B", "AG-B", "T-2", "proj-b", "org-1", "active")
	env.seedPMIssue(t, "I-A", "proj-a", "x", pm.IssueOpen)
	env.seedPMIssue(t, "I-B", "proj-b", "y", pm.IssueOpen)
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{ProjectID: "proj-a"})
	if len(snap.WorkItems) != 1 || snap.WorkItems[0].TaskID != "T-1" {
		t.Fatalf("work item project filter: %+v", snap.WorkItems)
	}
	if len(snap.PendingIssues) != 1 || snap.PendingIssues[0].IssueID != "I-A" {
		t.Fatalf("pending issues filter: %+v", snap.PendingIssues)
	}
}

func TestFleetSnapshot_PartialFailure_EmitsWarnings(t *testing.T) {
	env := newQEnv(t)
	// Drop two segments' repos to simulate partial failure. Post-#118 (IR
	// segment dropped) the fleet has 3 segments (work-items + workers +
	// pending-issues); drop work-items + workers → 2 warnings.
	deps := env.deps
	deps.WorkItemProjections = nil
	deps.Workers = nil
	svc := query.NewFleetSnapshotService(deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{})
	if len(snap.Warnings) != 2 {
		t.Fatalf("expected 2 warnings, got %d (%v)", len(snap.Warnings), snap.Warnings)
	}
}

func TestFleetSnapshot_Empty_NoCrash(t *testing.T) {
	env := newQEnv(t)
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{})
	if len(snap.WorkItems) != 0 || len(snap.Workers) != 0 {
		t.Fatalf("expected empty snapshot, got %+v", snap)
	}
	// Suppress unused warnings about untyped use of imports under build tags.
	_ = errors.New
	_ = discussion.StatusOpen
	_ = conversation.ConversationKindTask
	_ = wforce.WorkerOnline
}

// TestFleetSnapshot_OrgScoping_NoCrossOrgLeak is the v2.7 #107 hard §-1 gate:
// org-scoped fleet must NOT leak work items from other orgs. Scoping is resolved
// per work-item via task_ref → pm task → project → org (equivalent to the old
// Tasks.FindByID(org)); fail-closed if a project can't be resolved.
func TestFleetSnapshot_OrgScoping_NoCrossOrgLeak(t *testing.T) {
	env := newQEnv(t)
	// org-A work item + org-B work item.
	env.seedLiveWorkItem(t, "WI-A", "AG-A", "TA", "proj-a", "org-A", "active")
	env.seedLiveWorkItem(t, "WI-B", "AG-B", "TB", "proj-b", "org-B", "active")
	svc := query.NewFleetSnapshotService(env.deps)

	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-A"})
	if len(snap.WorkItems) != 1 || snap.WorkItems[0].WorkItemID != "WI-A" {
		t.Fatalf("org scope leaked / wrong: want only WI-A for org-A, got %+v", snap.WorkItems)
	}

	// A work item whose project/org can't be resolved (no task_ref hop) must be
	// EXCLUDED under org scope (fail-closed, never leak).
	env.seedWorkItemProjection(t, "WI-orphan", "AG-X", "active") // no work_item row → task_ref unresolvable
	snap = svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-A"})
	for _, wi := range snap.WorkItems {
		if wi.WorkItemID == "WI-orphan" {
			t.Fatalf("fail-closed violated: unresolvable-project work item leaked under org scope: %+v", snap.WorkItems)
		}
	}
	if len(snap.WorkItems) != 1 {
		t.Fatalf("org-A should still see exactly WI-A, got %+v", snap.WorkItems)
	}
}

// TestFleetSnapshot_PendingIssues_OrgScopingAndPendingSet is the #119 §-1 gate:
// the pending-issues segment reads pm_issues and org-scopes via the pm source
// (issue→pm-project→org), fail-closed — never the retired workforce orgProjectSet
// (empty at runtime → would exclude everything, the PR#1 bug class). It also pins
// the PD口径: pending = {open, in_progress, reopened}; terminal {resolved, closed,
// withdrawn} excluded.
func TestFleetSnapshot_PendingIssues_OrgScopingAndPendingSet(t *testing.T) {
	env := newQEnv(t)
	// real pm projects carrying org (the runtime org source).
	env.seedOrgProject(t, "proj-a", "org-A")
	env.seedOrgProject(t, "proj-b", "org-B")
	// org-A: all three non-terminal statuses + one terminal (must be excluded).
	env.seedPMIssue(t, "IA-open", "proj-a", "o", pm.IssueOpen)
	env.seedPMIssue(t, "IA-inprog", "proj-a", "p", pm.IssueInProgress)
	env.seedPMIssue(t, "IA-reopened", "proj-a", "r", pm.IssueReopened)
	env.seedPMIssue(t, "IA-resolved", "proj-a", "x", pm.IssueResolved) // terminal — excluded
	// org-B issue (must NOT leak under org-A scope).
	env.seedPMIssue(t, "IB-open", "proj-b", "b", pm.IssueOpen)
	// orphan: open issue whose project has no pm-project row → org unresolvable → fail-closed.
	env.seedPMIssue(t, "IO-orphan", "proj-missing", "m", pm.IssueOpen)

	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-A"})
	got := map[string]bool{}
	for _, i := range snap.PendingIssues {
		got[i.IssueID] = true
	}
	if len(snap.PendingIssues) != 3 || !got["IA-open"] || !got["IA-inprog"] || !got["IA-reopened"] {
		t.Fatalf("org-A pending set wrong: want {IA-open,IA-inprog,IA-reopened}, got %+v", snap.PendingIssues)
	}
	if got["IA-resolved"] {
		t.Fatal("terminal issue leaked into pending set")
	}
	if got["IB-open"] {
		t.Fatal("org scope leaked: org-B issue visible under org-A")
	}
	if got["IO-orphan"] {
		t.Fatal("fail-closed violated: orphan (unresolvable-org) issue leaked under org scope")
	}
}

// TestFleetSnapshot_PendingIssues_OrgScopeNoGlobalTruncation is the #126
// completeness fix: under org scope, an org's pending issues must NOT be dropped
// because they fall outside a global oldest-N window. The org-scoped path
// queries per the org's own projects (PMProjects.ListByOrg → ListByProject), so
// it is org-bounded — not global-limit(100)-then-filter.
func TestFleetSnapshot_PendingIssues_OrgScopeNoGlobalTruncation(t *testing.T) {
	env := newQEnv(t)
	env.seedOrgProject(t, "proj-a", "org-A")
	env.seedOrgProject(t, "proj-b", "org-B")
	// 100 org-A open issues whose ids sort BEFORE org-B's → under the fake clock
	// (equal created_at → id tiebreak) they fill the global oldest-100 window.
	for i := 0; i < 100; i++ {
		env.seedPMIssue(t, fmt.Sprintf("IA-%03d", i), "proj-a", "a", pm.IssueOpen)
	}
	env.seedPMIssue(t, "IB-zzz", "proj-b", "b", pm.IssueOpen)
	svc := query.NewFleetSnapshotService(env.deps)
	// Pre-#126 (global FindByStatuses(100) → org-filter) returned the 100 org-A
	// issues then dropped them all for org-B → IB-zzz silently lost. Per-org fix
	// returns exactly IB-zzz.
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-B"})
	if len(snap.PendingIssues) != 1 || snap.PendingIssues[0].IssueID != "IB-zzz" {
		t.Fatalf("org-B pending truncated by global window (want [IB-zzz]): got %+v", snap.PendingIssues)
	}
}
