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
	"github.com/oopslink/agent-center/internal/observability/projection"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

// Deps bundles the read-side repos QueryService needs. Every field is an
// interface, so tests can inject fakes; production wires SQLite impls via
// NewService.
type Deps struct {
	Events observability.EventRepository
	// v2.7 #107 Phase-2 (fleet repoint): new-model read deps. WorkItemProjections
	// is the fleet data source (agent_work_item_projections); WorkItems resolves
	// a work item's task_ref; PMTasks resolves task_ref→project; PMProjects
	// resolves project→org for the work-items segment's org-scoping (same pm
	// source as PMTasks, so org-scope no longer mixes the retired workforce
	// project model with the pm project model).
	WorkItemProjections projection.AgentWorkItemProjectionRepository
	WorkItems           agentpkg.WorkItemRepository
	PMTasks             pm.TaskRepository
	PMProjects          pm.ProjectRepository
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
	// work-items sub-section: the agent work items for this pm task (across
	// reassignments), resolved by task_ref "pm://tasks/{id}". Fulfills the
	// section proj-A deferred until inspectTask read the pm model.
	if s.deps.WorkItems != nil {
		wis, _ := s.deps.WorkItems.ListByTask(ctx, "pm://tasks/"+string(t.ID()))
		items := make([]any, 0, len(wis))
		for _, wi := range wis {
			items = append(items, projectWorkItemSummary(wi))
		}
		out["work_items"] = items
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
	// v2.7 #107 Phase-2 (proj-A): "execution" inspect repointed to the agent
	// work-item model. The id is a work-item id; rich activity/token detail comes
	// from the work-item projection (same source as fleet/stats). artifacts段
	// dropped (artifact is execution-keyed, no work-item equivalent — restored
	// work-item-native in the taskruntime carve-out slice). recent_events filter
	// by work_item_id (precise WI lifecycle incl transitions).
	if s.deps.WorkItems == nil {
		return InspectResult{}, errors.New("query: work items repo not wired")
	}
	wi, err := s.deps.WorkItems.FindByID(ctx, id)
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	taskID, _ := fleetTaskIDFromRef(wi.TaskRef())
	out := map[string]any{
		"work_item_id": wi.ID(),
		"agent_id":     string(wi.AgentID()),
		"task_id":      taskID,
		"status":       string(wi.Status()),
		"interactions": wi.Interactions(),
		"created_at":   wi.CreatedAt().UTC().Format(time.RFC3339Nano),
		"updated_at":   wi.UpdatedAt().UTC().Format(time.RFC3339Nano),
		"version":      wi.Version(),
	}
	if s.deps.WorkItemProjections != nil {
		if p, perr := s.deps.WorkItemProjections.FindByID(ctx, id); perr == nil {
			out["projection"] = workItemRowFromProjection(p, taskID)
		}
	}
	if s.deps.Events != nil {
		evs, _ := s.deps.Events.Find(ctx, observability.EventQueryFilter{
			Refs: observability.EventRefsFilter{WorkItemID: id}, Limit: 50,
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
	// v2.7 #107 Phase-2 (proj-A): "what's this worker running" = worker→its agents
	// →their live work items (Q3 MAP; same pm·agent source, no retired
	// task_executions). fail-loud if the deps aren't wired (should-be-wired:
	// missing injection must error, not nil-panic — mirrors other repo guards).
	if s.deps.Agents == nil || s.deps.WorkItems == nil {
		return InspectResult{}, errors.New("query: agents/work-items repo not wired")
	}
	agents, _ := s.deps.Agents.ListByWorker(ctx, string(w.ID()))
	activeWIs := make([]any, 0)
	for _, ag := range agents {
		wis, _ := s.deps.WorkItems.ListByAgent(ctx, ag.ID())
		for _, wi := range wis {
			if wi.Status().IsTerminal() {
				continue // active = non-terminal (queued/active/waiting_input)
			}
			activeWIs = append(activeWIs, projectWorkItemSummary(wi))
		}
	}
	out["active_work_items"] = activeWIs
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
	// v2.7 #107 Phase-2 (proj-A): repointed to the agent work-item model. Rows
	// come from work-item projections (rich activity/token detail, same source as
	// fleet/stats). by-task → WorkItems.ListByTask; by-worker → worker→agents→
	// ListByAgent (Q3); status/active → projections by status set. Labels are
	// work-item status names. The exec-specific FailedReason filter is dropped —
	// "why failed" is observable via `inspect execution <work_item_id>`
	// recent_events (the failed transition's Cause); by-worker-with-status is
	// covered by by-agent.
	if s.deps.WorkItemProjections == nil {
		return QueryResult{}, errors.New("query: work item projections repo not wired")
	}
	rowFor := func(wiID, taskID string) (any, bool) {
		p, perr := s.deps.WorkItemProjections.FindByID(ctx, wiID)
		if perr != nil {
			return nil, false
		}
		return workItemRowFromProjection(p, taskID), true
	}
	out := make([]any, 0)
	switch {
	case f.TaskID != "":
		if s.deps.WorkItems == nil {
			return QueryResult{}, errors.New("query: work items repo not wired")
		}
		wis, err := s.deps.WorkItems.ListByTask(ctx, "pm://tasks/"+f.TaskID)
		if err != nil {
			return QueryResult{}, err
		}
		for _, wi := range wis {
			if row, ok := rowFor(wi.ID(), f.TaskID); ok {
				out = append(out, row)
			}
		}
	case f.WorkerID != "":
		if s.deps.Agents == nil || s.deps.WorkItems == nil {
			return QueryResult{}, errors.New("query: agents/work items repo not wired")
		}
		agents, _ := s.deps.Agents.ListByWorker(ctx, f.WorkerID)
		for _, ag := range agents {
			wis, _ := s.deps.WorkItems.ListByAgent(ctx, ag.ID())
			for _, wi := range wis {
				taskID, _ := fleetTaskIDFromRef(wi.TaskRef())
				if row, ok := rowFor(wi.ID(), taskID); ok {
					out = append(out, row)
				}
			}
		}
	default:
		statuses := []string{
			string(agentpkg.WorkItemQueued),
			string(agentpkg.WorkItemActive),
			string(agentpkg.WorkItemWaitingInput),
		}
		if f.Status != "" && f.Status != "active" {
			statuses = []string{f.Status}
		}
		projs, err := s.deps.WorkItemProjections.List(ctx, projection.AgentWorkItemProjectionFilter{Statuses: statuses})
		if err != nil {
			return QueryResult{}, err
		}
		for _, p := range projs {
			taskID := ""
			if s.deps.WorkItems != nil {
				if wi, werr := s.deps.WorkItems.FindByID(ctx, p.WorkItemID); werr == nil {
					taskID, _ = fleetTaskIDFromRef(wi.TaskRef())
				}
			}
			out = append(out, workItemRowFromProjection(p, taskID))
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
