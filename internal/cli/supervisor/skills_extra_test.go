package supervisor_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/cli/supervisor"
)

func TestWriteSkillTo_NestedDir(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "deep", "nested", "skills")
	p, err := supervisor.WriteSkillTo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Errorf("file missing: %v", err)
	}
}

func TestWriteSkillTo_ErrUnwritable(t *testing.T) {
	// /dev/null is a file, not a directory; writing under it should error.
	_, err := supervisor.WriteSkillTo("/dev/null")
	if err == nil {
		t.Error("expected err writing under /dev/null")
	}
	_ = errors.Is // keep import
	_ = strings.Join
}
