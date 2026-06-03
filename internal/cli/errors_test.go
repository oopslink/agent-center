package cli

import (
	"errors"
	"testing"
)

func TestMapDomainError_UnknownPhase2(t *testing.T) {
	if _, _, ok := MapDomainError(errors.New("something random")); ok {
		t.Fatal("expected not mapped")
	}
}
