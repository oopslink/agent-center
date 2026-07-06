package skillscan

import (
	"os"
	"path/filepath"
	"testing"
)

// writeSkill creates root/<dir>/SKILL.md with the given frontmatter.
func writeSkill(t *testing.T, root, dir, name, desc string) {
	t.Helper()
	sd := filepath.Join(root, dir)
	if err := os.MkdirAll(sd, 0o755); err != nil {
		t.Fatal(err)
	}
	fm := "---\n"
	if name != "" {
		fm += "name: " + name + "\n"
	}
	if desc != "" {
		fm += "description: " + desc + "\n"
	}
	fm += "---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(sd, "SKILL.md"), []byte(fm), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestScan_FollowsSymlinkedSkillDirs is the F1 regression (tester1 acceptance): a
// skills root whose skill dirs are SYMLINKS (the real ~/.claude/skills layout — each
// <name> is a symlink into ../../.agents/skills/<name>) must still be scanned.
// os.ReadDir's DirEntry.IsDir() is FALSE for a symlink (its type is symlink, not dir),
// so the pre-fix `!e.IsDir()` guard silently dropped every symlinked skill — on the
// acceptance host all 6 user skills are symlinks → 0 reported. RED pre-fix (finds only
// the 1 real dir), GREEN post-fix (Stat follows the link → all 3).
func TestScan_FollowsSymlinkedSkillDirs(t *testing.T) {
	tmp := t.TempDir()
	root := filepath.Join(tmp, "user")   // the skills container that gets scanned
	store := filepath.Join(tmp, "store") // real skill dirs live here (symlink targets)

	writeSkill(t, root, "real", "real", "a real dir skill")
	writeSkill(t, store, "linked-a", "linked-a", "symlinked skill a")
	writeSkill(t, store, "linked-b", "linked-b", "symlinked skill b")
	for _, n := range []string{"linked-a", "linked-b"} {
		if err := os.Symlink(filepath.Join(store, n), filepath.Join(root, n)); err != nil {
			t.Fatalf("symlink %s: %v", n, err)
		}
	}

	got := Scan(LayerRoots{User: []string{root}})
	if len(got) != 3 {
		t.Fatalf("want 3 skills (1 real dir + 2 symlinked dirs), got %d: %+v", len(got), got)
	}
	names := map[string]bool{}
	for _, s := range got {
		names[s.Name] = true
	}
	for _, want := range []string{"real", "linked-a", "linked-b"} {
		if !names[want] {
			t.Fatalf("missing skill %q — symlinked skill dir dropped; got=%+v", want, got)
		}
	}
}

func TestScan_LayersAndShadowing(t *testing.T) {
	tmp := t.TempDir()
	userRoot := filepath.Join(tmp, "user")
	projRoot := filepath.Join(tmp, "proj")
	builtinRoot := filepath.Join(tmp, "builtin")

	writeSkill(t, builtinRoot, "review", "review", "builtin review")
	writeSkill(t, userRoot, "review", "review", "user review") // shadows builtin
	writeSkill(t, userRoot, "solo", "solo", "only in user")
	writeSkill(t, projRoot, "review", "review", "project review") // shadows user+builtin

	got := Scan(LayerRoots{
		Builtin: []string{builtinRoot},
		User:    []string{userRoot},
		Project: []string{projRoot},
	})
	if len(got) != 4 {
		t.Fatalf("want 4 skills, got %d: %+v", len(got), got)
	}
	// find the effective "review"
	var effectiveReview *Skill
	shadowedReviews := 0
	for i := range got {
		if got[i].Name == "review" {
			if got[i].Shadowed {
				shadowedReviews++
			} else {
				effectiveReview = &got[i]
			}
		}
	}
	if effectiveReview == nil || effectiveReview.Layer != LayerProject {
		t.Fatalf("project review should be the sole effective copy: %+v", got)
	}
	if shadowedReviews != 2 {
		t.Fatalf("built-in + user review should both be shadowed, got %d", shadowedReviews)
	}
	// ordering: built-in first, project last.
	if got[0].Layer != LayerBuiltin || got[len(got)-1].Layer != LayerProject {
		t.Fatalf("sort order wrong: %+v", got)
	}
}

func TestScan_NameFallbackToDir(t *testing.T) {
	tmp := t.TempDir()
	// frontmatter without a name → dir name is used.
	writeSkill(t, tmp, "my-skill", "", "no name field")
	got := Scan(LayerRoots{User: []string{tmp}})
	if len(got) != 1 || got[0].Name != "my-skill" {
		t.Fatalf("want name fallback to dir 'my-skill', got %+v", got)
	}
	if got[0].Description != "no name field" {
		t.Fatalf("description lost: %+v", got[0])
	}
}

func TestScan_MissingRootAndNonDir(t *testing.T) {
	tmp := t.TempDir()
	// a stray file at the root (not a dir) is ignored.
	if err := os.WriteFile(filepath.Join(tmp, "README.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := Scan(LayerRoots{
		User:    []string{tmp},
		Project: []string{filepath.Join(tmp, "does-not-exist")},
	})
	if len(got) != 0 {
		t.Fatalf("no skills expected, got %+v", got)
	}
}

func TestScan_PluginMultiRoot(t *testing.T) {
	tmp := t.TempDir()
	p1 := filepath.Join(tmp, "pluginA", "skills")
	p2 := filepath.Join(tmp, "pluginB", "skills")
	writeSkill(t, p1, "alpha", "alpha", "from A")
	writeSkill(t, p2, "beta", "beta", "from B")
	got := Scan(LayerRoots{Plugin: []string{p1, p2}})
	if len(got) != 2 {
		t.Fatalf("want 2 plugin skills across roots, got %+v", got)
	}
	for _, s := range got {
		if s.Layer != LayerPlugin {
			t.Fatalf("expected plugin layer, got %+v", s)
		}
	}
}

func TestFingerprint_StableAndSensitive(t *testing.T) {
	a := []Skill{{Layer: LayerUser, Name: "x", Description: "d"}}
	b := []Skill{{Layer: LayerUser, Name: "x", Description: "d"}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatal("same content should fingerprint equal")
	}
	c := []Skill{{Layer: LayerUser, Name: "x", Description: "CHANGED"}}
	if Fingerprint(a) == Fingerprint(c) {
		t.Fatal("description change must change fingerprint")
	}
	d := []Skill{{Layer: LayerUser, Name: "x", Description: "d", Shadowed: true}}
	if Fingerprint(a) == Fingerprint(d) {
		t.Fatal("shadowed change must change fingerprint")
	}
	if Fingerprint(nil) != Fingerprint([]Skill{}) {
		t.Fatal("nil and empty should fingerprint equal")
	}
}
