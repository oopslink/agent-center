package query

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability/projection"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
)

// FleetExecutionRow is one row in FleetSnapshot.Executions.
type FleetExecutionRow struct {
	ExecutionID          string `json:"execution_id"`
	TaskID               string `json:"task_id"`
	WorkerID             string `json:"worker_id"`
	AgentCLI             string `json:"agent_cli"`
	WorkspaceMode        string `json:"workspace_mode"`
	Status               string `json:"status"`
	CurrentActivity      string `json:"current_activity,omitempty"`
	WorkingSeconds       int64  `json:"working_seconds"`
	StartedAt            string `json:"started_at"`
	ProjectionLastPushAt string `json:"projection_last_push_at,omitempty"`
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
	Executions        []FleetExecutionRow    `json:"executions"`
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
		execs        []FleetExecutionRow
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
	snap.Executions = execs
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

func (s *FleetSnapshotService) fetchExecutions(ctx context.Context, filter SnapshotFilter) ([]FleetExecutionRow, error) {
	if s.deps.Executions == nil {
		return nil, errors.New("executions repo not wired")
	}
	items, err := s.deps.Executions.FindActive(ctx)
	if err != nil {
		return nil, err
	}
	// Optional project filter via Tasks lookup (multi-roundtrip).
	if filter.ProjectID != "" && s.deps.Tasks != nil {
		filtered := items[:0]
		for _, e := range items {
			t, terr := s.deps.Tasks.FindByID(ctx, e.TaskID())
			if terr == nil && t.ProjectID() == filter.ProjectID {
				filtered = append(filtered, e)
			}
		}
		items = filtered
	}
	// Multi-roundtrip projection lookup (PK 1:1; no SQL JOIN per § 9.z).
	var projMap map[taskruntime.TaskExecutionID]*projection.TaskExecutionProjection
	if s.deps.Projection != nil && len(items) > 0 {
		ids := make([]taskruntime.TaskExecutionID, 0, len(items))
		for _, e := range items {
			ids = append(ids, e.ID())
		}
		ps, perr := s.deps.Projection.FindByIDs(ctx, ids)
		if perr == nil {
			projMap = ps
		}
	}
	out := make([]FleetExecutionRow, 0, len(items))
	for _, e := range items {
		row := FleetExecutionRow{
			ExecutionID:    string(e.ID()),
			TaskID:         string(e.TaskID()),
			WorkerID:       e.WorkerID(),
			AgentCLI:       e.AgentCLI(),
			WorkspaceMode:  string(e.WorkspaceMode()),
			Status:         string(e.Status()),
			WorkingSeconds: e.WorkingSecondsAccumulated(),
			StartedAt:      e.StartedAt().UTC().Format(time.RFC3339Nano),
		}
		if projMap != nil {
			if p := projMap[e.ID()]; p != nil {
				row.CurrentActivity = p.CurrentActivity
				row.ProjectionLastPushAt = p.LastPushAt.UTC().Format(time.RFC3339Nano)
			}
		}
		out = append(out, row)
	}
	return out, nil
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
	out := make([]FleetInputRequestRow, 0, len(items))
	for _, ir := range items {
		if ir.Status() != inputrequest.StatusPending {
			continue
		}
		// Optional project filter: walk through execution → task → project.
		if filter.ProjectID != "" && !s.execMatchesProject(ctx, ir.TaskExecutionID(), filter.ProjectID) {
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
	out := make([]FleetIssueRow, 0, len(items))
	for _, i := range items {
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
