package cognition

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// DecisionRecord is the append-only entity owned by SupervisorInvocation
// (cognition/01-supervisor-invocation § 4). One row per concrete action
// the supervisor commands; rationale is mandatory.
type DecisionRecord struct {
	id              DecisionID
	invocationID    InvocationID
	kind            DecisionKind
	targetRefsJSON  string
	rationale       string
	outcome         DecisionOutcome
	outcomeMessage  string
	createdAt       time.Time
}

// NewDecisionInput captures factory args.
type NewDecisionInput struct {
	ID             DecisionID
	InvocationID   InvocationID
	Kind           DecisionKind
	TargetRefsJSON string
	Rationale      string
	Outcome        DecisionOutcome
	OutcomeMessage string
	CreatedAt      time.Time
}

// NewDecisionRecord constructs a DecisionRecord with full invariant
// validation:
//   - rationale required (cognition/01 § 4.7)
//   - invocation_id required
//   - kind closed enum
//   - outcome closed enum
//   - outcome=failed → outcome_message required (conv § 16)
func NewDecisionRecord(in NewDecisionInput) (*DecisionRecord, error) {
	if in.ID == "" {
		return nil, errors.New("decision: id required")
	}
	if in.InvocationID == "" {
		return nil, ErrInvocationIDRequired
	}
	if !in.Kind.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownDecisionKind, in.Kind)
	}
	if strings.TrimSpace(in.Rationale) == "" {
		return nil, ErrRationaleRequired
	}
	if !in.Outcome.IsValid() {
		return nil, fmt.Errorf("decision: outcome must be %q or %q, got %q",
			OutcomeSucceeded, OutcomeFailed, in.Outcome)
	}
	if in.Outcome == OutcomeFailed && strings.TrimSpace(in.OutcomeMessage) == "" {
		return nil, errors.New("decision: outcome_message required when outcome=failed (conventions § 16)")
	}
	if in.CreatedAt.IsZero() {
		return nil, errors.New("decision: created_at required")
	}
	refs := in.TargetRefsJSON
	if strings.TrimSpace(refs) == "" {
		refs = "{}"
	}
	return &DecisionRecord{
		id:             in.ID,
		invocationID:   in.InvocationID,
		kind:           in.Kind,
		targetRefsJSON: refs,
		rationale:      in.Rationale,
		outcome:        in.Outcome,
		outcomeMessage: in.OutcomeMessage,
		createdAt:      in.CreatedAt.UTC(),
	}, nil
}

// ID returns the decision id.
func (d *DecisionRecord) ID() DecisionID { return d.id }

// InvocationID returns the parent invocation id.
func (d *DecisionRecord) InvocationID() InvocationID { return d.invocationID }

// Kind returns the closed-enum kind.
func (d *DecisionRecord) Kind() DecisionKind { return d.kind }

// TargetRefsJSON returns the JSON-encoded target refs blob.
func (d *DecisionRecord) TargetRefsJSON() string { return d.targetRefsJSON }

// Rationale returns the (mandatory) rationale text.
func (d *DecisionRecord) Rationale() string { return d.rationale }

// Outcome returns the binary outcome.
func (d *DecisionRecord) Outcome() DecisionOutcome { return d.outcome }

// OutcomeMessage returns the per-row message (required when outcome=failed).
func (d *DecisionRecord) OutcomeMessage() string { return d.outcomeMessage }

// CreatedAt returns the insert time.
func (d *DecisionRecord) CreatedAt() time.Time { return d.createdAt }

// RehydrateDecisionInput is the SQLite → AR rehydration parameter set.
type RehydrateDecisionInput struct {
	ID             DecisionID
	InvocationID   InvocationID
	Kind           DecisionKind
	TargetRefsJSON string
	Rationale      string
	Outcome        DecisionOutcome
	OutcomeMessage string
	CreatedAt      time.Time
}

// RehydrateDecision constructs a DecisionRecord from DB without rerunning
// invariants. Returns error only on unknown Kind/Outcome (the only fields
// where a bad row indicates corruption rather than recoverable state).
func RehydrateDecision(in RehydrateDecisionInput) (*DecisionRecord, error) {
	if !in.Kind.IsValid() {
		return nil, fmt.Errorf("%w: %q", ErrUnknownDecisionKind, in.Kind)
	}
	if !in.Outcome.IsValid() {
		return nil, fmt.Errorf("decision: unknown outcome %q", in.Outcome)
	}
	return &DecisionRecord{
		id:             in.ID,
		invocationID:   in.InvocationID,
		kind:           in.Kind,
		targetRefsJSON: in.TargetRefsJSON,
		rationale:      in.Rationale,
		outcome:        in.Outcome,
		outcomeMessage: in.OutcomeMessage,
		createdAt:      in.CreatedAt.UTC(),
	}, nil
}
