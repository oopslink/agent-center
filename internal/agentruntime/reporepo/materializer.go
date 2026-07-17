// Package reporepo materializes canonical per-repo source checkouts on the worker
// host and derives per-executor git worktrees from them (agent-runtime repo
// workspaces design §4/§8). It is the replaceable RepoMaterializer port the agent
// runtime uses so that, later, source materialization can move to a bare mirror,
// a sidecar, or a remote artifact service without the runtime control-plane
// re-learning git.
//
// Layout (design §4), under an agent home:
//
//	repos/<repo_key>/source/   canonical non-bare checkout (worktrees branch off it)
//	repos/<repo_key>/meta.json repo identity + last_fetch_at
//
// repo_key is sha256(normalized repo URL) so the same remote always maps to one
// canonical source dir. The lower git plumbing is REUSED, not rebuilt: network
// clone/fetch go through executor.GitRunner, and every worktree add/remove/prune
// delegates to executor.WorktreeProvisioner.
//
// Hard rule (design §10): the canonical repos/<repo_key>/source is NEVER removed
// by executor cleanup — RemoveWorktree only ever tears down the per-executor
// worktree, never the source.
package reporepo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/clock"
)

// Sentinel errors (wrapped with repo_key context; match with errors.Is). None of
// them carry the repo URL or git output, so a credential embedded in a URL can
// never leak into a log line (design §8: errors print only ids / repo_key).
var (
	// ErrRepoURLRequired is returned when a RepoTarget has no URL.
	ErrRepoURLRequired = errors.New("reporepo: repo url required")
	// ErrRemoteMismatch is returned (fail-closed) when an existing source's origin
	// URL does not match the target URL — the repo_key dir was seeded from a
	// different remote and MUST NOT be reused.
	ErrRemoteMismatch = errors.New("reporepo: source origin url does not match target (fail-closed)")
	// ErrSourceNotGitRepo is returned (fail-closed) when the source path exists but
	// is not a git repository, so cloning into / reusing a stray dir is refused.
	ErrSourceNotGitRepo = errors.New("reporepo: source path exists but is not a git repo (fail-closed)")
	// ErrBaseRefUnresolved is returned (fail-closed) when the base ref names nothing
	// resolvable in the canonical source — neither on origin nor locally. Branching off a
	// silently-wrong base is worse than not spawning, so PrepareWorktree refuses.
	ErrBaseRefUnresolved = errors.New("reporepo: base ref does not resolve in source (fail-closed)")
)

// RepoTarget identifies the repository to materialize (design §8).
type RepoTarget struct {
	RepoID        string
	URL           string
	Provider      string
	DefaultBranch string
	// BaseRef is the task-resolved base ref; empty falls back to DefaultBranch.
	BaseRef string
}

// resolvedBaseRef is the effective base ref: explicit BaseRef wins, else DefaultBranch.
func (t RepoTarget) resolvedBaseRef() string {
	if s := strings.TrimSpace(t.BaseRef); s != "" {
		return s
	}
	return strings.TrimSpace(t.DefaultBranch)
}

// SourceRepo is a materialized canonical source checkout (design §4).
type SourceRepo struct {
	RepoKey string
	Path    string // <reposRoot>/<repo_key>/source
	URL     string
	BaseRef string // resolved base ref (RepoTarget.resolvedBaseRef)
}

// WorktreeRequest describes a per-executor worktree to derive from a SourceRepo
// (design §8).
type WorktreeRequest struct {
	ExecutorID    string
	TaskID        string
	BranchName    string // unique executor branch, e.g. ac-exec/<task_id>/<executor_id>
	WorkspacePath string // the executor's isolated workspace (the worktree path)
	BaseRef       string // optional override; empty ⇒ SourceRepo.BaseRef
}

// PreparedWorktree is a provisioned executor worktree + the durable record needed
// to tear it down later (design §10).
type PreparedWorktree struct {
	ExecutorID    string
	RepoKey       string
	SourcePath    string
	WorkspacePath string
	Branch        string
	// BaseRef is the concrete commit SHA the branch was actually cut from (resolved from
	// origin at cut time — never a branch name). Callers MUST carry this, not the requested
	// ref name, into the executor Record: it is what makes ahead-of-base count only this
	// executor's own commits. See resolveBaseCommit.
	BaseRef string
}

