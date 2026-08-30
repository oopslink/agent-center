package workerdaemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/runtimedeploy"
)

type sourceRuntimeDeployer struct {
	workRoot string
	workerID string
	log      func(string)
	run      func(context.Context, string, string, []string, []string) ([]byte, error)
}

func newSourceRuntimeDeployer(workRoot, workerID string, log func(string)) *sourceRuntimeDeployer {
	return &sourceRuntimeDeployer{
		workRoot: workRoot,
		workerID: workerID,
		log:      log,
		run:      runDeployCommand,
	}
}

func (d *sourceRuntimeDeployer) DeployRestart(ctx context.Context, req runtimedeploy.Request) (runtimedeploy.Result, error) {
	if d == nil {
		return runtimedeploy.Result{}, errors.New("runtime deployer not configured")
	}
	timeout := runtimedeploy.Timeout(req, 10*time.Minute)
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	sha := strings.ToLower(strings.TrimSpace(req.VerifiedTargetSHA))
	if sha == "" {
		sha = strings.ToLower(strings.TrimSpace(req.TargetSHA))
	}
	if !fullDeploySHA(sha) {
		return runtimedeploy.Result{}, fmt.Errorf("invalid verified sha %q", sha)
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "center"
	}
	if mode != "center" && mode != "worker" {
		return runtimedeploy.Result{}, fmt.Errorf("invalid deploy mode %q", mode)
	}
	root := strings.TrimSpace(d.workRoot)
	if root == "" {
		root = filepath.Join(os.TempDir(), "agent-center-runtime-deploy")
	}
	sourceDir := runtimedeploy.ManagedSourceDir(root, sha)
	stageDir := filepath.Join(root, "stage-"+sha[:12])
	if err := os.RemoveAll(sourceDir); err != nil {
		return runtimedeploy.Result{}, err
	}
	if err := os.RemoveAll(stageDir); err != nil {
		return runtimedeploy.Result{}, err
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		return runtimedeploy.Result{}, err
	}
	var transcript bytes.Buffer
	run := func(dir, name string, args ...string) error {
		out, err := d.run(ctx, dir, name, args, deployCommandEnv())
		if len(out) > 0 {
			transcript.Write(out)
			if !bytes.HasSuffix(out, []byte("\n")) {
				transcript.WriteByte('\n')
			}
		}
		return err
	}
	if err := run(root, "git", "clone", "--no-checkout", strings.TrimSpace(req.RepoURL), sourceDir); err != nil {
		return runtimedeploy.Result{}, fmt.Errorf("clone source: %w", err)
	}
	targetRef := strings.TrimSpace(req.TargetRef)
	if targetRef != "" {
		if err := run(sourceDir, "git", "fetch", "--no-tags", "origin", "+"+targetRef+":refs/ac-runtime-deploy/target"); err != nil {
			return runtimedeploy.Result{}, fmt.Errorf("fetch target ref: %w", err)
		}
	}
	if err := run(sourceDir, "git", "checkout", "--detach", sha); err != nil {
		return runtimedeploy.Result{}, fmt.Errorf("checkout verified sha: %w", err)
	}
	version := "runtime-deploy-" + sha[:12]
	if err := run(sourceDir, "make", "release-dir", "VERSION="+version, "OUT="+stageDir); err != nil {
		return runtimedeploy.Result{}, fmt.Errorf("build release: %w", err)
	}
	upgrade := filepath.Join(stageDir, "upgrade")
	args := []string{mode, "--force"}
	if prefix := strings.TrimSpace(req.Prefix); prefix != "" {
		args = append(args, "--prefix="+prefix)
	}
	if mode == "worker" {
		if strings.TrimSpace(d.workerID) == "" {
			return runtimedeploy.Result{}, errors.New("worker deploy requires worker id")
		}
		args = append(args, "--worker-id="+d.workerID)
	}
	if err := run(stageDir, upgrade, args...); err != nil {
		return runtimedeploy.Result{}, fmt.Errorf("upgrade %s: %w", mode, err)
	}
	runningVersion, runningCommit, err := d.readStagedBuildIdentity(ctx, stageDir, sha)
	if err != nil {
		return runtimedeploy.Result{}, fmt.Errorf("post-restart health readback: %w", err)
	}
	return runtimedeploy.Result{
		TargetSHA:               sha,
		Mode:                    mode,
		RunningSHA:              sha,
		RunningVersion:          runningVersion,
		RunningCommit:           runningCommit,
		PostRestartHealthStatus: "version_readback_ok",
		Output:                  trimDeployTranscript(transcript.String()),
	}, nil
}

func (d *sourceRuntimeDeployer) readStagedBuildIdentity(ctx context.Context, stageDir, sha string) (version, commit string, err error) {
	out, err := d.run(ctx, stageDir, filepath.Join(stageDir, "bin", "agent-center"), []string{"version"}, deployCommandEnv())
	if err != nil {
		return "", "", fmt.Errorf("agent-center version: %w: %s", err, trimDeployTranscript(string(out)))
	}
	version, commit, err = parseAgentCenterVersionReadback(string(out))
	if err != nil {
		return "", "", err
	}
	if version != "runtime-deploy-"+sha[:12] {
		return "", "", fmt.Errorf("running version %q does not match target sha %s", version, sha)
	}
	if !strings.HasPrefix(sha, strings.ToLower(commit)) {
		return "", "", fmt.Errorf("running commit %q does not match target sha %s", commit, sha)
	}
	return version, strings.ToLower(commit), nil
}

func parseAgentCenterVersionReadback(out string) (version, commit string, err error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) != 4 || fields[0] != "agent-center" || fields[2] != "(commit" || !strings.HasSuffix(fields[3], ")") {
		return "", "", fmt.Errorf("unexpected version readback %q", strings.TrimSpace(out))
	}
	version = strings.TrimSpace(fields[1])
	commit = strings.TrimSuffix(fields[3], ")")
	if version == "" || commit == "" || commit == "unknown" {
		return "", "", fmt.Errorf("incomplete version readback %q", strings.TrimSpace(out))
	}
	return version, commit, nil
}

func fullDeploySHA(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

func deployCommandEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_CONFIG_GLOBAL=/dev/null",
		"GIT_CONFIG_SYSTEM=/dev/null",
	)
}

func runDeployCommand(ctx context.Context, dir, name string, args []string, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	cmd.Env = env
	return cmd.CombinedOutput()
}

func trimDeployTranscript(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 8000 {
		return s[len(s)-8000:]
	}
	return s
}
