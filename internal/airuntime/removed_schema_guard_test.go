package airuntime

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestRemovedRuntimeSchemaHasNoActiveConsumers prevents the deleted runtime
// indirection from returning through a DTO, route, UI fixture, or production
// repository. Published migrations and the fail-closed import/migration guards
// are the only intentional compatibility boundary.
func TestRemovedRuntimeSchemaHasNoActiveConsumers(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test path")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	allowed := map[string]bool{
		"internal/airuntime/removed_schema_guard_test.go":                          true,
		"internal/persistence/migration_0116_ai_runtime_test.go":                   true,
		"internal/persistence/migrations/0116_ai_runtime_catalog.down.sql":         true,
		"internal/persistence/migrations/0116_ai_runtime_catalog.up.sql":           true,
		"internal/persistence/migrations/0126_remove_ai_runtime_profiles.down.sql": true,
		"internal/persistence/migrations/0126_remove_ai_runtime_profiles.up.sql":   true,
		"internal/webconsole/api/handlers_ai_runtime.go":                           true,
		"internal/webconsole/api/handlers_ai_runtime_test.go":                      true,
	}
	forbidden := []string{
		"ai_runtime_" + "profiles",
		"default_runtime_" + "profile_id",
		"default_" + "profile_key",
		"/ai-runtime/" + "profiles",
	}
	for _, dir := range []string{"internal", "web/src"} {
		err := filepath.WalkDir(filepath.Join(root, dir), func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if entry.IsDir() {
				if entry.Name() == "node_modules" || entry.Name() == "dist" {
					return filepath.SkipDir
				}
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil || allowed[filepath.ToSlash(rel)] {
				return err
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, token := range forbidden {
				if strings.Contains(string(body), token) {
					t.Errorf("removed runtime schema token %q remains in active file %s", token, filepath.ToSlash(rel))
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
