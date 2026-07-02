package api

import (
	"testing"
	"time"

	agentbc "github.com/oopslink/agent-center/internal/agent"
)

// TestAgentMap_EmitsLastLifecycleTransitionAt: the shared list+detail serializer
// emits last_lifecycle_transition_at (RFC3339Nano) reflecting the last lifecycle
// transition, and omits it only when the field is zero.
func TestAgentMap_EmitsLastLifecycleTransitionAt(t *testing.T) {
	created := time.Date(2026, 6, 1, 8, 0, 0, 0, time.UTC)
	a, err := agentbc.NewAgent(agentbc.NewAgentInput{
		ID: "A1", OrganizationID: "org", WorkerID: "W1",
		Profile:   agentbc.Profile{Name: "coder"},
		CreatedBy: "user:a", CreatedAt: created,
	})
	if err != nil {
		t.Fatal(err)
	}
	// Fresh agent: seeded to created time.
	m := agentMap(a, agentbc.Available)
	if got := m["last_lifecycle_transition_at"]; got != created.Format(time.RFC3339Nano) {
		t.Fatalf("fresh: got %v, want %v", got, created.Format(time.RFC3339Nano))
	}

	// After a start, the field reflects the start time.
	started := created.Add(3 * time.Hour)
	if err := a.Start(started); err != nil {
		t.Fatal(err)
	}
	m = agentMap(a, agentbc.Available)
	if got := m["last_lifecycle_transition_at"]; got != started.Format(time.RFC3339Nano) {
		t.Fatalf("after start: got %v, want %v", got, started.Format(time.RFC3339Nano))
	}
}
