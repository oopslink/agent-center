package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/observability"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

// Deps bundles the read-side repos QueryService needs. Every field is an
// interface, so tests can inject fakes; production wires SQLite impls via
// NewService.
type Deps struct {
	Events observability.EventRepository
	// v2.14.0 F7 (issue I14): the read side is sourced from the pm model. PMTasks
	// is the executions data source (pm_tasks, replacing the retired
	// agent_work_item_projections); PMProjects resolves project→org for the
	// executions segment's org-scoping (same pm source as PMTasks).
	PMTasks    pm.TaskRepository
	PMProjects pm.ProjectRepository
	// PMIssues is the fleet pending-issues source (v2.7 #107 #119): the
	// pending-issues segment reads pm_issues (not the retired discussion model)
	// and org-scopes via PMProjects (issue→pm-project→org, same pm source).
	PMIssues pm.IssueRepository
	// Agents resolves a worker's agents (worker→agents→work-items) for the
	// inspectWorker repoint (v2.7 #107 Phase-2 proj-A): one worker controls many
	// agents; work items are agent-keyed, so "what's this worker running" =
	// ListByWorker → ListByAgent. Same pm·agent source (no retired task_executions).
	Agents        agentpkg.Repository
	Conversations conversation.ConversationRepository
	Messages      conversation.MessageRepository
	Workers       workforce.WorkerRepository
}

// Service implements the 2 core verbs (Inspect / Query). FleetSnapshot is
// its own service (internal/observability/query/fleet.go); Stats is
// internal/observability/stats. Logs / peek-trace live in their own
// packages.
type Service struct {
	deps Deps
}

// NewService wires the QueryService.
func NewService(deps Deps) *Service {
	return &Service{deps: deps}
}

// Inspect dispatches to the kind-specific assembler. Returns
// ErrInspectKindUnknown for unrecognised kinds (caller maps to ExitUsage),
// ErrInspectNotFound when the id is unknown, ErrInspectUnimplemented when
// the kind is reserved for a future phase and has no data yet.
func (s *Service) Inspect(ctx context.Context, kind, id string) (InspectResult, error) {
	if id == "" {
		return InspectResult{}, ErrInspectIDRequired
	}
	// The switch is the single dispatch site for InspectKind. Its default
	// arm is the single source of truth for "unknown kind" — we don't pre-
	// validate, because that would create a dead post-switch fallback that
	// can never be covered (§ 17: every statement either handles a real
	// case or is removed).
	switch InspectKind(kind) {
	case InspectTask:
		return s.inspectTask(ctx, id)
	case InspectExecution:
		return s.inspectExecution(ctx, id)
	case InspectWorker:
		return s.inspectWorker(ctx, id)
	case InspectIssue:
		return s.inspectIssue(ctx, id)
	case InspectConversation:
		return s.inspectConversation(ctx, id)
	case InspectProject:
		return s.inspectProject(ctx, id)
	default:
		return InspectResult{}, fmt.Errorf("%w: %q", ErrInspectKindUnknown, kind)
	}
}

// Query dispatches to the resource-specific list assembler. Unknown
// resources route through the switch's default arm (single source of truth;
// no pre-validation, to avoid a dead post-switch fallback that can never
// be covered — § 17).
func (s *Service) Query(ctx context.Context, resource string, filter QueryFilter) (QueryResult, error) {
	switch QueryResource(resource) {
	case QueryTasks:
		return s.queryTasks(ctx, filter)
	case QueryExecutions:
		return s.queryExecutions(ctx, filter)
	case QueryWorkers:
		return s.queryWorkers(ctx, filter)
	case QueryIssues:
		return s.queryIssues(ctx, filter)
	case QueryEvents:
		return s.queryEvents(ctx, filter)
	default:
		return QueryResult{}, fmt.Errorf("%w: %q", ErrQueryResourceUnknown, resource)
	}
}

// applyDefaultLimit returns filter.Limit clamped to (0, observability.MaxEventQueryLimit].
// Used by per-resource queryX assemblers that pass through `limit`.
func applyDefaultLimit(limit int) int { //nolint:unused // referenced via dispatch helpers in queryTasks / queryIssues
	if limit <= 0 {
		return 100
	}
	if limit > observability.MaxEventQueryLimit {
		return observability.MaxEventQueryLimit
	}
	return limit
}

// ---- Inspect assemblers --------------------------------------------------

