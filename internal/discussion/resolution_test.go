package discussion

import (
	"errors"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime/dispatch"
)

func TestResolutionKind_IsValidAndTargetStatus(t *testing.T) {
	for _, k := range []ResolutionKind{ResolutionClosedNoAction, ResolutionClosedWithTasks, ResolutionWithdrawn} {
		if !k.IsValid() {
			t.Errorf("expected valid: %s", k)
		}
	}
	if ResolutionKind("bogus").IsValid() {
		t.Fatal("bogus should not be valid")
	}
	if ResolutionClosedNoAction.TargetStatus() != StatusClosedNoAction {
		t.Fatal("no_action target mismatch")
	}
	if ResolutionClosedWithTasks.TargetStatus() != StatusClosedWithTasks {
		t.Fatal("with_tasks target mismatch")
	}
	if ResolutionWithdrawn.TargetStatus() != StatusWithdrawn {
		t.Fatal("withdrawn target mismatch")
	}
	if ResolutionKind("bogus").TargetStatus() != "" {
		t.Fatal("unknown kind must return empty")
	}
	if ResolutionClosedNoAction.String() != "closed_no_action" {
		t.Fatal("string mismatch")
	}
}

func TestResolution_Validate_HappyAndFails(t *testing.T) {
	cases := []struct {
		name     string
		res      Resolution
		wantErr  bool
	}{
		{"happy_no_action", Resolution{Kind: ResolutionClosedNoAction, Summary: "skip"}, false},
		{"happy_with_tasks", Resolution{
			Kind:    ResolutionClosedWithTasks,
			Summary: "go",
			Tasks: []dispatch.IssueConcludeTaskSpec{
				{LocalID: "a", Title: "T1"},
			},
		}, false},
		{"happy_withdrawn", Resolution{Kind: ResolutionWithdrawn, Summary: "pull back"}, false},
		{"bad_kind", Resolution{Kind: "bogus", Summary: "x"}, true},
		{"empty_summary", Resolution{Kind: ResolutionClosedNoAction}, true},
		{"with_tasks_empty_list", Resolution{Kind: ResolutionClosedWithTasks, Summary: "x"}, true},
		{"no_action_with_tasks", Resolution{
			Kind:    ResolutionClosedNoAction,
			Summary: "x",
			Tasks:   []dispatch.IssueConcludeTaskSpec{{LocalID: "a", Title: "t"}},
		}, true},
		{"withdrawn_with_tasks", Resolution{
			Kind:    ResolutionWithdrawn,
			Summary: "x",
			Tasks:   []dispatch.IssueConcludeTaskSpec{{LocalID: "a", Title: "t"}},
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.res.Validate()
			if c.wantErr && err == nil {
				t.Fatal("expected err")
			}
			if c.wantErr && !errors.Is(err, ErrResolutionInvalid) {
				t.Fatalf("want ErrResolutionInvalid, got %v", err)
			}
			if !c.wantErr && err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
		})
	}
}
