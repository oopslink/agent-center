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

// T728: the include_description_in_system_prompt flag survives Save→Find and
// Update→Find for both values.
func TestAgentRepo_IncludeDescriptionInSystemPrompt_RoundTrip(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	for _, want := range []bool{true, false} {
		id := agent.AgentID("A-" + map[bool]string{true: "on", false: "off"}[want])
		a, err := agent.NewAgent(agent.NewAgentInput{
			ID: id, OrganizationID: "org", WorkerID: "W1",
			Profile:   agent.Profile{Name: "coder", Description: "persona text", IncludeDescriptionInSystemPrompt: want},
			CreatedBy: "user:a", CreatedAt: t0,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
		got, err := r.FindByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Profile().IncludeDescriptionInSystemPrompt != want {
			t.Fatalf("save round-trip: IncludeDescriptionInSystemPrompt = %v, want %v", got.Profile().IncludeDescriptionInSystemPrompt, want)
		}
		// Update path: flip the flag and re-read.
		p := got.Profile()
		p.IncludeDescriptionInSystemPrompt = !want
		if err := got.UpdateProfile(p, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		if err := r.Update(ctx, got); err != nil {
			t.Fatal(err)
		}
		got2, err := r.FindByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got2.Profile().IncludeDescriptionInSystemPrompt != !want {
			t.Fatalf("update round-trip: IncludeDescriptionInSystemPrompt = %v, want %v", got2.Profile().IncludeDescriptionInSystemPrompt, !want)
		}
	}
}

// T728: a pre-existing agent row (one that predates the 0090 column) reads the
// migration DEFAULT 1 → true. Simulated by a raw INSERT that omits the column so it
// takes its column default, mirroring what ALTER TABLE ... DEFAULT 1 does to old rows.
func TestAgentRepo_IncludeDescriptionInSystemPrompt_LegacyRowDefaultsTrue(t *testing.T) {
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Insert only the NOT-NULL-without-default columns; every ALTER-added column
	// (incl. include_description_in_system_prompt) takes its DEFAULT.
	if _, err := d.ExecContext(ctx,
		`INSERT INTO agents (id, organization_id, name, worker_id, lifecycle, created_by, created_at, updated_at)
		 VALUES ('LEGACY','org','coder','W1','stopped','user:a',?,?)`, ts(t0), ts(t0)); err != nil {
		t.Fatalf("raw legacy insert: %v", err)
	}
	got, err := NewAgentRepo(d).FindByID(ctx, "LEGACY")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Profile().IncludeDescriptionInSystemPrompt {
		t.Fatalf("legacy row IncludeDescriptionInSystemPrompt = false, want true (migration DEFAULT 1)")
	}
}

// F3 model routing (design §5 & §10): the new profile fields
// (orchestrator_model / default_executor_model / max_concurrent_tasks /
// allowed_models) round-trip through Save → FindByID with all values preserved,
// and a non-empty AllowedModels slice survives the JSON round-trip.
func TestAgentRepo_ModelRoutingRoundTrip(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	a, err := agent.NewAgent(agent.NewAgentInput{
		ID: "AR1", OrganizationID: "org", WorkerID: "W1",
		Profile: agent.Profile{
			Name: "router", Model: "claude", CLI: "claudecode",
			OrchestratorModel:    "claude-haiku",
			DefaultExecutorModel: "claude-sonnet",
			MaxConcurrentTasks:   7,
			AllowedModels:        []string{"claude-sonnet", "claude-opus"},
		},
		CreatedBy: "user:a", CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID(ctx, "AR1")
	if err != nil {
		t.Fatal(err)
	}
	p := got.Profile()
	if p.OrchestratorModel != "claude-haiku" || p.DefaultExecutorModel != "claude-sonnet" {
		t.Fatalf("orchestrator/default executor model lost: %+v", p)
	}
	if p.MaxConcurrentTasks != 7 || p.EffectiveMaxConcurrentTasks() != 7 {
		t.Fatalf("max_concurrent_tasks lost: got %d", p.MaxConcurrentTasks)
	}
	if len(p.AllowedModels) != 2 || p.AllowedModels[0] != "claude-sonnet" || p.AllowedModels[1] != "claude-opus" {
		t.Fatalf("allowed_models lost: %#v", p.AllowedModels)
	}
}

// F3: an agent created without model-routing fields round-trips to a zero
// MaxConcurrentTasks (the column DEFAULT 3 is applied at migration time for
// pre-existing rows, but a fresh INSERT writes the domain's 0) — the domain
// helper EffectiveMaxConcurrentTasks supplies the default of 3, and an empty
// AllowedModels round-trips to an empty (non-error) slice.
func TestAgentRepo_ModelRoutingDefaults(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	a := mkAgent(t, "AR2", "W1") // no model-routing fields set
	if err := r.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID(ctx, "AR2")
	if err != nil {
		t.Fatal(err)
	}
	p := got.Profile()
	if p.EffectiveMaxConcurrentTasks() != agent.DefaultMaxConcurrentTasks {
		t.Fatalf("want default %d, got %d", agent.DefaultMaxConcurrentTasks, p.EffectiveMaxConcurrentTasks())
	}
	if len(p.AllowedModels) != 0 {
		t.Fatalf("want empty allowed_models, got %#v", p.AllowedModels)
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

// TestAgentRepo_AllowedExecutorsRoundTrip covers v2.18.1 BE-1: allowed_executors
// persists + rehydrates as the authoritative list, and allowed_models is written as
// its DERIVED mirror (distinct models) for legacy model-only readers.
func TestAgentRepo_AllowedExecutorsRoundTrip(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	a, err := agent.NewAgent(agent.NewAgentInput{
		ID: "AX", OrganizationID: "org", WorkerID: "W1",
		Profile: agent.Profile{
			Name: "coder", CLI: "claude-code", MaxConcurrentTasks: 2,
			AllowedExecutors: []agent.ExecutorProfile{
				{CLI: "claude-code", Model: "opus"},
				{CLI: "codex", Model: "gpt-5-codex"},
				{CLI: "codex", Model: "opus"}, // distinct {cli,model}, same model "opus"
			},
			// A stale AllowedModels on input must be overwritten by the derived mirror.
			AllowedModels: []string{"STALE"},
		},
		CreatedBy: "user:a", CreatedAt: t0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID(ctx, "AX")
	if err != nil {
		t.Fatal(err)
	}
	p := got.Profile()
	if len(p.AllowedExecutors) != 3 ||
		p.AllowedExecutors[0] != (agent.ExecutorProfile{CLI: "claude-code", Model: "opus"}) ||
		p.AllowedExecutors[1] != (agent.ExecutorProfile{CLI: "codex", Model: "gpt-5-codex"}) ||
		p.AllowedExecutors[2] != (agent.ExecutorProfile{CLI: "codex", Model: "opus"}) {
		t.Fatalf("allowed_executors round-trip lost data: %+v", p.AllowedExecutors)
	}
	// Derived mirror = distinct models, first-seen order; NOT the stale input.
	want := []string{"opus", "gpt-5-codex"}
	if len(p.AllowedModels) != len(want) || p.AllowedModels[0] != want[0] || p.AllowedModels[1] != want[1] {
		t.Fatalf("derived allowed_models = %v, want %v", p.AllowedModels, want)
	}
	if !p.ConcurrencyEnabled() {
		t.Fatal("agent with executors + max>0 must be concurrency-enabled")
	}
}

// TestAgentRepo_AutoAssignableRoundTrip covers v2.18.3 BE-1: auto_assignable
// persists + rehydrates (true and false).
func TestAgentRepo_AutoAssignableRoundTrip(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	mk := func(id agent.AgentID, assignable bool) {
		a, err := agent.NewAgent(agent.NewAgentInput{
			ID: id, OrganizationID: "org", WorkerID: "W1",
			Profile:   agent.Profile{Name: "coder", AutoAssignable: assignable},
			CreatedBy: "user:a", CreatedAt: t0,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := r.Save(ctx, a); err != nil {
			t.Fatal(err)
		}
	}
	mk("AY", true)
	mk("AN", false)
	for id, want := range map[agent.AgentID]bool{"AY": true, "AN": false} {
		got, err := r.FindByID(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Profile().AutoAssignable != want {
			t.Fatalf("%s auto_assignable = %v, want %v", id, got.Profile().AutoAssignable, want)
		}
	}
}

// TestAgentRepo_LastLifecycleTransitionAt_RoundTrip: the lifecycle-transition
// timestamp is persisted on Save and updated via Update (the Start path), and it
// survives Save→Find and Update→Find.
func TestAgentRepo_LastLifecycleTransitionAt_RoundTrip(t *testing.T) {
	r := newDB(t)
	ctx := context.Background()
	a := mkAgent(t, "A1", "W1")
	if err := r.Save(ctx, a); err != nil {
		t.Fatal(err)
	}
	got, err := r.FindByID(ctx, "A1")
	if err != nil {
		t.Fatal(err)
	}
	// Fresh agent: seeded to createdAt.
	if !got.LastLifecycleTransitionAt().Equal(t0) {
		t.Fatalf("save round-trip: got %v, want %v", got.LastLifecycleTransitionAt(), t0)
	}
	// Start advances the field; Update must persist it.
	tStart := t0.Add(2 * time.Hour)
	if err := got.Start(tStart); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(ctx, got); err != nil {
		t.Fatal(err)
	}
	got2, err := r.FindByID(ctx, "A1")
	if err != nil {
		t.Fatal(err)
	}
	if !got2.LastLifecycleTransitionAt().Equal(tStart) {
		t.Fatalf("update round-trip: got %v, want %v", got2.LastLifecycleTransitionAt(), tStart)
	}
}

// TestAgentRepo_LastLifecycleTransitionAt_LegacyRowBackfill: a raw row whose
// last_lifecycle_transition_at column is NULL (predates 0096) reads back as
// updated_at via the RehydrateAgent fallback.
func TestAgentRepo_LastLifecycleTransitionAt_LegacyRowBackfill(t *testing.T) {
	d, err := persistence.Open(persistence.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if err := persistence.NewMigrator(d).Up(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	upd := t0.Add(6 * time.Hour)
	// Omit last_lifecycle_transition_at → it stays NULL for this post-migration insert.
	if _, err := d.ExecContext(ctx,
		`INSERT INTO agents (id, organization_id, name, worker_id, lifecycle, created_by, created_at, updated_at)
		 VALUES ('LEGACY','org','coder','W1','running','user:a',?,?)`, ts(t0), ts(upd)); err != nil {
		t.Fatalf("raw legacy insert: %v", err)
	}
	got, err := NewAgentRepo(d).FindByID(ctx, "LEGACY")
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastLifecycleTransitionAt().Equal(upd) {
		t.Fatalf("legacy backfill: got %v, want updatedAt %v", got.LastLifecycleTransitionAt(), upd)
	}
}
