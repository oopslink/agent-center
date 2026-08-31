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
	"github.com/oopslink/agent-center/internal/workforce"
)

type sourceRuntimeDeployer struct {
	workRoot string
	workerID string
	readback workerIdentityReadback
	log      func(string)
	run      func(context.Context, string, string, []string, []string) ([]byte, error)
}

type workerIdentityReadback interface {
	FindWorkerByID(context.Context, string) (WorkerReadback, error)
}

type WorkerReadback struct {
	WorkerID   string               `json:"worker_id"`
	Status     string               `json:"status"`
	Version    int                  `json:"version"`
	SystemInfo workforce.SystemInfo `json:"system_info"`
}

func newSourceRuntimeDeployer(workRoot, workerID string, readback workerIdentityReadback, log func(string)) *sourceRuntimeDeployer {
	return &sourceRuntimeDeployer{
		workRoot: workRoot,
		workerID: workerID,
		readback: readback,
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
	if err := run(sourceDir, "make", "release-dir", "VERSION="+version, "COMMIT="+sha, "OUT="+stageDir); err != nil {
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
	runningVersion, runningCommit, health, err := d.readPostRestartWorkerIdentity(ctx, sha)
	if err != nil {
		return runtimedeploy.Result{}, fmt.Errorf("post-restart health readback: %w", err)
	}
	return runtimedeploy.Result{
		TargetSHA:               sha,
		Mode:                    mode,
		RunningSHA:              sha,
		RunningVersion:          runningVersion,
		RunningCommit:           runningCommit,
		PostRestartHealthStatus: health,
		Output:                  trimDeployTranscript(transcript.String()),
	}, nil
}

func (d *sourceRuntimeDeployer) readPostRestartWorkerIdentity(ctx context.Context, sha string) (version, commit, health string, err error) {
	if d.readback == nil {
		return "", "", "", errors.New("authenticated worker identity readback not configured")
	}
	if strings.TrimSpace(d.workerID) == "" {
		return "", "", "", errors.New("worker identity readback requires worker id")
	}
	deadline := time.Now().Add(45 * time.Second)
	var lastErr error
	for {
		readCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		rb, rerr := d.readback.FindWorkerByID(readCtx, d.workerID)
		cancel()
		if rerr != nil {
			lastErr = fmt.Errorf("worker identity endpoint unavailable: %w", rerr)
		} else if err := validatePostRestartWorkerIdentity(rb, d.workerID, sha); err != nil {
			lastErr = err
		} else {
			si := rb.SystemInfo
			return strings.TrimSpace(si.WorkerVersion), strings.ToLower(strings.TrimSpace(si.BuildCommit)), "worker_identity_readback_ok", nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			if ctx.Err() != nil {
				return "", "", "", fmt.Errorf("worker identity readback timed out: %w", ctx.Err())
			}
			return "", "", "", fmt.Errorf("worker identity readback timed out waiting for target sha %s: %w", sha, lastErr)
		}
		select {
		case <-time.After(500 * time.Millisecond):
		case <-ctx.Done():
			return "", "", "", fmt.Errorf("worker identity readback timed out: %w", ctx.Err())
		}
	}
}

func validatePostRestartWorkerIdentity(rb WorkerReadback, workerID, sha string) error {
	if strings.TrimSpace(rb.WorkerID) != workerID {
		return fmt.Errorf("worker identity endpoint returned worker_id %q, want %q", rb.WorkerID, workerID)
	}
	if strings.TrimSpace(rb.Status) != workforce.WorkerOnline.String() {
		return fmt.Errorf("worker %s unhealthy after restart: status=%q", workerID, rb.Status)
	}
	si := rb.SystemInfo
	version := strings.TrimSpace(si.WorkerVersion)
	commit := strings.ToLower(strings.TrimSpace(si.BuildCommit))
	if version == "" || commit == "" || commit == "unknown" {
		return fmt.Errorf("incomplete worker identity readback: version=%q commit=%q", version, commit)
	}
	if version != "runtime-deploy-"+sha[:12] {
		return fmt.Errorf("running worker version %q does not match target sha %s", version, sha)
	}
	if !strings.HasPrefix(sha, commit) {
		return fmt.Errorf("running worker commit %q does not match target sha %s", commit, sha)
	}
	return nil
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
