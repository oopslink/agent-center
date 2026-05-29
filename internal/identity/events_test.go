package identity

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/observability"
	obsqlite "github.com/oopslink/agent-center/internal/observability/sqlite"
)

// eventSinkSuite wires up a full DB + EventSink for event emission tests.
type eventSinkSuite struct {
	db         *sql.DB
	idRepo     *SQLiteIdentityRepo
	orgRepo    *SQLiteOrganizationRepo
	memberRepo *SQLiteMemberRepo
	eventRepo  *obsqlite.EventRepo
	sink       *observability.EventSink
}

func setupEventSuite(t *testing.T) *eventSinkSuite {
	t.Helper()
	db := openTestDB(t)
	ctx := context.Background()

	fc := clock.NewFakeClock(time.Date(2026, 5, 27, 10, 0, 0, 0, time.UTC))
	gen := idgen.NewGenerator(fc)
	er, err := obsqlite.NewEventRepo(ctx, db)
	if err != nil {
		t.Fatalf("NewEventRepo: %v", err)
	}
	sink := observability.NewEventSink(er, er, gen, fc)

	return &eventSinkSuite{
		db:         db,
		idRepo:     NewSQLiteIdentityRepo(db),
		orgRepo:    NewSQLiteOrganizationRepo(db),
		memberRepo: NewSQLiteMemberRepo(db),
		eventRepo:  er,
		sink:       sink,
	}
}

func (s *eventSinkSuite) assertEventType(ctx context.Context, t *testing.T, evtType observability.EventType) {
	t.Helper()
	all, err := s.eventRepo.Find(ctx, observability.EventQueryFilter{EventType: &evtType})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if len(all) == 0 {
		t.Errorf("expected at least 1 event of type %s, got none", evtType)
	}
}

