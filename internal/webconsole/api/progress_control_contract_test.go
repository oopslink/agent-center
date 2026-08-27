package api

import (
	"testing"
	"time"

	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

func TestPMProgressControlMapContract(t *testing.T) {
	now := time.Date(2026, 8, 27, 12, 0, 0, 0, time.UTC)
	got := pmProgressControlMap(&pm.ProgressControlSnapshot{
		AsOf: now, Decision: pm.ResponsibilityBound,
		OpenHolds:       []pm.ProgressHold{{ID: "hold-1", TaskID: "task-1", BlocksAcceptance: true}},
		OpenObligations: []pm.ProgressObligation{{ID: "obl-1", Kind: pm.ProgressObligationAckWake}},
		OpenIncidents:   []pm.ProgressIncident{{ID: "inc-1", Kind: pm.ProgressIncidentOperational}},
	})
	if got["decision"] != string(pm.ResponsibilityBound) || len(got["open_holds"].([]map[string]any)) != 1 || len(got["open_obligations"].([]map[string]any)) != 1 || len(got["open_incidents"].([]map[string]any)) != 1 {
		t.Fatalf("progress_control contract = %#v", got)
	}
}
