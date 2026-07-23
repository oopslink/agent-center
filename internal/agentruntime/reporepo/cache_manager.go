package reporepo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/clock"
)

const defaultFetchTTL = 60 * time.Second
const (
	defaultMirrorIdle    = 7 * 24 * time.Hour
	defaultMaxCacheBytes = int64(20 << 30)
)

var ErrCacheRefUnavailable = errors.New("reporepo: cached ref unavailable")

// CacheHealth is the durable operational view of one repository mirror.
type CacheHealth struct {
	RepoKey      string    `json:"repo_key"`
	MirrorPath   string    `json:"mirror_path"`
	TargetRef    string    `json:"target_ref,omitempty"`
	LastFetchAt  time.Time `json:"last_fetch_at,omitempty"`
	LastAccessAt time.Time `json:"last_access_at"`
	Stale        bool      `json:"stale"`
	LastError    string    `json:"last_error,omitempty"`
}

// WorktreeRecord is the auditable run-to-repository mapping.
type WorktreeRecord struct {
	RunID        string    `json:"run_id"`
	OwnerID      string    `json:"owner_id,omitempty"`
	TaskID       string    `json:"task_id,omitempty"`
	RepoKey      string    `json:"repo_key"`
	Ref          string    `json:"ref"`
	Branch       string    `json:"branch"`
	Path         string    `json:"path"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"created_at"`
	CleanedAt    time.Time `json:"cleaned_at,omitempty"`
	CleanupError string    `json:"cleanup_error,omitempty"`
}

// RepoCacheManager owns worker-local bare mirrors and executor worktrees.
// runtimeRoot is shared by all agent-runtime processes on one worker.
type RepoCacheManager struct {
	Log func(string)

	runtimeRoot   string
	reposRoot     string
	worktreeRoot  string
	registryRoot  string
	runner        executor.GitRunner
	clock         clock.Clock
	fetchTTL      time.Duration
	mirrorIdle    time.Duration
	maxCacheBytes int64
	helper        *LocalGitMaterializer
	ownerID       string
}

// SetOwner namespaces liveness/reconcile to one agent-runtime while mirrors and
// the registry remain shared by every agent process on the worker.
func (m *RepoCacheManager) SetOwner(ownerID string) {
	m.ownerID = strings.TrimSpace(ownerID)
}

var _ RepoMaterializer = (*RepoCacheManager)(nil)

func NewRepoCacheManager(runtimeRoot string, runner executor.GitRunner, clk clock.Clock) (*RepoCacheManager, error) {
	if strings.TrimSpace(runtimeRoot) == "" {
		return nil, errors.New("reporepo: runtime_root required")
	}
	if runner == nil {
		runner = executor.NewExecGitRunner()
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	reposRoot := filepath.Join(runtimeRoot, "repos")
	helper, err := NewLocalGitMaterializer(reposRoot, runner, clk)
	if err != nil {
		return nil, err
	}
	m := &RepoCacheManager{
		runtimeRoot:   runtimeRoot,
		reposRoot:     reposRoot,
		worktreeRoot:  filepath.Join(runtimeRoot, "worktrees"),
		registryRoot:  filepath.Join(runtimeRoot, "registry", "worktrees"),
		runner:        runner,
		clock:         clk,
		fetchTTL:      defaultFetchTTL,
		mirrorIdle:    defaultMirrorIdle,
		maxCacheBytes: defaultMaxCacheBytes,
		helper:        helper,
	}
	for _, dir := range []string{m.reposRoot, m.worktreeRoot, m.registryRoot} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("reporepo: create cache directory: %w", err)
		}
	}
	return m, nil
}

