package workerdaemon

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/workerdaemon/workercontroller"
)

// DeployRuntime applies a new worker/agent-runtime binary and restarts supervised
// agent runtime processes so they come back on the verified build.
type DeployRuntime interface {
	DeployRestart(ctx context.Context, req DeployRequest) (DeployResult, error)
}

// DeployRequest is the runtime.deploy_restart command payload. The *_verified fields
// may exist in older callers, but are intentionally ignored: this worker resolves the
// remote refs and proves exact SHA + ancestry itself on every command.
type DeployRequest struct {
	Remote           string `json:"remote,omitempty"`
	Ref              string `json:"ref"`
	ExactSHA         string `json:"exact_sha,omitempty"`
	BaseRef          string `json:"base_ref,omitempty"`
	ExactSHAVerified bool   `json:"exact_sha_verified,omitempty"`
	AncestorVerified bool   `json:"ancestor_verified,omitempty"`
}

type DeployResult struct {
	Remote          string
	Ref             string
	ResolvedSHA     string
	BaseRef         string
	BaseSHA         string
	BinaryPath      string
	RestartedAgents int
}

type GitBuildDeployRuntimeConfig struct {
	SourceDir  string
	BinaryPath string
	Controller *workercontroller.Controller
	Log        func(string)
}

type GitBuildDeployRuntime struct {
	sourceDir  string
	binaryPath string
	controller *workercontroller.Controller
	log        func(string)
	run        func(context.Context, string, string, ...string) ([]byte, error)
}

func NewGitBuildDeployRuntime(cfg GitBuildDeployRuntimeConfig) *GitBuildDeployRuntime {
	logf := cfg.Log
	if logf == nil {
		logf = func(string) {}
	}
	return &GitBuildDeployRuntime{
		sourceDir:  strings.TrimSpace(cfg.SourceDir),
		binaryPath: strings.TrimSpace(cfg.BinaryPath),
		controller: cfg.Controller,
		log:        logf,
		run:        runCommand,
	}
}

func (d *GitBuildDeployRuntime) DeployRestart(ctx context.Context, req DeployRequest) (DeployResult, error) {
	remote := strings.TrimSpace(req.Remote)
	if remote == "" {
		remote = "origin"
	}
	ref := strings.TrimSpace(req.Ref)
	if ref == "" {
		return DeployResult{}, errors.New("runtime_deploy_ref_required")
	}
	ref = normalizeRemoteRef(remote, ref)
	baseRef := strings.TrimSpace(req.BaseRef)
	if baseRef == "" {
		baseRef = "refs/heads/main"
	}
	baseRef = normalizeRemoteRef(remote, baseRef)
	sourceDir, err := d.resolveSourceDir()
	if err != nil {
		return DeployResult{}, err
	}
	binaryPath, err := d.resolveBinaryPath()
	if err != nil {
		return DeployResult{}, err
	}

	targetSHA, err := d.resolveRemoteRef(ctx, sourceDir, remote, ref)
	if err != nil {
		return DeployResult{}, fmt.Errorf("runtime_deploy_resolve_ref: %w", err)
	}
	if exact := strings.TrimSpace(req.ExactSHA); exact != "" && !sameSHA(exact, targetSHA) {
		return DeployResult{}, fmt.Errorf("runtime_deploy_exact_sha_mismatch: remote %s %s resolved %s, want %s", remote, ref, targetSHA, exact)
	}
	baseSHA, err := d.resolveRemoteRef(ctx, sourceDir, remote, baseRef)
	if err != nil {
		return DeployResult{}, fmt.Errorf("runtime_deploy_resolve_base: %w", err)
	}
	if err := d.fetchRefs(ctx, sourceDir, remote, ref, baseRef); err != nil {
		return DeployResult{}, err
	}
	if err := d.verifyCommit(ctx, sourceDir, targetSHA); err != nil {
		return DeployResult{}, err
	}
	if err := d.verifyCommit(ctx, sourceDir, baseSHA); err != nil {
		return DeployResult{}, err
	}
	if err := d.verifyAncestor(ctx, sourceDir, baseSHA, targetSHA); err != nil {
		return DeployResult{}, err
	}
	if err := d.buildAndSwap(ctx, sourceDir, binaryPath, targetSHA, ref); err != nil {
		return DeployResult{}, err
	}
	restarted := 0
	if d.controller != nil {
		restarted, err = d.controller.RestartRunning(ctx)
		if err != nil {
			return DeployResult{}, fmt.Errorf("runtime_deploy_restart_agents: %w", err)
		}
	}
	return DeployResult{
		Remote: remote, Ref: ref, ResolvedSHA: targetSHA, BaseRef: baseRef, BaseSHA: baseSHA,
		BinaryPath: binaryPath, RestartedAgents: restarted,
	}, nil
}

func (d *GitBuildDeployRuntime) resolveSourceDir() (string, error) {
	if d.sourceDir != "" {
		return filepath.Abs(d.sourceDir)
	}
	if env := strings.TrimSpace(os.Getenv("AGENT_CENTER_SOURCE_DIR")); env != "" {
		abs, err := filepath.Abs(env)
		if err != nil {
			return "", err
		}
		d.sourceDir = abs
		return abs, nil
	}
	wd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for dir := wd; ; dir = filepath.Dir(dir) {
		if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && (st.IsDir() || st.Mode().IsRegular()) {
			d.sourceDir = dir
			return dir, nil
		}
		next := filepath.Dir(dir)
		if next == dir {
			return "", errors.New("runtime_deploy_source_unavailable: no git checkout found")
		}
	}
}

