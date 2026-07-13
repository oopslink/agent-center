package sqlite

import (
	"reflect"
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TestBlockedOn_WaitKeysCodec pins the wait_keys JSON codec: an empty list round-trips
// to "" (byte-stable — the idempotent materialize never sees a []-vs-nil diff), a
// populated list round-trips, and malformed stored JSON decodes to nil (defensive).
func TestBlockedOn_WaitKeysCodec(t *testing.T) {
	if got := encodeWaitKeys(nil); got != "" {
		t.Fatalf("encodeWaitKeys(nil) = %q, want \"\"", got)
	}
	if got := decodeWaitKeys(""); got != nil {
		t.Fatalf("decodeWaitKeys(\"\") = %v, want nil", got)
	}
	enc := encodeWaitKeys([]string{"A", "B"})
	if got := decodeWaitKeys(enc); !reflect.DeepEqual(got, []string{"A", "B"}) {
		t.Fatalf("round-trip = %v, want [A B]", got)
	}
	if got := decodeWaitKeys("{not-json"); got != nil {
		t.Fatalf("decodeWaitKeys(malformed) = %v, want nil (defensive)", got)
	}
}

// TestPlanRepo_BlockedOn_RoundTrip pins the I103 §1 BlockedOn store: upsert →
// get/list round-trips every field (incl. the JSON wait_keys and the optional
// deadline/last_probe timestamps), a single-slot latest-wins overwrite, clear, and
// per-plan isolation.
func TestPlanRepo_BlockedOn_RoundTrip(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	const plan = pm.PlanID("PL-bo")
	since := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	deadline := since.Add(2 * time.Hour)
	probe := since.Add(30 * time.Minute)

	b := pm.BlockedOn{
		NodeID:           "node-A",
		TaskID:           "A",
		PlanID:           plan,
		WaitType:         pm.WaitUpstreamCompletion,
		WaitKeys:         []string{"B", "C"},
		TriggerCondition: "all upstream deps complete",
		WaitedSince:      since,
		Deadline:         deadline,
		OnTimeout:        "escalate",
		LastProbeAt:      probe,
		ProbeCount:       3,
	}
	if err := pr.UpsertBlockedOn(ctx, b); err != nil {
		t.Fatalf("UpsertBlockedOn: %v", err)
	}

	got, ok, err := pr.GetBlockedOn(ctx, plan, "A")
	if err != nil || !ok {
		t.Fatalf("GetBlockedOn = (%+v, %v, %v), want ok", got, ok, err)
	}
	if !reflect.DeepEqual(got, b) {
		t.Fatalf("round-trip mismatch:\n got=%+v\nwant=%+v", got, b)
	}

	// Absent slot → ok=false, no error.
	if _, ok, err := pr.GetBlockedOn(ctx, plan, "nope"); err != nil || ok {
		t.Fatalf("GetBlockedOn(absent) = (ok=%v, err=%v), want (false, nil)", ok, err)
	}

	// Single-slot latest-wins: a second upsert for the SAME (plan,task) overwrites.
	b2 := b
	b2.WaitType = pm.WaitAcceptanceVerdict
	b2.WaitKeys = nil
	b2.TriggerCondition = "acceptance verdict passes"
	b2.ProbeCount = 0
	if err := pr.UpsertBlockedOn(ctx, b2); err != nil {
		t.Fatalf("UpsertBlockedOn(overwrite): %v", err)
	}
	got2, _, _ := pr.GetBlockedOn(ctx, plan, "A")
	if got2.WaitType != pm.WaitAcceptanceVerdict || got2.WaitKeys != nil || got2.ProbeCount != 0 {
		t.Fatalf("overwrite not latest-wins: %+v", got2)
	}
	// Still exactly one row for the plan (not two).
	if list, _ := pr.ListBlockedOn(ctx, plan); len(list) != 1 {
		t.Fatalf("ListBlockedOn after overwrite = %d rows, want 1", len(list))
	}

	// Clear removes the slot; clearing again is idempotent (no error).
	if err := pr.ClearBlockedOn(ctx, plan, "A"); err != nil {
		t.Fatalf("ClearBlockedOn: %v", err)
	}
	if _, ok, _ := pr.GetBlockedOn(ctx, plan, "A"); ok {
		t.Fatal("slot still present after clear")
	}
	if err := pr.ClearBlockedOn(ctx, plan, "A"); err != nil {
		t.Fatalf("ClearBlockedOn(absent) = %v, want nil (idempotent)", err)
	}
}

// TestPlanRepo_BlockedOn_ListIsolationAndOrder pins ListBlockedOn: it is scoped to
// one plan (no cross-plan leakage) and stable-ordered by task_id.
func TestPlanRepo_BlockedOn_ListIsolationAndOrder(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	since := time.Date(2026, 7, 13, 10, 0, 0, 0, time.UTC)
	mk := func(plan pm.PlanID, task pm.TaskID, wt pm.WaitType) pm.BlockedOn {
		return pm.BlockedOn{NodeID: "n-" + string(task), TaskID: task, PlanID: plan, WaitType: wt, WaitedSince: since}
	}
	if err := pr.UpsertBlockedOn(ctx, mk("PL-1", "C", pm.WaitTimeoutOnly)); err != nil {
		t.Fatal(err)
	}
	if err := pr.UpsertBlockedOn(ctx, mk("PL-1", "A", pm.WaitUpstreamCompletion)); err != nil {
		t.Fatal(err)
	}
	if err := pr.UpsertBlockedOn(ctx, mk("PL-2", "Z", pm.WaitStageBarrier)); err != nil {
		t.Fatal(err)
	}

	list, err := pr.ListBlockedOn(ctx, "PL-1")
	if err != nil {
		t.Fatalf("ListBlockedOn: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("ListBlockedOn(PL-1) = %d, want 2 (PL-2 must not leak)", len(list))
	}
	if list[0].TaskID != "A" || list[1].TaskID != "C" {
		t.Fatalf("ListBlockedOn not ordered by task_id: %v, %v", list[0].TaskID, list[1].TaskID)
	}
}

// TestPlanRepo_BlockedOn_DeletePlanCascade proves DeletePlan removes the plan's
// BlockedOn snapshots (they must not outlive the plan).
func TestPlanRepo_BlockedOn_DeletePlanCascade(t *testing.T) {
	ctx, pr, _ := planSetup(t)
	p, _ := pm.NewPlan(pm.NewPlanInput{ID: "PL-del", ProjectID: "P1", Name: "n", CreatorRef: "user:a", CreatedAt: t0})
	if err := pr.Save(ctx, p); err != nil {
		t.Fatalf("Save plan: %v", err)
	}
	if err := pr.UpsertBlockedOn(ctx, pm.BlockedOn{TaskID: "A", PlanID: "PL-del", WaitType: pm.WaitTimeoutOnly, WaitedSince: t0}); err != nil {
		t.Fatal(err)
	}
	if err := pr.DeletePlan(ctx, "PL-del"); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}
	if list, _ := pr.ListBlockedOn(ctx, "PL-del"); len(list) != 0 {
		t.Fatalf("BlockedOn survived DeletePlan: %d rows", len(list))
	}
}
