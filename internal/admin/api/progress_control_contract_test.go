package api

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestProgressControlMapContract(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	got := progressControlMap(&pm.ProgressControlSnapshot{
		AsOf: now, Decision: pm.ResponsibilityBound,
		OpenHolds:       []pm.ProgressHold{{ID: "hold-1", TaskID: "task-1", ReasonKind: "ack_wake", BlocksDispatch: true}},
		OpenObligations: []pm.ProgressObligation{{ID: "obl-1", TaskID: "task-1", Kind: pm.ProgressObligationAckWake, Status: pm.ResponsibilityOpen}},
		OpenIncidents:   []pm.ProgressIncident{{ID: "inc-1", TaskID: "task-1", Kind: pm.ProgressIncidentOperational, Severity: "operational", Status: pm.ResponsibilityOpen}},
	})
	if got["decision"] != string(pm.ResponsibilityBound) || len(got["open_holds"].([]map[string]any)) != 1 || len(got["open_obligations"].([]map[string]any)) != 1 || len(got["open_incidents"].([]map[string]any)) != 1 {
		t.Fatalf("progress_control contract = %#v", got)
	}
}
