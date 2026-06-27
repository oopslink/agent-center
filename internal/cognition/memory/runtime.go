package memory

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Engine is the runtime façade the agent CLI uses to manage its scoped memory:
// init the repo at startup, assemble scoped context for prompt injection
// (ancestor walk), write a scope's memory (commit), and sync dirty working-tree
// edits into commit history. It wires a GitOps + SkeletonFactory over a single
// memoryDir (the per-agent <home>/memory).
//
// W2 boundary: local commit only. Pushing the repo to the Center remote (W1) is
// deliberately out of scope here — Engine never calls GitOps.Push.
type Engine struct {
	memoryDir string
	gitops    *GitOps
	factory   *SkeletonFactory
}

// NewEngine wires an Engine over memoryDir. homeOverride pins git's HOME so the
// operator's ~/.gitconfig (gpgsign/hooks) cannot pollute memory commits; when
// empty, GitOps falls back to HOME=memoryDir (still isolated from the dev
// machine). Pass memoryDir == <agent home>/memory.
func NewEngine(memoryDir, homeOverride string) *Engine {
	g := NewGitOps(memoryDir, nil, homeOverride)
	return &Engine{
		memoryDir: memoryDir,
		gitops:    g,
		factory:   NewSkeletonFactory(memoryDir, g),
	}
}

// MemoryDir returns the absolute memory directory the engine manages.
func (e *Engine) MemoryDir() string { return e.memoryDir }

// GitLog returns the repo's `git log --oneline` (all refs) — used by tests and
// inspection to verify commit history. Empty repo (no commits) yields "".
func (e *Engine) GitLog(ctx context.Context) (string, error) {
	return e.gitops.LogOneline(ctx)
}

// EnsureRootInit makes memoryDir a git repo seeded with the global CLAUDE.md +
// supervisor.md skeletons. Idempotent — safe to call at every agent CLI startup.
func (e *Engine) EnsureRootInit(ctx context.Context) error {
	return e.factory.EnsureRootInit(ctx)
}

// AncestorScopes returns the scopes to consult for `scope`, ordered BROADEST →
// NARROWEST (global first, the scope itself last). Precedence runs the other
// way: the narrower a scope, the higher its priority, so when rendered into a
// prompt the most specific guidance is read last and overrides the general.
// supervisor.md is NOT part of any walk (cognition/02 §1: it is loaded
// explicitly) — use AssembleScoped's includeSupervisor flag for it.
func AncestorScopes(scope MemoryScope) []MemoryScope {
	global := MemoryScope{Kind: MemScopeGlobal}
	switch scope.Kind {
	case MemScopeProject:
		return []MemoryScope{global, scope}
	case MemScopeTask, MemScopeIssue:
		return []MemoryScope{
			global,
			{Kind: MemScopeProject, ProjectID: scope.ProjectID},
			scope,
		}
	case MemScopeConversation, MemScopeWorker:
		return []MemoryScope{global, scope}
	default:
		// global / supervisor / unknown → just the global root.
		return []MemoryScope{global}
	}
}

// AssembleScoped reads every existing memory file along scope's ancestor chain
// (missing files are skipped) and renders them into one injectable block,
// ordered broadest → narrowest. When includeSupervisor is set, supervisor.md
// (if present) is appended last as the supervisor's self-memory. Returns "" when
// no memory file exists anywhere on the chain, so callers can cheaply skip
// injection.
func (e *Engine) AssembleScoped(ctx context.Context, scope MemoryScope, includeSupervisor bool) (string, error) {
	chain := AncestorScopes(scope)
	if includeSupervisor {
		chain = append(chain, MemoryScope{Kind: MemScopeSupervisor})
	}
	var b strings.Builder
	n := 0
	for _, s := range chain {
		body, err := e.readScope(s)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(body) == "" {
			continue
		}
		if n == 0 {
			b.WriteString("<agent-memory>\n")
		}
		fmt.Fprintf(&b, "## scope: %s\n%s\n\n", scopeLabel(s), strings.TrimRight(body, "\n"))
		n++
	}
	if n == 0 {
		return "", nil
	}
	b.WriteString("</agent-memory>\n")
	return b.String(), nil
}

// HarnessContext renders the memory block injected into the agent CLI's
// append-system-prompt harness at launch: a short guide to the on-disk scoped
// layout (so the agent reads/writes the right CLAUDE.md via its own file tools)
// plus the always-relevant global + supervisor memory bodies. The runtime
// commits the agent's edits automatically (see CommitDirty), so the guide tells
// the agent to just edit the matching file. Never returns an error for a missing
// file (an empty repo yields the guide with no bodies).
func (e *Engine) HarnessContext(ctx context.Context) (string, error) {
	body, err := e.AssembleScoped(ctx, MemoryScope{Kind: MemScopeGlobal}, true)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("== Your memory ==\n")
	fmt.Fprintf(&b, "Your persistent memory is a git repo at %s — markdown, organised by scope:\n", e.memoryDir)
	b.WriteString("  CLAUDE.md                                          global (all your work)\n")
	b.WriteString("  supervisor.md                                      your self-memory\n")
	b.WriteString("  projects/<project_id>/CLAUDE.md                     project scope\n")
	b.WriteString("  projects/<project_id>/tasks/<task_id>/CLAUDE.md     task scope\n")
	b.WriteString("  projects/<project_id>/issues/<issue_id>/CLAUDE.md   issue scope\n")
	b.WriteString("  conversations/<conversation_id>/CLAUDE.md           conversation scope\n")
	b.WriteString("When you start a unit of work, consult the ancestor chain narrow→broad (task → project → global) and let the most specific notes win. Record durable lessons / skills / principles back into the MOST specific scope that fits by editing the matching CLAUDE.md with your file tools — the runtime commits your edits automatically. Never write outside this directory.\n")
	if body != "" {
		b.WriteString("\nCurrent global + supervisor memory:\n")
		b.WriteString(body)
	}
	return b.String(), nil
}

