package sqlite

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// issue-f30b7e7b: the persisted delivery (executor terminal git status) + fruitless_
// reopens columns round-trip through Save/Update/scan, and Complete zeroes the tally.
func TestTaskRepo_Delivery_RoundTrip(t *testing.T) {
	ctx, _, _, _, tr, _, _, _ := setup(t)
	tk, err := pm.NewTask(pm.NewTaskInput{ID: "T1", ProjectID: "P1", Title: "do", CreatedBy: "user:a", CreatedAt: t0})
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.Save(ctx, tk); err != nil {
		t.Fatal(err)
	}

	// Fresh task: no delivery reported, zero fruitless tally.
	got, _ := tr.FindByID(ctx, "T1")
	if got.Delivery() != nil {
		t.Fatalf("new task delivery = %+v, want nil", got.Delivery())
	}
	if got.Delivery().HasValidDelivery() {
		t.Fatalf("nil delivery must not be valid")
	}
	if got.FruitlessReopens() != 0 {
		t.Fatalf("new task fruitless_reopens = %d, want 0", got.FruitlessReopens())
	}

	// Committed-but-not-pushed (the review-only zero-delivery signature) round-trips
	// and is NOT a valid delivery.
	got.SetDelivery(&pm.Delivery{
		Probed: true, Pushed: false, Dirty: false,
		Branch: "feat/x", HeadSHA: "abc123", BaseRef: "main", BaseKnown: true, AheadOfBase: 2,
		// PushError (issue-f30b7e7b): the DURABLE record of WHY the push failed must persist.
		PushError: "eager-push refused: HEAD on \"main\"",
	})
	got.NoteFruitlessReopen()
	got.NoteFruitlessReopen()
	if err := tr.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := tr.FindByID(ctx, "T1")
	d := re.Delivery()
	if d == nil || !d.Probed || d.Pushed || d.AheadOfBase != 2 || d.Branch != "feat/x" || d.HeadSHA != "abc123" || d.BaseRef != "main" || !d.BaseKnown {
		t.Fatalf("delivery round-trip = %+v, want probed/!pushed/ahead=2/feat/x/abc123", d)
	}
	if d.PushError != "eager-push refused: HEAD on \"main\"" {
		t.Fatalf("delivery PushError must round-trip through the DB, got %q", d.PushError)
	}
	if d.HasValidDelivery() {
		t.Fatalf("committed-but-not-pushed must NOT be a valid delivery")
	}
	if re.FruitlessReopens() != 2 {
		t.Fatalf("fruitless_reopens round-trip = %d, want 2", re.FruitlessReopens())
	}

	// A durable pushed delivery IS valid.
	re.SetDelivery(&pm.Delivery{Probed: true, Pushed: true, Branch: "feat/x", HeadSHA: "def456"})
	if err := tr.Update(ctx, re); err != nil {
		t.Fatal(err)
	}
	pushed, _ := tr.FindByID(ctx, "T1")
	if !pushed.Delivery().HasValidDelivery() {
		t.Fatalf("probed && pushed must be a valid delivery: %+v", pushed.Delivery())
	}

	// Complete zeroes the fruitless tally (a valid forward progress).
	if err := pushed.SetStatus(pm.TaskRunning, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := pushed.Complete("user:a", t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := tr.Update(ctx, pushed); err != nil {
		t.Fatal(err)
	}
	done, _ := tr.FindByID(ctx, "T1")
	if done.FruitlessReopens() != 0 {
		t.Fatalf("after complete fruitless_reopens = %d, want 0", done.FruitlessReopens())
	}
}
