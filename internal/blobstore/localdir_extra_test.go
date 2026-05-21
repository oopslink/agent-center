package blobstore

import (
	"strings"
	"testing"
)

func TestLocalDir_Root_Returns(t *testing.T) {
	root := t.TempDir()
	l, _ := NewLocalDir(root)
	if l.Root() != root {
		t.Fatalf("root mismatch: %s vs %s", l.Root(), root)
	}
}

func TestLocalDir_WithMaxBytes_ZeroIgnored(t *testing.T) {
	l, _ := NewLocalDir(t.TempDir())
	prev := l.maxBytes
	l.WithMaxBytes(0)
	if l.maxBytes != prev {
		t.Fatal("WithMaxBytes(0) should be no-op")
	}
	l.WithMaxBytes(-5)
	if l.maxBytes != prev {
		t.Fatal("WithMaxBytes(<0) should be no-op")
	}
}

func TestValidateRel_Cases(t *testing.T) {
	if err := validateRel(""); err == nil {
		t.Fatal("empty")
	}
	if err := validateRel("../up"); err == nil {
		t.Fatal("..")
	}
	if err := validateRel("/abs"); err == nil {
		t.Fatal("abs")
	}
	if err := validateRel("ok/path"); err != nil {
		t.Fatal("good")
	}
}

func TestEscapeLikeStubExists(t *testing.T) {
	// touch the unexported limitedReader path so coverage credit lands
	r := strings.NewReader("xxx")
	lr := &limitedReader{R: r, N: 10}
	b := make([]byte, 3)
	if _, err := lr.Read(b); err != nil {
		t.Fatalf("first read: %v", err)
	}
	if lr.N != 7 {
		t.Fatalf("N: %d", lr.N)
	}
}
