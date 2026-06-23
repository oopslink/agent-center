package query_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/observability/query"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

func TestFleetSnapshot_FourSegments_HappyPath(t *testing.T) {
	env := newQEnv(t)
	// Seed: 1 worker online + 1 issue + 1 live execution (an agent-assigned pm
	// task WI-1 in proj/org-1).
	env.seedWorker(t, "W-1", workforce.WorkerOnline)
	env.seedPMIssue(t, "I-1", "proj", "discuss", pm.IssueOpen)
	// v2.14.0 F7 (issue I14): the fleet "executions" segment reads pm_tasks; the
	// worker ActiveCount segment reads worker→agents→ListByAssignee. Link AG-1 to
	// W-1 so its live task WI-1 counts toward W-1's ActiveCount.
	env.seedAgent(t, "AG-1", "W-1")
	env.seedLiveExecution(t, "WI-1", "AG-1", "proj", "org-1", "active")
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{})
	if len(snap.Warnings) != 0 {
		t.Fatalf("warnings: %v", snap.Warnings)
	}
	if len(snap.Tasks) != 1 {
		t.Fatalf("tasks: %d", len(snap.Tasks))
	}
	// v2.14.0 F7: token/tool metrics are 0 (no Task-model source); an active
	// (unblocked running) task has no blocked-reason → empty CurrentActivity.
	if snap.Tasks[0].TaskID != "WI-1" || snap.Tasks[0].Status != "active" {
		t.Fatalf("execution fields wrong: %+v", snap.Tasks[0])
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

// v2.7.1 #206: the fleet work-item row carries the real task title + owning
// project id (read-time, resolved from the pm task already loaded for org scope)
// so Home/Overview can show "Build login flow" + link to the task — not a blank
// name. Unresolvable → empty (UI falls back; covered by the org-scope tests where
// the task is absent).
func TestFleetSnapshot_WorkItemCarriesTaskTitleAndProject(t *testing.T) {
	env := newQEnv(t)
	env.seedOrgProject(t, "proj-x", "org-1")
	// v2.10.2 [T140]: seed with an allocated org number so the row carries the
	// org_ref token "T42" (Worker Activity shows "T<n> + title" + links correctly).
	// v2.14.0 F7: the execution IS an agent-assigned non-terminal pm task.
	tk, err := pm.RehydrateTask(pm.RehydrateTaskInput{
		ID: "task-abc12345", ProjectID: "proj-x", Title: "Build login flow",
		Status: pm.TaskRunning, Assignee: pm.IdentityRef("agent:AG-1"), OrgNumber: 42,
		CreatedBy: "user:test", CreatedAt: env.clk.Now(), UpdatedAt: env.clk.Now(), Version: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.deps.PMTasks.Save(context.Background(), tk); err != nil {
		t.Fatal(err)
	}
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{})
	if len(snap.Tasks) != 1 {
		t.Fatalf("tasks: %d", len(snap.Tasks))
	}
	wi := snap.Tasks[0]
	if wi.TaskID != "task-abc12345" {
		t.Errorf("task_id=%q want task-abc12345", wi.TaskID)
	}
	if wi.TaskTitle != "Build login flow" {
		t.Errorf("task_title=%q want \"Build login flow\"", wi.TaskTitle)
	}
	if wi.TaskOrgRef != "T42" {
		t.Errorf("task_org_ref=%q want T42", wi.TaskOrgRef)
	}
	if wi.ProjectID != "proj-x" {
		t.Errorf("project_id=%q want proj-x", wi.ProjectID)
	}
}

// v2.7 #176 (FINDING-C visibility): the fleet worker row must surface the
// worker's probed CLI capabilities (agent_cli + detected + enabled) so the
// Web Console Environment page can show what each worker discovered — the
// §5 "Environment 可见" exit criterion. Data already lives on
// workforce.Worker (ReportCapabilities → CapabilityList); this asserts the
// projection carries it through.
func TestFleetSnapshot_WorkerRowIncludesCapabilities(t *testing.T) {
	env := newQEnv(t)
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:         "W-CAP",
		EnrolledAt: env.clk.Now(),
		CapabilityList: []workforce.Capability{
			{AgentCLI: "claude-code", Detected: true, Enabled: true, Version: "1.2"},
			{AgentCLI: "codex", Detected: true, Enabled: false},
			{AgentCLI: "opencode", Detected: false, Enabled: false},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := env.deps.Workers.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}

	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{})
	row := fleetWorkerRow(t, snap, "W-CAP")
	if len(row.Capabilities) != 3 {
		t.Fatalf("capabilities: got %d, want 3: %+v", len(row.Capabilities), row.Capabilities)
	}
	byCLI := map[string]query.FleetCapabilityRow{}
	for _, c := range row.Capabilities {
		byCLI[c.AgentCLI] = c
	}
	if c := byCLI["claude-code"]; !c.Detected || !c.Enabled || c.Version != "1.2" {
		t.Fatalf("claude-code want detected+enabled v1.2, got %+v", c)
	}
	if c := byCLI["codex"]; !c.Detected || c.Enabled {
		t.Fatalf("codex want detected+disabled, got %+v", c)
	}
	if c := byCLI["opencode"]; c.Detected || c.Enabled {
		t.Fatalf("opencode want not-detected, got %+v", c)
	}
}

func TestFleetSnapshot_ProjectFilter(t *testing.T) {
	env := newQEnv(t)
	env.seedLiveExecution(t, "WI-A", "AG-A", "proj-a", "org-1", "active")
	env.seedLiveExecution(t, "WI-B", "AG-B", "proj-b", "org-1", "active")
	env.seedPMIssue(t, "I-A", "proj-a", "x", pm.IssueOpen)
	env.seedPMIssue(t, "I-B", "proj-b", "y", pm.IssueOpen)
	svc := query.NewFleetSnapshotService(env.deps)
	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{ProjectID: "proj-a"})
	if len(snap.Tasks) != 1 || snap.Tasks[0].TaskID != "WI-A" {
		t.Fatalf("work item project filter: %+v", snap.Tasks)
	}
	if len(snap.PendingIssues) != 1 || snap.PendingIssues[0].IssueID != "I-A" {
		t.Fatalf("pending issues filter: %+v", snap.PendingIssues)
	}
}

