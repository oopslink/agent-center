package agent

import (
	"testing"
	"time"
)

var t0 = time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)

func newAgent(t *testing.T) *Agent {
	t.Helper()
	a, err := NewAgent(NewAgentInput{
		ID: "A1", OrganizationID: "org", Profile: Profile{Name: "coder", Model: "claude", CLI: "claudecode", EnvVars: map[string]string{"K": "V"}},
		Skills: []string{"go"}, WorkerID: "W1", CreatedBy: "user:a", CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestNewAgent_RequiresWorker(t *testing.T) {
	if _, err := NewAgent(NewAgentInput{ID: "A1", OrganizationID: "org", Profile: Profile{Name: "x"}, CreatedBy: "user:a", CreatedAt: t0}); err != ErrWorkerRequired {
		t.Fatalf("want ErrWorkerRequired, got %v", err)
	}
}

func TestNewAgent_Defaults(t *testing.T) {
	a := newAgent(t)
	if a.Lifecycle() != LifecycleStopped {
		t.Fatalf("new agent should be stopped, got %s", a.Lifecycle())
	}
	if a.WorkerID() != "W1" || a.Version() != 1 {
		t.Fatal("worker/version")
	}
	if a.HomeRel() != "workers/W1/agents/A1" {
		t.Fatalf("HomeRel = %s", a.HomeRel())
	}
	if a.DefaultWorkspaceRel() != "workers/W1/agents/A1/workspace" {
		t.Fatalf("DefaultWorkspaceRel = %s", a.DefaultWorkspaceRel())
	}
}

func TestAgentLifecycle(t *testing.T) {
	a := newAgent(t)
	// can't stop a stopped agent
	if err := a.Stop(t0); err != ErrIllegalLifecycle {
		t.Fatalf("stop stopped want illegal, got %v", err)
	}
	if err := a.Start(t0); err != nil {
		t.Fatal(err)
	}
	if a.Lifecycle() != LifecycleRunning {
		t.Fatal("should be running")
	}
	if err := a.Restart(t0); err != nil {
		t.Fatal(err)
	}
	if err := a.Stop(t0); err != nil {
		t.Fatal(err)
	}
	if a.Lifecycle() != LifecycleStopping {
		t.Fatal("should be stopping")
	}
	if err := a.MarkStopped(t0); err != nil {
		t.Fatal(err)
	}
	if a.Lifecycle() != LifecycleStopped {
		t.Fatal("should be stopped")
	}
}

func TestAgentReset(t *testing.T) {
	a := newAgent(t)
	if err := a.Reset("bogus", t0); err != ErrInvalidResetScope {
		t.Fatalf("invalid scope want ErrInvalidResetScope, got %v", err)
	}
	if err := a.Reset(ResetAll, t0); err != nil {
		t.Fatal(err)
	}
	if a.Lifecycle() != LifecycleResetting {
		t.Fatal("should be resetting")
	}
	// resetting again is illegal
	if err := a.Reset(ResetMemory, t0); err != ErrIllegalLifecycle {
		t.Fatalf("double reset want illegal, got %v", err)
	}
	if err := a.MarkStopped(t0); err != nil {
		t.Fatal(err)
	}
	if a.Lifecycle() != LifecycleStopped {
		t.Fatal("reset should settle to stopped")
	}
}

func TestAgentError(t *testing.T) {
	a := newAgent(t)
	a.MarkError("boom", t0)
	if a.Lifecycle() != LifecycleError || a.LifecycleError() != "boom" {
		t.Fatal("error state")
	}
	// can start from error
	if err := a.Start(t0); err != nil {
		t.Fatal(err)
	}
	if a.LifecycleError() != "" {
		t.Fatal("start should clear error")
	}
}

func TestAgentUpdateProfileAndSkills(t *testing.T) {
	a := newAgent(t)
	v := a.Version()
	if err := a.UpdateProfile(Profile{Name: ""}, t0); err == nil {
		t.Fatal("empty name should fail")
	}
	if err := a.UpdateProfile(Profile{Name: "coder2", Model: "m"}, t0); err != nil {
		t.Fatal(err)
	}
	a.SetSkills([]string{"go", "rust"}, t0)
	if a.Profile().Name != "coder2" || len(a.Skills()) != 2 || a.Version() <= v {
		t.Fatalf("update profile/skills wrong: %+v v%d", a.Profile(), a.Version())
	}
}

// TestAvailabilityDerivation covers all four OQ2 branches (first-match-wins).
func TestAvailabilityDerivation(t *testing.T) {
	cases := []struct {
		online    bool
		lifecycle AgentLifecycle
		hasWork   bool
		want      Availability
	}{
		{false, LifecycleRunning, true, Unavailable},  // worker offline wins
		{true, LifecycleStopped, false, Unavailable},  // not running
		{true, LifecycleStopping, false, Unavailable}, // not running
		{true, LifecycleResetting, false, Unavailable},
		{true, LifecycleError, false, Unavailable},
		{true, LifecycleRunning, true, Busy},       // running + active work
		{true, LifecycleRunning, false, Available}, // running + idle
	}
	for _, c := range cases {
		if got := DeriveAvailability(c.online, c.lifecycle, c.hasWork); got != c.want {
			t.Errorf("Derive(online=%v,%s,work=%v) = %s, want %s", c.online, c.lifecycle, c.hasWork, got, c.want)
		}
	}
	// method form
	a := newAgent(t)
	_ = a.Start(t0)
	if a.Availability(true, false) != Available {
		t.Fatal("method form")
	}
}

func TestRehydrateInvalidLifecycle(t *testing.T) {
	if _, err := RehydrateAgent(RehydrateAgentInput{Lifecycle: "bad", Version: 1}); err != ErrInvalidLifecycle {
		t.Fatalf("want ErrInvalidLifecycle, got %v", err)
	}
	if _, err := RehydrateAgent(RehydrateAgentInput{Lifecycle: LifecycleStopped, Version: 0}); err == nil {
		t.Fatal("version<1 should fail")
	}
}

func TestIdentityRefValidate(t *testing.T) {
	for _, ok := range []IdentityRef{"system", "user:x", "agent:y"} {
		if err := ok.Validate(); err != nil {
			t.Fatalf("%s valid: %v", ok, err)
		}
	}
	for _, bad := range []IdentityRef{"", "nope", "user:"} {
		if bad.Validate() == nil {
			t.Fatalf("%s should be invalid", bad)
		}
	}
	_ = AgentID("a").String()
	_ = IdentityRef("user:x").String()
}
