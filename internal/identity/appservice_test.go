package identity

import (
	"context"
	"testing"
)

func testSigningKey() []byte {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i + 1)
	}
	return key
}

func TestSignupService_Execute(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	memberRepo := NewSQLiteMemberRepo(db)
	svc := NewSignupService(db, idRepo, orgRepo, memberRepo)

	form := SignupForm{
		DisplayName:      "Hayang",
		PasscodePlain:    "123456",
		OrganizationName: "My Organization",
		OrganizationSlug: "my-org",
	}

	result, err := svc.Execute(ctx, form)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Identity.DisplayName() != "Hayang" {
		t.Errorf("expected Hayang, got %s", result.Identity.DisplayName())
	}
	if result.Organization.Slug() != "my-org" {
		t.Errorf("expected slug my-org, got %s", result.Organization.Slug())
	}
	if result.Member.Role() != RoleOwner {
		t.Errorf("expected role=owner, got %s", result.Member.Role())
	}

	// Verify DB state.
	identity, _ := idRepo.GetByID(ctx, result.Identity.ID())
	if identity == nil {
		t.Error("identity not found in DB")
	}
	org, _ := orgRepo.GetBySlug(ctx, "my-org")
	if org == nil {
		t.Error("organization not found in DB")
	}
	member, _ := memberRepo.GetByOrganizationAndIdentity(ctx, org.ID(), identity.ID())
	if member == nil {
		t.Error("member not found in DB")
	}
}

func TestSignupService_DuplicateDisplayName(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	memberRepo := NewSQLiteMemberRepo(db)
	svc := NewSignupService(db, idRepo, orgRepo, memberRepo)

	form := SignupForm{DisplayName: "User1", PasscodePlain: "123456", OrganizationName: "Org1", OrganizationSlug: "org-one"}
	svc.Execute(ctx, form)

	form2 := SignupForm{DisplayName: "User1", PasscodePlain: "654321", OrganizationName: "Org2", OrganizationSlug: "org-two"}
	_, err := svc.Execute(ctx, form2)
	if err != ErrIdentityDisplayNameTaken {
		t.Errorf("expected ErrIdentityDisplayNameTaken, got %v", err)
	}
}

func TestSignupService_DuplicateSlug(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	memberRepo := NewSQLiteMemberRepo(db)
	svc := NewSignupService(db, idRepo, orgRepo, memberRepo)

	form := SignupForm{DisplayName: "User1", PasscodePlain: "123456", OrganizationName: "Org", OrganizationSlug: "dup-slug"}
	svc.Execute(ctx, form)

	form2 := SignupForm{DisplayName: "User2", PasscodePlain: "654321", OrganizationName: "Org", OrganizationSlug: "dup-slug"}
	_, err := svc.Execute(ctx, form2)
	if err != ErrOrganizationSlugTaken {
		t.Errorf("expected ErrOrganizationSlugTaken, got %v", err)
	}
}

func TestSignupService_Validation(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	memberRepo := NewSQLiteMemberRepo(db)
	svc := NewSignupService(db, idRepo, orgRepo, memberRepo)

	t.Run("invalid passcode", func(t *testing.T) {
		_, err := svc.Execute(ctx, SignupForm{
			DisplayName: "Alice", PasscodePlain: "abc", OrganizationName: "Org", OrganizationSlug: "org",
		})
		if err == nil {
			t.Error("expected error for invalid passcode")
		}
	})

	t.Run("invalid slug", func(t *testing.T) {
		_, err := svc.Execute(ctx, SignupForm{
			DisplayName: "Bob", PasscodePlain: "123456", OrganizationName: "Org", OrganizationSlug: "INVALID",
		})
		if err != ErrOrganizationSlugInvalid {
			t.Errorf("expected ErrOrganizationSlugInvalid, got %v", err)
		}
	})
}

func TestSigninService_Execute(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	memberRepo := NewSQLiteMemberRepo(db)
	signupSvc := NewSignupService(db, idRepo, orgRepo, memberRepo)
	signingKey := testSigningKey()
	signinSvc := NewSigninService(idRepo, signingKey)

	form := SignupForm{DisplayName: "LoginUser", PasscodePlain: "123456", OrganizationName: "Org", OrganizationSlug: "login-org"}
	signupSvc.Execute(ctx, form)

	t.Run("correct credentials", func(t *testing.T) {
		result, err := signinSvc.Execute(ctx, "LoginUser", "123456")
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if result.JWT == "" {
			t.Error("expected non-empty JWT")
		}
		// JWT should be verifiable.
		claims, err := VerifyJWT(result.JWT, signingKey)
		if err != nil {
			t.Fatalf("VerifyJWT: %v", err)
		}
		if claims.Sub == "" {
			t.Error("expected non-empty sub")
		}
	})

	t.Run("wrong passcode", func(t *testing.T) {
		_, err := signinSvc.Execute(ctx, "LoginUser", "000000")
		if err != ErrPasscodeInvalid {
			t.Errorf("expected ErrPasscodeInvalid, got %v", err)
		}
	})

	t.Run("unknown user", func(t *testing.T) {
		_, err := signinSvc.Execute(ctx, "NoSuchUser", "123456")
		if err != ErrPasscodeInvalid {
			t.Errorf("expected ErrPasscodeInvalid, got %v", err)
		}
	})
}

func TestAuthService_AuthenticateToken(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()

	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	memberRepo := NewSQLiteMemberRepo(db)
	signingKey := testSigningKey()

	signupSvc := NewSignupService(db, idRepo, orgRepo, memberRepo)
	signinSvc := NewSigninService(idRepo, signingKey)
	authSvc := NewAuthService(idRepo, signingKey)

	signupSvc.Execute(ctx, SignupForm{
		DisplayName: "AuthUser", PasscodePlain: "123456",
		OrganizationName: "Org", OrganizationSlug: "auth-org",
	})

	sinResult, _ := signinSvc.Execute(ctx, "AuthUser", "123456")

	t.Run("valid token", func(t *testing.T) {
		identity, err := authSvc.AuthenticateToken(ctx, sinResult.JWT)
		if err != nil {
			t.Fatalf("expected success, got: %v", err)
		}
		if identity.DisplayName() != "AuthUser" {
			t.Errorf("expected AuthUser, got %s", identity.DisplayName())
		}
	})

	t.Run("invalid token", func(t *testing.T) {
		_, err := authSvc.AuthenticateToken(ctx, "not.a.jwt")
		if err != ErrUnauthenticated {
			t.Errorf("expected ErrUnauthenticated, got %v", err)
		}
	})

	t.Run("disabled identity", func(t *testing.T) {
		// Disable the user.
		id, _ := idRepo.GetByDisplayName(ctx, "AuthUser")
		id.Disable()
		idRepo.Update(ctx, id)

		_, err := authSvc.AuthenticateToken(ctx, sinResult.JWT)
		if err != ErrUnauthenticated {
			t.Errorf("expected ErrUnauthenticated for disabled identity (DS-4), got %v", err)
		}
	})
}
