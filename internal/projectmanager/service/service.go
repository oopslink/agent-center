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
)

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
	outbox       outbox.Repository
	idgen        idgen.Generator
	clock        clock.Clock
}

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
	Outbox       outbox.Repository
	IDGen        idgen.Generator
	Clock        clock.Clock
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
		codeRepoRefs: d.CodeRepoRefs, outbox: d.Outbox, idgen: d.IDGen, clock: clk,
	}
}

// taskEventPayload is the JSON payload for task subscriber-affecting events.
// It carries the new EFFECTIVE subscriber set so the B2-b projector can
// overwrite the Conversation participants idempotently (set semantics) and
// (for created) create the Conversation by owner_ref.
type taskEventPayload struct {
	TaskID               string   `json:"task_id"`
	ProjectID            string   `json:"project_id"`
	OwnerRef             string   `json:"owner_ref"` // pm://tasks/{id}
	EffectiveSubscribers []string `json:"effective_subscribers"`
	Assignee             string   `json:"assignee,omitempty"`
	PreviousAssignee     string   `json:"previous_assignee,omitempty"`
	Status               string   `json:"status,omitempty"`
	Reason               string   `json:"reason,omitempty"`
}

type issueEventPayload struct {
	IssueID              string   `json:"issue_id"`
	ProjectID            string   `json:"project_id"`
	OwnerRef             string   `json:"owner_ref"` // pm://issues/{id}
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

func (s *Service) requireProjectMember(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef) error {
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
