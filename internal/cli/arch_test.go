package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArch_NoDirectPersistenceOpenInHandlers enforces conventions
// § 0.4 enforce mechanism #1:
//
//	internal/cli/handlers_*.go 出现 persistence.Open = CI fail
//	(白名单：handlers_migrate.go schema 迁移工具 + handlers_system.go
//	 server 启动)
//
// Rationale: CLI handlers MUST go through the admin endpoint
// (AppService transport), not open the sqlite store directly. The
// only legitimate exceptions are:
//
//   - handlers_migrate_v1_to_v2.go — schema migration tool (target
//     might be empty / un-served DB; can't go through admin).
//   - handlers_system.go — `agent-center server` boot path (the
//     process that OWNS the DB and serves the admin endpoint).
//
// Any new handler file that opens sqlite directly = architectural
// regression of the v2.0 GA "CLI bypasses server" defect that this
// rule exists to prevent.
func TestArch_NoDirectPersistenceOpenInHandlers(t *testing.T) {
	whitelist := map[string]bool{
		"handlers_migrate_v1_to_v2.go": true,
		"handlers_system.go":           true,
	}

	matches, err := filepath.Glob("handlers_*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(matches) == 0 {
		t.Fatal("found no handlers_*.go files — wrong cwd?")
	}

	var offenders []string
	for _, f := range matches {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		base := filepath.Base(f)
		if whitelist[base] {
			continue
		}
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if strings.Contains(string(body), "persistence.Open") {
			offenders = append(offenders, base)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("conventions § 0.4 violation — these handler files "+
			"call persistence.Open directly (must go through admin "+
			"endpoint instead): %v\n\n"+
			"If your CLI command genuinely needs raw DB access (rare; "+
			"think hard before assuming this), add it to the whitelist "+
			"in arch_test.go and document why in conventions § 0.4 / "+
			"the file's package comment.",
			offenders)
	}
}
