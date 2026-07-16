package reporepo

// source_heal_test.go — issue-13e7bfe8 layer 2, proven with REAL git (real clone, real
// corruption, real heal). The fake-git suite in materializer_test.go cannot cover any of
// this: its `clone` is an os.MkdirAll, so it can neither be interrupted nor leave a
// half-written .git — which is exactly why the pre-fix code shipped with this hole.

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/clock"
)

// realGitMat builds a materializer over an EMPTY repos root with the REAL git runner.
func realGitMat(t *testing.T) (*LocalGitMaterializer, string) {
	t.Helper()
	root := t.TempDir()
	m, err := NewLocalGitMaterializer(root, executor.NewExecGitRunner(), clock.SystemClock{})
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	return m, root
}

// makeRealRemote creates a real git repo with one commit on main, usable as a clone URL.
func makeRealRemote(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, dir, "add", "-A")
	runGit(t, dir, "commit", "-q", "-m", "init")
	return dir
}

// headOK reports whether repoPath has a resolvable HEAD (i.e. is actually usable).
func headOK(t *testing.T, repoPath string) bool {
	t.Helper()
	_, err := executor.NewExecGitRunner().Run(context.Background(), repoPath, os.Environ(), "rev-parse", "--verify", "HEAD")
	return err == nil
}

// TestEnsureSource_KilledCloneLeavesNoCanonicalSource pins the ATOMIC-PUBLISH invariant
// against the mechanism that actually caused the incident: a clone killed part-way.
//
// It uses a runner that emulates a SIGKILLed git precisely — some of .git already
// written, then a non-zero exit — because that is the one failure real git does NOT clean
// up after itself. (A gracefully failing `git clone` removes its own target dir, which is
// why an unreachable-remote test does NOT discriminate here; the real-git tests below
// cover that path for its own sake.) When the control ctx died at 5s, exec.CommandContext
// SIGKILLed git exactly like this and the partial tree stayed at the canonical path
// forever.
//
// This is a unit test of OUR publish rule — "nothing reaches `source` unless git exited
// 0" — not of git. It fails on the pre-fix code, which cloned straight into `source`.
func TestEnsureSource_KilledCloneLeavesNoCanonicalSource(t *testing.T) {
	killed := &killedCloneGit{}
	root := t.TempDir()
	m, err := NewLocalGitMaterializer(root, killed, clock.SystemClock{})
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	url := "git@example.com:o/r.git"

	if _, err := m.EnsureSource(context.Background(), RepoTarget{URL: url, DefaultBranch: "main"}); err == nil {
		t.Fatalf("a killed clone must surface an error, got nil")
	}
	if !killed.wrotePartial {
		t.Fatalf("precondition: the runner should have written a partial .git")
	}

	repoDir := filepath.Join(root, RepoKey(url))
	sourcePath := filepath.Join(repoDir, "source")
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("a KILLED clone published a partial tree at the canonical path (stat err = %v) — this is the exact prod poisoning: the next EnsureSource sees .git, reuses it, and never heals", err)
	}
	assertNoDebris(t, repoDir)
}

// killedCloneGit emulates git being SIGKILLed mid-clone: it writes a partial .git into
// the destination (as a real interrupted clone leaves behind) and then fails.
type killedCloneGit struct{ wrotePartial bool }

func (k *killedCloneGit) Run(ctx context.Context, workdir string, env []string, args ...string) (string, error) {
	if len(args) >= 3 && args[0] == "clone" {
		dst := filepath.Join(workdir, args[len(args)-1])
		if err := os.MkdirAll(filepath.Join(dst, ".git", "objects"), 0o755); err != nil {
			return "", err
		}
		// A half-written .git: present, but with no refs and no resolvable HEAD.
		if err := os.WriteFile(filepath.Join(dst, ".git", "config"), []byte("[core]\n"), 0o644); err != nil {
			return "", err
		}
		k.wrotePartial = true
		return "", errors.New("signal: killed")
	}
	return "", nil
}

