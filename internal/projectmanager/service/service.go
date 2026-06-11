// Package service hosts the ProjectManager AppServices (v2.7 B2, ADR-0046 /
// ADR-0052). Every AppService writes ONLY ProjectManager state + an outbox
// event in ONE local transaction (OQ1 = outbox-now purity): creating the task
// Conversation, syncing ConversationParticipant, and enqueuing AgentWorkItems
// are CROSS-BC effects handled by idempotent outbox projectors (B2-b / B2-c),
// never inline in the producer transaction. PM is thus fully decoupled from
// Conversation and Agent.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// Outbox event types (the OQ1 cross-BC producer set, ADR-0052 §3).
const (
	EvtProjectCreated    = "pm.project.created"
	EvtMemberAdded       = "pm.member.added"
	EvtMemberRemoved     = "pm.member.removed"
	EvtIssueCreated      = "pm.issue.created"
	EvtIssueStateChanged = "pm.issue.state_changed"
	EvtIssueSubsChanged  = "pm.issue.subscribers_changed"
	EvtTaskCreated       = "pm.task.created"
	EvtTaskAssigned      = "pm.task.assigned"
	EvtTaskReassigned    = "pm.task.reassigned"
	EvtTaskStateChanged  = "pm.task.state_changed"
	EvtTaskSubsChanged   = "pm.task.subscribers_changed"
	// v2.9 plan orchestration (#284). EvtPlanCreated drives the Plan↔Conversation
	// 1:1 create (owner_ref pm://plans/{id}); EvtPlanParticipantsChanged drives
	// the ADDITIVE participant sync (§9.5) when a task is selected into a Plan or
	// a plan-task's assignee changes.
	EvtPlanCreated             = "pm.plan.created"
	EvtPlanParticipantsChanged = "pm.plan.participants_changed"
	// v2.9 P2-1 auto-advance: EvtPlanStarted is emitted by StartPlan after the
	// draft→running transition. The PlanOrchestratorProjector consumes it to
	// dispatch the Plan's INITIAL ready nodes (those with no upstream) — no
	// manual Advance click. Payload mirrors planEventPayload (PlanID + ProjectID
	// + OrganizationID).
	EvtPlanStarted = "pm.plan.started"
)

// AgentDirectory resolves an agent's owning Organization (v2.7 D2 b2/d-i, #5a,
// ADR-0049/0052/OQ6). It is an OPTIONAL dependency of the pm Service: when wired
// (non-nil) AssignTask grants an assignee agent project membership so it can pass
// the project write-gate (OQ4 gives agents project-level write). The agentID is
// the bare id (the `agent:` prefix already stripped by the caller).
type AgentDirectory interface {
	OrgOfAgent(ctx context.Context, agentID string) (orgID string, err error)
}

// Service is the ProjectManager AppService facade.
type Service struct {
	db           *sql.DB
	projects     pm.ProjectRepository
	members      pm.ProjectMemberRepository
	issues       pm.IssueRepository
	tasks        pm.TaskRepository
	taskSubs     pm.TaskSubscriberRepository
	issueSubs    pm.IssueSubscriberRepository
	codeRepoRefs pm.CodeRepoRefRepository
	// plans is OPTIONAL (nil-safe, v2.9 #284). nil ⇒ the Plan AppServices
	// (CreatePlan / SelectTaskIntoPlan / RemoveTaskFromPlan) are unavailable;
	// pre-#284 service constructions keep working unchanged.
	plans  pm.PlanRepository
	outbox outbox.Repository
	idgen  idgen.Generator
	clock  clock.Clock
	// agentDir is OPTIONAL (nil-safe). nil ⇒ AssignTask skips the
	// agent-membership step entirely (preserves pre-#5a behavior).
	agentDir AgentDirectory
	// orgSeq is OPTIONAL (nil-safe, v2.7.1 #245). nil ⇒ CreateTask/CreateIssue
	// skip org-number allocation (org_number stays 0, org_ref omitted) — keeps
	// pre-#245 service constructions (tests) working unchanged.
	orgSeq pm.OrgSequenceRepository
	// planDispatcher is OPTIONAL (nil-safe, v2.9 #285). nil ⇒ AdvancePlan returns
	// ErrDispatcherUnavailable (fail-loud — a missing dispatcher must not silently
	// no-op the @mention). Posts the node-ready @mention into the Plan conversation.
	planDispatcher PlanDispatcher
}

