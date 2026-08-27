package orchestration_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	. "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	orchsqlite "github.com/oopslink/agent-center/internal/projectmanager/orchestration/sqlite"
)

func controllerSetup(t *testing.T) (context.Context, *Service, *clock.FakeClock, *sql.DB) {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })

	clk := clock.NewFakeClock(time.Date(2026, 8, 27, 8, 0, 0, 0, time.UTC))
	gen := idgen.NewGeneratorWithReader(clk, idgen.DeterministicReader(1605))
	svc := NewService(ServiceDeps{
		DB:     db,
		Graphs: orchsqlite.NewGraphRepo(db),
		Nodes:  orchsqlite.NewNodeRepo(db),
		Edges:  orchsqlite.NewEdgeRepo(db),
		IDGen:  gen,
		Clock:  clk,
	})
	return context.Background(), svc, clk, db
}

func TestPlanControllerLease_DualControllerSingleWriter(t *testing.T) {
	ctx, svc, _, _ := controllerSetup(t)
	const planID = "plan-dual"

	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, owner := range []string{"controller-a", "controller-b"} {
		wg.Add(1)
		go func(owner string) {
			defer wg.Done()
			_, err := svc.AcquirePlanLease(ctx, AcquireLeaseCommand{PlanID: planID, OwnerInstanceID: owner, TTL: time.Minute})
			errs <- err
		}(owner)
	}
	wg.Wait()
	close(errs)

	acquired := 0
	busy := 0
	for err := range errs {
		switch {
		case err == nil:
			acquired++
		case errors.Is(err, ErrLeaseBusy):
			busy++
		default:
			t.Fatalf("AcquirePlanLease unexpected error: %v", err)
		}
	}
	if acquired != 1 || busy != 1 {
		t.Fatalf("dual acquire acquired=%d busy=%d, want 1/1", acquired, busy)
	}
}

func TestPlanControllerLease_ExpiryTakeoverRejectsStaleToken(t *testing.T) {
	ctx, svc, clk, _ := controllerSetup(t)

	graphID, err := svc.CreateGraph(ctx, "plan-takeover")
	if err != nil {
		t.Fatalf("CreateGraph: %v", err)
	}
	nodeID, err := svc.AddNode(ctx, graphID, string(NodeCategoryBusiness), "", "dev", nil)
	if err != nil {
		t.Fatalf("AddNode: %v", err)
	}

	leaseA, err := svc.AcquirePlanLease(ctx, AcquireLeaseCommand{PlanID: "plan-takeover", OwnerInstanceID: "a", TTL: time.Minute})
	if err != nil {
		t.Fatalf("acquire a: %v", err)
	}
	stale := PlanControllerToken{PlanID: leaseA.PlanID, OwnerInstanceID: leaseA.OwnerInstanceID, FencingToken: leaseA.FencingToken}

	clk.Advance(2 * time.Minute)
	leaseB, err := svc.AcquirePlanLease(ctx, AcquireLeaseCommand{PlanID: "plan-takeover", OwnerInstanceID: "b", TTL: time.Minute})
	if err != nil {
		t.Fatalf("takeover b: %v", err)
	}
	if leaseB.FencingToken <= leaseA.FencingToken {
		t.Fatalf("fencing token did not increase: a=%d b=%d", leaseA.FencingToken, leaseB.FencingToken)
	}

	if err := svc.FencedUpdateNode(ctx, stale, nodeID, func(n *Node) error {
		return n.Start(clk.Now())
	}); !errors.Is(err, ErrStaleFence) {
		t.Fatalf("stale FencedUpdateNode err=%v want ErrStaleFence", err)
	}

	fresh := PlanControllerToken{PlanID: leaseB.PlanID, OwnerInstanceID: leaseB.OwnerInstanceID, FencingToken: leaseB.FencingToken}
	if err := svc.FencedUpdateNode(ctx, fresh, nodeID, func(n *Node) error {
		return n.Start(clk.Now())
	}); err != nil {
		t.Fatalf("fresh FencedUpdateNode: %v", err)
	}
	n, _ := svc.GetNode(ctx, nodeID)
	if n.Status() != NodeRunning {
		t.Fatalf("node status=%s want running", n.Status())
	}
}

