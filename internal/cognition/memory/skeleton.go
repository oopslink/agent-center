package memory

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
)

// SkeletonFactory creates initial MEMORY.md / supervisor.md files for
// new scopes (cognition/02-memory § 4). It owns mkdir + write + git
// add+commit; the actual git interactions are delegated to GitOps.
type SkeletonFactory struct {
	memoryDir string
	gitops    *GitOps
}

// NewSkeletonFactory wires a factory.
func NewSkeletonFactory(memoryDir string, gitops *GitOps) *SkeletonFactory {
	return &SkeletonFactory{memoryDir: memoryDir, gitops: gitops}
}

// CreateSkeleton materialises the file (mkdir -p + write H1 + comment) and
// commits via gitops. Idempotent: if the file already exists returns nil
// (replay-safe; cognition/02 § 4).
func (f *SkeletonFactory) CreateSkeleton(ctx context.Context, scope MemoryScope) error {
	if f.memoryDir == "" {
		return ErrMemoryDirEmpty
	}
	abs, err := AbsPath(f.memoryDir, scope)
	if err != nil {
		return err
	}
	if _, err := os.Stat(abs); err == nil {
		return nil // idempotent
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("memory: stat: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("memory: mkdir: %w", err)
	}
	content := defaultSkeleton(scope)
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}
	rel, err := ScopeToFSPath(scope)
	if err != nil {
		return err
	}
	msg := fmt.Sprintf("init: skeleton for %s", scopeLabel(scope))
	if err := f.gitops.CommitFile(ctx, rel,
		"system:bootstrap", "system:bootstrap@agent-center.local", msg); err != nil {
		return err
	}
	return nil
}

// EnsureRootInit checks if memoryDir is a git repo; if not, runs `git init`
// and seeds MEMORY.md + supervisor.md skeletons. Idempotent. Called at
// center startup.
func (f *SkeletonFactory) EnsureRootInit(ctx context.Context) error {
	if f.memoryDir == "" {
		return ErrMemoryDirEmpty
	}
	if err := os.MkdirAll(f.memoryDir, 0o755); err != nil {
		return fmt.Errorf("memory: mkdir root: %w", err)
	}
	isRepo, err := f.gitops.IsGitRepo(ctx)
	if err != nil {
		return err
	}
	if !isRepo {
		if err := f.gitops.Init(ctx); err != nil {
			return err
		}
	}
	// Migrate legacy CLAUDE.md → MEMORY.md (idempotent).
	f.migrateFromLegacyName(ctx)
	// global MEMORY.md
	if err := f.CreateSkeleton(ctx, MemoryScope{Kind: MemScopeGlobal}); err != nil {
		return err
	}
	// supervisor.md
	if err := f.CreateSkeleton(ctx, MemoryScope{Kind: MemScopeSupervisor}); err != nil {
		return err
	}
	return nil
}

func defaultSkeleton(scope MemoryScope) string {
	switch scope.Kind {
	case MemScopeGlobal:
		return `# agent-center Memory

<!-- This is the supervisor's global notebook. Append observations, runbooks,
     and cross-scope patterns here. Files closer to a scope (project / task /
     issue / conversation / worker) take precedence via ancestor walk. -->
`
	case MemScopeSupervisor:
		return `# Supervisor Self-Memory

<!-- Self-referential notes for the supervisor (workflow tweaks, common
     failure modes). Loaded explicitly at the top of every invocation prompt
     (not via MEMORY.md ancestor walk). Keep it short; expand into specific
     scope files when relevant. -->
`
	case MemScopeProject:
		return fmt.Sprintf("# Project %s\n\n<!-- Project-scope memory. -->\n", scope.ProjectID)
	case MemScopeTask:
		return fmt.Sprintf("# Task %s (project %s)\n\n<!-- Task-scope memory. -->\n", scope.Key, scope.ProjectID)
	case MemScopeIssue:
		return fmt.Sprintf("# Issue %s (project %s)\n\n<!-- Issue-scope memory. -->\n", scope.Key, scope.ProjectID)
	case MemScopeConversation:
		return fmt.Sprintf("# Conversation %s\n\n<!-- Conversation-scope memory. -->\n", scope.Key)
	case MemScopeWorker:
		return fmt.Sprintf("# Worker %s\n\n<!-- Worker-scope memory. -->\n", scope.Key)
	}
	return "# (unknown scope)\n"
}

// migrateFromLegacyName walks memoryDir looking for files named "CLAUDE.md"
// and renames them to "MEMORY.md". Best-effort: errors are logged but do not
// fail the caller (an agent should not fail to start due to a migration issue).
func (f *SkeletonFactory) migrateFromLegacyName(ctx context.Context) {
	if f.memoryDir == "" {
		return
	}
	_ = filepath.WalkDir(f.memoryDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if d.Name() != "CLAUDE.md" {
			return nil
		}
		newPath := filepath.Join(filepath.Dir(path), "MEMORY.md")
		rel, relErr := filepath.Rel(f.memoryDir, path)
		if relErr != nil {
			slog.WarnContext(ctx, "memory migrate: cannot compute rel path", "path", path, "err", relErr)
			return nil
		}
		newRel, relErr := filepath.Rel(f.memoryDir, newPath)
		if relErr != nil {
			slog.WarnContext(ctx, "memory migrate: cannot compute new rel path", "path", newPath, "err", relErr)
			return nil
		}
		if _, statErr := os.Stat(newPath); statErr == nil {
			// MEMORY.md already exists — just remove the stale legacy file.
			if rmErr := os.Remove(path); rmErr != nil {
				slog.WarnContext(ctx, "memory migrate: remove legacy file failed", "path", path, "err", rmErr)
			}
			return nil
		}
		// Use git mv so history is preserved.
		if out, mvErr := f.gitops.run(ctx, "system:migrate", "system:migrate@agent-center.local",
			"mv", rel, newRel); mvErr != nil {
			slog.WarnContext(ctx, "memory migrate: git mv failed", "from", rel, "to", newRel, "err", mvErr, "out", out)
			return nil
		}
		if cmErr := f.gitops.AutoCommitDirty(ctx,
			"system:migrate", "system:migrate@agent-center.local",
			"migrate: rename CLAUDE.md → MEMORY.md"); cmErr != nil {
			slog.WarnContext(ctx, "memory migrate: commit failed", "err", cmErr)
		}
		return nil
	})
}

func scopeLabel(scope MemoryScope) string {
	switch scope.Kind {
	case MemScopeGlobal:
		return "global"
	case MemScopeSupervisor:
		return "supervisor"
	case MemScopeProject:
		return "project:" + scope.ProjectID
	case MemScopeTask:
		return "task:" + scope.Key
	case MemScopeIssue:
		return "issue:" + scope.Key
	case MemScopeConversation:
		return "conversation:" + scope.Key
	case MemScopeWorker:
		return "worker:" + scope.Key
	}
	return string(scope.Kind)
}
