package memory_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition/memory"
)

// newEngine wires an Engine over a fresh tempdir with an isolated git HOME so the
// dev machine's ~/.gitconfig cannot pollute the commits.
func newEngine(t *testing.T) (*memory.Engine, string) {
	t.Helper()
	dir := t.TempDir()
	home := t.TempDir()
	return memory.NewEngine(dir, home), dir
}

func TestAncestorScopes_TaskChain(t *testing.T) {
	got := memory.AncestorScopes(memory.MemoryScope{
		Kind: memory.MemScopeTask, Key: "T1", ProjectID: "P1",
	})
	// broad → narrow: global, project, task.
	if len(got) != 3 {
		t.Fatalf("want 3 scopes, got %d: %+v", len(got), got)
	}
	if got[0].Kind != memory.MemScopeGlobal {
		t.Errorf("scope[0] = %v, want global", got[0].Kind)
	}
	if got[1].Kind != memory.MemScopeProject || got[1].ProjectID != "P1" {
		t.Errorf("scope[1] = %+v, want project P1", got[1])
	}
	if got[2].Kind != memory.MemScopeTask || got[2].Key != "T1" || got[2].ProjectID != "P1" {
		t.Errorf("scope[2] = %+v, want task T1/P1", got[2])
	}
}

func TestAncestorScopes_IssueAndConversation(t *testing.T) {
	iss := memory.AncestorScopes(memory.MemoryScope{Kind: memory.MemScopeIssue, Key: "I1", ProjectID: "P1"})
	if len(iss) != 3 || iss[1].Kind != memory.MemScopeProject || iss[2].Kind != memory.MemScopeIssue {
		t.Errorf("issue chain wrong: %+v", iss)
	}
	conv := memory.AncestorScopes(memory.MemoryScope{Kind: memory.MemScopeConversation, Key: "C1"})
	// conversation has no project ancestor: global → conversation.
	if len(conv) != 2 || conv[0].Kind != memory.MemScopeGlobal || conv[1].Kind != memory.MemScopeConversation {
		t.Errorf("conversation chain wrong: %+v", conv)
	}
}

// TestAssembleScoped_AncestorWalk is the headline read-path test: writes global,
// project, and task memory, then proves AssembleScoped returns all three in
// broad→narrow order, skips a missing middle scope, and renders nothing when the
// whole chain is empty.
func TestAssembleScoped_AncestorWalk(t *testing.T) {
	e, _ := newEngine(t)
	ctx := context.Background()
	if err := e.EnsureRootInit(ctx); err != nil {
		t.Fatalf("EnsureRootInit: %v", err)
	}

	task := memory.MemoryScope{Kind: memory.MemScopeTask, Key: "T1", ProjectID: "P1"}
	project := memory.MemoryScope{Kind: memory.MemScopeProject, ProjectID: "P1"}
	global := memory.MemoryScope{Kind: memory.MemScopeGlobal}

	if err := e.WriteScoped(ctx, global, "GLOBAL-NOTE\n", "n", "n@x", "g"); err != nil {
		t.Fatalf("write global: %v", err)
	}
	if err := e.WriteScoped(ctx, project, "PROJECT-NOTE\n", "n", "n@x", "p"); err != nil {
		t.Fatalf("write project: %v", err)
	}
	if err := e.WriteScoped(ctx, task, "TASK-NOTE\n", "n", "n@x", "t"); err != nil {
		t.Fatalf("write task: %v", err)
	}

	out, err := e.AssembleScoped(ctx, task, false)
	if err != nil {
		t.Fatalf("AssembleScoped: %v", err)
	}
	for _, want := range []string{"GLOBAL-NOTE", "PROJECT-NOTE", "TASK-NOTE", "<agent-memory>", "</agent-memory>"} {
		if !strings.Contains(out, want) {
			t.Errorf("assembled output missing %q:\n%s", want, out)
		}
	}
	// Order: global before project before task (broad → narrow).
	gi := strings.Index(out, "GLOBAL-NOTE")
	pi := strings.Index(out, "PROJECT-NOTE")
	ti := strings.Index(out, "TASK-NOTE")
	if !(gi < pi && pi < ti) {
		t.Errorf("order wrong: global=%d project=%d task=%d\n%s", gi, pi, ti, out)
	}

	// Remove the middle (project) scope file: assemble must skip it gracefully.
	pPath := filepath.Join(e.MemoryDir(), "projects", "P1", "CLAUDE.md")
	if err := os.Remove(pPath); err != nil {
		t.Fatalf("rm project file: %v", err)
	}
	out2, err := e.AssembleScoped(ctx, task, false)
	if err != nil {
		t.Fatalf("AssembleScoped after rm: %v", err)
	}
	if strings.Contains(out2, "PROJECT-NOTE") {
		t.Errorf("expected project scope skipped, got:\n%s", out2)
	}
	if !strings.Contains(out2, "GLOBAL-NOTE") || !strings.Contains(out2, "TASK-NOTE") {
		t.Errorf("expected global+task still present:\n%s", out2)
	}
}