// TestEnsureSource_RealGit_FailedCloneLeavesNoCanonicalSource guards the gracefully-failing
// clone path (unreachable remote) with REAL git: nothing may be published at `source`.
func TestEnsureSource_RealGit_FailedCloneLeavesNoCanonicalSource(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	m, root := realGitMat(t)
	ctx := context.Background()
	missing := filepath.Join(t.TempDir(), "definitely-not-a-repo")

	if _, err := m.EnsureSource(ctx, RepoTarget{URL: missing, DefaultBranch: "main"}); err == nil {
		t.Fatalf("EnsureSource against a non-existent remote must fail, got nil")
	}

	sourcePath := filepath.Join(root, RepoKey(missing), "source")
	if _, err := os.Stat(sourcePath); !os.IsNotExist(err) {
		t.Fatalf("a FAILED clone must leave nothing at the canonical path (stat err = %v); debris here is what poisons every later EnsureSource", err)
	}
	// No staging debris accumulates either.
	assertNoDebris(t, filepath.Join(root, RepoKey(missing)))
}

// TestEnsureSource_RealGit_RetryAfterFailedCloneSelfHeals proves the retry story the
// incident lacked: the first attempt fails, the second (same repo_key, now-reachable
// remote) succeeds. Pre-fix, the first failure's debris made every retry fail forever.
func TestEnsureSource_RealGit_RetryAfterFailedCloneSelfHeals(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	m, root := realGitMat(t)
	ctx := context.Background()

	// A remote path that does not exist YET — the clone fails.
	remote := filepath.Join(t.TempDir(), "later")
	target := RepoTarget{URL: remote, DefaultBranch: "main"}
	if _, err := m.EnsureSource(ctx, target); err == nil {
		t.Fatalf("first EnsureSource must fail (remote absent)")
	}

	// The remote appears (operator fixed it / network came back) → retry must self-heal.
	if err := os.MkdirAll(remote, 0o755); err != nil {
		t.Fatalf("mkdir remote: %v", err)
	}
	runGit(t, remote, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	runGit(t, remote, "add", "-A")
	runGit(t, remote, "commit", "-q", "-m", "init")

	src, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("retry after a failed clone must self-heal, got: %v", err)
	}
	if !headOK(t, src.Path) {
		t.Fatalf("healed source %s has no resolvable HEAD", src.Path)
	}
	assertNoDebris(t, filepath.Join(root, RepoKey(remote)))
}

// TestEnsureSource_RealGit_BrokenSourceIsQuarantinedAndRecloned is the direct regression
// test for the FIELD state this incident left behind: a `source` that has a .git but is
// unusable ("fatal: your current branch appears to be broken").
//
// It reproduces that state on a real repo and asserts EnsureSource heals it. This is not
// optional cleanup — repos poisoned by the pre-fix code are already on disk on real
// workers, and without this they would stay wedged forever: the old reuse branch keyed
// only on os.Stat(.git), and `remote get-url` + `fetch` BOTH succeed on such a corpse, so
// EnsureSource kept reporting success while every worktree add failed.
func TestEnsureSource_RealGit_BrokenSourceIsQuarantinedAndRecloned(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	m, root := realGitMat(t)
	ctx := context.Background()
	remote := makeRealRemote(t)
	target := RepoTarget{URL: remote, DefaultBranch: "main"}

	src, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("initial EnsureSource: %v", err)
	}

	// Poison it exactly as an interrupted clone does: .git survives, refs do not.
	if err := os.RemoveAll(filepath.Join(src.Path, ".git", "refs", "heads")); err != nil {
		t.Fatalf("break refs: %v", err)
	}
	_ = os.Remove(filepath.Join(src.Path, ".git", "packed-refs"))
	if headOK(t, src.Path) {
		t.Fatalf("precondition failed: the source should now be BROKEN (HEAD unresolvable)")
	}

	// The heal: EnsureSource must notice and re-clone rather than report false success.
	healed, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("EnsureSource must heal a broken source, got: %v", err)
	}
	if !headOK(t, healed.Path) {
		t.Fatalf("EnsureSource returned success for a source with no resolvable HEAD — the pre-fix false-success bug is back")
	}
	if healed.Path != src.Path {
		t.Fatalf("healed source path drifted: %q vs %q", healed.Path, src.Path)
	}
	// The corpse is moved aside, not left at the canonical path, and swept afterwards.
	assertNoDebris(t, filepath.Join(root, RepoKey(remote)))
}

