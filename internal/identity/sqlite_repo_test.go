package identity

import (
	"context"
	"database/sql"
	"testing"

	"github.com/oopslink/agent-center/internal/persistence"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	m := persistence.NewMigrator(db)
	if err := m.Up(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// ---- Identity repo --------------------------------------------------------

func TestSQLiteIdentityRepo_SaveGetByID(t *testing.T) {
	db := openTestDB(t)
	repo := NewSQLiteIdentityRepo(db)
	ctx := context.Background()

	f := IdentityFactory{}
	id, err := f.NewUser("Alice", "hash123")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repo.Save(ctx, id); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.GetByID(ctx, id.ID())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.DisplayName() != "Alice" {
		t.Errorf("expected Alice, got %s", got.DisplayName())
	}
	if got.Kind() != KindUser {
		t.Errorf("expected kind=user")
	}
}

func TestSQLiteIdentityRepo_GetByDisplayName(t *testing.T) {
	db := openTestDB(t)
	repo := NewSQLiteIdentityRepo(db)
	ctx := context.Background()

	f := IdentityFactory{}
	id, _ := f.NewUser("Bob", "hash")
	repo.Save(ctx, id)

	got, err := repo.GetByDisplayName(ctx, "Bob")
	if err != nil {
		t.Fatalf("GetByDisplayName: %v", err)
	}
	if got.ID() != id.ID() {
		t.Error("id mismatch")
	}
}

func TestSQLiteIdentityRepo_NotFound(t *testing.T) {
	db := openTestDB(t)
	repo := NewSQLiteIdentityRepo(db)
	ctx := context.Background()

	_, err := repo.GetByID(ctx, "user-00000000")
	if err != ErrIdentityNotFound {
		t.Errorf("expected ErrIdentityNotFound, got %v", err)
	}
}

func TestSQLiteIdentityRepo_DuplicateSave(t *testing.T) {
	db := openTestDB(t)
	repo := NewSQLiteIdentityRepo(db)
	ctx := context.Background()

	f := IdentityFactory{}
	id, _ := f.NewUser("Carol", "hash")
	repo.Save(ctx, id)

	err := repo.Save(ctx, id)
	if err != ErrIdentityAlreadyExists {
		t.Errorf("expected ErrIdentityAlreadyExists, got %v", err)
	}
}

func TestSQLiteIdentityRepo_Update(t *testing.T) {
	db := openTestDB(t)
	repo := NewSQLiteIdentityRepo(db)
	ctx := context.Background()

	f := IdentityFactory{}
	id, _ := f.NewUser("Dave", "hash")
	repo.Save(ctx, id)

	id.Disable()
	if err := repo.Update(ctx, id); err != nil {
		t.Fatalf("Update: %v", err)
	}

	got, _ := repo.GetByID(ctx, id.ID())
	if got.AccountStatus() != AccountDisabled {
		t.Error("expected disabled after update")
	}
}

// ---- Organization repo --------------------------------------------------------

func TestSQLiteOrganizationRepo_SaveGetBySlug(t *testing.T) {
	db := openTestDB(t)
	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	ctx := context.Background()

	// Need a valid identity first.
	idf := IdentityFactory{}
	identity, _ := idf.NewUser("Owner", "hash")
	idRepo.Save(ctx, identity)

	f := OrganizationFactory{}
	org, err := f.New("test-org", "Test Org", identity.ID())
	if err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := orgRepo.Save(ctx, org); err != nil {
		t.Fatalf("Save org: %v", err)
	}

	got, err := orgRepo.GetBySlug(ctx, "test-org")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.Name() != "Test Org" {
		t.Errorf("expected 'Test Org', got %s", got.Name())
	}
}

func TestSQLiteOrganizationRepo_SlugUnique(t *testing.T) {
	db := openTestDB(t)
	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	ctx := context.Background()

	idf := IdentityFactory{}
	identity, _ := idf.NewUser("Owner2", "hash")
	idRepo.Save(ctx, identity)

	f := OrganizationFactory{}
	org1, _ := f.New("dup-slug", "First", identity.ID())
	orgRepo.Save(ctx, org1)

	org2, _ := f.New("dup-slug", "Second", identity.ID())
	err := orgRepo.Save(ctx, org2)
	if err != ErrOrganizationSlugTaken {
		t.Errorf("expected ErrOrganizationSlugTaken, got %v", err)
	}
}

// ---- Member repo --------------------------------------------------------

func TestSQLiteMemberRepo_SaveGetByOrganizationAndIdentity(t *testing.T) {
	db := openTestDB(t)
	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	memberRepo := NewSQLiteMemberRepo(db)
	ctx := context.Background()

	idf := IdentityFactory{}
	identity, _ := idf.NewUser("MemberUser", "hash")
	idRepo.Save(ctx, identity)

	orgf := OrganizationFactory{}
	org, _ := orgf.New("member-org", "MemberOrg", identity.ID())
	orgRepo.Save(ctx, org)

	mf := MemberFactory{}
	m, err := mf.New(org.ID(), identity.ID(), RoleOwner, nil)
	if err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := memberRepo.Save(ctx, m); err != nil {
		t.Fatalf("Save member: %v", err)
	}

	got, err := memberRepo.GetByOrganizationAndIdentity(ctx, org.ID(), identity.ID())
	if err != nil {
		t.Fatalf("GetByOrganizationAndIdentity: %v", err)
	}
	if got.Role() != RoleOwner {
		t.Errorf("expected role=owner, got %s", got.Role())
	}
}

func TestSQLiteMemberRepo_CountActiveOwners(t *testing.T) {
	db := openTestDB(t)
	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	memberRepo := NewSQLiteMemberRepo(db)
	ctx := context.Background()

	idf := IdentityFactory{}
	id1, _ := idf.NewUser("Owner1", "hash")
	id2, _ := idf.NewUser("Owner2", "hash")
	idRepo.Save(ctx, id1)
	idRepo.Save(ctx, id2)

	orgf := OrganizationFactory{}
	org, _ := orgf.New("count-org", "CountOrg", id1.ID())
	orgRepo.Save(ctx, org)

	mf := MemberFactory{}
	m1, _ := mf.New(org.ID(), id1.ID(), RoleOwner, nil)
	m2, _ := mf.New(org.ID(), id2.ID(), RoleOwner, nil)
	memberRepo.Save(ctx, m1)
	memberRepo.Save(ctx, m2)

	count, err := memberRepo.CountActiveOwners(ctx, org.ID())
	if err != nil {
		t.Fatalf("CountActiveOwners: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 owners, got %d", count)
	}

	// Disable one owner.
	m1.Disable("test")
	memberRepo.Save(ctx, m1)

	count, _ = memberRepo.CountActiveOwners(ctx, org.ID())
	if count != 1 {
		t.Errorf("expected 1 owner after disable, got %d", count)
	}
}
