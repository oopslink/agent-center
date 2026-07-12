package centergit

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// testEnv is a hermetic git env for the test client, isolated from the dev
// machine's ~/.gitconfig. HOME points at a per-test dir so nothing leaks.
func testEnv(home string) []string {
	return append(baseGitEnv(home, "Tester", "tester@agent-center.local"),
		"GIT_AUTHOR_DATE=2026-07-12T00:00:00+00:00",
		"GIT_COMMITTER_DATE=2026-07-12T00:00:00+00:00",
	)
}

// runGit runs the git client in dir and fails the test on error.
func runGit(t *testing.T, home, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = testEnv(home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s failed: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// tryGit runs the git client and returns output + error without failing.
func tryGit(home, dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = testEnv(home)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// seedBare gives an empty bare repo an initial commit on main so multiple clones
// share a base history (needed to exercise the non-fast-forward retry path).
func seedBare(t *testing.T, home, bareDir string) {
	t.Helper()
	tmp := t.TempDir()
	runGit(t, home, tmp, "clone", bareDir, "wc")
	wc := filepath.Join(tmp, "wc")
	if err := os.WriteFile(filepath.Join(wc, "README.md"), []byte("# team memory\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runGit(t, home, wc, "add", "-A")
	runGit(t, home, wc, "-c", "commit.gpgsign=false", "commit", "-m", "seed")
	runGit(t, home, wc, "push", "origin", "HEAD:main")
}

// mustDeterministicIDs returns an id generator yielding a fixed sequence, so
// entry file names and index output are stable in tests.
func mustDeterministicIDs(ids ...string) func() string {
	i := 0
	return func() string {
		if i >= len(ids) {
			return "overflow"
		}
		id := ids[i]
		i++
		return id
	}
}