func (m *RepoCacheManager) EnsureSource(ctx context.Context, target RepoTarget) (SourceRepo, error) {
	url := strings.TrimSpace(target.URL)
	if url == "" {
		return SourceRepo{}, ErrRepoURLRequired
	}
	key := RepoKey(url)
	repoDir := filepath.Join(m.reposRoot, key)
	mirrorPath := filepath.Join(repoDir, "mirror.git")
	ref := target.resolvedBaseRef()
	var result SourceRepo
	err := m.withRepoLock(ctx, key, func() error {
		if err := os.MkdirAll(repoDir, 0o755); err != nil {
			return err
		}
		health := m.readHealth(repoDir)
		isBare := false
		if pathExists(mirrorPath) {
			out, err := m.git(ctx, mirrorPath, "rev-parse", "--is-bare-repository")
			isBare = err == nil && strings.TrimSpace(out) == "true"
			if !isBare {
				return ErrSourceNotGitRepo
			}
			origin, err := m.helper.originURL(ctx, mirrorPath)
			if err != nil {
				return err
			}
			if normalizeRepoURL(origin) != normalizeRepoURL(url) {
				return ErrRemoteMismatch
			}
		} else {
			staging := mirrorPath + ".staging"
			_ = os.RemoveAll(staging)
			if _, err := m.git(ctx, repoDir, "clone", "--mirror", url, staging); err != nil {
				_ = os.RemoveAll(staging)
				return fmt.Errorf("initialize mirror: %w", err)
			}
			if err := os.Rename(staging, mirrorPath); err != nil {
				_ = os.RemoveAll(staging)
				return fmt.Errorf("publish mirror: %w", err)
			}
			isBare = true
		}

		now := m.clock.Now()
		stale := false
		if health.LastFetchAt.IsZero() || now.Sub(health.LastFetchAt) >= m.fetchTTL {
			if _, err := m.git(ctx, mirrorPath, "fetch", "--prune", "origin", "+refs/heads/*:refs/heads/*", "+refs/tags/*:refs/tags/*"); err != nil {
				if _, resolveErr := m.resolveMirrorCommit(ctx, mirrorPath, ref); resolveErr != nil {
					health = CacheHealth{RepoKey: key, MirrorPath: mirrorPath, TargetRef: ref, LastAccessAt: now, Stale: true, LastError: "fetch failed and target ref is unavailable"}
					_ = m.writeHealth(repoDir, health)
					return fmt.Errorf("%w: repo_key=%s ref=%s", ErrCacheRefUnavailable, key, ref)
				}
				stale = true
			} else {
				health.LastFetchAt = now
			}
		}
		if !isBare {
			return ErrSourceNotGitRepo
		}
		if _, err := m.resolveMirrorCommit(ctx, mirrorPath, ref); err != nil {
			return fmt.Errorf("%w: repo_key=%s ref=%s", ErrCacheRefUnavailable, key, ref)
		}
		health.RepoKey = key
		health.MirrorPath = mirrorPath
		health.TargetRef = ref
		health.LastAccessAt = now
		health.Stale = stale
		if !stale {
			health.LastError = ""
		} else {
			health.LastError = "network refresh failed; cached ref used"
		}
		if err := m.writeHealth(repoDir, health); err != nil {
			return err
		}
		result = SourceRepo{RepoKey: key, Path: mirrorPath, URL: url, BaseRef: ref, Stale: stale, LastFetchAt: health.LastFetchAt}
		return nil
	})
	if err != nil {
		return SourceRepo{}, fmt.Errorf("reporepo: ensure mirror repo_key=%s: %w", key, err)
	}
	m.logf("reporepo: repo_key=%s mirror=%s ref=%s stale=%t", key, mirrorPath, ref, result.Stale)
	return result, nil
}

