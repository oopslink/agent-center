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

func setupReg(t *testing.T) *RegistrationService {
	t.Helper()
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 23, 12, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	sink := observability.NewEventSink(er, er, gen, fc)
	return NewRegistrationService(db, NewSQLiteIdentityRepo(db), sink, gen, fc)
}

func TestRegisterIdentity_Happy(t *testing.T) {
	svc := setupReg(t)
	res, err := svc.RegisterIdentity(context.Background(), RegisterIdentityCommand{
		ID: "user:hayang", DisplayName: "Hayang", Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Identity.Kind() != KindUser || res.EventID == "" {
		t.Fatalf("got %+v", res)
	}
}

func TestRegisterIdentity_KindDerivedFromID(t *testing.T) {
	svc := setupReg(t)
	res, err := svc.RegisterIdentity(context.Background(), RegisterIdentityCommand{
		ID: "agent:s-1", DisplayName: "S", Actor: observability.Actor("system"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Identity.Kind() != KindAgent {
		t.Fatalf("got %s", res.Identity.Kind())
	}
}

func TestRegisterIdentity_BadActor(t *testing.T) {
	svc := setupReg(t)
	_, err := svc.RegisterIdentity(context.Background(), RegisterIdentityCommand{
		ID: "user:x", DisplayName: "x", Actor: observability.Actor(""),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRegisterIdentity_BadID(t *testing.T) {
	svc := setupReg(t)
	_, err := svc.RegisterIdentity(context.Background(), RegisterIdentityCommand{
		ID: "", DisplayName: "x", Actor: observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRegisterIdentity_KindMismatch(t *testing.T) {
	svc := setupReg(t)
	_, err := svc.RegisterIdentity(context.Background(), RegisterIdentityCommand{
		ID: "user:x", Kind: KindAgent, DisplayName: "x", Actor: observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRegisterIdentity_EmptyDisplayName(t *testing.T) {
	svc := setupReg(t)
	_, err := svc.RegisterIdentity(context.Background(), RegisterIdentityCommand{
		ID: "user:x", DisplayName: "", Actor: observability.Actor("user:h"),
	})
	if err == nil {
		t.Fatal()
	}
}

func TestRegisterIdentity_Duplicate(t *testing.T) {
	svc := setupReg(t)
	_, _ = svc.RegisterIdentity(context.Background(), RegisterIdentityCommand{
		ID: "user:dup", DisplayName: "x", Actor: observability.Actor("user:h"),
	})
	_, err := svc.RegisterIdentity(context.Background(), RegisterIdentityCommand{
		ID: "user:dup", DisplayName: "x", Actor: observability.Actor("user:h"),
	})
	if !errors.Is(err, ErrIdentityAlreadyExists) {
		t.Fatalf("got %v", err)
	}
}

func TestNewRegistrationService_NilClock(t *testing.T) {
	db, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_ = persistence.NewMigrator(db).Up(context.Background())
	er, _ := obsqlite.NewEventRepo(context.Background(), db)
	gen := idgen.NewGenerator(clock.SystemClock{})
	sink := observability.NewEventSink(er, er, gen, clock.SystemClock{})
	svc := NewRegistrationService(db, NewSQLiteIdentityRepo(db), sink, gen, nil)
	if svc == nil {
		t.Fatal()
	}
}

func TestKindString(t *testing.T) {
	if KindUser.String() != "user" {
		t.Fatal()
	}
}

func TestIdentityID_String(t *testing.T) {
	if IdentityID("user:x").String() != "user:x" {
		t.Fatal()
	}
}

func TestRegisterAgentIdentityInTx_Happy(t *testing.T) {
	svc := setupReg(t)
	err := svc.RegisterAgentIdentityInTx(context.Background(), "ai-1", "MyAgent", observability.Actor("system"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.identities.FindByID(context.Background(), IdentityID("agent:ai-1"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != KindAgent || got.DisplayName() != "MyAgent" {
		t.Fatalf("got %+v", got)
	}
}

func TestRegisterAgentIdentityInTx_BadActor(t *testing.T) {
	svc := setupReg(t)
	if err := svc.RegisterAgentIdentityInTx(context.Background(), "ai-1", "x", observability.Actor("")); err == nil {
		t.Fatal()
	}
}

func TestRegisterAgentIdentityInTx_BadInstanceID(t *testing.T) {
	svc := setupReg(t)
	if err := svc.RegisterAgentIdentityInTx(context.Background(), "", "x", observability.Actor("system")); err == nil {
		t.Fatal()
	}
}

func TestEnsureSystemIdentity_FirstTime(t *testing.T) {
	svc := setupReg(t)
	if err := svc.EnsureSystemIdentity(context.Background(), observability.Actor("system")); err != nil {
		t.Fatal(err)
	}
	got, err := svc.identities.FindByID(context.Background(), IdentityID("system"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind() != KindSystem {
		t.Fatalf("kind: %s", got.Kind())
	}
}

func TestEnsureSystemIdentity_Idempotent(t *testing.T) {
	svc := setupReg(t)
	_ = svc.EnsureSystemIdentity(context.Background(), observability.Actor("system"))
	if err := svc.EnsureSystemIdentity(context.Background(), observability.Actor("system")); err != nil {
		t.Fatalf("second call: %v", err)
	}
}

func TestEnsureSystemIdentity_BadActor(t *testing.T) {
	svc := setupReg(t)
	if err := svc.EnsureSystemIdentity(context.Background(), observability.Actor("")); err == nil {
		t.Fatal()
	}
}
