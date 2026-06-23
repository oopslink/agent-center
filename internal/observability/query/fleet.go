package query

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Execution rows live in FleetSnapshot.Tasks. The row VO + its Task-sourced
// formatter live in task_exec_row.go (TaskExecRow / taskExecutionRow) as the
// single source shared with the inspect/query verbs.

// FleetWorkerRow is one row in FleetSnapshot.Workers.
type FleetWorkerRow struct {
	WorkerID string `json:"worker_id"`
	// Name is the operator-facing friendly label (v2.4-D-X1).
	// Defaults to WorkerID when unset so the Fleet view never shows
	// an empty cell.
	Name            string `json:"name"`
	Status          string `json:"status"`
	ActiveCount     int    `json:"active_count"`
	LastHeartbeatAt string `json:"last_heartbeat_at,omitempty"`
	// Capabilities is the worker's probed agent-CLI list (v2.7 #176 /
	// FINDING-C): what ProbeAllAdapters discovered + the per-CLI
	// detected/enabled state, so the Web Console can show which CLIs a
	// worker can run. Omitted when the worker has reported none yet.
	Capabilities []FleetCapabilityRow `json:"capabilities,omitempty"`
}

// FleetCapabilityRow is one probed agent-CLI capability on a worker
// (v2.7 #176). Mirrors workforce.Capability's user-facing fields.
type FleetCapabilityRow struct {
	AgentCLI string `json:"agent_cli"`
	Detected bool   `json:"detected"`
	Enabled  bool   `json:"enabled"`
	Version  string `json:"version,omitempty"`
}