func (s *Service) inspectTask(ctx context.Context, id string) (InspectResult, error) {
	if s.deps.PMTasks == nil {
		return InspectResult{}, errors.New("query: pm tasks repo not wired")
	}
	t, err := s.deps.PMTasks.FindByID(ctx, pm.TaskID(id))
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	// v2.7 #107 Phase-2 (proj-B): read pm.Task. priority/conversation_id dropped
	// (no pm.Task field); assignee/created_by/completed_by/blocked_reason added
	// (pm has them); from_issue_id → derived_from_issue.
	out := map[string]any{
		"id":                 string(t.ID()),
		"project_id":         string(t.ProjectID()),
		"title":              t.Title(),
		"description":        t.Description(),
		"status":             string(t.Status()),
		"assignee":           stringOrNil(string(t.Assignee())),
		"created_by":         string(t.CreatedBy()),
		"completed_by":       stringOrNil(string(t.CompletedBy())),
		"blocked_reason":     stringOrNil(t.BlockedReason()),
		"derived_from_issue": stringOrNil(string(t.DerivedFromIssue())),
		"created_at":         t.CreatedAt().UTC().Format(time.RFC3339Nano),
		"updated_at":         t.UpdatedAt().UTC().Format(time.RFC3339Nano),
		"version":            t.Version(),
	}
	// Recent events (limit small).
	if s.deps.Events != nil {
		evs, _ := s.deps.Events.Find(ctx, observability.EventQueryFilter{
			Refs: observability.EventRefsFilter{TaskID: id}, Limit: 50,
		})
		out["recent_events"] = projectEventSummaryList(evs)
	}
	return InspectResult{Kind: InspectTask, ID: id, Data: out}, nil
}

func (s *Service) inspectExecution(ctx context.Context, id string) (InspectResult, error) {
	// v2.14.0 F7 (issue I14): "execution" inspect repointed off the retired
	// AgentWorkItem model onto pm_tasks. The id is now a TASK id (the task is the
	// unit of agent work); the execution row carries the mapped status + the
	// blocked annotation. recent_events filter by task_id (the task lifecycle).
	if s.deps.PMTasks == nil {
		return InspectResult{}, errors.New("query: pm tasks repo not wired")
	}
	t, err := s.deps.PMTasks.FindByID(ctx, pm.TaskID(id))
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	out := map[string]any{
		"work_item_id": string(t.ID()),
		"agent_id":     agentMemberIDFromAssignee(t.Assignee()),
		"task_id":      string(t.ID()),
		"status":       taskExecStatus(t),
		"created_at":   t.CreatedAt().UTC().Format(time.RFC3339Nano),
		"updated_at":   t.UpdatedAt().UTC().Format(time.RFC3339Nano),
		"version":      t.Version(),
	}
	out["projection"] = taskExecutionRow(t)
	if s.deps.Events != nil {
		evs, _ := s.deps.Events.Find(ctx, observability.EventQueryFilter{
			Refs: observability.EventRefsFilter{TaskID: id}, Limit: 50,
		})
		out["recent_events"] = projectEventSummaryList(evs)
	}
	return InspectResult{Kind: InspectExecution, ID: id, Data: out}, nil
}

func (s *Service) inspectWorker(ctx context.Context, id string) (InspectResult, error) {
	if s.deps.Workers == nil {
		return InspectResult{}, errors.New("query: workers repo not wired")
	}
	w, err := s.deps.Workers.FindByID(ctx, workforce.WorkerID(id))
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	out := map[string]any{
		"id":                string(w.ID()),
		"status":            string(w.Status()),
		"capabilities":      w.Capabilities(),
		"enrolled_at":       w.EnrolledAt().UTC().Format(time.RFC3339Nano),
		"last_heartbeat_at": fmtTimePtr(w.LastHeartbeatAt()),
		"working_seconds":   w.WorkingSeconds(),
		"version":           w.Version(),
	}
	// v2.14.0 F7 (issue I14): "what's this worker running" = worker→its agents→
	// their live tasks (non-terminal), repointed off the retired AgentWorkItem
	// model onto pm_tasks (assignee-keyed by the agent's member ref). fail-loud if
	// the deps aren't wired (missing injection must error, not nil-panic).
	if s.deps.Agents == nil || s.deps.PMTasks == nil {
		return InspectResult{}, errors.New("query: agents/pm-tasks repo not wired")
	}
	agents, _ := s.deps.Agents.ListByWorker(ctx, string(w.ID()))
	activeTasks := make([]any, 0)
	for _, ag := range agents {
		memberID := strings.TrimSpace(ag.IdentityMemberID())
		if memberID == "" {
			continue
		}
		tasks, _ := s.deps.PMTasks.ListByAssignee(ctx, pm.IdentityRef("agent:"+memberID))
		for _, t := range tasks {
			if t.Status().IsTerminal() {
				continue // active = non-terminal (queued/active/waiting_input)
			}
			activeTasks = append(activeTasks, projectTaskExecutionSummary(t))
		}
	}
	out["active_work_items"] = activeTasks
	return InspectResult{Kind: InspectWorker, ID: id, Data: out}, nil
}

