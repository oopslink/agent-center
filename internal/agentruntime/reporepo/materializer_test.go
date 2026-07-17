package reporepo

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
)

// fakeGit is a recording GitRunner. It simulates just enough git side effects for
// the materializer: `clone` creates <workdir>/<dst>/.git; `remote get-url origin`
// returns a canned URL; `rev-parse --verify --quiet <ref>^{commit}` resolves against
// `refs`; every call is recorded and (optionally) delayed so the per-repo_key mutex can
// be observed. It also injects per-subcommand errors.
//
// NOTE this fake models ref RESOLUTION but not ref FRESHNESS — it cannot express "local
// main is frozen while origin/main moved", which is the actual bug. That is precisely why
// the base-advance regression lock lives in base_advance_test.go against REAL git; these
// fake-git tests only pin the delegation shape (which candidate is asked for, in what
// order). A fake whose rev-parse always agrees with itself would have happily passed the
// pre-fix code.
type fakeGit struct {
	mu    sync.Mutex
	calls [][]string // args of each call, in order
	dirs  []string   // workdir of each call, parallel to calls

	originURL string
	errs      map[string]error
	delay     time.Duration
	// refs maps a fully-spelled ref (as rev-parse would be asked for it, without the
	// ^{commit} peel) to the sha it resolves to. A ref absent here resolves to nothing —
	// rev-parse --verify --quiet's contract: empty output, non-zero exit. nil ⇒ defaultRefs.
	refs map[string]string

	active    int
	maxActive int
}

// defaultRefs is the ref layout of an ordinary freshly-cloned source: main exists both on
// origin and locally, at the same sha (a clone's two views agree — they only diverge later,
// once origin moves and the local branch does not).
// HEAD resolves too, so sourceHealth's probe reads this fake source as healthy (a fake
// whose HEAD did not resolve would drag every test through the quarantine/heal path).
var defaultRefs = map[string]string{
	"refs/remotes/origin/main": "aaaa111122223333444455556666777788889999",
	"main":                     "aaaa111122223333444455556666777788889999",
	"HEAD":                     "aaaa111122223333444455556666777788889999",
}

// resolveRef models `git rev-parse --verify --quiet <arg>`: strip the ^{commit} peel, look
// the ref up, and return ("", exit-1-style error) when it names nothing.
func (f *fakeGit) resolveRef(arg string) (string, error) {
	refs := f.refs
	if refs == nil {
		refs = defaultRefs
	}
	name := strings.TrimSuffix(arg, "^{commit}")
	if sha, ok := refs[name]; ok {
		return sha, nil
	}
	return "", errors.New("fatal: needed a single revision")
}

func (f *fakeGit) Run(ctx context.Context, workdir string, env []string, args ...string) (string, error) {
	f.mu.Lock()
	f.active++
	if f.active > f.maxActive {
		f.maxActive = f.active
	}
	f.calls = append(f.calls, append([]string(nil), args...))
	f.dirs = append(f.dirs, workdir)
	sub := ""
	if len(args) > 0 {
		sub = args[0]
	}
	delay, origin, err := f.delay, f.originURL, f.errs[sub]
	f.mu.Unlock()

	if delay > 0 {
		time.Sleep(delay)
	}

	var out string
	switch {
	case sub == "clone" && len(args) >= 3:
		// git clone <url> <dst>  (workdir = repoDir) → materialize <dst>/.git
		if mkErr := os.MkdirAll(filepath.Join(workdir, args[len(args)-1], ".git"), 0o755); mkErr != nil {
			f.done()
			return "", mkErr
		}
	case sub == "remote":
		out = origin
	case sub == "rev-parse" && len(args) >= 2:
		sha, rerr := f.resolveRef(args[len(args)-1])
		f.done()
		return sha, rerr
	}

	f.done()
	return out, err
}

func (f *fakeGit) done() {
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
}

func (f *fakeGit) countSub(sub string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if len(c) > 0 && c[0] == sub {
			n++
		}
	}
	return n
}

// findCall returns the workdir of the first recorded call whose args have the
// given prefix, and whether it was found.
func (f *fakeGit) findCall(prefix ...string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, c := range f.calls {
		if len(c) < len(prefix) {
			continue
		}
		match := true
		for j, p := range prefix {
			if c[j] != p {
				match = false
				break
			}
		}
		if match {
			return f.dirs[i], true
		}
	}
	return "", false
}

const testURL = "git@github.com:oopslink/agent-center.git"

