package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemDirOfPath_Variants(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"/foo/bar/baz.md", "/foo/bar"},
		{"foo/bar.md", "foo"},
		{"plain", "plain"},
		{"", ""},
		{`win\path\file.md`, `win\path`},
	}
	for _, tc := range cases {
		got := memDirOfPath(tc.in)
		if got != tc.want {
			t.Errorf("memDirOfPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestWriteFile_CreatesNestedDir(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "a", "b", "c.md")
	if err := writeFile(p, []byte("hello")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Errorf("content %q", b)
	}
}

func TestWriteFile_BadPath(t *testing.T) {
	if err := writeFile("/dev/null/x/y/z.md", []byte("")); err == nil {
		t.Error("expected mkdir err")
	}
}

func TestFindProject(t *testing.T) {
	if got := findProject(nil); got != "" {
		t.Errorf("nil = %q", got)
	}
}
