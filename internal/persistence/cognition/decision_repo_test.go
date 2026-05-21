package cognition_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	cognitiondb "github.com/oopslink/agent-center/internal/persistence/cognition"
)

func TestDecisionRepo_AppendAndFind(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	ctx := context.Background()
	now := time.Now().UTC()
	d, err := cognition.NewDecisionRecord(cognition.NewDecisionInput{
		ID:             "D1",
		InvocationID:   "I1",
		Kind:           cognition.DecisionDispatch,
		TargetRefsJSON: `{"task_id":"T-1"}`,
		Rationale:      "W-1 is idle",
		Outcome:        cognition.OutcomeSucceeded,
		CreatedAt:      now,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Append(ctx, d); err != nil {
		t.Fatalf("append: %v", err)
	}
	got, err := repo.FindByID(ctx, "D1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Rationale() != "W-1 is idle" {
		t.Errorf("rationale = %q", got.Rationale())
	}
}

func TestDecisionRepo_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	if _, err := repo.FindByID(context.Background(), "nope"); !errors.Is(err, cognition.ErrDecisionNotFound) {
		t.Fatalf("expected ErrDecisionNotFound, got %v", err)
	}
}

func TestDecisionRepo_DuplicateImmutable(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	ctx := context.Background()
	d, _ := cognition.NewDecisionRecord(cognition.NewDecisionInput{
		ID: "D1", InvocationID: "I1", Kind: cognition.DecisionNoOp,
		Rationale: "r", Outcome: cognition.OutcomeSucceeded, CreatedAt: time.Now(),
	})
	if err := repo.Append(ctx, d); err != nil {
		t.Fatal(err)
	}
	if err := repo.Append(ctx, d); !errors.Is(err, cognition.ErrDecisionImmutable) {
		t.Fatalf("expected ErrDecisionImmutable, got %v", err)
	}
}

func TestDecisionRepo_FindByInvocation(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	ctx := context.Background()
	base := time.Now().UTC()
	for i, kind := range []cognition.DecisionKind{cognition.DecisionDispatch, cognition.DecisionKillExecution, cognition.DecisionNoOp} {
		d, _ := cognition.NewDecisionRecord(cognition.NewDecisionInput{
			ID:           cognition.DecisionID("D" + string(rune('A'+i))),
			InvocationID: "INV",
			Kind:         kind,
			Rationale:    "r",
			Outcome:      cognition.OutcomeSucceeded,
			CreatedAt:    base.Add(time.Duration(i) * time.Second),
		})
		if err := repo.Append(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	got, err := repo.FindByInvocationID(ctx, "INV")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Errorf("got %d", len(got))
	}
	// chronological
	if !got[0].CreatedAt().Before(got[2].CreatedAt()) {
		t.Errorf("not chronological")
	}
}

func TestDecisionRepo_FindWithFilter(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	ctx := context.Background()
	base := time.Now().UTC()
	for i, kind := range []cognition.DecisionKind{cognition.DecisionDispatch, cognition.DecisionNoOp} {
		d, _ := cognition.NewDecisionRecord(cognition.NewDecisionInput{
			ID:           cognition.DecisionID("D" + string(rune('A'+i))),
			InvocationID: "INV",
			Kind:         kind,
			Rationale:    "r",
			Outcome:      cognition.OutcomeSucceeded,
			CreatedAt:    base.Add(time.Duration(i) * time.Second),
		})
		_ = repo.Append(ctx, d)
	}
	invID := cognition.InvocationID("INV")
	k := cognition.DecisionDispatch
	got, err := repo.Find(ctx, cognition.DecisionFilter{InvocationID: &invID, Kind: &k, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Errorf("got %d", len(got))
	}
	// limit
	if _, err := repo.Find(ctx, cognition.DecisionFilter{Limit: cognition.MaxDecisionLimit + 1}); !errors.Is(err, cognition.ErrDecisionLimitTooLarge) {
		t.Fatalf("expected ErrDecisionLimitTooLarge, got %v", err)
	}
	// cursor
	cursor := cognition.DecisionID("DA")
	got, _ = repo.Find(ctx, cognition.DecisionFilter{Cursor: &cursor, Limit: 10})
	for _, d := range got {
		if d.ID() <= "DA" {
			t.Errorf("cursor: got %s", d.ID())
		}
	}
}

func TestDecisionRepo_NilGuard(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	if err := repo.Append(context.Background(), nil); err == nil {
		t.Error("nil append")
	}
}

// TestDecisionRepo_Append_EmptyRationale guards the "rationale==''" branch
// in Append (ADR-0014 invariant: rationale required pre-INSERT).
func TestDecisionRepo_Append_EmptyRationale(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	// Construct a DecisionRecord via RehydrateDecision so we can bypass the
	// AR constructor's rationale check and exercise the repo's own check.
	d, err := cognition.RehydrateDecision(cognition.RehydrateDecisionInput{
		ID: "DRX", InvocationID: "INVX", Kind: cognition.DecisionDispatch,
		Rationale: "", Outcome: cognition.OutcomeSucceeded,
		CreatedAt: time.Now().UTC(),
	})
	if err == nil {
		// If rehydrate now rejects empty rationale upstream, the repo
		// check is unreachable but still serves as defense-in-depth.
		if appendErr := repo.Append(context.Background(), d); appendErr == nil {
			t.Error("expected rationale-required error")
		}
	}
}

// TestDecisionRepo_Find_InvocationOnly exercises the InvocationID-only filter.
func TestDecisionRepo_Find_InvocationOnly(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	ctx := context.Background()
	base := time.Now().UTC()
	for i := 0; i < 2; i++ {
		d, _ := cognition.NewDecisionRecord(cognition.NewDecisionInput{
			ID:           cognition.DecisionID("DV" + string(rune('A'+i))),
			InvocationID: "INVV",
			Kind:         cognition.DecisionDispatch,
			Rationale:    "r",
			Outcome:      cognition.OutcomeSucceeded,
			CreatedAt:    base.Add(time.Duration(i) * time.Second),
		})
		_ = repo.Append(ctx, d)
	}
	invID := cognition.InvocationID("INVV")
	got, err := repo.Find(ctx, cognition.DecisionFilter{InvocationID: &invID})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("invocation-only filter: got %d, want 2", len(got))
	}
}

func TestDecisionRepo_FailedOutcomeRehydrate(t *testing.T) {
	db := openTestDB(t)
	repo := cognitiondb.NewDecisionRepo(db)
	ctx := context.Background()
	d, _ := cognition.NewDecisionRecord(cognition.NewDecisionInput{
		ID: "D1", InvocationID: "I1", Kind: cognition.DecisionDispatch,
		Rationale: "r", Outcome: cognition.OutcomeFailed, OutcomeMessage: "boom",
		CreatedAt: time.Now(),
	})
	if err := repo.Append(ctx, d); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByID(ctx, "D1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Outcome() != cognition.OutcomeFailed || got.OutcomeMessage() != "boom" {
		t.Errorf("got %+v", got)
	}
}
