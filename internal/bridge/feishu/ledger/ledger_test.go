package ledger_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/bridge/feishu/ledger"
	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newRepo(t *testing.T) (*ledger.SQLiteRepo, *sql.DB, *clock.FakeClock, idgen.Generator) {
	t.Helper()
	path := t.TempDir() + "/ledger.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC))
	return ledger.NewSQLiteRepo(db, fc), db, fc, idgen.NewGenerator(fc)
}

func newRow(t *testing.T, fc *clock.FakeClock, gen idgen.Generator, msgID string) *ledger.FeishuDeliveryLedger {
	t.Helper()
	l, err := ledger.NewLedger(ledger.NewLedgerInput{
		ID: gen.NewULID(), MessageID: msgID, ConversationID: "C-1",
		Channel: "feishu", ThreadKey: "", CreatedAt: fc.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestNewLedgerValidatesRequired(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	for name, in := range map[string]ledger.NewLedgerInput{
		"missing id":              {MessageID: "m", ConversationID: "c", Channel: "feishu", CreatedAt: now},
		"missing message_id":      {ID: "i", ConversationID: "c", Channel: "feishu", CreatedAt: now},
		"missing conversation_id": {ID: "i", MessageID: "m", Channel: "feishu", CreatedAt: now},
		"missing channel":         {ID: "i", MessageID: "m", ConversationID: "c", CreatedAt: now},
		"missing created_at":      {ID: "i", MessageID: "m", ConversationID: "c", Channel: "feishu"},
	} {
		if _, err := ledger.NewLedger(in); err == nil {
			t.Errorf("%s: want err", name)
		}
	}
	if _, err := ledger.NewLedger(ledger.NewLedgerInput{
		ID: "i", MessageID: "m", ConversationID: "c", Channel: "feishu", CreatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
}

func TestDeliveryStatusIsValid(t *testing.T) {
	t.Parallel()
	for _, ok := range []ledger.DeliveryStatus{ledger.StatusPending, ledger.StatusDelivered, ledger.StatusFailed} {
		if !ok.IsValid() {
			t.Errorf("%s should be valid", ok)
		}
	}
	if ledger.DeliveryStatus("weird").IsValid() {
		t.Error("weird should not be valid")
	}
}

func TestAppendAndFind(t *testing.T) {
	r, _, fc, gen := newRepo(t)
	ctx := context.Background()
	row := newRow(t, fc, gen, "M-1")
	if err := r.Append(ctx, row); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByMessageID(ctx, "M-1")
	if err != nil || got.MessageID() != "M-1" || got.Status() != ledger.StatusPending {
		t.Fatalf("got %+v err=%v", got, err)
	}
	byID, err := r.FindByID(ctx, row.ID())
	if err != nil || byID.ID() != row.ID() {
		t.Fatalf("FindByID: %+v err=%v", byID, err)
	}
	// Duplicate Append
	if err := r.Append(ctx, row); !errors.Is(err, ledger.ErrLedgerDuplicate) {
		t.Fatalf("want duplicate err, got %v", err)
	}
	if _, err := r.FindByMessageID(ctx, "missing"); !errors.Is(err, ledger.ErrLedgerNotFound) {
		t.Fatalf("want NotFound, got %v", err)
	}
	if _, err := r.FindByID(ctx, "missing"); !errors.Is(err, ledger.ErrLedgerNotFound) {
		t.Fatalf("want NotFound for FindByID, got %v", err)
	}
}

func TestMarkDeliveredCAS(t *testing.T) {
	r, _, fc, gen := newRepo(t)
	ctx := context.Background()
	row := newRow(t, fc, gen, "M-1")
	if err := r.Append(ctx, row); err != nil {
		t.Fatal(err)
	}
	fc.Advance(time.Second)
	if err := r.MarkDelivered(ctx, row.ID(), row.Version(), "vm_1", "card_1", "thr_1"); err != nil {
		t.Fatalf("mark delivered: %v", err)
	}
	got, _ := r.FindByID(ctx, row.ID())
	if got.Status() != ledger.StatusDelivered || got.VendorMsgRef() != "vm_1" ||
		got.CardMessageID() != "card_1" || got.ThreadKey() != "thr_1" || got.DeliveredAt() == nil {
		t.Fatalf("unexpected state %+v", got)
	}
	// Try again on the now-delivered row with the freshly-bumped version
	// → InvalidTransition (pending → ... is the only legal source state).
	if err := r.MarkDelivered(ctx, row.ID(), got.Version(), "vm_2", "", ""); !errors.Is(err, ledger.ErrLedgerInvalidTransition) {
		t.Fatalf("want InvalidTransition, got %v", err)
	}
	// Stale CAS version after status change → VersionConflict via diagnose path.
	row2 := newRow(t, fc, gen, "M-2")
	if err := r.Append(ctx, row2); err != nil {
		t.Fatal(err)
	}
	// Simulate concurrent UPDATE by bumping version via MarkFailed in the
	// background.
	if err := r.MarkFailed(ctx, row2.ID(), row2.Version(), "first failure"); err != nil {
		t.Fatal(err)
	}
	// MarkDelivered with stale version → VersionConflict.
	if err := r.MarkDelivered(ctx, row2.ID(), row2.Version(), "vm", "", ""); !errors.Is(err, ledger.ErrLedgerVersionConflict) {
		t.Fatalf("want VersionConflict, got %v", err)
	}
	// MarkDelivered with the new (post-fail) version → InvalidTransition
	// because status is no longer pending.
	got2, _ := r.FindByID(ctx, row2.ID())
	if err := r.MarkDelivered(ctx, row2.ID(), got2.Version(), "vm", "", ""); !errors.Is(err, ledger.ErrLedgerInvalidTransition) {
		t.Fatalf("want InvalidTransition, got %v", err)
	}
}

func TestMarkDeliveredNotFoundAndVersionConflict(t *testing.T) {
	r, _, _, _ := newRepo(t)
	ctx := context.Background()
	if err := r.MarkDelivered(ctx, "missing", 1, "vm", "", ""); !errors.Is(err, ledger.ErrLedgerNotFound) {
		t.Fatalf("want NotFound, got %v", err)
	}
}

func TestMarkFailedIncrementsRetry(t *testing.T) {
	r, _, fc, gen := newRepo(t)
	ctx := context.Background()
	row := newRow(t, fc, gen, "M-1")
	if err := r.Append(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := r.MarkFailed(ctx, row.ID(), row.Version(), "5xx exhausted"); err != nil {
		t.Fatal(err)
	}
	got, _ := r.FindByID(ctx, row.ID())
	if got.Status() != ledger.StatusFailed || got.RetryCount() != 1 || got.LastError() != "5xx exhausted" {
		t.Fatalf("unexpected state %+v", got)
	}
}

func TestAppendNilGuard(t *testing.T) {
	r, _, _, _ := newRepo(t)
	if err := r.Append(context.Background(), nil); err == nil {
		t.Fatal("want nil-ledger err")
	}
}

func TestRehydrateRoundTrip(t *testing.T) {
	t.Parallel()
	at := time.Date(2026, 5, 22, 10, 0, 0, 0, time.UTC)
	d := at.Add(time.Minute)
	row := ledger.Rehydrate(ledger.RehydrateInput{
		ID: "L-1", MessageID: "M", ConversationID: "C", Channel: "feishu",
		ThreadKey: "T", VendorMsgRef: "VM", CardMessageID: "CM",
		Status: ledger.StatusDelivered, RetryCount: 2, LastError: "x",
		DeliveredAt: &d, UpdatedAt: at, CreatedAt: at, Version: 3,
	})
	if row.DeliveredAt() == nil || row.RetryCount() != 2 || row.Version() != 3 {
		t.Fatalf("rehydrate lost fields %+v", row)
	}
}
