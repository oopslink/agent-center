package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/workforce"
)

// v2.7 #148 (#108 gate follow-up): a worker that enrolled via a bare admin token
// (never through the org-domain install-command path) has no organization_id.
// Connecting it to the control channel is a missing-precondition, not a server
// fault — the handler must reject with a clear 4xx, not let the empty-org error
// bubble up to a 500 internal error.

// orglessWorker seeds an org-less workforce.Worker into deps.WorkerRepo and
// returns its id.
func orglessWorker(t *testing.T, deps HandlerDeps, id string) string {
	t.Helper()
	clk := clock.NewFakeClock(time.Date(2026, 5, 30, 12, 0, 0, 0, time.UTC))
	w, err := workforce.NewWorker(workforce.NewWorkerInput{
		ID:         workforce.WorkerID(id),
		EnrolledAt: clk.Now(),
		// OrganizationID intentionally empty (bare admin-token self-enroll).
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := deps.WorkerRepo.Save(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestEnvWorkerConnect_OrgLessWorker_Returns409NotInternalError(t *testing.T) {
	deps, _ := newEnvTestDeps(t)
	id := orglessWorker(t, deps, "w-orgless")
	srv := envServer(t, deps)

	status, out := envPost(t, srv.URL, "/admin/environment/worker/connect",
		map[string]any{"worker_id": id})
	if status != http.StatusConflict {
		t.Fatalf("org-less connect: status=%d body=%v, want 409 Conflict (not a 500)", status, out)
	}
	// Must carry a clear client-error code, not an opaque internal failure.
	if out["error"] == nil || out["error"] == "" {
		t.Fatalf("expected an error code in body, got %v", out)
	}
}

func TestEnvWorkerConnect_OrgEnrolledWorker_Still200(t *testing.T) {
	// Regression guard: the org-enrolled worker (w-1/org-1, seeded by
	// newEnvTestDeps) must still connect successfully — the precondition check
	// must not narrow the happy path.
	deps, _ := newEnvTestDeps(t)
	srv := envServer(t, deps)

	status, out := envPost(t, srv.URL, "/admin/environment/worker/connect",
		map[string]any{"worker_id": "w-1"})
	if status != http.StatusOK {
		t.Fatalf("org-enrolled connect: status=%d body=%v, want 200", status, out)
	}
}
