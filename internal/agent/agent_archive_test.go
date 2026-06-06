package agent

import "testing"

func TestAgentLifecycle_IsValidArchived(t *testing.T) {
	if !LifecycleArchived.IsValid() {
		t.Fatal("archived must be a valid lifecycle")
	}
}

func TestAgentArchive_FromStopped(t *testing.T) {
	a := newAgent(t) // starts stopped, WorkerID W1
	if err := a.Archive(t0); err != nil {
		t.Fatalf("archive stopped: %v", err)
	}
	if a.Lifecycle() != LifecycleArchived {
		t.Fatalf("lifecycle = %s want archived", a.Lifecycle())
	}
	if a.WorkerID() != "" {
		t.Fatalf("archive must clear the worker binding, got %q", a.WorkerID())
	}
	if a.Version() != 2 {
		t.Fatalf("version = %d want 2 (bumped)", a.Version())
	}
}

func TestAgentArchive_FromErrorAndFailed(t *testing.T) {
	for _, lc := range []AgentLifecycle{LifecycleError, LifecycleFailed} {
		a, err := RehydrateAgent(RehydrateAgentInput{
			ID: "A1", OrganizationID: "org", Profile: Profile{Name: "x"}, WorkerID: "W1",
			Lifecycle: lc, CreatedBy: "user:a", CreatedAt: t0, UpdatedAt: t0, Version: 1,
		})
		if err != nil {
			t.Fatal(err)
		}
		if aerr := a.Archive(t0); aerr != nil {
			t.Fatalf("archive from %s: %v", lc, aerr)
		}
		if a.Lifecycle() != LifecycleArchived || a.WorkerID() != "" {
			t.Fatalf("from %s: lifecycle=%s worker=%q", lc, a.Lifecycle(), a.WorkerID())
		}
	}
}

func TestAgentArchive_RunningRejected_MustStopFirst(t *testing.T) {
	a := newAgent(t)
	if err := a.Start(t0); err != nil {
		t.Fatal(err)
	}
	if err := a.Archive(t0); err != ErrAgentNotStoppedForArchive {
		t.Fatalf("archive running want ErrAgentNotStoppedForArchive, got %v", err)
	}
	// Transitioning states also rejected.
	a2 := newAgent(t)
	_ = a2.Start(t0)
	_ = a2.Stop(t0) // → stopping
	if err := a2.Archive(t0); err != ErrAgentNotStoppedForArchive {
		t.Fatalf("archive stopping want ErrAgentNotStoppedForArchive, got %v", err)
	}
}

func TestAgentArchive_Idempotent(t *testing.T) {
	a := newAgent(t)
	if err := a.Archive(t0); err != nil {
		t.Fatal(err)
	}
	if err := a.Archive(t0); err != ErrAgentAlreadyArchived {
		t.Fatalf("re-archive want ErrAgentAlreadyArchived, got %v", err)
	}
}

func TestAgentArchive_StartRejected_Terminal(t *testing.T) {
	a := newAgent(t)
	if err := a.Archive(t0); err != nil {
		t.Fatal(err)
	}
	if err := a.Start(t0); err != ErrAgentArchived {
		t.Fatalf("start archived want ErrAgentArchived (400-class), got %v", err)
	}
}
