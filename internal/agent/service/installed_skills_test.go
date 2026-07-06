package service

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
)

func TestReportAndListInstalledSkills(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	at := time.Date(2026, 7, 5, 9, 0, 0, 0, time.UTC)

	// Report an out-of-order set with a collision; the service normalizes (shadow
	// recompute + sort) and stamps agent_ref + collected_at.
	err := f.svc.ReportInstalledSkills(ctx, "A1", []agent.InstalledSkill{
		{Layer: agent.SkillLayerBuiltin, Name: "review"},
		{Layer: agent.SkillLayerProject, Name: "review"},
		{Layer: agent.SkillLayerUser, Name: "solo"},
	}, at)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.svc.ListInstalledSkills(ctx, "A1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	for _, s := range got {
		if s.AgentRef != "A1" {
			t.Fatalf("agent_ref not stamped: %+v", s)
		}
		if !s.CollectedAt.Equal(at) {
			t.Fatalf("collected_at not stamped: %+v", s)
		}
	}
	// built-in review is shadowed; project review effective.
	if !got[0].Shadowed || got[0].Layer != agent.SkillLayerBuiltin {
		t.Fatalf("built-in review should be shadowed: %+v", got[0])
	}
	if got[2].Shadowed || got[2].Layer != agent.SkillLayerProject {
		t.Fatalf("project review should be effective: %+v", got[2])
	}

	// Re-report replaces wholesale.
	if err := f.svc.ReportInstalledSkills(ctx, "A1", nil, at); err != nil {
		t.Fatal(err)
	}
	if got, _ := f.svc.ListInstalledSkills(ctx, "A1"); len(got) != 0 {
		t.Fatalf("empty report should clear, got %d", len(got))
	}

	// Invalid layer is rejected.
	if err := f.svc.ReportInstalledSkills(ctx, "A1", []agent.InstalledSkill{{Layer: "bad", Name: "x"}}, at); err != agent.ErrInvalidSkillLayer {
		t.Fatalf("want ErrInvalidSkillLayer, got %v", err)
	}
}

func TestInstalledSkills_NilRepoNoOp(t *testing.T) {
	// A Service without a Skills repo (feature not wired) is a safe no-op.
	s := New(Deps{})
	if err := s.ReportInstalledSkills(context.Background(), "A1", []agent.InstalledSkill{{Layer: agent.SkillLayerUser, Name: "x"}}, time.Now()); err != nil {
		t.Fatalf("nil repo report should no-op, got %v", err)
	}
	if got, err := s.ListInstalledSkills(context.Background(), "A1"); err != nil || got != nil {
		t.Fatalf("nil repo list should be nil,nil; got %v,%v", got, err)
	}
}
