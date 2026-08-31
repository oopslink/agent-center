package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/admintoken"
	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsqlite "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/runtimedeploy"
)

type fakeRuntimeDeployVerifier struct {
	got runtimedeploy.Request
	out runtimedeploy.VerifiedRef
	err error
}

func (v *fakeRuntimeDeployVerifier) VerifyRemote(_ context.Context, req runtimedeploy.Request) (runtimedeploy.VerifiedRef, error) {
	v.got = req
	if v.err != nil {
		return runtimedeploy.VerifiedRef{}, v.err
	}
	return v.out, nil
}

func TestRuntimeDeployRestartHandler_VerifiesRemoteBeforeEnqueue(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	fx.seedMemberProject(t)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envsqlite.NewWorkerRepo(fx.db), Events: envsqlite.NewControlEventRepo(fx.db),
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	baseSHA := strings.Repeat("b", 40)
	targetSHA := strings.Repeat("a", 40)
	fx.deps.RuntimeDeployVerifier = &fakeRuntimeDeployVerifier{out: runtimedeploy.VerifiedRef{TargetSHA: targetSHA, BaseSHA: baseSHA}}
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", map[string]any{
		"agent_id":           atAgent1,
		"repo_url":           "https://example.invalid/repo.git",
		"target_ref":         "refs/heads/feature",
		"target_sha":         targetSHA,
		"base_ref":           "refs/heads/main",
		"timeout_ms":         600000,
		"idempotency_key":    "deploy-once",
		"pushed_ref":         "refs/heads/feature",
		"exact_sha_verified": true,
		"ancestor_verified":  true,
	})
	if st != http.StatusAccepted || body["accepted"] != true || body["verified_target_sha"] != targetSHA || body["verified_base_sha"] != baseSHA ||
		body["idempotency_key"] != "deploy-once" || body["terminal"] != false {
		t.Fatalf("status=%d body=%v", st, body)
	}
	cmds, err := fx.deps.EnvControlSvc.CommandsAfter(context.Background(), environment.WorkerID(atWorker1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 || cmds[0].CommandType() != runtimedeploy.CommandType || cmds[0].Status() != environment.CommandStatusPending {
		t.Fatalf("commands=%+v", cmds)
	}
	var payload runtimedeploy.Request
	if err := json.Unmarshal([]byte(cmds[0].Payload()), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.VerifiedTargetSHA != targetSHA || payload.VerifiedBaseSHA != baseSHA || payload.VerifiedAt == "" {
		t.Fatalf("payload verification fields = %+v", payload)
	}
}

func TestRuntimeDeployRestartHandler_RejectsMismatchedRemoteSHA(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	fx.seedMemberProject(t)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envsqlite.NewWorkerRepo(fx.db), Events: envsqlite.NewControlEventRepo(fx.db),
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	baseSHA := strings.Repeat("b", 40)
	fx.deps.RuntimeDeployVerifier = &fakeRuntimeDeployVerifier{err: errors.New("target_ref resolves to different sha")}
	srv := fx.server(t)
	st, body := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", map[string]any{
		"agent_id":   atAgent1,
		"repo_url":   "https://example.invalid/repo.git",
		"target_ref": "refs/heads/feature",
		"target_sha": baseSHA,
		"base_ref":   "refs/heads/main",
	})
	if st != http.StatusUnprocessableEntity || body["error"] != "remote_ref_verification_failed" {
		t.Fatalf("status=%d body=%v", st, body)
	}
}

