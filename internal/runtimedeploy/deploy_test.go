package runtimedeploy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyRemoteRequiresExactTargetSHAAndAncestor(t *testing.T) {
	remote, mainSHA, featureSHA, _ := seedDeployRemote(t)
	got, err := verifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/feature", TargetSHA: featureSHA, BaseRef: "refs/heads/main",
	}, true)
	if err != nil {
		t.Fatalf("VerifyRemote: %v", err)
	}
	if got.TargetSHA != featureSHA || got.BaseSHA != mainSHA {
		t.Fatalf("verified refs = %+v, want target=%s base=%s", got, featureSHA, mainSHA)
	}

	_, err = verifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/feature", TargetSHA: mainSHA, BaseRef: "refs/heads/main",
	}, true)
	if err == nil || !strings.Contains(err.Error(), "not requested target_sha") {
		t.Fatalf("exact SHA mismatch should fail closed, got %v", err)
	}
}

func TestVerifyRemoteRejectsNonAncestor(t *testing.T) {
	remote, mainSHA, _, orphanSHA := seedDeployRemote(t)
	_, err := verifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/orphan", TargetSHA: orphanSHA, BaseRef: "refs/heads/main",
	}, true)
	if err == nil || !strings.Contains(err.Error(), "is not an ancestor") {
		t.Fatalf("non-ancestor should fail closed, got %v (main=%s orphan=%s)", err, mainSHA, orphanSHA)
	}
}

func TestVerifyRemoteRequiresCanonicalHTTPSRemote(t *testing.T) {
	_, err := VerifyRemote(context.Background(), Request{
		RepoURL: "/tmp/repo.git", TargetRef: "refs/heads/main", TargetSHA: strings.Repeat("a", 40), BaseRef: "refs/heads/main",
	})
	if err == nil || !strings.Contains(err.Error(), "canonical HTTPS") {
		t.Fatalf("non-HTTPS remote should fail closed, got %v", err)
	}
}

func seedDeployRemote(t *testing.T) (remoteURL, mainSHA, featureSHA, orphanSHA string) {
	t.Helper()
	dir := t.TempDir()
	work := filepath.Join(dir, "work")
	runGitForTest(t, dir, "init", "-b", "main", work)
	runGitForTest(t, work, "config", "user.email", "test@example.invalid")
	runGitForTest(t, work, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, work, "add", "README.md")
	runGitForTest(t, work, "commit", "-m", "main")
	mainSHA = gitOut(t, work, "rev-parse", "HEAD")
	runGitForTest(t, work, "checkout", "-b", "feature")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, work, "add", "feature.txt")
	runGitForTest(t, work, "commit", "-m", "feature")
	featureSHA = gitOut(t, work, "rev-parse", "HEAD")
	runGitForTest(t, work, "checkout", "--orphan", "orphan")
	runGitForTest(t, work, "rm", "-rf", ".")
	if err := os.WriteFile(filepath.Join(work, "orphan.txt"), []byte("orphan\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGitForTest(t, work, "add", "orphan.txt")
	runGitForTest(t, work, "commit", "-m", "orphan")
	orphanSHA = gitOut(t, work, "rev-parse", "HEAD")
	remote := filepath.Join(dir, "remote.git")
	runGitForTest(t, dir, "clone", "--bare", work, remote)
	return remote, mainSHA, featureSHA, orphanSHA
}

func runGitForTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git -C %s %s: %v\n%s", dir, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}
