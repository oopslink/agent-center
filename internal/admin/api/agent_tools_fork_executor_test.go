package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsqlite "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
)

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
