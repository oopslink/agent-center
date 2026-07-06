package sqlite

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
)

func newSkillDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func TestInstalledSkillRepo_ReplaceAndList(t *testing.T) {
	db := newSkillDB(t)
	r := NewInstalledSkillRepo(db)
	ctx := context.Background()
	at := time.Date(2026, 7, 5, 10, 0, 0, 0, time.UTC)

	// empty → no rows.
	if got, err := r.ListByAgent(ctx, "A1"); err != nil || len(got) != 0 {
		t.Fatalf("empty list want 0,nil; got %v,%v", got, err)
	}

	first := []agent.InstalledSkill{
		{AgentRef: "A1", Layer: agent.SkillLayerProject, Name: "review", Description: "code review", Shadowed: false, CollectedAt: at},
		{AgentRef: "A1", Layer: agent.SkillLayerBuiltin, Name: "review", Description: "builtin review", Shadowed: true, CollectedAt: at},
		{AgentRef: "A1", Layer: agent.SkillLayerUser, Name: "solo", Description: "", Shadowed: false, CollectedAt: at},
	}
	if err := r.ReplaceForAgent(ctx, "A1", first); err != nil {
		t.Fatal(err)
	}
	got, err := r.ListByAgent(ctx, "A1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 rows, got %d", len(got))
	}
	// ordered by layer rank then name: built-in review, user solo, project review.
	if got[0].Layer != agent.SkillLayerBuiltin || !got[0].Shadowed {
		t.Fatalf("row0 should be built-in review shadowed: %+v", got[0])
	}
	if got[1].Layer != agent.SkillLayerUser || got[1].Name != "solo" {
		t.Fatalf("row1 should be user solo: %+v", got[1])
	}
	if got[2].Layer != agent.SkillLayerProject || got[2].Shadowed {
		t.Fatalf("row2 should be project review effective: %+v", got[2])
	}
	if !got[0].CollectedAt.Equal(at) {
		t.Fatalf("collected_at round-trip lost: %v", got[0].CollectedAt)
	}

	// replace shrinks the set (delete-by-agent + insert-all).
	if err := r.ReplaceForAgent(ctx, "A1", []agent.InstalledSkill{
		{AgentRef: "A1", Layer: agent.SkillLayerUser, Name: "solo", CollectedAt: at},
	}); err != nil {
		t.Fatal(err)
	}
	got, _ = r.ListByAgent(ctx, "A1")
	if len(got) != 1 || got[0].Name != "solo" {
		t.Fatalf("replace should leave exactly [solo], got %+v", got)
	}

	// replace with empty clears the agent's rows.
	if err := r.ReplaceForAgent(ctx, "A1", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ListByAgent(ctx, "A1"); len(got) != 0 {
		t.Fatalf("empty replace should clear rows, got %d", len(got))
	}
}

func TestInstalledSkillRepo_PerAgentIsolation(t *testing.T) {
	db := newSkillDB(t)
	r := NewInstalledSkillRepo(db)
	ctx := context.Background()
	at := time.Now().UTC()
	if err := r.ReplaceForAgent(ctx, "A1", []agent.InstalledSkill{{AgentRef: "A1", Layer: agent.SkillLayerUser, Name: "x", CollectedAt: at}}); err != nil {
		t.Fatal(err)
	}
	if err := r.ReplaceForAgent(ctx, "A2", []agent.InstalledSkill{{AgentRef: "A2", Layer: agent.SkillLayerUser, Name: "y", CollectedAt: at}}); err != nil {
		t.Fatal(err)
	}
	// replacing A1 must not touch A2.
	if err := r.ReplaceForAgent(ctx, "A1", nil); err != nil {
		t.Fatal(err)
	}
	if got, _ := r.ListByAgent(ctx, "A2"); len(got) != 1 || got[0].Name != "y" {
		t.Fatalf("A2 rows must survive A1 replace, got %+v", got)
	}
}

// TestInstalledSkillRepo_SameLayerDupReport_NormalizesAndPersists is the
// issue-4a45e9cc real-machine BLOCKER regression (tester1 A/B: case B returned 500).
// A report with SAME-LAYER duplicate names (a multi-plugin / multi-version install of
// e.g. skill-creator) must, after NormalizeInstalledSkills, persist WITHOUT the
// "UNIQUE constraint failed: agent_installed_skills.id" error — the two copies would
// otherwise mint the SAME store id (agent_ref\x1flayer\x1fname) and roll back the whole
// report. Post-fix the normalizer collapses same-layer dups to one row per name, so
// ReplaceForAgent succeeds. This crosses the normalize↔store seam that each side's own
// unit test missed (normalize kept both dups; the store required id uniqueness).
func TestInstalledSkillRepo_SameLayerDupReport_NormalizesAndPersists(t *testing.T) {
	db := newSkillDB(t)
	r := NewInstalledSkillRepo(db)
	ctx := context.Background()
	at := time.Date(2026, 7, 6, 10, 0, 0, 0, time.UTC)

	// A real ~/.claude-shaped report: two plugin-layer copies of "skill-creator" (two
	// installs) + a distinct plugin skill + a user skill.
	report := []agent.InstalledSkill{
		{AgentRef: "A1", Layer: agent.SkillLayerPlugin, Name: "skill-creator", Description: "install-a", CollectedAt: at},
		{AgentRef: "A1", Layer: agent.SkillLayerPlugin, Name: "skill-creator", Description: "install-b", CollectedAt: at},
		{AgentRef: "A1", Layer: agent.SkillLayerPlugin, Name: "brainstorming", Description: "bs", CollectedAt: at},
		{AgentRef: "A1", Layer: agent.SkillLayerUser, Name: "slack", Description: "user slack", CollectedAt: at},
	}
	norm, err := agent.NormalizeInstalledSkills(report)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if len(norm) != 3 {
		t.Fatalf("normalize should collapse the same-layer dup to one: want 3 (skill-creator+brainstorming+slack), got %d: %+v", len(norm), norm)
	}
	// The store id is agent_ref\x1flayer\x1fname; pre-fix the two "skill-creator" plugin
	// rows collided here → this ReplaceForAgent returned the UNIQUE 500 and stored 0 rows.
	if err := r.ReplaceForAgent(ctx, "A1", norm); err != nil {
		t.Fatalf("same-layer-dup report must persist after normalize (blocker regression): %v", err)
	}
	got, err := r.ListByAgent(ctx, "A1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3 persisted rows, got %d: %+v", len(got), got)
	}
	perName := map[string]int{}
	for _, s := range got {
		perName[string(s.Layer)+"/"+s.Name]++
	}
	if perName["plugin/skill-creator"] != 1 {
		t.Fatalf("skill-creator must persist exactly once in the plugin layer, got %d: %+v", perName["plugin/skill-creator"], got)
	}
}