// RepoMaterializer is the replaceable port the runtime uses to materialize repo
// sources and derive per-executor worktrees (design §8).
type RepoMaterializer interface {
	EnsureSource(ctx context.Context, target RepoTarget) (SourceRepo, error)
	PrepareWorktree(ctx context.Context, source SourceRepo, req WorktreeRequest) (PreparedWorktree, error)
	RemoveWorktree(ctx context.Context, wt PreparedWorktree) error
	// PruneOrphanWorktrees reaps per-executor worktrees under ONE source whose owning
	// executor is no longer live (v2.31.1 orphan-reap). isLive reports whether an
	// executor id is still running/adopted; a nil isLive treats every worktree as live
	// (reaps nothing). Never removes the canonical source. Returns the count reaped.
	PruneOrphanWorktrees(ctx context.Context, source SourceRepo, isLive func(execID string) bool) (int, error)
	// ReapOrphanWorktrees runs PruneOrphanWorktrees across EVERY materialized source
	// under the repos root (the boot-reconcile sweep). Best-effort per source.
	ReapOrphanWorktrees(ctx context.Context, isLive func(execID string) bool) (int, error)
}

// Transient sibling-dir prefixes under repos/<repo_key>/ (issue-13e7bfe8 layer 2).
// Both are dot-prefixed so they can never collide with `source` / `meta.json`, and
// both live under repoDir so a promote is a same-filesystem os.Rename.
const (
	// stagingPrefix is the in-progress clone dir; renamed onto `source` on success,
	// removed on every failure.
	stagingPrefix = ".staging-"
	// stagingCheckout is the checkout dir name git clones into inside the staging dir.
	stagingCheckout = "co"
	// quarantinePrefix is where a broken `source` is moved aside before a heal re-clone.
	quarantinePrefix = ".broken-"
)

// LocalGitMaterializer is the single-machine v1 RepoMaterializer: a non-bare
// `source` checkout per repo_key, per-executor worktrees added off it, all git
// state on the local worker host.
type LocalGitMaterializer struct {
	// Log is an OPTIONAL operational sink. Set post-construction by the wiring so a
	// self-heal (quarantine + re-clone of a poisoned source) is visible in the worker
	// log instead of being silently swallowed. nil ⇒ silent.
	Log func(string)

	reposRoot string
	runner    executor.GitRunner
	clock     clock.Clock

	mu    sync.Mutex
	locks map[string]*sync.Mutex // per repo_key serialization (design §8)
}

var _ RepoMaterializer = (*LocalGitMaterializer)(nil)

