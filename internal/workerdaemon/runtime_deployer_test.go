package workerdaemon

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/runtimedeploy"
)

func TestSourceRuntimeDeployerReportsAuthoritativeReadback(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "install")
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	var commands []string
	d := newSourceRuntimeDeployer(root, "worker-1", func(string) {})
	d.run = func(_ context.Context, dir, name string, args []string, _ []string) ([]byte, error) {
		commands = append(commands, name+" "+strings.Join(args, " "))
		switch {
		case name == "git" && len(args) > 0 && args[0] == "clone":
			if err := os.MkdirAll(runtimedeploy.ManagedSourceDir(root, sha), 0o700); err != nil {
				return nil, err
			}
		case name == "make" && slices.Contains(args, "OUT="+filepath.Join(root, "stage-"+sha[:12])):
			if err := os.MkdirAll(filepath.Join(root, "stage-"+sha[:12], "bin"), 0o700); err != nil {
				return nil, err
			}
		case name == filepath.Join(prefix, "current", "bin", "agent-center") && strings.Join(args, " ") == "version":
			return []byte("agent-center runtime-deploy-" + sha[:12] + " (commit " + sha + ")\n"), nil
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
		Prefix:            prefix,
	})
	if err != nil {
		t.Fatalf("DeployRestart: %v", err)
	}
	if got.TargetSHA != sha || got.RunningSHA != sha || got.RunningVersion != "runtime-deploy-"+sha[:12] ||
		got.RunningCommit != sha || got.PostRestartHealthStatus != "version_readback_ok" {
		t.Fatalf("deploy result missing authoritative readback: %+v", got)
	}
	if !slices.Contains(commands, "make release-dir VERSION=runtime-deploy-"+sha[:12]+" COMMIT="+sha+" OUT="+filepath.Join(root, "stage-"+sha[:12])) {
		t.Fatalf("make release-dir did not receive full COMMIT; commands=%v", commands)
	}
	if !slices.Contains(commands, filepath.Join(prefix, "current", "bin", "agent-center")+" version") {
		t.Fatalf("installed version readback command not run; commands=%v", commands)
	}
}

func TestParseAgentCenterVersionReadbackRejectsUnknownCommit(t *testing.T) {
	_, _, err := parseAgentCenterVersionReadback("agent-center runtime-deploy-aaaaaaaaaaaa (commit unknown)\n")
	if err == nil || !strings.Contains(err.Error(), "incomplete version readback") {
		t.Fatalf("parse should reject unknown commit, got %v", err)
	}
}

func TestSourceRuntimeDeployerRejectsNonExactInstalledCommitReadback(t *testing.T) {
	root := t.TempDir()
	prefix := filepath.Join(root, "install")
	sha := "aaaaaaaaaaaabbbbbbbbbbbbccccccccccccdddd"
	d := newSourceRuntimeDeployer(root, "worker-1", func(string) {})
	d.run = func(_ context.Context, _ string, name string, args []string, _ []string) ([]byte, error) {
		switch {
		case name == "git" && len(args) > 0 && args[0] == "clone":
			return []byte("ok\n"), os.MkdirAll(runtimedeploy.ManagedSourceDir(root, sha), 0o700)
		case name == filepath.Join(prefix, "current", "bin", "agent-center") && strings.Join(args, " ") == "version":
			return []byte("agent-center runtime-deploy-" + sha[:12] + " (commit " + sha[:7] + ")\n"), nil
		default:
			return []byte("ok\n"), nil
		}
	}
	_, err := d.DeployRestart(context.Background(), runtimedeploy.Request{
		RepoURL:           "https://example.invalid/repo.git",
		TargetRef:         "refs/heads/main",
		TargetSHA:         sha,
		VerifiedTargetSHA: sha,
		VerifiedBaseSHA:   strings.Repeat("b", 40),
		VerifiedAt:        "2026-08-31T00:00:00Z",
		Mode:              "center",
		Prefix:            prefix,
	})
	if err == nil || !strings.Contains(err.Error(), "does not match target sha") {
		t.Fatalf("short/non-exact installed commit should fail closed, got %v", err)
	}
}
