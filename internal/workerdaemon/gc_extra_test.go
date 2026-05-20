package workerdaemon

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/shim"
	"github.com/oopslink/agent-center/internal/taskruntime/execution"
)

func TestNewGCSweeper_Defaults(t *testing.T) {
	g := NewGCSweeper("/x", 0, nil, nil, nil, "W-1", nil)
	if g.retention != 24*time.Hour {
		t.Fatalf("retention: %v", g.retention)
	}
	if g.clk == nil {
		t.Fatal("clk")
	}
}

func TestGC_ReleasesWorktreeOnExpiry(t *testing.T) {
	root := t.TempDir()
	d, _ := shim.NewDir(root, "E-1")
	if err := d.WriteEnvelope([]byte(`{"workspace_mode":"worktree","project_id":"p-1"}`)); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	_ = os.Chtimes(d.Path(), old, old)

	resolver := NewStaticMappingResolver()
	resolver.Set("W-1", "p-1", "/repo")
	ws := NewWorkspaceManager(&fakeGit{})

	releaseEvents := []execution.WorkspaceMode{}
	g := NewGCSweeper(root, 24*time.Hour, clock.NewFakeClock(time.Now()), ws, resolver, "W-1", func(_ string, m execution.WorkspaceMode) {
		releaseEvents = append(releaseEvents, m)
	})
	cleaned, err := g.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cleaned) != 1 || cleaned[0] != "E-1" {
		t.Fatalf("cleaned: %+v", cleaned)
	}
	if len(releaseEvents) != 1 || releaseEvents[0] != execution.WorkspaceWorktree {
		t.Fatalf("release: %+v", releaseEvents)
	}
}

func TestGC_FileEntriesIgnored(t *testing.T) {
	root := t.TempDir()
	// Create a regular file at root; sweep should skip.
	if err := os.WriteFile(root+"/not-a-dir", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := NewGCSweeper(root, time.Hour, clock.NewFakeClock(time.Now()), nil, nil, "W-1", nil)
	cleaned, err := g.Sweep(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(cleaned) != 0 {
		t.Fatalf("expected skip: %+v", cleaned)
	}
}
