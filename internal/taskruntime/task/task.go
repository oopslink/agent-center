package task

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

// Task is the work-unit identity AR (01-task.md).
//
// Invariants per § 10:
//  1. project_id non-empty + immutable
//  2. id immutable
//  3. from_issue_id / parent_task_id immutable
//  4. status transitions are constrained to § 4
//  5. single-active execution is enforced at DispatchService (not here)
//  6. no `failed` status (failure lives on TaskExecution)
//  7. conversation_id only goes null→non-null (no unbind in v1)
type Task struct {
	id                  taskruntime.TaskID
	projectID           string
	parentTaskID        taskruntime.TaskID
	fromIssueID         string
	title               string
	description         string
	descriptionBlobRef  string
	status              Status
	priority            Priority
	etaAt               *time.Time
	requiresWorktree    bool
	dependsOnTaskIDs    []taskruntime.TaskID
	abandonedReason     string
	abandonedMessage    string
	conversationID      string
	currentExecutionID  taskruntime.TaskExecutionID
	createdBy           string
	createdAt           time.Time
	updatedAt           time.Time
	version             int
}

// NewInput captures the constructor arguments.
type NewInput struct {
	ID                 taskruntime.TaskID
	ProjectID          string
	ParentTaskID       taskruntime.TaskID
	FromIssueID        string
	Title              string
	Description        string
	DescriptionBlobRef string
	Priority           Priority
	EtaAt              *time.Time
	RequiresWorktree   bool
	DependsOnTaskIDs   []taskruntime.TaskID
	ConversationID     string
	CreatedBy          string
	Now                time.Time
}

// New constructs a fresh open Task. Returns the validation error if any
// invariant is violated.
func New(in NewInput) (*Task, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("task: id required")
	}
	if strings.TrimSpace(in.ProjectID) == "" {
		return nil, fmt.Errorf("%w: project_id required (conventions § 1: no orphan task)", ErrTaskInvariantViolation)
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("task: title required")
	}
	if strings.TrimSpace(in.CreatedBy) == "" {
		return nil, errors.New("task: created_by required")
	}
	priority := in.Priority
	if priority == "" {
		priority = PriorityMedium
	}
	if !priority.IsValid() {
		return nil, ErrInvalidPriority
	}
	if in.Now.IsZero() {
		return nil, errors.New("task: now required")
	}
	// Validate deps
	if err := validateDeps(in.ID, in.DependsOnTaskIDs); err != nil {
		return nil, err
	}
	now := in.Now.UTC()
	var etaCopy *time.Time
	if in.EtaAt != nil {
		v := in.EtaAt.UTC()
		etaCopy = &v
	}
	return &Task{
		id:                 in.ID,
		projectID:          in.ProjectID,
		parentTaskID:       in.ParentTaskID,
		fromIssueID:        in.FromIssueID,
		title:              in.Title,
		description:        in.Description,
		descriptionBlobRef: in.DescriptionBlobRef,
		status:             StatusOpen,
		priority:           priority,
		etaAt:              etaCopy,
		requiresWorktree:   in.RequiresWorktree,
		dependsOnTaskIDs:   append([]taskruntime.TaskID(nil), in.DependsOnTaskIDs...),
		conversationID:     in.ConversationID,
		createdBy:          in.CreatedBy,
		createdAt:          now,
		updatedAt:          now,
		version:            1,
	}, nil
}

// RehydrateInput is for repository round-trip without invariant checks.
type RehydrateInput struct {
	ID                 taskruntime.TaskID
	ProjectID          string
	ParentTaskID       taskruntime.TaskID
	FromIssueID        string
	Title              string
	Description        string
	DescriptionBlobRef string
	Status             Status
	Priority           Priority
	EtaAt              *time.Time
	RequiresWorktree   bool
	DependsOnTaskIDs   []taskruntime.TaskID
	AbandonedReason    string
	AbandonedMessage   string
	ConversationID     string
	CurrentExecutionID taskruntime.TaskExecutionID
	CreatedBy          string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Version            int
}

