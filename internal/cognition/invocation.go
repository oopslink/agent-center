package cognition

import (
	"errors"
	"fmt"
	"time"
)

// SupervisorInvocation is the AR for one spawn → exit decision cycle.
//
// Immutable by construction at the field level: all fields unexported, only
// settable through state-machine methods (Spawn / MarkSucceeded / MarkFailed
// / MarkTimedOut) — these are factories or transition methods only.
//
// Invariants (cognition/01-supervisor-invocation § 5):
//   1. trigger_event_ids ≥ 1
//   2. status != running → ended_at != zero
//   3. failed_reason non-empty → failed_message non-empty (conv § 16)
//   4. status=timed_out → timed_out_at non-zero
//   5. scope_kind + scope_key immutable after construction
//   6. version monotonically increasing (CAS owner)
type SupervisorInvocation struct {
	id                 InvocationID
	agentInstanceID    string // ULID; nullable in v2 transitional, required from P9 onward (per ADR-0029 § 1)
	scope              InvocationScope
	triggerEvents      TriggerEventSet
	status             InvocationStatus
	hardTimeoutSeconds int
	startedAt          time.Time
	endedAt            *time.Time
	failedReason       InvocationFailedReason
	failedMessage      string
	timedOutAt         *time.Time
	tokenUsage         TokenUsage
	decisionsMade      int
	promptBlobRef      string
	createdAt          time.Time
	updatedAt          time.Time
	version            int64
}

// SpawnInput captures the constructor arguments for a fresh invocation.
type SpawnInput struct {
	ID              InvocationID
	AgentInstanceID string // ULID of the built-in supervisor AgentInstance (per ADR-0029 § 1); optional in P8 transitional
	Scope           InvocationScope
	TriggerEvents   TriggerEventSet
	PromptBlobRef   string
	StartedAt       time.Time
}

// Spawn constructs a freshly running SupervisorInvocation. Invariants are
// validated up-front; ErrXxx returned on violation.
func Spawn(in SpawnInput) (*SupervisorInvocation, error) {
	if in.ID == "" {
		return nil, errors.New("invocation: id required")
	}
	if in.Scope.IsZero() {
		return nil, errors.New("invocation: scope required")
	}
	if in.TriggerEvents.Len() == 0 {
		return nil, errors.New("invocation: trigger_event_ids required (≥ 1)")
	}
	if in.StartedAt.IsZero() {
		return nil, errors.New("invocation: started_at required")
	}
	ht := HardTimeoutFor(in.Scope.Kind())
	return &SupervisorInvocation{
		id:                 in.ID,
		agentInstanceID:    in.AgentInstanceID,
		scope:              in.Scope,
		triggerEvents:      in.TriggerEvents,
		status:             StatusRunning,
		hardTimeoutSeconds: ht.Seconds(),
		startedAt:          in.StartedAt.UTC(),
		promptBlobRef:      in.PromptBlobRef,
		createdAt:          in.StartedAt.UTC(),
		updatedAt:          in.StartedAt.UTC(),
		version:            1,
	}, nil
}

// AgentInstanceID returns the built-in supervisor AgentInstance ID (per
// ADR-0029 § 1). Empty in v1 / P8 transitional rows; required from P9.
func (inv *SupervisorInvocation) AgentInstanceID() string { return inv.agentInstanceID }

// ID returns the invocation id.
func (inv *SupervisorInvocation) ID() InvocationID { return inv.id }

// Scope returns the scope VO.
func (inv *SupervisorInvocation) Scope() InvocationScope { return inv.scope }

// TriggerEvents returns the trigger event set.
func (inv *SupervisorInvocation) TriggerEvents() TriggerEventSet { return inv.triggerEvents }

// Status returns the current status.
func (inv *SupervisorInvocation) Status() InvocationStatus { return inv.status }

// HardTimeoutSeconds returns the per-scope hard timeout (seconds).
func (inv *SupervisorInvocation) HardTimeoutSeconds() int { return inv.hardTimeoutSeconds }

// StartedAt returns the start time.
func (inv *SupervisorInvocation) StartedAt() time.Time { return inv.startedAt }

// EndedAt returns the terminal time (nil while running).
func (inv *SupervisorInvocation) EndedAt() *time.Time {
	if inv.endedAt == nil {
		return nil
	}
	cp := *inv.endedAt
	return &cp
}

// FailedReason returns the closed-enum reason (zero while not failed).
func (inv *SupervisorInvocation) FailedReason() InvocationFailedReason {
	return inv.failedReason
}

// FailedMessage returns the human-readable failure message.
func (inv *SupervisorInvocation) FailedMessage() string { return inv.failedMessage }

// TimedOutAt returns the timeout instant (nil unless status=timed_out).
func (inv *SupervisorInvocation) TimedOutAt() *time.Time {
	if inv.timedOutAt == nil {
		return nil
	}
	cp := *inv.timedOutAt
	return &cp
}

// TokenUsage returns the accumulated token usage (zero unless succeeded).
func (inv *SupervisorInvocation) TokenUsage() TokenUsage { return inv.tokenUsage }

