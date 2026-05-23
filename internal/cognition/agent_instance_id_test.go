package cognition_test

import (
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/cognition"
	"github.com/oopslink/agent-center/internal/observability"
)

func TestSpawn_AgentInstanceID_PropagatesThroughAR(t *testing.T) {
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "task-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"01H1"})
	inv, err := cognition.Spawn(cognition.SpawnInput{
		ID:              cognition.InvocationID("01HINV"),
		AgentInstanceID: "01HSUPERVISOR_AGENT_INSTANCE_ULID",
		Scope:           scope,
		TriggerEvents:   tes,
		StartedAt:       time.Now(),
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if inv.AgentInstanceID() != "01HSUPERVISOR_AGENT_INSTANCE_ULID" {
		t.Fatalf("agent_instance_id: %s", inv.AgentInstanceID())
	}
}

func TestSpawn_AgentInstanceID_OptionalInP8(t *testing.T) {
	// v1 / P8-transitional path: AgentInstanceID may be empty; Spawn must
	// still succeed (P9 adds caller-side enforcement that built-in
	// supervisor id is always passed in).
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "task-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"01H1"})
	inv, err := cognition.Spawn(cognition.SpawnInput{
		ID:            cognition.InvocationID("01HINV"),
		Scope:         scope,
		TriggerEvents: tes,
		StartedAt:     time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if inv.AgentInstanceID() != "" {
		t.Fatalf("expected empty AgentInstanceID, got %s", inv.AgentInstanceID())
	}
}

func TestRehydrate_AgentInstanceID_Roundtrips(t *testing.T) {
	scope := cognition.MustNewInvocationScope(cognition.ScopeTask, "task-1")
	tes, _ := cognition.NewTriggerEventSet([]observability.EventID{"01H1"})
	rehydrated, err := cognition.Rehydrate(cognition.RehydrateInput{
		ID:                 cognition.InvocationID("01HINV"),
		AgentInstanceID:    "01HSUPERVISOR_ULID",
		Scope:              scope,
		TriggerEvents:      tes,
		Status:             cognition.StatusRunning,
		HardTimeoutSeconds: 180,
		StartedAt:          time.Now(),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
		Version:            1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rehydrated.AgentInstanceID() != "01HSUPERVISOR_ULID" {
		t.Fatalf("agent_instance_id: %s", rehydrated.AgentInstanceID())
	}
}
