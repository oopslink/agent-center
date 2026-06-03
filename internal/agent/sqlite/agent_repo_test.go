package sqlite

import (
	"context"
	"testing"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
)

var t0 = time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC)

func newDB(t *testing.T) *AgentRepo {
	t.Helper()
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return NewAgentRepo(d)
}

func mkAgent(t *testing.T, id agent.AgentID, worker string) *agent.Agent {
	t.Helper()
	a, err := agent.NewAgent(agent.NewAgentInput{
		ID: id, OrganizationID: "org", WorkerID: worker,
		Profile: agent.Profile{Name: "coder", Description: "d", Model: "claude", CLI: "claudecode", EnvVars: map[string]string{"K": "V"}},
		Skills:  []string{"go", "rust"}, CreatedBy: "user:a", CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestAgentRepo_RoundTrip(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	a := mkAgent(t, "A1", "W1")
	if err := r.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, a); err != agent.ErrAgentExists {
		t.Fatalf("dup save want ErrAgentExists, got %v", err)
	}
	got, err := r.FindByID(ctx, "A1")
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkerID() != "W1" || got.Profile().Name != "coder" || got.Profile().EnvVars["K"] != "V" ||
		len(got.Skills()) != 2 || got.Lifecycle() != agent.LifecycleStopped {
		t.Fatalf("round-trip lost fields: %+v", got)
	}
	if _, err := r.FindByID(ctx, "nope"); err != agent.ErrAgentNotFound {
		t.Fatalf("want ErrAgentNotFound, got %v", err)
	}
}

// v2.7 #185 FINDING-J: the member→entity bridge. An agent saved with an
// identity_member_id is resolvable by it; an empty/absent id and a NULL
// identity_member_id (standalone agent) both yield ErrAgentNotFound.
func TestAgentRepo_FindByIdentityMemberID(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	withMember, err := agent.NewAgent(agent.NewAgentInput{
		ID: "01ENTITY", OrganizationID: "org", WorkerID: "W1",
		Profile: agent.Profile{Name: "bot"}, CreatedBy: "user:a", CreatedAt: t0,
		IdentityMemberID: "agent-mem1",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, withMember); err != nil {
		t.Fatal(err)
	}
	// standalone agent (no identity_member_id → NULL column).
	if err := r.Save(ctx, mkAgent(t, "01STANDALONE", "W1")); err != nil {
		t.Fatal(err)
	}

	got, err := r.FindByIdentityMemberID(ctx, "agent-mem1")
	if err != nil {
		t.Fatalf("resolve by member id: %v", err)
	}
	if got.ID() != "01ENTITY" || got.IdentityMemberID() != "agent-mem1" {
		t.Fatalf("wrong agent resolved: %+v", got)
	}
	if _, err := r.FindByIdentityMemberID(ctx, "agent-absent"); err != agent.ErrAgentNotFound {
		t.Fatalf("absent member id want ErrAgentNotFound, got %v", err)
	}
	if _, err := r.FindByIdentityMemberID(ctx, ""); err != agent.ErrAgentNotFound {
		t.Fatalf("empty member id want ErrAgentNotFound, got %v", err)
	}
	// the NULL-column standalone agent must not match an empty lookup.
	if _, err := r.FindByIdentityMemberID(ctx, "01STANDALONE"); err != agent.ErrAgentNotFound {
		t.Fatalf("entity id is not a member id, want ErrAgentNotFound, got %v", err)
	}
}

func TestAgentRepo_UpdateKeepsWorkerImmutable(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	a := mkAgent(t, "A1", "W1")
	_ = r.Save(ctx, a)

	got, _ := r.FindByID(ctx, "A1")
	_ = got.Start(t0)
	_ = got.UpdateProfile(agent.Profile{Name: "coder2"}, t0)
	got.SetSkills([]string{"python"}, t0)
	if err := r.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	re, _ := r.FindByID(ctx, "A1")
	if re.Lifecycle() != agent.LifecycleRunning || re.Profile().Name != "coder2" || len(re.Skills()) != 1 {
		t.Fatalf("update not persisted: %+v", re)
	}
	// worker_id is immutable — Update never changes it.
	if re.WorkerID() != "W1" {
		t.Fatalf("worker_id must stay W1, got %s", re.WorkerID())
	}

	// update missing
	missing := mkAgent(t, "AX", "W9")
	if err := r.Update(ctx, missing); err != agent.ErrAgentNotFound {
		t.Fatalf("update missing want ErrAgentNotFound, got %v", err)
	}
}

func TestAgentRepo_ListScoping(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	_ = r.Save(ctx, mkAgent(t, "A1", "W1"))
	_ = r.Save(ctx, mkAgent(t, "A2", "W1"))
	_ = r.Save(ctx, mkAgent(t, "A3", "W2"))

	byOrg, _ := r.ListByOrg(ctx, "org")
	if len(byOrg) != 3 {
		t.Fatalf("ListByOrg = %d, want 3", len(byOrg))
	}
	if l, _ := r.ListByOrg(ctx, "other"); len(l) != 0 {
		t.Fatalf("ListByOrg other = %d, want 0", len(l))
	}
	byW1, _ := r.ListByWorker(ctx, "W1")
	if len(byW1) != 2 {
		t.Fatalf("ListByWorker W1 = %d, want 2", len(byW1))
	}
}
