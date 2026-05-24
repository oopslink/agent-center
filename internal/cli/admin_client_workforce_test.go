// Package cli — admin_client_workforce_test.go: worked-example test
// that proves the v2.2 Phase B Client + admin transport works
// end-to-end for the workforce BC handlers.
//
// Pattern (per docs/plans/v2.2-audits/v22-B-cli-refactor-audit.md):
//
//   1. setupAdminServerForTests spins up an in-process admin endpoint
//      on a unix socket + returns an App whose Client points at it.
//   2. The test runs the worker enroll / list / status handlers through
//      the router exactly as a real CLI invocation would — `a.Client`
//      is non-nil so the handler routes through the admin endpoint,
//      not the direct Service field.
//   3. Assertions cover the exit code and the JSON-output projection
//      so we know the DTO ↔ map projection helpers are correct.
//
// The other 17 handler files are migrated in follow-up commits using
// this same shape (one test per BC at minimum).
package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestClient_WorkerEnrollAndList_OverAdminEndpoint(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()

	// --- enroll ----------------------------------------------------------
	enroll := findCmd(app.WorkerCommands(), "enroll")
	if enroll == nil {
		t.Fatal("enroll command missing")
	}
	out, _, code := runHandler(t, enroll, []string{
		"--worker-id=W-CLIENT-1",
		"--capabilities=claude-code,codex",
		"--format=json",
	})
	if code != ExitOK {
		t.Fatalf("enroll exit=%d out=%s", code, out)
	}
	var enrolled map[string]any
	if err := json.Unmarshal([]byte(out), &enrolled); err != nil {
		t.Fatalf("decode enroll out: %v body=%s", err, out)
	}
	if enrolled["worker_id"] != "W-CLIENT-1" {
		t.Fatalf("enroll worker_id = %v", enrolled["worker_id"])
	}
	if enrolled["event_id"] == "" {
		t.Fatal("enroll event_id empty")
	}

	// --- list (table) ----------------------------------------------------
	list := findCmd(app.WorkerCommands(), "list")
	out2, _, code := runHandler(t, list, []string{"--format=table"})
	if code != ExitOK {
		t.Fatalf("list exit=%d out=%s", code, out2)
	}
	if !strings.Contains(out2, "W-CLIENT-1") {
		t.Fatalf("list missing enrolled worker: %s", out2)
	}
	if !strings.Contains(out2, "claude-code,codex") {
		t.Fatalf("list missing capabilities: %s", out2)
	}

	// --- status ----------------------------------------------------------
	status := findCmd(app.WorkerCommands(), "status")
	out3, _, code := runHandler(t, status, []string{"W-CLIENT-1", "--format=json"})
	if code != ExitOK {
		t.Fatalf("status exit=%d out=%s", code, out3)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out3), &got); err != nil {
		t.Fatalf("decode status: %v body=%s", err, out3)
	}
	if got["worker_id"] != "W-CLIENT-1" {
		t.Fatalf("status worker_id = %v", got["worker_id"])
	}
}

func TestClient_WorkerStatus_NotFoundMapsToExitNotFound(t *testing.T) {
	app, cleanup := setupAdminServerForTests(t)
	defer cleanup()

	status := findCmd(app.WorkerCommands(), "status")
	_, errOut, code := runHandler(t, status, []string{"W-DOES-NOT-EXIST", "--format=json"})
	if code != ExitNotFound {
		t.Fatalf("expected ExitNotFound (%d), got %d. stderr=%s", ExitNotFound, code, errOut)
	}
}

func TestClient_ServerUnreachable_FriendlyError(t *testing.T) {
	// Construct an App with a Client pointing at a non-existent socket.
	cfg, err := loadConfigForCLI("", nil)
	if err != nil {
		// DefaultConfig should be fine without a path; if not, just skip
		// — this assertion is about Client behaviour, not config loading.
		t.Skipf("loadConfigForCLI: %v", err)
	}
	bogus := testShortSocketPath(t, "missing.sock")
	app := NewClientApp(cfg, NewClient(bogus, 0))

	enroll := findCmd(app.WorkerCommands(), "enroll")
	_, errOut, code := runHandler(t, enroll, []string{
		"--worker-id=W-1", "--format=json",
	})
	if code != ExitBusinessError {
		t.Fatalf("expected ExitBusinessError, got %d. stderr=%s", code, errOut)
	}
	if !strings.Contains(errOut, "server_unreachable") {
		t.Fatalf("expected friendly server_unreachable in stderr, got %s", errOut)
	}

	// Sanity: a context cancel doesn't reach the unreachable check
	// (just ensure ctx import is referenced; keeps lint happy).
	_ = context.Background
}
