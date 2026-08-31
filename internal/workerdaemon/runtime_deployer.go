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
)

type sourceRuntimeDeployer struct {
	workRoot string
	workerID string
	log      func(string)
	run      func(context.Context, string, string, []string, []string) ([]byte, error)
	readback func(context.Context, string, string, string) (runtimeBuildReadback, error)
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
	rb, err := d.readback(ctx, mode, req.Prefix, d.workerID)
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

func defaultRuntimeBuildReadback(ctx context.Context, mode, prefix, workerID string) (runtimeBuildReadback, error) {
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
		Version string `json:"version"`
		Commit  string `json:"commit"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return runtimeBuildReadback{}, fmt.Errorf("admin health readback json: %w", err)
	}
	if !doc.OK {
		return runtimeBuildReadback{}, errors.New("admin health readback not ok")
	}
	return runtimeBuildReadback{Version: doc.Version, Commit: doc.Commit, Health: "healthy"}, nil
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
