package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmsql "github.com/oopslink/agent-center/internal/projectmanager/sqlite"
)

func progressServiceFixture(t *testing.T) (*Service, *pmsql.ProgressControlRepo, *clock.FakeClock, context.Context) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	clk := clock.NewFakeClock(time.Date(2026, 8, 27, 10, 0, 0, 0, time.UTC))
	repo := pmsql.NewProgressControlRepo(db)
	svc := New(Deps{DB: db, Clock: clk, IDGen: idgen.NewGenerator(clk), ProgressControl: repo})
	return svc, repo, clk, context.Background()
}

func TestProgressControl_ReconcileExpiredWakeCreatesHoldAndEscalates(t *testing.T) {
	svc, repo, clk, ctx := progressServiceFixture(t)
	now := clk.Now()
	_, err := repo.RecordWake(ctx, pm.ProgressWake{
		ID: "wake-1", PlanID: "plan-1", TaskID: "task-1", NodeID: "node-1",
		OwnerRef: "user:owner", OwnerDisplay: "Owner", Reason: "blocked", Status: pm.ProgressWakeDelivered,
		IdempotencyKey: "chain-1", RequestedAt: now, DeliveredAt: now, AckDeadline: now.Add(time.Minute),
		MaxHoldDuration: 2 * time.Minute, NextEscalationAt: now.Add(time.Minute), OrganizationOwnerRef: "role:oncall",
	})
	if err != nil {
		t.Fatal(err)
	}
	clk.Advance(90 * time.Second)
	if err := svc.ReconcileProgressControl(ctx, 10); err != nil {
		t.Fatalf("ReconcileProgressControl: %v", err)
	}
	holds, err := repo.ListOpenHoldsByPlan(ctx, "plan-1")
	if err != nil || len(holds) != 1 {
		t.Fatalf("holds = %+v err=%v", holds, err)
	}
	if err := svc.guardTaskProgressHolds(ctx, "task-1", true, false, false); !errors.Is(err, pm.ErrProgressHoldOpen) {
		t.Fatalf("dispatch guard err=%v, want ErrProgressHoldOpen", err)
	}
	if err := svc.AcknowledgeProgressWake(ctx, "wake-1", "user:owner"); err != nil {
		t.Fatalf("AcknowledgeProgressWake: %v", err)
	}
	if err := svc.guardTaskProgressHolds(ctx, "task-1", true, false, false); err != nil {
		t.Fatalf("owner ack should release hold guard, got %v", err)
	}
	_, err = repo.RecordWake(ctx, pm.ProgressWake{
		ID: "wake-2", PlanID: "plan-1", TaskID: "task-1", NodeID: "node-1",
		OwnerRef: "user:owner", OwnerDisplay: "Owner", Reason: "blocked", Status: pm.ProgressWakeDelivered,
		IdempotencyKey: "chain-2", RequestedAt: now, DeliveredAt: now, AckDeadline: now.Add(time.Minute),
		MaxHoldDuration: 2 * time.Minute, NextEscalationAt: now.Add(time.Minute), OrganizationOwnerRef: "role:oncall",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ReconcileProgressControl(ctx, 10); err != nil {
		t.Fatalf("ReconcileProgressControl second wake: %v", err)
	}
	clk.Advance(2 * time.Minute)
	if err := svc.ReconcileProgressControl(ctx, 10); err != nil {
		t.Fatalf("ReconcileProgressControl breach: %v", err)
	}
	snap, err := repo.SnapshotPlan(ctx, "plan-1", clk.Now())
	if err != nil {
		t.Fatal(err)
	}
	if snap.Decision != pm.ProgressDecisionBound || len(snap.OpenHolds) != 1 {
		t.Fatalf("snapshot = %+v, want responsibility_bound with open hold", snap)
	}
	foundP0 := false
	for _, inc := range snap.OpenIncidents {
		if inc.Kind == pm.ProgressIncidentHoldSLOBreach && inc.Severity == "P0" {
			foundP0 = true
		}
	}
	if !foundP0 {
		t.Fatalf("snapshot incidents = %+v, want hold_slo_breached P0", snap.OpenIncidents)
	}
}
