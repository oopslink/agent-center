package runtimedeploy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyRemoteAcceptsExactSHAAndAncestor(t *testing.T) {
	remote, mainSHA, featureSHA, _ := seedDeployRemote(t)
	got, err := VerifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/feature", ExactSHA: featureSHA, BaseRef: "refs/heads/main",
	})
	if err != nil {
		t.Fatalf("VerifyRemote: %v", err)
	}
	if got.TargetSHA != featureSHA || got.BaseSHA != mainSHA {
		t.Fatalf("verified refs = %+v, want target=%s base=%s", got, featureSHA, mainSHA)
	}
}

func TestVerifyRemoteRejectsMissingExactSHA(t *testing.T) {
	remote, _, _, _ := seedDeployRemote(t)
	_, err := VerifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/feature", BaseRef: "refs/heads/main",
	})
	if err == nil || !strings.Contains(err.Error(), "exact_sha required") {
		t.Fatalf("missing exact_sha should fail closed, got %v", err)
	}
}

func TestVerifyRemoteRejectsShortPrefixExactSHA(t *testing.T) {
	remote, _, featureSHA, _ := seedDeployRemote(t)
	_, err := VerifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/feature", ExactSHA: featureSHA[:12], BaseRef: "refs/heads/main",
	})
	if err == nil || !strings.Contains(err.Error(), "exact_sha must be exactly 40 hexadecimal characters") {
		t.Fatalf("short exact_sha should fail closed, got %v", err)
	}
}

func TestVerifyRemoteRejectsMalformedExactSHA(t *testing.T) {
	remote, _, _, _ := seedDeployRemote(t)
	_, err := VerifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/feature", ExactSHA: "zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz", BaseRef: "refs/heads/main",
	})
	if err == nil || !strings.Contains(err.Error(), "exact_sha must be exactly 40 hexadecimal characters") {
		t.Fatalf("malformed exact_sha should fail closed, got %v", err)
	}
}

func TestVerifyRemoteRejectsExactSHAMismatch(t *testing.T) {
	remote, mainSHA, _, _ := seedDeployRemote(t)
	_, err := VerifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/feature", ExactSHA: mainSHA, BaseRef: "refs/heads/main",
	})
	if err == nil || !strings.Contains(err.Error(), "not requested exact_sha") {
		t.Fatalf("exact SHA mismatch should fail closed, got %v", err)
	}
}

func TestVerifyRemoteRejectsMissingTargetRef(t *testing.T) {
	remote, _, featureSHA, _ := seedDeployRemote(t)
	_, err := VerifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/missing", ExactSHA: featureSHA, BaseRef: "refs/heads/main",
	})
	if err == nil || !strings.Contains(err.Error(), "git ls-remote target_ref") {
		t.Fatalf("missing target ref should fail closed, got %v", err)
	}
}

func TestVerifyRemoteRejectsNonAncestor(t *testing.T) {
	remote, mainSHA, _, orphanSHA := seedDeployRemote(t)
	_, err := VerifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/orphan", ExactSHA: orphanSHA, BaseRef: "refs/heads/main",
	})
	if err == nil || !strings.Contains(err.Error(), "is not an ancestor") {
		t.Fatalf("non-ancestor should fail closed, got %v (main=%s orphan=%s)", err, mainSHA, orphanSHA)
	}
}

func TestVerifyRemoteIgnoresCallerVerificationFields(t *testing.T) {
	remote, _, featureSHA, _ := seedDeployRemote(t)
	_, err := VerifyRemote(context.Background(), Request{
		RepoURL: remote, TargetRef: "refs/heads/feature", TargetSHA: featureSHA, BaseRef: "refs/heads/main",
		VerifiedTargetSHA: featureSHA, VerifiedBaseSHA: featureSHA, VerifiedAt: "2026-08-31T00:00:00Z",
	})
	if err == nil || !strings.Contains(err.Error(), "exact_sha required") {
		t.Fatalf("caller verification fields must be ignored, got %v", err)
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