func TestRuntimeDeployRestartHandler_IdempotencyKeyDoesNotDoubleMutate(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	fx.seedMemberProject(t)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envsqlite.NewWorkerRepo(fx.db), Events: envsqlite.NewControlEventRepo(fx.db),
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	sha := strings.Repeat("a", 40)
	fx.deps.RuntimeDeployVerifier = &fakeRuntimeDeployVerifier{out: runtimedeploy.VerifiedRef{TargetSHA: sha, BaseSHA: strings.Repeat("b", 40)}}
	srv := fx.server(t)
	body := map[string]any{
		"agent_id": atAgent1, "repo_url": "https://example.invalid/repo.git", "target_ref": "refs/heads/main",
		"target_sha": sha, "base_ref": "refs/heads/main", "idempotency_key": "same-key",
	}
	st1, out1 := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", body)
	st2, out2 := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", body)
	if st1 != http.StatusAccepted || st2 != http.StatusAccepted || out1["command_id"] != out2["command_id"] {
		t.Fatalf("idempotent posts status/body: %d %v / %d %v", st1, out1, st2, out2)
	}
	cmds, err := fx.deps.EnvControlSvc.CommandsAfter(context.Background(), environment.WorkerID(atWorker1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(cmds) != 1 {
		t.Fatalf("commands=%d want 1", len(cmds))
	}
}

func TestRuntimeDeployRestartHandler_LongTimeoutReturnsBeforeTerminal(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	fx.seedMemberProject(t)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envsqlite.NewWorkerRepo(fx.db), Events: envsqlite.NewControlEventRepo(fx.db),
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	sha := strings.Repeat("a", 40)
	fx.deps.RuntimeDeployVerifier = &fakeRuntimeDeployVerifier{out: runtimedeploy.VerifiedRef{TargetSHA: sha, BaseSHA: strings.Repeat("b", 40)}}
	srv := fx.server(t)
	start := time.Now()
	st, body := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", map[string]any{
		"agent_id": atAgent1, "repo_url": "https://example.invalid/repo.git", "target_ref": "refs/heads/main",
		"target_sha": sha, "base_ref": "refs/heads/main", "idempotency_key": "long-timeout",
		"timeout_ms": 600000,
	})
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("runtime_deploy_restart blocked for %s despite async attempt response", elapsed)
	}
	if st != http.StatusAccepted || body["command_status"] != environment.CommandStatusPending {
		t.Fatalf("status=%d body=%v", st, body)
	}
}

func TestRuntimeDeployRestartHandler_IdempotencyConflictDifferentPayload(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	fx.seedMemberProject(t)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envsqlite.NewWorkerRepo(fx.db), Events: envsqlite.NewControlEventRepo(fx.db),
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	sha := strings.Repeat("a", 40)
	fx.deps.RuntimeDeployVerifier = &fakeRuntimeDeployVerifier{out: runtimedeploy.VerifiedRef{TargetSHA: sha, BaseSHA: strings.Repeat("b", 40)}}
	srv := fx.server(t)
	body := map[string]any{
		"agent_id": atAgent1, "repo_url": "https://example.invalid/repo.git", "target_ref": "refs/heads/main",
		"target_sha": sha, "base_ref": "refs/heads/main", "idempotency_key": "same-key",
	}
	if st, _ := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", body); st != http.StatusAccepted {
		t.Fatalf("first status=%d", st)
	}
	body["prefix"] = "/different"
	st, out := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", body)
	if st != http.StatusConflict || out["error"] != "idempotency_conflict" {
		t.Fatalf("status=%d body=%v, want idempotency conflict", st, out)
	}
}

func TestRuntimeDeployStatusHandler_ReadsTerminalAttemptByIdempotencyKey(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	fx.seedMemberProject(t)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envsqlite.NewWorkerRepo(fx.db), Events: envsqlite.NewControlEventRepo(fx.db),
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	if _, err := fx.deps.EnvControlSvc.ConnectWorker(context.Background(), environment.WorkerID(atWorker1)); err != nil {
		t.Fatalf("connect env worker: %v", err)
	}
	sha := strings.Repeat("a", 40)
	fx.deps.RuntimeDeployVerifier = &fakeRuntimeDeployVerifier{out: runtimedeploy.VerifiedRef{TargetSHA: sha, BaseSHA: strings.Repeat("b", 40)}}
	srv := fx.server(t)
	_, out := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", map[string]any{
		"agent_id": atAgent1, "repo_url": "https://example.invalid/repo.git", "target_ref": "refs/heads/main",
		"target_sha": sha, "base_ref": "refs/heads/main", "idempotency_key": "read-key",
	})
	cmdID, _ := out["command_id"].(string)
	if cmdID == "" {
		t.Fatalf("missing command id: %v", out)
	}
	if _, err := fx.deps.EnvControlSvc.UpdateCommandStatus(context.Background(), environment.UpdateCommandStatusInput{
		WorkerID: environment.WorkerID(atWorker1), CommandID: cmdID, AgentID: atAgent1,
		Status: environment.CommandStatusSucceeded, StatusReason: "runtime_deploy_succeeded",
		StatusDetail: `{"running_sha":"` + sha + `","running_version":"runtime-deploy-` + sha[:12] + `","post_restart_health_status":"version_readback_ok"}`,
	}); err != nil {
		t.Fatal(err)
	}
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envsqlite.NewWorkerRepo(fx.db), Events: envsqlite.NewControlEventRepo(fx.db),
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	srv = fx.server(t)
	st, status := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_status", "acat_w1", map[string]any{
		"agent_id": atAgent1, "idempotency_key": "read-key",
	})
	if st != http.StatusOK || status["command_status"] != environment.CommandStatusSucceeded || status["terminal"] != true ||
		!strings.Contains(status["status_detail"].(string), `"running_sha":"`+sha+`"`) {
		t.Fatalf("status=%d body=%v", st, status)
	}
}

