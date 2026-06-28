package executor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateExecutorID(t *testing.T) {
	bad := []string{"", "  ", "a/b", "a\\b", "a:b", "a\x00b", ".", "..", "a..b"}
	for _, v := range bad {
		if err := validateExecutorID(v); err == nil {
			t.Errorf("validateExecutorID(%q) should fail", v)
		}
	}
	for _, v := range []string{"exec-abc123", "e1", "01ARZ3NDEKTSV4RRFFQ69G5FAV"} {
		if err := validateExecutorID(v); err != nil {
			t.Errorf("validateExecutorID(%q) unexpected err: %v", v, err)
		}
	}
}

func TestPathWithinRoot(t *testing.T) {
	if !pathWithinRoot("/a", "/a") {
		t.Error("root itself should be within")
	}
	if !pathWithinRoot("/a", "/a/b") {
		t.Error("child should be within")
	}
	if pathWithinRoot("/a", "/ab") {
		t.Error("sibling sharing a string prefix must NOT be within")
	}
}

// TestBadIDRejectedEverywhere drives every public op with a traversal id, covering
// the validateExecutorID error returns threaded through each path helper.
func TestBadIDRejectedEverywhere(t *testing.T) {
	fx, _ := newFx(t)
	const bad = "../x"
	ops := map[string]func() error{
		"Provision":      func() error { _, e := fx.Provision(bad); return e },
		"WriteInput":     func() error { in := validInput(); in.ExecutorID = bad; return fx.WriteInput(in) },
		"ReadInput":      func() error { _, e := fx.ReadInput(bad); return e },
		"ReadOutput":     func() error { _, e := fx.ReadOutput(bad); return e },
		"ReadStatus":     func() error { _, e := fx.ReadStatus(bad); return e },
		"ReadProgress":   func() error { _, e := fx.ReadProgress(bad); return e },
		"AppendProgress": func() error { return fx.AppendProgress(bad, ProgressEntry{At: testNow, Message: "m"}) },
		"WriteStatus":    func() error { return fx.WriteStatus(Status{ExecutorID: bad, State: StateRunning, StartedAt: testNow}) },
		"WriteOutput":    func() error { return fx.WriteOutput(Output{ExecutorID: bad, Success: true, FinishedAt: testNow}) },
		"ContainedPath":  func() error { _, e := fx.ContainedPath(bad, "f", true); return e },
		"Remove":         func() error { return fx.Remove(bad) },
		"Dir":            func() error { _, e := fx.layout.Dir(bad); return e },
		"WorkspaceDir":   func() error { _, e := fx.layout.WorkspaceDir(bad); return e },
	}
	for name, fn := range ops {
		if err := fn(); err == nil {
			t.Errorf("%s should reject a traversal id", name)
		}
	}
}

func TestWriteJSONAtomic_Errors(t *testing.T) {
	// Marshal failure (a channel cannot be JSON-encoded).
	if err := writeJSONAtomic(filepath.Join(t.TempDir(), "x.json"), make(chan int)); err == nil {
		t.Error("writeJSONAtomic should surface a marshal error")
	}
	// MkdirAll failure: the parent path is a regular file, so creating a subdir
	// under it cannot succeed.
	root := t.TempDir()
	fileParent := filepath.Join(root, "afile")
	if err := os.WriteFile(fileParent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(fileParent, "sub", "x.json"), map[string]int{"a": 1}); err == nil {
		t.Error("writeJSONAtomic should surface a mkdir error")
	}
}

// TestWriteOps_MkdirError covers the per-op MkdirAll error branch by turning the
// executor dir's parent (executors/<id>) into a regular file before writing.
func TestWriteOps_MkdirError(t *testing.T) {
	fx, root := newFx(t)
	const id = "exec-mk"
	// Create executors/<id> as a FILE so MkdirAll(filepath.Dir(path)) fails.
	if err := os.MkdirAll(filepath.Join(root, "executors"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "executors", id), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := fx.AppendProgress(id, ProgressEntry{At: testNow, Message: "m"}); err == nil {
		t.Error("AppendProgress should fail when dir is a file")
	}
	if err := fx.WriteStatus(Status{ExecutorID: id, State: StateRunning, StartedAt: testNow}); err == nil {
		t.Error("WriteStatus should fail when dir is a file")
	}
	if err := fx.WriteOutput(Output{ExecutorID: id, Success: true, FinishedAt: testNow}); err == nil {
		t.Error("WriteOutput should fail when dir is a file")
	}
	if _, err := fx.Provision(id); err == nil {
		t.Error("Provision should fail when dir is a file")
	}
}

func TestReadOutput_ValidationFailure(t *testing.T) {
	fx, root := newFx(t)
	const id = "exec-outinvalid"
	if _, err := fx.Provision(id); err != nil {
		t.Fatal(err)
	}
	// Well-formed JSON, but success=true with an error is a protocol contradiction.
	body := `{"executor_id":"exec-outinvalid","success":true,"error":{"kind":"k","message":"m"},"finished_at":"2026-06-28T10:00:00Z"}`
	if err := os.WriteFile(filepath.Join(root, "executors", id, "output.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := fx.ReadOutput(id); err == nil {
		t.Fatal("ReadOutput should reject an output that fails Validate")
	}
}
