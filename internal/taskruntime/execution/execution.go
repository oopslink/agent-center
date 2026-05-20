package execution

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/taskruntime"
)

// TaskExecution is the per-dispatch trace AR (02-task-execution.md).
//
// Invariants per § 13:
//  1. task_id / worker_id / agent_cli / workspace_mode immutable
//  2. execution_id unique + immutable
//  3. authority on center: worker / agent never mutate state directly
//  4. terminal state unreachable from terminal state
//  5. dispatch contract fields frozen
//  6. each reason field paired with message (conventions § 16)
type TaskExecution struct {
	id                        taskruntime.TaskExecutionID
	taskID                    taskruntime.TaskID
	workerID                  string
	agentCLI                  string
	workspaceMode             WorkspaceMode
	cwd                       string
	branchName                string
	baseBranch                string
	priority                  string
	etaAt                     *time.Time
	executionTimeoutOverride  *time.Duration
	workingSecondsAccumulated int64
	status                    Status
	dispatchState             DispatchState
	pendingInputRequestID     taskruntime.InputRequestID
	startedAt                 time.Time
	workingStartedAt          *time.Time
	cancelRequestedAt         *time.Time
	cancelReason              string
	cancelMessage             string
	endedAt                   *time.Time
	completedReason           CompletedReason
	completedMessage          string
	failedReason              FailedReason
	failedMessage             string
	killedReason              KilledReason
	killedMessage             string
	createdAt                 time.Time
	updatedAt                 time.Time
	version                   int
}

// NewInput captures constructor args.
type NewInput struct {
	ID                       taskruntime.TaskExecutionID
	TaskID                   taskruntime.TaskID
	WorkerID                 string
	AgentCLI                 string
	WorkspaceMode            WorkspaceMode
	BaseBranch               string
	Priority                 string
	EtaAt                    *time.Time
	ExecutionTimeoutOverride *time.Duration
	Now                      time.Time
}

// New constructs a fresh submitted TaskExecution.
func New(in NewInput) (*TaskExecution, error) {
	if strings.TrimSpace(string(in.ID)) == "" {
		return nil, errors.New("execution: id required")
	}
	if strings.TrimSpace(string(in.TaskID)) == "" {
		return nil, errors.New("execution: task_id required")
	}
	if strings.TrimSpace(in.WorkerID) == "" {
		return nil, errors.New("execution: worker_id required")
	}
	if strings.TrimSpace(in.AgentCLI) == "" {
		return nil, errors.New("execution: agent_cli required")
	}
	if !in.WorkspaceMode.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownWorkspaceMode, in.WorkspaceMode)
	}
	if in.Now.IsZero() {
		return nil, errors.New("execution: now required")
	}
	priority := in.Priority
	if priority == "" {
		priority = "medium"
	}
	now := in.Now.UTC()
	var etaCopy *time.Time
	if in.EtaAt != nil {
		v := in.EtaAt.UTC()
		etaCopy = &v
	}
	var ttoCopy *time.Duration
	if in.ExecutionTimeoutOverride != nil {
		v := *in.ExecutionTimeoutOverride
		ttoCopy = &v
	}
	return &TaskExecution{
		id:                       in.ID,
		taskID:                   in.TaskID,
		workerID:                 in.WorkerID,
		agentCLI:                 in.AgentCLI,
		workspaceMode:            in.WorkspaceMode,
		baseBranch:               in.BaseBranch,
		priority:                 priority,
		etaAt:                    etaCopy,
		executionTimeoutOverride: ttoCopy,
		status:                   StatusSubmitted,
		dispatchState:            DispatchPendingAck,
		startedAt:                now,
		createdAt:                now,
		updatedAt:                now,
		version:                  1,
	}, nil
}

// RehydrateInput is for repository round-trip.
type RehydrateInput struct {
	ID                        taskruntime.TaskExecutionID
	TaskID                    taskruntime.TaskID
	WorkerID                  string
	AgentCLI                  string
	WorkspaceMode             WorkspaceMode
	CWD                       string
	BranchName                string
	BaseBranch                string
	Priority                  string
	EtaAt                     *time.Time
	ExecutionTimeoutOverride  *time.Duration
	WorkingSecondsAccumulated int64
	Status                    Status
	DispatchState             DispatchState
	PendingInputRequestID     taskruntime.InputRequestID
	StartedAt                 time.Time
	WorkingStartedAt          *time.Time
	CancelRequestedAt         *time.Time
	CancelReason              string
	CancelMessage             string
	EndedAt                   *time.Time
	CompletedReason           CompletedReason
	CompletedMessage          string
	FailedReason              FailedReason
	FailedMessage             string
	KilledReason              KilledReason
	KilledMessage             string
	CreatedAt                 time.Time
	UpdatedAt                 time.Time
	Version                   int
}

