package workerdaemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/shim"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

func TestGC_NoExecBaseDir(t *testing.T) {
	g := NewGCSweeper("", time.Hour, clock.NewFakeClock(time.Now()), nil, nil, "W-1", nil)
	if _, err := g.Sweep(context.Background()); err == nil {
		t.Fatal("expected error")
	}
}

func TestGC_MissingDir(t *testing.T) {
	g := NewGCSweeper(filepath.Join(t.TempDir(), "missing"), time.Hour, clock.NewFakeClock(time.Now()), nil, nil, "W-1", nil)
	got, err := g.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty: %+v", got)
	}
}

func TestGC_SweepRemovesOldDir(t *testing.T) {
	root := t.TempDir()
	d, _ := shim.NewDir(root, "E-1")
	if err := d.WriteEnvelope([]byte(`{"workspace_mode":"direct","project_id":"P-1"}`)); err != nil {
		t.Fatal(err)
	}
	// Make the directory look old (set mtime back).
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(d.Path(), old, old); err != nil {
		t.Fatal(err)
	}
	released := []string{}
	g := NewGCSweeper(root, 24*time.Hour, clock.NewFakeClock(time.Now()), nil, nil, "W-1", func(id string, _ execution.WorkspaceMode) {
		released = append(released, id)
	})
	cleaned, err := g.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cleaned) != 1 || cleaned[0] != "E-1" {
		t.Fatalf("cleaned: %+v", cleaned)
	}
	if len(released) != 1 {
		t.Fatalf("released: %+v", released)
	}
	if d.Exists() {
		t.Fatal("expected removed")
	}
}

func TestGC_KeepsFreshDir(t *testing.T) {
	root := t.TempDir()
	d, _ := shim.NewDir(root, "E-1")
	if err := d.WriteEnvelope([]byte(`{"workspace_mode":"direct","project_id":"P-1"}`)); err != nil {
		t.Fatal(err)
	}
	g := NewGCSweeper(root, 24*time.Hour, clock.NewFakeClock(time.Now()), nil, nil, "W-1", nil)
	cleaned, err := g.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cleaned) != 0 {
		t.Fatalf("expected 0, got %d", len(cleaned))
	}
	if !d.Exists() {
		t.Fatal("expected kept")
	}
}

func TestParseWorkspaceModeAndProject(t *testing.T) {
	if _, ok := parseWorkspaceModeFromEnvelope([]byte(`{"workspace_mode":"worktree"}`)); !ok {
		t.Fatal("expected ok")
	}
	if _, ok := parseWorkspaceModeFromEnvelope([]byte(`{"workspace_mode":"garbage"}`)); ok {
		t.Fatal("expected invalid")
	}
	if _, ok := parseWorkspaceModeFromEnvelope([]byte("not-json")); ok {
		t.Fatal("expected bad json")
	}
	if _, ok := parseProjectIDFromEnvelope([]byte(`{"project_id":"P"}`)); !ok {
		t.Fatal("expected ok")
	}
	if _, ok := parseProjectIDFromEnvelope([]byte(`{}`)); ok {
		t.Fatal("expected missing")
	}
	if _, ok := parseProjectIDFromEnvelope([]byte("nope")); ok {
		t.Fatal("expected bad json")
	}
}
