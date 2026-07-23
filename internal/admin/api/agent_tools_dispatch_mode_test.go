package api

import (
	"context"
	"net/http"
	"testing"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// I105 Phase 1, layer 1 (pm/center): create_task accepts an optional dispatch_mode
// override and get_task emits it ONLY for supervisor_inline nodes.
//
// The emit asymmetry is deliberate and load-bearing: the worker's fork gate reads this
// key, so an ordinary fork node's projection must stay exactly as it is today — no key
// at all. That way the default path cannot be perturbed by the new field, and an older
// worker (which ignores unknown keys) keeps forking either way.

// TestCreateTask_DispatchModeSupervisorInline_PersistsAndEmits is the round-trip: the
// mark survives create_task → domain → get_task.
func TestCreateTask_DispatchModeSupervisorInline_PersistsAndEmits(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{
			"agent_id": atAgent1, "project_id": string(pid),
			"title": "Deploy v2.31.0", "description": "cut the release",
			"dispatch_mode": "supervisor_inline",
		})
	if status != http.StatusOK {
		t.Fatalf("create_task status = %d, want 200; body = %v", status, body)
	}
	tid, _ := body["task_id"].(string)
	if tid == "" {
		t.Fatalf("no task_id in body: %v", body)
	}

	// Persisted on the aggregate.
	tk, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	if tk.DispatchMode() != pm.DispatchSupervisorInline {
		t.Fatalf("persisted dispatch_mode = %q, want supervisor_inline", tk.DispatchMode())
	}

	// Emitted on the get_task projection the worker's fork gate reads.
	status, got := postBearer(t, srv.URL, "/admin/agent-tools/get_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK {
		t.Fatalf("get_task status = %d, want 200; body = %v", status, got)
	}
	if got["dispatch_mode"] != "supervisor_inline" {
		t.Fatalf("get_task dispatch_mode = %v, want supervisor_inline (the worker gate reads this key)", got["dispatch_mode"])
	}
}

// TestGetTask_OrdinaryTask_OmitsDispatchMode is the I105 red line #1 lock at the wire
// boundary: an ordinary task's get_task projection must carry NO dispatch_mode key at
// all. If this goes red, every ordinary Dev node started emitting a routing signal.
func TestGetTask_OrdinaryTask_EmitsResolvedDispatchMode(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	_, tid := f.seedMemberProject(t) // a plain seeded task — never marked
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/get_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %v", status, body)
	}
	if v := body["dispatch_mode"]; v != "executor_fork" {
		t.Fatalf("ordinary task dispatch_mode = %v, want executor_fork", v)
	}
}

// TestCreateTask_DispatchModeExecutorFork_OmittedFromGetTask locks that even an
// EXPLICIT executor_fork stays off the wire: it is the default, so emitting it would
// be noise on every fork node's projection. Only a value that actually overrides the
// default is emitted.
func TestCreateTask_DispatchModeExecutorFork_EmittedFromGetTask(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{
			"agent_id": atAgent1, "project_id": string(pid), "title": "ordinary dev task",
			"dispatch_mode": "executor_fork",
		})
	if status != http.StatusOK {
		t.Fatalf("create_task status = %d, want 200; body = %v", status, body)
	}
	tid, _ := body["task_id"].(string)

	tk, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	if tk.DispatchMode() != pm.DispatchExecutorFork {
		t.Fatalf("persisted dispatch_mode = %q, want executor_fork", tk.DispatchMode())
	}

	status, got := postBearer(t, srv.URL, "/admin/agent-tools/get_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "task_id": tid})
	if status != http.StatusOK {
		t.Fatalf("get_task status = %d; body = %v", status, got)
	}
	if v := got["dispatch_mode"]; v != "executor_fork" {
		t.Fatalf("executor_fork dispatch_mode = %v, want executor_fork", v)
	}
}

// TestCreateTask_InvalidDispatchMode_400 locks that a typo'd mode is rejected LOUDLY
// at the write boundary rather than silently persisted. A silently-accepted typo would
// read back as "not supervisor_inline" and quietly fork a center-action node into an
// empty workspace — the exact failure I105 removes — with nothing in the logs.
func TestCreateTask_InvalidDispatchMode_400(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	for _, bad := range []string{"supervisor-inline", "inline", "SUPERVISOR_INLINE", "fork", "true"} {
		t.Run(bad, func(t *testing.T) {
			status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1",
				map[string]any{
					"agent_id": atAgent1, "project_id": string(pid), "title": "t",
					"dispatch_mode": bad,
				})
			if status != http.StatusBadRequest {
				t.Fatalf("dispatch_mode=%q status = %d, want 400; body = %v", bad, status, body)
			}
			if body["error"] != "invalid_input" {
				t.Errorf("error code = %v, want invalid_input; body = %v", body["error"], body)
			}
		})
	}
}

// TestCreateTask_DispatchModeOmitted_DefaultsToFork is the create-side half of red
// line #1: omitting the field entirely (every existing caller) must leave the task on
// the default fork route.
func TestCreateTask_DispatchModeOmitted_DefaultsToFork(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	pid, _ := f.seedMemberProject(t)
	srv := f.server(t)

	status, body := postBearer(t, srv.URL, "/admin/agent-tools/create_task", "acat_w1",
		map[string]any{"agent_id": atAgent1, "project_id": string(pid), "title": "plain task"})
	if status != http.StatusOK {
		t.Fatalf("create_task status = %d, want 200 (an omitted dispatch_mode must stay legal); body = %v", status, body)
	}
	tid, _ := body["task_id"].(string)
	tk, err := f.pmSvc.GetTask(context.Background(), pm.TaskID(tid))
	if err != nil {
		t.Fatal(err)
	}
	if tk.DispatchMode() != "" {
		t.Fatalf("omitted dispatch_mode = %q, want \"\" (= executor_fork)", tk.DispatchMode())
	}
	if tk.DispatchMode().RoutesInline() {
		t.Fatal("an unmarked task must never route inline")
	}
}
