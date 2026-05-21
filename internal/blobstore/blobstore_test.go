package blobstore_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/blobstore"
)

// newStore is a tiny factory the contract test uses to instantiate fresh
// store instances. Adding a second implementation (S3-compat) means
// adding a `t.Run("s3", ...)` block here.
func newLocalStore(t *testing.T) blobstore.BlobStore {
	t.Helper()
	s, err := blobstore.NewLocalDir(t.TempDir())
	if err != nil {
		t.Fatalf("NewLocalDir: %v", err)
	}
	return s
}

func runContract(t *testing.T, factory func(t *testing.T) blobstore.BlobStore) {
	t.Run("PutGetRoundTrip", func(t *testing.T) {
		s := factory(t)
		data := []byte("hello world")
		if err := s.Put(context.Background(), "tasks/T-1/log.log.gz", bytes.NewReader(data), int64(len(data))); err != nil {
			t.Fatalf("Put: %v", err)
		}
		r, err := s.Get(context.Background(), "tasks/T-1/log.log.gz")
		if err != nil {
			t.Fatalf("Get: %v", err)
		}
		got, err := io.ReadAll(r)
		_ = r.Close()
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, data) {
			t.Fatalf("content mismatch")
		}
	})
	t.Run("ExistsTrueAfterPut", func(t *testing.T) {
		s := factory(t)
		_ = s.Put(context.Background(), "a/b", strings.NewReader("x"), 1)
		ok, err := s.Exists(context.Background(), "a/b")
		if err != nil || !ok {
			t.Fatalf("Exists: ok=%v err=%v", ok, err)
		}
	})
	t.Run("ExistsFalseMissing", func(t *testing.T) {
		s := factory(t)
		ok, err := s.Exists(context.Background(), "missing")
		if err != nil || ok {
			t.Fatalf("Exists missing: ok=%v err=%v", ok, err)
		}
	})
	t.Run("GetNotFound", func(t *testing.T) {
		s := factory(t)
		_, err := s.Get(context.Background(), "nope")
		if !errors.Is(err, blobstore.ErrBlobNotFound) {
			t.Fatalf("expected ErrBlobNotFound, got %v", err)
		}
	})
	t.Run("Delete", func(t *testing.T) {
		s := factory(t)
		_ = s.Put(context.Background(), "x", strings.NewReader("y"), 1)
		if err := s.Delete(context.Background(), "x"); err != nil {
			t.Fatalf("Delete: %v", err)
		}
		ok, _ := s.Exists(context.Background(), "x")
		if ok {
			t.Fatal("post-delete still exists")
		}
		if err := s.Delete(context.Background(), "x"); !errors.Is(err, blobstore.ErrBlobNotFound) {
			t.Fatalf("expected ErrBlobNotFound on second Delete, got %v", err)
		}
	})
	t.Run("List", func(t *testing.T) {
		s := factory(t)
		_ = s.Put(context.Background(), "tasks/T-1/log.log.gz", strings.NewReader("x"), 1)
		_ = s.Put(context.Background(), "tasks/T-2/log.log.gz", strings.NewReader("y"), 1)
		_ = s.Put(context.Background(), "tasks/T-1/trace.jsonl.gz", strings.NewReader("z"), 1)
		got, err := s.List(context.Background(), "tasks")
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("expected 3, got %d: %v", len(got), got)
		}
	})
	t.Run("URL", func(t *testing.T) {
		s := factory(t)
		u := s.URL("tasks/T-1/log.log.gz")
		if u == "" {
			t.Fatal("empty URL")
		}
	})
	t.Run("RelPathValidation", func(t *testing.T) {
		s := factory(t)
		// Absolute and traversal should be rejected.
		for _, p := range []string{"/abs", "a/../b", ""} {
			if err := s.Put(context.Background(), p, strings.NewReader("x"), 1); err == nil {
				t.Errorf("expected validation error for %q", p)
			}
		}
	})
	t.Run("OverwriteAllowed", func(t *testing.T) {
		s := factory(t)
		_ = s.Put(context.Background(), "k", strings.NewReader("v1"), 2)
		_ = s.Put(context.Background(), "k", strings.NewReader("v2"), 2)
		r, _ := s.Get(context.Background(), "k")
		got, _ := io.ReadAll(r)
		_ = r.Close()
		if string(got) != "v2" {
			t.Fatalf("overwrite failed: %q", got)
		}
	})
	t.Run("SizeMismatch", func(t *testing.T) {
		s := factory(t)
		err := s.Put(context.Background(), "k2", strings.NewReader("hello"), 100)
		if err == nil {
			t.Fatal("expected size mismatch error")
		}
	})
}

func TestBlobStore_Contract_LocalDir(t *testing.T) {
	runContract(t, newLocalStore)
}

func TestLocalDir_MaxBytes(t *testing.T) {
	s, err := blobstore.NewLocalDir(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s = s.WithMaxBytes(10)
	// Declared size > limit fails fast.
	if err := s.Put(context.Background(), "big", strings.NewReader("xxxxxxxxxxxxxxxxxxxxxxxxxx"), 100); !errors.Is(err, blobstore.ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge, got %v", err)
	}
	// Unknown size, runtime > limit fails too.
	if err := s.Put(context.Background(), "big2", strings.NewReader("xxxxxxxxxxxxxxxxxxxxxxxxxx"), -1); !errors.Is(err, blobstore.ErrPayloadTooLarge) {
		t.Fatalf("expected ErrPayloadTooLarge runtime, got %v", err)
	}
}

func TestLocalDir_EmptyRootRejected(t *testing.T) {
	if _, err := blobstore.NewLocalDir(""); err == nil {
		t.Fatal("expected error for empty root")
	}
}
