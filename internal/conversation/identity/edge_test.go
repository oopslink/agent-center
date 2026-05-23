package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

func TestIsUniqueConstraint_Nil(t *testing.T) {
	if isUniqueConstraint(nil) {
		t.Fatal("nil should return false")
	}
	if !isUniqueConstraint(errors.New("UNIQUE constraint failed: x")) {
		t.Fatal()
	}
	if !isUniqueConstraint(errors.New("constraint failed: UNIQUE x.y")) {
		t.Fatal()
	}
	if !isUniqueConstraint(errors.New("constraint failed: identities.id")) {
		t.Fatal()
	}
	if isUniqueConstraint(errors.New("some other error")) {
		t.Fatal()
	}
}

// TestSave_ExecError exercises the non-unique-constraint INSERT error
// path via a TEMP TRIGGER that always RAISE(ABORT)s.
func TestSave_ExecError(t *testing.T) {
	r := setupIDRepo(t)
	// Install a TEMP TRIGGER that aborts every identities INSERT with a
	// non-unique error so we hit the generic err return branch.
	_, err := r.db.Exec(`CREATE TEMP TRIGGER ban_id_insert BEFORE INSERT ON identities BEGIN
		SELECT RAISE(ABORT, 'forbidden');
	END`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Exec(`DROP TRIGGER IF EXISTS ban_id_insert`)
	err = r.Save(context.Background(), mkID(t, "user:x", KindUser, "n"))
	if err == nil {
		t.Fatal("expected error")
	}
	if errors.Is(err, ErrIdentityAlreadyExists) {
		t.Fatal("non-unique error should not map to ErrIdentityAlreadyExists")
	}
}

// TestUpdate_ExecError covers the UPDATE error path that is neither
// not-found nor version-conflict.
func TestUpdate_ExecError(t *testing.T) {
	r := setupIDRepo(t)
	i := mkID(t, "user:a", KindUser, "x")
	if err := r.Save(context.Background(), i); err != nil {
		t.Fatal(err)
	}
	_, err := r.db.Exec(`CREATE TEMP TRIGGER ban_id_update BEFORE UPDATE ON identities BEGIN
		SELECT RAISE(ABORT, 'forbidden');
	END`)
	if err != nil {
		t.Fatal(err)
	}
	defer r.db.Exec(`DROP TRIGGER IF EXISTS ban_id_update`)
	_ = i.Rename("y", time.Now())
	if err := r.Update(context.Background(), i, 1); err == nil {
		t.Fatal("expected error")
	}
}

// TestFind_QueryError exercises the QueryContext error branch.
func TestFind_QueryError(t *testing.T) {
	r := setupIDRepo(t)
	// Close the DB to force every query to fail.
	r.db.Close()
	_, err := r.Find(context.Background(), IdentityFilter{})
	if err == nil {
		t.Fatal("expected error from closed db")
	}
}

// TestScanIdentity_BadTime covers the time.Parse error branches in
// scanIdentity by inserting rows with bogus timestamps directly.
func TestScanIdentity_BadCreatedAt(t *testing.T) {
	r := setupIDRepo(t)
	_, err := r.db.Exec(`INSERT INTO identities (id, kind, display_name, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"user:bad", "user", "x", "not-a-time", time.Now().UTC().Format(time.RFC3339Nano), 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FindByID(context.Background(), IdentityID("user:bad")); err == nil {
		t.Fatal("expected time parse error")
	}
}

func TestScanIdentity_BadUpdatedAt(t *testing.T) {
	r := setupIDRepo(t)
	_, err := r.db.Exec(`INSERT INTO identities (id, kind, display_name, created_at, updated_at, version)
		VALUES (?, ?, ?, ?, ?, ?)`,
		"user:bad2", "user", "x", time.Now().UTC().Format(time.RFC3339Nano), "not-a-time", 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.FindByID(context.Background(), IdentityID("user:bad2")); err == nil {
		t.Fatal()
	}
}

// failingEventRepo always errors on Append; used to exercise the
// EventSink.Emit error branch.
type failingEventRepo struct{}

func (failingEventRepo) Append(ctx context.Context, e *observability.Event) error {
	return errors.New("forced emit failure")
}
func (failingEventRepo) FindByID(ctx context.Context, id observability.EventID) (*observability.Event, error) {
	return nil, observability.ErrEventNotFound
}
func (failingEventRepo) Find(ctx context.Context, filter observability.EventQueryFilter) ([]*observability.Event, error) {
	return nil, nil
}

func TestRegisterAgentIdentityInTx_EmitFailure(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(failingEventRepo{}, er, gen, fc)
	svc := NewRegistrationService(db, NewSQLiteIdentityRepo(db), sink, gen, fc)
	if err := svc.RegisterAgentIdentityInTx(context.Background(), "ai-1", "x", observability.Actor("system")); err == nil {
		t.Fatal("expected emit failure")
	}
}

func TestRegisterIdentity_EmitFailure(t *testing.T) {
	db, _ := persistence.Open(persistence.MemoryDSN())
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	fc := clock.NewFakeClock(time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(failingEventRepo{}, er, gen, fc)
	svc := NewRegistrationService(db, NewSQLiteIdentityRepo(db), sink, gen, fc)
	_, err := svc.RegisterIdentity(context.Background(), RegisterIdentityCommand{
		ID: "user:x", DisplayName: "n", Actor: observability.Actor("system"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestEnsureSystemIdentity_FindError(t *testing.T) {
	db, _ := persistence.Open(persistence.MemoryDSN())
	_ = persistence.NewMigrator(db).Up(context.Background())
	fc := clock.NewFakeClock(time.Date(2026, 5, 24, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, fc)
	svc := NewRegistrationService(db, NewSQLiteIdentityRepo(db), sink, gen, fc)
	db.Close() // force FindByID error
	if err := svc.EnsureSystemIdentity(context.Background(), observability.Actor("system")); err == nil {
		t.Fatal("expected error from closed db")
	}
}
