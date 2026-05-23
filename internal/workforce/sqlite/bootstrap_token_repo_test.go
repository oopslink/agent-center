package sqlite

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/workforce"
)

func TestBootstrapTokenRepo_SaveAndFindByID(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	tok := newActiveToken(t, "01HID1", "W-1")
	if err := repo.Save(context.Background(), tok); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := repo.FindByID(context.Background(), "01HID1")
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.WorkerID() != "W-1" {
		t.Fatalf("worker_id: %s", got.WorkerID())
	}
	if got.Status() != workforce.BootstrapTokenActive {
		t.Fatalf("status: %s", got.Status())
	}
}

func TestBootstrapTokenRepo_Save_DuplicateValueHash(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	tok1 := newActiveToken(t, "01HID1", "W-1")
	if err := repo.Save(context.Background(), tok1); err != nil {
		t.Fatal(err)
	}
	// Same value_hash with a different id; should hit value_hash unique.
	tok2, err := workforce.NewBootstrapToken(workforce.NewBootstrapTokenInput{
		ID: "01HID2", WorkerID: "W-2",
		ValueHash: tok1.ValueHash(),
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute), CreatedBy: "u",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = repo.Save(context.Background(), tok2)
	if !errors.Is(err, workforce.ErrBootstrapTokenValueHashConflict) {
		t.Fatalf("expected value_hash conflict, got %v", err)
	}
}

func TestBootstrapTokenRepo_Save_ActiveExistsPerWorker(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	tok1 := newActiveToken(t, "01HID1", "W-1")
	if err := repo.Save(context.Background(), tok1); err != nil {
		t.Fatal(err)
	}
	// Second active for the same worker — should fail at DB unique index.
	tok2, err := workforce.NewBootstrapToken(workforce.NewBootstrapTokenInput{
		ID: "01HID2", WorkerID: "W-1",
		ValueHash: workforce.HashTokenValue("other"),
		CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Minute), CreatedBy: "u",
	})
	if err != nil {
		t.Fatal(err)
	}
	err = repo.Save(context.Background(), tok2)
	if !errors.Is(err, workforce.ErrBootstrapTokenActiveExists) {
		t.Fatalf("expected active-per-worker conflict, got %v", err)
	}
}

