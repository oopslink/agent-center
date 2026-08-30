package workerdaemon

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testBaseSHA   = "1111111111111111111111111111111111111111"
	testTargetSHA = "2222222222222222222222222222222222222222"
)

func TestGitBuildDeployRuntime_VerifiesAndSwapsBinary(t *testing.T) {
	source := tempGitDir(t)
	bin := filepath.Join(t.TempDir(), "agent-center")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	runner := newDeployRunner(t, source, nil)
	d := NewGitBuildDeployRuntime(GitBuildDeployRuntimeConfig{SourceDir: source, BinaryPath: bin})
	d.run = runner.run

	res, err := d.DeployRestart(context.Background(), DeployRequest{
		Remote:           "origin",
		Ref:              "refs/heads/release",
		ExactSHA:         testTargetSHA,
		BaseRef:          "refs/heads/main",
		ExactSHAVerified: false,
		AncestorVerified: false,
	})
	if err != nil {
		t.Fatalf("DeployRestart: %v", err)
	}
	if res.ResolvedSHA != testTargetSHA || res.BaseSHA != testBaseSHA {
		t.Fatalf("result SHAs = %+v", res)
	}
	body, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "new-runtime" {
		t.Fatalf("binary body=%q want swapped build", body)
	}
	if !runner.called("git merge-base --is-ancestor " + testBaseSHA + " " + testTargetSHA) {
		t.Fatalf("merge-base ancestry check was not run; calls=%v", runner.calls)
	}
}

func TestGitBuildDeployRuntime_RejectsExactSHAMismatch(t *testing.T) {
	source := tempGitDir(t)
	bin := filepath.Join(t.TempDir(), "agent-center")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := NewGitBuildDeployRuntime(GitBuildDeployRuntimeConfig{SourceDir: source, BinaryPath: bin})
	d.run = newDeployRunner(t, source, nil).run

	_, err := d.DeployRestart(context.Background(), DeployRequest{
		Remote: "origin", Ref: "refs/heads/release", ExactSHA: testBaseSHA,
		ExactSHAVerified: true, AncestorVerified: true,
	})
	if err == nil || !strings.Contains(err.Error(), "runtime_deploy_exact_sha_mismatch") {
		t.Fatalf("err=%v want exact SHA mismatch", err)
	}
}

func TestGitBuildDeployRuntime_RejectsFailedAncestryDespiteCallerBooleans(t *testing.T) {
	source := tempGitDir(t)
	bin := filepath.Join(t.TempDir(), "agent-center")
	if err := os.WriteFile(bin, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := NewGitBuildDeployRuntime(GitBuildDeployRuntimeConfig{SourceDir: source, BinaryPath: bin})
	d.run = newDeployRunner(t, source, map[string]error{
		"git merge-base --is-ancestor " + testBaseSHA + " " + testTargetSHA: errors.New("not ancestor"),
	}).run

	_, err := d.DeployRestart(context.Background(), DeployRequest{
		Remote: "origin", Ref: "refs/heads/release", ExactSHA: testTargetSHA, BaseRef: "refs/heads/main",
		ExactSHAVerified: true, AncestorVerified: true,
	})
	if err == nil || !strings.Contains(err.Error(), "runtime_deploy_ancestor_check_failed") {
		t.Fatalf("err=%v want authoritative ancestry failure", err)
	}
}

func TestControllerHandler_RuntimeDeployNotWiredFails(t *testing.T) {
	h := controllerHandler{log: func(string) {}}
	err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeRuntimeDeploy,
		Payload:     `{"ref":"refs/heads/release","exact_sha":"` + testTargetSHA + `"}`,
	})
	if err == nil || err.Error() != "runtime_deployer_not_wired" {
		t.Fatalf("err=%v want runtime_deployer_not_wired", err)
	}
}

func TestControllerHandler_RuntimeDeployRoutesToDeployerWithoutAgentID(t *testing.T) {
	fd := &fakeDeployRuntime{}
	h := controllerHandler{deployer: fd, log: func(string) {}}
	err := h.Handle(context.Background(), ControlCommand{
		CommandType: cmdTypeRuntimeDeploy,
		Payload:     `{"ref":"refs/heads/release","exact_sha":"` + testTargetSHA + `"}`,
	})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if fd.calls != 1 || fd.last.Ref != "refs/heads/release" || fd.last.ExactSHA != testTargetSHA {
		t.Fatalf("deployer calls=%d last=%+v", fd.calls, fd.last)
	}
}

type fakeDeployRuntime struct {
	calls int
	last  DeployRequest
}

func (f *fakeDeployRuntime) DeployRestart(_ context.Context, req DeployRequest) (DeployResult, error) {
	f.calls++
	f.last = req
	return DeployResult{Ref: req.Ref, ResolvedSHA: req.ExactSHA}, nil
}

type deployRunner struct {
	t      *testing.T
	source string
	calls  []string
	fail   map[string]error
}

func newDeployRunner(t *testing.T, source string, fail map[string]error) *deployRunner {
	t.Helper()
	return &deployRunner{t: t, source: source, fail: fail}
}

func (r *deployRunner) run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	r.calls = append(r.calls, call)
	if err := r.fail[call]; err != nil {
		return nil, err
	}
	switch call {
	case "git ls-remote --exit-code origin refs/heads/release":
		return []byte(testTargetSHA + "\trefs/heads/release\n"), nil
	case "git ls-remote --exit-code origin refs/heads/main":
		return []byte(testBaseSHA + "\trefs/heads/main\n"), nil
	case "git fetch --quiet origin refs/heads/release refs/heads/main",
		"git rev-parse --verify " + testTargetSHA + "^{commit}",
		"git rev-parse --verify " + testBaseSHA + "^{commit}",
		"git merge-base --is-ancestor " + testBaseSHA + " " + testTargetSHA:
		return []byte{}, nil
	}
	if strings.HasPrefix(call, "git worktree add --detach ") {
		wt := args[3]
		if err := os.MkdirAll(filepath.Join(wt, "cmd", "agent-center"), 0o755); err != nil {
			r.t.Fatal(err)
		}
		return []byte{}, nil
	}
	if strings.HasPrefix(call, "git worktree remove --force ") {
		return []byte{}, nil
	}
	if strings.HasPrefix(call, "go build ") {
		for i, arg := range args {
			if arg == "-o" && i+1 < len(args) {
				if err := os.WriteFile(args[i+1], []byte("new-runtime"), 0o755); err != nil {
					r.t.Fatal(err)
				}
				return []byte{}, nil
			}
		}
		r.t.Fatalf("go build call without -o: %v", args)
	}
	r.t.Fatalf("unexpected command dir=%s call=%s", dir, call)
	return nil, nil
}

func (r *deployRunner) called(want string) bool {
	for _, call := range r.calls {
		if call == want {
			return true
		}
	}
	return false
}

func tempGitDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}
