package api

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
	pmservice "github.com/oopslink/agent-center/internal/projectmanager/service"
)

func TestPMPlanDetailMap_ProgressControlContract(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	plan, err := pm.NewPlan(pm.NewPlanInput{ID: "plan-api", ProjectID: "project-api", Name: "API", CreatorRef: "user:a", CreatedAt: now})
	if err != nil {
		t.Fatal(err)
	}
	detail := &pmservice.PlanDetail{
		Plan: plan,
		View: pm.PlanView{Nodes: []pm.PlanNodeView{{TaskID: "task-a", NodeStatus: pm.NodeBlocked, Effective: true}}},
		ProgressControl: &pm.ProgressControl{
			AsOf:                now,
			Health:              pm.ProgressHealthDegraded,
			Freshness:           pm.ProgressFreshness{State: pm.ProgressFreshnessDegraded, WatermarkLag: time.Hour, Threshold: 15 * time.Minute},
			Decision:            pm.ProgressDecisionCannotDetermine,
			ObservationVectorID: "plan-progress:plan-api:1",
			Quality:             pm.ProgressQualitySuspect,
			OpenIncidents: []pm.ProgressIncident{{
				ID: "incident-1", PlanID: plan.ID(), TaskID: "task-a", Kind: "progress_classification_unknown",
				OwnerRef: "service:projectmanager", DeadlineAt: now.Add(15 * time.Minute), AckRequired: true, Status: "open",
			}},
			OpenHolds: []pm.ProgressHold{{
				ID: "hold-1", PlanID: plan.ID(), TaskID: "task-a", ReasonKind: "incident", ReasonID: "incident-1",
				BlocksNewDispatch: true, BlocksGatePassToken: true, BlocksDestructiveDownstreamStart: true,
				InFlightPolicy: "do_not_kill_unproven_execution", StartedAt: now.Add(-time.Hour), DeadlineAt: now.Add(15 * time.Minute), Age: time.Hour,
			}},
			RequiredActions: []pm.ProgressRequiredAction{{
				ID: "required-1", Kind: pm.ProgressAttentionIncident, Action: "repair_progress_classification",
				SubjectID: "task-a", OwnerRef: "service:projectmanager", IncidentID: "incident-1", HoldID: "hold-1",
				Severity: "critical", Source: "blocked_on:plan-api:task-a", Summary: "Classification source is missing the required owner/deadline discipline.",
			}},
			Coverage: pm.ProgressCoverage{TotalNodes: 1, ClassifiedNodes: 1, CannotDetermineNodes: 1, OpenIncidents: 1, OpenHolds: 1},
		},
	}
	detail.ProgressControl.PrimaryAttention = &detail.ProgressControl.RequiredActions[0]

	m := pmPlanDetailMap(detail)
	pc, ok := m["progress_control"].(map[string]any)
	if !ok {
		t.Fatalf("progress_control missing: %+v", m)
	}
	if pc["decision"] != string(pm.ProgressDecisionCannotDetermine) || pc["quality"] != string(pm.ProgressQualitySuspect) {
		t.Fatalf("progress_control decision/quality = %+v", pc)
	}
	if pc["health"] != string(pm.ProgressHealthDegraded) {
		t.Fatalf("health=%+v want degraded", pc["health"])
	}
	fresh := pc["freshness"].(map[string]any)
	if fresh["state"] != string(pm.ProgressFreshnessDegraded) || fresh["watermark_lag_ms"] != int64(time.Hour/time.Millisecond) {
		t.Fatalf("freshness=%+v", fresh)
	}
	actions := pc["required_actions"].([]map[string]any)
	if len(actions) != 1 || actions[0]["summary"] == "future release condition" {
		t.Fatalf("required_actions=%+v", actions)
	}
	holds := pc["open_holds"].([]map[string]any)
	if len(holds) != 1 || holds[0]["in_flight_policy"] != "do_not_kill_unproven_execution" {
		t.Fatalf("open_holds=%+v", holds)
	}
	coverage := pc["coverage"].(map[string]any)
	if coverage["cannot_determine_nodes"] != 1 || coverage["open_holds"] != 1 {
		t.Fatalf("coverage=%+v", coverage)
	}
}