// DecisionsMade returns the count of DecisionRecord rows attributed to
// this invocation (set on success).
func (inv *SupervisorInvocation) DecisionsMade() int { return inv.decisionsMade }

// PromptBlobRef returns the BlobStore ref if prompt > threshold; "" otherwise.
func (inv *SupervisorInvocation) PromptBlobRef() string { return inv.promptBlobRef }

// CreatedAt returns the row insert time.
func (inv *SupervisorInvocation) CreatedAt() time.Time { return inv.createdAt }

// UpdatedAt returns the latest UPDATE time.
func (inv *SupervisorInvocation) UpdatedAt() time.Time { return inv.updatedAt }

// Version returns the CAS version.
func (inv *SupervisorInvocation) Version() int64 { return inv.version }

// IsTerminal reports whether the invocation is in a terminal status.
func (inv *SupervisorInvocation) IsTerminal() bool { return inv.status.IsTerminal() }

// MarkSucceeded transitions running → succeeded. Returns
// ErrInvalidStatusTransition if already terminal.
func (inv *SupervisorInvocation) MarkSucceeded(at time.Time, tu TokenUsage, decisions int) error {
	if inv.status != StatusRunning {
		return fmt.Errorf("%w: %s → succeeded", ErrInvalidStatusTransition, inv.status)
	}
	if at.IsZero() {
		return errors.New("invocation: ended_at required")
	}
	at = at.UTC()
	inv.status = StatusSucceeded
	inv.endedAt = &at
	inv.tokenUsage = tu
	inv.decisionsMade = decisions
	inv.updatedAt = at
	inv.version++
	return nil
}

// MarkFailed transitions running → failed. reason / message both required
// (conventions § 16).
func (inv *SupervisorInvocation) MarkFailed(reason InvocationFailedReason, message string, at time.Time) error {
	if inv.status != StatusRunning {
		return fmt.Errorf("%w: %s → failed", ErrInvalidStatusTransition, inv.status)
	}
	if !reason.IsValid() {
		return fmt.Errorf("invocation: invalid failed_reason %q", reason)
	}
	if message == "" {
		return errors.New("invocation: failed_message required when failed_reason is set (conventions § 16)")
	}
	if at.IsZero() {
		return errors.New("invocation: ended_at required")
	}
	at = at.UTC()
	inv.status = StatusFailed
	inv.endedAt = &at
	inv.failedReason = reason
	inv.failedMessage = message
	inv.updatedAt = at
	inv.version++
	return nil
}

// MarkTimedOut transitions running → timed_out.
func (inv *SupervisorInvocation) MarkTimedOut(at time.Time) error {
	if inv.status != StatusRunning {
		return fmt.Errorf("%w: %s → timed_out", ErrInvalidStatusTransition, inv.status)
	}
	if at.IsZero() {
		return errors.New("invocation: timed_out_at required")
	}
	at = at.UTC()
	inv.status = StatusTimedOut
	inv.endedAt = &at
	inv.timedOutAt = &at
	inv.updatedAt = at
	inv.version++
	return nil
}

// RehydrateInput is the SQLite-repo → AR adapter input.
type RehydrateInput struct {
	ID                 InvocationID
	AgentInstanceID    string
	Scope              InvocationScope
	TriggerEvents      TriggerEventSet
	Status             InvocationStatus
	HardTimeoutSeconds int
	StartedAt          time.Time
	EndedAt            *time.Time
	FailedReason       InvocationFailedReason
	FailedMessage      string
	TimedOutAt         *time.Time
	TokenUsage         TokenUsage
	DecisionsMade      int
	PromptBlobRef      string
	CreatedAt          time.Time
	UpdatedAt          time.Time
	Version            int64
}

// Rehydrate constructs an AR from a DB row without re-running invariants —
// rows that violate invariants are a corruption / migration bug and would
// have been caught at write time. Returns an error if Status is unknown
// (the only field we cannot tolerate a bad value for).
func Rehydrate(in RehydrateInput) (*SupervisorInvocation, error) {
	if !in.Status.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownStatus, in.Status)
	}
	inv := &SupervisorInvocation{
		id:                 in.ID,
		agentInstanceID:    in.AgentInstanceID,
		scope:              in.Scope,
		triggerEvents:      in.TriggerEvents,
		status:             in.Status,
		hardTimeoutSeconds: in.HardTimeoutSeconds,
		startedAt:          in.StartedAt.UTC(),
		failedReason:       in.FailedReason,
		failedMessage:      in.FailedMessage,
		tokenUsage:         in.TokenUsage,
		decisionsMade:      in.DecisionsMade,
		promptBlobRef:      in.PromptBlobRef,
		createdAt:          in.CreatedAt.UTC(),
		updatedAt:          in.UpdatedAt.UTC(),
		version:            in.Version,
	}
	if in.EndedAt != nil {
		t := in.EndedAt.UTC()
		inv.endedAt = &t
	}
	if in.TimedOutAt != nil {
		t := in.TimedOutAt.UTC()
		inv.timedOutAt = &t
	}
	return inv, nil
}
