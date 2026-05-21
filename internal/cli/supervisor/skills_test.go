package supervisor_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/cli/supervisor"
	"github.com/oopslink/agent-center/internal/cognition"
)

func TestSkillContent_NotEmpty(t *testing.T) {
	b, err := supervisor.SkillContent()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("empty")
	}
	s := string(b)
	for _, k := range cognition.AllDecisionKinds() {
		if !strings.Contains(s, string(k)) {
			t.Errorf("skill missing decision kind %s", k)
		}
	}
	// must mention rationale + memory protocol
	if !strings.Contains(s, "--rationale") {
		t.Error("missing --rationale guidance")
	}
	if !strings.Contains(s, "supervisor.md") {
		t.Error("missing supervisor.md self-memory mention")
	}
}

func TestSkillContent_SizeWithinBudget(t *testing.T) {
	b, err := supervisor.SkillContent()
	if err != nil {
		t.Fatal(err)
	}
	// keep skill < 8 KB so prompt stays under blob threshold
	if len(b) > 8*1024 {
		t.Errorf("skill = %d bytes; budget 8 KB", len(b))
	}
}

func TestWriteSkillTo_HappyAndEmpty(t *testing.T) {
	dir := t.TempDir()
	p, err := supervisor.WriteSkillTo(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(p) != "supervisor.md" {
		t.Errorf("path = %s", p)
	}
	if _, err := supervisor.WriteSkillTo(""); err == nil {
		t.Error("expected dir-required err")
	}
}