// TestEventEmission_Signup verifies DS-1 emits identity.created + organization.created + member.added.
func TestEventEmission_Signup(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	svc := NewSignupServiceWithSink(s.db, s.idRepo, s.orgRepo, s.memberRepo, s.sink)
	_, err := svc.Execute(ctx, SignupForm{
		DisplayName: "Alice", PasscodePlain: "123456",
		OrganizationName: "Acme", OrganizationSlug: "acme",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	s.assertEventType(ctx, t, EvtIdentityCreated)
	s.assertEventType(ctx, t, EvtOrganizationCreated)
	s.assertEventType(ctx, t, EvtMemberAdded)
}

// TestEventEmission_Signin verifies auth.signed_in is emitted on success
// and auth.signin_failed on failure.
func TestEventEmission_Signin(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	hash, _ := HashPasscode("123456")
	identity, _ := IdentityFactory{}.NewUser("Bob", hash)
	s.idRepo.Save(ctx, identity)

	key := []byte("test-signing-key")
	svc := NewSigninServiceWithSink(s.idRepo, key, s.sink)

	// Successful signin.
	_, err := svc.Execute(ctx, "Bob", "123456")
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s.assertEventType(ctx, t, EvtAuthSignedIn)

	// Failed signin — bad passcode.
	_, _ = svc.Execute(ctx, "Bob", "wrong!")
	s.assertEventType(ctx, t, EvtAuthSigninFailed)
}

// TestEventEmission_Signout verifies auth.signed_out is emitted.
func TestEventEmission_Signout(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	svc := NewSignoutService(s.sink)
	if err := svc.Execute(ctx, "user-abc12345", "jti-abc"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	s.assertEventType(ctx, t, EvtAuthSignedOut)
}

// TestEventEmission_MemberRoleChanged verifies member.role_changed is emitted.
func TestEventEmission_MemberRoleChanged(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	signupSvc := NewSignupService(s.db, s.idRepo, s.orgRepo, s.memberRepo)
	r, _ := signupSvc.Execute(ctx, SignupForm{
		DisplayName: "Owner1", PasscodePlain: "123456",
		OrganizationName: "O", OrganizationSlug: "org-a",
	})

	idf := IdentityFactory{}
	id2, _ := idf.NewUser("Owner2", "hash")
	s.idRepo.Save(ctx, id2)
	m2, _ := MemberFactory{}.New(r.Organization.ID(), id2.ID(), RoleOwner, nil)
	s.memberRepo.Save(ctx, m2)

	lock := NewOrganizationLockManager()
	svc := NewMemberRoleChangeServiceWithSink(s.db, s.memberRepo, lock, s.sink)
	if err := svc.Change(ctx, m2.ID(), RoleMember, r.Identity.ID()); err != nil {
		t.Fatalf("Change: %v", err)
	}
	s.assertEventType(ctx, t, EvtMemberRoleChanged)
}

// TestEventEmission_MemberDisableReEnable verifies member.disabled and member.re_enabled.
func TestEventEmission_MemberDisableReEnable(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	signupSvc := NewSignupService(s.db, s.idRepo, s.orgRepo, s.memberRepo)
	r, _ := signupSvc.Execute(ctx, SignupForm{
		DisplayName: "Owner3", PasscodePlain: "123456",
		OrganizationName: "P", OrganizationSlug: "org-b",
	})

	idf := IdentityFactory{}
	id2, _ := idf.NewUser("Member1", "hash")
	s.idRepo.Save(ctx, id2)
	m2, _ := MemberFactory{}.New(r.Organization.ID(), id2.ID(), RoleMember, nil)
	s.memberRepo.Save(ctx, m2)

	lock := NewOrganizationLockManager()
	svc := NewMemberDisableServiceWithSink(s.db, s.memberRepo, lock, s.sink)

	if err := svc.Disable(ctx, m2.ID(), "test"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	s.assertEventType(ctx, t, EvtMemberDisabled)

	if err := svc.ReEnable(ctx, m2.ID()); err != nil {
		t.Fatalf("ReEnable: %v", err)
	}
	s.assertEventType(ctx, t, EvtMemberReEnabled)
}

// TestEventEmission_OrganizationDeleted verifies organization.deleted + member.disabled cascade.
func TestEventEmission_OrganizationDeleted(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	signupSvc := NewSignupService(s.db, s.idRepo, s.orgRepo, s.memberRepo)
	r, _ := signupSvc.Execute(ctx, SignupForm{
		DisplayName: "Owner4", PasscodePlain: "123456",
		OrganizationName: "Q", OrganizationSlug: "org-c",
	})

	lock := NewOrganizationLockManager()
	svc := NewOrganizationLifecycleServiceWithSink(s.db, s.orgRepo, s.memberRepo, lock, s.sink)
	if err := svc.Delete(ctx, r.Organization.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	s.assertEventType(ctx, t, EvtOrganizationDeleted)
	s.assertEventType(ctx, t, EvtMemberDisabled)
}

// TestEventEmission_OrganizationUpdated verifies organization.updated is emitted.
func TestEventEmission_OrganizationUpdated(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	signupSvc := NewSignupService(s.db, s.idRepo, s.orgRepo, s.memberRepo)
	r, _ := signupSvc.Execute(ctx, SignupForm{
		DisplayName: "Owner5", PasscodePlain: "123456",
		OrganizationName: "R", OrganizationSlug: "org-d",
	})

	svc := NewOrganizationUpdateServiceWithSink(s.db, s.orgRepo, s.sink)
	if err := svc.UpdateName(ctx, r.Organization.ID(), "Renamed Org", r.Identity.ID()); err != nil {
		t.Fatalf("UpdateName: %v", err)
	}
	s.assertEventType(ctx, t, EvtOrganizationUpdated)
}

// TestEventEmission_MemberRemoved verifies member.removed is emitted.
func TestEventEmission_MemberRemoved(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	signupSvc := NewSignupService(s.db, s.idRepo, s.orgRepo, s.memberRepo)
	r, _ := signupSvc.Execute(ctx, SignupForm{
		DisplayName: "Owner6", PasscodePlain: "123456",
		OrganizationName: "S", OrganizationSlug: "org-e",
	})

	idf := IdentityFactory{}
	id2, _ := idf.NewUser("ToRemove", "hash")
	s.idRepo.Save(ctx, id2)
	m2, _ := MemberFactory{}.New(r.Organization.ID(), id2.ID(), RoleMember, nil)
	s.memberRepo.Save(ctx, m2)

	lock := NewOrganizationLockManager()
	svc := NewMemberRemoveServiceWithSink(s.db, s.memberRepo, lock, s.sink)
	if err := svc.Remove(ctx, m2.ID(), r.Identity.ID()); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	s.assertEventType(ctx, t, EvtMemberRemoved)
}

// TestEventEmission_AgentProvision verifies identity.created (kind=agent) + member.added.
func TestEventEmission_AgentProvision(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	signupSvc := NewSignupService(s.db, s.idRepo, s.orgRepo, s.memberRepo)
	r, _ := signupSvc.Execute(ctx, SignupForm{
		DisplayName: "Owner7", PasscodePlain: "123456",
		OrganizationName: "T", OrganizationSlug: "org-f",
	})

	svc := NewAgentIdentityProvisionServiceWithSink(s.db, s.idRepo, s.memberRepo, s.sink)
	_, err := svc.Provision(ctx, AgentProvisionForm{
		DisplayName: "Bot1", Role: RoleMember,
	}, r.Organization.ID(), r.Identity.ID())
	if err != nil {
		t.Fatalf("Provision: %v", err)
	}

	// Signup used NewSignupService (no sink), so only the provision step emits.
	evtType := EvtIdentityCreated
	all, _ := s.eventRepo.Find(ctx, observability.EventQueryFilter{EventType: &evtType})
	if len(all) == 0 {
		t.Errorf("expected at least 1 identity.created event (agent), got 0")
	}
	var sawAgent bool
	for _, e := range all {
		if k, ok := e.Payload()["kind"]; ok && k == "agent" {
			sawAgent = true
		}
	}
	if !sawAgent {
		t.Error("expected identity.created with kind=agent payload")
	}
}

// TestEventEmission_IdentityAccountDisableReEnable verifies identity.account_disabled + re_enabled.
func TestEventEmission_IdentityAccountDisableReEnable(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	hash, _ := HashPasscode("123456")
	identity, _ := IdentityFactory{}.NewUser("Carol", hash)
	s.idRepo.Save(ctx, identity)

	svc := NewIdentityAccountServiceWithSink(s.db, s.idRepo, s.sink)

	if err := svc.Disable(ctx, identity.ID(), "test", identity.ID()); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	s.assertEventType(ctx, t, EvtIdentityAccountDisabled)

	if err := svc.ReEnable(ctx, identity.ID(), identity.ID()); err != nil {
		t.Fatalf("ReEnable: %v", err)
	}
	s.assertEventType(ctx, t, EvtIdentityAccountReEnabled)
}

// TestEventEmission_PasscodeChanged verifies identity.passcode_changed is emitted.
func TestEventEmission_PasscodeChanged(t *testing.T) {
	s := setupEventSuite(t)
	ctx := context.Background()

	hash, _ := HashPasscode("123456")
	identity, _ := IdentityFactory{}.NewUser("Dave", hash)
	s.idRepo.Save(ctx, identity)

	svc := NewPasscodeChangeServiceWithSink(s.db, s.idRepo, s.sink)
	if err := svc.Change(ctx, identity.ID(), "123456", "654321"); err != nil {
		t.Fatalf("Change: %v", err)
	}
	s.assertEventType(ctx, t, EvtIdentityPasscodeChanged)
}

// TestEventEmission_AllFifteenTypesDefined verifies the 15 constants are all valid EventType values.
func TestEventEmission_AllFifteenTypesDefined(t *testing.T) {
	types := []observability.EventType{
		EvtIdentityCreated,
		EvtIdentityPasscodeChanged,
		EvtIdentityAccountDisabled,
		EvtIdentityAccountReEnabled,
		EvtOrganizationCreated,
		EvtOrganizationUpdated,
		EvtOrganizationDeleted,
		EvtMemberAdded,
		EvtMemberRoleChanged,
		EvtMemberDisabled,
		EvtMemberReEnabled,
		EvtMemberRemoved,
		EvtAuthSignedIn,
		EvtAuthSignedOut,
		EvtAuthSigninFailed,
	}
	if len(types) != 15 {
		t.Fatalf("expected 15 event types, got %d", len(types))
	}
	for _, et := range types {
		if err := et.Validate(); err != nil {
			t.Errorf("EventType %q invalid: %v", et, err)
		}
	}
}
