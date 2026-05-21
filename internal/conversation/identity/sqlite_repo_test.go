package identity_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/conversation/identity"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
	"github.com/oopslink/agent-center/internal/persistence"
)

type kit struct {
	db      *sql.DB
	clock   *clock.FakeClock
	idgen   idgen.Generator
	idents  *identity.SQLiteIdentityRepo
	binds   *identity.SQLiteChannelBindingRepo
	sink    *observability.EventSink
	events  *obsqlite.EventRepo
	service *identity.RegistrationService
}

func newKit(t *testing.T) *kit {
	t.Helper()
	path := t.TempDir() + "/identity.db"
	db, err := persistence.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := persistence.NewMigrator(db).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc := clock.NewFakeClock(time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, err := obsqlite.NewEventRepo(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	sink := observability.NewEventSink(er, er, gen, fc)
	idents := identity.NewSQLiteIdentityRepo(db)
	binds := identity.NewSQLiteChannelBindingRepo(db)
	svc := identity.NewRegistrationService(db, idents, binds, sink, gen, fc)
	return &kit{db, fc, gen, idents, binds, sink, er, svc}
}

func TestIdentityRepoSaveFindFindByKindAndDuplicate(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	for _, kind := range []identity.Kind{identity.KindUser, identity.KindSupervisor, identity.KindAgent, identity.KindBot} {
		id := "user:" + string(kind)
		if kind == identity.KindSupervisor {
			id = "supervisor:" + string(kind)
		}
		if kind == identity.KindAgent {
			id = "agent:" + string(kind)
		}
		if kind == identity.KindBot {
			id = "bot"
		}
		i, _ := identity.NewIdentity(identity.NewIdentityInput{
			ID: identity.IdentityID(id), Kind: kind, DisplayName: string(kind), CreatedAt: k.clock.Now(),
		})
		if err := k.idents.Save(ctx, i); err != nil {
			t.Fatalf("save %s: %v", id, err)
		}
		if got, err := k.idents.FindByID(ctx, identity.IdentityID(id)); err != nil || got.ID() != identity.IdentityID(id) {
			t.Fatalf("find %s: %v / %+v", id, err, got)
		}
		if err := k.idents.Save(ctx, i); !errors.Is(err, identity.ErrIdentityAlreadyExists) {
			t.Fatalf("dup %s want ErrIdentityAlreadyExists, got %v", id, err)
		}
	}
	// FindByKind
	users, err := k.idents.Find(ctx, identity.IdentityFilter{Kind: kindPtr(identity.KindUser)})
	if err != nil || len(users) != 1 {
		t.Fatalf("find user-kind got %d (%v)", len(users), err)
	}
	// FindByID not found
	if _, err := k.idents.FindByID(ctx, "user:missing"); !errors.Is(err, identity.ErrIdentityNotFound) {
		t.Fatalf("want ErrIdentityNotFound, got %v", err)
	}
}

func kindPtr(k identity.Kind) *identity.Kind { return &k }

func TestRegistrationService_RegisterEmitsEventSameTx(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	res, err := k.service.RegisterIdentity(ctx, identity.RegisterIdentityCommand{
		ID: "user:hayang", Kind: identity.KindUser, DisplayName: "Hayang",
		Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Identity.ID() != "user:hayang" {
		t.Fatalf("id: %v", res.Identity.ID())
	}
	if res.EventID == "" {
		t.Fatal("missing event id")
	}
	// Tx visibility: row + event both present.
	if _, err := k.idents.FindByID(ctx, "user:hayang"); err != nil {
		t.Fatalf("row missing: %v", err)
	}
	events, _ := k.events.Find(ctx, observability.EventQueryFilter{})
	found := false
	for _, e := range events {
		if e.Type() == "identity.registered" && e.Payload()["identity_id"] == "user:hayang" {
			found = true
		}
	}
	if !found {
		t.Fatal("identity.registered event not emitted")
	}
}

func TestRegistrationService_RegisterDuplicateAndKindMismatch(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	_, err := k.service.RegisterIdentity(ctx, identity.RegisterIdentityCommand{
		ID: "user:hayang", Kind: identity.KindUser, DisplayName: "h",
		Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = k.service.RegisterIdentity(ctx, identity.RegisterIdentityCommand{
		ID: "user:hayang", Kind: identity.KindUser, DisplayName: "h",
		Actor: observability.Actor("user:hayang"),
	})
	if !errors.Is(err, identity.ErrIdentityAlreadyExists) {
		t.Fatalf("want ErrIdentityAlreadyExists, got %v", err)
	}
	_, err = k.service.RegisterIdentity(ctx, identity.RegisterIdentityCommand{
		ID: "user:other", Kind: identity.KindSupervisor, DisplayName: "h",
		Actor: observability.Actor("user:hayang"),
	})
	if err == nil {
		t.Fatal("want kind mismatch err")
	}
}

func TestRegistrationService_AutoRegisterReservedForPhase7(t *testing.T) {
	k := newKit(t)
	if _, err := k.service.AutoRegisterFromVendor(context.Background(),
		identity.Channel("feishu"), "ou_x", "x",
		observability.Actor("user:hayang")); err == nil {
		t.Fatal("want Phase 7 reserved err")
	}
}

func TestBindUnbindFindByVendor(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	if _, err := k.service.RegisterIdentity(ctx, identity.RegisterIdentityCommand{
		ID: "user:hayang", Kind: identity.KindUser, DisplayName: "h",
		Actor: observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
	res, err := k.service.BindChannel(ctx, identity.BindChannelCommand{
		IdentityID: "user:hayang", Channel: "feishu", VendorUserID: "ou_x",
		Preferred: true, Actor: observability.Actor("user:hayang"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Binding.IdentityID() != "user:hayang" {
		t.Fatalf("binding id mismatch")
	}
	// FindByVendorUserID
	got, err := k.binds.FindByVendorUserID(ctx, "feishu", "ou_x")
	if err != nil {
		t.Fatal(err)
	}
	if got.IdentityID() != "user:hayang" {
		t.Fatalf("found %s", got.IdentityID())
	}
	// FindPreferred
	pref, err := k.binds.FindPreferred(ctx, "user:hayang", "feishu")
	if err != nil {
		t.Fatal(err)
	}
	if !pref.Preferred() {
		t.Fatal("preferred lost")
	}
	// Duplicate (channel, vendor_user_id) → err
	if _, err := k.service.BindChannel(ctx, identity.BindChannelCommand{
		IdentityID: "user:other2", Channel: "feishu", VendorUserID: "ou_x",
		Actor: observability.Actor("user:hayang"),
	}); !errors.Is(err, identity.ErrIdentityNotFound) {
		// First the unknown identity error wins because we check existence before insert.
		t.Fatalf("expected identity_not_found check to win, got %v", err)
	}
	// register a second identity to actually test the (channel,vendor) UNIQ
	if _, err := k.service.RegisterIdentity(ctx, identity.RegisterIdentityCommand{
		ID: "user:other", Kind: identity.KindUser, DisplayName: "o",
		Actor: observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.service.BindChannel(ctx, identity.BindChannelCommand{
		IdentityID: "user:other", Channel: "feishu", VendorUserID: "ou_x",
		Actor: observability.Actor("user:hayang"),
	}); !errors.Is(err, identity.ErrChannelBindingAlreadyExists) {
		t.Fatalf("want ErrChannelBindingAlreadyExists, got %v", err)
	}
	// Preferred conflict
	if _, err := k.service.BindChannel(ctx, identity.BindChannelCommand{
		IdentityID: "user:hayang", Channel: "feishu", VendorUserID: "ou_other",
		Preferred: true, Actor: observability.Actor("user:hayang"),
	}); !errors.Is(err, identity.ErrChannelBindingPreferredConflict) {
		t.Fatalf("want ErrChannelBindingPreferredConflict, got %v", err)
	}
	// Unbind
	if _, err := k.service.UnbindChannel(ctx, identity.UnbindChannelCommand{
		IdentityID: "user:hayang", Channel: "feishu",
		Actor: observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := k.binds.FindByVendorUserID(ctx, "feishu", "ou_x"); !errors.Is(err, identity.ErrChannelBindingNotFound) {
		t.Fatalf("post-unbind want ErrChannelBindingNotFound, got %v", err)
	}
	if _, err := k.service.UnbindChannel(ctx, identity.UnbindChannelCommand{
		IdentityID: "user:hayang", Channel: "feishu",
		Actor: observability.Actor("user:hayang"),
	}); !errors.Is(err, identity.ErrChannelBindingNotFound) {
		t.Fatalf("idempotent unbind want NotFound, got %v", err)
	}
}

func TestBindChannelMissingIdentity(t *testing.T) {
	k := newKit(t)
	if _, err := k.service.BindChannel(context.Background(), identity.BindChannelCommand{
		IdentityID: "user:ghost", Channel: "feishu", VendorUserID: "ou",
		Actor: observability.Actor("user:hayang"),
	}); !errors.Is(err, identity.ErrIdentityNotFound) {
		t.Fatalf("want ErrIdentityNotFound, got %v", err)
	}
}

func TestIdentityRepoUpdateCASAndVersionConflict(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	i, _ := identity.NewIdentity(identity.NewIdentityInput{
		ID: "user:hayang", Kind: identity.KindUser, DisplayName: "h", CreatedAt: k.clock.Now(),
	})
	if err := k.idents.Save(ctx, i); err != nil {
		t.Fatal(err)
	}
	if err := i.Rename("Hayang!", k.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := k.idents.Update(ctx, i, 1); err != nil {
		t.Fatal(err)
	}
	// Stale version → conflict.
	if err := k.idents.Update(ctx, i, 1); !errors.Is(err, identity.ErrIdentityVersionConflict) {
		t.Fatalf("want version conflict, got %v", err)
	}
	// Missing row
	ghost, _ := identity.NewIdentity(identity.NewIdentityInput{
		ID: "user:ghost", Kind: identity.KindUser, DisplayName: "g", CreatedAt: k.clock.Now(),
	})
	if err := k.idents.Update(ctx, ghost, 1); !errors.Is(err, identity.ErrIdentityNotFound) {
		t.Fatalf("want not found, got %v", err)
	}
}

func TestChannelBindingFindByIdentityAndFindPreferred(t *testing.T) {
	k := newKit(t)
	ctx := context.Background()
	if _, err := k.service.RegisterIdentity(ctx, identity.RegisterIdentityCommand{
		ID: "user:hayang", Kind: identity.KindUser, DisplayName: "h",
		Actor: observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
	// Two non-preferred bindings.
	for _, vu := range []string{"ou_a", "ou_b"} {
		if _, err := k.service.BindChannel(ctx, identity.BindChannelCommand{
			IdentityID: "user:hayang", Channel: "feishu", VendorUserID: vu,
			Actor: observability.Actor("user:hayang"),
		}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := k.binds.FindByIdentityID(ctx, "user:hayang")
	if err != nil || len(all) != 2 {
		t.Fatalf("FindByIdentityID got %d (%v)", len(all), err)
	}
	if _, err := k.binds.FindPreferred(ctx, "user:hayang", "feishu"); !errors.Is(err, identity.ErrChannelBindingNotFound) {
		t.Fatalf("want NotFound for no preferred, got %v", err)
	}
	// Add a preferred + find it.
	if _, err := k.service.BindChannel(ctx, identity.BindChannelCommand{
		IdentityID: "user:hayang", Channel: "feishu", VendorUserID: "ou_c", Preferred: true,
		Actor: observability.Actor("user:hayang"),
	}); err != nil {
		t.Fatal(err)
	}
	pref, err := k.binds.FindPreferred(ctx, "user:hayang", "feishu")
	if err != nil || pref.VendorUserID() != "ou_c" {
		t.Fatalf("pref = %+v (%v)", pref, err)
	}
	if _, err := k.binds.FindByID(ctx, pref.ID()); err != nil {
		t.Fatal(err)
	}
	if _, err := k.binds.FindByID(ctx, "missing"); !errors.Is(err, identity.ErrChannelBindingNotFound) {
		t.Fatalf("want NotFound, got %v", err)
	}
}
