package identity

import (
	"context"
	"errors"
	"testing"
)

// v2.7.1 #214: email persists + is unique (dup → ErrIdentityEmailTaken), multiple
// email-less users coexist (upgrade safety / multi-NULL), and signin stamps
// last_session_at (NULL → timestamp).
func TestV271_EmailAndLastSession(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	idRepo := NewSQLiteIdentityRepo(db)
	orgRepo := NewSQLiteOrganizationRepo(db)
	memberRepo := NewSQLiteMemberRepo(db)
	signupSvc := NewSignupService(db, idRepo, orgRepo, memberRepo)
	signinSvc := NewSigninService(idRepo, testSigningKey())

	res, err := signupSvc.Execute(ctx, SignupForm{
		DisplayName: "Alice", PasscodePlain: "Passw0rd1!",
		OrganizationName: "Org", OrganizationSlug: "org-a", Email: "alice@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := idRepo.GetByID(ctx, res.Identity.ID())
	if err != nil {
		t.Fatal(err)
	}
	if got.Email() == nil || *got.Email() != "alice@example.com" {
		t.Fatalf("email not persisted: %v", got.Email())
	}
	if got.LastSessionAt() != nil {
		t.Fatalf("last_session_at should be nil before any signin, got %v", got.LastSessionAt())
	}

	// signin stamps last_session_at (NULL → timestamp).
	if _, err := signinSvc.Execute(ctx, "Alice", "Passw0rd1!"); err != nil {
		t.Fatal(err)
	}
	got2, _ := idRepo.GetByID(ctx, res.Identity.ID())
	if got2.LastSessionAt() == nil {
		t.Fatal("last_session_at should be stamped after signin")
	}

	// duplicate email → ErrIdentityEmailTaken (not a generic conflict).
	_, derr := signupSvc.Execute(ctx, SignupForm{
		DisplayName: "Bob", PasscodePlain: "Passw0rd1!",
		OrganizationName: "Org2", OrganizationSlug: "org-b", Email: "alice@example.com",
	})
	if !errors.Is(derr, ErrIdentityEmailTaken) {
		t.Fatalf("dup email → want ErrIdentityEmailTaken, got %v", derr)
	}

	// Upgrade safety: multiple email-less users coexist (multi-NULL, no unique violation).
	if _, err := signupSvc.Execute(ctx, SignupForm{
		DisplayName: "NoEmail1", PasscodePlain: "Passw0rd1!", OrganizationName: "O3", OrganizationSlug: "org-three",
	}); err != nil {
		t.Fatalf("first NULL-email signup: %v", err)
	}
	if _, err := signupSvc.Execute(ctx, SignupForm{
		DisplayName: "NoEmail2", PasscodePlain: "Passw0rd1!", OrganizationName: "O4", OrganizationSlug: "org-four",
	}); err != nil {
		t.Fatalf("second NULL-email signup must NOT collide on NULL: %v", err)
	}
}

// v2.7.1 #214: email shape validation (light check, not verification).
func TestV271_ValidateEmail(t *testing.T) {
	for _, e := range []string{"a@b.co", "x.y+z@sub.example.com"} {
		if err := validateEmail(e); err != nil {
			t.Errorf("want valid %q: %v", e, err)
		}
	}
	for _, e := range []string{"", "noat", "a@", "@b.com", "a@b", "a b@c.com", "a@@b.com", "a@b."} {
		if err := validateEmail(e); err == nil {
			t.Errorf("want invalid: %q", e)
		}
	}
}
