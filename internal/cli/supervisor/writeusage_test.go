package supervisor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/oopslink/agent-center/internal/cognition"
)

func TestWriteUsage_AtomicRename(t *testing.T) {
	dir := t.TempDir()
	u := cognition.TokenUsage{Input: 7, Output: 11, CacheRead: 3, CacheCreate: 2}
	if err := writeUsage(dir, "INV1", u); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "INV1.usage.json")
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var got cognition.TokenUsage
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got != u {
		t.Errorf("usage %+v", got)
	}
}

func TestWriteUsage_BadDir(t *testing.T) {
	// /dev/null/x cannot be created as a dir.
	if err := writeUsage("/dev/null/foo", "INV", cognition.TokenUsage{}); err == nil {
		t.Error("expected mkdir err")
	}
}
