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

func TestSkeleton_EnsureRootInit(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	g := memory.NewGitOps(dir, memory.NewExecGitRunner(), home)
	f := memory.NewSkeletonFactory(dir, g)
	ctx := context.Background()
	if err := f.EnsureRootInit(ctx); err != nil {
		t.Fatalf("init: %v", err)
	}
	// global + supervisor exist
	for _, name := range []string{"CLAUDE.md", "supervisor.md"} {
		p := filepath.Join(dir, name)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s missing: %v", name, err)
		}
	}
	// idempotent
	if err := f.EnsureRootInit(ctx); err != nil {
		t.Fatalf("re-init: %v", err)
	}
	log, _ := g.LogOneline(ctx)
	if !strings.Contains(log, "global") || !strings.Contains(log, "supervisor") {
		t.Errorf("log missing commits: %s", log)
	}
}

func TestSkeleton_CreateProjectAndTask(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	g := memory.NewGitOps(dir, memory.NewExecGitRunner(), home)
	f := memory.NewSkeletonFactory(dir, g)
	ctx := context.Background()
	if err := f.EnsureRootInit(ctx); err != nil {
		t.Fatal(err)
	}
	scopes := []memory.MemoryScope{
		{Kind: memory.MemScopeProject, ProjectID: "demo"},
		{Kind: memory.MemScopeTask, Key: "T-1", ProjectID: "demo"},
		{Kind: memory.MemScopeIssue, Key: "I-1", ProjectID: "demo"},
		{Kind: memory.MemScopeConversation, Key: "C-1"},
		{Kind: memory.MemScopeWorker, Key: "W-1"},
	}
	for _, s := range scopes {
		if err := f.CreateSkeleton(ctx, s); err != nil {
			t.Errorf("scope %+v: %v", s, err)
		}
	}
	// confirm files
	wantPaths := []string{
		"projects/demo/CLAUDE.md",
		"projects/demo/tasks/T-1/CLAUDE.md",
		"projects/demo/issues/I-1/CLAUDE.md",
		"conversations/C-1/CLAUDE.md",
		"workers/W-1/CLAUDE.md",
	}
	for _, p := range wantPaths {
		if _, err := os.Stat(filepath.Join(dir, p)); err != nil {
			t.Errorf("missing %s: %v", p, err)
		}
	}
	// idempotent create
	for _, s := range scopes {
		if err := f.CreateSkeleton(ctx, s); err != nil {
			t.Errorf("idempotent %+v: %v", s, err)
		}
	}
}

func TestSkeleton_PathTraversalRejected(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	g := memory.NewGitOps(dir, memory.NewExecGitRunner(), home)
	f := memory.NewSkeletonFactory(dir, g)
	ctx := context.Background()
	if err := f.EnsureRootInit(ctx); err != nil {
		t.Fatal(err)
	}
	bad := memory.MemoryScope{Kind: memory.MemScopeTask, Key: "../etc", ProjectID: "demo"}
	if err := f.CreateSkeleton(ctx, bad); err == nil {
		t.Fatal("expected path traversal err")
	}
}

func TestSkeleton_EmptyDir(t *testing.T) {
	f := memory.NewSkeletonFactory("", nil)
	if err := f.EnsureRootInit(context.Background()); !errors.Is(err, memory.ErrMemoryDirEmpty) {
		t.Fatalf("expected empty dir err, got %v", err)
	}
	if err := f.CreateSkeleton(context.Background(), memory.MemoryScope{Kind: memory.MemScopeGlobal}); !errors.Is(err, memory.ErrMemoryDirEmpty) {
		t.Fatalf("create empty: %v", err)
	}
}
