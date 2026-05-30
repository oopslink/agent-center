package agent

import (
	"errors"
	"strings"
	"time"
)

// AgentWorkItem is a logical work-queue item assigned to an Agent — NOT a
// process or run (ADR-0049 §2/§3). It references a Task by id/URI but does not
// own Task state. A Task→Agent assignment creates a WorkItem; reassignment
// supersedes the old one and creates a new one, while the Task keeps ONE stable
// Conversation (ADR-0047). A WorkItem may span multiple AgentInteractions
// (logical turns) — the wait→wake loop is waiting_input→active, repeatable;
// interaction detail lives in AgentActivityEvent (plan §2.4).
//
// C2 ships the AR + state machine + repo. The outbox-driven
// AssignTask→EnqueueWorkItem projector wiring is added once ProjectManager's
// AssignTask producer (B2) lands.

// WorkItemStatus enum + state machine (plan §2.4, simplified per §10 OQ11 /
// ADR-0049 — there is NO `blocked` status on the WorkItem: "blocked" is a Task
// (business) concept only; when a Task is blocked the live WorkItem is
// CANCELED, not blocked):
//
//	queued → active → waiting_input → active
//	active → done
//	active → failed
//	queued/active/waiting_input → canceled    (Task blocked / Task canceled / terminate)
//	queued/active/waiting_input → superseded  (reassignment only)
type WorkItemStatus string

const (
	WorkItemQueued       WorkItemStatus = "queued"
	WorkItemActive       WorkItemStatus = "active"
	WorkItemWaitingInput WorkItemStatus = "waiting_input"
	WorkItemDone         WorkItemStatus = "done"
	WorkItemFailed       WorkItemStatus = "failed"
	WorkItemCanceled     WorkItemStatus = "canceled"
	WorkItemSuperseded   WorkItemStatus = "superseded"
)

// IsValid reports enum membership.
func (s WorkItemStatus) IsValid() bool {
	switch s {
	case WorkItemQueued, WorkItemActive, WorkItemWaitingInput,
		WorkItemDone, WorkItemFailed, WorkItemCanceled, WorkItemSuperseded:
		return true
	}
	return false
}

// workItemTransitions is the allowed-transition adjacency (plan §2.4 / §10 OQ11).
var workItemTransitions = map[WorkItemStatus][]WorkItemStatus{
	WorkItemQueued:       {WorkItemActive, WorkItemCanceled, WorkItemSuperseded},
	WorkItemActive:       {WorkItemWaitingInput, WorkItemDone, WorkItemFailed, WorkItemCanceled, WorkItemSuperseded},
	WorkItemWaitingInput: {WorkItemActive, WorkItemCanceled, WorkItemSuperseded},
	WorkItemDone:         {},
	WorkItemFailed:       {},
	WorkItemCanceled:     {},
	WorkItemSuperseded:   {},
}

// CanTransitionTo reports whether from→to is a legal WorkItem transition.
func (s WorkItemStatus) CanTransitionTo(to WorkItemStatus) bool {
	for _, n := range workItemTransitions[s] {
		if n == to {
			return true
		}
	}
	return false
}

// IsTerminal reports whether the WorkItem can no longer transition.
func (s WorkItemStatus) IsTerminal() bool {
	switch s {
	case WorkItemDone, WorkItemFailed, WorkItemCanceled, WorkItemSuperseded:
		return true
	}
	return false
}

// ErrWorkItem* sentinels.
var (
	ErrWorkItemNotFound      = errors.New("agent: work item not found")
	ErrWorkItemExists        = errors.New("agent: work item already exists")
	ErrWorkItemBadStatus     = errors.New("agent: invalid work item status")
	ErrWorkItemIllegalMove   = errors.New("agent: illegal work item transition")
	ErrWorkItemTaskRequired  = errors.New("agent: work item must reference a task")
	ErrWorkItemAgentRequired = errors.New("agent: work item must reference an agent")
)

// AgentWorkItem AR.
type AgentWorkItem struct {
	id           string
	agentID      AgentID
	taskRef      string // Task id/URI — referenced, NOT owned
	status       WorkItemStatus
	interactions int // count of active entries (initial activate + each wake)
	createdAt    time.Time
	updatedAt    time.Time
	version      int
}

// NewWorkItemInput captures constructor args.
type NewWorkItemInput struct {
	ID        string
	AgentID   AgentID
	TaskRef   string
	CreatedAt time.Time
}

// NewWorkItem constructs a queued work item.
func NewWorkItem(in NewWorkItemInput) (*AgentWorkItem, error) {
	if strings.TrimSpace(in.ID) == "" {
		return nil, errors.New("agent: work item id required")
	}
	if strings.TrimSpace(string(in.AgentID)) == "" {
		return nil, ErrWorkItemAgentRequired
	}
	if strings.TrimSpace(in.TaskRef) == "" {
		return nil, ErrWorkItemTaskRequired
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("agent: created_at required")
	}
	at := in.CreatedAt.UTC()
	return &AgentWorkItem{
		id: in.ID, agentID: in.AgentID, taskRef: in.TaskRef,
		status: WorkItemQueued, createdAt: at, updatedAt: at, version: 1,
	}, nil
}