func TestFleetSnapshot_PartialFailure_EmitsWarnings(t *testing.T) {
	env := newQEnv(t)
	// Drop two segments' repos to simulate partial failure. The fleet has 3
	// segments (executions + workers + pending-issues); drop the executions source
	// (PMTasks) + workers → 2 warnings. v2.14.0 F7: executions read pm_tasks.
	deps := env.deps
	deps.PMTasks = nil
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
	if len(snap.Tasks) != 0 || len(snap.Workers) != 0 {
		t.Fatalf("expected empty snapshot, got %+v", snap)
	}
}

// TestFleetSnapshot_OrgScoping_NoCrossOrgLeak is the v2.7 #107 hard §-1 gate:
// org-scoped fleet must NOT leak work items from other orgs. Scoping is resolved
// per work-item via task_ref → pm task → project → org (equivalent to the old
// Tasks.FindByID(org)); fail-closed if a project can't be resolved.
func TestFleetSnapshot_OrgScoping_NoCrossOrgLeak(t *testing.T) {
	env := newQEnv(t)
	// org-A execution + org-B execution.
	env.seedLiveExecution(t, "WI-A", "AG-A", "proj-a", "org-A", "active")
	env.seedLiveExecution(t, "WI-B", "AG-B", "proj-b", "org-B", "active")
	svc := query.NewFleetSnapshotService(env.deps)

	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-A"})
	if len(snap.Tasks) != 1 || snap.Tasks[0].TaskID != "WI-A" {
		t.Fatalf("org scope leaked / wrong: want only WI-A for org-A, got %+v", snap.Tasks)
	}

	// An execution whose project/org can't be resolved (task in a project with no
	// pm-project row) must be EXCLUDED under org scope (fail-closed, never leak).
	env.seedAgentTask(t, "WI-orphan", "AG-X", "proj-missing", "active") // project has no pm-project row
	snap = svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-A"})
	for _, wi := range snap.Tasks {
		if wi.TaskID == "WI-orphan" {
			t.Fatalf("fail-closed violated: unresolvable-project execution leaked under org scope: %+v", snap.Tasks)
		}
	}
	if len(snap.Tasks) != 1 {
		t.Fatalf("org-A should still see exactly WI-A, got %+v", snap.Tasks)
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

// TestFleetSnapshot_ActiveCount_OrgMatch_Counted pins the normal state of the
// #131 §-1 #4 invariant: when a worker's agent runs a work item whose task's
// pm-project belongs to the SAME org as the worker (org-scoped dispatch holds),
// the work item IS counted in ActiveCount and no warning is surfaced.
func TestFleetSnapshot_ActiveCount_OrgMatch_Counted(t *testing.T) {
	env := newQEnv(t)
	env.seedWorkerOrg(t, "W-1", "org-A")
	env.seedAgent(t, "AG-1", "W-1")
	env.seedLiveExecution(t, "WI-1", "AG-1", "proj-a", "org-A", "active")
	svc := query.NewFleetSnapshotService(env.deps)

	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-A"})
	row := fleetWorkerRow(t, snap, "W-1")
	if row.ActiveCount != 1 {
		t.Fatalf("org-match work item must be counted: ActiveCount=%d", row.ActiveCount)
	}
	if len(snap.Warnings) != 0 {
		t.Fatalf("no warning expected on org-match: %v", snap.Warnings)
	}
}

// TestFleetSnapshot_ActiveCount_OrgMismatch_FailClosed is the #131 §-1 #4
// defensive gate. ActiveCount org-scopes via worker.org; the work-item list
// org-scopes via task→pm-project.org — two independent resolution chains. If
// the org-scoped-dispatch invariant breaks (a worker's agent runs a work item
// whose task's pm-project belongs to a DIFFERENT org), the work item must NOT
// be counted (fail-closed — no cross-org count mixing) and a visible warning is
// surfaced instead of a silent count≠list drift.
func TestFleetSnapshot_ActiveCount_OrgMismatch_FailClosed(t *testing.T) {
	env := newQEnv(t)
	env.seedWorkerOrg(t, "W-1", "org-A")
	env.seedAgent(t, "AG-1", "W-1")
	// AG-1 (on org-A worker) runs a live task whose pm-project is org-B — the
	// org-scoped-dispatch invariant is broken.
	env.seedLiveExecution(t, "WI-x", "AG-1", "proj-b", "org-B", "active")
	svc := query.NewFleetSnapshotService(env.deps)

	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-A"})
	row := fleetWorkerRow(t, snap, "W-1")
	if row.ActiveCount != 0 {
		t.Fatalf("fail-closed: org-mismatched work item must NOT be counted, got ActiveCount=%d", row.ActiveCount)
	}
	if len(snap.Warnings) == 0 {
		t.Fatalf("expected a visible org-mismatch warning, got none")
	}
	// §-1 no-leak (PD/Tester red line, same as #119/#137/#138): the warning must
	// NOT name the foreign org — surfacing org-B's id to an org-A viewer is a
	// cross-org existence leak. It must still name the work item for diagnosis.
	joined := strings.Join(snap.Warnings, " ")
	if strings.Contains(joined, "org-B") {
		t.Fatalf("no-leak: warning must not name the foreign org, got %q", joined)
	}
	if !strings.Contains(joined, "WI-x") {
		t.Fatalf("warning should name the work item for diagnosis, got %q", joined)
	}
}

// TestFleetSnapshot_ActiveCount_OrgUnresolvable_FailClosed pins the count==list
// rule for the THIRD state (#131 §-1, PD catch): a work item whose org can't be
// resolved (missing pm task/project). The work-item LIST fail-closes (excludes)
// such items under org scope (fetchExecutions), so ActiveCount must too — else
// count>list. Unresolvable is missing data, NOT an invariant violation, so it
// must NOT emit an "invariant broken" warning (only positive divergence does).
func TestFleetSnapshot_ActiveCount_OrgUnresolvable_FailClosed(t *testing.T) {
	env := newQEnv(t)
	env.seedWorkerOrg(t, "W-1", "org-A")
	env.seedAgent(t, "AG-1", "W-1")
	// Agent-assigned task whose project has no pm-project row → org unresolvable.
	env.seedAgentTask(t, "WI-u", "AG-1", "proj-missing", "active")
	svc := query.NewFleetSnapshotService(env.deps)

	snap := svc.Snapshot(context.Background(), query.SnapshotFilter{OrganizationID: "org-A"})
	row := fleetWorkerRow(t, snap, "W-1")
	if row.ActiveCount != 0 {
		t.Fatalf("unresolvable work item must NOT be counted (match list fail-closed), got ActiveCount=%d", row.ActiveCount)
	}
	for _, w := range snap.Warnings {
		if strings.Contains(w, "invariant broken") {
			t.Fatalf("unresolvable is missing-data, not a violation — no invariant-broken warning expected, got %q", w)
		}
	}
}

func fleetWorkerRow(t *testing.T, snap query.FleetSnapshot, workerID string) query.FleetWorkerRow {
	t.Helper()
	for _, w := range snap.Workers {
		if w.WorkerID == workerID {
			return w
		}
	}
	t.Fatalf("worker row %q not found in snapshot: %+v", workerID, snap.Workers)
	return query.FleetWorkerRow{}
}
