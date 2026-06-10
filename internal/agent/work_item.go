package agent

import (
	"context"
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
	// WorkItemPaused (v2.8.1 #278 D PR4 scheduling autonomy): the agent paused an
	// active task to switch to another. paused RELEASES the single-active slot (it
	// is NOT in the single-active set — see availability/idx_awi_agent_active, which
	// stay active|waiting_input only), so the agent can start_work another item.
	// resume_paused_work moves paused→active (re-acquiring the slot, single-active-
	// gated). NOT terminal.
	WorkItemPaused WorkItemStatus = "paused"
)

// IsValid reports enum membership.
func (s WorkItemStatus) IsValid() bool {
	switch s {
	case WorkItemQueued, WorkItemActive, WorkItemWaitingInput,
		WorkItemDone, WorkItemFailed, WorkItemCanceled, WorkItemSuperseded,
		WorkItemPaused:
		return true
	}
	return false
}

// workItemTransitions is the allowed-transition adjacency (plan §2.4 / §10 OQ11).
var workItemTransitions = map[WorkItemStatus][]WorkItemStatus{
	WorkItemQueued:       {WorkItemActive, WorkItemCanceled, WorkItemSuperseded},
	WorkItemActive:       {WorkItemWaitingInput, WorkItemDone, WorkItemFailed, WorkItemCanceled, WorkItemSuperseded, WorkItemPaused},
	WorkItemWaitingInput: {WorkItemActive, WorkItemCanceled, WorkItemSuperseded},
	WorkItemPaused:       {WorkItemActive, WorkItemCanceled, WorkItemSuperseded}, // resume→active / cancel / supersede
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
	// ErrWorkItemReassigned (v2.8.1 #278 D PR4): an optimistic-lock (version CAS)
	// write lost the race — the work item's version moved since the caller loaded
	// it (e.g. the PR5 reconciler released it, or another writer transitioned it),
	// so this agent-facing write (complete/fail/pause/resume) is rejected. The
	// agent handles it gracefully (prompt: "back to step A" — pull fresh). Surfaced
	// as HTTP 409 work_item_reassigned (mirrors ErrAgentHasActiveWork → 409 agent_busy).
	ErrWorkItemReassigned = errors.New("agent: work item reassigned (version conflict)")
)

// WorkItemTransition is a status change recorded on an AgentWorkItem at
// transition time (v2.7 #111 locus B). The persistence adapter drains these on
// Save/Update and the wired sink emits each — within the persisting tx — as an
// agent.work_item_transitioned domain event consumed by the work-item /
// pm-task-status projections + observability stats. PrevStatus is "" for
// creation (queued). Version is the AR version AFTER the transition, used by
// consumers for idempotency / ordering.
type WorkItemTransition struct {
	WorkItemID string
	AgentID    AgentID
	TaskRef    string
	PrevStatus WorkItemStatus
	Status     WorkItemStatus
	Version    int
	OccurredAt time.Time
	// Cause distinguishes WHY a transition happened when the status alone is
	// ambiguous. v2.7 #111 ②: FailFromAgentDeath (B3 circuit-break) sets
	// WorkItemCauseAgentDeath so a consumer can tell a B3 agent-death failure
	// apart from an L2 single-turn failure — both are active→failed and otherwise
	// indistinguishable. Empty for ordinary transitions.
	Cause string
}

// WorkItemCauseAgentDeath marks a transition caused by the B3 agent-death
// circuit-break cascade (FailFromAgentDeath). Consumed by the pm-task-status
// sync to drive task→blocked only on agent-death, never on an L2 single-turn
// failure.
const WorkItemCauseAgentDeath = "agent_death"

// WorkItemTransitionSink receives transitions drained by the WorkItem repository
// after a successful row write, for same-tx emission. Defined here (domain) so
// the persistence adapter need not depend on the outbox package; the concrete
// impl is wired at the composition root.
type WorkItemTransitionSink interface {
	AppendTransitions(ctx context.Context, transitions []WorkItemTransition) error
}

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

	// pending holds transitions recorded since the last DrainTransitions, for
	// same-tx emission by the repository. Never persisted/rehydrated.
	pending []WorkItemTransition
}

// recordTransition appends a transition VO capturing the AR's current identity +
// post-transition status/version/timestamp. prev is the status before the move.
func (w *AgentWorkItem) recordTransition(prev WorkItemStatus, cause string) {
	w.pending = append(w.pending, WorkItemTransition{
		WorkItemID: w.id,
		AgentID:    w.agentID,
		TaskRef:    w.taskRef,
		PrevStatus: prev,
		Status:     w.status,
		Version:    w.version,
		OccurredAt: w.updatedAt,
		Cause:      cause,
	})
}

// DrainTransitions returns the transitions recorded since the last drain and
// clears the buffer. The repository calls this on Save/Update and emits the
// result in the persisting tx. Returns nil when there is nothing pending.
func (w *AgentWorkItem) DrainTransitions() []WorkItemTransition {
	if len(w.pending) == 0 {
		return nil
	}
	out := w.pending
	w.pending = nil
	return out
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
	w := &AgentWorkItem{
		id: in.ID, agentID: in.AgentID, taskRef: in.TaskRef,
		status: WorkItemQueued, createdAt: at, updatedAt: at, version: 1,
	}
	// Creation is a transition too (""→queued) so a freshly-enqueued work item is
	// visible in fleet/projections before its first activation (#111 口径).
	w.recordTransition("", "")
	return w, nil
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

// Pause moves active→paused (v2.8.1 #278 D PR4 scheduling autonomy): the agent
// sets the current task aside to switch to another. It RELEASES the single-active
// slot (paused is not in the single-active set), so the agent may then start_work
// another item. No new interaction (the task is suspended, not advanced).
func (w *AgentWorkItem) Pause(at time.Time) error { return w.move(WorkItemPaused, at) }

// Resume moves paused→active, re-acquiring the single-active slot and beginning a
// NEW AgentInteraction on the same WorkItem (mirrors Wake). Single-active is
// enforced at the service layer (resume_paused_work, like start_work) + the DB
// UNIQUE backstop.
func (w *AgentWorkItem) Resume(at time.Time) error {
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
	if w.status != WorkItemActive && w.status != WorkItemWaitingInput && w.status != WorkItemPaused {
		return ErrWorkItemIllegalMove // not in flight (e.g. queued)
	}
	prev := w.status
	w.status = WorkItemFailed
	if at.IsZero() {
		at = time.Now()
	}
	w.updatedAt = at.UTC()
	w.version++
	w.recordTransition(prev, WorkItemCauseAgentDeath)
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
	prev := w.status
	w.status = to
	if at.IsZero() {
		at = time.Now()
	}
	w.updatedAt = at.UTC()
	w.version++
	w.recordTransition(prev, "")
	return nil
}