func TestAssembleScoped_EmptyChainReturnsEmpty(t *testing.T) {
	e, _ := newEngine(t)
	ctx := context.Background()
	// No EnsureRootInit, no files: nothing on the chain → "" (caller skips inject).
	out, err := e.AssembleScoped(ctx, memory.MemoryScope{Kind: memory.MemScopeTask, Key: "T1", ProjectID: "P1"}, false)
	if err != nil {
		t.Fatalf("AssembleScoped: %v", err)
	}
	if out != "" {
		t.Errorf("want empty, got %q", out)
	}
}

func TestAssembleScoped_IncludesSupervisor(t *testing.T) {
	e, _ := newEngine(t)
	ctx := context.Background()
	if err := e.EnsureRootInit(ctx); err != nil {
		t.Fatalf("EnsureRootInit: %v", err)
	}
	if err := e.WriteScoped(ctx, memory.MemoryScope{Kind: memory.MemScopeSupervisor}, "SUPER-SELF\n", "n", "n@x", "s"); err != nil {
		t.Fatalf("write supervisor: %v", err)
	}
	out, err := e.AssembleScoped(ctx, memory.MemoryScope{Kind: memory.MemScopeGlobal}, true)
	if err != nil {
		t.Fatalf("AssembleScoped: %v", err)
	}
	if !strings.Contains(out, "SUPER-SELF") {
		t.Errorf("supervisor memory not included:\n%s", out)
	}
}

