package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/concurrency"
	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsqlite "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/workforce"
)

func markForkRuntimeReady(t *testing.T, fx *writeToolsFixture, workerID, agentID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UTC()
	fx.clk.Set(now)
	wk, err := fx.deps.WorkerRepo.FindByID(ctx, workforce.WorkerID(workerID))
	if err != nil {
		t.Fatalf("find worker: %v", err)
	}
	if wk.Status() != workforce.WorkerOnline {
		if err := fx.deps.WorkerRepo.UpdateStatus(ctx, wk.ID(), wk.Status(), workforce.WorkerOnline, wk.Version()); err != nil {
			t.Fatalf("mark worker online: %v", err)
		}
	}
	store := concurrency.NewInMemoryStore()
	store.Put(agentID, concurrency.AgentSnapshot{AdmissionCap: 1, SlotCount: 1, ConfigVersion: 1, Active: 0, Executors: []concurrency.ExecutorSnapshot{}}, now)
	fx.deps.LiveState = store
}

func TestForkExecutorHandler_EnqueuesRuntimeCommand(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	_, taskID := fx.seedMemberProject(t)
	envWorkers := envsqlite.NewWorkerRepo(fx.db)
	envEvents := envsqlite.NewControlEventRepo(fx.db)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB:      fx.db,
		Workers: envWorkers,
		Events:  envEvents,
		IDGen:   idgen.NewGenerator(fx.clk),
		Clock:   fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	markForkRuntimeReady(t, fx, atWorker1, atAgent1)
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/fork_executor", "acat_w1", map[string]any{
		"agent_id": atAgent1,
		"task_id":  taskID,
		"model":    "claude-sonnet",
		"context":  "prefer tests first",
	})
	if st != http.StatusAccepted {
		t.Fatalf("status=%d body=%v, want 202", st, body)
	}
	if body["status"] != "accepted" || body["command_type"] != cmdTypeAgentForkExecutor {
		t.Fatalf("response body=%v", body)
	}

	cmds, err := fx.deps.EnvControlSvc.CommandsAfter(context.Background(), environment.WorkerID(atWorker1), 0)
	if err != nil {
		t.Fatalf("commands after: %v", err)
	}
	if len(cmds) != 1 {
		t.Fatalf("commands=%d, want 1", len(cmds))
	}
	if cmds[0].CommandType() != cmdTypeAgentForkExecutor {
		t.Fatalf("command type=%q, want %q", cmds[0].CommandType(), cmdTypeAgentForkExecutor)
	}
	var pl map[string]string
	if err := json.Unmarshal([]byte(cmds[0].Payload()), &pl); err != nil {
		t.Fatalf("payload json: %v", err)
	}
	if pl["agent_id"] != atAgent1 || pl["task_id"] != taskID || pl["model"] != "claude-sonnet" || pl["context"] != "prefer tests first" {
		t.Fatalf("payload=%v", pl)
	}
	if !strings.Contains(cmds[0].IdempotencyKey(), "fork_executor:"+atAgent1+":"+taskID+":") {
		t.Fatalf("idempotency key=%q", cmds[0].IdempotencyKey())
	}
	if cmds[0].Status() != environment.CommandStatusPending || cmds[0].AgentID() != atAgent1 || cmds[0].TaskID() != taskID {
		t.Fatalf("command metadata status/agent/task = %q/%q/%q", cmds[0].Status(), cmds[0].AgentID(), cmds[0].TaskID())
	}
}

func TestForkExecutorHandler_RejectsWithoutRuntimeSnapshot(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	_, taskID := fx.seedMemberProject(t)
	envWorkers := envsqlite.NewWorkerRepo(fx.db)
	envEvents := envsqlite.NewControlEventRepo(fx.db)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envWorkers, Events: envEvents,
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	wk, err := fx.deps.WorkerRepo.FindByID(context.Background(), workforce.WorkerID(atWorker1))
	if err != nil {
		t.Fatal(err)
	}
	if err := fx.deps.WorkerRepo.UpdateStatus(context.Background(), wk.ID(), wk.Status(), workforce.WorkerOnline, wk.Version()); err != nil {
		t.Fatal(err)
	}
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/fork_executor", "acat_w1", map[string]any{
		"agent_id": atAgent1,
		"task_id":  taskID,
	})
	if st != http.StatusServiceUnavailable || body["error"] != "runtime_not_ready" {
		t.Fatalf("status=%d body=%v, want runtime_not_ready 503", st, body)
	}
	cmds, err := fx.deps.EnvControlSvc.CommandsAfter(context.Background(), environment.WorkerID(atWorker1), 0)
	if err != nil {
		t.Fatalf("commands after: %v", err)
	}
	if len(cmds) != 0 {
		t.Fatalf("runtime-not-ready must not enqueue, got %d command(s)", len(cmds))
	}
}