func TestRuntimeDeployStatusHandler_UnknownAttempt(t *testing.T) {
	fx := newWriteToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	fx.seedMemberProject(t)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envsqlite.NewWorkerRepo(fx.db), Events: envsqlite.NewControlEventRepo(fx.db),
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	srv := fx.server(t)
	st, body := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_status", "acat_w1", map[string]any{
		"agent_id": atAgent1, "idempotency_key": "missing",
	})
	if st != http.StatusNotFound || body["error"] != "runtime_deploy_attempt_not_found" {
		t.Fatalf("status=%d body=%v", st, body)
	}
}

func TestRuntimeDeployRestartHandler_RejectsNonWorkerToken(t *testing.T) {
	fx := newAgentToolsFixture(t)
	fx.addOwnerToken(t, "acat_user", admintoken.Owner("user:user-1"))
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envsqlite.NewWorkerRepo(fx.db), Events: envsqlite.NewControlEventRepo(fx.db),
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_user", map[string]any{
		"agent_id":   atAgent1,
		"repo_url":   "https://example.invalid/repo.git",
		"target_ref": "refs/heads/main",
		"target_sha": strings.Repeat("a", 40),
	})
	if st != http.StatusForbidden || body["error"] != "not_a_worker_token" {
		t.Fatalf("status=%d body=%v, want 403 not_a_worker_token", st, body)
	}
}

func TestRuntimeDeployRestartHandler_RejectsWrongWorkerAgent(t *testing.T) {
	fx := newAgentToolsFixture(t)
	fx.addWorkerToken(t, "acat_w1", atWorker1)
	fx.deps.EnvControlSvc = envservice.New(envservice.Deps{
		DB: fx.db, Workers: envsqlite.NewWorkerRepo(fx.db), Events: envsqlite.NewControlEventRepo(fx.db),
		IDGen: idgen.NewGenerator(fx.clk), Clock: fx.clk,
	})
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", map[string]any{
		"agent_id":   atAgent2,
		"repo_url":   "https://example.invalid/repo.git",
		"target_ref": "refs/heads/main",
		"target_sha": strings.Repeat("a", 40),
	})
	if st != http.StatusForbidden || body["error"] != "agent_not_bound_to_worker" {
		t.Fatalf("status=%d body=%v, want 403 agent_not_bound_to_worker", st, body)
	}
}

func seedRuntimeDeployRemoteForAPI(t *testing.T) (remoteURL, baseSHA, targetSHA string) {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	runAPIGit(t, dir, "init", "-b", "main", work)
	runAPIGit(t, work, "config", "user.email", "test@example.invalid")
	runAPIGit(t, work, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAPIGit(t, work, "add", "README.md")
	runAPIGit(t, work, "commit", "-m", "base")
	baseSHA = gitAPIOut(t, work, "rev-parse", "HEAD")
	runAPIGit(t, work, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("target\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runAPIGit(t, work, "add", "feature.txt")
	runAPIGit(t, work, "commit", "-m", "target")
	targetSHA = gitAPIOut(t, work, "rev-parse", "HEAD")
	remote := filepath.Join(dir, "remote.git")
	runAPIGit(t, dir, "clone", "--bare", work, remote)
	return remote, baseSHA, targetSHA
}

func runAPIGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
}

func gitAPIOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
