package workerdaemon

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

// GitRunner abstracts `git` invocations for testability.
type GitRunner interface {
	RunInDir(ctx context.Context, dir string, args ...string) ([]byte, error)
}

// ExecGitRunner is the production implementation backed by os/exec.
type ExecGitRunner struct{}

// RunInDir invokes `git` with the given args, captured in dir.
func (ExecGitRunner) RunInDir(ctx context.Context, dir string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

// WorkspaceManager creates / releases worker-side workspaces (worktree or
// direct mode per ADR-0018 + 02-task-execution § 8).
type WorkspaceManager struct {
	git GitRunner
}

// NewWorkspaceManager returns a manager.
func NewWorkspaceManager(git GitRunner) *WorkspaceManager {
	if git == nil {
		git = ExecGitRunner{}
	}
	return &WorkspaceManager{git: git}
}

// PrepareInput captures the parameters for workspace prep.
type PrepareInput struct {
	BasePath      string                  // worker's mapping base path
	WorkspaceMode execution.WorkspaceMode
	ExecutionID   string
	BaseBranch    string // default main
}

// PrepareResult describes the prepared workspace.
type PrepareResult struct {
	CWD        string // path the agent should `cd` into
	BranchName string // worktree branch (empty for direct mode)
}

// Prepare creates the per-execution workspace. worktree mode runs
// `git worktree add -b task/<execution_id> <wt-path> <base_branch>`.
// direct mode no-ops (cwd = base_path).
func (m *WorkspaceManager) Prepare(ctx context.Context, in PrepareInput) (PrepareResult, error) {
	if in.BasePath == "" {
		return PrepareResult{}, errors.New("workspace: base_path required")
	}
	if !in.WorkspaceMode.IsValid() {
		return PrepareResult{}, fmt.Errorf("workspace: invalid mode %q", in.WorkspaceMode)
	}
	if in.WorkspaceMode == execution.WorkspaceDirect {
		return PrepareResult{CWD: in.BasePath}, nil
	}
	if in.ExecutionID == "" {
		return PrepareResult{}, errors.New("workspace: execution_id required for worktree mode")
	}
	branch := "task/" + in.ExecutionID
	wtPath := filepath.Join(in.BasePath+".wt", "task-"+in.ExecutionID)
	args := []string{"worktree", "add", "-b", branch, wtPath}
	if in.BaseBranch != "" {
		args = append(args, in.BaseBranch)
	}
	if _, err := m.git.RunInDir(ctx, in.BasePath, args...); err != nil {
		return PrepareResult{}, fmt.Errorf("workspace: git worktree add: %w", err)
	}
	return PrepareResult{CWD: wtPath, BranchName: branch}, nil
}

// Release removes a worktree (worktree mode only). direct mode no-ops.
func (m *WorkspaceManager) Release(ctx context.Context, basePath string, mode execution.WorkspaceMode, executionID string) error {
	if mode != execution.WorkspaceWorktree {
		return nil
	}
	wtPath := filepath.Join(basePath+".wt", "task-"+executionID)
	if _, err := m.git.RunInDir(ctx, basePath, "worktree", "remove", "--force", wtPath); err != nil {
		return fmt.Errorf("workspace: git worktree remove: %w", err)
	}
	return nil
}