func TestForkExecutorHandler_DuplicateForkReturnsSamePendingCommand(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	_, taskID := fx.seedMemberProject(t)
	envWorkers := envsqlite.NewWorkerRepo(fx.db)
	envEvents := envsqlite.NewControlEventRepo(fx.db)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envWorkers, Events: envEvents,
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	markForkRuntimeReady(t, fx, atWorker1, atAgent1)
	srv := fx.server(t)

	firstStatus, first := postBearer(t, srv.URL, "/admin/agent-tools/fork_executor", "acat_w1", map[string]any{
		"agent_id": atAgent1,
		"task_id":  taskID,
	})
	secondStatus, second := postBearer(t, srv.URL, "/admin/agent-tools/fork_executor", "acat_w1", map[string]any{
		"agent_id": atAgent1,
		"task_id":  taskID,
	})
	if firstStatus != http.StatusAccepted || secondStatus != http.StatusAccepted {
		t.Fatalf("statuses=%d/%d bodies=%v/%v", firstStatus, secondStatus, first, second)
	}
	cmds, err := fx.deps.EnvControlSvc.CommandsAfter(context.Background(), environment.WorkerID(atWorker1), 0)
	if err != nil {
		t.Fatalf("commands after: %v", err)
	}
	if first["command_id"] != second["command_id"] || first["offset"] != second["offset"] {
		keys := make([]string, 0, len(cmds))
		for _, cmd := range cmds {
			keys = append(keys, cmd.IdempotencyKey()+"/"+cmd.Status()+"/"+cmd.AgentID()+"/"+cmd.TaskID())
		}
		t.Fatalf("duplicate fork should return same command, first=%v second=%v rows=%v", first, second, keys)
	}
	if len(cmds) != 1 {
		keys := make([]string, 0, len(cmds))
		for _, cmd := range cmds {
			keys = append(keys, cmd.IdempotencyKey()+"/"+cmd.Status()+"/"+cmd.AgentID()+"/"+cmd.TaskID())
		}
		t.Fatalf("duplicate fork enqueued %d command(s), want 1; keys=%v", len(cmds), keys)
	}
}

func TestForkExecutorHandler_ExpiresLegacyPendingCommandBeforeNewAccepted(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	_, taskID := fx.seedMemberProject(t)
	envWorkers := envsqlite.NewWorkerRepo(fx.db)
	envEvents := envsqlite.NewControlEventRepo(fx.db)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envWorkers, Events: envEvents,
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	fx.clk.Set(time.Now().Add(-forkCommandExpireAfter - time.Minute).UTC())
	if _, err := fx.deps.EnvControlSvc.EnqueueCommand(context.Background(), environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(atWorker1),
		CommandType:    cmdTypeAgentForkExecutor,
		IdempotencyKey: "legacy-accepted",
		Payload:        `{"agent_id":"` + atAgent1 + `","task_id":"` + taskID + `"}`,
	}); err != nil {
		t.Fatalf("enqueue legacy fork: %v", err)
	}
	markForkRuntimeReady(t, fx, atWorker1, atAgent1)
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/fork_executor", "acat_w1", map[string]any{
		"agent_id": atAgent1,
		"task_id":  taskID,
	})
	if st != http.StatusAccepted {
		t.Fatalf("status=%d body=%v, want 202", st, body)
	}
	if body["offset"] != float64(2) || body["command_status"] != environment.CommandStatusPending {
		t.Fatalf("new accepted command body=%v, want offset=2 pending", body)
	}
	cmds, err := fx.deps.EnvControlSvc.CommandsAfter(context.Background(), environment.WorkerID(atWorker1), 0)
	if err != nil {
		t.Fatalf("commands after: %v", err)
	}
	if len(cmds) != 2 {
		t.Fatalf("commands=%d, want expired legacy + new pending", len(cmds))
	}
	if cmds[0].Status() != environment.CommandStatusExpired || cmds[0].AgentID() != atAgent1 || cmds[0].TaskID() != taskID {
		t.Fatalf("legacy command not terminal/backfilled: %+v", cmds[0])
	}
	if cmds[1].Status() != environment.CommandStatusPending {
		t.Fatalf("new command status=%q, want pending", cmds[1].Status())
	}
}