// Rehydrate reconstructs without invariant checks.
func Rehydrate(in RehydrateInput) (*TaskExecution, error) {
	if !in.Status.IsValid() {
		return nil, ErrInvalidStatus
	}
	if !in.DispatchState.IsValid() {
		return nil, fmt.Errorf("execution: invalid dispatch_state %q", in.DispatchState)
	}
	if !in.WorkspaceMode.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownWorkspaceMode, in.WorkspaceMode)
	}
	if in.Version < 1 {
		return nil, errors.New("execution: version must be >= 1")
	}
	var etaCopy *time.Time
	if in.EtaAt != nil {
		v := in.EtaAt.UTC()
		etaCopy = &v
	}
	var ttoCopy *time.Duration
	if in.ExecutionTimeoutOverride != nil {
		v := *in.ExecutionTimeoutOverride
		ttoCopy = &v
	}
	var workingStarted *time.Time
	if in.WorkingStartedAt != nil {
		v := in.WorkingStartedAt.UTC()
		workingStarted = &v
	}
	var cancelReq *time.Time
	if in.CancelRequestedAt != nil {
		v := in.CancelRequestedAt.UTC()
		cancelReq = &v
	}
	var ended *time.Time
	if in.EndedAt != nil {
		v := in.EndedAt.UTC()
		ended = &v
	}
	return &TaskExecution{
		id:                       in.ID,
		taskID:                   in.TaskID,
		workerID:                 in.WorkerID,
		agentCLI:                 in.AgentCLI,
		workspaceMode:            in.WorkspaceMode,
		cwd:                      in.CWD,
		branchName:               in.BranchName,
		baseBranch:               in.BaseBranch,
		priority:                 in.Priority,
		etaAt:                    etaCopy,
		executionTimeoutOverride: ttoCopy,
		workingSecondsAccumulated: in.WorkingSecondsAccumulated,
		status:                   in.Status,
		dispatchState:            in.DispatchState,
		pendingInputRequestID:    in.PendingInputRequestID,
		startedAt:                in.StartedAt.UTC(),
		workingStartedAt:         workingStarted,
		cancelRequestedAt:        cancelReq,
		cancelReason:             in.CancelReason,
		cancelMessage:            in.CancelMessage,
		endedAt:                  ended,
		completedReason:          in.CompletedReason,
		completedMessage:         in.CompletedMessage,
		failedReason:             in.FailedReason,
		failedMessage:            in.FailedMessage,
		killedReason:             in.KilledReason,
		killedMessage:            in.KilledMessage,
		createdAt:                in.CreatedAt.UTC(),
		updatedAt:                in.UpdatedAt.UTC(),
		version:                  in.Version,
	}, nil
}

// Getters.
func (e *TaskExecution) ID() taskruntime.TaskExecutionID         { return e.id }
func (e *TaskExecution) TaskID() taskruntime.TaskID              { return e.taskID }
func (e *TaskExecution) WorkerID() string                         { return e.workerID }
func (e *TaskExecution) AgentCLI() string                         { return e.agentCLI }
func (e *TaskExecution) WorkspaceMode() WorkspaceMode             { return e.workspaceMode }
func (e *TaskExecution) CWD() string                              { return e.cwd }
func (e *TaskExecution) BranchName() string                       { return e.branchName }
func (e *TaskExecution) BaseBranch() string                       { return e.baseBranch }
func (e *TaskExecution) Priority() string                         { return e.priority }
func (e *TaskExecution) WorkingSecondsAccumulated() int64         { return e.workingSecondsAccumulated }
func (e *TaskExecution) Status() Status                           { return e.status }
func (e *TaskExecution) DispatchState() DispatchState             { return e.dispatchState }
func (e *TaskExecution) PendingInputRequestID() taskruntime.InputRequestID {
	return e.pendingInputRequestID
}
func (e *TaskExecution) StartedAt() time.Time          { return e.startedAt }
func (e *TaskExecution) CancelReason() string          { return e.cancelReason }
func (e *TaskExecution) CancelMessage() string         { return e.cancelMessage }
func (e *TaskExecution) CompletedReason() CompletedReason { return e.completedReason }
func (e *TaskExecution) CompletedMessage() string         { return e.completedMessage }
func (e *TaskExecution) FailedReason() FailedReason       { return e.failedReason }
func (e *TaskExecution) FailedMessage() string            { return e.failedMessage }
func (e *TaskExecution) KilledReason() KilledReason       { return e.killedReason }
func (e *TaskExecution) KilledMessage() string            { return e.killedMessage }
func (e *TaskExecution) CreatedAt() time.Time             { return e.createdAt }
func (e *TaskExecution) UpdatedAt() time.Time             { return e.updatedAt }
func (e *TaskExecution) Version() int                     { return e.version }