func (s *Service) inspectIssue(ctx context.Context, id string) (InspectResult, error) {
	// v2.7 #125: inspect-issue reads pm_issues (the retired discussion model is
	// gone from this read path). pm.Issue has no origin/conversation_id — so
	// opened_by←created_by, opened_at←created_at, +description/updated_at; the
	// origin field + messages-via-conversation section are dropped (E2-doc).
	if s.deps.PMIssues == nil {
		return InspectResult{}, errors.New("query: pm issues repo not wired")
	}
	i, err := s.deps.PMIssues.FindByID(ctx, pm.IssueID(id))
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	out := map[string]any{
		"id":          string(i.ID()),
		"project_id":  string(i.ProjectID()),
		"title":       i.Title(),
		"description": i.Description(),
		"status":      string(i.Status()),
		"opened_by":   string(i.CreatedBy()),
		"opened_at":   i.CreatedAt().UTC().Format(time.RFC3339Nano),
		"updated_at":  i.UpdatedAt().UTC().Format(time.RFC3339Nano),
		"version":     i.Version(),
	}
	if s.deps.Events != nil {
		evs, _ := s.deps.Events.Find(ctx, observability.EventQueryFilter{
			Refs: observability.EventRefsFilter{IssueID: id}, Limit: 50,
		})
		out["recent_events"] = projectEventSummaryList(evs)
	}
	return InspectResult{Kind: InspectIssue, ID: id, Data: out}, nil
}

func (s *Service) inspectConversation(ctx context.Context, id string) (InspectResult, error) {
	if s.deps.Conversations == nil {
		return InspectResult{}, errors.New("query: conversations repo not wired")
	}
	c, err := s.deps.Conversations.FindByID(ctx, conversation.ConversationID(id))
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	out := map[string]any{
		"id":          string(c.ID()),
		"kind":        string(c.Kind()),
		"status":      string(c.Status()),
		"name":        c.Name(),
		"description": c.Description(),
		"opened_at":   c.OpenedAt().UTC().Format(time.RFC3339Nano),
		"version":     c.Version(),
	}
	if s.deps.Messages != nil {
		msgs, _ := s.deps.Messages.FindByConversationID(ctx, c.ID(), conversation.MessageFilter{Limit: 100})
		out["messages"] = projectMessageList(msgs)
	}
	return InspectResult{Kind: InspectConversation, ID: id, Data: out}, nil
}

func (s *Service) inspectProject(ctx context.Context, id string) (InspectResult, error) {
	// v2.7 #131: inspect-project reads pm_projects (the retired workforce project
	// model is gone from this read path). pm.Project has NO Tags() → the tags
	// output field is dropped; the mappings segment is dropped (workforce
	// WorkerProjectMapping retired). The tasks segment already reads pm_tasks.
	if s.deps.PMProjects == nil {
		return InspectResult{}, errors.New("query: pm projects repo not wired")
	}
	p, err := s.deps.PMProjects.FindByID(ctx, pm.ProjectID(id))
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	out := map[string]any{
		"id":              string(p.ID()),
		"organization_id": p.OrganizationID(),
		"name":            p.Name(),
		"description":     p.Description(),
		"version":         p.Version(),
		"created_at":      p.CreatedAt().UTC().Format(time.RFC3339Nano),
	}
	if s.deps.PMTasks != nil {
		ts, _ := s.deps.PMTasks.ListByProject(ctx, pm.ProjectID(string(p.ID())))
		out["tasks"] = projectTaskList(ts)
	}
	return InspectResult{Kind: InspectProject, ID: id, Data: out}, nil
}

// ---- Query assemblers ---------------------------------------------------

// activeTaskStatuses is the non-terminal task set — exactly pm.TaskStatus where
// !IsTerminal() — used as the default `query tasks` view (the new-model "active
// work" equivalent of the old open-only default). Pinned to IsTerminal() by a
// partition test (TestActiveTaskStatuses_MatchesIsTerminal). v2.7 #107 proj-B.
var activeTaskStatuses = []pm.TaskStatus{
	pm.TaskOpen, pm.TaskRunning, pm.TaskReopened,
}

