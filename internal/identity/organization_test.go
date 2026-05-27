package identity

import (
	"testing"
)

func TestOrganizationFactory_New(t *testing.T) {
	f := OrganizationFactory{}

	t.Run("valid organization", func(t *testing.T) {
		org, err := f.New("my-org", "My Organization", "user-abc12345")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if org.Slug() != "my-org" {
			t.Errorf("expected slug 'my-org', got %s", org.Slug())
		}
		if org.Name() != "My Organization" {
			t.Errorf("expected name 'My Organization', got %s", org.Name())
		}
		if org.IsDeleted() {
			t.Error("expected organization to not be deleted")
		}
	})

	t.Run("invalid slug rejected", func(t *testing.T) {
		cases := []string{"AB", "my_org", "-start", "end-", "ab", ""}
		for _, slug := range cases {
			_, err := f.New(slug, "Name", "user-abc12345")
			if err == nil {
				t.Errorf("expected error for slug %q", slug)
			}
		}
	})

	t.Run("valid slug formats", func(t *testing.T) {
		cases := []string{"abc", "my-org", "org123", "a1b2c3"}
		for _, slug := range cases {
			_, err := f.New(slug, "Name", "user-abc12345")
			if err != nil {
				t.Errorf("unexpected error for slug %q: %v", slug, err)
			}
		}
	})
}

func TestOrganization_SoftDelete(t *testing.T) {
	f := OrganizationFactory{}
	org, _ := f.New("test-org", "Test", "user-abc12345")

	if org.IsDeleted() {
		t.Error("org should not be deleted")
	}
	org.SoftDelete()
	if !org.IsDeleted() {
		t.Error("org should be deleted after SoftDelete")
	}
	if org.DeletedAt() == nil {
		t.Error("deleted_at should be set")
	}
}

func TestValidateSlug(t *testing.T) {
	valid := []string{"abc", "a-b-c", "my-org-123", "a1b", "123"}
	for _, s := range valid {
		if err := ValidateSlug(s); err != nil {
			t.Errorf("expected valid slug %q, got error: %v", s, err)
		}
	}

	invalid := []string{"ab", "", "My-Org", "_org", "-org", "org-", "a b", "org!"}
	for _, s := range invalid {
		if err := ValidateSlug(s); err == nil {
			t.Errorf("expected invalid slug %q, got no error", s)
		}
	}
}