func (m *RepoCacheManager) PrepareWorktree(ctx context.Context, source SourceRepo, req WorktreeRequest) (PreparedWorktree, error) {
	if strings.TrimSpace(req.ExecutorID) == "" || strings.TrimSpace(req.BranchName) == "" {
		return PreparedWorktree{}, errors.New("reporepo: run_id and branch_name required")
	}
	if filepath.Base(req.ExecutorID) != req.ExecutorID || req.ExecutorID == "." || req.ExecutorID == ".." {
		return PreparedWorktree{}, errors.New("reporepo: invalid run_id")
	}
	path := filepath.Join(m.worktreeRoot, req.ExecutorID)
	baseRef := strings.TrimSpace(req.BaseRef)
	if baseRef == "" {
		baseRef = source.BaseRef
	}
	var prepared PreparedWorktree
	err := m.withRepoLock(ctx, source.RepoKey, func() error {
		baseCommit, err := m.resolveMirrorCommit(ctx, source.Path, baseRef)
		if err != nil {
			return err
		}
		if pathExists(path) {
			return fmt.Errorf("worktree path already exists: %s", path)
		}
		prov, err := executor.NewWorktreeProvisioner(source.Path, m.runner)
		if err != nil {
			return err
		}
		if err := prov.AddNewBranch(ctx, path, req.BranchName, baseCommit); err != nil {
			return err
		}
		rec := WorktreeRecord{RunID: req.ExecutorID, OwnerID: m.ownerID, TaskID: req.TaskID, RepoKey: source.RepoKey, Ref: baseCommit, Branch: req.BranchName, Path: path, Status: "active", CreatedAt: m.clock.Now()}
		if err := m.writeWorktreeRecord(rec); err != nil {
			_ = prov.Remove(ctx, path)
			return err
		}
		prepared = PreparedWorktree{ExecutorID: req.ExecutorID, RepoKey: source.RepoKey, SourcePath: source.Path, WorkspacePath: path, Branch: req.BranchName, BaseRef: baseCommit}
		return nil
	})
	return prepared, err
}

// CreateWorktree is the runtime-facing lifecycle API. PrepareWorktree remains
// the RepoMaterializer compatibility method used by the existing fork engine.
func (m *RepoCacheManager) CreateWorktree(ctx context.Context, source SourceRepo, req WorktreeRequest) (PreparedWorktree, error) {
	return m.PrepareWorktree(ctx, source, req)
}

func (m *RepoCacheManager) PrepareClone(ctx context.Context, target RepoTarget, req CloneRequest) (PreparedClone, error) {
	source, err := m.EnsureSource(ctx, target)
	if err != nil {
		return PreparedClone{}, err
	}
	wt, err := m.PrepareWorktree(ctx, source, WorktreeRequest{ExecutorID: req.ExecutorID, TaskID: req.TaskID, BranchName: req.BranchName, BaseRef: req.BaseRef})
	if err != nil {
		return PreparedClone{}, err
	}
	return PreparedClone{ExecutorID: wt.ExecutorID, RepoKey: wt.RepoKey, WorkspacePath: wt.WorkspacePath, Branch: wt.Branch, BaseRef: wt.BaseRef}, nil
}

func (m *RepoCacheManager) RemoveWorktree(ctx context.Context, wt PreparedWorktree) error {
	rec, _ := m.readWorktreeRecord(wt.ExecutorID)
	path := strings.TrimSpace(wt.WorkspacePath)
	if path == "" {
		path = rec.Path
	}
	if rec.RunID == "" && path != "" {
		for _, candidate := range mustRecords(m.records()) {
			if filepath.Clean(candidate.Path) == filepath.Clean(path) {
				rec = candidate
				break
			}
		}
	}
	if strings.TrimSpace(wt.Branch) == "" {
		wt.Branch = rec.Branch
	}
	if strings.TrimSpace(wt.SourcePath) == "" {
		wt.SourcePath = filepath.Join(m.reposRoot, wt.RepoKey, "mirror.git")
	}
	rel, relErr := filepath.Rel(m.worktreeRoot, path)
	if relErr != nil || rel == "." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return errors.New("reporepo: worktree path outside runtime root")
	}
	return m.withRepoLock(ctx, wt.RepoKey, func() error {
		prov, err := executor.NewWorktreeProvisioner(wt.SourcePath, m.runner)
		if err != nil {
			return err
		}
		cleanupErr := prov.Remove(ctx, path)
		if cleanupErr == nil && strings.TrimSpace(wt.Branch) != "" {
			_, _ = m.git(ctx, wt.SourcePath, "branch", "-D", wt.Branch)
		}
		if rec.RunID != "" {
			rec.Status = "cleaned"
			rec.CleanedAt = m.clock.Now()
			if cleanupErr != nil {
				rec.Status = "cleanup_failed"
				rec.CleanupError = cleanupErr.Error()
			}
			_ = m.writeWorktreeRecord(rec)
		}
		return cleanupErr
	})
}