// TestEnsureSource_TransientProbeFailureNeverDestroysHealthySource is the guard against
// this fix's own worst failure mode.
//
// The heal deletes the canonical source, so the "is it broken?" probe must be DECISIVE. If
// a probe error that merely means "git could not run" (fork/EAGAIN under load, a missing
// binary, a cancelled ctx — note a ctx-killed git surfaces as an ExitError too) were read
// as "broken", every transient hiccup would destroy a perfectly healthy repo: corruption-
// on-cancel traded for data-loss-on-cancel. A non-verdict must leave the source ALONE.
func TestEnsureSource_TransientProbeFailureNeverDestroysHealthySource(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	root := t.TempDir()
	flaky := &probeFailGit{real: executor.NewExecGitRunner()}
	m, err := NewLocalGitMaterializer(root, flaky, clock.SystemClock{})
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	ctx := context.Background()
	remote := makeRealRemote(t)
	target := RepoTarget{URL: remote, DefaultBranch: "main"}

	src, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("initial EnsureSource: %v", err)
	}
	headSHA := headRev(t, src.Path)

	// From now on the HEAD probe fails in a way that is NOT a repo verdict.
	flaky.failProbe = true
	if _, err := m.EnsureSource(ctx, target); err != nil {
		t.Fatalf("a non-verdict probe error must not fail EnsureSource: %v", err)
	}

	if !headOK(t, src.Path) || headRev(t, src.Path) != headSHA {
		t.Fatalf("a healthy canonical source was DESTROYED by a transient probe error — the heal must only act on a decisive git verdict")
	}
	if flaky.cloned > 1 {
		t.Fatalf("re-cloned %d times: a transient probe error must not trigger a heal", flaky.cloned)
	}
}

// probeFailGit delegates to real git but can make the HEAD probe fail with a non-ExitError
// (i.e. "git could not be executed"), the shape of a transient infrastructure failure.
type probeFailGit struct {
	real      executor.GitRunner
	failProbe bool
	cloned    int
}

func (p *probeFailGit) Run(ctx context.Context, workdir string, env []string, args ...string) (string, error) {
	if len(args) > 0 && args[0] == "clone" {
		p.cloned++
	}
	if p.failProbe && len(args) >= 2 && args[0] == "rev-parse" {
		return "", errors.New("fork/exec /usr/bin/git: resource temporarily unavailable")
	}
	return p.real.Run(ctx, workdir, env, args...)
}

// headRev returns the resolved HEAD sha of a repo.
func headRev(t *testing.T, repoPath string) string {
	t.Helper()
	out, err := executor.NewExecGitRunner().Run(context.Background(), repoPath, os.Environ(), "rev-parse", "HEAD")
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return strings.TrimSpace(out)
}

// TestEnsureSource_RealGit_HealIsLoud pins that a self-heal is not silent: a poisoned
// canonical source is an incident artifact and its removal must be greppable.
func TestEnsureSource_RealGit_HealIsLoud(t *testing.T) {
	if !gitAvailable() {
		t.Skip("git not available")
	}
	m, _ := realGitMat(t)
	var logs []string
	m.Log = func(s string) { logs = append(logs, s) }
	ctx := context.Background()
	remote := makeRealRemote(t)
	target := RepoTarget{URL: remote, DefaultBranch: "main"}

	src, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("initial EnsureSource: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(src.Path, ".git", "refs", "heads")); err != nil {
		t.Fatalf("break refs: %v", err)
	}
	_ = os.Remove(filepath.Join(src.Path, ".git", "packed-refs"))

	if _, err := m.EnsureSource(ctx, target); err != nil {
		t.Fatalf("heal: %v", err)
	}
	for _, l := range logs {
		if strings.Contains(l, "BROKEN") {
			return
		}
	}
	t.Fatalf("a self-heal must log loudly (want a BROKEN line), got %v", logs)
}

// assertNoDebris fails when transient staging/quarantine dirs survive under repoDir.
func assertNoDebris(t *testing.T, repoDir string) {
	t.Helper()
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return // repoDir may legitimately not exist (nothing was ever published)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), stagingPrefix) || strings.HasPrefix(e.Name(), quarantinePrefix) {
			t.Fatalf("transient dir %q survived under %s — debris must not accumulate", e.Name(), repoDir)
		}
	}
}
