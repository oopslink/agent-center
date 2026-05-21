package memory

import (
	"context"
	"errors"
	"testing"
)

func TestGitOps_LogOneline_EmptyMemoryDir(t *testing.T) {
	g := NewGitOps("", nil, "")
	if _, err := g.LogOneline(context.Background()); !errors.Is(err, ErrMemoryDirEmpty) {
		t.Errorf("expected ErrMemoryDirEmpty, got %v", err)
	}
}

func TestGitOps_LogOneline_NoCommitsYet(t *testing.T) {
	dir := t.TempDir()
	home := t.TempDir()
	g := NewGitOps(dir, NewExecGitRunner(), home)
	if err := g.Init(context.Background()); err != nil {
		t.Fatal(err)
	}
	// no commits yet — log should return empty, no error
	out, err := g.LogOneline(context.Background())
	if err != nil {
		t.Errorf("err: %v", err)
	}
	_ = out
}

func TestSafeDefaultPath(t *testing.T) {
	if safeDefaultPath() == "" {
		t.Error("path empty")
	}
}
