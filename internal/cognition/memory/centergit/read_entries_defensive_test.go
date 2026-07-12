package centergit

import (
	"os"
	"path/filepath"
	"testing"
)

// TestReadEntriesSkipsNonStandardFiles proves the extract read path is DEFENSIVE
// (design §6): a member may push any non-standard / stray file into the shared team
// repo (no frontmatter, an unterminated fence, a scratch note). ReadEntries must
// SKIP such files and report them in `skipped` — NOT error out and crash the whole
// extract. Well-formed entries alongside them still read.
func TestReadEntriesSkipsNonStandardFiles(t *testing.T) {
	dir := t.TempDir()
	ed := filepath.Join(dir, entriesDir)
	if err := os.MkdirAll(ed, 0o700); err != nil {
		t.Fatal(err)
	}
	write := func(name, content string) {
		if err := os.WriteFile(filepath.Join(ed, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// One good entry.
	write("good-1.md", "---\nname: good\ndescription: a real hook\n---\n\nbody text\n")
	// Three non-standard files a member could have pushed.
	write("stray-note.md", "just some markdown, no frontmatter at all\n")
	write("open-fence.md", "---\nname: x\n") // unterminated frontmatter
	write("broken-yaml.md", "---\n: : nope\n---\n")

	s := NewStore(dir, nil)
	entries, skipped, err := s.ReadEntries()
	if err != nil {
		t.Fatalf("ReadEntries errored on stray files (want defensive skip): %v", err)
	}
	if len(entries) != 1 || entries[0].Slug != "good" {
		t.Fatalf("entries=%+v, want exactly the one good entry", entries)
	}
	if len(skipped) != 3 {
		t.Fatalf("skipped=%v, want 3 non-standard files reported", skipped)
	}
	// Skipped list is sorted deterministically.
	want := []string{"broken-yaml.md", "open-fence.md", "stray-note.md"}
	for i, w := range want {
		if skipped[i] != w {
			t.Fatalf("skipped[%d]=%q want %q (sorted)", i, skipped[i], w)
		}
	}

	// A genuinely empty entries dir → no entries, no skips, no error.
	empty := NewStore(t.TempDir(), nil)
	e2, sk2, err2 := empty.ReadEntries()
	if err2 != nil || len(e2) != 0 || len(sk2) != 0 {
		t.Fatalf("empty repo: entries=%v skipped=%v err=%v", e2, sk2, err2)
	}
}