// ErrDispatcherUnavailable is returned by AdvancePlan when no PlanDispatcher is
// wired (s.planDispatcher == nil) — fail-loud, mirroring ErrPlansUnavailable.
var ErrDispatcherUnavailable = errors.New("projectmanager: plan dispatcher unavailable — advance cannot post @mentions")

// Deps bundles the Service dependencies.
type Deps struct {
	DB           *sql.DB
	Projects     pm.ProjectRepository
	Members      pm.ProjectMemberRepository
	Issues       pm.IssueRepository
	Tasks        pm.TaskRepository
	TaskSubs     pm.TaskSubscriberRepository
	IssueSubs    pm.IssueSubscriberRepository
	CodeRepoRefs pm.CodeRepoRefRepository
	// Plans is OPTIONAL (v2.9 #284): when set, the Plan AppServices are available.
	// nil ⇒ CreatePlan/SelectTaskIntoPlan/RemoveTaskFromPlan are unavailable.
	Plans  pm.PlanRepository
	Outbox outbox.Repository
	IDGen  idgen.Generator
	Clock  clock.Clock
	// AgentDir is OPTIONAL: when set, AssignTask grants an assignee agent
	// project membership (cross-org-guarded). When nil, that step is skipped.
	AgentDir AgentDirectory
	// OrgSeq is OPTIONAL (v2.7.1 #245): when set, CreateTask/CreateIssue allocate
	// a per-org T<n>/I<n> number. nil ⇒ allocation skipped (org_number 0).
	OrgSeq pm.OrgSequenceRepository
	// PlanDispatcher is OPTIONAL (v2.9 #285): when set, AdvancePlan posts the
	// node-ready @mention into the Plan conversation. nil ⇒ AdvancePlan unavailable.
	PlanDispatcher PlanDispatcher
}

// New constructs the Service.
func New(d Deps) *Service {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &Service{
		db: d.DB, projects: d.Projects, members: d.Members, issues: d.Issues,
		tasks: d.Tasks, taskSubs: d.TaskSubs, issueSubs: d.IssueSubs,
		codeRepoRefs: d.CodeRepoRefs, plans: d.Plans, outbox: d.Outbox, idgen: d.IDGen, clock: clk,
		agentDir: d.AgentDir, orgSeq: d.OrgSeq, planDispatcher: d.PlanDispatcher,
	}
}

// taskEventPayload is the JSON payload for task subscriber-affecting events.
// It carries the new EFFECTIVE subscriber set so the B2-b projector can
// overwrite the Conversation participants idempotently (set semantics) and
// (for created) create the Conversation by owner_ref.
type taskEventPayload struct {
	TaskID               string   `json:"task_id"`
	ProjectID            string   `json:"project_id"`
	OrganizationID       string   `json:"organization_id"` // the project's org — the participant projector stamps it onto the task Conversation so org-scoped endpoints (incl. human reply → agent wake) resolve it (v2.7 GATE-4 fix)
	OwnerRef             string   `json:"owner_ref"`       // pm://tasks/{id}
	EffectiveSubscribers []string `json:"effective_subscribers"`
	Assignee             string   `json:"assignee,omitempty"`
	PreviousAssignee     string   `json:"previous_assignee,omitempty"`
	Status               string   `json:"status,omitempty"`
	Reason               string   `json:"reason,omitempty"`
}

// planEventPayload is the JSON payload for Plan participant-affecting events
// (v2.9 #284). It carries enough for the participant projector to (a) create the
// Plan's 1:1 Conversation by owner_ref on EvtPlanCreated and (b) ADDITIVELY add
// participants on EvtPlanCreated / EvtPlanParticipantsChanged.
//
// Unlike taskEventPayload (which carries the full EFFECTIVE set for overwrite
// semantics), Participants here is an ADD-ONLY delta: the projector unions it
// into the conversation's existing participants and NEVER removes anyone (§9.5 —
// preserve history access, don't yank mid-plan). The creator rides EvtPlanCreated
// (always a participant); each selected task's current assignee rides a
// subsequent EvtPlanParticipantsChanged.
type planEventPayload struct {
	PlanID         string   `json:"plan_id"`
	ProjectID      string   `json:"project_id"`
	OrganizationID string   `json:"organization_id"` // the project's org — stamped onto the Plan Conversation so org-scoped endpoints (incl. agent wake via @mention) resolve it
	OwnerRef       string   `json:"owner_ref"`       // pm://plans/{id}
	CreatorRef     string   `json:"creator_ref,omitempty"`
	Participants   []string `json:"participants"` // ADD-ONLY (additive §9.5); unioned into existing, never removed
}

