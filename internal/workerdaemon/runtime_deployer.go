package workerdaemon

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/admin/clienttransport"
	"github.com/oopslink/agent-center/internal/config"
	"github.com/oopslink/agent-center/internal/runtimedeploy"
	"github.com/oopslink/agent-center/internal/workforce"
)

type sourceRuntimeDeployer struct {
	workRoot string
	workerID string
	log      func(string)
	run      func(context.Context, string, string, []string, []string) ([]byte, error)
	readback func(context.Context, string, string, string, string) (runtimeBuildReadback, error)
}

type runtimeBuildReadback struct {
	Version string
	Commit  string
	Health  string
}

func newSourceRuntimeDeployer(workRoot, workerID string, log func(string)) *sourceRuntimeDeployer {
	return &sourceRuntimeDeployer{
		workRoot: workRoot,
		workerID: workerID,
		log:      log,
		run:      runDeployCommand,
		readback: defaultRuntimeBuildReadback,
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
	rb, err := d.readback(ctx, mode, req.Prefix, d.workerID, sha)
	if err != nil {
		return runtimedeploy.Result{}, fmt.Errorf("post-restart health readback: %w", err)
	}
	res := runtimedeploy.Result{
		TargetSHA:               sha,
		Mode:                    mode,
		RunningSHA:              strings.ToLower(strings.TrimSpace(rb.Commit)),
		RunningVersion:          strings.TrimSpace(rb.Version),
		RunningCommit:           strings.ToLower(strings.TrimSpace(rb.Commit)),
		RestartTerminalSuccess:  true,
		PostRestartHealthStatus: strings.TrimSpace(rb.Health),
		Output:                  trimDeployTranscript(transcript.String()),
	}
	if err := runtimedeploy.ValidateTerminalSuccessResult(res, sha); err != nil {
		return runtimedeploy.Result{}, err
	}
	return res, nil
}

func defaultRuntimeBuildReadback(ctx context.Context, mode, prefix, workerID, expectedSHA string) (runtimeBuildReadback, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "worker":
		return runtimeBuildReadback{}, errors.New("worker process readback not configured")
	default:
		return readCenterAdminHealth(ctx, prefix)
	}
}

