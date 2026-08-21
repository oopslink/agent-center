package persistence

import "testing"

func latestMigrationVersionForTest(t *testing.T) int {
	t.Helper()
	version, err := LatestMigrationVersion()
	if err != nil {
		t.Fatalf("latest migration version: %v", err)
	}
	return version
}