func TestControllerInboxAndOutboxAreIdempotentAndFenced(t *testing.T) {
	ctx, svc, _, db := controllerSetup(t)
	lease, err := svc.AcquirePlanLease(ctx, AcquireLeaseCommand{PlanID: "plan-io", OwnerInstanceID: "a", TTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	token := PlanControllerToken{PlanID: lease.PlanID, OwnerInstanceID: lease.OwnerInstanceID, FencingToken: lease.FencingToken}

	runs := 0
	for i := 0; i < 2; i++ {
		processed, err := svc.ProcessControllerInboxOnce(ctx, token, "evt-in-1", func(context.Context) error {
			runs++
			return nil
		})
		if err != nil {
			t.Fatalf("ProcessControllerInboxOnce: %v", err)
		}
		if i == 0 && !processed {
			t.Fatal("first inbox delivery should process")
		}
		if i == 1 && processed {
			t.Fatal("duplicate inbox delivery should be skipped")
		}
	}
	if runs != 1 {
		t.Fatalf("inbox function runs=%d want 1", runs)
	}

	for i := 0; i < 2; i++ {
		if err := svc.FencedAppendOutbox(ctx, FencedOutboxCommand{
			Token: token, EventID: "evt-out-1", EventType: "pm.plan.controller_tick",
			Payload: map[string]any{"ok": true},
		}); err != nil {
			t.Fatalf("FencedAppendOutbox: %v", err)
		}
	}
	var count int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM outbox_events WHERE id='evt-out-1'`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("outbox event count=%d want 1", count)
	}
}

func TestPlanControllerWatchdogFindsOwnerAndLagFailures(t *testing.T) {
	ctx, svc, clk, db := controllerSetup(t)

	graphNoOwner, _ := svc.CreateGraph(ctx, "plan-no-owner")
	if err := svc.StartGraph(ctx, graphNoOwner); err != nil {
		t.Fatal(err)
	}
	graphSlow, _ := svc.CreateGraph(ctx, "plan-slow")
	if err := svc.StartGraph(ctx, graphSlow); err != nil {
		t.Fatal(err)
	}
	lease, err := svc.AcquirePlanLease(ctx, AcquireLeaseCommand{PlanID: "plan-slow", OwnerInstanceID: "slow", TTL: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	old := clk.Now().Add(-30 * time.Minute).UTC().Format(time.RFC3339Nano)
	if _, err := db.ExecContext(ctx, `UPDATE pm_plan_controller_leases SET last_renewed_at=? WHERE plan_id=?`, old, lease.PlanID); err != nil {
		t.Fatal(err)
	}

	obs, err := svc.RunPlanControllerWatchdog(ctx, WatchdogConfig{
		LeaseRotationAfter: 5 * time.Minute,
		WatermarkLagAfter:  10 * time.Minute,
		Watermarks:         map[string]time.Time{"outbox": clk.Now().Add(-20 * time.Minute)},
	})
	if err != nil {
		t.Fatalf("watchdog: %v", err)
	}
	kinds := map[string]int{}
	for _, ob := range obs {
		kinds[ob.Kind]++
	}
	if kinds["shard_without_owner"] != 1 || kinds["non_rotating_lease"] != 1 || kinds["reconcile_watermark_lag"] != 1 {
		t.Fatalf("watchdog observations=%v want owner/non-rotating/lag", kinds)
	}
}

func TestWakeBackpressure_StormOverflowAndResume(t *testing.T) {
	ctx, svc, clk, _ := controllerSetup(t)

	for i := 0; i < 1000; i++ {
		sev := WakeSeverityP1
		if i%10 == 0 {
			sev = WakeSeverityP0
		}
		if err := svc.EnqueueWake(ctx, WakeRequest{
			IncidentID: fmt.Sprintf("inc-%04d", i),
			OrgID:      "org-1", Severity: sev, Channel: "agent",
			PlanID:    fmt.Sprintf("plan-%04d", i),
			CreatedAt: clk.Now().Add(time.Duration(i) * time.Millisecond),
			Payload:   map[string]any{"i": i},
		}); err != nil {
			t.Fatalf("EnqueueWake %d: %v", i, err)
		}
	}

	delivered, overflows, err := svc.DrainWakeTokens(ctx, map[WakeBucketKey]WakeBucketCapacity{
		{OrgID: "org-1", Severity: WakeSeverityP0, Channel: "agent"}: {Capacity: 20, P0Reserved: 10},
		{OrgID: "org-1", Severity: WakeSeverityP1, Channel: "agent"}: {Capacity: 40, P0Reserved: 10},
	})
	if err != nil {
		t.Fatalf("DrainWakeTokens: %v", err)
	}
	if len(delivered) != 40 {
		t.Fatalf("delivered=%d want 40", len(delivered))
	}
	p0 := 0
	for _, d := range delivered {
		if d.Severity == WakeSeverityP0 {
			p0++
		}
	}
	if p0 != 20 {
		t.Fatalf("delivered P0=%d want reserved/full P0 capacity 20", p0)
	}
	if len(overflows) != 1 || overflows[0].Count != 960 || overflows[0].MaxSeverity != WakeSeverityP0 || len(overflows[0].AffectedPlans) != 960 {
		t.Fatalf("overflow=%+v want count=960 max=P0 affected=960", overflows)
	}

	if err := svc.ResumeWakeOverflow(ctx, "org-1", "agent"); err != nil {
		t.Fatalf("ResumeWakeOverflow: %v", err)
	}
	delivered, overflows, err = svc.DrainWakeTokens(ctx, map[WakeBucketKey]WakeBucketCapacity{
		{OrgID: "org-1", Severity: WakeSeverityP0, Channel: "agent"}: {Capacity: 1000, P0Reserved: 100},
		{OrgID: "org-1", Severity: WakeSeverityP1, Channel: "agent"}: {Capacity: 1000, P0Reserved: 100},
	})
	if err != nil {
		t.Fatalf("DrainWakeTokens resumed: %v", err)
	}
	if len(overflows) != 0 || len(delivered) != 960 {
		t.Fatalf("after resume delivered=%d overflow=%d, want 960/0", len(delivered), len(overflows))
	}
}
