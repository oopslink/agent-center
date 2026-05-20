package workforce

import (
	"errors"
	"testing"
	"time"
)

func newTestProject(t *testing.T) *Project {
	t.Helper()
	p, err := NewProject(NewProjectInput{
		ID:                  "agent-center",
		Name:                "Agent Center",
		Kind:                ProjectKindCoding,
		CreatedByIdentityID: "user:hayang",
		CreatedAt:           time.Now(),
	})
	if err != nil {
		t.Fatalf("NewProject: %v", err)
	}
	return p
}

func TestProject_New_Happy(t *testing.T) {
	p := newTestProject(t)
	if p.ID() != "agent-center" {
		t.Fatal()
	}
	if p.Kind() != ProjectKindCoding {
		t.Fatal()
	}
	if p.Version() != 1 {
		t.Fatal()
	}
}

func TestProject_New_BadSlug(t *testing.T) {
	for _, slug := range []ProjectID{"", "UPPER", "with space", "with_underscore", "-leading", "trailing-", "with/slash"} {
		_, err := NewProject(NewProjectInput{
			ID:                  slug,
			Name:                "x",
			CreatedByIdentityID: "user:x",
			CreatedAt:           time.Now(),
		})
		if !errors.Is(err, ErrProjectInvalidSlug) {
			t.Fatalf("expected slug error for %q, got %v", slug, err)
		}
	}
}

func TestProject_New_BadKind(t *testing.T) {
	_, err := NewProject(NewProjectInput{
		ID:                  "p",
		Name:                "P",
		Kind:                "unicorns",
		CreatedByIdentityID: "user:x",
		CreatedAt:           time.Now(),
	})
	if !errors.Is(err, ErrProjectInvalidKind) {
		t.Fatalf("expected kind error, got %v", err)
	}
}

func TestProject_New_EmptyName(t *testing.T) {
	_, err := NewProject(NewProjectInput{
		ID:                  "p",
		Name:                "",
		CreatedByIdentityID: "user:x",
		CreatedAt:           time.Now(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProject_New_RequiresIdentity(t *testing.T) {
	_, err := NewProject(NewProjectInput{
		ID:        "p",
		Name:      "P",
		CreatedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProject_New_RequiresCreatedAt(t *testing.T) {
	_, err := NewProject(NewProjectInput{
		ID:                  "p",
		Name:                "P",
		CreatedByIdentityID: "user:x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProject_New_AllowsNullKind(t *testing.T) {
	_, err := NewProject(NewProjectInput{
		ID:                  "p",
		Name:                "P",
		Kind:                "",
		CreatedByIdentityID: "user:x",
		CreatedAt:           time.Now(),
	})
	if err != nil {
		t.Fatalf("null kind should be allowed: %v", err)
	}
}

func TestProject_Update_BumpsVersion(t *testing.T) {
	p := newTestProject(t)
	newName := "Renamed"
	at := time.Now().Add(time.Hour)
	err := p.ApplyAndBumpVersion(ProjectUpdateFields{Name: &newName}, at)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name() != "Renamed" {
		t.Fatal()
	}
	if p.Version() != 2 {
		t.Fatalf("version: %d", p.Version())
	}
}

func TestProject_Update_EmptyFields(t *testing.T) {
	p := newTestProject(t)
	err := p.ApplyAndBumpVersion(ProjectUpdateFields{}, time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
	if p.Version() != 1 {
		t.Fatal("version should not bump")
	}
}

func TestProject_Update_EmptyName(t *testing.T) {
	p := newTestProject(t)
	empty := "  "
	err := p.ApplyAndBumpVersion(ProjectUpdateFields{Name: &empty}, time.Now())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProject_Update_BadKind(t *testing.T) {
	p := newTestProject(t)
	bad := ProjectKind("bogus")
	err := p.ApplyAndBumpVersion(ProjectUpdateFields{Kind: &bad}, time.Now())
	if !errors.Is(err, ErrProjectInvalidKind) {
		t.Fatalf("got %v", err)
	}
}

func TestProject_Update_AllFields(t *testing.T) {
	p := newTestProject(t)
	name := "n"
	kind := ProjectKindWriting
	cli := "codex"
	desc := "d"
	if err := p.ApplyAndBumpVersion(ProjectUpdateFields{
		Name: &name, Kind: &kind, DefaultAgentCLI: &cli, Description: &desc,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if p.Name() != "n" || p.Kind() != "writing" || p.DefaultAgentCLI() != "codex" || p.Description() != "d" {
		t.Fatal("fields not applied")
	}
}

func TestProject_RehydrateBadVersion(t *testing.T) {
	_, err := RehydrateProject(RehydrateProjectInput{ID: "p", Version: 0})
	if err == nil {
		t.Fatal()
	}
}

func TestProject_RehydrateNoID(t *testing.T) {
	_, err := RehydrateProject(RehydrateProjectInput{Version: 1})
	if err == nil {
		t.Fatal()
	}
}

func TestProjectKind_Validation(t *testing.T) {
	if !ProjectKindCoding.IsValid() {
		t.Fatal()
	}
	if !ProjectKind("").IsValid() {
		t.Fatal("empty kind should be valid")
	}
	if ProjectKind("nope").IsValid() {
		t.Fatal()
	}
	if ProjectKindCoding.String() != "coding" {
		t.Fatal()
	}
}

func TestProjectUpdateFields_IsEmpty(t *testing.T) {
	if !(ProjectUpdateFields{}).IsEmpty() {
		t.Fatal()
	}
	n := "x"
	if (ProjectUpdateFields{Name: &n}).IsEmpty() {
		t.Fatal()
	}
}

func TestValidateProjectSlug_TooLong(t *testing.T) {
	long := ""
	for i := 0; i < 200; i++ {
		long += "a"
	}
	if err := ValidateProjectSlug(ProjectID(long)); !errors.Is(err, ErrProjectInvalidSlug) {
		t.Fatalf("got %v", err)
	}
}

func TestProjectID_String(t *testing.T) {
	if ProjectID("foo").String() != "foo" {
		t.Fatal()
	}
}