func newMat(t *testing.T, runner *fakeGit) (*LocalGitMaterializer, string) {
	t.Helper()
	root := t.TempDir()
	clk := clock.NewFakeClock(time.Date(2026, 7, 3, 12, 0, 0, 0, time.UTC))
	m, err := NewLocalGitMaterializer(root, runner, clk)
	if err != nil {
		t.Fatalf("NewLocalGitMaterializer: %v", err)
	}
	return m, root
}

func TestEnsureSource_ClonesThenFetches_Idempotent(t *testing.T) {
	f := &fakeGit{originURL: testURL}
	m, root := newMat(t, f)
	ctx := context.Background()
	target := RepoTarget{RepoID: "repo-1", URL: testURL, Provider: "github", DefaultBranch: "main"}

	src1, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("first EnsureSource: %v", err)
	}
	wantPath := filepath.Join(root, RepoKey(testURL), "source")
	if src1.Path != wantPath {
		t.Fatalf("source path = %q, want %q", src1.Path, wantPath)
	}
	if src1.BaseRef != "main" {
		t.Fatalf("base ref = %q, want main (default_branch fallback)", src1.BaseRef)
	}
	if _, err := os.Stat(filepath.Join(wantPath, ".git")); err != nil {
		t.Fatalf(".git not materialized after clone: %v", err)
	}

	src2, err := m.EnsureSource(ctx, target)
	if err != nil {
		t.Fatalf("second EnsureSource: %v", err)
	}
	if src2.Path != src1.Path {
		t.Fatalf("second call path drifted: %q vs %q", src2.Path, src1.Path)
	}

	if got := f.countSub("clone"); got != 1 {
		t.Fatalf("clone count = %d, want exactly 1 (idempotent)", got)
	}
	if got := f.countSub("fetch"); got != 1 {
		t.Fatalf("fetch count = %d, want 1 (second call fetches)", got)
	}
}

func TestEnsureSource_ConcurrentSameKey_SerializesAndClonesOnce(t *testing.T) {
	f := &fakeGit{originURL: testURL, delay: 5 * time.Millisecond}
	m, _ := newMat(t, f)
	ctx := context.Background()
	target := RepoTarget{RepoID: "repo-1", URL: testURL, DefaultBranch: "main"}

	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = m.EnsureSource(ctx, target)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d EnsureSource: %v", i, err)
		}
	}
	if got := f.countSub("clone"); got != 1 {
		t.Fatalf("clone count = %d, want exactly 1 under concurrency", got)
	}
	if f.maxActive != 1 {
		t.Fatalf("max concurrent git calls for one repo_key = %d, want 1 (mutex not serializing)", f.maxActive)
	}
}

