package projectmanager

import (
	"strings"
	"testing"
	"time"
)

func validFindingInput() NewPlanFindingInput {
	return NewPlanFindingInput{
		ID:        "PF-1",
		PlanID:    "PL-1",
		TaskID:    "T-1",
		ProjectID: "P-1",
		AuthorRef: "agent:ag1",
		Kind:      FindingFact,
		Content:   "the real bug is on the tuple-build path, not the printer",
		CreatedAt: t0,
	}
}

func TestPlanFindingKind_IsValid(t *testing.T) {
	for _, k := range []PlanFindingKind{FindingFact, FindingFailure, FindingConstraint, FindingPatchSummary} {
		if !k.IsValid() {
			t.Errorf("kind %q should be valid", k)
		}
	}
	for _, k := range []PlanFindingKind{"", "bug", "FACT", "note"} {
		if k.IsValid() {
			t.Errorf("kind %q should be invalid", k)
		}
	}
}

func TestNewPlanFinding_Validation(t *testing.T) {
	cases := []struct {
		name string
		mut  func(in *NewPlanFindingInput)
		want error // nil in "want" column means "expect some non-nil error" unless name=="ok"
	}{
		{"ok", func(in *NewPlanFindingInput) {}, nil},
		{"empty id", func(in *NewPlanFindingInput) { in.ID = "" }, nil},
		{"empty plan", func(in *NewPlanFindingInput) { in.PlanID = "" }, ErrPlanFindingNoPlan},
		{"empty task", func(in *NewPlanFindingInput) { in.TaskID = "" }, ErrPlanFindingNoTask},
		{"empty project", func(in *NewPlanFindingInput) { in.ProjectID = "" }, ErrEmptyProjectScope},
		{"bad author", func(in *NewPlanFindingInput) { in.AuthorRef = "nope" }, nil},
		{"empty author", func(in *NewPlanFindingInput) { in.AuthorRef = "" }, nil},
		{"bad kind", func(in *NewPlanFindingInput) { in.Kind = "bug" }, ErrInvalidFindingKind},
		{"empty content", func(in *NewPlanFindingInput) { in.Content = "   " }, ErrEmptyFindingContent},
		{"too long", func(in *NewPlanFindingInput) { in.Content = strings.Repeat("x", MaxFindingContentLen+1) }, ErrFindingContentTooLong},
		{"max length ok", func(in *NewPlanFindingInput) { in.Content = strings.Repeat("x", MaxFindingContentLen) }, nil},
		{"zero created", func(in *NewPlanFindingInput) { in.CreatedAt = time.Time{} }, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			in := validFindingInput()
			c.mut(&in)
			f, err := NewPlanFinding(in)
			if c.name == "ok" || c.name == "max length ok" {
				if err != nil {
					t.Fatalf("want nil err, got %v", err)
				}
				if f == nil || f.ID() == "" {
					t.Fatalf("expected a constructed finding")
				}
				return
			}
			if err == nil {
				t.Fatalf("want error, got nil")
			}
			if c.want != nil && err != c.want {
				t.Fatalf("want %v, got %v", c.want, err)
			}
		})
	}
}

func TestNewPlanFinding_FieldsAndNormalization(t *testing.T) {
	in := validFindingInput()
	in.Content = "  trim me  "
	in.CreatedAt = t0.In(time.FixedZone("x", 3600)) // non-UTC → must normalize
	f, err := NewPlanFinding(in)
	if err != nil {
		t.Fatalf("NewPlanFinding: %v", err)
	}
	if f.Content() != "trim me" {
		t.Errorf("content not trimmed: %q", f.Content())
	}
	if f.CreatedAt().Location() != time.UTC {
		t.Errorf("created_at not UTC: %v", f.CreatedAt().Location())
	}
	if f.Version() != 1 {
		t.Errorf("version = %d, want 1", f.Version())
	}
	if f.PlanID() != "PL-1" || f.TaskID() != "T-1" || f.ProjectID() != "P-1" ||
		f.AuthorRef() != "agent:ag1" || f.Kind() != FindingFact {
		t.Errorf("getter mismatch: %+v", f)
	}
	if f.ID().String() != string(f.ID()) {
		t.Errorf("PlanFindingID.String() mismatch")
	}
}

func TestRehydratePlanFinding(t *testing.T) {
	in := RehydratePlanFindingInput{
		ID: "PF-9", PlanID: "PL-1", TaskID: "T-1", ProjectID: "P-1",
		AuthorRef: "agent:ag1", Kind: FindingFailure, Content: "printer change did not help",
		CreatedAt: t0, Version: 1,
	}
	f, err := RehydratePlanFinding(in)
	if err != nil {
		t.Fatalf("RehydratePlanFinding: %v", err)
	}
	if f.Kind() != FindingFailure || f.Content() != "printer change did not help" {
		t.Errorf("rehydrate mismatch: %+v", f)
	}

	// invalid kind rejected
	bad := in
	bad.Kind = "nope"
	if _, err := RehydratePlanFinding(bad); err != ErrInvalidFindingKind {
		t.Errorf("want ErrInvalidFindingKind, got %v", err)
	}
	// version < 1 rejected
	bad = in
	bad.Version = 0
	if _, err := RehydratePlanFinding(bad); err == nil {
		t.Errorf("want version error, got nil")
	}
}