// NewLocalGitMaterializer builds a materializer rooted at reposRoot (the
// `<agent_home>/repos` directory). A nil runner defaults to the real git binary;
// a nil clock defaults to the system clock.
func NewLocalGitMaterializer(reposRoot string, runner executor.GitRunner, clk clock.Clock) (*LocalGitMaterializer, error) {
	if strings.TrimSpace(reposRoot) == "" {
		return nil, errors.New("reporepo: repos_root required")
	}
	if runner == nil {
		runner = executor.NewExecGitRunner()
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	return &LocalGitMaterializer{
		reposRoot: reposRoot,
		runner:    runner,
		clock:     clk,
		locks:     map[string]*sync.Mutex{},
	}, nil
}

// EnsureSource idempotently materializes the canonical source for target: first
// call clones, subsequent calls verify the remote and fetch --prune. It holds the
// per-repo_key mutex for the whole clone-or-fetch + meta write so two concurrent
// calls for the same repo never race (design §8). A source whose origin URL does
// not match (or a non-git stray dir) is refused fail-closed.
func (m *LocalGitMaterializer) EnsureSource(ctx context.Context, target RepoTarget) (SourceRepo, error) {
	url := strings.TrimSpace(target.URL)
	if url == "" {
		return SourceRepo{}, ErrRepoURLRequired
	}
	key := RepoKey(url)

	lk := m.lockFor(key)
	lk.Lock()
	defer lk.Unlock()

	repoDir := filepath.Join(m.reposRoot, key)
	sourcePath := filepath.Join(repoDir, "source")

	isRepo, err := isGitRepo(sourcePath)
	if err != nil {
		return SourceRepo{}, fmt.Errorf("reporepo: stat source repo_key=%s: %w", key, err)
	}

	// issue-13e7bfe8 layer 2 (self-heal): a `source` carrying a .git is NOT proof of a
	// USABLE repo. The pre-fix clone wrote straight into the canonical path, so a
	// cancelled clone (the 5s control-handler deadline SIGKILLing git) left a half-
	// written .git right here — and the reuse branch below could never notice: `remote
	// get-url` and `fetch` BOTH succeed on such a corpse (verified), so EnsureSource
	// returned "success" forever while every worktree add died on
	// "fatal: your current branch appears to be broken". Probe HEAD and quarantine a
	// broken source so the re-clone below heals it. Repos poisoned by the old code are
	// already on disk in the field, so this heal path is required, not theoretical.
	if isRepo {
		if herr := m.sourceHealth(ctx, sourcePath); herr != nil {
			m.logf("reporepo: repo_key=%s canonical source is BROKEN (%v) — quarantining + re-cloning (self-heal)", key, herr)
			if qerr := m.quarantineSource(repoDir, sourcePath); qerr != nil {
				return SourceRepo{}, fmt.Errorf("reporepo: quarantine broken source repo_key=%s: %w", key, qerr)
			}
			isRepo = false
		}
	}

	if isRepo {
		// Reuse only if the existing source points at the SAME remote (design §4).
		origin, oerr := m.originURL(ctx, sourcePath)
		if oerr != nil {
			return SourceRepo{}, fmt.Errorf("reporepo: read origin repo_key=%s: %w", key, oerr)
		}
		if normalizeRepoURL(origin) != normalizeRepoURL(url) {
			return SourceRepo{}, fmt.Errorf("repo_key=%s: %w", key, ErrRemoteMismatch)
		}
		if _, ferr := m.git(ctx, sourcePath, "fetch", "--prune", "origin"); ferr != nil {
			return SourceRepo{}, fmt.Errorf("reporepo: fetch repo_key=%s: %w", key, ferr)
		}
	} else {
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			return SourceRepo{}, fmt.Errorf("reporepo: mkdir repo_key=%s: %w", key, err)
		}
		// A `source` that exists but is not a git repo is a stray dir — refuse rather
		// than clone into or reuse it. (A HALF-clone has a .git and is handled by the
		// quarantine probe above; this stays fail-closed for a genuinely foreign dir.)
		if pathExists(sourcePath) {
			return SourceRepo{}, fmt.Errorf("repo_key=%s: %w", key, ErrSourceNotGitRepo)
		}
		if cerr := m.cloneAtomic(ctx, repoDir, url, sourcePath); cerr != nil {
			return SourceRepo{}, fmt.Errorf("reporepo: clone repo_key=%s: %w", key, cerr)
		}
	}

	if err := m.writeMeta(repoDir, target); err != nil {
		return SourceRepo{}, fmt.Errorf("reporepo: write meta repo_key=%s: %w", key, err)
	}
	return SourceRepo{RepoKey: key, Path: sourcePath, URL: url, BaseRef: target.resolvedBaseRef()}, nil
}

// cloneAtomic clones url into a STAGING dir under repoDir and only then renames the
// finished checkout onto the canonical sourcePath (issue-13e7bfe8 layer 2). This is the
// invariant that makes the canonical source un-poisonable: a clone is either absent or
// COMPLETE at sourcePath — never half-written — because the rename is the single atomic
// commit point and it runs only after git exited 0. It mirrors what writeMeta already
// did for meta.json; the repo itself was the one thing left un-staged.
//
// Staging lives INSIDE repoDir so the rename is a same-filesystem move (an os.Rename
// across devices would fail). Every non-success exit — error, ctx cancel, the SIGKILL a
// cancelled ctx delivers to git — removes the staging dir, so a retry always starts from
// a clean slate and self-heals rather than tripping over its own debris.
func (m *LocalGitMaterializer) cloneAtomic(ctx context.Context, repoDir, url, sourcePath string) error {
	staging, err := os.MkdirTemp(repoDir, stagingPrefix)
	if err != nil {
		return fmt.Errorf("stage dir: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			// Best-effort: ctx may already be cancelled, but removal needs no ctx.
			_ = os.RemoveAll(staging)
		}
	}()

	// Clone into <staging>/co, NOT into <repoDir>/source: nothing git writes is visible
	// at the canonical path until the rename below.
	if _, cerr := m.git(ctx, staging, "clone", url, stagingCheckout); cerr != nil {
		return cerr
	}
	if rerr := os.Rename(filepath.Join(staging, stagingCheckout), sourcePath); rerr != nil {
		return fmt.Errorf("promote staged clone: %w", rerr)
	}
	committed = true
	return os.RemoveAll(staging) // now-empty staging shell
}

