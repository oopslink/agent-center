// Package memory implements the Memory AR (file + git) for the Cognition
// BC. Per plan-6 § 3.3 and cognition/02-memory.
//
// Memory is the supervisor's persistent brain — markdown files committed
// via real git, stored under $AGENT_CENTER_MEMORY_DIR. Each invocation /
// scope walks an ancestor chain of CLAUDE.md files; supervisor.md sits at
// the root and is loaded explicitly (not via ancestor walk).
package memory

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// MemoryScopeKind extends cognition.ScopeKind with Memory-only scopes that
// the Invocation AR does not use (project + supervisor; cognition/02 § 1).
type MemoryScopeKind string

const (
	MemScopeGlobal       MemoryScopeKind = "global"
	MemScopeProject      MemoryScopeKind = "project"
	MemScopeTask         MemoryScopeKind = "task"
	MemScopeIssue        MemoryScopeKind = "issue"
	MemScopeConversation MemoryScopeKind = "conversation"
	MemScopeWorker       MemoryScopeKind = "worker"
	MemScopeSupervisor   MemoryScopeKind = "supervisor"
)

// MemoryScope identifies a memory file. Parent IDs (e.g. project_id for
// task/issue) are passed in explicitly — we do NOT reverse-query the DB
// (cognition/02 § 3.2: skeleton subscriber receives parent id in the
// event payload).
type MemoryScope struct {
	Kind      MemoryScopeKind
	Key       string // task_id / issue_id / ... ; empty for global+supervisor
	ProjectID string // required for task / issue / project
}

// ScopeToFSPath returns the relative path (within MemoryDir) of the
// CLAUDE.md / supervisor.md file for the given scope. Path-traversal
// validation runs against the constructed components.
//
// Layout (cognition/02 § 1):
//
//	global       → CLAUDE.md
//	project:X    → projects/X/CLAUDE.md
//	task:T (in X) → projects/X/tasks/T/CLAUDE.md
//	issue:I (in X) → projects/X/issues/I/CLAUDE.md
//	conversation:C → conversations/C/CLAUDE.md
//	worker:W      → workers/W/CLAUDE.md
//	supervisor    → supervisor.md
func ScopeToFSPath(scope MemoryScope) (string, error) {
	if err := validateScopeComponents(scope); err != nil {
		return "", err
	}
	switch scope.Kind {
	case MemScopeGlobal:
		return "CLAUDE.md", nil
	case MemScopeProject:
		return filepath.ToSlash(filepath.Join("projects", scope.ProjectID, "CLAUDE.md")), nil
	case MemScopeTask:
		return filepath.ToSlash(filepath.Join("projects", scope.ProjectID, "tasks", scope.Key, "CLAUDE.md")), nil
	case MemScopeIssue:
		return filepath.ToSlash(filepath.Join("projects", scope.ProjectID, "issues", scope.Key, "CLAUDE.md")), nil
	case MemScopeConversation:
		return filepath.ToSlash(filepath.Join("conversations", scope.Key, "CLAUDE.md")), nil
	case MemScopeWorker:
		return filepath.ToSlash(filepath.Join("workers", scope.Key, "CLAUDE.md")), nil
	case MemScopeSupervisor:
		return "supervisor.md", nil
	default:
		return "", fmt.Errorf("memory: unknown scope_kind %q", scope.Kind)
	}
}

// ScopeToDirPath returns the parent directory containing the CLAUDE.md
// file (used by mkdir -p before write). For global / supervisor scopes
// it returns "." (root).
func ScopeToDirPath(scope MemoryScope) (string, error) {
	fsPath, err := ScopeToFSPath(scope)
	if err != nil {
		return "", err
	}
	d := filepath.Dir(fsPath)
	if d == "" || d == "/" {
		return ".", nil
	}
	return filepath.ToSlash(d), nil
}

func validateScopeComponents(scope MemoryScope) error {
	switch scope.Kind {
	case MemScopeGlobal, MemScopeSupervisor:
		// no Key / ProjectID required
		return nil
	case MemScopeProject:
		return validatePathComponent("project_id", scope.ProjectID)
	case MemScopeTask, MemScopeIssue:
		if err := validatePathComponent("project_id", scope.ProjectID); err != nil {
			return err
		}
		return validatePathComponent("key", scope.Key)
	case MemScopeConversation, MemScopeWorker:
		return validatePathComponent("key", scope.Key)
	default:
		return fmt.Errorf("memory: unknown scope_kind %q", scope.Kind)
	}
}

// validatePathComponent rejects any value that could escape MemoryDir.
//   - non-empty
//   - no path separators (/ \ :)
//   - no leading dot run (".") or ".."
//   - no null byte
//   - no relative-path tricks
//   - length capped at 128 (prevents pathological filesystem behaviour)
func validatePathComponent(name, v string) error {
	if v == "" {
		return fmt.Errorf("memory: %s required", name)
	}
	if len(v) > 128 {
		return fmt.Errorf("memory: %s %q too long (max 128)", name, v)
	}
	if strings.ContainsAny(v, "/\\:") {
		return fmt.Errorf("memory: %s %q contains forbidden path separator", name, v)
	}
	if strings.Contains(v, "\x00") {
		return fmt.Errorf("memory: %s %q contains null byte", name, v)
	}
	if v == "." || v == ".." {
		return fmt.Errorf("memory: %s %q is a reserved path component", name, v)
	}
	if strings.Contains(v, "..") {
		return fmt.Errorf("memory: %s %q contains '..' traversal", name, v)
	}
	// disallow leading dot to avoid hidden files / dotfile attacks
	if strings.HasPrefix(v, ".") {
		return fmt.Errorf("memory: %s %q must not start with '.'", name, v)
	}
	return nil
}

// AbsPath joins memoryDir with the scope's FS path. memoryDir is rejected
// when empty; it must be an absolute path so we can pin the resulting
// path inside it.
func AbsPath(memoryDir string, scope MemoryScope) (string, error) {
	if memoryDir == "" {
		return "", errors.New("memory: memoryDir required")
	}
	rel, err := ScopeToFSPath(scope)
	if err != nil {
		return "", err
	}
	abs := filepath.Join(memoryDir, filepath.FromSlash(rel))
	// Belt + suspenders: make sure the joined path is still inside memoryDir.
	cleanRoot := filepath.Clean(memoryDir)
	cleanAbs := filepath.Clean(abs)
	if !strings.HasPrefix(cleanAbs, cleanRoot+string(filepath.Separator)) && cleanAbs != cleanRoot {
		return "", fmt.Errorf("memory: path %q escapes memoryDir %q", cleanAbs, cleanRoot)
	}
	return abs, nil
}