// EtaAt returns a copy or nil.
func (e *TaskExecution) EtaAt() *time.Time {
	if e.etaAt == nil {
		return nil
	}
	v := e.etaAt.UTC()
	return &v
}

// ExecutionTimeoutOverride returns a copy or nil.
func (e *TaskExecution) ExecutionTimeoutOverride() *time.Duration {
	if e.executionTimeoutOverride == nil {
		return nil
	}
	v := *e.executionTimeoutOverride
	return &v
}

// WorkingStartedAt returns a copy or nil.
func (e *TaskExecution) WorkingStartedAt() *time.Time {
	if e.workingStartedAt == nil {
		return nil
	}
	v := e.workingStartedAt.UTC()
	return &v
}

// CancelRequestedAt returns a copy or nil.
func (e *TaskExecution) CancelRequestedAt() *time.Time {
	if e.cancelRequestedAt == nil {
		return nil
	}
	v := e.cancelRequestedAt.UTC()
	return &v
}

// EndedAt returns a copy or nil.
func (e *TaskExecution) EndedAt() *time.Time {
	if e.endedAt == nil {
		return nil
	}
	v := e.endedAt.UTC()
	return &v
}

// IsTerminal returns true for completed / failed / killed.
func (e *TaskExecution) IsTerminal() bool { return e.status.IsTerminal() }

// AckDispatch marks the dispatch ACKed (worker accepted envelope).
func (e *TaskExecution) AckDispatch(now time.Time) error {
	if e.IsTerminal() {
		return fmt.Errorf("%w: cannot ack terminal execution", ErrTaskExecutionAlreadyTerminated)
	}
	if e.dispatchState == DispatchAcked {
		return nil // idempotent
	}
	if e.dispatchState != DispatchPendingAck {
		return fmt.Errorf("%w: dispatch_state=%s cannot ack", ErrInvalidTransition, e.dispatchState)
	}
	e.dispatchState = DispatchAcked
	e.updatedAt = now.UTC()
	e.version++
	return nil
}

// StartWorking transitions submitted→working (worker spawned agent OK).
func (e *TaskExecution) StartWorking(cwd string, now time.Time) error {
	if e.IsTerminal() {
		return fmt.Errorf("%w: cannot start terminal execution", ErrTaskExecutionAlreadyTerminated)
	}
	if e.status != StatusSubmitted {
		return fmt.Errorf("%w: %s → working not allowed", ErrInvalidTransition, e.status)
	}
	e.status = StatusWorking
	e.cwd = cwd
	if e.workingStartedAt == nil {
		ws := now.UTC()
		e.workingStartedAt = &ws
	}
	e.updatedAt = now.UTC()
	e.version++
	return nil
}

// SetBranchName sets the worktree branch (only meaningful in worktree mode).
func (e *TaskExecution) SetBranchName(branch string) {
	e.branchName = branch
}

// EnterInputRequired transitions working→input_required and stores the IR id.
func (e *TaskExecution) EnterInputRequired(irID taskruntime.InputRequestID, now time.Time) error {
	if e.IsTerminal() {
		return fmt.Errorf("%w: cannot enter input_required from terminal", ErrTaskExecutionAlreadyTerminated)
	}
	if e.status != StatusWorking {
		return fmt.Errorf("%w: %s → input_required not allowed", ErrInvalidTransition, e.status)
	}
	if strings.TrimSpace(string(irID)) == "" {
		return errors.New("execution: input_request_id required")
	}
	e.status = StatusInputRequired
	e.pendingInputRequestID = irID
	e.updatedAt = now.UTC()
	e.version++
	return nil
}