// sourceHealth returns a non-nil error ONLY when it can prove the canonical source is
// broken. A nil return means "no verdict" — reuse the source.
//
// The probe is `rev-parse --verify HEAD` — chosen over the alternatives by measurement,
// not taste:
//   - `git status` is NOT a probe: it exits 0 on a refs-stripped half-clone.
//   - `remote get-url` / `fetch` are NOT probes: both exit 0 on the same corpse, which
//     is precisely why the pre-fix reuse branch never self-healed.
//
// THE VERDICT MUST BE DECISIVE, because the caller acts on it by DELETING the source.
// A probe that says "broken" whenever git merely failed to run would turn every transient
// hiccup — ctx cancellation, a fork/EAGAIN under load, a momentarily missing git binary —
// into destruction of a perfectly healthy repo. That would be a worse bug than the one
// this file fixes: corruption-on-cancel traded for data-loss-on-cancel. So we quarantine
// only when git ACTUALLY RAN and returned a non-zero verdict about the repo:
//
//   - ctx already dead ⇒ no verdict (note a ctx-killed git still surfaces as an
//     *exec.ExitError, "signal: killed", so this check MUST come first).
//   - the error is not an ExitError ⇒ git never ran ⇒ infrastructure, not corruption.
//
// A false "healthy" is safe: the fetch immediately below will fail on a broken repo and
// EnsureSource returns that error rather than destroying anything.
//
// Caveat, deliberately accepted: a clone of a genuinely EMPTY remote also has no
// resolvable HEAD, so it is judged broken and re-cloned once per EnsureSource. That is
// bounded and non-wedging (the re-clone succeeds; EnsureSource still returns success),
// and such a repo has no base commit to derive a worktree from either way.
func (m *LocalGitMaterializer) sourceHealth(ctx context.Context, sourcePath string) error {
	_, err := m.git(ctx, sourcePath, "rev-parse", "--verify", "HEAD")
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return nil // cancelled/timed out: says nothing about the repo
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		return nil // git could not be executed at all: not a repo verdict
	}
	return err
}

// quarantineSource clears a broken canonical source out of the way so the caller can
// re-clone into a clean path.
//
// It DOES destroy any per-executor worktrees linked off the old source (their admin data
// lives in source/.git/worktrees/<id>), and unlike PruneOrphanWorktrees it takes no
// isLive guard. That is a considered trade, not an oversight: sourceHealth only reports
// broken on a decisive git verdict, and a source with no resolvable HEAD cannot serve the
// worktrees hanging off it anyway — they are already unusable, which is how this incident
// surfaced (every `git worktree add` failing on "your current branch appears to be
// broken"). Leaving the corpse in place to protect them would keep the repo permanently
// wedged, which is the failure being fixed. The heal is logged loudly by the caller so a
// live executor's workspace disappearing is attributable rather than mysterious.
//
// It RENAMES aside first and only then deletes. The rename is the meaningful step: it
// unpublishes `source` atomically, so no concurrent reader can ever observe a
// half-deleted repo, and a delete that fails midway cannot recreate the very
// "partially-populated source" state this whole change exists to prevent. The subsequent
// RemoveAll is best-effort — a broken source is a full clone's worth of disk, so keeping
// the corpse around as a post-mortem artifact is not worth the space on a worker; if the
// delete fails the dir is inert (it is no longer named `source`) and sweepDebris collects
// it on the next pass.
func (m *LocalGitMaterializer) quarantineSource(repoDir, sourcePath string) error {
	// Sweep any earlier staging/quarantine debris so repoDir cannot grow across heals.
	m.sweepDebris(repoDir)
	dst, err := os.MkdirTemp(repoDir, quarantinePrefix)
	if err != nil {
		return err
	}
	// MkdirTemp created dst; rename needs a non-existent target.
	if rmErr := os.RemoveAll(dst); rmErr != nil {
		return rmErr
	}
	if rerr := os.Rename(sourcePath, dst); rerr != nil {
		return rerr
	}
	_ = os.RemoveAll(dst)
	return nil
}

