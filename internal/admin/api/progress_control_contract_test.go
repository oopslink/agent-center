package api

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestProgressControlMapContract(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	got := progressControlMap(&pm.ProgressControlSnapshot{
		AsOf: now, Decision: pm.ResponsibilityBound, ObservationVectorID: "obs-1", Quality: pm.ProgressQualitySuspect,
		Freshness:       pm.ProgressFreshness{State: "degraded", WatermarkLagMS: 10, ThresholdMS: 20},
		OpenHolds:       []pm.ProgressHold{{ID: "hold-1", TaskID: "task-1", ReasonKind: "ack_wake", BlocksDispatch: true}},
		OpenObligations: []pm.ProgressObligation{{ID: "obl-1", TaskID: "task-1", Kind: pm.ProgressObligationAckWake, Status: pm.ResponsibilityOpen}},
		OpenIncidents:   []pm.ProgressIncident{{ID: "inc-1", TaskID: "task-1", Kind: pm.ProgressIncidentOperational, Severity: "operational", Status: pm.ResponsibilityOpen}},
		RequiredActions: []pm.ProgressRequiredAction{{ID: "action:obl-1", SourceType: "obligation", SourceID: "obl-1", Category: "owner_action", Action: "acknowledge_wake", OwnerRef: "user:a", TriggerFactRefs: []string{"wake-1"}}},
	})
	if got["decision"] != string(pm.ResponsibilityBound) || got["observation_vector_id"] != "obs-1" || len(got["required_actions"].([]map[string]any)) != 1 || len(got["open_holds"].([]map[string]any)) != 1 || len(got["open_obligations"].([]map[string]any)) != 1 || len(got["open_incidents"].([]map[string]any)) != 1 {
		t.Fatalf("progress_control contract = %#v", got)
	}
}
