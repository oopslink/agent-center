package persistence

import (
	"errors"
	"fmt"
	"testing"
)

func TestIsUniqueViolation(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"unrelated", errors.New("disk full"), false},
		// Plain stringified driver message (the form repo helpers relied on
		// when the typed *sqlite.Error has been flattened by fmt.Errorf %v).
		{"string UNIQUE constraint failed", errors.New("UNIQUE constraint failed: identities.email"), true},
		{"string constraint failed: UNIQUE", errors.New("constraint failed: UNIQUE (2067)"), true},
		// Wrapped stringified message.
		{"wrapped string", fmt.Errorf("insert identity: %v", errors.New("UNIQUE constraint failed: identities.email")), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsUniqueViolation(tc.err); got != tc.want {
				t.Fatalf("IsUniqueViolation(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
