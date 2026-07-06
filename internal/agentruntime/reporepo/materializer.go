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
	BaseRef       string
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

// LocalGitMaterializer is the single-machine v1 RepoMaterializer: a non-bare
// `source` checkout per repo_key, per-executor worktrees added off it, all git
// state on the local worker host.
type LocalGitMaterializer struct {
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
		// A `source` that exists but is not a git repo is a stray/half-materialized
		// dir — refuse rather than clone into or reuse it.
		if pathExists(sourcePath) {
			return SourceRepo{}, fmt.Errorf("repo_key=%s: %w", key, ErrSourceNotGitRepo)
		}
		if _, cerr := m.git(ctx, repoDir, "clone", url, "source"); cerr != nil {
			return SourceRepo{}, fmt.Errorf("reporepo: clone repo_key=%s: %w", key, cerr)
		}
	}

	if err := m.writeMeta(repoDir, target); err != nil {
		return SourceRepo{}, fmt.Errorf("reporepo: write meta repo_key=%s: %w", key, err)
	}
	return SourceRepo{RepoKey: key, Path: sourcePath, URL: url, BaseRef: target.resolvedBaseRef()}, nil
}

// PrepareWorktree derives a per-executor worktree on a fresh branch off the source,
// delegating the git worktree add to executor.WorktreeProvisioner (design §8 —
// reuse, don't rebuild). Serialized against clone/fetch/remove for the same repo_key.
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

	prov, err := executor.NewWorktreeProvisioner(source.Path, m.runner)
	if err != nil {
		return PreparedWorktree{}, err
	}
	if err := prov.AddNewBranch(ctx, req.WorkspacePath, req.BranchName, baseRef); err != nil {
		return PreparedWorktree{}, err
	}
	return PreparedWorktree{
		ExecutorID:    req.ExecutorID,
		RepoKey:       source.RepoKey,
		SourcePath:    source.Path,
		WorkspacePath: req.WorkspacePath,
		Branch:        req.BranchName,
		BaseRef:       baseRef,
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