func filterTasksByStatus(items []*pm.Task, st pm.TaskStatus) []*pm.Task {
	var out []*pm.Task
	for _, t := range items {
		if t.Status() == st {
			out = append(out, t)
		}
	}
	return out
}

func (s *Service) queryTasks(ctx context.Context, f QueryFilter) (QueryResult, error) {
	if s.deps.PMTasks == nil {
		return QueryResult{}, errors.New("query: pm tasks repo not wired")
	}
	limit := applyDefaultLimit(f.Limit)
	var items []*pm.Task
	var err error
	switch {
	case f.ProjectID != "":
		items, err = s.deps.PMTasks.ListByProject(ctx, pm.ProjectID(f.ProjectID))
		if err == nil && f.Status != "" {
			items = filterTasksByStatus(items, pm.TaskStatus(f.Status))
		}
	case f.Status != "":
		items, err = s.deps.PMTasks.ListByStatuses(ctx, []pm.TaskStatus{pm.TaskStatus(f.Status)})
	default:
		// default = the non-terminal active set {open,assigned,running,blocked,
		// reopened} (== !IsTerminal()).
		items, err = s.deps.PMTasks.ListByStatuses(ctx, activeTaskStatuses)
	}
	if err != nil {
		return QueryResult{}, err
	}
	// v2.7 #107 Phase-2 (proj-B): the --blocked-by and --priority filters are
	// dropped — pm.Task has no dependency graph (only a blocked_reason string)
	// and no priority, so neither has a new-model equivalent (direct-delete,
	// same class as the retired input_request verb in #127).
	if len(items) > limit {
		items = items[:limit]
	}
	out := make([]any, 0, len(items))
	for _, t := range items {
		out = append(out, projectTaskRow(t))
	}
	return QueryResult{Resource: QueryTasks, Items: out}, nil
}

func (s *Service) queryExecutions(ctx context.Context, f QueryFilter) (QueryResult, error) {
	// v2.14.0 F7 (issue I14): repointed off the retired AgentWorkItem model onto
	// pm_tasks. An execution is a non-terminal agent-assigned task; rows carry the
	// mapped status (queued/active/waiting_input). by-task → the task itself;
	// by-worker → worker→agents→ListByAssignee; status/active → ListByStatuses over
	// the active set. The exec-specific FailedReason filter is dropped — there is
	// no failed status on the Task model (blocked annotation replaces it).
	if s.deps.PMTasks == nil {
		return QueryResult{}, errors.New("query: pm tasks repo not wired")
	}
	rowFor := func(t *pm.Task) (any, bool) {
		if t.Status().IsTerminal() {
			return nil, false
		}
		if agentMemberIDFromAssignee(t.Assignee()) == "" {
			return nil, false // executions are agent work only
		}
		return taskExecutionRow(t), true
	}
	out := make([]any, 0)
	switch {
	case f.TaskID != "":
		t, err := s.deps.PMTasks.FindByID(ctx, pm.TaskID(f.TaskID))
		if err != nil {
			return QueryResult{}, mapNotFound(err)
		}
		if row, ok := rowFor(t); ok {
			out = append(out, row)
		}
	case f.WorkerID != "":
		if s.deps.Agents == nil {
			return QueryResult{}, errors.New("query: agents repo not wired")
		}
		agents, _ := s.deps.Agents.ListByWorker(ctx, f.WorkerID)
		for _, ag := range agents {
			memberID := strings.TrimSpace(ag.IdentityMemberID())
			if memberID == "" {
				continue
			}
			tasks, _ := s.deps.PMTasks.ListByAssignee(ctx, pm.IdentityRef("agent:"+memberID))
			for _, t := range tasks {
				if row, ok := rowFor(t); ok {
					out = append(out, row)
				}
			}
		}
	default:
		tasks, err := s.deps.PMTasks.ListByStatuses(ctx, activeTaskStatuses)
		if err != nil {
			return QueryResult{}, err
		}
		// --status filters on the mapped execution-status label (queued/active/
		// waiting_input); "active" (or empty) means the whole active set.
		want := f.Status
		for _, t := range tasks {
			row, ok := rowFor(t)
			if !ok {
				continue
			}
			if want != "" && want != "active" && taskExecStatus(t) != want {
				continue
			}
			out = append(out, row)
		}
	}
	return QueryResult{Resource: QueryExecutions, Items: out}, nil
}

