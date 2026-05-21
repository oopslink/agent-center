package decision_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/cognition/decision"
	"github.com/oopslink/agent-center/internal/idgen"
)

// memRepo is an in-memory DecisionRecordRepository for testing.
type memRepo struct {
	mu   sync.Mutex
	rows []*cognition.DecisionRecord
}

func (r *memRepo) Append(_ context.Context, d *cognition.DecisionRecord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.rows {
		if existing.ID() == d.ID() {
			return cognition.ErrDecisionImmutable
		}
	}
	r.rows = append(r.rows, d)
	return nil
}

func (r *memRepo) FindByID(_ context.Context, id cognition.DecisionID) (*cognition.DecisionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, d := range r.rows {
		if d.ID() == id {
			return d, nil
		}
	}
	return nil, cognition.ErrDecisionNotFound
}

func (r *memRepo) FindByInvocationID(_ context.Context, id cognition.InvocationID) ([]*cognition.DecisionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*cognition.DecisionRecord
	for _, d := range r.rows {
		if d.InvocationID() == id {
			out = append(out, d)
		}
	}
	return out, nil
}

func (r *memRepo) Find(_ context.Context, _ cognition.DecisionFilter) ([]*cognition.DecisionRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*cognition.DecisionRecord, len(r.rows))
	copy(out, r.rows)
	return out, nil
}

func TestInferActor(t *testing.T) {
	env := func(k string) (string, bool) {
		if k == "AGENT_CENTER_INVOCATION_ID" {
			return "INV1", true
		}
		return "", false
	}
	a := decision.InferActorFromEnv(env, "alice")
	if !a.IsSupervisor() {
		t.Error("expected supervisor")
	}
	if a.ActorString() != "supervisor:INV1" {
		t.Errorf("actor = %q", a.ActorString())
	}
}

func TestInferActor_User(t *testing.T) {
	env := func(k string) (string, bool) { return "", false }
	a := decision.InferActorFromEnv(env, "alice")
	if a.IsSupervisor() {
		t.Error("user should not be supervisor")
	}
	if a.ActorString() != "user:alice" {
		t.Errorf("actor = %q", a.ActorString())
	}
}

func TestInferActor_System(t *testing.T) {
	env := func(k string) (string, bool) { return "", false }
	a := decision.InferActorFromEnv(env, "")
	if a.ActorString() != "system" {
		t.Errorf("actor = %q", a.ActorString())
	}
}

func TestRecorder_Validation(t *testing.T) {
	if _, err := decision.NewRecorder(nil, nil, nil); err == nil {
		t.Fatal("missing repo")
	}
}

func TestRecorder_SkipUserActor(t *testing.T) {
	repo := &memRepo{}
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(1))
	r, _ := decision.NewRecorder(repo, clk, gen)
	id, err := r.Record(context.Background(),
		decision.Actor{Kind: "user", ID: "alice"},
		decision.RecordRequest{Kind: cognition.DecisionDispatch, Rationale: "x", Outcome: cognition.OutcomeSucceeded})
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Errorf("expected empty id for user actor, got %s", id)
	}
	if len(repo.rows) != 0 {
		t.Errorf("user actor should not write rows; got %d", len(repo.rows))
	}
}

func TestRecorder_SupervisorWrites(t *testing.T) {
	repo := &memRepo{}
	clk := clock.NewFakeClock(time.Date(2026, 5, 22, 0, 0, 0, 0, time.UTC))
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(2))
	r, _ := decision.NewRecorder(repo, clk, gen)
	actor := decision.Actor{Kind: "supervisor", ID: "INV1", InvocationID: "INV1"}
	id, err := r.Record(context.Background(), actor, decision.RecordRequest{
		Kind: cognition.DecisionDispatch, Rationale: "W-1 idle",
		Outcome: cognition.OutcomeSucceeded, TargetRefsJSON: `{"task_id":"T-1"}`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if id == "" {
		t.Fatal("empty id")
	}
	if len(repo.rows) != 1 {
		t.Errorf("rows = %d", len(repo.rows))
	}
	if repo.rows[0].Rationale() != "W-1 idle" {
		t.Errorf("rationale = %q", repo.rows[0].Rationale())
	}
}

func TestRecorder_RationaleRequired(t *testing.T) {
	repo := &memRepo{}
	clk := clock.NewFakeClock(time.Now())
	r, _ := decision.NewRecorder(repo, clk, idgen.NewGenerator(clk))
	actor := decision.Actor{Kind: "supervisor", ID: "I1", InvocationID: "I1"}
	_, err := r.Record(context.Background(), actor, decision.RecordRequest{
		Kind: cognition.DecisionNoOp, Outcome: cognition.OutcomeSucceeded,
	})
	if !errors.Is(err, cognition.ErrRationaleRequired) {
		t.Fatalf("expected ErrRationaleRequired, got %v", err)
	}
	if len(repo.rows) != 0 {
		t.Error("should not write on validation error")
	}
}

func TestRecorder_InvalidKind(t *testing.T) {
	repo := &memRepo{}
	r, _ := decision.NewRecorder(repo, nil, nil)
	actor := decision.Actor{Kind: "supervisor", ID: "I1", InvocationID: "I1"}
	_, err := r.Record(context.Background(), actor, decision.RecordRequest{
		Kind: cognition.DecisionKind("bogus"), Rationale: "x", Outcome: cognition.OutcomeSucceeded,
	})
	if err == nil {
		t.Fatal("expected invalid kind err")
	}
}

func TestRecorder_FailedNeedsMessage(t *testing.T) {
	repo := &memRepo{}
	r, _ := decision.NewRecorder(repo, nil, nil)
	actor := decision.Actor{Kind: "supervisor", ID: "I1", InvocationID: "I1"}
	_, err := r.Record(context.Background(), actor, decision.RecordRequest{
		Kind: cognition.DecisionDispatch, Rationale: "r", Outcome: cognition.OutcomeFailed,
	})
	if err == nil {
		t.Fatal("expected outcome_message err")
	}
}
