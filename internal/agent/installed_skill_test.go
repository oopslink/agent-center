package agent

import "testing"

func TestSkillLayer_ValidAndRank(t *testing.T) {
	for _, l := range []SkillLayer{SkillLayerBuiltin, SkillLayerPlugin, SkillLayerUser, SkillLayerProject} {
		if !l.IsValid() {
			t.Fatalf("%q should be valid", l)
		}
	}
	if SkillLayer("bogus").IsValid() {
		t.Fatal("bogus layer must be invalid")
	}
	if SkillLayer("bogus").Rank() != -1 {
		t.Fatal("unknown layer rank must be -1")
	}
	// precedence: built-in < plugin < user < project
	if !(SkillLayerBuiltin.Rank() < SkillLayerPlugin.Rank() &&
		SkillLayerPlugin.Rank() < SkillLayerUser.Rank() &&
		SkillLayerUser.Rank() < SkillLayerProject.Rank()) {
		t.Fatal("layer precedence order wrong")
	}
}

func TestNormalizeInstalledSkills_EmptyAndBlank(t *testing.T) {
	if got, err := NormalizeInstalledSkills(nil); err != nil || got != nil {
		t.Fatalf("nil → nil,nil; got %v,%v", got, err)
	}
	// all-blank names → nil
	got, err := NormalizeInstalledSkills([]InstalledSkill{
		{Layer: SkillLayerUser, Name: "   "},
	})
	if err != nil || got != nil {
		t.Fatalf("blank-only → nil,nil; got %v,%v", got, err)
	}
}

func TestNormalizeInstalledSkills_InvalidLayer(t *testing.T) {
	_, err := NormalizeInstalledSkills([]InstalledSkill{{Layer: "nope", Name: "x"}})
	if err != ErrInvalidSkillLayer {
		t.Fatalf("want ErrInvalidSkillLayer, got %v", err)
	}
}

func TestNormalizeInstalledSkills_ShadowRecompute(t *testing.T) {
	// "review" exists in built-in and project; project wins (highest rank), built-in
	// is shadowed. "solo" only in user — effective. Input arrives out of order and
	// with wrong shadowed flags to prove the normalizer RECOMPUTES from precedence.
	in := []InstalledSkill{
		{Layer: SkillLayerBuiltin, Name: "review", Shadowed: false},
		{Layer: SkillLayerUser, Name: " solo ", Shadowed: true},
		{Layer: SkillLayerProject, Name: "review", Shadowed: true},
	}
	got, err := NormalizeInstalledSkills(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 skills, got %d", len(got))
	}
	// sorted by layer rank then name: built-in review, user solo, project review
	if got[0].Layer != SkillLayerBuiltin || got[0].Name != "review" || !got[0].Shadowed {
		t.Fatalf("built-in review should be shadowed: %+v", got[0])
	}
	if got[1].Layer != SkillLayerUser || got[1].Name != "solo" || got[1].Shadowed {
		t.Fatalf("user solo should be effective + trimmed: %+v", got[1])
	}
	if got[2].Layer != SkillLayerProject || got[2].Name != "review" || got[2].Shadowed {
		t.Fatalf("project review should be effective (wins): %+v", got[2])
	}
}

func TestNormalizeInstalledSkills_SameLayerDup(t *testing.T) {
	// SAME-layer same-name (case-insensitive) COLLAPSES to the FIRST seen — one
	// effective skill per name per layer (issue-4a45e9cc real-machine blocker: two
	// same-layer copies minted the same store id → UNIQUE constraint → whole report
	// rejected → panel empty). Cross-layer dups are handled separately (shadowed).
	in := []InstalledSkill{
		{Layer: SkillLayerUser, Name: "dup", Description: "first"},
		{Layer: SkillLayerUser, Name: "DUP", Description: "second"}, // case-insensitive dup
		{Layer: SkillLayerUser, Name: "solo", Description: "kept"},
	}
	got, err := NormalizeInstalledSkills(in)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("same-layer dup must collapse to one: want 2 (dup+solo), got %d: %+v", len(got), got)
	}
	byName := map[string]InstalledSkill{}
	for _, s := range got {
		if _, seen := byName[s.Name]; seen {
			t.Fatalf("name %q appears twice in the same layer — not collapsed: %+v", s.Name, got)
		}
		byName[s.Name] = s
	}
	dup, ok := byName["dup"]
	if !ok {
		t.Fatalf("first-seen 'dup' must survive: %+v", got)
	}
	if dup.Description != "first" {
		t.Fatalf("keep-first: want description %q, got %q", "first", dup.Description)
	}
	if dup.Shadowed {
		t.Fatalf("the single surviving same-layer skill must not be shadowed: %+v", dup)
	}
}