// Health returns the persisted mirror health/audit state for repoKey.
func (m *RepoCacheManager) Health(repoKey string) (CacheHealth, error) {
	repoKey = strings.TrimSpace(repoKey)
	if repoKey == "" || filepath.Base(repoKey) != repoKey {
		return CacheHealth{}, errors.New("reporepo: invalid repo_key")
	}
	path := m.healthPath(filepath.Join(m.reposRoot, repoKey))
	var health CacheHealth
	b, err := os.ReadFile(path)
	if err != nil {
		return health, err
	}
	return health, json.Unmarshal(b, &health)
}

// Worktrees returns the durable worktree registry, including cleaned and failed
// cleanup records, for operator audit.
func (m *RepoCacheManager) Worktrees() ([]WorktreeRecord, error) {
	return m.records()
}

func mustRecords(records []WorktreeRecord, err error) []WorktreeRecord {
	if err != nil {
		return nil
	}
	return records
}

func (m *RepoCacheManager) PruneOrphanWorktrees(ctx context.Context, source SourceRepo, isLive func(string) bool) (int, error) {
	if isLive == nil {
		return 0, nil
	}
	records, err := m.records()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, rec := range records {
		if !m.owns(rec) || rec.RepoKey != source.RepoKey || rec.Status != "active" || isLive(rec.RunID) {
			continue
		}
		err := m.RemoveWorktree(ctx, PreparedWorktree{ExecutorID: rec.RunID, RepoKey: rec.RepoKey, SourcePath: source.Path, WorkspacePath: rec.Path, Branch: rec.Branch})
		if err == nil {
			n++
		}
	}
	return n, nil
}

func (m *RepoCacheManager) ReapOrphanWorktrees(ctx context.Context, isLive func(string) bool) (int, error) {
	release, ok, err := m.tryFileLock(filepath.Join(m.runtimeRoot, "gc.lock"))
	if err != nil || !ok {
		return 0, err
	}
	defer release()
	records, err := m.records()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, rec := range records {
		if !m.owns(rec) || rec.Status != "active" || isLive == nil || isLive(rec.RunID) {
			continue
		}
		source := filepath.Join(m.reposRoot, rec.RepoKey, "mirror.git")
		if err := m.RemoveWorktree(ctx, PreparedWorktree{ExecutorID: rec.RunID, RepoKey: rec.RepoKey, SourcePath: source, WorkspacePath: rec.Path, Branch: rec.Branch}); err == nil {
			n++
		}
	}
	m.gcMirrors(records)
	return n, nil
}

func (m *RepoCacheManager) owns(rec WorktreeRecord) bool {
	return m.ownerID == "" || rec.OwnerID == m.ownerID
}

// gcMirrors evicts only mirrors with no active worktree. Idle mirrors go first;
// disk pressure then removes least-recently-accessed mirrors until under budget.
// Every evicted mirror is reconstructible from the persistent repository URL.
func (m *RepoCacheManager) gcMirrors(records []WorktreeRecord) {
	active := make(map[string]bool)
	for _, rec := range records {
		if rec.Status == "active" {
			active[rec.RepoKey] = true
		}
	}
	type candidate struct {
		key    string
		path   string
		access time.Time
		size   int64
	}
	entries, err := os.ReadDir(m.reposRoot)
	if err != nil {
		return
	}
	var candidates []candidate
	var total int64
	now := m.clock.Now()
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		repoDir := filepath.Join(m.reposRoot, ent.Name())
		size := dirSize(repoDir)
		total += size
		h := m.readHealth(repoDir)
		if !active[ent.Name()] {
			candidates = append(candidates, candidate{key: ent.Name(), path: repoDir, access: h.LastAccessAt, size: size})
		}
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].access.Before(candidates[j].access) })
	for _, c := range candidates {
		idle := c.access.IsZero() || now.Sub(c.access) >= m.mirrorIdle
		if !idle && total <= m.maxCacheBytes {
			continue
		}
		release, ok, err := m.tryFileLock(filepath.Join(c.path, "repo.lock"))
		if err != nil || !ok {
			continue
		}
		trash := c.path + ".gc-" + fmt.Sprint(now.UnixNano())
		if err := os.Rename(c.path, trash); err == nil {
			_ = os.RemoveAll(trash)
			total -= c.size
			m.logf("reporepo: evicted repo_key=%s mirror cache size=%d", c.key, c.size)
		}
		release()
	}
}