// TestWriteScoped_CommitsHistory is the headline write-path test: a scoped write
// produces a real commit (git log shows it), and a second write to the same
// scope appends another commit.
func TestWriteScoped_CommitsHistory(t *testing.T) {
	e, _ := newEngine(t)
	ctx := context.Background()
	if err := e.EnsureRootInit(ctx); err != nil {
		t.Fatalf("EnsureRootInit: %v", err)
	}
	task := memory.MemoryScope{Kind: memory.MemScopeTask, Key: "T9", ProjectID: "P9"}
	if err := e.WriteScoped(ctx, task, "v1\n", "dev1", "dev1@agent-center", "memory: lesson A"); err != nil {
		t.Fatalf("write v1: %v", err)
	}
	if err := e.WriteScoped(ctx, task, "v1\nv2\n", "dev1", "dev1@agent-center", "memory: lesson B"); err != nil {
		t.Fatalf("write v2: %v", err)
	}
	log, err := e.GitLog(ctx)
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(log, "memory: lesson A") || !strings.Contains(log, "memory: lesson B") {
		t.Errorf("commit history missing writes:\n%s", log)
	}
	// File holds the latest content.
	got, err := os.ReadFile(filepath.Join(e.MemoryDir(), "projects", "P9", "tasks", "T9", "CLAUDE.md"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "v1\nv2\n" {
		t.Errorf("file content = %q, want v1\\nv2\\n", got)
	}
}

func TestCommitDirty_TurnsAgentEditsIntoCommits(t *testing.T) {
	e, _ := newEngine(t)
	ctx := context.Background()
	if err := e.EnsureRootInit(ctx); err != nil {
		t.Fatalf("EnsureRootInit: %v", err)
	}
	// Simulate the agent editing a CLAUDE.md directly via its file tools.
	p := filepath.Join(e.MemoryDir(), "projects", "PX", "tasks", "TX")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(p, "CLAUDE.md"), []byte("# agent wrote this\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := e.CommitDirty(ctx, "dev1", "dev1@agent-center", "memory: sync"); err != nil {
		t.Fatalf("CommitDirty: %v", err)
	}
	log, err := e.GitLog(ctx)
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(log, "memory: sync") {
		t.Errorf("dirty edit not committed:\n%s", log)
	}
	// A second CommitDirty on a clean tree is a no-op (no error).
	if err := e.CommitDirty(ctx, "dev1", "dev1@agent-center", "memory: sync"); err != nil {
		t.Fatalf("CommitDirty on clean tree: %v", err)
	}
}

// TestContainment_SymlinkEscapeRejected proves the runtime fs guard: a scope dir
// under memory/ that is a symlink pointing OUTSIDE memoryDir is refused for both
// read and write (AbsPath's lexical guard cannot catch this — only the symlink
// resolution can).
func TestContainment_SymlinkEscapeRejected(t *testing.T) {
	e, _ := newEngine(t)
	ctx := context.Background()
	if err := e.EnsureRootInit(ctx); err != nil {
		t.Fatalf("EnsureRootInit: %v", err)
	}
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("TOP-SECRET\n"), 0o644); err != nil {
		t.Fatalf("seed outside: %v", err)
	}
	// projects/EVIL -> <outside> (a symlink escaping the containment root).
	projectsDir := filepath.Join(e.MemoryDir(), "projects")
	if err := os.MkdirAll(projectsDir, 0o755); err != nil {
		t.Fatalf("mkdir projects: %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(projectsDir, "EVIL")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	// A project scope whose dir resolves through the EVIL symlink must be refused.
	evil := memory.MemoryScope{Kind: memory.MemScopeProject, ProjectID: "EVIL"}
	if err := e.WriteScoped(ctx, evil, "x", "n", "n@x", "m"); !errors.Is(err, memory.ErrMemoryPathEscapes) {
		t.Errorf("write through symlink: want ErrMemoryPathEscapes, got %v", err)
	}
	// Reading via the assembler must also refuse (escape surfaces as an error, not
	// a silent read of the outside file).
	if _, err := e.AssembleScoped(ctx, memory.MemoryScope{Kind: memory.MemScopeTask, Key: "T", ProjectID: "EVIL"}, false); !errors.Is(err, memory.ErrMemoryPathEscapes) {
		t.Errorf("read through symlink: want ErrMemoryPathEscapes, got %v", err)
	}
}

func TestAncestorScopes_WorkerAndProject(t *testing.T) {
	w := memory.AncestorScopes(memory.MemoryScope{Kind: memory.MemScopeWorker, Key: "W1"})
	if len(w) != 2 || w[0].Kind != memory.MemScopeGlobal || w[1].Kind != memory.MemScopeWorker {
		t.Errorf("worker chain wrong: %+v", w)
	}
	p := memory.AncestorScopes(memory.MemoryScope{Kind: memory.MemScopeProject, ProjectID: "P1"})
	if len(p) != 2 || p[0].Kind != memory.MemScopeGlobal || p[1].Kind != memory.MemScopeProject {
		t.Errorf("project chain wrong: %+v", p)
	}
	// global/supervisor degenerate to the global root only.
	g := memory.AncestorScopes(memory.MemoryScope{Kind: memory.MemScopeSupervisor})
	if len(g) != 1 || g[0].Kind != memory.MemScopeGlobal {
		t.Errorf("supervisor chain wrong: %+v", g)
	}
}

// TestHarnessContext renders the launch memory block: the scoped-layout guide is
// always present, and global + supervisor bodies appear once written.
func TestHarnessContext(t *testing.T) {
	e, _ := newEngine(t)
	ctx := context.Background()
	if err := e.EnsureRootInit(ctx); err != nil {
		t.Fatalf("EnsureRootInit: %v", err)
	}
	// Even with only skeletons, the guide + memoryDir path are present.
	out, err := e.HarnessContext(ctx)
	if err != nil {
		t.Fatalf("HarnessContext: %v", err)
	}
	for _, want := range []string{"== Your memory ==", e.MemoryDir(), "tasks/<task_id>/CLAUDE.md", "ancestor chain"} {
		if !strings.Contains(out, want) {
			t.Errorf("harness context missing %q:\n%s", want, out)
		}
	}
	// Write real global + supervisor memory; both bodies must surface.
	if err := e.WriteScoped(ctx, memory.MemoryScope{Kind: memory.MemScopeGlobal}, "GLOBAL-BRAIN\n", "n", "n@x", "g"); err != nil {
		t.Fatalf("write global: %v", err)
	}
	if err := e.WriteScoped(ctx, memory.MemoryScope{Kind: memory.MemScopeSupervisor}, "SELF-NOTE\n", "n", "n@x", "s"); err != nil {
		t.Fatalf("write supervisor: %v", err)
	}
	out2, err := e.HarnessContext(ctx)
	if err != nil {
		t.Fatalf("HarnessContext: %v", err)
	}
	if !strings.Contains(out2, "GLOBAL-BRAIN") || !strings.Contains(out2, "SELF-NOTE") {
		t.Errorf("harness context missing memory bodies:\n%s", out2)
	}
}

// TestWriteAndCommit_DefaultIdentityFallback exercises the empty-author/message
// fallbacks in WriteScoped + CommitDirty (system identity, default message).
func TestWriteAndCommit_DefaultIdentityFallback(t *testing.T) {
	e, _ := newEngine(t)
	ctx := context.Background()
	if err := e.EnsureRootInit(ctx); err != nil {
		t.Fatalf("EnsureRootInit: %v", err)
	}
	// Empty author + empty message → system:bootstrap identity + default message.
	if err := e.WriteScoped(ctx, memory.MemoryScope{Kind: memory.MemScopeProject, ProjectID: "PD"}, "x\n", "", "", ""); err != nil {
		t.Fatalf("WriteScoped fallback: %v", err)
	}
	// Dirty edit + empty author/message on CommitDirty → system:memory-sync.
	if err := os.WriteFile(filepath.Join(e.MemoryDir(), "CLAUDE.md"), []byte("# edited\n"), 0o644); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if err := e.CommitDirty(ctx, "", "", ""); err != nil {
		t.Fatalf("CommitDirty fallback: %v", err)
	}
	log, err := e.GitLog(ctx)
	if err != nil {
		t.Fatalf("git log: %v", err)
	}
	if !strings.Contains(log, "update: memory for project:PD") || !strings.Contains(log, "memory: sync working tree") {
		t.Errorf("default messages missing from history:\n%s", log)
	}
}

// TestContainment_LexicalTraversalRejected confirms the AbsPath lexical guard
// still rejects "../" style components surfaced via the scope key.
func TestContainment_LexicalTraversalRejected(t *testing.T) {
	e, _ := newEngine(t)
	ctx := context.Background()
	bad := memory.MemoryScope{Kind: memory.MemScopeProject, ProjectID: "../escape"}
	if err := e.WriteScoped(ctx, bad, "x", "n", "n@x", "m"); err == nil {
		t.Errorf("expected rejection of '../escape' project id")
	}
}
