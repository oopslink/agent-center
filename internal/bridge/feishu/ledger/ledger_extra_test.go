package ledger_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/ledger"
	"github.com/oopslink/agent-center/internal/clock"
)

// Rehydrate with empty time pointer + invalid status downstream.
func TestRehydrateEdgeCases(t *testing.T) {
	t.Parallel()
	row := ledger.Rehydrate(ledger.RehydrateInput{
		ID: "L", MessageID: "M", ConversationID: "C", Channel: "feishu",
		Status: ledger.StatusPending, UpdatedAt: time.Now(), CreatedAt: time.Now(), Version: 1,
	})
	if row.DeliveredAt() != nil {
		t.Fatal("delivered_at should be nil")
	}
}

// MarkDelivered + MarkFailed against non-existent row.
func TestMarkOpsAgainstMissing(t *testing.T) {
	r, _, _, _ := newRepo(t)
	ctx := context.Background()
	if err := r.MarkDelivered(ctx, "ghost", 1, "vm", "", ""); !errors.Is(err, ledger.ErrLedgerNotFound) {
		t.Fatalf("MarkDelivered ghost: %v", err)
	}
	if err := r.MarkFailed(ctx, "ghost", 1, "x"); !errors.Is(err, ledger.ErrLedgerNotFound) {
		t.Fatalf("MarkFailed ghost: %v", err)
	}
}

// NewSQLiteRepo nil clock branch.
func TestNewSQLiteRepoNilClock(t *testing.T) {
	r, db, _, _ := newRepo(t)
	_ = r // ensure base path covered already
	r2 := ledger.NewSQLiteRepo(db, nil)
	if r2 == nil {
		t.Fatal("nil repo")
	}
}

// FindByMessageID / FindByID nil-receiver-style guards via empty inputs.
func TestFindOps(t *testing.T) {
	r, _, fc, gen := newRepo(t)
	ctx := context.Background()
	if _, err := r.FindByMessageID(ctx, "M"); !errors.Is(err, ledger.ErrLedgerNotFound) {
		t.Fatalf("want NotFound, got %v", err)
	}
	// FindByID with empty
	if _, err := r.FindByID(ctx, ""); !errors.Is(err, ledger.ErrLedgerNotFound) {
		t.Fatalf("want NotFound, got %v", err)
	}
	_ = fc
	_ = gen
}

// Repository null-time helpers reach the !=nil path via DeliveredAt scan.
func TestAppendNullDeliveredAt(t *testing.T) {
	r, _, fc, gen := newRepo(t)
	ctx := context.Background()
	row := newRow(t, fc, gen, "M-null")
	if err := r.Append(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByMessageID(ctx, "M-null")
	if got.DeliveredAt() != nil {
		t.Fatal("delivered_at should be nil for pending")
	}
}

// SleepWith helper coverage (FakeClock branch — completes immediately).
func TestSleepWithFakeClock(t *testing.T) {
	fc := clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC))
	clock.SleepWith(fc, time.Hour)
	// After SleepWith, fake clock should have advanced by an hour.
	if fc.Now().Hour() != 11 {
		t.Fatalf("fake clock didn't advance: %v", fc.Now())
	}
}

// DeliveryStatus String() coverage.
func TestDeliveryStatusString(t *testing.T) {
	for _, s := range []ledger.DeliveryStatus{ledger.StatusPending, ledger.StatusDelivered, ledger.StatusFailed} {
		if s.String() != string(s) {
			t.Errorf("String() != raw for %s", s)
		}
	}
}