// FleetIssueRow is one row in FleetSnapshot.PendingIssues.
type FleetIssueRow struct {
	IssueID   string `json:"issue_id"`
	ProjectID string `json:"project_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	OpenedAt  string `json:"opened_at"`
	Opener    string `json:"opener"`
}

// FleetSnapshot is the VO returned by FleetSnapshotService.Snapshot.
// Per observability/00 § 7.2 + plan-4 § 1.3.
type FleetSnapshot struct {
	Tasks         []TaskExecRow    `json:"tasks"`
	Workers       []FleetWorkerRow `json:"workers"`
	PendingIssues []FleetIssueRow  `json:"pending_issues"`
	GeneratedAt   string           `json:"generated_at"`
	Warnings      []string         `json:"warnings,omitempty"`
}

// FleetSnapshotService runs the 4-segment parallel aggregation
// (plan-4 § 3.3). Partial failure produces a partial snapshot + warning;
// each segment that returns an error is surfaced via Warnings (per
// conventions § 17: never silently swallow).
type FleetSnapshotService struct {
	deps Deps
}

// NewFleetSnapshotService wires the service.
func NewFleetSnapshotService(deps Deps) *FleetSnapshotService {
	return &FleetSnapshotService{deps: deps}
}

// SnapshotFilter narrows the 4 segments.
type SnapshotFilter struct {
	ProjectID string
	// OrganizationID scopes the entire snapshot to a single organization
	// (v2.6 X1 §3). When set, work-items/issues are limited to the org's pm
	// projects (issue→pm-project→org, work-item→task→pm-project→org) and
	// workers to the org (workers carry organization_id directly).
	OrganizationID string
}

// Snapshot runs the 4 segments concurrently and returns the assembled VO.
//
// Each segment captures its error into Warnings rather than fail-fast —
// users see partial info instead of total opacity. Underlying repo errors
// are NOT discarded; the caller can decide whether non-empty Warnings is
// a hard failure (CLI maps to exit-code 1 + stderr lines).
func (s *FleetSnapshotService) Snapshot(ctx context.Context, filter SnapshotFilter) FleetSnapshot {
	now := time.Now().UTC()
	snap := FleetSnapshot{GeneratedAt: now.Format(time.RFC3339Nano)}
	var (
		execs       []TaskExecRow
		execsErr    error
		workers     []FleetWorkerRow
		workerWarns []string
		workersErr  error
		issues      []FleetIssueRow
		issuesErr   error
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		execs, execsErr = s.fetchExecutions(ctx, filter)
	}()
	go func() {
		defer wg.Done()
		workers, workerWarns, workersErr = s.fetchWorkers(ctx, filter)
	}()
	go func() {
		defer wg.Done()
		issues, issuesErr = s.fetchPendingIssues(ctx, filter)
	}()
	wg.Wait()
	snap.Tasks = execs
	snap.Workers = workers
	snap.PendingIssues = issues
	if execsErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("executions: %v", execsErr))
	}
	if workersErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("workers: %v", workersErr))
	}
	snap.Warnings = append(snap.Warnings, workerWarns...)
	if issuesErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("pending_issues: %v", issuesErr))
	}
	return snap
}

// fetchExecutions reads the live agent executions from pm_tasks (v2.14.0 F7 /
// issue I14: repointed off the retired AgentWorkItem projection model). An
// "execution" is a non-terminal task assigned to an agent (open/running/
// reopened == activeTaskStatuses); the row's work_item_id carries the task id.
// Project/org scoping resolves each task → pm project → org directly; fail-closed
// so a task whose project can't be resolved is excluded under org scope (no
// cross-org leak).
func (s *FleetSnapshotService) fetchExecutions(ctx context.Context, filter SnapshotFilter) ([]TaskExecRow, error) {
	if s.deps.PMTasks == nil {
		return nil, errors.New("pm tasks repo not wired")
	}
	tasks, err := s.deps.PMTasks.ListByStatuses(ctx, activeTaskStatuses)
	if err != nil {
		return nil, err
	}
	orgScoped := filter.OrganizationID != ""
	out := make([]TaskExecRow, 0, len(tasks))
	for _, t := range tasks {
		row := taskExecutionRow(t)
		if row.AgentID == "" {
			continue // executions are agent work only — skip human-assigned/unassigned
		}
		taskOrgRef, projectID, orgID := s.taskProjectOrg(ctx, t)
		if filter.ProjectID != "" && projectID != filter.ProjectID {
			continue
		}
		if orgScoped && orgID != filter.OrganizationID {
			continue // fail-closed: never leak a task whose org can't be confirmed
		}
		// Read-time enrichment for the Home rows (task title + org_ref + owning
		// project for the click-through link). No extra query — the task is in hand.
		row.TaskTitle = t.Title()
		row.TaskOrgRef = taskOrgRef
		row.ProjectID = projectID
		out = append(out, row)
	}
	return out, nil
}

// taskProjectOrg resolves a task's org_ref token ("T<n>") + owning project id +
// owning org id from the pm model: pm task → pm project → organization. Returns
// "" for any hop that can't be resolved (missing repo / project); callers
// fail-closed on org scope so a task whose org can't be confirmed is never
// leaked. No task lookup — the caller already holds the loaded task.
func (s *FleetSnapshotService) taskProjectOrg(ctx context.Context, t *pm.Task) (taskOrgRef, projectID, orgID string) {
	// The org_ref token ("T<n>"); "" when the org-number isn't allocated → UI
	// falls back to a clean #hash.
	if n := t.OrgNumber(); n > 0 {
		taskOrgRef = "T" + strconv.Itoa(n)
	}
	projectID = string(t.ProjectID())
	if s.deps.PMProjects != nil && projectID != "" {
		if pr, perr := s.deps.PMProjects.FindByID(ctx, pm.ProjectID(projectID)); perr == nil && pr != nil {
			orgID = pr.OrganizationID()
		}
	}
	return taskOrgRef, projectID, orgID
}

func (s *FleetSnapshotService) fetchWorkers(ctx context.Context, filter SnapshotFilter) ([]FleetWorkerRow, []string, error) {
	if s.deps.Workers == nil {
		return nil, nil, errors.New("workers repo not wired")
	}
	all, err := s.deps.Workers.FindAll(ctx)
	if err != nil {
		return nil, nil, err
	}
	var warnings []string
	out := make([]FleetWorkerRow, 0, len(all))
	for _, w := range all {
		// v2.6 X1 §3: org scope — workers carry organization_id directly.
		if filter.OrganizationID != "" && w.OrganizationID() != filter.OrganizationID {
			continue
		}
		row := FleetWorkerRow{
			WorkerID:        string(w.ID()),
			Name:            w.Name(),
			Status:          string(w.Status()),
			LastHeartbeatAt: fmtTimePtrStr(w.LastHeartbeatAt()),
		}
		if row.Name == "" {
			row.Name = row.WorkerID
		}
		// v2.7 #176: surface probed CLI capabilities (detected/enabled per
		// agent_cli) so the Web Console Environment view can show what each
		// worker discovered. Data already lives on the Worker AR.
		if caps := w.CapabilityList(); len(caps) > 0 {
			rows := make([]FleetCapabilityRow, 0, len(caps))
			for _, c := range caps {
				rows = append(rows, FleetCapabilityRow{
					AgentCLI: c.AgentCLI,
					Detected: c.Detected,
					Enabled:  c.Enabled,
					Version:  c.Version,
				})
			}
			row.Capabilities = rows
		}
		// v2.14.0 F7 (issue I14): ActiveCount repointed off the retired AgentWorkItem
		// model onto pm_tasks. A worker controls many agents; tasks are assignee-keyed
		// by the agent's member ref ("agent:<member-id>"), so "what's this worker
		// actively running" = ListByWorker → ListByAssignee, counting non-terminal.
		//
		// (multi-path-resolution-same-source): this ActiveCount org-scope (the
		// worker-loop skip above, by worker.OrganizationID) and the executions LIST
		// org-scope (fetchExecutions, by task→pm-project.org) are two INDEPENDENT
		// resolution chains. They agree only insofar as the org-scoped-dispatch
		// invariant holds — i.e. a worker's agents run only tasks whose pm-project
		// shares the worker's org. To keep count and list consistent fail-closed,
		// when org-scoped we verify each counted task's pm-project org equals the
		// worker's org; on mismatch we DON'T count it and surface a visible warning
		// instead of a silent count≠list discrepancy.
		if s.deps.Agents != nil && s.deps.PMTasks != nil {
			agents, _ := s.deps.Agents.ListByWorker(ctx, string(w.ID()))
			active := 0
			for _, ag := range agents {
				memberID := strings.TrimSpace(ag.IdentityMemberID())
				if memberID == "" {
					continue
				}
				tasks, _ := s.deps.PMTasks.ListByAssignee(ctx, pm.IdentityRef("agent:"+memberID))
				for _, t := range tasks {
					if t.Status().IsTerminal() {
						continue
					}
					if filter.OrganizationID != "" {
						// Count==list: fetchExecutions only includes tasks whose
						// pm-project org equals the scope org, fail-closed — so
						// unresolvable ("") AND divergent are BOTH excluded there.
						// Mirror that here: only count when the org resolves to this
						// worker's org.
						_, _, tOrg := s.taskProjectOrg(ctx, t)
						if tOrg != w.OrganizationID() {
							if tOrg != "" {
								// Positive divergence (resolved to a DIFFERENT org) = the
								// org-scoped-dispatch invariant is actually broken → surface
								// a visible warning. No-leak: do NOT name the foreign org;
								// the worker + task ids (in-org) keep it actionable.
								warnings = append(warnings, fmt.Sprintf(
									"worker %s active_count: task %s skipped — its pm-project belongs to a different organization (org-scoped-dispatch invariant broken)",
									w.ID(), t.ID()))
							}
							// tOrg=="" is unresolvable (missing pm project) = missing data,
							// NOT a violation → skip silently, still fail-closed.
							continue
						}
					}
					active++
				}
			}
			row.ActiveCount = active
		}
		out = append(out, row)
	}
	return out, warnings, nil
}

// fleetPendingIssueStatuses is the fleet "pending" issue set (v2.7 #107 #119,
// PD-pinned口径): all NON-terminal pm issue statuses. Terminal {resolved,
// closed, withdrawn} are excluded — they are no longer awaiting attention.
var fleetPendingIssueStatuses = []pm.IssueStatus{pm.IssueOpen, pm.IssueInProgress, pm.IssueReopened}

func (s *FleetSnapshotService) fetchPendingIssues(ctx context.Context, filter SnapshotFilter) ([]FleetIssueRow, error) {
	if s.deps.PMIssues == nil {
		return nil, errors.New("pm issues repo not wired")
	}
	orgScoped := filter.OrganizationID != ""
	var items []*pm.Issue
	var err error
	switch {
	case filter.ProjectID != "":
		items, err = s.deps.PMIssues.ListByProject(ctx, pm.ProjectID(filter.ProjectID))
	case orgScoped:
		// v2.7 #126: org-scoped → query per the org's OWN pm projects
		// (org-bounded), NOT a global oldest-N scan then in-memory org-filter —
		// the latter silently drops an org's issues that fall outside the global
		// window at scale (>100 global pending). Completeness, no silent cap.
		items, err = s.pendingIssuesForOrg(ctx, filter.OrganizationID)
	default:
		// Global admin overview (no project/org filter): capped scan. The 100
		// cap is an explicit overview limit, not a per-org correctness gap.
		items, err = s.deps.PMIssues.FindByStatuses(ctx, fleetPendingIssueStatuses, 100)
	}
	if err != nil {
		return nil, err
	}
	out := make([]FleetIssueRow, 0, len(items))
	for _, i := range items {
		// ListByProject returns all statuses → apply the pending-set filter here
		// (the global FindByStatuses path is already status-filtered; harmless).
		if !isFleetPendingIssue(i.Status()) {
			continue
		}
		projectID := string(i.ProjectID())
		// v2.7 #107 #119: org scope resolved from the pm source
		// (issue→pm-project→org), fail-closed.
		if orgScoped && s.issueOrg(ctx, projectID) != filter.OrganizationID {
			continue
		}
		out = append(out, FleetIssueRow{
			IssueID:   string(i.ID()),
			ProjectID: projectID,
			Title:     i.Title(),
			Status:    string(i.Status()),
			OpenedAt:  i.CreatedAt().UTC().Format(time.RFC3339Nano),
			Opener:    string(i.CreatedBy()),
		})
	}
	return out, nil
}

// issueOrg resolves a pm project's owning org (same pm source as the fleet
// work-item org-scope). Returns "" when unresolvable → caller fail-closes.
func (s *FleetSnapshotService) issueOrg(ctx context.Context, projectID string) string {
	if s.deps.PMProjects == nil || projectID == "" {
		return ""
	}
	pr, err := s.deps.PMProjects.FindByID(ctx, pm.ProjectID(projectID))
	if err != nil || pr == nil {
		return ""
	}
	return pr.OrganizationID()
}

// pendingIssuesForOrg returns issues across ALL of the org's pm projects
// (org-bounded), so org-scoped fleet sees its complete pending set — avoiding
// the global oldest-N truncation that a single capped FindByStatuses-then-filter
// would impose at scale (v2.7 #126). Status filtering stays in the caller.
func (s *FleetSnapshotService) pendingIssuesForOrg(ctx context.Context, orgID string) ([]*pm.Issue, error) {
	if s.deps.PMProjects == nil {
		return nil, errors.New("pm projects repo not wired")
	}
	projects, err := s.deps.PMProjects.ListByOrg(ctx, orgID)
	if err != nil {
		return nil, err
	}
	var out []*pm.Issue
	for _, p := range projects {
		issues, lerr := s.deps.PMIssues.ListByProject(ctx, p.ID())
		if lerr != nil {
			return nil, lerr
		}
		out = append(out, issues...)
	}
	return out, nil
}

func isFleetPendingIssue(st pm.IssueStatus) bool {
	for _, p := range fleetPendingIssueStatuses {
		if st == p {
			return true
		}
	}
	return false
}

func fmtTimePtrStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
