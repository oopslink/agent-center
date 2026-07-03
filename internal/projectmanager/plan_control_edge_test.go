package projectmanager

import (
	"errors"
	"testing"
)

// TestValidateControlEdgeShape covers the T802 add_plan_dependency authoring guard:
// kind enum + conditional-needs-When. Loopback rounds/ancestry are validated
// separately (ValidateLoopback) so the shape check passes a loopback here.
func TestValidateControlEdgeShape(t *testing.T) {
	cases := []struct {
		name string
		dep  Dependency
		want error
	}{
		{"empty kind normalizes to seq", Dependency{}, nil},
		{"explicit seq", Dependency{Kind: EdgeSeq}, nil},
		{"conditional without When", Dependency{Kind: EdgeConditional}, ErrConditionalNeedsWhen},
		{"conditional with When", Dependency{Kind: EdgeConditional, When: "pass"}, nil},
		{"loopback shape ok (rounds checked by ValidateLoopback)", Dependency{Kind: EdgeLoopback}, nil},
		{"unknown kind", Dependency{Kind: EdgeKind("weird")}, ErrInvalidEdgeKind},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := ValidateControlEdgeShape(tc.dep); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateControlEdgeShape(%+v) = %v, want %v", tc.dep, err, tc.want)
			}
		})
	}
}
