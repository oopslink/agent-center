package workforce

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func newTestProject(t *testing.T) *Project {
	t.Helper()
	p, err := NewProject(NewProjectInput{
		ID:                  "proj-aabb0011",
		Name:                "Agent Center",
		Tags:                []string{"coding"},
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
	if p.ID() != "proj-aabb0011" {
		t.Fatal()
	}
	tags := p.Tags()
	if len(tags) != 1 || tags[0] != "coding" {
		t.Fatalf("tags = %v", tags)
	}
	if p.Version() != 1 {
		t.Fatal()
	}
}

func TestProject_New_BadID(t *testing.T) {
	cases := []ProjectID{
		"",                    // empty
		"with whitespace",     // whitespace rejected
		ProjectID(strings.Repeat("x", 80)), // over 64 chars
	}
	for _, id := range cases {
		_, err := NewProject(NewProjectInput{
			ID:                  id,
			Name:                "x",
			CreatedByIdentityID: "user:x",
			CreatedAt:           time.Now(),
		})
		if err == nil {
			t.Fatalf("expected error for id %q", id)
		}
	}
}

func TestProject_New_EmptyName(t *testing.T) {
	_, err := NewProject(NewProjectInput{
		ID:                  "proj-aabbccdd",
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
		ID:        "proj-aabbccdd",
		Name:      "P",
		CreatedAt: time.Now(),
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProject_New_RequiresCreatedAt(t *testing.T) {
	_, err := NewProject(NewProjectInput{
		ID:                  "proj-aabbccdd",
		Name:                "P",
		CreatedByIdentityID: "user:x",
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestProject_New_NormalizesTags(t *testing.T) {
	p, err := NewProject(NewProjectInput{
		ID:                  "proj-aabbccdd",
		Name:                "P",
		Tags:                []string{" coding ", "", "coding", "ops"},
		CreatedByIdentityID: "user:x",
		CreatedAt:           time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	tags := p.Tags()
	if len(tags) != 2 || tags[0] != "coding" || tags[1] != "ops" {
		t.Fatalf("tags = %v", tags)
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

func TestProject_Update_AllFields(t *testing.T) {
	p := newTestProject(t)
	name := "n"
	desc := "d"
	tags := []string{"ops", "docs"}
	if err := p.ApplyAndBumpVersion(ProjectUpdateFields{
		Name: &name, Description: &desc, Tags: &tags,
	}, time.Now()); err != nil {
		t.Fatal(err)
	}
	got := p.Tags()
	if p.Name() != "n" || p.Description() != "d" || len(got) != 2 || got[0] != "ops" || got[1] != "docs" {
		t.Fatalf("fields not applied: name=%q desc=%q tags=%v", p.Name(), p.Description(), got)
	}
}

func TestProject_RehydrateBadVersion(t *testing.T) {
	_, err := RehydrateProject(RehydrateProjectInput{ID: "proj-aabbccdd", Version: 0})
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

func TestProjectUpdateFields_IsEmpty(t *testing.T) {
	if !(ProjectUpdateFields{}).IsEmpty() {
		t.Fatal()
	}
	n := "x"
	if (ProjectUpdateFields{Name: &n}).IsEmpty() {
		t.Fatal()
	}
	tags := []string{"x"}
	if (ProjectUpdateFields{Tags: &tags}).IsEmpty() {
		t.Fatal()
	}
}

func TestValidateProjectID_ShapeChecks(t *testing.T) {
	good := []ProjectID{"proj-00000000", "proj-abcdef01", "proj-deadbeef"}
	for _, id := range good {
		if err := ValidateProjectID(id); err != nil {
			t.Fatalf("good id %q rejected: %v", id, err)
		}
	}
}

func TestProjectID_String(t *testing.T) {
	if ProjectID("proj-aabbccdd").String() != "proj-aabbccdd" {
		t.Fatal()
	}
}

func TestNewProjectID_FormatAndUniqueness(t *testing.T) {
	id, err := NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateProjectID(id); err != nil {
		t.Fatalf("generated id failed validation: %v", err)
	}
	if !strings.HasPrefix(string(id), "proj-") {
		t.Fatal("expected proj- prefix")
	}
	// Generate a second id; collision probability is ~1/2^32, so two
	// fresh ids should differ in normal runs.
	id2, err := NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	if id == id2 {
		t.Fatalf("expected distinct ids, got %q twice", id)
	}
}

func TestValidateProjectID_BareWordIsLegal(t *testing.T) {
	// The lenient validator accepts the legacy `bogus`-style ids that
	// fixtures/tests still pass; the prefix invariant is preserved at
	// the generator (NewProjectID), not enforced at the validator. If
	// this assertion ever needs to flip, audit fixture ids first.
	if err := ValidateProjectID(ProjectID("bogus")); err != nil {
		t.Fatalf("bare id should be accepted by lenient validator, got %v", err)
	}
}

func TestValidateProjectID_WhitespaceRejected(t *testing.T) {
	err := ValidateProjectID(ProjectID("with whitespace"))
	if !errors.Is(err, ErrProjectInvalidID) {
		t.Fatalf("expected ErrProjectInvalidID, got %v", err)
	}
}
