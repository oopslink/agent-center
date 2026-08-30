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
		case name == filepath.Join(root, "stage-"+sha[:12], "bin", "agent-center") && strings.Join(args, " ") == "version":
			return []byte("agent-center runtime-deploy-" + sha[:12] + " (commit " + sha[:7] + ")\n"), nil
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
		got.RunningCommit != sha[:7] || got.PostRestartHealthStatus != "version_readback_ok" {
		t.Fatalf("deploy result missing authoritative readback: %+v", got)
	}
	if !slices.Contains(commands, filepath.Join(root, "stage-"+sha[:12], "bin", "agent-center")+" version") {
		t.Fatalf("version readback command not run; commands=%v", commands)
	}
}

func TestParseAgentCenterVersionReadbackRejectsUnknownCommit(t *testing.T) {
	_, _, err := parseAgentCenterVersionReadback("agent-center runtime-deploy-aaaaaaaaaaaa (commit unknown)\n")
	if err == nil || !strings.Contains(err.Error(), "incomplete version readback") {
		t.Fatalf("parse should reject unknown commit, got %v", err)
	}
}
