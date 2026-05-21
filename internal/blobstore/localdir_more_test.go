package blobstore_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/blobstore"
)

func TestLocalDir_Get_AbsPathRejected(t *testing.T) {
	s, _ := blobstore.NewLocalDir(t.TempDir())
	if _, err := s.Get(context.Background(), "/abs"); err == nil {
		t.Fatal("expected error for abs path")
	}
}

func TestLocalDir_Delete_AbsPathRejected(t *testing.T) {
	s, _ := blobstore.NewLocalDir(t.TempDir())
	if err := s.Delete(context.Background(), "/abs"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLocalDir_Exists_TraversalRejected(t *testing.T) {
	s, _ := blobstore.NewLocalDir(t.TempDir())
	if _, err := s.Exists(context.Background(), "../up"); err == nil {
		t.Fatal("expected error")
	}
}

func TestLocalDir_List_EmptyDir(t *testing.T) {
	s, _ := blobstore.NewLocalDir(t.TempDir())
	got, err := s.List(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty list, got %v", got)
	}
}

func TestLocalDir_List_SkipsTmpFiles(t *testing.T) {
	root := t.TempDir()
	s, _ := blobstore.NewLocalDir(root)
	// Create a .tmp file directly — simulating an interrupted Put.
	if err := os.WriteFile(filepath.Join(root, "abandoned.tmp"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = s.Put(context.Background(), "a", strings.NewReader("y"), 1)
	got, _ := s.List(context.Background(), "")
	for _, p := range got {
		if strings.HasSuffix(p, ".tmp") {
			t.Fatalf("List returned .tmp file: %v", got)
		}
	}
}

func TestLocalDir_Put_SizeUnknown_OK(t *testing.T) {
	s, _ := blobstore.NewLocalDir(t.TempDir())
	if err := s.Put(context.Background(), "a", bytes.NewReader([]byte("hi")), -1); err != nil {
		t.Fatal(err)
	}
	rc, _ := s.Get(context.Background(), "a")
	defer rc.Close()
}

func TestLocalDir_Get_FailsOnMissing(t *testing.T) {
	s, _ := blobstore.NewLocalDir(t.TempDir())
	_, err := s.Get(context.Background(), "missing/here")
	if !errors.Is(err, blobstore.ErrBlobNotFound) {
		t.Fatalf("expected ErrBlobNotFound, got %v", err)
	}
}
