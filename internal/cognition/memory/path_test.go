package memory_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition/memory"
)

func TestScopeToFSPath_AllScopes(t *testing.T) {
	cases := []struct {
		name  string
		scope memory.MemoryScope
		want  string
	}{
		{"global", memory.MemoryScope{Kind: memory.MemScopeGlobal}, "MEMORY.md"},
		{"supervisor", memory.MemoryScope{Kind: memory.MemScopeSupervisor}, "supervisor.md"},
		{"project", memory.MemoryScope{Kind: memory.MemScopeProject, ProjectID: "demo"}, "projects/demo/MEMORY.md"},
		{"task", memory.MemoryScope{Kind: memory.MemScopeTask, Key: "T-1", ProjectID: "demo"}, "projects/demo/tasks/T-1/MEMORY.md"},
		{"issue", memory.MemoryScope{Kind: memory.MemScopeIssue, Key: "I-7", ProjectID: "demo"}, "projects/demo/issues/I-7/MEMORY.md"},
		{"conversation", memory.MemoryScope{Kind: memory.MemScopeConversation, Key: "C-3"}, "conversations/C-3/MEMORY.md"},
		{"worker", memory.MemoryScope{Kind: memory.MemScopeWorker, Key: "W-1"}, "workers/W-1/MEMORY.md"},
	}
	for _, tc := range cases {
		got, err := memory.ScopeToFSPath(tc.scope)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestScopeToDirPath(t *testing.T) {
	d, err := memory.ScopeToDirPath(memory.MemoryScope{Kind: memory.MemScopeTask, Key: "T-1", ProjectID: "demo"})
	if err != nil {
		t.Fatal(err)
	}
	if d != "projects/demo/tasks/T-1" {
		t.Errorf("got %q", d)
	}
	d, err = memory.ScopeToDirPath(memory.MemoryScope{Kind: memory.MemScopeGlobal})
	if err != nil {
		t.Fatal(err)
	}
	if d != "." {
		t.Errorf("global got %q", d)
	}
}

func TestScopeToFSPath_PathTraversal(t *testing.T) {
	bad := []memory.MemoryScope{
		{Kind: memory.MemScopeTask, Key: "../etc", ProjectID: "demo"},
		{Kind: memory.MemScopeTask, Key: "T-1", ProjectID: ".."},
		{Kind: memory.MemScopeTask, Key: "T-1", ProjectID: "/abs/path"},
		{Kind: memory.MemScopeTask, Key: "T-1", ProjectID: "x\x00y"},
		{Kind: memory.MemScopeProject, ProjectID: "demo/sub"},
		{Kind: memory.MemScopeProject, ProjectID: ".hidden"},
		{Kind: memory.MemScopeProject, ProjectID: ""},
		{Kind: memory.MemScopeConversation, Key: ""},
		{Kind: memory.MemoryScopeKind("bogus")},
	}
	for _, s := range bad {
		if _, err := memory.ScopeToFSPath(s); err == nil {
			t.Errorf("expected error for %+v", s)
		}
	}
}

func TestScopeToFSPath_TooLong(t *testing.T) {
	long := strings.Repeat("a", 129)
	if _, err := memory.ScopeToFSPath(memory.MemoryScope{Kind: memory.MemScopeProject, ProjectID: long}); err == nil {
		t.Fatal("expected too-long error")
	}
}

func TestAbsPath_StaysInsideRoot(t *testing.T) {
	root := t.TempDir()
	abs, err := memory.AbsPath(root, memory.MemoryScope{Kind: memory.MemScopeTask, Key: "T-1", ProjectID: "demo"})
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	if !strings.HasPrefix(abs, root+string(filepath.Separator)) {
		t.Errorf("abs %q outside root %q", abs, root)
	}
}

func TestAbsPath_EmptyRoot(t *testing.T) {
	if _, err := memory.AbsPath("", memory.MemoryScope{Kind: memory.MemScopeGlobal}); err == nil {
		t.Error("expected err")
	}
}