type issueEventPayload struct {
	IssueID              string   `json:"issue_id"`
	ProjectID            string   `json:"project_id"`
	OrganizationID       string   `json:"organization_id"` // the project's org — stamped onto the issue Conversation (same org-scoping fix as tasks)
	OwnerRef             string   `json:"owner_ref"`       // pm://issues/{id}
	EffectiveSubscribers []string `json:"effective_subscribers"`
	Status               string   `json:"status,omitempty"`
}

// emit appends an outbox event inside the current transaction. Producer
// AppServices call this within RunInTx so the PM state write + event commit
// atomically (OQ1).
func (s *Service) emit(ctx context.Context, eventType, refs string, payload any) error {
	pb, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.outbox.Append(ctx, outbox.Event{
		ID:        s.idgen.NewULID(),
		EventType: eventType,
		Refs:      refs,
		Payload:   string(pb),
		CreatedAt: s.clock.Now(),
	})
}

func refsJSON(kv map[string]string) string {
	b, _ := json.Marshal(kv)
	return string(b)
}

// EffectiveTaskSubscribers computes the effective subscriber set for a Task
// (ADR-0052 §1): {creator} ∪ {current assignee} ∪ {manual subscriber rows}.
// creator/assignee are DERIVED here (not stored as rows), so they can never be
// unsubscribed while they hold that role.
func EffectiveTaskSubscribers(t *pm.Task, manual []*pm.TaskSubscriber) []string {
	set := map[string]struct{}{string(t.CreatedBy()): {}}
	if a := string(t.Assignee()); a != "" {
		set[a] = struct{}{}
	}
	for _, m := range manual {
		set[string(m.IdentityID())] = struct{}{}
	}
	return sortedKeys(set)
}

// EffectiveIssueSubscribers computes the effective subscriber set for an Issue:
// {creator} ∪ {manual subscriber rows} (issues have no assignee).
func EffectiveIssueSubscribers(i *pm.Issue, manual []*pm.IssueSubscriber) []string {
	set := map[string]struct{}{string(i.CreatedBy()): {}}
	for _, m := range manual {
		set[string(m.IdentityID())] = struct{}{}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	// deterministic order (insertion-independent) for stable payloads/tests.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// requireProjectMember is the minimum write-gate (OQ6): the actor must be a
// member of the project to write in it. ErrNotMember on failure.
var ErrNotMember = errors.New("projectmanager: actor is not a member of this project")

// v2.7 #207/#208 (RemoveProjectMember): owner-only authz + owner-protection.
var (
	// ErrNotOwner is returned when a non-owner member attempts an owner-only
	// action (e.g. removing a project member). Maps to 403.
	ErrNotOwner = errors.New("projectmanager: actor must be a project owner")
	// ErrCannotRemoveOwner blocks removing an owner member so a project always
	// retains an owner. Maps to 409.
	ErrCannotRemoveOwner = errors.New("projectmanager: cannot remove an owner member")
)

func (s *Service) requireProjectMember(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef) error {
	// v2.7.1 #239: distinguish "project does not exist" from "not a member" — a
	// membership miss alone can't tell them apart, so a bad/guessed project id
	// surfaced the misleading ErrNotMember (@oopslink screenshot pain). Check
	// existence FIRST: a missing project yields ErrProjectNotFound (404), and
	// ErrNotMember (403) is reserved for a project that exists but the actor isn't
	// a member of.
	if _, err := s.projects.FindByID(ctx, projectID); err != nil {
		return err // pm.ErrProjectNotFound when missing
	}
	if _, err := s.members.FindByProjectAndIdentity(ctx, projectID, actor); err != nil {
		if errors.Is(err, pm.ErrMemberNotFound) {
			return ErrNotMember
		}
		return err
	}
	return nil
}

// runInTx is a thin wrapper so AppServices read clearly.
func (s *Service) runInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return persistence.RunInTx(ctx, s.db, fn)
}
