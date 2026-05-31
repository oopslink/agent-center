package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability/projection"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/workforce"
)

// FleetWorkItemRow rows live in FleetSnapshot.WorkItems. The row VO + its
// formatter moved to work_item_row.go (WorkItemRow / workItemRowFromProjection)
// as the single source shared with the inspect/query verbs (#107 Phase-2).

// FleetWorkerRow is one row in FleetSnapshot.Workers.
type FleetWorkerRow struct {
	WorkerID        string `json:"worker_id"`
	// Name is the operator-facing friendly label (v2.4-D-X1).
	// Defaults to WorkerID when unset so the Fleet view never shows
	// an empty cell.
	Name            string `json:"name"`
	Status          string `json:"status"`
	ActiveCount     int    `json:"active_count"`
	MappingsCount   int    `json:"mappings_count"`
	LastHeartbeatAt string `json:"last_heartbeat_at,omitempty"`
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
	WorkItems     []WorkItemRow    `json:"work_items"`
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
	// (v2.6 X1 §3). When set, executions/input-requests/issues are limited to
	// the org's projects and workers to the org. Requires deps.Projects.
	OrganizationID string
}

// orgProjectSet resolves the set of project IDs owned by filter.OrganizationID.
// Returns (nil, false) when no org scoping is requested. Returns (set, true)
// when org scoping applies (set may be empty → no rows pass).
func (s *FleetSnapshotService) orgProjectSet(ctx context.Context, filter SnapshotFilter) (map[string]bool, bool) {
	if filter.OrganizationID == "" || s.deps.Projects == nil {
		return nil, false
	}
	projects, err := s.deps.Projects.FindAll(ctx, workforce.ProjectFilter{OrganizationID: filter.OrganizationID})
	if err != nil {
		return map[string]bool{}, true // scoping requested but lookup failed → empty (fail-closed)
	}
	set := make(map[string]bool, len(projects))
	for _, p := range projects {
		set[string(p.ID())] = true
	}
	return set, true
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
		execs      []WorkItemRow
		execsErr   error
		workers    []FleetWorkerRow
		workersErr error
		issues     []FleetIssueRow
		issuesErr  error
	)
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		execs, execsErr = s.fetchExecutions(ctx, filter)
	}()
	go func() {
		defer wg.Done()
		workers, workersErr = s.fetchWorkers(ctx, filter)
	}()
	go func() {
		defer wg.Done()
		issues, issuesErr = s.fetchPendingIssues(ctx, filter)
	}()
	wg.Wait()
	snap.WorkItems = execs
	snap.Workers = workers
	snap.PendingIssues = issues
	if execsErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("executions: %v", execsErr))
	}
	if workersErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("workers: %v", workersErr))
	}
	if issuesErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("pending_issues: %v", issuesErr))
	}
	return snap
}

// fetchExecutions reads the LIVE work-item projections (v2.7 #107: repointed off
// the retired task-execution model to agent_work_item_projections). Project/org
// scoping is preserved by resolving each work item's task_ref → pm task → project
// (equivalent to the old Tasks.FindByID(org) path); fail-closed so a work item
// whose project can't be resolved is excluded under org scope (no cross-org leak).
func (s *FleetSnapshotService) fetchExecutions(ctx context.Context, filter SnapshotFilter) ([]WorkItemRow, error) {
	if s.deps.WorkItemProjections == nil {
		return nil, errors.New("work item projections repo not wired")
	}
	projs, err := s.deps.WorkItemProjections.List(ctx, projection.AgentWorkItemProjectionFilter{
		Statuses: []string{
			string(agentpkg.WorkItemQueued),
			string(agentpkg.WorkItemActive),
			string(agentpkg.WorkItemWaitingInput),
		},
	})
	if err != nil {
		return nil, err
	}
	orgScoped := filter.OrganizationID != ""
	out := make([]WorkItemRow, 0, len(projs))
	for _, p := range projs {
		taskID, projectID, orgID := s.workItemTaskProjectOrg(ctx, p.WorkItemID)
		if filter.ProjectID != "" && projectID != filter.ProjectID {
			continue
		}
		if orgScoped && orgID != filter.OrganizationID {
			continue // fail-closed: never leak a work item whose org can't be confirmed
		}
		out = append(out, workItemRowFromProjection(p, taskID))
	}
	return out, nil
}