// sweepDebris removes leftover staging/quarantine dirs under repoDir (best-effort).
// Never touches `source` or `meta.json`.
func (m *LocalGitMaterializer) sweepDebris(repoDir string) {
	entries, err := os.ReadDir(repoDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, stagingPrefix) || strings.HasPrefix(name, quarantinePrefix) {
			_ = os.RemoveAll(filepath.Join(repoDir, name))
		}
	}
}

// logf emits an operational line when a Log sink is wired (nil ⇒ silent).
func (m *LocalGitMaterializer) logf(format string, args ...any) {
	if m.Log == nil {
		return
	}
	m.Log(fmt.Sprintf(format, args...))
}

// baseRefCandidates is the resolution order for a base ref, remote-tracking FIRST.
//
// THE ORDER IS THE BUG FIX. `origin/<base>` is what `fetch --prune` advances; the local
// branch `<base>` is what it does NOT — EnsureSource never merges, checks out, or
// update-refs the local branch, so in a canonical source that is only ever fetched, local
// `main` is frozen at the SHA the FIRST clone happened to see, forever. Resolving `main`
// therefore used to hand every worktree a base that drifts further behind origin with
// every commit anyone lands (observed in prod: local main 4 commits and ~3h stale, growing
// without bound).
//
// Falling back to the bare ref keeps every non-branch base working unchanged: an explicit
// SHA (refs/remotes/origin/<sha> never exists), a tag, an already-qualified `origin/main`
// (which would otherwise become refs/remotes/origin/origin/main), or a local-only branch in
// a source with no such remote branch.
func baseRefCandidates(baseRef string) []string {
	return []string{"refs/remotes/origin/" + baseRef, baseRef}
}

// resolveBaseCommit pins baseRef to the concrete commit SHA a worktree should branch off,
// preferring origin's view of it (see baseRefCandidates). Returns ErrBaseRefUnresolved when
// nothing resolves.
//
// PINNING TO A SHA — not returning the ref name — is the second half of the fix, and it is
// what makes the first half safe. The BaseRef this returns is carried into the executor's
// durable Record and later feeds `rev-list --count <base>..HEAD`, whose result gates the
// eager-push (`BaseKnown && ahead<=0 && !Dirty` ⇒ silent skip ⇒ the commit dies with the
// reaped worktree — issue-f30b7e7b). A MOVING base ref is poison for that count: had we
// stored the name `origin/main`, every commit landing on origin mid-run would silently
// decrement this executor's ahead count and could drive it to 0 while it holds real,
// unpushed work. A SHA cannot move, so ahead counts exactly the commits THIS executor made
// — no undercount (no lost delivery), and no overcount (the "ahead 5, actually 1" report
// that made the number meaningless). The pinned commit stays reachable from the executor's
// own branch, so it always resolves in the worktree afterwards.
func (m *LocalGitMaterializer) resolveBaseCommit(ctx context.Context, sourcePath, baseRef string) (string, error) {
	for _, cand := range baseRefCandidates(baseRef) {
		// ^{commit} forces a commit (peels a tag, rejects a tree/blob); --verify --quiet
		// exits non-zero with no output when the ref names nothing.
		out, err := m.git(ctx, sourcePath, "rev-parse", "--verify", "--quiet", cand+"^{commit}")
		if err != nil {
			continue
		}
		if sha := strings.TrimSpace(out); sha != "" {
			return sha, nil
		}
	}
	return "", fmt.Errorf("base_ref=%q: %w", baseRef, ErrBaseRefUnresolved)
}