// RehydrateWorkItemInput is for repository round-trip.
type RehydrateWorkItemInput struct {
	ID           string
	AgentID      AgentID
	TaskRef      string
	Status       WorkItemStatus
	Interactions int
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Version      int
}

// RehydrateWorkItem reconstructs without invariant checks.
func RehydrateWorkItem(in RehydrateWorkItemInput) (*AgentWorkItem, error) {
	if !in.Status.IsValid() {
		return nil, ErrWorkItemBadStatus
	}
	if in.Version < 1 {
		return nil, errors.New("agent: version must be >= 1")
	}
	return &AgentWorkItem{
		id: in.ID, agentID: in.AgentID, taskRef: in.TaskRef, status: in.Status,
		interactions: in.Interactions, createdAt: in.CreatedAt.UTC(), updatedAt: in.UpdatedAt.UTC(), version: in.Version,
	}, nil
}

// Getters.
func (w *AgentWorkItem) ID() string             { return w.id }
func (w *AgentWorkItem) AgentID() AgentID       { return w.agentID }
func (w *AgentWorkItem) TaskRef() string        { return w.taskRef }
func (w *AgentWorkItem) Status() WorkItemStatus { return w.status }
func (w *AgentWorkItem) Interactions() int      { return w.interactions }
func (w *AgentWorkItem) CreatedAt() time.Time   { return w.createdAt }
func (w *AgentWorkItem) UpdatedAt() time.Time   { return w.updatedAt }
func (w *AgentWorkItem) Version() int           { return w.version }

// Activate moves queued→active, beginning the first AgentInteraction.
func (w *AgentWorkItem) Activate(at time.Time) error {
	if err := w.move(WorkItemActive, at); err != nil {
		return err
	}
	w.interactions++
	return nil
}

// WaitInput moves active→waiting_input (the agent asked for input; the turn
// ends and the process idles — plan §2.6).
func (w *AgentWorkItem) WaitInput(at time.Time) error { return w.move(WorkItemWaitingInput, at) }

// Wake moves waiting_input→active when a reply arrives, beginning a NEW
// AgentInteraction on the same WorkItem (plan §2.4/§10 OQ5).
func (w *AgentWorkItem) Wake(at time.Time) error {
	if err := w.move(WorkItemActive, at); err != nil {
		return err
	}
	w.interactions++
	return nil
}

// Done / Fail terminate the WorkItem.
func (w *AgentWorkItem) Done(at time.Time) error { return w.move(WorkItemDone, at) }
func (w *AgentWorkItem) Fail(at time.Time) error { return w.move(WorkItemFailed, at) }

// FailFromAgentDeath terminates an IN-FLIGHT WorkItem (active or waiting_input)
// because its owning agent hit the terminal crash-loop circuit-breaker (v2.7 GATE-7
// Mode-B: self-heal relaunch cap exhausted → the agent is not auto-relaunched, so
// the work cannot continue).
//
// This is the SOLE path that may move waiting_input→failed. The general transition
// map (workItemTransitions, used by Fail()/move()) deliberately does NOT allow
// waiting_input→failed, so the normal worker feedback (MarkWorkItemState "failed")
// on a waiting_input WI still returns ErrWorkItemIllegalMove. The terminal edge is
// therefore reachable ONLY via the agent-death cascade (MarkAgentFailed) — a
// structural guard, by construction, not a behavioral coincidence. Idempotent: a
// no-op (nil) if the WorkItem is already terminal; illegal from queued (not in
// flight). Failure cause is traceable via the owning agent's lifecycleError.
func (w *AgentWorkItem) FailFromAgentDeath(at time.Time) error {
	if w.status.IsTerminal() {
		return nil // already done/failed/canceled/superseded — nothing to cascade
	}
	if w.status != WorkItemActive && w.status != WorkItemWaitingInput {
		return ErrWorkItemIllegalMove // not in flight (e.g. queued)
	}
	w.status = WorkItemFailed
	if at.IsZero() {
		at = time.Now()
	}
	w.updatedAt = at.UTC()
	w.version++
	return nil
}

// Cancel terminates a non-terminal WorkItem.
func (w *AgentWorkItem) Cancel(at time.Time) error { return w.move(WorkItemCanceled, at) }

// Supersede marks the WorkItem superseded — used on reassignment (the new
// AgentWorkItem is created separately by the AppService).
func (w *AgentWorkItem) Supersede(at time.Time) error { return w.move(WorkItemSuperseded, at) }

func (w *AgentWorkItem) move(to WorkItemStatus, at time.Time) error {
	if !to.IsValid() {
		return ErrWorkItemBadStatus
	}
	if !w.status.CanTransitionTo(to) {
		return ErrWorkItemIllegalMove
	}
	w.status = to
	if at.IsZero() {
		at = time.Now()
	}
	w.updatedAt = at.UTC()
	w.version++
	return nil
}
