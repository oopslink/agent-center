package identity

import (
	"context"
	"database/sql"
	"sync"
	"testing"
)

func setupSignedUpOrg(t *testing.T) (*sql.DB, *SQLiteIdentityRepo, *SQLiteOrganizationRepo, *SQLiteMemberRepo, *Organization, *Identity) {
	t.Helper()
	d := openTestDB(t)
	ctx := context.Background()

	idR := NewSQLiteIdentityRepo(d)
	orgR := NewSQLiteOrganizationRepo(d)
	memR := NewSQLiteMemberRepo(d)

	svc := NewSignupService(d, idR, orgR, memR)
	result, err := svc.Execute(ctx, SignupForm{
		DisplayName: "Owner", PasscodePlain: "123456",
		OrganizationName: "Test Org", OrganizationSlug: "test-org",
	})
	if err != nil {
		t.Fatalf("signup: %v", err)
	}
	return d, idR, orgR, memR, result.Organization, result.Identity
}

// ---- MemberRoleChangeService ----

func TestMemberRoleChangeService_Demote(t *testing.T) {
	db, idRepo, _, memberRepo, org, _ := setupSignedUpOrg(t)
	defer db.Close()
	ctx := context.Background()

	// Add a second owner.
	idf := IdentityFactory{}
	id2, _ := idf.NewUser("User2", "hash")
	idRepo.Save(ctx, id2)
	m2, _ := MemberFactory{}.New(org.ID(), id2.ID(), RoleOwner, nil)
	memberRepo.Save(ctx, m2)

	lock := NewOrganizationLockManager()
	svc := NewMemberRoleChangeService(db, memberRepo, lock)

	// Can demote id2 because there's still Owner1.
	if err := svc.Change(ctx, m2.ID(), RoleMember, "admin"); err != nil {
		t.Fatalf("demote second owner: %v", err)
	}
	got, _ := memberRepo.GetByID(ctx, m2.ID())
	if got.Role() != RoleMember {
		t.Errorf("expected role=member, got %s", got.Role())
	}
}

func TestMemberRoleChangeService_LastOwnerRejected(t *testing.T) {
	db, _, _, memberRepo, org, _ := setupSignedUpOrg(t)
	defer db.Close()
	ctx := context.Background()

	lock := NewOrganizationLockManager()
	svc := NewMemberRoleChangeService(db, memberRepo, lock)

	// Get the owner member.
	members, _ := memberRepo.ListByOrganization(ctx, org.ID())
	if len(members) == 0 {
		t.Fatal("no members found")
	}
	ownerMember := members[0]

	err := svc.Change(ctx, ownerMember.ID(), RoleMember, "system")
	if err != ErrLastOwnerCannotChangeRole {
		t.Errorf("expected ErrLastOwnerCannotChangeRole, got %v", err)
	}
}

// ---- MemberDisableService ----

func TestMemberDisableService_DisableAndReEnable(t *testing.T) {
	db, idRepo, _, memberRepo, org, _ := setupSignedUpOrg(t)
	defer db.Close()
	ctx := context.Background()

	// Add a second member to disable.
	idf := IdentityFactory{}
	id2, _ := idf.NewUser("User2", "hash")
	idRepo.Save(ctx, id2)
	m2, _ := MemberFactory{}.New(org.ID(), id2.ID(), RoleMember, nil)
	memberRepo.Save(ctx, m2)

	lock := NewOrganizationLockManager()
	svc := NewMemberDisableService(db, memberRepo, lock)

	if err := svc.Disable(ctx, m2.ID(), "test"); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	got, _ := memberRepo.GetByID(ctx, m2.ID())
	if got.Status() != MemberDisabled {
		t.Error("expected disabled")
	}

	// Idempotent.
	if err := svc.Disable(ctx, m2.ID(), "again"); err != nil {
		t.Fatalf("second Disable should be idempotent: %v", err)
	}

	if err := svc.ReEnable(ctx, m2.ID()); err != nil {
		t.Fatalf("ReEnable: %v", err)
	}
	got, _ = memberRepo.GetByID(ctx, m2.ID())
	if got.Status() != MemberJoined {
		t.Error("expected joined after re-enable")
	}
}