func TestBootstrapTokenRepo_FindByValueHash(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	tok := newActiveToken(t, "01HID1", "W-1")
	if err := repo.Save(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindByValueHash(context.Background(), tok.ValueHash())
	if err != nil {
		t.Fatalf("FindByValueHash: %v", err)
	}
	if got.ID() != tok.ID() {
		t.Fatalf("id: %s", got.ID())
	}
}

func TestBootstrapTokenRepo_FindByValueHash_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	_, err := repo.FindByValueHash(context.Background(), workforce.HashTokenValue("missing"))
	if !errors.Is(err, workforce.ErrBootstrapTokenNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestBootstrapTokenRepo_FindByWorkerID_AllAndFiltered(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	// One active + one used.
	tokA := newActiveToken(t, "01HID1", "W-1")
	if err := repo.Save(context.Background(), tokA); err != nil {
		t.Fatal(err)
	}
	// Mark used.
	if err := tokA.MarkUsed(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(context.Background(), tokA, workforce.BootstrapTokenActive); err != nil {
		t.Fatal(err)
	}
	tokB := newActiveTokenWithHash(t, "01HID2", "W-1", workforce.HashTokenValue("other"))
	if err := repo.Save(context.Background(), tokB); err != nil {
		t.Fatal(err)
	}
	all, err := repo.FindByWorkerID(context.Background(), "W-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2, got %d", len(all))
	}
	activeOnly, _ := repo.FindByWorkerID(context.Background(), "W-1", workforce.BootstrapTokenActive)
	if len(activeOnly) != 1 || activeOnly[0].ID() != "01HID2" {
		t.Fatalf("active filter: %v", activeOnly)
	}
}

func TestBootstrapTokenRepo_FindActiveByWorkerForUpdate(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	tok := newActiveToken(t, "01HID1", "W-1")
	if err := repo.Save(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	got, err := repo.FindActiveByWorkerForUpdate(context.Background(), "W-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID() != "01HID1" {
		t.Fatalf("id: %s", got.ID())
	}
}

func TestBootstrapTokenRepo_UpdateStatus_HappyPath(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	tok := newActiveToken(t, "01HID1", "W-1")
	if err := repo.Save(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	if err := tok.MarkUsed(time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := repo.UpdateStatus(context.Background(), tok, workforce.BootstrapTokenActive); err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	got, _ := repo.FindByID(context.Background(), "01HID1")
	if got.Status() != workforce.BootstrapTokenUsed {
		t.Fatalf("status: %s", got.Status())
	}
	if got.UsedAt() == nil {
		t.Fatal("used_at must be set")
	}
}

func TestBootstrapTokenRepo_UpdateStatus_PreImageMismatch(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	tok := newActiveToken(t, "01HID1", "W-1")
	if err := repo.Save(context.Background(), tok); err != nil {
		t.Fatal(err)
	}
	if err := tok.MarkUsed(time.Now()); err != nil {
		t.Fatal(err)
	}
	// First update succeeds.
	if err := repo.UpdateStatus(context.Background(), tok, workforce.BootstrapTokenActive); err != nil {
		t.Fatal(err)
	}
	// Second update with pre-image=active fails (already used).
	err := repo.UpdateStatus(context.Background(), tok, workforce.BootstrapTokenActive)
	if !errors.Is(err, workforce.ErrBootstrapTokenStatusConflict) {
		t.Fatalf("expected status conflict, got %v", err)
	}
}

func TestBootstrapTokenRepo_UpdateStatus_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	tok := newActiveToken(t, "01HID1", "W-1")
	// Don't save; UpdateStatus should hit not-found.
	if err := tok.MarkUsed(time.Now()); err != nil {
		t.Fatal(err)
	}
	err := repo.UpdateStatus(context.Background(), tok, workforce.BootstrapTokenActive)
	if !errors.Is(err, workforce.ErrBootstrapTokenNotFound) {
		t.Fatalf("expected not found, got %v", err)
	}
}

func TestBootstrapTokenRepo_FindExpired(t *testing.T) {
	db := openTestDB(t)
	repo := NewBootstrapTokenRepo(db)
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	// Token expiring at now+1min.
	tok1, _ := workforce.NewBootstrapToken(workforce.NewBootstrapTokenInput{
		ID: "01HID1", WorkerID: "W-1",
		ValueHash: workforce.HashTokenValue("a"),
		CreatedAt: now, ExpiresAt: now.Add(time.Minute), CreatedBy: "u",
	})
	if err := repo.Save(context.Background(), tok1); err != nil {
		t.Fatal(err)
	}
	// Token expiring at now+30min.
	tok2, _ := workforce.NewBootstrapToken(workforce.NewBootstrapTokenInput{
		ID: "01HID2", WorkerID: "W-2",
		ValueHash: workforce.HashTokenValue("b"),
		CreatedAt: now, ExpiresAt: now.Add(30 * time.Minute), CreatedBy: "u",
	})
	if err := repo.Save(context.Background(), tok2); err != nil {
		t.Fatal(err)
	}
	// Scan before now+5min — should hit only tok1.
	expired, err := repo.FindExpired(context.Background(), now.Add(5*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(expired) != 1 || expired[0].ID() != "01HID1" {
		t.Fatalf("expired list: %v", expired)
	}
}

func newActiveToken(t *testing.T, id workforce.BootstrapTokenID, workerID workforce.WorkerID) *workforce.BootstrapToken {
	return newActiveTokenWithHash(t, id, workerID, workforce.HashTokenValue("plain-"+string(id)))
}

func newActiveTokenWithHash(t *testing.T, id workforce.BootstrapTokenID, workerID workforce.WorkerID, hash string) *workforce.BootstrapToken {
	t.Helper()
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)
	tok, err := workforce.NewBootstrapToken(workforce.NewBootstrapTokenInput{
		ID:        id,
		WorkerID:  workerID,
		ValueHash: hash,
		CreatedAt: now,
		ExpiresAt: now.Add(30 * time.Minute),
		CreatedBy: "user:hayang",
	})
	if err != nil {
		t.Fatal(err)
	}
	return tok
}
