package workerdaemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/runtimedeploy"
	"github.com/oopslink/agent-center/internal/workforce"
)

type fakeWorkerIdentityReadback struct {
	resp WorkerReadback
	err  error
}

func (f fakeWorkerIdentityReadback) FindWorkerByID(context.Context, string) (WorkerReadback, error) {
	return f.resp, f.err
}

func TestSourceRuntimeDeployerReportsAuthoritativeReadback(t *testing.T) {
	root := t.TempDir()
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	var commands []string
	d := newSourceRuntimeDeployer(root, "worker-1", fakeWorkerIdentityReadback{resp: WorkerReadback{
		WorkerID: "worker-1",
		Status:   workforce.WorkerOnline.String(),
		SystemInfo: workforce.SystemInfo{
			WorkerVersion: "runtime-deploy-" + sha[:12],
			BuildCommit:   sha,
		},
	}}, func(string) {})
	d.run = func(_ context.Context, dir, name string, args []string, _ []string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		switch {
		case name == "git" && len(args) > 0 && args[0] == "clone":
			if err := os.MkdirAll(runtimedeploy.ManagedSourceDir(root, sha), 0o700); err != nil {
				return nil, err
			}
		case name == "make" && slices.Contains(args, "OUT="+filepath.Join(root, "stage-"+sha[:12])):
			if err := os.MkdirAll(filepath.Join(root, "stage-"+sha[:12]), 0o700); err != nil {
				return nil, err
			}
		}
		return []byte("ok\n"), nil
	}

	got, err := d.DeployRestart(context.Background(), runtimedeploy.Request{
		RepoURL:           "https://example.invalid/repo.git",
		TargetRef:         "refs/heads/main",
		TargetSHA:         sha,
		VerifiedTargetSHA: sha,
		VerifiedBaseSHA:   "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		VerifiedAt:        "2026-08-31T00:00:00Z",
		Mode:              "center",
	})
	if err != nil {
		t.Fatalf("DeployRestart: %v", err)
	}
	if got.TargetSHA != sha || got.RunningSHA != sha || got.RunningVersion != "runtime-deploy-"+sha[:12] ||
		got.RunningCommit != sha || got.PostRestartHealthStatus != "worker_identity_readback_ok" {
		t.Fatalf("deploy result missing authoritative readback: %+v", got)
	}
	for _, cmd := range commands {
		if strings.Contains(cmd, filepath.Join("bin", "agent-center")+" version") {
			t.Fatalf("staged artifact version readback must not run; commands=%v", commands)
		}
	}
	if !slices.Contains(commands, "make release-dir VERSION=runtime-deploy-"+sha[:12]+" COMMIT="+sha+" OUT="+filepath.Join(root, "stage-"+sha[:12])) {
		t.Fatalf("release build must inject full verified commit; commands=%v", commands)
	}
}

func TestValidatePostRestartWorkerIdentityRejectsOldSHA(t *testing.T) {
	target := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	old := "eeeeeeeeeeeebbbbbbbbbbbbccccccccccccdddd"
	err := validatePostRestartWorkerIdentity(WorkerReadback{
		WorkerID: "worker-1",
		Status:   workforce.WorkerOnline.String(),
		SystemInfo: workforce.SystemInfo{
			WorkerVersion: "runtime-deploy-" + old[:12],
			BuildCommit:   old,
		},
	}, "worker-1", target)
	if err == nil || !strings.Contains(err.Error(), "does not match target sha") {
		t.Fatalf("old sha should fail closed, got %v", err)
	}
}

func TestValidatePostRestartWorkerIdentityRejectsUnhealthy(t *testing.T) {
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	err := validatePostRestartWorkerIdentity(WorkerReadback{
		WorkerID: "worker-1",
		Status:   workforce.WorkerOffline.String(),
		SystemInfo: workforce.SystemInfo{
			WorkerVersion: "runtime-deploy-" + sha[:12],
			BuildCommit:   sha,
		},
	}, "worker-1", sha)
	if err == nil || !strings.Contains(err.Error(), "unhealthy") {
		t.Fatalf("unhealthy worker should fail closed, got %v", err)
	}
}

func TestReadPostRestartWorkerIdentityFailsClosedWhenEndpointUnavailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := newSourceRuntimeDeployer(t.TempDir(), "worker-1", fakeWorkerIdentityReadback{err: errors.New("dial failed")}, func(string) {})
	_, _, _, err := d.readPostRestartWorkerIdentity(ctx, "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("endpoint unavailable should fail closed, got %v", err)
	}
}