func readCenterAdminHealth(ctx context.Context, prefix string) (runtimeBuildReadback, error) {
	cfg, err := loadRuntimeDeployConfig(prefix)
	if err != nil {
		return runtimeBuildReadback{}, err
	}
	targetSpec := strings.TrimSpace(cfg.Server.AdminSocketPath)
	if targetSpec == "" && strings.TrimSpace(cfg.Server.AdminTCPListen) != "" {
		targetSpec = "tcp://" + strings.TrimSpace(cfg.Server.AdminTCPListen)
	}
	target, err := clienttransport.ParseTarget(targetSpec)
	if err != nil {
		return runtimeBuildReadback{}, err
	}
	tr, err := clienttransport.NewHTTPTransport(target, "", 5*time.Second)
	if err != nil {
		return runtimeBuildReadback{}, err
	}
	defer tr.CloseIdleConnections()
	httpc := &http.Client{Transport: tr, Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.BaseURL()+"/admin/health", nil)
	if err != nil {
		return runtimeBuildReadback{}, err
	}
	resp, err := httpc.Do(req)
	if err != nil {
		return runtimeBuildReadback{}, fmt.Errorf("admin health readback: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return runtimeBuildReadback{}, fmt.Errorf("admin health readback status=%d body=%s", resp.StatusCode, trimDeployTranscript(string(body)))
	}
	var doc struct {
		OK      bool   `json:"ok"`
		Health  string `json:"health,omitempty"`
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return runtimeBuildReadback{}, fmt.Errorf("admin health readback json: %w", err)
	}
	if !doc.OK {
		return runtimeBuildReadback{}, errors.New("admin health readback not ok")
	}
	health := strings.TrimSpace(doc.Health)
	if health == "" && doc.OK {
		health = "healthy"
	}
	return runtimeBuildReadback{Version: doc.Version, Commit: doc.Commit, Health: health}, nil
}

func readWorkerAdminReadback(ctx context.Context, targetSpec, fingerprint, tokenPath, workerID, expectedSHA string) (runtimeBuildReadback, error) {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return runtimeBuildReadback{}, errors.New("worker readback requires worker id")
	}
	expectedSHA = strings.ToLower(strings.TrimSpace(expectedSHA))
	if !fullDeploySHA(expectedSHA) {
		return runtimeBuildReadback{}, fmt.Errorf("worker readback requires full expected sha, got %q", expectedSHA)
	}
	target, err := clienttransport.ParseTarget(targetSpec)
	if err != nil {
		return runtimeBuildReadback{}, err
	}
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	var lastErr error
	for {
		rb, err := readWorkerAdminReadbackOnce(ctx, target, fingerprint, tokenPath, workerID)
		if err == nil {
			commit := strings.ToLower(strings.TrimSpace(rb.Commit))
			switch {
			case commit == expectedSHA:
				return rb, nil
			case commit == "":
				lastErr = errors.New("worker readback missing running sha")
			default:
				lastErr = fmt.Errorf("worker readback stale running sha %s, want %s", commit, expectedSHA)
			}
		} else {
			lastErr = err
		}
		select {
		case <-ctx.Done():
			return runtimeBuildReadback{}, fmt.Errorf("worker readback timeout: %w (last=%v)", ctx.Err(), lastErr)
		case <-deadline.C:
			return runtimeBuildReadback{}, fmt.Errorf("worker readback timeout waiting for %s (last=%v)", expectedSHA, lastErr)
		case <-tick.C:
		}
	}
}

func readWorkerAdminReadbackOnce(ctx context.Context, target clienttransport.Target, fingerprint, tokenPath, workerID string) (runtimeBuildReadback, error) {
	token, err := readWorkerTokenFile(tokenPath)
	if err != nil {
		return runtimeBuildReadback{}, fmt.Errorf("worker readback token unavailable: %w", err)
	}
	client, err := NewAdminClientFromTarget(target, fingerprint, 5*time.Second)
	if err != nil {
		return runtimeBuildReadback{}, fmt.Errorf("worker readback admin client: %w", err)
	}
	client = client.WithToken(token)
	info, err := client.WorkerFindByID(ctx, workerID)
	if err != nil {
		return runtimeBuildReadback{}, fmt.Errorf("worker readback unavailable: %w", err)
	}
	if strings.TrimSpace(info.WorkerID) != workerID {
		return runtimeBuildReadback{}, fmt.Errorf("worker readback identity mismatch: got %q want %q", info.WorkerID, workerID)
	}
	if strings.TrimSpace(info.Status) != string(workforce.WorkerOnline) {
		return runtimeBuildReadback{}, fmt.Errorf("worker readback unhealthy status %q", info.Status)
	}
	sys := info.SystemInfo
	version := strings.TrimSpace(sys.WorkerVersion)
	commit := strings.ToLower(strings.TrimSpace(sys.BuildCommit))
	switch {
	case version == "":
		return runtimeBuildReadback{}, errors.New("worker readback missing running version")
	case !fullDeploySHA(commit):
		return runtimeBuildReadback{}, fmt.Errorf("worker readback running sha must be full 40 character commit SHA, got %q", commit)
	}
	return runtimeBuildReadback{Version: version, Commit: commit, Health: "healthy"}, nil
}

func loadRuntimeDeployConfig(prefix string) (config.Config, error) {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return config.Config{}, fmt.Errorf("resolve install prefix: %w", err)
		}
		prefix = filepath.Join(home, ".agent-center")
	}
	return config.Load(config.LoadOptions{Path: filepath.Join(prefix, "etc", "config.yaml")})
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
