package workerdaemon

import (
	"context"
	"testing"
)

// TestExecGitRunner exercises the real `git --help`-style invocation so
// the production runner doesn't sit at 0% coverage.
func TestExecGitRunner_HelpRuns(t *testing.T) {
	runner := ExecGitRunner{}
	// `git --version` works in any directory; the runner's contract is to
	// run with cwd=dir + the args, so this stays portable.
	dir := t.TempDir()
	out, err := runner.RunInDir(context.Background(), dir, "--version")
	if err != nil {
		t.Skipf("git not available: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("expected version output")
	}
}
