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
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/workforce"
)

// FleetWorkItemRow is one row in FleetSnapshot.WorkItems (v2.7 #107: the new
// work-item model replaced the retired task-execution model — execution→work-item).
type FleetWorkItemRow struct {
	WorkItemID        string `json:"work_item_id"`
	AgentID           string `json:"agent_id"`
	TaskID            string `json:"task_id,omitempty"`
	Status            string `json:"status"`
	CurrentActivity   string `json:"current_activity,omitempty"`
	TotalToolCalls    int64  `json:"total_tool_calls"`
	TotalTokensInput  int64  `json:"total_tokens_input"`
	TotalTokensOutput int64  `json:"total_tokens_output"`
	// WorkingSeconds is 0 in v2.7 (no per-turn duration source; Opt2 deferred v2.8).
	WorkingSeconds int64  `json:"working_seconds"`
	LastActivityAt string `json:"last_activity_at,omitempty"`
}

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

// FleetInputRequestRow is one row in FleetSnapshot.OpenInputRequests.
type FleetInputRequestRow struct {
	InputRequestID  string `json:"input_request_id"`
	TaskExecutionID string `json:"task_execution_id"`
	Question        string `json:"question"`
	Urgency         string `json:"urgency"`
	RequestedAt     string `json:"requested_at"`
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
	WorkItems         []FleetWorkItemRow     `json:"work_items"`
	Workers           []FleetWorkerRow       `json:"workers"`
	OpenInputRequests []FleetInputRequestRow `json:"open_input_requests"`
	PendingIssues     []FleetIssueRow        `json:"pending_issues"`
	GeneratedAt       string                 `json:"generated_at"`
	Warnings          []string               `json:"warnings,omitempty"`
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
		execs        []FleetWorkItemRow
		execsErr     error
		workers      []FleetWorkerRow
		workersErr   error
		inputReqs    []FleetInputRequestRow
		inputReqsErr error
		issues       []FleetIssueRow
		issuesErr    error
	)
	var wg sync.WaitGroup
	wg.Add(4)
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
		inputReqs, inputReqsErr = s.fetchOpenInputRequests(ctx, filter)
	}()
	go func() {
		defer wg.Done()
		issues, issuesErr = s.fetchPendingIssues(ctx, filter)
	}()
	wg.Wait()
	snap.WorkItems = execs
	snap.Workers = workers
	snap.OpenInputRequests = inputReqs
	snap.PendingIssues = issues
	if execsErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("executions: %v", execsErr))
	}
	if workersErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("workers: %v", workersErr))
	}
	if inputReqsErr != nil {
		snap.Warnings = append(snap.Warnings, fmt.Sprintf("input_requests: %v", inputReqsErr))
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
func (s *FleetSnapshotService) fetchExecutions(ctx context.Context, filter SnapshotFilter) ([]FleetWorkItemRow, error) {
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
	orgProjects, orgScoped := s.orgProjectSet(ctx, filter)
	out := make([]FleetWorkItemRow, 0, len(projs))
	for _, p := range projs {
		taskID, projectID := s.workItemTaskAndProject(ctx, p.WorkItemID)
		if filter.ProjectID != "" && projectID != filter.ProjectID {
			continue
		}
		if orgScoped && (projectID == "" || !orgProjects[projectID]) {
			continue // fail-closed: never leak a work item whose org can't be confirmed
		}
		out = append(out, FleetWorkItemRow{
			WorkItemID:        p.WorkItemID,
			AgentID:           p.AgentID,
			TaskID:            taskID,
			Status:            p.Status,
			CurrentActivity:   p.CurrentActivity,
			TotalToolCalls:    p.TotalToolCalls,
			TotalTokensInput:  p.TotalTokensInput,
			TotalTokensOutput: p.TotalTokensOutput,
			WorkingSeconds:    p.WorkingSecondsAccumulated,
			LastActivityAt:    p.LastActivityAt.UTC().Format(time.RFC3339Nano),
		})
	}
	return out, nil
}

// workItemTaskAndProject resolves a work item's task id + owning project id via
// work_item.task_ref ("pm://tasks/{id}") → pm task. Returns ("","") when
// unresolvable (missing repos / work item / task); callers fail-closed on org scope.
func (s *FleetSnapshotService) workItemTaskAndProject(ctx context.Context, workItemID string) (taskID, projectID string) {
	if s.deps.WorkItems == nil {
		return "", ""
	}
	wi, err := s.deps.WorkItems.FindByID(ctx, workItemID)
	if err != nil || wi == nil {
		return "", ""
	}
	id, ok := fleetTaskIDFromRef(wi.TaskRef())
	if !ok {
		return "", ""
	}
	taskID = id
	if s.deps.PMTasks != nil {
		if tk, terr := s.deps.PMTasks.FindByID(ctx, pm.TaskID(id)); terr == nil && tk != nil {
			projectID = string(tk.ProjectID())
		}
	}
	return taskID, projectID
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

func (s *FleetSnapshotService) fetchOpenInputRequests(ctx context.Context, filter SnapshotFilter) ([]FleetInputRequestRow, error) {
	if s.deps.InputReqs == nil {
		return nil, errors.New("input_requests repo not wired")
	}
	items, err := s.deps.InputReqs.FindPending(ctx, time.Now().Add(365*24*time.Hour))
	if err != nil {
		return nil, err
	}
	orgProjects, orgScoped := s.orgProjectSet(ctx, filter)
	out := make([]FleetInputRequestRow, 0, len(items))
	for _, ir := range items {
		if ir.Status() != inputrequest.StatusPending {
			continue
		}
		// Optional project filter: walk through execution → task → project.
		if filter.ProjectID != "" && !s.execMatchesProject(ctx, ir.TaskExecutionID(), filter.ProjectID) {
			continue
		}
		// v2.6 X1 §3: org scope — walk execution → task → project, keep only org's.
		if orgScoped && !s.execMatchesOrgProjects(ctx, ir.TaskExecutionID(), orgProjects) {
			continue
		}
		out = append(out, FleetInputRequestRow{
			InputRequestID:  string(ir.ID()),
			TaskExecutionID: string(ir.TaskExecutionID()),
			Question:        ir.Question(),
			Urgency:         string(ir.Urgency()),
			RequestedAt:     ir.RequestedAt().UTC().Format(time.RFC3339Nano),
		})
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

// execMatchesOrgProjects resolves execID → task → project and reports whether
// the project is in the org's project set.
func (s *FleetSnapshotService) execMatchesOrgProjects(ctx context.Context, execID taskruntime.TaskExecutionID, orgProjects map[string]bool) bool {
	if s.deps.Executions == nil || s.deps.Tasks == nil {
		return false
	}
	e, err := s.deps.Executions.FindByID(ctx, execID)
	if err != nil || e == nil {
		return false
	}
	t, err := s.deps.Tasks.FindByID(ctx, e.TaskID())
	if err != nil || t == nil {
		return false
	}
	return orgProjects[t.ProjectID()]
}

func (s *FleetSnapshotService) execMatchesProject(ctx context.Context, execID taskruntime.TaskExecutionID, projectID string) bool {
	if s.deps.Executions == nil || s.deps.Tasks == nil {
		return true
	}
	e, err := s.deps.Executions.FindByID(ctx, execID)
	if err != nil {
		return false
	}
	t, err := s.deps.Tasks.FindByID(ctx, e.TaskID())
	if err != nil {
		return false
	}
	return t.ProjectID() == projectID
}

func fmtTimePtrStr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.UTC().Format(time.RFC3339Nano)
}