func TestEnsureSource_RemoteMismatch_FailsClosed(t *testing.T) {
	f := &fakeGit{originURL: "git@github.com:someone/other.git"}
	m, root := newMat(t, f)
	ctx := context.Background()

	// Pre-seed an existing source that is a git repo but with a different origin.
	sourcePath := filepath.Join(root, RepoKey(testURL), "source")
	if err := os.MkdirAll(filepath.Join(sourcePath, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := m.EnsureSource(ctx, RepoTarget{URL: testURL, DefaultBranch: "main"})
	if !errors.Is(err, ErrRemoteMismatch) {
		t.Fatalf("err = %v, want ErrRemoteMismatch", err)
	}
	if got := f.countSub("fetch"); got != 0 {
		t.Fatalf("fetch count = %d, want 0 (must not fetch a mismatched remote)", got)
	}
	if got := f.countSub("clone"); got != 0 {
		t.Fatalf("clone count = %d, want 0", got)
	}
}

func TestEnsureSource_SourceExistsNotGitRepo_FailsClosed(t *testing.T) {
	f := &fakeGit{originURL: testURL}
	m, root := newMat(t, f)
	ctx := context.Background()

	// A stray non-git dir sitting where source should be.
	sourcePath := filepath.Join(root, RepoKey(testURL), "source")
	if err := os.MkdirAll(sourcePath, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := m.EnsureSource(ctx, RepoTarget{URL: testURL, DefaultBranch: "main"})
	if !errors.Is(err, ErrSourceNotGitRepo) {
		t.Fatalf("err = %v, want ErrSourceNotGitRepo", err)
	}
	if got := f.countSub("clone"); got != 0 {
		t.Fatalf("clone count = %d, want 0 (must not clone over a stray dir)", got)
	}
}

func TestEnsureSource_EmptyURL(t *testing.T) {
	f := &fakeGit{}
	m, _ := newMat(t, f)
	if _, err := m.EnsureSource(context.Background(), RepoTarget{URL: "  "}); !errors.Is(err, ErrRepoURLRequired) {
		t.Fatalf("err = %v, want ErrRepoURLRequired", err)
	}
}

func TestEnsureSource_WritesMeta(t *testing.T) {
	f := &fakeGit{originURL: testURL}
	m, root := newMat(t, f)
	target := RepoTarget{RepoID: "repo-42", URL: testURL, Provider: "github", DefaultBranch: "main"}

	if _, err := m.EnsureSource(context.Background(), target); err != nil {
		t.Fatalf("EnsureSource: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(root, RepoKey(testURL), "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var meta repoMeta
	if err := json.Unmarshal(b, &meta); err != nil {
		t.Fatalf("unmarshal meta: %v", err)
	}
	if meta.RepoID != "repo-42" || meta.URL != testURL || meta.Provider != "github" || meta.DefaultBranch != "main" {
		t.Fatalf("meta identity wrong: %+v", meta)
	}
	if meta.LastFetchAt != "2026-07-03T12:00:00Z" {
		t.Fatalf("last_fetch_at = %q, want injected clock time", meta.LastFetchAt)
	}
}

func TestEnsureSource_BaseRefPrefersExplicit(t *testing.T) {
	f := &fakeGit{originURL: testURL}
	m, _ := newMat(t, f)
	src, err := m.EnsureSource(context.Background(), RepoTarget{URL: testURL, DefaultBranch: "main", BaseRef: "release/v2"})
	if err != nil {
		t.Fatal(err)
	}
	if src.BaseRef != "release/v2" {
		t.Fatalf("base ref = %q, want explicit release/v2", src.BaseRef)
	}
}

func TestRepoKey_NormalizationStable(t *testing.T) {
	base := RepoKey("git@github.com:oopslink/agent-center")
	for _, v := range []string{
		"git@github.com:oopslink/agent-center.git",
		"git@github.com:oopslink/agent-center/",
		"  git@github.com:oopslink/agent-center.git  ",
	} {
		if got := RepoKey(v); got != base {
			t.Fatalf("RepoKey(%q) = %s, want %s (normalization)", v, got, base)
		}
	}
	if RepoKey("git@github.com:oopslink/other") == base {
		t.Fatal("distinct repos must not collide")
	}
}

func TestPrepareAndRemoveWorktree_DelegatesAndNeverTouchesSource(t *testing.T) {
	f := &fakeGit{originURL: testURL}
	m, _ := newMat(t, f)
	ctx := context.Background()

	src, err := m.EnsureSource(ctx, RepoTarget{URL: testURL, DefaultBranch: "main"})
	if err != nil {
		t.Fatalf("EnsureSource: %v", err)
	}

	wt, err := m.PrepareWorktree(ctx, src, WorktreeRequest{
		ExecutorID:    "exec-1",
		TaskID:        "task-1",
		BranchName:    "ac-exec/task-1/exec-1",
		WorkspacePath: "/tmp/ws/exec-1",
	})
	if err != nil {
		t.Fatalf("PrepareWorktree: %v", err)
	}
	// The base is PINNED to the sha origin/main resolved to — not the name "main".
	wantSHA := defaultRefs["refs/remotes/origin/main"]
	if wt.BaseRef != wantSHA {
		t.Fatalf("worktree base ref = %q, want pinned sha %s", wt.BaseRef, wantSHA)
	}
	// worktree add must be issued from the SOURCE repo dir (delegated to WorktreeProvisioner).
	if dir, ok := f.findCall("worktree", "add", "-b", "ac-exec/task-1/exec-1"); !ok {
		t.Fatal("no `git worktree add -b` delegated call recorded")
	} else if dir != src.Path {
		t.Fatalf("worktree add workdir = %q, want source path %q", dir, src.Path)
	}

	if err := m.RemoveWorktree(ctx, wt); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	// §10 hard rule: remove targets the WORKSPACE, never the source; and the
	// canonical source checkout survives.
	if dir, ok := f.findCall("worktree", "remove", "--force", "/tmp/ws/exec-1"); !ok {
		t.Fatal("no `git worktree remove --force <workspace>` call recorded")
	} else if dir != src.Path {
		t.Fatalf("worktree remove workdir = %q, want source path %q", dir, src.Path)
	}
	for _, c := range f.calls {
		if len(c) >= 2 && c[0] == "worktree" && c[1] == "remove" && c[len(c)-1] == src.Path {
			t.Fatal("RemoveWorktree must NOT remove the canonical source path")
		}
	}
	if _, err := os.Stat(filepath.Join(src.Path, ".git")); err != nil {
		t.Fatalf("canonical source .git must survive worktree removal: %v", err)
	}
}