func dirSize(root string) int64 {
	var total int64
	_ = filepath.Walk(root, func(_ string, info os.FileInfo, err error) error {
		if err == nil && info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total
}

func (m *RepoCacheManager) resolveMirrorCommit(ctx context.Context, mirror, ref string) (string, error) {
	if strings.TrimSpace(ref) == "" {
		return "", ErrBaseRefUnresolved
	}
	for _, candidate := range []string{ref, "refs/heads/" + ref, "refs/tags/" + ref, "refs/remotes/origin/" + ref} {
		out, err := m.git(ctx, mirror, "rev-parse", "--verify", candidate+"^{commit}")
		if err == nil && strings.TrimSpace(out) != "" {
			return strings.TrimSpace(out), nil
		}
	}
	return "", ErrBaseRefUnresolved
}

func (m *RepoCacheManager) withRepoLock(ctx context.Context, key string, fn func() error) error {
	lockPath := filepath.Join(m.reposRoot, key, "repo.lock")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o755); err != nil {
		return err
	}
	for {
		release, ok, err := m.tryFileLock(lockPath)
		if err != nil {
			return err
		}
		if ok {
			defer release()
			return fn()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(25 * time.Millisecond):
		}
	}
}

func (m *RepoCacheManager) tryFileLock(path string) (func(), bool, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, true, nil
}

func (m *RepoCacheManager) healthPath(repoDir string) string {
	return filepath.Join(repoDir, "health.json")
}
func (m *RepoCacheManager) readHealth(repoDir string) CacheHealth {
	var h CacheHealth
	b, err := os.ReadFile(m.healthPath(repoDir))
	if err == nil {
		_ = json.Unmarshal(b, &h)
	}
	return h
}
func (m *RepoCacheManager) writeHealth(repoDir string, h CacheHealth) error {
	return writeJSONFileAtomic(m.healthPath(repoDir), h)
}
func (m *RepoCacheManager) recordPath(id string) string {
	return filepath.Join(m.registryRoot, id+".json")
}
func (m *RepoCacheManager) writeWorktreeRecord(rec WorktreeRecord) error {
	return writeJSONFileAtomic(m.recordPath(rec.RunID), rec)
}
func (m *RepoCacheManager) readWorktreeRecord(id string) (WorktreeRecord, error) {
	var rec WorktreeRecord
	b, err := os.ReadFile(m.recordPath(id))
	if err != nil {
		return rec, err
	}
	return rec, json.Unmarshal(b, &rec)
}
func (m *RepoCacheManager) records() ([]WorktreeRecord, error) {
	entries, err := os.ReadDir(m.registryRoot)
	if err != nil {
		return nil, err
	}
	out := make([]WorktreeRecord, 0, len(entries))
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		rec, err := m.readWorktreeRecord(strings.TrimSuffix(ent.Name(), ".json"))
		if err == nil {
			out = append(out, rec)
		}
	}
	return out, nil
}
func writeJSONFileAtomic(path string, value any) error {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
func (m *RepoCacheManager) git(ctx context.Context, dir string, args ...string) (string, error) {
	return m.runner.Run(ctx, dir, nil, args...)
}
func (m *RepoCacheManager) logf(format string, args ...any) {
	if m.Log != nil {
		m.Log(fmt.Sprintf(format, args...))
	}
}