func (s *Service) queryWorkers(ctx context.Context, f QueryFilter) (QueryResult, error) {
	if s.deps.Workers == nil {
		return QueryResult{}, errors.New("query: workers repo not wired")
	}
	var items []*workforce.Worker
	var err error
	if f.Status != "" {
		items, err = s.deps.Workers.FindByStatus(ctx, workforce.WorkerStatus(f.Status))
	} else {
		items, err = s.deps.Workers.FindAll(ctx)
	}
	if err != nil {
		return QueryResult{}, err
	}
	out := make([]any, 0, len(items))
	for _, w := range items {
		out = append(out, projectWorker(w))
	}
	return QueryResult{Resource: QueryWorkers, Items: out}, nil
}

func (s *Service) queryIssues(ctx context.Context, f QueryFilter) (QueryResult, error) {
	// v2.7 #125: query-issues reads pm_issues. by-project → ListByProject (+ in-mem
	// status filter); by-status → FindByStatuses([status]); default → the
	// non-terminal set {open,in_progress,reopened} (was discussion StatusOpen).
	// --opener is a faithful repoint to created_by (in-memory filter), NOT a drop:
	// created_by is the successor of OpenedByIdentityID (the field exists).
	if s.deps.PMIssues == nil {
		return QueryResult{}, errors.New("query: pm issues repo not wired")
	}
	limit := applyDefaultLimit(f.Limit)
	var items []*pm.Issue
	var err error
	switch {
	case f.ProjectID != "":
		items, err = s.deps.PMIssues.ListByProject(ctx, pm.ProjectID(f.ProjectID))
	case f.Status != "":
		items, err = s.deps.PMIssues.FindByStatuses(ctx, []pm.IssueStatus{pm.IssueStatus(f.Status)}, limit)
	default:
		items, err = s.deps.PMIssues.FindByStatuses(ctx, fleetPendingIssueStatuses, limit)
	}
	if err != nil {
		return QueryResult{}, err
	}
	out := make([]any, 0, len(items))
	for _, i := range items {
		// ListByProject returns all statuses → honor an explicit --status filter.
		if f.ProjectID != "" && f.Status != "" && string(i.Status()) != f.Status {
			continue
		}
		// --opener → created_by (faithful repoint, in-memory filter).
		if f.Opener != "" && string(i.CreatedBy()) != f.Opener {
			continue
		}
		out = append(out, projectIssueRow(i))
	}
	return QueryResult{Resource: QueryIssues, Items: out}, nil
}

func (s *Service) queryEvents(ctx context.Context, f QueryFilter) (QueryResult, error) {
	if s.deps.Events == nil {
		return QueryResult{}, errors.New("query: events repo not wired")
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > observability.MaxEventQueryLimit {
		return QueryResult{}, observability.ErrEventQueryLimitTooLarge
	}
	filter := observability.EventQueryFilter{
		Limit: limit,
		Since: f.Since,
		Until: f.Until,
	}
	if f.EventType != "" {
		if strings.HasSuffix(f.EventType, ".") {
			p := f.EventType
			filter.EventTypePrefix = &p
		} else {
			t := observability.EventType(f.EventType)
			filter.EventType = &t
		}
	}
	if f.Actor != "" {
		actor := f.Actor
		filter.Actor = &actor
	}
	if f.CorrelationID != "" {
		c := f.CorrelationID
		filter.CorrelationID = &c
	}
	if f.DecisionID != "" {
		d := f.DecisionID
		filter.DecisionID = &d
	}
	if f.TaskID != "" {
		filter.Refs.TaskID = f.TaskID
	}
	if f.ExecutionID != "" {
		filter.Refs.ExecutionID = f.ExecutionID
	}
	if f.WorkerID != "" {
		filter.Refs.WorkerID = f.WorkerID
	}
	if f.IssueID != "" {
		filter.Refs.IssueID = f.IssueID
	}
	if f.Cursor != "" {
		c := observability.EventID(f.Cursor)
		filter.Cursor = &c
	}
	evs, err := s.deps.Events.Find(ctx, filter)
	if err != nil {
		return QueryResult{}, err
	}
	out := make([]any, 0, len(evs))
	for _, e := range evs {
		out = append(out, projectEventFull(e))
	}
	result := QueryResult{Resource: QueryEvents, Items: out}
	if len(evs) == limit {
		result.NextCursor = string(evs[len(evs)-1].ID())
	}
	return result, nil
}
