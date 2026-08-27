package api

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

func TestAgentPlanDetailMap_ProgressControlContract(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	plan, err := pm.NewPlan(pm.NewPlanInput{ID: "plan-agent", ProjectID: "project-agent", Name: "Agent", CreatorRef: "agent:a", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	detail := &pmservice.PlanDetail{
		Plan: plan,
		View: pm.PlanView{Nodes: []pm.PlanNodeView{{TaskID: "task-a", NodeStatus: pm.NodeBlocked, Effective: true}}},
		ProgressControl: &pm.ProgressControl{
			AsOf:                now,
			Health:              pm.ProgressHealthAttention,
			Freshness:           pm.ProgressFreshness{State: pm.ProgressFreshnessFresh, Threshold: 15 * time.Minute},
			Decision:            pm.ProgressDecisionResponsibility,
			ObservationVectorID: "plan-progress:plan-agent:1",
			Quality:             pm.ProgressQualityValid,
			RequiredActions: []pm.ProgressRequiredAction{{
				ID: "required-1", Kind: pm.ProgressAttentionObligation, Action: "acceptance_verdict",
				SubjectID: "task-a", OwnerRef: "agent:a", Severity: "attention",
				Source: "blocked_on:plan-agent:task-a", Summary: "Record an authoritative acceptance verdict for the exact subject.",
			}},
			ValidInFlight: []pm.ProgressInFlight{{TaskID: "task-run", Status: pm.NodeRunning, AssigneeRef: "agent:a", Quality: pm.ProgressQualityValid, Source: "plan_view"}},
			Coverage:      pm.ProgressCoverage{TotalNodes: 2, ClassifiedNodes: 2, ResponsibilityNodes: 1, ValidInFlightNodes: 1},
		},
	}
	detail.ProgressControl.PrimaryAttention = &detail.ProgressControl.RequiredActions[0]

	m := planDetailMap(detail)
	pc, ok := m["progress_control"].(map[string]any)
	if !ok {
		t.Fatalf("progress_control missing: %+v", m)
	}
	if pc["health"] != string(pm.ProgressHealthAttention) || pc["decision"] != string(pm.ProgressDecisionResponsibility) {
		t.Fatalf("progress_control=%+v", pc)
	}
	actions := pc["required_actions"].([]map[string]any)
	if len(actions) != 1 || actions[0]["action"] != "acceptance_verdict" {
		t.Fatalf("required_actions=%+v", actions)
	}
	inFlight := pc["valid_in_flight"].([]map[string]any)
	if len(inFlight) != 1 || inFlight[0]["quality"] != string(pm.ProgressQualityValid) {
		t.Fatalf("valid_in_flight=%+v", inFlight)
	}
}
