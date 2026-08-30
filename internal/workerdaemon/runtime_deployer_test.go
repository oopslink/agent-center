package workerdaemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/runtimedeploy"
)

func TestSourceRuntimeDeployerIgnoresVerifiedFieldsAndRequiresExactSHA(t *testing.T) {
	remote, _, targetSHA, _ := seedDeployRemote(t)
	d := newSourceRuntimeDeployer(t.TempDir(), "worker-1", func(string) {})
	d.run = func(context.Context, string, string, []string, []string) ([]byte, error) {
		t.Fatal("deploy commands must not run when exact_sha is missing")
		return nil, nil
	}

	_, err := d.DeployRestart(context.Background(), runtimedeploy.Request{
		RepoURL: remote, TargetRef: "refs/heads/feature", BaseRef: "refs/heads/main",
		TargetSHA: targetSHA, VerifiedTargetSHA: targetSHA, VerifiedBaseSHA: targetSHA, VerifiedAt: "2026-08-31T00:00:00Z",
	})
	if err == nil || !strings.Contains(err.Error(), "exact_sha required") {
		t.Fatalf("err=%v want exact_sha required despite verified fields", err)
	}
}

func TestSourceRuntimeDeployerRejectsNonAncestorBeforeCheckout(t *testing.T) {
	remote, _, _, orphanSHA := seedDeployRemote(t)
	d := newSourceRuntimeDeployer(t.TempDir(), "worker-1", func(string) {})
	d.run = func(context.Context, string, string, []string, []string) ([]byte, error) {
		t.Fatal("deploy commands must not run until server-side ancestry is proven")
		return nil, nil
	}

	_, err := d.DeployRestart(context.Background(), runtimedeploy.Request{
		RepoURL: remote, TargetRef: "refs/heads/orphan", ExactSHA: orphanSHA, BaseRef: "refs/heads/main",
	})
	if err == nil || !strings.Contains(err.Error(), "is not an ancestor") {
		t.Fatalf("err=%v want non-ancestor rejection", err)
	}
}

func TestSourceRuntimeDeployerVerifiesExactSHAThenBuildsAndUpgrades(t *testing.T) {
	remote, _, targetSHA, _ := seedDeployRemote(t)
	root := t.TempDir()
	d := newSourceRuntimeDeployer(root, "worker-1", func(string) {})
	runner := &sourceDeployRunner{t: t, root: root, targetSHA: targetSHA}
	d.run = runner.run

	res, err := d.DeployRestart(context.Background(), runtimedeploy.Request{
		RepoURL: remote, TargetRef: "refs/heads/feature", ExactSHA: targetSHA, BaseRef: "refs/heads/main", Mode: "worker",
	})
	if err != nil {
		t.Fatalf("DeployRestart: %v", err)
	}
	if res.TargetSHA != targetSHA || res.Mode != "worker" {
		t.Fatalf("result=%+v want target=%s mode=worker", res, targetSHA)
	}
	if !runner.called("git checkout --detach " + targetSHA) {
		t.Fatalf("checkout did not use server-verified exact SHA; calls=%v", runner.calls)
	}
	if !runner.calledPrefix(filepath.Join(root, "stage-"+targetSHA[:12], "upgrade") + " worker --force --worker-id=worker-1") {
		t.Fatalf("worker upgrade was not run; calls=%v", runner.calls)
	}
}

type sourceDeployRunner struct {
	t         *testing.T
	root      string
	targetSHA string
	calls     []string
}

func (r *sourceDeployRunner) run(_ context.Context, dir, name string, args []string, _ []string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	switch {
	case call == "git clone --no-checkout "+args[len(args)-2]+" "+args[len(args)-1]:
		if err := os.MkdirAll(args[len(args)-1], 0o755); err != nil {
			r.t.Fatal(err)
		}
		return []byte{}, nil
	case strings.HasPrefix(call, "git fetch --no-tags origin +refs/heads/feature:refs/ac-runtime-deploy/target"):
		return []byte{}, nil
	case call == "git checkout --detach "+r.targetSHA:
		return []byte{}, nil
	case strings.HasPrefix(call, "make release-dir "):
		out := ""
		for _, arg := range args {
			if strings.HasPrefix(arg, "OUT=") {
				out = strings.TrimPrefix(arg, "OUT=")
				break
			}
		}
		if out == "" {
			return nil, errors.New("missing OUT")
		}
		if err := os.MkdirAll(out, 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(out, "upgrade"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			r.t.Fatal(err)
		}
		return []byte{}, nil
	case strings.HasPrefix(call, filepath.Join(r.root, "stage-"+r.targetSHA[:12], "upgrade")+" worker --force --worker-id=worker-1"):
		return []byte{}, nil
	default:
		r.t.Fatalf("unexpected deploy command dir=%s call=%s", dir, call)
		return nil, nil
	}
}

func (r *sourceDeployRunner) called(want string) bool {
	for _, call := range r.calls {
		if call == want {
			return true
		}
	}
	return false
}

func (r *sourceDeployRunner) calledPrefix(want string) bool {
	for _, call := range r.calls {
		if strings.HasPrefix(call, want) {
			return true
		}
	}
	return false
}

func seedDeployRemote(t *testing.T) (remoteURL, mainSHA, featureSHA, orphanSHA string) {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	runGitForRuntimeDeployTest(t, dir, "init", "-b", "main", work)
	runGitForRuntimeDeployTest(t, work, "config", "user.email", "test@example.invalid")
	runGitForRuntimeDeployTest(t, work, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForRuntimeDeployTest(t, work, "add", "README.md")
	runGitForRuntimeDeployTest(t, work, "commit", "-m", "main")
	mainSHA = gitRuntimeDeployOut(t, work, "rev-parse", "HEAD")
	runGitForRuntimeDeployTest(t, work, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForRuntimeDeployTest(t, work, "add", "feature.txt")
	runGitForRuntimeDeployTest(t, work, "commit", "-m", "feature")
	featureSHA = gitRuntimeDeployOut(t, work, "rev-parse", "HEAD")
	runGitForRuntimeDeployTest(t, work, "checkout", "--orphan", "orphan")
	runGitForRuntimeDeployTest(t, work, "rm", "-rf", ".")
	if err := os.WriteFile(filepath.Join(work, "orphan.txt"), []byte("orphan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForRuntimeDeployTest(t, work, "add", "orphan.txt")
	runGitForRuntimeDeployTest(t, work, "commit", "-m", "orphan")
	orphanSHA = gitRuntimeDeployOut(t, work, "rev-parse", "HEAD")
	remote := filepath.Join(dir, "remote.git")
	runGitForRuntimeDeployTest(t, dir, "clone", "--bare", work, remote)
	return remote, mainSHA, featureSHA, orphanSHA
}

func runGitForRuntimeDeployTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
}

func gitRuntimeDeployOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
