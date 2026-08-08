package agentruntime

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/oopslink/agent-center/internal/mcphost"
)

func planningRulesBody(commit string) map[string]any {
	return map[string]any{
		"team_id": "team-a", "phase": "plan", "commit": commit,
		"refresh_semantics": "next_planning_session",
		"rules": []map[string]any{{
			"slug": "plan-contract", "title": "Plan contract",
			"description": "Required plan shape.", "body": "Use a Decision node.",
			"applies_to": []string{"plan"}, "source_path": "rules/plan-contract.md",
		}},
	}
}

func TestStartCodex_ProductionPlanningEntryInjectsTeamRules(t *testing.T) {
	base := t.TempDir()
	var launched CodexSpec
	sc := &scriptedToolCaller{teamRulesBody: planningRulesBody("production-commit")}
	rt := NewLocalRuntime(LocalRuntimeConfig{
		AgentID: "agent-plan", AgentHomeBase: base, WorkerID: "worker-1",
		BinaryPath: "/opt/agent-center-worker", AdminURL: "https://center.invalid",
		WorkerToken: "token", Reporter: &nopReporter{}, Log: func(string, ...any) {},
		MCPPreflight: func(context.Context, mcphost.Config, ...string) error { return nil },
		CodexStarter: func(_ context.Context, spec CodexSpec) (Session, error) {
			launched = spec
			return &fakeSession{}, nil
		},
	}, &SessionState{})
	setToolCaller(rt, sc)

	if err := rt.Start(context.Background(), StartSpec{
		AgentID: "agent-plan", Version: 1, CLI: CLICodex, PromptDescription: "Planner persona",
	}); err != nil {
		t.Fatalf("Start(codex): %v", err)
	}
	for _, want := range []string{"Planner persona", "production-commit", "rules/plan-contract.md", "Use a Decision node."} {
		if !strings.Contains(launched.ExtraSystemPrompt, want) {
			t.Fatalf("production Codex prompt missing %q:\n%s", want, launched.ExtraSystemPrompt)
		}
	}
	if launched.TasksDir != filepath.Join(base, "agents", "agent-plan", "tasks") {
		t.Fatalf("unexpected task dir %q", launched.TasksDir)
	}
}

func TestPlanningPromptDescription_LoadsAuditableFrozenPlanSnapshot(t *testing.T) {
	sc := &scriptedToolCaller{teamRulesBody: planningRulesBody("commit-1")}
	rt := NewLocalRuntime(LocalRuntimeConfig{AgentID: "agent-plan"}, &SessionState{})
	setToolCaller(rt, sc)

	got := rt.planningPromptDescription(context.Background(), "agent-plan", "Persona")
	for _, want := range []string{
		"Persona", "frozen planning snapshot", "Team: team-a", "Phase: plan",
		"Commit: commit-1", "Refresh: next_planning_session",
		"rules/plan-contract.md", "Applies to: plan", "Use a Decision node.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("planning prompt missing %q:\n%s", want, got)
		}
	}
	body, ok := sc.callFor("get_team_rules")
	if !ok || body["agent_id"] != "agent-plan" || body["phase"] != rulePhasePlan {
		t.Fatalf("get_team_rules call = %v, ok=%v", body, ok)
	}

	// The rendered string is a frozen value: changing the backing response cannot
	// mutate an already-running session. A new generation calls the loader again.
	sc.teamRulesBody = planningRulesBody("commit-2")
	if strings.Contains(got, "commit-2") {
		t.Fatal("running planning snapshot refreshed in place")
	}
	next := rt.planningPromptDescription(context.Background(), "agent-plan", "Persona")
	if !strings.Contains(next, "Commit: commit-2") {
		t.Fatalf("new planning generation did not reload: %s", next)
	}
}

func TestPlanningPromptDescription_FailureAndNoTeamAreIsolated(t *testing.T) {
	for name, sc := range map[string]*scriptedToolCaller{
		"load failure": {teamRulesErr: context.DeadlineExceeded},
		"no team":      {teamRulesBody: map[string]any{"phase": "plan"}},
	} {
		t.Run(name, func(t *testing.T) {
			rt := NewLocalRuntime(LocalRuntimeConfig{AgentID: "agent-plan", Log: func(string, ...any) {}}, &SessionState{})
			setToolCaller(rt, sc)
			if got := rt.planningPromptDescription(context.Background(), "agent-plan", "Persona"); got != "Persona" {
				t.Fatalf("failure isolation changed prompt: %q", got)
			}
		})
	}
}

func TestPlanningPromptDescription_InvalidRulesRemainAuditableButDoNotInjectBody(t *testing.T) {
	sc := &scriptedToolCaller{teamRulesBody: map[string]any{
		"team_id": "team-a", "phase": "plan", "commit": "commit-invalid",
		"refresh_semantics":   "next_planning_session",
		"skipped_nonstandard": []string{"rules/not-standard.md"},
	}}
	rt := NewLocalRuntime(LocalRuntimeConfig{AgentID: "agent-plan"}, &SessionState{})
	setToolCaller(rt, sc)
	got := rt.planningPromptDescription(context.Background(), "agent-plan", "Persona")
	for _, want := range []string{"commit-invalid", "Skipped nonstandard rules: rules/not-standard.md"} {
		if !strings.Contains(got, want) {
			t.Fatalf("invalid-rule audit missing %q: %s", want, got)
		}
	}
	if strings.Contains(got, "Use a Decision node.") {
		t.Fatalf("invalid rule body was injected: %s", got)
	}
}
