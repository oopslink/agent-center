package api

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/environment"
	envservice "github.com/oopslink/agent-center/internal/environment/service"
	envsqlite "github.com/oopslink/agent-center/internal/environment/sqlite"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/runtimedeploy"
)

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
	remote, baseSHA, targetSHA := seedRuntimeDeployRemoteForAPI(t)
	srv := fx.server(t)

	st, body := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", map[string]any{
		"agent_id":           atAgent1,
		"repo_url":           remote,
		"target_ref":         "refs/heads/feature",
		"target_sha":         targetSHA,
		"base_ref":           "refs/heads/main",
		"timeout_ms":         1,
		"pushed_ref":         "refs/heads/feature",
		"exact_sha_verified": true,
		"ancestor_verified":  true,
	})
	if st != http.StatusOK || body["accepted"] != true || body["verified_target_sha"] != targetSHA || body["verified_base_sha"] != baseSHA {
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
	remote, baseSHA, _ := seedRuntimeDeployRemoteForAPI(t)
	srv := fx.server(t)
	st, body := postBearer(t, srv.URL, "/admin/agent-tools/runtime_deploy_restart", "acat_w1", map[string]any{
		"agent_id":   atAgent1,
		"repo_url":   remote,
		"target_ref": "refs/heads/feature",
		"target_sha": baseSHA,
		"base_ref":   "refs/heads/main",
	})
	if st != http.StatusUnprocessableEntity || body["error"] != "remote_ref_verification_failed" {
		t.Fatalf("status=%d body=%v", st, body)
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
