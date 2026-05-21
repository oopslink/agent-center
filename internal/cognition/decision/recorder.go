// Package decision hosts the DecisionRecorder domain service (plan-6 § 3.7).
//
// Pattern: action CLI handlers wrap their state-machine call + event emit
// in a tx, and additionally call Record inside the same tx — three-table
// atomicity (state UPDATE + decision_records INSERT + events INSERT, per
// ADR-0014 § 2).
package decision

import (
	"context"
	"errors"
	"os"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/idgen"
)

// Actor describes who's running the CLI: a supervisor with a known
// invocation_id (the env-injected case) or a user (everything else).
type Actor struct {
	Kind         string // "supervisor" | "user" | "system"
	ID           string // invocation id / user id / "system"
	InvocationID cognition.InvocationID
}

// IsSupervisor reports whether the actor is a supervisor invocation.
func (a Actor) IsSupervisor() bool {
	return a.Kind == "supervisor" && a.InvocationID != ""
}

// ActorString returns the formal observability.Actor representation.
func (a Actor) ActorString() string {
	if a.Kind == "system" || a.ID == "system" {
		return "system"
	}
	return a.Kind + ":" + a.ID
}

// InferActorFromEnv looks at AGENT_CENTER_INVOCATION_ID; when set the
// caller is the supervisor subprocess, otherwise the caller is a user
// (defaultUserID is supplied by the App layer from Identity config).
func InferActorFromEnv(env func(string) (string, bool), defaultUserID string) Actor {
	if v, ok := env("AGENT_CENTER_INVOCATION_ID"); ok && v != "" {
		return Actor{Kind: "supervisor", ID: v, InvocationID: cognition.InvocationID(v)}
	}
	if defaultUserID == "" {
		return Actor{Kind: "system", ID: "system"}
	}
	return Actor{Kind: "user", ID: defaultUserID}
}

// DefaultActor uses os.LookupEnv.
func DefaultActor(defaultUserID string) Actor {
	return InferActorFromEnv(os.LookupEnv, defaultUserID)
}

// RecordRequest is the parameter set for Recorder.Record.
type RecordRequest struct {
	Kind           cognition.DecisionKind
	TargetRefsJSON string
	Rationale      string
	Outcome        cognition.DecisionOutcome
	OutcomeMessage string
}

// Recorder writes DecisionRecord rows. Construct once per App; pass to
// each action handler.
type Recorder struct {
	repo  cognition.DecisionRecordRepository
	clock clock.Clock
	idgen idgen.Generator
}

// NewRecorder wires a Recorder.
func NewRecorder(repo cognition.DecisionRecordRepository, clk clock.Clock, gen idgen.Generator) (*Recorder, error) {
	if repo == nil {
		return nil, errors.New("decision: repo required")
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if gen == nil {
		gen = idgen.NewGenerator(clk)
	}
	return &Recorder{repo: repo, clock: clk, idgen: gen}, nil
}

// Record persists a decision row. The caller MUST be running inside a tx
// (ctx with WithTx); the repo will pick it up. Returns the new
// DecisionID for caller to attach to subsequent emit() events via
// EmitCommand.DecisionID.
//
// Skipped silently with empty id + nil error when actor is NOT a
// supervisor — user-driven CLI invocations do not write to
// decision_records (cognition/01 § 4.8). Callers that want to enforce
// supervisor-only paths should check actor.IsSupervisor() first.
func (r *Recorder) Record(ctx context.Context, actor Actor, req RecordRequest) (cognition.DecisionID, error) {
	if !actor.IsSupervisor() {
		return "", nil
	}
	if req.Rationale == "" {
		return "", cognition.ErrRationaleRequired
	}
	d, err := cognition.NewDecisionRecord(cognition.NewDecisionInput{
		ID:             cognition.DecisionID(r.idgen.NewULID()),
		InvocationID:   actor.InvocationID,
		Kind:           req.Kind,
		TargetRefsJSON: req.TargetRefsJSON,
		Rationale:      req.Rationale,
		Outcome:        req.Outcome,
		OutcomeMessage: req.OutcomeMessage,
		CreatedAt:      r.clock.Now(),
	})
	if err != nil {
		return "", err
	}
	if err := r.repo.Append(ctx, d); err != nil {
		return "", err
	}
	return d.ID(), nil
}
