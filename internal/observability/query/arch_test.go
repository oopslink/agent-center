package query

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestArch_WorkerReadFacesUseWorkforceOnly locks the #140 worker-model
// convergence切口 step-1 invariant (PD口径, #agent-center:44106f11):
//
//	canonical = workforce.Worker. The A+C+D read planes — observability/query
//	(this package: fleet / inspect / query / stats worker projections), admin/api
//	worker handlers (workerMap + FindAll/FindByID/FindByStatus), and the CLI
//	worker handlers (workerToDTO) — MUST read workforce.Worker, never the
//	environment.Worker model. environment.Worker is the SECOND model being
//	retired in #140; this test prevents any worker READ projection from drifting
//	to the environment side while the convergence (step-2/3) is still in flight.
//
// Precision note: admin/api/environment.go LEGITIMATELY references
// environment.WorkerID (the env ConnectWorker bridge, recon touchpoint #13) —
// so we do NOT blanket-forbid "environment." across the admin package. We scope
// the C/D checks to the specific worker-projection files and forbid the swap
// target type "environment.Worker". The A plane (this query package) has ZERO
// environment dependency today, so there we forbid any environment import
// outright — the strictest, false-positive-free lock for the read half I own.
func TestArch_WorkerReadFacesUseWorkforceOnly(t *testing.T) {
	const envPkg = "github.com/oopslink/agent-center/internal/environment"

	// A plane — observability/query: forbid ANY environment dependency.
	queryFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	if len(queryFiles) == 0 {
		t.Fatal("found no *.go in observability/query — wrong cwd?")
	}
	for _, f := range queryFiles {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		body := readFileOrFatal(t, f)
		if strings.Contains(body, envPkg) {
			t.Errorf("#140 step-1 violation: observability/query/%s imports the "+
				"environment package. Worker observability reads MUST stay on the "+
				"canonical workforce.Worker model, never environment.Worker. "+
				"(see #140口径 #agent-center:44106f11)", filepath.Base(f))
		}
	}

	// C/D planes — admin + CLI worker-projection files: forbid the
	// environment.Worker swap target (file-scoped so the env ConnectWorker
	// bridge's legitimate environment.WorkerID use in admin/api/environment.go
	// is NOT a false positive).
	scoped := []string{
		"../../admin/api/workforce.go",
		"../../cli/handlers_worker.go",
	}
	for _, rel := range scoped {
		body := readFileOrFatal(t, rel)
		if strings.Contains(body, "environment.Worker") {
			t.Errorf("#140 step-1 violation: %s references environment.Worker. "+
				"This worker read/projection face MUST read workforce.Worker "+
				"(canonical), never the retiring environment.Worker model. "+
				"(see #140口径 #agent-center:44106f11)", rel)
		}
	}
}

func readFileOrFatal(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}
