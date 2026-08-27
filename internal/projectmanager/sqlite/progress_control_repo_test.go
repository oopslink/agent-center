package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func progressSetup(t *testing.T) (context.Context, *ProgressControlRepo) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return context.Background(), NewProgressControlRepo(db)
}

func TestProgressControlRepo_WakeAckHoldRoundTripIdempotent(t *testing.T) {
	ctx, repo := progressSetup(t)
	now := time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC)
	w := pm.ProgressWake{
		ID: "wake-1", PlanID: "plan-1", TaskID: "task-1", NodeID: "node-1",
		OwnerRef: "user:owner", OwnerDisplay: "Owner", Reason: "blocked", Status: pm.ProgressWakeRequested,
		IdempotencyKey: "same-chain", RequestedAt: now, AckDeadline: now.Add(time.Minute),
		MaxHoldDuration: time.Hour, NextEscalationAt: now.Add(time.Minute), OrganizationOwnerRef: "role:oncall",
	}
	created, err := repo.RecordWake(ctx, w)
	if err != nil || !created {
		t.Fatalf("RecordWake created=%v err=%v", created, err)
	}
	created, err = repo.RecordWake(ctx, w)
	if err != nil || created {
		t.Fatalf("duplicate RecordWake created=%v err=%v, want idempotent false/nil", created, err)
	}
	expired, err := repo.ListExpiredUnackedWakes(ctx, now.Add(2*time.Minute), 10)
	if err != nil || len(expired) != 1 || expired[0].ID != "wake-1" {
		t.Fatalf("expired wakes = %+v err=%v", expired, err)
	}
	ok, err := repo.AcknowledgeWake(ctx, "wake-1", "user:owner", now.Add(3*time.Minute), "ack:wake-1")
	if err != nil || !ok {
		t.Fatalf("AcknowledgeWake ok=%v err=%v", ok, err)
	}
	expired, err = repo.ListExpiredUnackedWakes(ctx, now.Add(4*time.Minute), 10)
	if err != nil || len(expired) != 0 {
		t.Fatalf("acked wake still expired = %+v err=%v", expired, err)
	}

	h := pm.ProgressHold{
		ID: "hold-1", PlanID: "plan-1", TaskID: "task-1", NodeID: "node-1",
		ReasonKind: pm.ProgressObligationAckWake, ReasonID: "obl-1", OwnerRef: "user:owner", OwnerDisplay: "Owner",
		EnteredAt: now, HoldAckDeadline: now.Add(time.Minute), MaxHoldDuration: time.Hour,
		NextEscalationAt: now.Add(time.Minute), BlocksDispatch: true, BlocksAcceptance: true, BlocksCompletion: true,
	}
	if _, err := repo.UpsertHold(ctx, h); err != nil {
		t.Fatalf("UpsertHold: %v", err)
	}
	holds, err := repo.ListOpenHoldsByPlan(ctx, "plan-1")
	if err != nil || len(holds) != 1 || !holds[0].BlocksDispatch {
		t.Fatalf("open holds = %+v err=%v", holds, err)
	}
	n, err := repo.ReleaseHoldsByFact(ctx, "plan-1", "task-1", "user:owner", pm.ProgressDecisionRecorded+":d1", now.Add(5*time.Minute))
	if err != nil || n != 1 {
		t.Fatalf("ReleaseHoldsByFact n=%d err=%v", n, err)
	}
	holds, _ = repo.ListOpenHoldsByPlan(ctx, "plan-1")
	if len(holds) != 0 {
		t.Fatalf("released hold still open: %+v", holds)
	}
}