// LeaveInputRequired transitions input_required→working (IR responded).
// Also accumulates working_seconds_accumulated for the previously-working
// session (caller calculates the increment elsewhere; this method just
// resets pending_ir and bumps state).
func (e *TaskExecution) LeaveInputRequired(now time.Time) error {
	if e.IsTerminal() {
		return fmt.Errorf("%w: cannot leave input_required from terminal", ErrTaskExecutionAlreadyTerminated)
	}
	if e.status != StatusInputRequired {
		return fmt.Errorf("%w: %s → working not allowed", ErrInvalidTransition, e.status)
	}
	e.status = StatusWorking
	e.pendingInputRequestID = ""
	e.updatedAt = now.UTC()
	e.version++
	return nil
}

// AccumulateWorking adds seconds to working_seconds_accumulated.
func (e *TaskExecution) AccumulateWorking(seconds int64) {
	if seconds < 0 {
		return
	}
	e.workingSecondsAccumulated += seconds
}

// MarkCompleted transitions a non-terminal execution → completed.
func (e *TaskExecution) MarkCompleted(reason CompletedReason, message string, now time.Time) error {
	if e.IsTerminal() {
		return fmt.Errorf("%w: %s already terminal", ErrTaskExecutionAlreadyTerminated, e.status)
	}
	if e.status != StatusWorking && e.status != StatusInputRequired {
		return fmt.Errorf("%w: %s → completed not allowed", ErrInvalidTransition, e.status)
	}
	if err := reason.Validate(); err != nil {
		return err
	}
	if reason != "" && strings.TrimSpace(message) == "" {
		return errors.New("execution: completed message required when reason is set (conventions § 16)")
	}
	e.status = StatusCompleted
	e.completedReason = reason
	e.completedMessage = message
	e.dispatchState = DispatchNone
	end := now.UTC()
	e.endedAt = &end
	e.updatedAt = end
	e.version++
	return nil
}

// MarkFailed transitions a non-terminal execution → failed.
func (e *TaskExecution) MarkFailed(reason FailedReason, message string, now time.Time) error {
	if e.IsTerminal() {
		return fmt.Errorf("%w: %s already terminal", ErrTaskExecutionAlreadyTerminated, e.status)
	}
	if err := reason.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("execution: failed message required (conventions § 16)")
	}
	e.status = StatusFailed
	e.failedReason = reason
	e.failedMessage = message
	e.dispatchState = DispatchNone
	end := now.UTC()
	e.endedAt = &end
	e.updatedAt = end
	e.version++
	return nil
}

// RequestKill writes cancel_requested_at (two-phase stage 1). Idempotent
// no-op when cancel_requested_at already set.
func (e *TaskExecution) RequestKill(reason string, message string, now time.Time) error {
	if e.IsTerminal() {
		return fmt.Errorf("%w: %s already terminal", ErrTaskExecutionAlreadyTerminated, e.status)
	}
	if strings.TrimSpace(reason) == "" {
		return errors.New("execution: kill reason required (conventions § 16)")
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("execution: kill message required (conventions § 16)")
	}
	if e.cancelRequestedAt != nil {
		return nil // idempotent
	}
	at := now.UTC()
	e.cancelRequestedAt = &at
	e.cancelReason = reason
	e.cancelMessage = message
	e.updatedAt = at
	e.version++
	return nil
}

// MarkKilled transitions a non-terminal execution → killed (two-phase
// stage 2).
func (e *TaskExecution) MarkKilled(reason KilledReason, message string, now time.Time) error {
	if e.IsTerminal() {
		return fmt.Errorf("%w: %s already terminal", ErrTaskExecutionAlreadyTerminated, e.status)
	}
	if err := reason.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(message) == "" {
		return errors.New("execution: killed message required (conventions § 16)")
	}
	e.status = StatusKilled
	e.killedReason = reason
	e.killedMessage = message
	e.dispatchState = DispatchNone
	end := now.UTC()
	e.endedAt = &end
	e.updatedAt = end
	e.version++
	return nil
}
