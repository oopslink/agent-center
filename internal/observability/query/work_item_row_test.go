package query

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/observability/projection"
)

// TestWorkItemRowFromProjection_MapsAllFields pins the single shared work-item
// row formatter used by fleet + inspect/query (#107 Phase-2). Any drift in the
// field mapping (e.g. WorkingSeconds source, timestamp format, taskID injection)
// fails here — one source, one test.
func TestWorkItemRowFromProjection_MapsAllFields(t *testing.T) {
	at := time.Date(2026, 5, 20, 10, 0, 0, 0, time.UTC)
	p := &projection.AgentWorkItemProjection{
		WorkItemID:                "WI-1",
		AgentID:                   "AG-1",
		Status:                    "active",
		CurrentActivity:           "edit",
		TotalToolCalls:            2,
		TotalTokensInput:          100,
		TotalTokensOutput:         50,
		WorkingSecondsAccumulated: 7,
		LastActivityAt:            at,
	}
	row := workItemRowFromProjection(p, "T-1")
	if row.WorkItemID != "WI-1" || row.AgentID != "AG-1" || row.TaskID != "T-1" ||
		row.Status != "active" || row.CurrentActivity != "edit" ||
		row.TotalToolCalls != 2 || row.TotalTokensInput != 100 || row.TotalTokensOutput != 50 ||
		row.WorkingSeconds != 7 || row.LastActivityAt != at.Format(time.RFC3339Nano) {
		t.Fatalf("formatter field mapping wrong: %+v", row)
	}
}
