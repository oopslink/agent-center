package api

import (
	"context"
	"net/http"
	"strings"
	"testing"

	authz "github.com/oopslink/agent-center/internal/authorization"
	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsqlite "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/workforce"
)

func wireRuntimeDeployFixture(t *testing.T, f *writeToolsFixture) {
	t.Helper()
	f.deps.Authorizer = authz.New(authz.Deps{DB: f.db, Mode: authz.EnforcementEnforce})
	f.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB:      f.db,
		Workers: envsqlite.NewWorkerRepo(f.db),
		Events:  envsqlite.NewControlEventRepo(f.db),
		IDGen:   idgen.NewGenerator(f.clk),
		Clock:   f.clk,
	})
	if _, err := f.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	wk, err := f.deps.WorkerRepo.FindByID(context.Background(), workforce.WorkerID(atWorker1))
	if err != nil {
		t.Fatal(err)
	}
	info := workforce.SystemInfo{
		WorkerVersion: "v2.20.0+d55279d",
		BuildCommit:   "d55279debfa874ecaeff90eaac020aa62d8a7a2e",
		BuildBranch:   "main",
		BuildBuiltAt:  "2026-08-31T00:00:00Z",
		PID:           1234,
		ParentPID:     1,
		StartedAt:     "2026-08-31T00:01:00Z",
		InstallPath:   "/opt/agent-center/current/bin/agent-center",
	}
	if err := f.deps.WorkerRepo.UpdateSystemInfo(context.Background(), wk.ID(), info, wk.Version()); err != nil {
		t.Fatalf("update system info: %v", err)
	}
}

func TestDeployRuntime_EnqueuesCommandWithEvidenceAndReadback(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	wireRuntimeDeployFixture(t, f)
	srv := f.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/deploy_runtime", "acat_w1", map[string]any{
		"agent_id":           atAgent1,
		"commit_sha":         "0123456789012345678901234567890123456789",
		"ancestor_sha":       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"pushed_ref":         "origin/main",
		"exact_sha_verified": true,
		"ancestor_verified":  true,
	})
	if st != http.StatusAccepted {
		t.Fatalf("status=%d body=%v, want 202", st, body)
	}
	if body["completion_semantics"] == "" {
		t.Fatalf("missing completion semantics: %v", body)
	}
	cmd := body["command"].(map[string]any)
	if cmd["command_type"] != cmdTypeRuntimeDeployRestart || cmd["status"] != environment.CommandStatusPending {
		t.Fatalf("command=%v", cmd)
	}
	rt := body["runtime"].(map[string]any)
	actual := rt["actual"].(map[string]any)
	if rt["health"] != string(workforce.WorkerOffline) || actual["build_commit"] != "d55279debfa874ecaeff90eaac020aa62d8a7a2e" {
		t.Fatalf("runtime readback=%v", rt)
	}
	cmds, err := f.deps.EnvControlSvc.CommandsAfter(context.Background(), environment.WorkerID(atWorker1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].CommandType() != cmdTypeRuntimeDeployRestart {
		t.Fatalf("commands=%v", cmds)
	}
	if got := cmds[0].Payload(); got == "" || !containsAll(got, "commit_sha", "ancestor_sha", "pushed_ref", "restart_semantics") {
		t.Fatalf("payload lacks deploy evidence: %s", got)
	}
}

func TestDeployRuntime_FailsClosedWithoutAuthorizer(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	srv := f.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/get_runtime_status", "acat_w1", map[string]any{
		"agent_id": atAgent1,
	})
	if st != http.StatusForbidden || body["error"] != "authorization_not_wired" {
		t.Fatalf("status=%d body=%v, want fail-closed authorization_not_wired", st, body)
	}
}

func TestDeployRuntime_RejectsMissingExactSHAEvidence(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	wireRuntimeDeployFixture(t, f)
	srv := f.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/deploy_runtime", "acat_w1", map[string]any{
		"agent_id":           atAgent1,
		"commit_sha":         "0123456789012345678901234567890123456789",
		"ancestor_sha":       "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"pushed_ref":         "origin/main",
		"exact_sha_verified": false,
		"ancestor_verified":  true,
	})
	if st != http.StatusBadRequest || body["error"] != "exact_sha_not_verified" {
		t.Fatalf("status=%d body=%v", st, body)
	}
}

func TestGetRuntimeStatus_IncludesCommandStatus(t *testing.T) {
	f := newWriteToolsFixture(t)
	f.addWorkerToken(t, "acat_w1", atWorker1)
	wireRuntimeDeployFixture(t, f)
	evt, err := f.deps.EnvControlSvc.EnqueueCommand(context.Background(), environment.AppendCommandInput{
		WorkerID:       environment.WorkerID(atWorker1),
		CommandType:    cmdTypeRuntimeDeployRestart,
		IdempotencyKey: "deploy-readback",
		Payload:        `{"commit_sha":"0123456789012345678901234567890123456789"}`,
		AgentID:        atAgent1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.deps.EnvControlSvc.UpdateCommandStatus(context.Background(), environment.UpdateCommandStatusInput{
		WorkerID: environment.WorkerID(atWorker1), CommandID: evt.ID(), Status: environment.CommandStatusSucceeded,
		StatusReason: "runtime_restart_complete", StatusDetail: "reported build_commit matched target",
	}); err != nil {
		t.Fatal(err)
	}
	srv := f.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/get_runtime_status", "acat_w1", map[string]any{
		"agent_id":   atAgent1,
		"command_id": evt.ID(),
	})
	if st != http.StatusOK {
		t.Fatalf("status=%d body=%v", st, body)
	}
	cmd := body["command"].(map[string]any)
	if cmd["status"] != environment.CommandStatusSucceeded || cmd["status_reason"] != "runtime_restart_complete" {
		t.Fatalf("command status readback=%v", cmd)
	}
}

func containsAll(s string, wants ...string) bool {
	for _, want := range wants {
		if !strings.Contains(s, want) {
			return false
		}
	}
	return true
}