// Rehydrate reconstructs without invariant checks (repository use only).
func Rehydrate(in RehydrateInput) (*Task, error) {
	if !in.Status.IsValid() {
		return nil, ErrInvalidStatus
	}
	if !in.Priority.IsValid() {
		return nil, ErrInvalidPriority
	}
	if in.Version < 1 {
		return nil, errors.New("task: version must be >= 1")
	}
	var etaCopy *time.Time
	if in.EtaAt != nil {
		v := in.EtaAt.UTC()
		etaCopy = &v
	}
	return &Task{
		id:                 in.ID,
		projectID:          in.ProjectID,
		parentTaskID:       in.ParentTaskID,
		fromIssueID:        in.FromIssueID,
		title:              in.Title,
		description:        in.Description,
		descriptionBlobRef: in.DescriptionBlobRef,
		status:             in.Status,
		priority:           in.Priority,
		etaAt:              etaCopy,
		requiresWorktree:   in.RequiresWorktree,
		dependsOnTaskIDs:   append([]taskruntime.TaskID(nil), in.DependsOnTaskIDs...),
		abandonedReason:    in.AbandonedReason,
		abandonedMessage:   in.AbandonedMessage,
		conversationID:     in.ConversationID,
		currentExecutionID: in.CurrentExecutionID,
		createdBy:          in.CreatedBy,
		createdAt:          in.CreatedAt.UTC(),
		updatedAt:          in.UpdatedAt.UTC(),
		version:            in.Version,
	}, nil
}

// Getters.
func (t *Task) ID() taskruntime.TaskID                          { return t.id }
func (t *Task) ProjectID() string                                { return t.projectID }
func (t *Task) ParentTaskID() taskruntime.TaskID                 { return t.parentTaskID }
func (t *Task) FromIssueID() string                              { return t.fromIssueID }
func (t *Task) Title() string                                    { return t.title }
func (t *Task) Description() string                              { return t.description }
func (t *Task) DescriptionBlobRef() string                       { return t.descriptionBlobRef }
func (t *Task) Status() Status                                   { return t.status }
func (t *Task) Priority() Priority                               { return t.priority }
func (t *Task) RequiresWorktree() bool                           { return t.requiresWorktree }
func (t *Task) AbandonedReason() string                          { return t.abandonedReason }
func (t *Task) AbandonedMessage() string                         { return t.abandonedMessage }
func (t *Task) ConversationID() string                           { return t.conversationID }
func (t *Task) CurrentExecutionID() taskruntime.TaskExecutionID  { return t.currentExecutionID }
func (t *Task) CreatedBy() string                                { return t.createdBy }
func (t *Task) CreatedAt() time.Time                             { return t.createdAt }
func (t *Task) UpdatedAt() time.Time                             { return t.updatedAt }
func (t *Task) Version() int                                     { return t.version }

// EtaAt returns a copy or nil.
func (t *Task) EtaAt() *time.Time {
	if t.etaAt == nil {
		return nil
	}
	v := t.etaAt.UTC()
	return &v
}

// DependsOnTaskIDs returns a defensive copy.
func (t *Task) DependsOnTaskIDs() []taskruntime.TaskID {
	return append([]taskruntime.TaskID(nil), t.dependsOnTaskIDs...)
}

// IsTerminal returns true for done / abandoned.
func (t *Task) IsTerminal() bool { return t.status.IsTerminal() }

// HasActiveExecution returns true if current_execution_id is set (caller
// validates the execution is in a non-terminal state separately).
func (t *Task) HasActiveExecution() bool {
	return strings.TrimSpace(string(t.currentExecutionID)) != ""
}

// Suspend transitions open→suspended (caller must have killed any active
// execution first; that's enforced at DomainService layer).
func (t *Task) Suspend(now time.Time) error {
	if t.status != StatusOpen {
		return fmt.Errorf("%w: %s → suspended not allowed", ErrTaskInvalidTransition, t.status)
	}
	t.status = StatusSuspended
	t.updatedAt = now.UTC()
	t.version++
	return nil
}

// Resume transitions suspended→open.
func (t *Task) Resume(now time.Time) error {
	if t.status != StatusSuspended {
		return fmt.Errorf("%w: %s → open not allowed", ErrTaskInvalidTransition, t.status)
	}
	t.status = StatusOpen
	t.updatedAt = now.UTC()
	t.version++
	return nil
}

// Abandon transitions open/suspended → abandoned with reason+message
// (conventions § 16).
func (t *Task) Abandon(reason, message string, now time.Time) error {
	if t.status != StatusOpen && t.status != StatusSuspended {
		return fmt.Errorf("%w: %s → abandoned not allowed", ErrTaskInvalidTransition, t.status)
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("task: abandon reason required (conventions § 16)")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("task: abandon message required (conventions § 16)")
	}
	t.status = StatusAbandoned
	t.abandonedReason = reason
	t.abandonedMessage = message
	t.updatedAt = now.UTC()
	t.version++
	return nil
}