func TestMemberDisableService_LastOwnerRejected(t *testing.T) {
	db, _, _, memberRepo, org, _ := setupSignedUpOrg(t)
	defer db.Close()
	ctx := context.Background()

	lock := NewOrganizationLockManager()
	svc := NewMemberDisableService(db, memberRepo, lock)

	members, _ := memberRepo.ListByOrganization(ctx, org.ID())
	ownerMember := members[0]

	err := svc.Disable(ctx, ownerMember.ID(), "test")
	if err != ErrLastOwnerCannotDisable {
		t.Errorf("expected ErrLastOwnerCannotDisable, got %v", err)
	}
}

// ---- DS-2 race condition test ----

func TestMemberDisableService_DS2_RaceCondition(t *testing.T) {
	// Concurrent disable of two owners — exactly one must fail.
	db, idRepo, _, memberRepo, org, _ := setupSignedUpOrg(t)
	defer db.Close()
	ctx := context.Background()

	// Add second owner.
	idf := IdentityFactory{}
	id2, _ := idf.NewUser("Owner2", "hash")
	idRepo.Save(ctx, id2)
	m2, _ := MemberFactory{}.New(org.ID(), id2.ID(), RoleOwner, nil)
	memberRepo.Save(ctx, m2)

	// Get first owner member.
	members, _ := memberRepo.ListByOrganization(ctx, org.ID())
	var m1 *Member
	for _, m := range members {
		if m.ID() != m2.ID() {
			m1 = m
			break
		}
	}
	if m1 == nil {
		t.Fatal("could not find first owner member")
	}

	lock := NewOrganizationLockManager()
	svc := NewMemberDisableService(db, memberRepo, lock)

	var (
		wg   sync.WaitGroup
		errs [2]error
	)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = svc.Disable(ctx, m1.ID(), "concurrent")
	}()
	go func() {
		defer wg.Done()
		errs[1] = svc.Disable(ctx, m2.ID(), "concurrent")
	}()
	wg.Wait()

	// Exactly one should succeed, the other should fail with last-owner error.
	successes := 0
	for _, err := range errs {
		if err == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Errorf("expected exactly 1 success in concurrent disable, got %d (errs: %v, %v)", successes, errs[0], errs[1])
	}

	// Verify organization still has 1 active owner.
	count, _ := memberRepo.CountActiveOwners(ctx, org.ID())
	if count != 1 {
		t.Errorf("expected 1 active owner after race, got %d", count)
	}
}

// ---- OrganizationLifecycleService ----

func TestOrganizationLifecycleService_Delete(t *testing.T) {
	db, idRepo, orgRepo, memberRepo, org, _ := setupSignedUpOrg(t)
	defer db.Close()
	ctx := context.Background()

	// Add a second member.
	idf := IdentityFactory{}
	id2, _ := idf.NewUser("Member2", "hash")
	idRepo.Save(ctx, id2)
	m2, _ := MemberFactory{}.New(org.ID(), id2.ID(), RoleMember, nil)
	memberRepo.Save(ctx, m2)

	lock := NewOrganizationLockManager()
	svc := NewOrganizationLifecycleService(db, orgRepo, memberRepo, lock)

	if err := svc.Delete(ctx, org.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// Organization should be soft-deleted.
	got, _ := orgRepo.GetByID(ctx, org.ID())
	if !got.IsDeleted() {
		t.Error("expected organization to be deleted")
	}

	// All members should be disabled.
	members, _ := memberRepo.ListByOrganization(ctx, org.ID())
	for _, m := range members {
		if m.Status() == MemberJoined {
			t.Errorf("member %s should be disabled after org delete", m.ID())
		}
	}

	// Idempotent.
	if err := svc.Delete(ctx, org.ID()); err != nil {
		t.Fatalf("second Delete should be idempotent: %v", err)
	}
}