// workItemTaskProjectOrg resolves a work item's task id + owning project id +
// owning org id, all from the pm model: work_item.task_ref ("pm://tasks/{id}")
// → pm task → pm project → organization. Returns "" for any hop that can't be
// resolved (missing repos / work item / task / project); callers fail-closed on
// org scope so a work item whose org can't be confirmed is never leaked.
//
// v2.7 #107: org is resolved from the pm project (same source as project), NOT
// the retired workforce `projects` table — mixing the two made org-scope fail
// closed on every work item at runtime (workforce projects are empty).
func (s *FleetSnapshotService) workItemTaskProjectOrg(ctx context.Context, workItemID string) (taskID, projectID, orgID string) {
	if s.deps.WorkItems == nil {
		return "", "", ""
	}
	wi, err := s.deps.WorkItems.FindByID(ctx, workItemID)
	if err != nil || wi == nil {
		return "", "", ""
	}
	id, ok := fleetTaskIDFromRef(wi.TaskRef())
	if !ok {
		return "", "", ""
	}
	taskID = id
	if s.deps.PMTasks == nil {
		return taskID, "", ""
	}
	tk, terr := s.deps.PMTasks.FindByID(ctx, pm.TaskID(id))
	if terr != nil || tk == nil {
		return taskID, "", ""
	}
	projectID = string(tk.ProjectID())
	if s.deps.PMProjects != nil && projectID != "" {
		if pr, perr := s.deps.PMProjects.FindByID(ctx, pm.ProjectID(projectID)); perr == nil && pr != nil {
			orgID = pr.OrganizationID()
		}
	}
	return taskID, projectID, orgID
}

// fleetTaskIDFromRef extracts {id} from a "pm://tasks/{id}" work-item task ref.
func fleetTaskIDFromRef(ref string) (string, bool) {
	const p = "pm://tasks/"
	if strings.HasPrefix(ref, p) && len(ref) > len(p) {
		return strings.TrimPrefix(ref, p), true
	}
	return "", false
}

func (s *FleetSnapshotService) fetchWorkers(ctx context.Context, filter SnapshotFilter) ([]FleetWorkerRow, error) {
	if s.deps.Workers == nil {
		return nil, errors.New("workers repo not wired")
	}
	all, err := s.deps.Workers.FindAll(ctx)
	if err != nil {
		return nil, err
	}
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
		if s.deps.Mappings != nil {
			ms, _ := s.deps.Mappings.FindByWorkerID(ctx, w.ID())
			if filter.ProjectID != "" {
				match := false
				for _, m := range ms {
					if string(m.ProjectID()) == filter.ProjectID {
						match = true
						break
					}
				}
				if !match {
					continue
				}
			}
			row.MappingsCount = len(ms)
		}
		if s.deps.Executions != nil {
			exs, _ := s.deps.Executions.FindByWorkerID(ctx, string(w.ID()), execution.StatusSubmitted, execution.StatusWorking, execution.StatusInputRequired)
			row.ActiveCount = len(exs)
		}
		out = append(out, row)
	}
	return out, nil
}

func (s *FleetSnapshotService) fetchPendingIssues(ctx context.Context, filter SnapshotFilter) ([]FleetIssueRow, error) {
	if s.deps.Issues == nil {
		return nil, errors.New("issues repo not wired")
	}
	var items []*discussion.Issue
	var err error
	openStatus := discussion.StatusOpen
	if filter.ProjectID != "" {
		items, err = s.deps.Issues.FindByProject(ctx, filter.ProjectID, discussion.IssueFilter{Status: &openStatus, Limit: 100})
	} else {
		items, err = s.deps.Issues.FindByStatus(ctx, discussion.StatusOpen, discussion.IssueFilter{Limit: 100})
	}
	if err != nil {
		return nil, err
	}
	orgProjects, orgScoped := s.orgProjectSet(ctx, filter)
	out := make([]FleetIssueRow, 0, len(items))
	for _, i := range items {
		// v2.6 X1 §3: org scope — issue's project must be in the org.
		if orgScoped && !orgProjects[i.ProjectID()] {
			continue
		}
		out = append(out, FleetIssueRow{
			IssueID:   string(i.ID()),
			ProjectID: i.ProjectID(),
			Title:     i.Title(),
			Status:    string(i.Status()),
			OpenedAt:  i.OpenedAt().UTC().Format(time.RFC3339Nano),
			Opener:    i.OpenedByIdentityID(),
		})
	}
	return out, nil
}

func fmtTimePtrStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