// MarkDone transitions any non-terminal → done (driven by
// task_execution.completed; system actor).
func (t *Task) MarkDone(now time.Time) error {
	if t.IsTerminal() {
		return fmt.Errorf("%w: %s → done not allowed", ErrTaskInvalidTransition, t.status)
	}
	t.status = StatusDone
	t.updatedAt = now.UTC()
	t.version++
	return nil
}

// BindConversation sets conversation_id (null→non-null only; unbind not
// supported in v1).
func (t *Task) BindConversation(convID string, now time.Time) error {
	if strings.TrimSpace(convID) == "" {
		return errors.New("task: conversation_id required (use BindConversation only for binding)")
	}
	if strings.TrimSpace(t.conversationID) != "" {
		return fmt.Errorf("%w: conversation_id already set", ErrCannotUnbindConversation)
	}
	t.conversationID = convID
	t.updatedAt = now.UTC()
	t.version++
	return nil
}

// SetCurrentExecutionID assigns the active execution; only callable when
// task is in non-terminal status (DispatchService enforces single-active).
func (t *Task) SetCurrentExecutionID(execID taskruntime.TaskExecutionID, now time.Time) error {
	if t.IsTerminal() {
		return fmt.Errorf("%w: cannot dispatch terminal task", ErrTaskInvalidTransition)
	}
	t.currentExecutionID = execID
	t.updatedAt = now.UTC()
	t.version++
	return nil
}

// ClearCurrentExecutionID resets current_execution_id to empty (after
// execution terminates and task remains open for redispatch).
func (t *Task) ClearCurrentExecutionID(now time.Time) {
	t.currentExecutionID = ""
	t.updatedAt = now.UTC()
	t.version++
}

// UpdatePriority changes priority on a non-terminal task.
func (t *Task) UpdatePriority(p Priority, now time.Time) error {
	if t.IsTerminal() {
		return fmt.Errorf("%w: terminal task is immutable", ErrTaskInvalidTransition)
	}
	if !p.IsValid() {
		return ErrInvalidPriority
	}
	t.priority = p
	t.updatedAt = now.UTC()
	t.version++
	return nil
}

// UpdateDependencies replaces depends_on_task_ids; rejects when task has
// active execution (01-task § 8.4).
func (t *Task) UpdateDependencies(deps []taskruntime.TaskID, now time.Time) error {
	if t.IsTerminal() {
		return fmt.Errorf("%w: terminal task is immutable", ErrTaskInvalidTransition)
	}
	if t.HasActiveExecution() {
		return fmt.Errorf("%w: cannot change deps while execution is active", ErrTaskInvariantViolation)
	}
	if err := validateDeps(t.id, deps); err != nil {
		return err
	}
	t.dependsOnTaskIDs = append([]taskruntime.TaskID(nil), deps...)
	t.updatedAt = now.UTC()
	t.version++
	return nil
}

// SetRequiresWorktree changes workspace_mode; rejects when task has active
// execution (02-task-execution § 8.5).
func (t *Task) SetRequiresWorktree(v bool, now time.Time) error {
	if t.IsTerminal() {
		return fmt.Errorf("%w: terminal task is immutable", ErrTaskInvalidTransition)
	}
	if t.HasActiveExecution() {
		return fmt.Errorf("%w: cannot change requires_worktree while execution is active", ErrTaskInvariantViolation)
	}
	t.requiresWorktree = v
	t.updatedAt = now.UTC()
	t.version++
	return nil
}

func validateDeps(self taskruntime.TaskID, deps []taskruntime.TaskID) error {
	seen := make(map[taskruntime.TaskID]struct{}, len(deps))
	for _, d := range deps {
		if d == self {
			return fmt.Errorf("%w: self-dependency not allowed", ErrTaskInvariantViolation)
		}
		if strings.TrimSpace(string(d)) == "" {
			return fmt.Errorf("%w: empty task id in deps", ErrTaskInvariantViolation)
		}
		if _, ok := seen[d]; ok {
			return fmt.Errorf("%w: duplicate dep %s", ErrTaskInvariantViolation, d)
		}
		seen[d] = struct{}{}
	}
	return nil
}