func TestForkExecutorHandler_CommandFailureIsQueryableWithoutExecutionStart(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	_, taskID := fx.seedMemberProject(t)
	envWorkers := envsqlite.NewWorkerRepo(fx.db)
	envEvents := envsqlite.NewControlEventRepo(fx.db)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envWorkers, Events: envEvents,
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	markForkRuntimeReady(t, fx, atWorker1, atAgent1)
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/fork_executor", "acat_w1", map[string]any{
		"agent_id": atAgent1,
		"task_id":  taskID,
	})
	if st != http.StatusAccepted {
		t.Fatalf("fork status=%d body=%v, want 202", st, body)
	}
	commandID, _ := body["command_id"].(string)
	if commandID == "" {
		t.Fatalf("fork response missing command_id: %v", body)
	}
	st, statusBody := postBearer(t, srv.URL, "/admin/environment/agent/control-command-status", "acat_w1", map[string]any{
		"agent_id":   atAgent1,
		"command_id": commandID,
		"task_id":    taskID,
		"status":     environment.CommandStatusFailed,
		"reason":     "runtime_executor_unavailable",
		"detail":     "worker has not attached an executor engine",
	})
	if st != http.StatusOK {
		t.Fatalf("status report=%d body=%v, want 200", st, statusBody)
	}

	st, execBody := postBearer(t, srv.URL, "/admin/agent-tools/list_task_executions", "acat_w1", map[string]any{
		"agent_id": atAgent1,
		"task_id":  taskID,
	})
	if st != http.StatusOK {
		t.Fatalf("list_task_executions=%d body=%v, want 200", st, execBody)
	}
	items, ok := execBody["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("executions items=%#v, want one command row", execBody["items"])
	}
	run, _ := items[0].(map[string]any)
	if run["command_id"] != commandID || run["state"] != "terminal" ||
		run["command_status"] != environment.CommandStatusFailed ||
		run["health_status"] != environment.CommandStatusFailed ||
		run["error_kind"] != "runtime_executor_unavailable" ||
		run["recovery_required"] != true {
		t.Fatalf("failed command execution row = %+v", run)
	}

	st, auditBody := postBearer(t, srv.URL, "/admin/agent-tools/get_task_audit", "acat_w1", map[string]any{
		"agent_id":  atAgent1,
		"task_id":   taskID,
		"page_size": 100,
	})
	if st != http.StatusOK {
		t.Fatalf("get_task_audit=%d body=%v, want 200", st, auditBody)
	}
	auditItems, ok := auditBody["items"].([]any)
	if !ok {
		t.Fatalf("audit items=%#v", auditBody["items"])
	}
	found := false
	for _, raw := range auditItems {
		item, _ := raw.(map[string]any)
		if item["id"] == commandID && item["action"] == "fork_executor."+environment.CommandStatusFailed {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("audit did not include failed fork command %s: %v", commandID, auditItems)
	}
}

func TestForkExecutorHandler_RejectsTaskAssignedToAnotherAgent(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w2", atWorker2)
	_, taskID := fx.seedMemberProject(t) // assigned to AG1
	envWorkers := envsqlite.NewWorkerRepo(fx.db)
	envEvents := envsqlite.NewControlEventRepo(fx.db)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envWorkers, Events: envEvents,
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker2)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/fork_executor", "acat_w2", map[string]any{
		"agent_id": atAgent2,
		"task_id":  taskID,
	})
	if st != http.StatusForbidden || body["error"] != "not_agents_task" {
		t.Fatalf("status=%d body=%v, want not_agents_task", st, body)
	}
	cmds, err := fx.deps.EnvControlSvc.CommandsAfter(context.Background(), environment.WorkerID(atWorker2), 0)
	if err != nil {
		t.Fatalf("commands after: %v", err)
	}
	if len(cmds) != 0 {
		t.Fatalf("foreign task enqueued %d command(s), want 0", len(cmds))
	}
}

func TestForkExecutorHandler_RejectsMissingTaskID(t *testing.T) {
	fx := newAgentToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/fork_executor", "acat_w1", map[string]any{
		"agent_id": atAgent1,
	})
	if st != http.StatusBadRequest || body["error"] != "missing_task_id" {
		t.Fatalf("status=%d body=%v, want missing_task_id", st, body)
	}
}

func TestForkExecutorHandler_RejectsCrossWorkerAgent(t *testing.T) {
	fx := newAgentToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/fork_executor", "acat_w1", map[string]any{
		"agent_id": atAgent2,
		"task_id":  "task-x",
	})
	if st != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status=%d body=%v, want cross-worker rejection", st, body)
	}
}
