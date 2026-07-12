package centergit

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/oopslink/agent-center/internal/cognition/memory"
)

// ErrHostRootEmpty is returned when a Host is constructed without a root dir.
var ErrHostRootEmpty = errors.New("centergit: host root is empty")

// ErrGitOpFailed wraps a failed git plumbing invocation.
var ErrGitOpFailed = errors.New("centergit: git operation failed")

// Host owns the center's bare-repo tree: one bare repo per agent / team plus a
// single global repo, all under root (§4.2 "center 每个 agent/team 建一个 bare
// repo 在自己盘上"). It is the provisioning surface — runtimes never touch these
// dirs directly, they clone/push over the smart-HTTP Handler.
type Host struct {
	root   string
	runner memory.GitRunner
}

// NewHost wires a Host at root. A nil runner defaults to the real git binary.
func NewHost(root string, runner memory.GitRunner) *Host {
	if runner == nil {
		runner = memory.NewExecGitRunner()
	}
	return &Host{root: root, runner: runner}
}

// Root is the absolute directory holding all bare repos (== git-http-backend's
// GIT_PROJECT_ROOT).
func (h *Host) Root() string { return h.root }

// RepoDir returns the absolute on-disk bare-repo directory for ref.
func (h *Host) RepoDir(ref RepoRef) (string, error) {
	if h.root == "" {
		return "", ErrHostRootEmpty
	}
	if err := ref.Validate(); err != nil {
		return "", err
	}
	return filepath.Join(h.root, filepath.FromSlash(ref.dirName())), nil
}

// RepoExists reports whether ref's bare repo has been provisioned (probed via
// the presence of its HEAD file, which git init --bare always writes).
func (h *Host) RepoExists(ref RepoRef) (bool, error) {
	dir, err := h.RepoDir(ref)
	if err != nil {
		return false, err
	}
	if _, statErr := os.Stat(filepath.Join(dir, "HEAD")); statErr != nil {
		if os.IsNotExist(statErr) {
			return false, nil
		}
		return false, statErr
	}
	return true, nil
}

// EnsureRepo idempotently provisions ref's bare repo: git init --bare (initial
// branch main) plus http.receivepack=true so authenticated push works over
// smart-HTTP. Provisioning per-agent / per-team repos and, at instantiation, a
// team's shared repo, all funnel through here (§4.2/§4.3, §9 provisioning).
func (h *Host) EnsureRepo(ctx context.Context, ref RepoRef) error {
	dir, err := h.RepoDir(ref)
	if err != nil {
		return err
	}
	exists, err := h.RepoExists(ref)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if mkErr := os.MkdirAll(filepath.Dir(dir), 0o700); mkErr != nil {
		return fmt.Errorf("%w: mkdir %s: %v", ErrGitOpFailed, filepath.Dir(dir), mkErr)
	}
	env := baseGitEnv("", "", "")
	if out, initErr := h.runner.Run(ctx, h.root, env, "init", "--bare", "--initial-branch=main", dir); initErr != nil {
		return fmt.Errorf("%w: init --bare %s: %v: %s", ErrGitOpFailed, dir, initErr, out)
	}
	// Enable smart-HTTP push; upload-pack (read) is on by default but set it
	// explicitly for clarity.
	for _, kv := range [][2]string{
		{"http.receivepack", "true"},
		{"http.uploadpack", "true"},
	} {
		if out, cfgErr := h.runner.Run(ctx, dir, env, "config", kv[0], kv[1]); cfgErr != nil {
			return fmt.Errorf("%w: config %s: %v: %s", ErrGitOpFailed, kv[0], cfgErr, out)
		}
	}
	return nil
}