// WriteScoped writes content as the memory file for scope (mkdir -p) and commits
// it via GitOps under the given author. The path is containment-guarded
// (AbsPath blocks lexical "../" traversal; a runtime symlink-escape check blocks
// a directory/file under memory/ that links outside it). An empty author or
// message falls back to a system identity / default message.
func (e *Engine) WriteScoped(ctx context.Context, scope MemoryScope, content, authorName, authorEmail, message string) error {
	abs, err := e.containedAbs(scope)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return fmt.Errorf("memory: mkdir: %w", err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		return fmt.Errorf("memory: write: %w", err)
	}
	rel, err := ScopeToFSPath(scope)
	if err != nil {
		return err
	}
	if authorName == "" || authorEmail == "" {
		authorName, authorEmail = "system:bootstrap", "system:bootstrap@agent-center.local"
	}
	if message == "" {
		message = "update: memory for " + scopeLabel(scope)
	}
	return e.gitops.CommitFile(ctx, rel, authorName, authorEmail, message)
}

// CommitDirty stages and commits any dirty working-tree changes under memoryDir.
// The agent edits CLAUDE.md files directly via its file tools; this is the
// "memory sync" that turns those edits into commit history. No-op on a clean
// tree. W2 scope: LOCAL commit only — remote push (W1) is out.
func (e *Engine) CommitDirty(ctx context.Context, authorName, authorEmail, message string) error {
	if authorName == "" || authorEmail == "" {
		authorName, authorEmail = "system:memory-sync", "system:memory-sync@agent-center.local"
	}
	if message == "" {
		message = "memory: sync working tree"
	}
	return e.gitops.AutoCommitDirty(ctx, authorName, authorEmail, message)
}

// readScope returns the body of scope's memory file, or "" if it does not exist.
// It goes through the same containment + symlink-escape guard as writes, so a
// memory tree poisoned with a symlink pointing outside memoryDir is refused
// rather than silently read.
func (e *Engine) readScope(scope MemoryScope) (string, error) {
	abs, err := e.containedAbs(scope)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("memory: read %s: %w", scopeLabel(scope), err)
	}
	return string(data), nil
}

// containedAbs resolves scope's absolute path and guards against escape:
// AbsPath blocks lexical "../" traversal, and guardSymlinkEscape blocks a path
// whose deepest existing ancestor resolves (via symlinks) outside memoryDir.
func (e *Engine) containedAbs(scope MemoryScope) (string, error) {
	abs, err := AbsPath(e.memoryDir, scope)
	if err != nil {
		return "", err
	}
	if err := guardSymlinkEscape(e.memoryDir, abs); err != nil {
		return "", err
	}
	return abs, nil
}

// ErrMemoryPathEscapes is returned when a memory path resolves outside memoryDir
// through a symlink (the lexical guard lives in AbsPath; this is the runtime fs
// guard). Use errors.Is to test.
var ErrMemoryPathEscapes = errors.New("memory: path escapes memoryDir")

// guardSymlinkEscape walks from abs up to its deepest EXISTING ancestor,
// resolves that ancestor through symlinks, and refuses if the resolved real path
// is no longer inside memoryDir. This catches the case AbsPath cannot: a real
// directory (or the target file itself) under memory/ that is a symlink to
// somewhere outside the containment root. A not-yet-existing path is fine — its
// nearest existing parent is what gets checked.
func guardSymlinkEscape(memoryDir, abs string) error {
	realRoot, err := filepath.EvalSymlinks(memoryDir)
	if err != nil {
		// memoryDir may not exist at the very first init; fall back to its clean
		// lexical form for the prefix comparison.
		realRoot = filepath.Clean(memoryDir)
	}
	probe := filepath.Clean(abs)
	for {
		if _, statErr := os.Lstat(probe); statErr == nil {
			real, evalErr := filepath.EvalSymlinks(probe)
			if evalErr != nil {
				return fmt.Errorf("memory: resolve %q: %w", probe, evalErr)
			}
			real = filepath.Clean(real)
			if real != realRoot && !strings.HasPrefix(real, realRoot+string(filepath.Separator)) {
				return fmt.Errorf("%w: %q -> %q", ErrMemoryPathEscapes, abs, real)
			}
			return nil
		}
		parent := filepath.Dir(probe)
		if parent == probe {
			return nil // reached filesystem root without finding an existing ancestor
		}
		probe = parent
	}
}
