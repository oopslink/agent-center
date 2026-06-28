package agent

import (
	"reflect"
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

// TestAgent_IdentityMemberID — v2.7 #157: an execution Agent created via the
// unified Members→Add Agent flow carries the identity-member id it represents
// (so Members can navigate member→AgentDetail by member.identity_id == this).
// It holds the identity-member's identity ID ("agent-<ulid>"), NOT an ADR-0033
// actor ref. Optional: a bare POST /api/agents create omits it (empty).
func TestAgent_IdentityMemberID(t *testing.T) {
	a, err := NewAgent(NewAgentInput{
		ID: "A1", OrganizationID: "org", Profile: Profile{Name: "x"},
		WorkerID: "W1", CreatedBy: "user:a", CreatedAt: t0, IdentityMemberID: "agent-bot-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.IdentityMemberID() != "agent-bot-1" {
		t.Fatalf("IdentityMemberID = %q, want agent-bot-1", a.IdentityMemberID())
	}
	// Roundtrip through the repo rehydrate path must preserve it.
	r, err := RehydrateAgent(RehydrateAgentInput{
		ID: "A1", OrganizationID: "org", Profile: Profile{Name: "x"}, WorkerID: "W1",
		Lifecycle: LifecycleStopped, CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0,
		Version: 1, IdentityMemberID: "agent-bot-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if r.IdentityMemberID() != "agent-bot-1" {
		t.Fatalf("rehydrate IdentityMemberID lost: %q", r.IdentityMemberID())
	}
	// Omitted → empty (the standalone execution-agent create path).
	a2, err := NewAgent(NewAgentInput{
		ID: "A2", OrganizationID: "org", Profile: Profile{Name: "x"},
		WorkerID: "W1", CreatedBy: "user:a", CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a2.IdentityMemberID() != "" {
		t.Fatalf("unset IdentityMemberID should be empty, got %q", a2.IdentityMemberID())
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
}

// mustNewAgent is a helper that creates an agent with specific IDs for path tests.
func mustNewAgent(t *testing.T, id, orgID, workerID string) *Agent {
	t.Helper()
	a, err := NewAgent(NewAgentInput{
		ID: AgentID(id), OrganizationID: orgID,
		Profile:   Profile{Name: "coder", Model: "claude", CLI: "claudecode"},
		WorkerID:  workerID,
		CreatedBy: "user:a", CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAgent_HomeSubdirs_NewLayout(t *testing.T) {
	// Design §3: memory, plans, tasks (no config/logs/tmp/workspace)
	want := []string{"memory", "plans", "tasks"}
	if !reflect.DeepEqual(HomeSubdirs, want) {
		t.Errorf("HomeSubdirs = %v, want %v", HomeSubdirs, want)
	}
}

func TestAgent_TasksDirRel(t *testing.T) {
	a := mustNewAgent(t, "ag-1", "org-1", "w-1")
	got := a.TasksDirRel()
	want := "workers/w-1/agents/ag-1/tasks"
	if got != want {
		t.Errorf("TasksDirRel() = %q, want %q", got, want)
	}
}

func TestAgent_PlansDirRel(t *testing.T) {
	a := mustNewAgent(t, "ag-1", "org-1", "w-1")
	got := a.PlansDirRel()
	want := "workers/w-1/agents/ag-1/plans"
	if got != want {
		t.Errorf("PlansDirRel() = %q, want %q", got, want)
	}
}

func TestAgent_MemoryDirRel(t *testing.T) {
	a := mustNewAgent(t, "ag-1", "org-1", "w-1")
	got := a.MemoryDirRel()
	want := "workers/w-1/agents/ag-1/memory"
	if got != want {
		t.Errorf("MemoryDirRel() = %q, want %q", got, want)
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
	// Scope validation runs before the precondition (from stopped).
	if err := a.Reset("bogus", t0); err != ErrInvalidResetScope {
		t.Fatalf("invalid scope want ErrInvalidResetScope, got %v", err)
	}
	// stopped → resetting (the happy path; a fresh agent is stopped).
	if err := a.Reset(ResetAll, t0); err != nil {
		t.Fatal(err)
	}
	if a.Lifecycle() != LifecycleResetting {
		t.Fatal("should be resetting")
	}
	// resetting again is rejected by the W5 precondition (not a settled state).
	if err := a.Reset(ResetMemory, t0); err != ErrResetRequiresStopped {
		t.Fatalf("double reset want ErrResetRequiresStopped, got %v", err)
	}
	if err := a.MarkStopped(t0); err != nil {
		t.Fatal(err)
	}
	if a.Lifecycle() != LifecycleStopped {
		t.Fatal("reset should settle to stopped")
	}
}

// TestAgentReset_PreconditionByState — v2.16 W5 (design §3.1): Reset is legal
// ONLY from a settled lifecycle (stopped / error / failed); running / stopping /
// resetting are rejected with ErrResetRequiresStopped and leave the lifecycle
// untouched.
func TestAgentReset_PreconditionByState(t *testing.T) {
	// --- allowed: settled states transition to resetting -----------------
	t.Run("stopped_ok", func(t *testing.T) {
		a := newAgent(t) // fresh → stopped
		if err := a.Reset(ResetAll, t0); err != nil {
			t.Fatalf("reset from stopped: %v", err)
		}
		if a.Lifecycle() != LifecycleResetting {
			t.Fatalf("want resetting, got %s", a.Lifecycle())
		}
	})
	t.Run("error_ok", func(t *testing.T) {
		a := newAgent(t)
		a.MarkError("boom", t0)
		if err := a.Reset(ResetMemory, t0); err != nil {
			t.Fatalf("reset from error: %v", err)
		}
		if a.Lifecycle() != LifecycleResetting {
			t.Fatalf("want resetting, got %s", a.Lifecycle())
		}
	})
	t.Run("failed_ok", func(t *testing.T) {
		a := newAgent(t)
		// reach failed via running → failed (the terminal circuit-breaker).
		if err := a.Start(t0); err != nil {
			t.Fatal(err)
		}
		if err := a.MarkFailed("crash-loop", t0); err != nil {
			t.Fatal(err)
		}
		if a.Lifecycle() != LifecycleFailed {
			t.Fatalf("setup: want failed, got %s", a.Lifecycle())
		}
		if err := a.Reset(ResetAll, t0); err != nil {
			t.Fatalf("reset from failed: %v", err)
		}
		if a.Lifecycle() != LifecycleResetting {
			t.Fatalf("want resetting, got %s", a.Lifecycle())
		}
	})

	// --- rejected: non-settled states keep their lifecycle ----------------
	t.Run("running_rejected", func(t *testing.T) {
		a := newAgent(t)
		if err := a.Start(t0); err != nil {
			t.Fatal(err)
		}
		if err := a.Reset(ResetAll, t0); err != ErrResetRequiresStopped {
			t.Fatalf("reset from running want ErrResetRequiresStopped, got %v", err)
		}
		if a.Lifecycle() != LifecycleRunning {
			t.Fatalf("lifecycle mutated on rejected reset: %s", a.Lifecycle())
		}
	})
	t.Run("stopping_rejected", func(t *testing.T) {
		a := newAgent(t)
		if err := a.Start(t0); err != nil {
			t.Fatal(err)
		}
		if err := a.Stop(t0); err != nil {
			t.Fatal(err)
		}
		if a.Lifecycle() != LifecycleStopping {
			t.Fatalf("setup: want stopping, got %s", a.Lifecycle())
		}
		if err := a.Reset(ResetAll, t0); err != ErrResetRequiresStopped {
			t.Fatalf("reset from stopping want ErrResetRequiresStopped, got %v", err)
		}
		if a.Lifecycle() != LifecycleStopping {
			t.Fatalf("lifecycle mutated on rejected reset: %s", a.Lifecycle())
		}
	})
	t.Run("resetting_rejected", func(t *testing.T) {
		a := newAgent(t) // stopped
		if err := a.Reset(ResetAll, t0); err != nil {
			t.Fatal(err)
		}
		if err := a.Reset(ResetAll, t0); err != ErrResetRequiresStopped {
			t.Fatalf("reset from resetting want ErrResetRequiresStopped, got %v", err)
		}
		if a.Lifecycle() != LifecycleResetting {
			t.Fatalf("lifecycle mutated on rejected reset: %s", a.Lifecycle())
		}
	})

	// Invalid scope is rejected before the precondition even from a non-settled
	// state (validation order is scope → precondition).
	t.Run("scope_before_precondition", func(t *testing.T) {
		a := newAgent(t)
		if err := a.Start(t0); err != nil {
			t.Fatal(err)
		}
		if err := a.Reset("bogus", t0); err != ErrInvalidResetScope {
			t.Fatalf("invalid scope from running want ErrInvalidResetScope, got %v", err)
		}
	})
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

// TestAgentFailed pins the v2.7 terminal crash-loop state (GATE-7 Mode-B): MarkFailed
// from running/error → failed (with cause), illegal from stopped, and the manual
// recovery paths (Start/Reset) out of terminal-failed.
func TestAgentFailed(t *testing.T) {
	if !LifecycleFailed.IsValid() {
		t.Fatal("LifecycleFailed must be a valid lifecycle")
	}

	// running → failed.
	a := newAgent(t)
	if err := a.Start(t0); err != nil {
		t.Fatal(err)
	}
	if err := a.MarkFailed("crash-loop", t0); err != nil {
		t.Fatalf("running→failed: %v", err)
	}
	if a.Lifecycle() != LifecycleFailed || a.LifecycleError() != "crash-loop" {
		t.Fatalf("want failed+cause, got %s / %q", a.Lifecycle(), a.LifecycleError())
	}
	// Manual recovery: Start clears terminal-failed → running (cause cleared).
	if err := a.Start(t0); err != nil {
		t.Fatalf("manual Start out of failed: %v", err)
	}
	if a.Lifecycle() != LifecycleRunning || a.LifecycleError() != "" {
		t.Fatal("Start from failed must clear to running")
	}

	// error → failed, then Reset out of failed.
	b := newAgent(t)
	b.MarkError("boom", t0)
	if err := b.MarkFailed("gave up", t0); err != nil {
		t.Fatalf("error→failed: %v", err)
	}
	if err := b.Reset(ResetAll, t0); err != nil {
		t.Fatalf("Reset out of failed must be allowed: %v", err)
	}
	if b.Lifecycle() != LifecycleResetting {
		t.Fatal("Reset from failed → resetting")
	}

	// illegal: MarkFailed from a stopped agent (not running/error).
	c := newAgent(t) // fresh = stopped
	if err := c.MarkFailed("x", t0); err != ErrIllegalLifecycle {
		t.Fatalf("MarkFailed from stopped want ErrIllegalLifecycle, got %v", err)
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
