package api

import (
	"os"
	"strings"
	"testing"
)

func TestPermissionsHandlersUseResolveEffectiveNotLegacyCheckExplain(t *testing.T) {
	body, err := os.ReadFile("handlers_permissions.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(body)
	for _, forbidden := range []string{".Check(", ".Explain("} {
		if strings.Contains(src, forbidden) {
			t.Fatalf("permissions handlers must use ResolveEffective, found %s", forbidden)
		}
	}
	if count := strings.Count(src, ".ResolveEffective("); count < 2 {
		t.Fatalf("permissions check/explain handlers should call ResolveEffective, count=%d", count)
	}
}
