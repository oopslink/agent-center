package workerdaemon

import "testing"

func TestGitWorktreeFlagEnabled(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		t.Setenv("AC_EXECUTOR_GIT_WORKTREE", "")
		if gitWorktreeFlagEnabled() {
			t.Fatal("empty AC_EXECUTOR_GIT_WORKTREE must be OFF")
		}
	})
	t.Run("on values", func(t *testing.T) {
		for _, v := range []string{"1", "true", "TRUE", "yes"} {
			t.Setenv("AC_EXECUTOR_GIT_WORKTREE", v)
			if !gitWorktreeFlagEnabled() {
				t.Fatalf("AC_EXECUTOR_GIT_WORKTREE=%q must be ON", v)
			}
		}
	})
	t.Run("off values", func(t *testing.T) {
		for _, v := range []string{"0", "false", "no", "anything-else"} {
			t.Setenv("AC_EXECUTOR_GIT_WORKTREE", v)
			if gitWorktreeFlagEnabled() {
				t.Fatalf("AC_EXECUTOR_GIT_WORKTREE=%q must be OFF", v)
			}
		}
	})
}
