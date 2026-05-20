package workerdaemon

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

type fakeGit struct {
	calls   [][]string
	wantErr error
}

func (f *fakeGit) RunInDir(_ context.Context, dir string, args ...string) ([]byte, error) {
	f.calls = append(f.calls, append([]string{dir}, args...))
	if f.wantErr != nil {
		return nil, f.wantErr
	}
	return nil, nil
}

func TestWorkspace_Direct(t *testing.T) {
	m := NewWorkspaceManager(&fakeGit{})
	got, err := m.Prepare(context.Background(), PrepareInput{
		BasePath:      "/repo",
		WorkspaceMode: execution.WorkspaceDirect,
		ExecutionID:   "E-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.CWD != "/repo" {
		t.Fatalf("cwd: %s", got.CWD)
	}
	if got.BranchName != "" {
		t.Fatalf("branch: %s", got.BranchName)
	}
}

func TestWorkspace_Worktree(t *testing.T) {
	git := &fakeGit{}
	m := NewWorkspaceManager(git)
	got, err := m.Prepare(context.Background(), PrepareInput{
		BasePath: "/repo", WorkspaceMode: execution.WorkspaceWorktree,
		ExecutionID: "E-1", BaseBranch: "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.BranchName != "task/E-1" {
		t.Fatalf("branch: %s", got.BranchName)
	}
	if !strings.Contains(got.CWD, "task-E-1") {
		t.Fatalf("cwd: %s", got.CWD)
	}
	if len(git.calls) != 1 {
		t.Fatalf("calls: %d", len(git.calls))
	}
	if git.calls[0][1] != "worktree" {
		t.Fatalf("args: %+v", git.calls[0])
	}
}

func TestWorkspace_Worktree_Failure(t *testing.T) {
	git := &fakeGit{wantErr: errors.New("git fail")}
	m := NewWorkspaceManager(git)
	if _, err := m.Prepare(context.Background(), PrepareInput{
		BasePath: "/repo", WorkspaceMode: execution.WorkspaceWorktree,
		ExecutionID: "E-1", BaseBranch: "main",
	}); err == nil {
		t.Fatal("expected error")
	}
}

func TestWorkspace_Release(t *testing.T) {
	git := &fakeGit{}
	m := NewWorkspaceManager(git)
	if err := m.Release(context.Background(), "/repo", execution.WorkspaceWorktree, "E-1"); err != nil {
		t.Fatal(err)
	}
	if len(git.calls) != 1 {
		t.Fatalf("calls: %d", len(git.calls))
	}
	// direct mode = no-op
	if err := m.Release(context.Background(), "/repo", execution.WorkspaceDirect, "E-1"); err != nil {
		t.Fatal(err)
	}
}

func TestWorkspace_ValidationErrors(t *testing.T) {
	m := NewWorkspaceManager(&fakeGit{})
	if _, err := m.Prepare(context.Background(), PrepareInput{}); err == nil {
		t.Fatal("expected base_path")
	}
	if _, err := m.Prepare(context.Background(), PrepareInput{BasePath: "/x", WorkspaceMode: "BAD"}); err == nil {
		t.Fatal("expected mode")
	}
	if _, err := m.Prepare(context.Background(), PrepareInput{BasePath: "/x", WorkspaceMode: execution.WorkspaceWorktree}); err == nil {
		t.Fatal("expected execution_id")
	}
}