func (d *GitBuildDeployRuntime) resolveBinaryPath() (string, error) {
	if d.binaryPath != "" {
		return filepath.Abs(d.binaryPath)
	}
	self, err := os.Executable()
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(self)
	if err == nil {
		self = resolved
	}
	d.binaryPath = self
	return self, nil
}

func (d *GitBuildDeployRuntime) resolveRemoteRef(ctx context.Context, dir, remote, ref string) (string, error) {
	out, err := d.run(ctx, dir, "git", "ls-remote", "--exit-code", remote, ref)
	if err != nil {
		return "", err
	}
	lines := bytes.Split(bytes.TrimSpace(out), []byte{'\n'})
	if len(lines) != 1 {
		return "", fmt.Errorf("remote ref %q resolved to %d matches", ref, len(lines))
	}
	fields := strings.Fields(string(lines[0]))
	if len(fields) < 2 {
		return "", fmt.Errorf("unexpected ls-remote output for %q", ref)
	}
	if !isFullSHA(fields[0]) {
		return "", fmt.Errorf("remote ref %q resolved non-SHA %q", ref, fields[0])
	}
	return fields[0], nil
}

func (d *GitBuildDeployRuntime) fetchRefs(ctx context.Context, dir, remote, ref, baseRef string) error {
	args := []string{"fetch", "--quiet", remote, ref, baseRef}
	if _, err := d.run(ctx, dir, "git", args...); err != nil {
		return fmt.Errorf("runtime_deploy_fetch: %w", err)
	}
	return nil
}

func (d *GitBuildDeployRuntime) verifyCommit(ctx context.Context, dir, sha string) error {
	if _, err := d.run(ctx, dir, "git", "rev-parse", "--verify", sha+"^{commit}"); err != nil {
		return fmt.Errorf("runtime_deploy_verify_commit %s: %w", sha, err)
	}
	return nil
}

func (d *GitBuildDeployRuntime) verifyAncestor(ctx context.Context, dir, baseSHA, targetSHA string) error {
	if _, err := d.run(ctx, dir, "git", "merge-base", "--is-ancestor", baseSHA, targetSHA); err != nil {
		return fmt.Errorf("runtime_deploy_ancestor_check_failed: %s is not an ancestor of %s", baseSHA, targetSHA)
	}
	return nil
}

func (d *GitBuildDeployRuntime) buildAndSwap(ctx context.Context, sourceDir, binaryPath, targetSHA, ref string) error {
	tmp, err := os.MkdirTemp("", "ac-runtime-deploy-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	wt := filepath.Join(tmp, "src")
	if _, err := d.run(ctx, sourceDir, "git", "worktree", "add", "--detach", wt, targetSHA); err != nil {
		return fmt.Errorf("runtime_deploy_worktree: %w", err)
	}
	defer func() {
		_, _ = d.run(context.Background(), sourceDir, "git", "worktree", "remove", "--force", wt)
	}()
	outBin := filepath.Join(tmp, "agent-center")
	short := targetSHA
	if len(short) > 12 {
		short = short[:12]
	}
	builtAt := time.Now().UTC().Format(time.RFC3339)
	version := sanitizeVersionRef(ref) + "-" + short
	ldflags := fmt.Sprintf("-X main.buildVersion=%s -X main.buildCommit=%s -X main.buildBranch=%s -X main.buildBuiltAt=%s",
		version, short, sanitizeVersionRef(ref), builtAt)
	if _, err := d.run(ctx, wt, "go", "build", "-ldflags", ldflags, "-o", outBin, "./cmd/agent-center"); err != nil {
		return fmt.Errorf("runtime_deploy_build: %w", err)
	}
	info, err := os.Stat(outBin)
	if err != nil {
		return fmt.Errorf("runtime_deploy_build_missing: %w", err)
	}
	if info.Size() == 0 {
		return errors.New("runtime_deploy_build_empty")
	}
	if err := os.Chmod(outBin, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0o755); err != nil {
		return err
	}
	swap := binaryPath + ".new"
	if err := copyFile(outBin, swap, 0o755); err != nil {
		return fmt.Errorf("runtime_deploy_stage_binary: %w", err)
	}
	if err := os.Rename(swap, binaryPath); err != nil {
		_ = os.Remove(swap)
		return fmt.Errorf("runtime_deploy_swap_binary: %w", err)
	}
	d.log(fmt.Sprintf("runtime deploy_restart swapped binary=%s sha=%s", binaryPath, targetSHA))
	return nil
}

func runCommand(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, mode)
}

var fullSHARe = regexp.MustCompile(`^[0-9a-f]{40}$`)

func isFullSHA(s string) bool {
	return fullSHARe.MatchString(strings.ToLower(strings.TrimSpace(s)))
}

func sameSHA(a, b string) bool {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if len(a) < 7 || len(b) < 7 {
		return a == b
	}
	if len(a) > len(b) {
		return strings.HasPrefix(a, b)
	}
	return strings.HasPrefix(b, a)
}

func sanitizeVersionRef(ref string) string {
	ref = strings.TrimSpace(ref)
	ref = strings.TrimPrefix(ref, "refs/heads/")
	ref = strings.TrimPrefix(ref, "refs/tags/")
	if ref == "" {
		ref = "deploy"
	}
	var b strings.Builder
	for _, r := range ref {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		case r == '/':
			b.WriteRune('-')
		}
	}
	out := b.String()
	if out == "" {
		sum := sha1.Sum([]byte(ref))
		out = "deploy-" + hex.EncodeToString(sum[:4])
	}
	return out
}

func normalizeRemoteRef(remote, ref string) string {
	ref = strings.TrimSpace(ref)
	remotePrefix := strings.TrimSpace(remote) + "/"
	if strings.HasPrefix(ref, remotePrefix) {
		return "refs/heads/" + strings.TrimPrefix(ref, remotePrefix)
	}
	return ref
}
