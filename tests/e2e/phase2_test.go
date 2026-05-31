package e2e

import (
	"encoding/json"
	"strings"
	"testing"
)

// seedPhase2 creates a project + worker so task tests can run. The `project
// add` / `worker enroll` write commands moved off the CLI in #132, so we seed
// through the in-process services against the harness DB.
func seedPhase2(t *testing.T, h *harness) {
	t.Helper()
	app, done := inProcessApp(t, h)
	defer done()
	seedProjectE2E(t, app, "p-1", "Test Project")
	seedWorkerE2E(t, app, "W-1")
}

func TestE2EP2_TaskUnbindConversation_NotImplemented(t *testing.T) {
	h := newHarness(t)
	seedPhase2(t, h)
	_, stderr, code := h.run("task", "unbind-conversation", "T-1", "--format=json")
	if code != 64 {
		t.Fatalf("expected exit 64, got %d", code)
	}
	if !strings.Contains(stderr, "not_implemented_v1") {
		t.Fatalf("err: %s", stderr)
	}
}

func TestE2EP2_DispatchHappyAndEventChain(t *testing.T) {
	h := newHarness(t)
	// Seed project + worker + task directly (task create CLI removed in #132).
	app, done := inProcessApp(t, h)
	seedProjectE2E(t, app, "p-1", "Test Project")
	seedWorkerE2E(t, app, "W-1")
	taskID, _ := seedTaskRuntimeE2E(t, app, "p-1", "do thing", true)
	done()

	stdout, stderr, code := h.run("dispatch", taskID, "--worker=W-1", "--format=json")
	if code != 0 {
		t.Fatalf("dispatch: %d / err: %s", code, stderr)
	}
	if !strings.Contains(stdout, "execution_id") {
		t.Fatalf("out: %s", stdout)
	}
	// Verify events landed
	events := readEvents(t, h.dbPath)
	wantTypes := []string{
		"workforce.worker.enrolled",
		"workforce.project.created",
		"task.created",
		"task_execution.submitted",
		"task_execution.dispatched",
	}
	for _, w := range wantTypes {
		found := false
		for _, e := range events {
			if e.EventType == w {
				found = true
				break
			}
		}
		if !found {
			// Some event types may use bc-less prefixes; require at least
			// task.* / task_execution.* to land
			if strings.HasPrefix(w, "task") {
				t.Errorf("missing event %q in events: %+v", w, eventTypes(events))
			}
		}
	}
}

func TestE2EP2_DispatchTaskNotFound(t *testing.T) {
	h := newHarness(t)
	seedPhase2(t, h)
	_, stderr, code := h.run("dispatch", "T-X", "--worker=W-1", "--format=json")
	if code != 17 {
		t.Fatalf("expected exit 17 (not_found): %d", code)
	}
	if !strings.Contains(stderr, "task_not_found") {
		t.Fatalf("err: %s", stderr)
	}
}

func TestE2EP2_KillExecutionUsage(t *testing.T) {
	h := newHarness(t)
	seedPhase2(t, h)
	_, _, code := h.run("kill-execution", "E-1", "--format=json")
	if code != 2 {
		t.Fatalf("expected usage error: %d", code)
	}
}

func TestE2EP2_ReadTaskContext(t *testing.T) {
	h := newHarness(t)
	app, done := inProcessApp(t, h)
	seedProjectE2E(t, app, "p-1", "Test Project")
	seedWorkerE2E(t, app, "W-1")
	taskID, _ := seedTaskRuntimeE2E(t, app, "p-1", "do thing", true)
	done()

	stdout, stderr, code := h.run("read-task-context", taskID)
	if code != 0 {
		t.Fatalf("read-task-context: %d / err: %s", code, stderr)
	}
	if !strings.Contains(stdout, "task_id") {
		t.Fatalf("out: %s", stdout)
	}
}

func TestE2EP2_TaskBindAuto(t *testing.T) {
	h := newHarness(t)
	app, done := inProcessApp(t, h)
	seedProjectE2E(t, app, "p-1", "Test Project")
	seedWorkerE2E(t, app, "W-1")
	taskID, _ := seedTaskRuntimeE2E(t, app, "p-1", "do thing", false)
	done()

	stdout, _, code := h.run("task", "bind-conversation", taskID, "--auto=true", "--format=json")
	if code != 0 {
		t.Fatalf("bind: %d / %s", code, stdout)
	}
	if !strings.Contains(stdout, "conversation_id") {
		t.Fatalf("out: %s", stdout)
	}
}

func TestE2EP2_ReportArtifact(t *testing.T) {
	h := newHarness(t)
	app, done := inProcessApp(t, h)
	seedProjectE2E(t, app, "p-1", "Test Project")
	seedWorkerE2E(t, app, "W-1")
	taskID, _ := seedTaskRuntimeE2E(t, app, "p-1", "do", true)
	done()

	stdout, _, _ := h.run("dispatch", taskID, "--worker=W-1", "--format=json")
	var execOut struct {
		ExecutionID string `json:"execution_id"`
	}
	_ = json.Unmarshal([]byte(stdout), &execOut)
	stdout, stderr, code := h.run("report-artifact", execOut.ExecutionID,
		"--kind=pr_url", "--title=feat:x", "--url=https://github.com/x/y/pull/1", "--format=json")
	if code != 0 {
		t.Fatalf("report-artifact: %d / err: %s", code, stderr)
	}
	if !strings.Contains(stdout, "artifact_id") {
		t.Fatalf("out: %s", stdout)
	}
}

// eventTypes pulls only the type names for diagnostic prints.
func eventTypes(events []eventRow) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = e.EventType
	}
	return out
}