// PrepareWorktree derives a per-executor worktree on a fresh branch off the source,
// delegating the git worktree add to executor.WorktreeProvisioner (design §8 —
// reuse, don't rebuild). Serialized against clone/fetch/remove for the same repo_key.
//
// The base is resolved to a concrete commit (resolveBaseCommit) BEFORE the branch is cut,
// so the worktree starts at origin's current tip rather than at a stale local branch, and
// the returned BaseRef is that exact SHA rather than a name.
//
// Note what this deliberately does NOT do: fast-forward the source's local branch. The
// canonical source is a non-bare checkout with <default_branch> checked out in its MAIN
// worktree, so `update-ref refs/heads/main origin/main` there would move HEAD out from
// under a populated index — the source's own worktree would read as "every file deleted",
// and it is SHARED by every executor branching off it. Reading through origin/* instead
// makes the local branch's staleness irrelevant rather than racing to correct it, and
// touches no state any existing worktree depends on.
func (m *LocalGitMaterializer) PrepareWorktree(ctx context.Context, source SourceRepo, req WorktreeRequest) (PreparedWorktree, error) {
	if strings.TrimSpace(source.Path) == "" {
		return PreparedWorktree{}, errors.New("reporepo: source path required")
	}
	if strings.TrimSpace(req.WorkspacePath) == "" {
		return PreparedWorktree{}, errors.New("reporepo: workspace_path required")
	}
	if strings.TrimSpace(req.BranchName) == "" {
		return PreparedWorktree{}, errors.New("reporepo: branch_name required")
	}
	baseRef := strings.TrimSpace(req.BaseRef)
	if baseRef == "" {
		baseRef = strings.TrimSpace(source.BaseRef)
	}
	if baseRef == "" {
		return PreparedWorktree{}, errors.New("reporepo: base_ref required")
	}

	lk := m.lockFor(source.RepoKey)
	lk.Lock()
	defer lk.Unlock()

	// Resolve to a SHA under the lock, so no concurrent fetch for this repo_key can move
	// origin/<base> between the resolve and the branch cut: the SHA we record is provably
	// the SHA we branched off.
	baseCommit, rerr := m.resolveBaseCommit(ctx, source.Path, baseRef)
	if rerr != nil {
		return PreparedWorktree{}, fmt.Errorf("reporepo: resolve base repo_key=%s: %w", source.RepoKey, rerr)
	}

	prov, err := executor.NewWorktreeProvisioner(source.Path, m.runner)
	if err != nil {
		return PreparedWorktree{}, err
	}
	if err := prov.AddNewBranch(ctx, req.WorkspacePath, req.BranchName, baseCommit); err != nil {
		return PreparedWorktree{}, err
	}
	return PreparedWorktree{
		ExecutorID:    req.ExecutorID,
		RepoKey:       source.RepoKey,
		SourcePath:    source.Path,
		WorkspacePath: req.WorkspacePath,
		Branch:        req.BranchName,
		BaseRef:       baseCommit,
	}, nil
}

// RemoveWorktree tears down ONLY the per-executor worktree (design §10 hard rule:
// the canonical source is never removed by cleanup). Delegates to
// executor.WorktreeProvisioner (git worktree remove --force + prune).
func (m *LocalGitMaterializer) RemoveWorktree(ctx context.Context, wt PreparedWorktree) error {
	if strings.TrimSpace(wt.SourcePath) == "" {
		return errors.New("reporepo: source path required")
	}
	if strings.TrimSpace(wt.WorkspacePath) == "" {
		return errors.New("reporepo: workspace_path required")
	}
	lk := m.lockFor(wt.RepoKey)
	lk.Lock()
	defer lk.Unlock()

	prov, err := executor.NewWorktreeProvisioner(wt.SourcePath, m.runner)
	if err != nil {
		return err
	}
	// Only the worktree path is removed; wt.SourcePath is never touched.
	return prov.Remove(ctx, wt.WorkspacePath)
}

