package query

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/conversation"
	"github.com/oopslink/agent-center/internal/discussion"
	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/observability/projection"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/taskruntime"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
	"github.com/oopslink/agent-center/internal/taskruntime/inputrequest"
	"github.com/oopslink/agent-center/internal/taskruntime/task"
	"github.com/oopslink/agent-center/internal/workforce"
)

// Deps bundles the read-side repos QueryService needs. Every field is an
// interface, so tests can inject fakes; production wires SQLite impls via
// NewService.
type Deps struct {
	Events       observability.EventRepository
	Projection   projection.Repository
	Tasks        task.Repository
	Executions   execution.Repository
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
	Agents agentpkg.Repository
	Artifacts    execution.ArtifactRepository
	InputReqs    inputrequest.Repository
	Issues       discussion.IssueRepository
	Conversations conversation.ConversationRepository
	Messages     conversation.MessageRepository
	Workers      workforce.WorkerRepository
	Mappings     workforce.WorkerProjectMappingRepository
	Proposals    workforce.WorkerProjectProposalRepository
	Projects     workforce.ProjectRepository
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
	case InspectInputRequest:
		return s.inspectInputRequest(ctx, id)
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
	case QueryInputRequests:
		return s.queryInputRequests(ctx, filter)
	case QueryProposals:
		return s.queryProposals(ctx, filter)
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
	if s.deps.Tasks == nil {
		return InspectResult{}, errors.New("query: tasks repo not wired")
	}
	t, err := s.deps.Tasks.FindByID(ctx, taskruntime.TaskID(id))
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	out := map[string]any{
		"id":              string(t.ID()),
		"project_id":      t.ProjectID(),
		"title":           t.Title(),
		"description":     t.Description(),
		"status":          string(t.Status()),
		"priority":        string(t.Priority()),
		"conversation_id": stringOrNil(string(t.ConversationID())),
		"from_issue_id":   stringOrNil(string(t.FromIssueID())),
		"created_at":      t.CreatedAt().UTC().Format(time.RFC3339Nano),
		"updated_at":      t.UpdatedAt().UTC().Format(time.RFC3339Nano),
		"version":         t.Version(),
	}
	// v2.7 #107 Phase-2 (proj-A): old execution reads retired. The task's
	// work-items sub-section is re-added when inspectTask repoints to pm
	// (proj-B: old taskruntime task id ≠ pm task id, so work-items can't be
	// resolved here until then).
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
	if s.deps.Mappings != nil {
		ms, _ := s.deps.Mappings.FindByWorkerID(ctx, w.ID())
		out["mappings"] = projectMappingList(ms)
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
	if s.deps.Issues == nil {
		return InspectResult{}, errors.New("query: issues repo not wired")
	}
	i, err := s.deps.Issues.FindByID(ctx, discussion.IssueID(id))
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	out := map[string]any{
		"id":              string(i.ID()),
		"project_id":      i.ProjectID(),
		"title":           i.Title(),
		"status":          string(i.Status()),
		"opened_by":       i.OpenedByIdentityID(),
		"origin":          string(i.Origin()),
		"opened_at":       i.OpenedAt().UTC().Format(time.RFC3339Nano),
		"conversation_id": stringOrNil(string(i.ConversationID())),
		"version":         i.Version(),
	}
	if s.deps.Events != nil {
		evs, _ := s.deps.Events.Find(ctx, observability.EventQueryFilter{
			Refs: observability.EventRefsFilter{IssueID: id}, Limit: 50,
		})
		out["recent_events"] = projectEventSummaryList(evs)
	}
	if s.deps.Conversations != nil && i.ConversationID() != "" && s.deps.Messages != nil {
		msgs, _ := s.deps.Messages.FindByConversationID(ctx, i.ConversationID(), conversation.MessageFilter{Limit: 50})
		out["messages"] = projectMessageList(msgs)
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

func (s *Service) inspectInputRequest(ctx context.Context, id string) (InspectResult, error) {
	if s.deps.InputReqs == nil {
		return InspectResult{}, errors.New("query: input_requests repo not wired")
	}
	ir, err := s.deps.InputReqs.FindByID(ctx, taskruntime.InputRequestID(id))
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	out := map[string]any{
		"id":                string(ir.ID()),
		"task_execution_id": string(ir.TaskExecutionID()),
		"status":            string(ir.Status()),
		"question":          ir.Question(),
		"urgency":           string(ir.Urgency()),
		"requested_at":      ir.RequestedAt().UTC().Format(time.RFC3339Nano),
		"version":           ir.Version(),
	}
	if s.deps.Events != nil {
		evs, _ := s.deps.Events.Find(ctx, observability.EventQueryFilter{
			Refs: observability.EventRefsFilter{InputRequestID: id}, Limit: 50,
		})
		out["recent_events"] = projectEventSummaryList(evs)
	}
	return InspectResult{Kind: InspectInputRequest, ID: id, Data: out}, nil
}

func (s *Service) inspectProject(ctx context.Context, id string) (InspectResult, error) {
	if s.deps.Projects == nil {
		return InspectResult{}, errors.New("query: projects repo not wired")
	}
	p, err := s.deps.Projects.FindByID(ctx, workforce.ProjectID(id))
	if err != nil {
		return InspectResult{}, mapNotFound(err)
	}
	tags := p.Tags()
	if tags == nil {
		tags = []string{}
	}
	out := map[string]any{
		"id":          string(p.ID()),
		"name":        p.Name(),
		"tags":        tags,
		"description": p.Description(),
		"version":     p.Version(),
		"created_at":  p.CreatedAt().UTC().Format(time.RFC3339Nano),
	}
	if s.deps.Mappings != nil {
		ms, _ := s.deps.Mappings.FindByProjectID(ctx, p.ID())
		out["mappings"] = projectMappingList(ms)
	}
	if s.deps.Tasks != nil {
		ts, _ := s.deps.Tasks.FindByProject(ctx, string(p.ID()), task.Filter{Limit: 100})
		out["tasks"] = projectTaskList(ts)
	}
	return InspectResult{Kind: InspectProject, ID: id, Data: out}, nil
}

// ---- Query assemblers ---------------------------------------------------

func (s *Service) queryTasks(ctx context.Context, f QueryFilter) (QueryResult, error) {
	if s.deps.Tasks == nil {
		return QueryResult{}, errors.New("query: tasks repo not wired")
	}
	limit := applyDefaultLimit(f.Limit)
	var items []*task.Task
	var err error
	switch {
	case f.ProjectID != "":
		filter := task.Filter{Limit: limit}
		if f.Status != "" {
			st := task.Status(f.Status)
			filter.Status = &st
		}
		items, err = s.deps.Tasks.FindByProject(ctx, f.ProjectID, filter)
	case f.Status != "":
		items, err = s.deps.Tasks.FindByStatus(ctx, task.Status(f.Status), task.Filter{Limit: limit})
	case f.BlockedBy != "":
		items, err = s.deps.Tasks.FindBlockedBy(ctx, taskruntime.TaskID(f.BlockedBy))
	default:
		items, err = s.deps.Tasks.FindByStatus(ctx, task.StatusOpen, task.Filter{Limit: limit})
	}
	if err != nil {
		return QueryResult{}, err
	}
	// Optional priority post-filter
	if f.Priority != "" {
		var pruned []*task.Task
		for _, t := range items {
			if string(t.Priority()) == f.Priority {
				pruned = append(pruned, t)
			}
		}
		items = pruned
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
	if f.HasMapping != nil && s.deps.Mappings != nil {
		var pruned []*workforce.Worker
		for _, w := range items {
			ms, _ := s.deps.Mappings.FindByWorkerID(ctx, w.ID())
			has := len(ms) > 0
			if has == *f.HasMapping {
				pruned = append(pruned, w)
			}
		}
		items = pruned
	}
	out := make([]any, 0, len(items))
	for _, w := range items {
		out = append(out, projectWorker(w))
	}
	return QueryResult{Resource: QueryWorkers, Items: out}, nil
}

func (s *Service) queryIssues(ctx context.Context, f QueryFilter) (QueryResult, error) {
	if s.deps.Issues == nil {
		return QueryResult{}, errors.New("query: issues repo not wired")
	}
	limit := applyDefaultLimit(f.Limit)
	var items []*discussion.Issue
	var err error
	switch {
	case f.ProjectID != "":
		filter := discussion.IssueFilter{Limit: limit}
		if f.Status != "" {
			st := discussion.Status(f.Status)
			filter.Status = &st
		}
		items, err = s.deps.Issues.FindByProject(ctx, f.ProjectID, filter)
	case f.Opener != "":
		items, err = s.deps.Issues.FindByOpener(ctx, f.Opener)
	case f.Status != "":
		items, err = s.deps.Issues.FindByStatus(ctx, discussion.Status(f.Status), discussion.IssueFilter{Limit: limit})
	default:
		items, err = s.deps.Issues.FindByStatus(ctx, discussion.StatusOpen, discussion.IssueFilter{Limit: limit})
	}
	if err != nil {
		return QueryResult{}, err
	}
	out := make([]any, 0, len(items))
	for _, i := range items {
		out = append(out, projectIssueRow(i))
	}
	return QueryResult{Resource: QueryIssues, Items: out}, nil
}

func (s *Service) queryInputRequests(ctx context.Context, f QueryFilter) (QueryResult, error) {
	if s.deps.InputReqs == nil {
		return QueryResult{}, errors.New("query: input_requests repo not wired")
	}
	// We re-use FindPending(epoch) to cover "all" + the caller can filter
	// by status after.
	items, err := s.deps.InputReqs.FindPending(ctx, time.Now().Add(365*24*time.Hour))
	if err != nil {
		return QueryResult{}, err
	}
	if f.TaskID != "" {
		var pruned []*inputrequest.InputRequest
		for _, ir := range items {
			if string(ir.TaskExecutionID()) == f.TaskID || string(ir.ID()) == f.TaskID {
				pruned = append(pruned, ir)
			}
		}
		items = pruned
	}
	if f.Status != "" {
		var pruned []*inputrequest.InputRequest
		for _, ir := range items {
			if string(ir.Status()) == f.Status {
				pruned = append(pruned, ir)
			}
		}
		items = pruned
	}
	out := make([]any, 0, len(items))
	for _, ir := range items {
		out = append(out, projectInputRequest(ir))
	}
	return QueryResult{Resource: QueryInputRequests, Items: out}, nil
}

func (s *Service) queryProposals(ctx context.Context, f QueryFilter) (QueryResult, error) {
	if s.deps.Proposals == nil {
		return QueryResult{}, errors.New("query: proposals repo not wired")
	}
	var items []*workforce.WorkerProjectProposal
	var err error
	if f.WorkerID != "" {
		if f.Status != "" {
			items, err = s.deps.Proposals.FindByWorkerID(ctx, workforce.WorkerID(f.WorkerID), workforce.ProposalStatus(f.Status))
		} else {
			items, err = s.deps.Proposals.FindByWorkerID(ctx, workforce.WorkerID(f.WorkerID))
		}
	} else {
		items, err = s.deps.Proposals.FindPending(ctx)
		if f.Status != "" && f.Status != "pending" {
			items = nil
		}
	}
	if err != nil {
		return QueryResult{}, err
	}
	out := make([]any, 0, len(items))
	for _, p := range items {
		out = append(out, projectProposal(p))
	}
	return QueryResult{Resource: QueryProposals, Items: out}, nil
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