// PruneOrphanWorktrees reaps orphaned per-executor worktrees under ONE source (v2.31.1
// bugfix: a retryable-crash keeps its worktree — design §7 inspection — but re-dispatch
// is fresh-id, so the old worktree is never reused and nothing else cleans it). It
// lists the source's linked worktrees and removes each whose owning executor is no
// longer live, plus its stale ac-exec/* branch. The canonical source (the source's own
// MAIN worktree) is NEVER touched. Best-effort per entry.
//
// FAIL-SAFE (hard rule): only an UNAMBIGUOUSLY orphaned worktree is removed — the
// executor id must parse from the worktree PATH and the branch AND agree, and isLive
// must report false. A nil isLive, an unparseable/mismatched id, or any doubt → the
// worktree is KEPT (deleting a live executor's worktree is unrecoverable corruption;
// leaking one orphan is recoverable and reaped on a later pass).
func (m *LocalGitMaterializer) PruneOrphanWorktrees(ctx context.Context, source SourceRepo, isLive func(execID string) bool) (int, error) {
	if strings.TrimSpace(source.Path) == "" {
		return 0, errors.New("reporepo: source path required")
	}
	if isLive == nil {
		// No liveness oracle → treat everything as live, reap nothing (fail-safe).
		return 0, nil
	}
	lk := m.lockFor(source.RepoKey)
	lk.Lock()
	defer lk.Unlock()

	out, err := m.git(ctx, source.Path, "worktree", "list", "--porcelain")
	if err != nil {
		return 0, fmt.Errorf("reporepo: worktree list repo_key=%s: %w", source.RepoKey, err)
	}
	prov, perr := executor.NewWorktreeProvisioner(source.Path, m.runner)
	if perr != nil {
		return 0, perr
	}
	canonical := filepath.Clean(source.Path)
	pruned := 0
	for _, e := range parseWorktreeList(out) {
		if filepath.Clean(e.path) == canonical {
			continue // the source's own MAIN worktree — never touched (§10)
		}
		execID := orphanExecID(e.path, e.branch)
		if execID == "" {
			continue // can't unambiguously identify → KEEP (fail-safe)
		}
		if isLive(execID) {
			continue // still running / adopted → KEEP
		}
		// Unambiguous orphan: remove the worktree (never the source) + its stale branch.
		if rerr := prov.Remove(ctx, e.path); rerr != nil {
			// Best-effort: leave it for a later pass rather than abort the whole sweep.
			continue
		}
		if e.branch != "" {
			_, _ = m.git(ctx, source.Path, "branch", "-D", e.branch) // best-effort stale-branch cleanup
		}
		pruned++
	}
	return pruned, nil
}

// ReapOrphanWorktrees runs PruneOrphanWorktrees across every materialized source under
// the repos root — the boot-reconcile sweep that cleans orphans left by a prior process
// (full-crash path). Best-effort per source: a bad/absent repo dir is skipped, not fatal.
func (m *LocalGitMaterializer) ReapOrphanWorktrees(ctx context.Context, isLive func(execID string) bool) (int, error) {
	entries, err := os.ReadDir(m.reposRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil // no repos materialized yet
		}
		return 0, fmt.Errorf("reporepo: read repos root: %w", err)
	}
	total := 0
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		key := ent.Name()
		sourcePath := filepath.Join(m.reposRoot, key, "source")
		if fi, statErr := os.Stat(filepath.Join(sourcePath, ".git")); statErr != nil || (!fi.IsDir() && fi.Mode().IsRegular() == false) {
			continue // not a materialized source checkout — skip
		}
		n, perr := m.PruneOrphanWorktrees(ctx, SourceRepo{RepoKey: key, Path: sourcePath}, isLive)
		if perr != nil {
			continue // best-effort per source
		}
		total += n
	}
	return total, nil
}

// worktreeEntry is one `git worktree list --porcelain` record (path + checked-out branch).
type worktreeEntry struct {
	path   string
	branch string // short branch name (refs/heads/ stripped); "" when detached
}

// parseWorktreeList parses `git worktree list --porcelain` output: blank-line-separated
// records, each with a "worktree <path>" line and (unless detached) a "branch
// refs/heads/<name>" line.
func parseWorktreeList(out string) []worktreeEntry {
	var entries []worktreeEntry
	var cur worktreeEntry
	flush := func() {
		if cur.path != "" {
			entries = append(entries, cur)
		}
		cur = worktreeEntry{}
	}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			cur.path = strings.TrimSpace(strings.TrimPrefix(line, "worktree "))
		case strings.HasPrefix(line, "branch "):
			ref := strings.TrimSpace(strings.TrimPrefix(line, "branch "))
			cur.branch = strings.TrimPrefix(ref, "refs/heads/")
		case line == "":
			flush()
		}
	}
	flush()
	return entries
}

// orphanExecID returns the executor id for a per-executor worktree ONLY when it parses
// unambiguously from BOTH the workspace path (<...>/executors/<id>/workspace) AND the
// branch (ac-exec/<task_id>/<id>) and the two AGREE. Any mismatch / missing side → ""
// (the caller keeps the worktree — fail-safe). This double-parse is the guard against
// ever reaping a worktree we can't positively tie to a dead executor.
func orphanExecID(worktreePath, branch string) string {
	fromPath := execIDFromWorkspacePath(worktreePath)
	fromBranch := execIDFromBranch(branch)
	if fromPath == "" || fromBranch == "" || fromPath != fromBranch {
		return ""
	}
	return fromPath
}

// execIDFromWorkspacePath extracts <id> from a path ending .../executors/<id>/workspace.
func execIDFromWorkspacePath(p string) string {
	p = filepath.Clean(p)
	if filepath.Base(p) != "workspace" {
		return ""
	}
	idDir := filepath.Dir(p) // .../executors/<id>
	if filepath.Base(filepath.Dir(idDir)) != "executors" {
		return ""
	}
	id := filepath.Base(idDir)
	if id == "." || id == string(filepath.Separator) {
		return ""
	}
	return id
}

// execIDFromBranch extracts <id> from an ac-exec/<task_id>/<id> branch.
func execIDFromBranch(branch string) string {
	if branch == "" {
		return ""
	}
	parts := strings.Split(branch, "/")
	if len(parts) < 3 || parts[0] != "ac-exec" {
		return ""
	}
	return parts[len(parts)-1]
}

// lockFor returns the shared per-repo_key mutex, creating it on first use.
func (m *LocalGitMaterializer) lockFor(key string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	l, ok := m.locks[key]
	if !ok {
		l = &sync.Mutex{}
		m.locks[key] = l
	}
	return l
}

// originURL reads the source's origin remote URL.
func (m *LocalGitMaterializer) originURL(ctx context.Context, sourcePath string) (string, error) {
	out, err := m.git(ctx, sourcePath, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// git runs a materializer-owned git command (clone / fetch / remote) under workdir
// with the network-capable environment. Worktree subcommands do NOT go through
// here — they use executor.WorktreeProvisioner.
func (m *LocalGitMaterializer) git(ctx context.Context, workdir string, args ...string) (string, error) {
	return m.runner.Run(ctx, workdir, networkGitEnv(), args...)
}

// repoMeta is the on-disk repos/<repo_key>/meta.json (design §4).
type repoMeta struct {
	RepoID        string `json:"repo_id"`
	URL           string `json:"url"`
	Provider      string `json:"provider"`
	DefaultBranch string `json:"default_branch"`
	LastFetchAt   string `json:"last_fetch_at"`
}

// writeMeta atomically writes meta.json (temp + rename) so a crash mid-write never
// leaves a truncated identity file.
func (m *LocalGitMaterializer) writeMeta(repoDir string, t RepoTarget) error {
	meta := repoMeta{
		RepoID:        t.RepoID,
		URL:           t.URL,
		Provider:      t.Provider,
		DefaultBranch: t.DefaultBranch,
		LastFetchAt:   m.clock.Now().UTC().Format(time.RFC3339),
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	metaPath := filepath.Join(repoDir, "meta.json")
	tmp := metaPath + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, metaPath)
}

// RepoKey is the stable per-repo directory key: sha256(normalized URL) as hex
// (design §4). Distinct spellings of the same remote (trailing "/", ".git")
// collapse to one key.
func RepoKey(url string) string {
	sum := sha256.Sum256([]byte(normalizeRepoURL(url)))
	return hex.EncodeToString(sum[:])
}

// normalizeRepoURL trims surrounding space and a single trailing "/" and ".git"
// so "…/x", "…/x/", and "…/x.git" normalize identically.
func normalizeRepoURL(url string) string {
	s := strings.TrimSpace(url)
	s = strings.TrimSuffix(s, "/")
	s = strings.TrimSuffix(s, ".git")
	s = strings.TrimSuffix(s, "/")
	return s
}

// isGitRepo reports whether path is a git working tree (has a .git entry).
func isGitRepo(path string) (bool, error) {
	_, err := os.Stat(filepath.Join(path, ".git"))
	if err == nil {
		return true, nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

// pathExists reports whether path exists at all.
func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// networkGitEnv inherits the process environment (so the host SSH agent / deploy
// key and gitconfig url-rewrites remain available for clone/fetch — design §6 v1
// auth model) and only disables interactive prompts so a missing credential fails
// closed instead of hanging.
func networkGitEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
	)
}
