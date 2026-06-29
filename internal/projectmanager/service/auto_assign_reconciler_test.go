package service

import (
	"context"
	"sync"
	"testing"
	"time"

	agentpkg "github.com/oopslink/agent-center/internal/agent"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	"github.com/oopslink/agent-center/internal/workforce"
)

// ── fakes ─────────────────────────────────────────────────────────────────────

// fakeAutoAssignDir is a programmable AutoAssignDirectory: tests set the per-org
// candidate snapshots directly, bypassing the real agent/worker repos.
type fakeAutoAssignDir struct{ byOrg map[string][]AutoAssignCandidate }

func (f *fakeAutoAssignDir) ListAutoAssignCandidates(_ context.Context, org string) ([]AutoAssignCandidate, error) {
	return f.byOrg[org], nil
}

// fakeSettings is an in-memory settings.Store for the per-project master switch.
type fakeSettings struct{ m map[string]string }

func (f *fakeSettings) Get(_ context.Context, key string) (string, bool, error) {
	v, ok := f.m[key]
	return v, ok, nil
}
func (f *fakeSettings) GetByPrefix(_ context.Context, prefix string) (map[string]string, error) {
	out := map[string]string{}
	for k, v := range f.m {
		if len(k) >= len(prefix) && k[:len(prefix)] == prefix {
			out[k] = v
		}
	}
	return out, nil
}
func (f *fakeSettings) Set(_ context.Context, key, value string) error {
	f.m[key] = value
	return nil
}

// autoAssignInject wires a fresh plan-advance harness with the BE-2 auto-assign deps
// (an empty programmable directory + an in-memory settings store, default ON).
func autoAssignInject(t *testing.T) (*planAdvanceHarness, *fakeAutoAssignDir, *fakeSettings) {
	t.Helper()
	h := planAdvanceSetup(t)
	dir := &fakeAutoAssignDir{byOrg: map[string][]AutoAssignCandidate{}}
	st := &fakeSettings{m: map[string]string{}}
	h.svc.autoAssignDir = dir
	h.svc.autoAssignSettings = st
	return h, dir, st
}

// poolTaskCaps seeds a dispatched (open-claimable) builtin-pool task carrying the
// given required_capabilities. Returns (projectID, poolPlan, taskID).
func poolTaskCaps(t *testing.T, h *planAdvanceHarness, org, projName string, caps []string) (pm.ProjectID, *pm.Plan, pm.TaskID) {
	t.Helper()
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: org, Name: projName, CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	pool := findBuiltinPlan(t, h, pid)
	tid, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "pool task", CreatedBy: "user:a", RequiredCapabilities: caps})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), tid, "user:a"); err != nil {
		t.Fatalf("SelectTaskIntoPlan: %v", err)
	}
	if err := h.svc.ReconcileRunningPlans(h.ctx, nil); err != nil {
		t.Fatalf("ReconcileRunningPlans: %v", err)
	}
	return pid, pool, tid
}

func cand(ref string, online, autoAssignable bool, cap int, tags ...string) AutoAssignCandidate {
	return AutoAssignCandidate{
		AgentRef: pm.IdentityRef(ref), Online: online, AutoAssignable: autoAssignable,
		CapabilityTags: pm.NormalizeCapabilities(tags), ConcurrencyCap: cap,
	}
}

func assigneeOf(t *testing.T, h *planAdvanceHarness, tid pm.TaskID) pm.IdentityRef {
	t.Helper()
	got, err := h.svc.GetTask(h.ctx, tid)
	if err != nil {
		t.Fatal(err)
	}
	return got.Assignee()
}

// ── behaviour ─────────────────────────────────────────────────────────────────

// strict capability gate HIT: required ⊆ tags → assigned to the capable agent, and
// the task stays OPEN (claim→open semantics; the agent start_tasks it when woken).
func TestAutoAssign_StrictHit(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	pid, _, tid := poolTaskCaps(t, h, "org-1", "P", []string{"go"})
	addMember(t, h, pid, "agent:m1")
	dir.byOrg["org-1"] = []AutoAssignCandidate{cand("agent:m1", true, true, 1, "go", "backend")}

	n, err := h.svc.AutoAssignSweep(h.ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 1 {
		t.Fatalf("assigned=%d, want 1", n)
	}
	if got := assigneeOf(t, h, tid); got != "agent:m1" {
		t.Fatalf("assignee=%q, want agent:m1", got)
	}
	got, _ := h.svc.GetTask(h.ctx, tid)
	if got.Status() != pm.TaskOpen {
		t.Fatalf("status=%s, want open (claim→open)", got.Status())
	}
}

// strict capability gate MISS: no capable agent → task STAYS in the pool (strict, no
// fallback) AND is STARVED (required non-empty, no eligible online agent).
func TestAutoAssign_StrictMiss_StaysPooled_Starved(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	pid, pool, tid := poolTaskCaps(t, h, "org-1", "P", []string{"rust"})
	addMember(t, h, pid, "agent:m1")
	dir.byOrg["org-1"] = []AutoAssignCandidate{cand("agent:m1", true, true, 1, "go")}

	n, err := h.svc.AutoAssignSweep(h.ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 0 {
		t.Fatalf("assigned=%d, want 0 (strict miss)", n)
	}
	if got := assigneeOf(t, h, tid); got != "" {
		t.Fatalf("assignee=%q, want empty (stays pooled)", got)
	}
	// Observable starvation via the FE-facing plan detail.
	detail, err := h.svc.GetPlanDetail(h.ctx, pool.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !detail.Starved[tid] {
		t.Fatalf("starved=%v, want true (required caps, no capable agent)", detail.Starved[tid])
	}
}

// required EMPTY ⇒ unrestricted: any otherwise-eligible agent may take it, and it is
// NEVER starved.
func TestAutoAssign_RequiredEmpty_AnyEligible(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	pid, pool, tid := poolTaskCaps(t, h, "org-1", "P", nil)
	addMember(t, h, pid, "agent:m1")
	dir.byOrg["org-1"] = []AutoAssignCandidate{cand("agent:m1", true, true, 1)} // no tags

	if _, err := h.svc.AutoAssignSweep(h.ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := assigneeOf(t, h, tid); got != "agent:m1" {
		t.Fatalf("assignee=%q, want agent:m1 (unrestricted)", got)
	}
	detail, _ := h.svc.GetPlanDetail(h.ctx, pool.ID())
	if detail.Starved[tid] {
		t.Fatal("a task with no required caps must never be starved")
	}
}

// case-insensitive gate: task wants "go", the agent is labelled "Go"/"Backend" (the
// directory canonicalises tags via NormalizeCapabilities, as the real adapter does).
func TestAutoAssign_CapabilityCaseInsensitive(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	pid, _, tid := poolTaskCaps(t, h, "org-1", "P", []string{"go"})
	addMember(t, h, pid, "agent:m1")
	dir.byOrg["org-1"] = []AutoAssignCandidate{cand("agent:m1", true, true, 1, "Go", "Backend")}

	if _, err := h.svc.AutoAssignSweep(h.ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := assigneeOf(t, h, tid); got != "agent:m1" {
		t.Fatalf("assignee=%q, want agent:m1 (Go matches go)", got)
	}
}

// per-agent opt-out: a capable agent with auto_assignable=false is excluded → the task
// stays pooled.
func TestAutoAssign_OptOutExcluded(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	pid, _, tid := poolTaskCaps(t, h, "org-1", "P", []string{"go"})
	addMember(t, h, pid, "agent:m1")
	dir.byOrg["org-1"] = []AutoAssignCandidate{cand("agent:m1", true, false /*opt-out*/, 1, "go")}

	if _, err := h.svc.AutoAssignSweep(h.ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := assigneeOf(t, h, tid); got != "" {
		t.Fatalf("assignee=%q, want empty (opted out)", got)
	}
}

// offline agents are excluded.
func TestAutoAssign_OfflineExcluded(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	pid, _, tid := poolTaskCaps(t, h, "org-1", "P", []string{"go"})
	addMember(t, h, pid, "agent:m1")
	dir.byOrg["org-1"] = []AutoAssignCandidate{cand("agent:m1", false /*offline*/, true, 1, "go")}

	if _, err := h.svc.AutoAssignSweep(h.ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := assigneeOf(t, h, tid); got != "" {
		t.Fatalf("assignee=%q, want empty (offline)", got)
	}
}

// non-member agents are excluded even when online + capable.
func TestAutoAssign_NonMemberExcluded(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	_, _, tid := poolTaskCaps(t, h, "org-1", "P", []string{"go"})
	// m1 is NOT added as a project member.
	dir.byOrg["org-1"] = []AutoAssignCandidate{cand("agent:m1", true, true, 1, "go")}

	if _, err := h.svc.AutoAssignSweep(h.ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := assigneeOf(t, h, tid); got != "" {
		t.Fatalf("assignee=%q, want empty (non-member)", got)
	}
}

// least-busy selection: two eligible agents; m2 already runs a task (1 running, cap 1
// → no free slot), so the pool task goes to the idle m1.
func TestAutoAssign_LeastBusy_FreeSlot(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	pid, _, tid := poolTaskCaps(t, h, "org-1", "P", []string{"go"})
	addMember(t, h, pid, "agent:m1")
	addMember(t, h, pid, "agent:m2")

	// Make m2 busy: assign + start a separate task (the harness AgentDir caps at 1).
	busy, err := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "busy", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	if err := h.svc.AssignTask(h.ctx, busy, "agent:m2", "user:a"); err != nil {
		t.Fatalf("assign busy: %v", err)
	}
	if err := h.svc.StartTask(h.ctx, busy, "agent:m2"); err != nil {
		t.Fatalf("start busy: %v", err)
	}
	dir.byOrg["org-1"] = []AutoAssignCandidate{
		cand("agent:m1", true, true, 1, "go"),
		cand("agent:m2", true, true, 1, "go"),
	}

	if _, err := h.svc.AutoAssignSweep(h.ctx); err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if got := assigneeOf(t, h, tid); got != "agent:m1" {
		t.Fatalf("assignee=%q, want agent:m1 (m2 has no free slot)", got)
	}
}

// project master switch OFF ⇒ no-op even with a perfectly eligible agent.
func TestAutoAssign_ProjectDisabled_NoOp(t *testing.T) {
	h, dir, st := autoAssignInject(t)
	pid, _, tid := poolTaskCaps(t, h, "org-1", "P", []string{"go"})
	addMember(t, h, pid, "agent:m1")
	dir.byOrg["org-1"] = []AutoAssignCandidate{cand("agent:m1", true, true, 1, "go")}
	st.m["auto_assign.enabled."+string(pid)] = "false" // explicit opt-out

	n, err := h.svc.AutoAssignSweep(h.ctx)
	if err != nil {
		t.Fatalf("sweep: %v", err)
	}
	if n != 0 {
		t.Fatalf("assigned=%d, want 0 (project disabled)", n)
	}
	if got := assigneeOf(t, h, tid); got != "" {
		t.Fatalf("assignee=%q, want empty (project disabled)", got)
	}
}

// no-directory ⇒ a strict no-op (pool stays claim-only, pre-BE-2 behaviour).
func TestAutoAssign_NoDirectory_NoOp(t *testing.T) {
	h := planAdvanceSetup(t)
	h.svc.autoAssignDir = nil
	pid, err := h.svc.CreateProject(h.ctx, CreateProjectCommand{OrganizationID: "org-1", Name: "P", CreatedBy: "user:a"})
	if err != nil {
		t.Fatal(err)
	}
	pool := findBuiltinPlan(t, h, pid)
	tid, _ := h.svc.CreateTask(h.ctx, CreateTaskCommand{ProjectID: pid, Title: "x", CreatedBy: "user:a"})
	_ = h.svc.SelectTaskIntoPlan(h.ctx, pool.ID(), tid, "user:a")
	_ = h.svc.ReconcileRunningPlans(h.ctx, nil)
	if n, err := h.svc.AutoAssignSweep(h.ctx); err != nil || n != 0 {
		t.Fatalf("sweep n=%d err=%v, want 0,nil", n, err)
	}
}

// race-safety: many concurrent sweeps + a concurrent claim_task on ONE pool task all
// converge on the SAME open+unassigned CAS — the task ends assigned to EXACTLY one
// agent, never double-assigned. Run under -race.
func TestAutoAssign_ConcurrentSweeps_NoDoubleAssign(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	pid, _, tid := poolTaskCaps(t, h, "org-1", "P", []string{"go"})
	addMember(t, h, pid, "agent:m1")
	addMember(t, h, pid, "agent:m2")
	addMember(t, h, pid, "agent:m3")
	dir.byOrg["org-1"] = []AutoAssignCandidate{
		cand("agent:m1", true, true, 2, "go"),
		cand("agent:m2", true, true, 2, "go"),
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = h.svc.AutoAssignSweep(context.Background())
		}()
	}
	// A concurrent manual claim by a third member races the sweeps on the same CAS.
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = h.svc.ClaimPoolTask(context.Background(), tid, "agent:m3")
	}()
	wg.Wait()

	got := assigneeOf(t, h, tid)
	switch got {
	case "agent:m1", "agent:m2", "agent:m3":
		// exactly one winner — good.
	default:
		t.Fatalf("assignee=%q, want one of m1/m2/m3 (single CAS winner)", got)
	}
	// A follow-up sweep must NOT re-assign (already owned → skipped).
	if n, err := h.svc.AutoAssignSweep(h.ctx); err != nil || n != 0 {
		t.Fatalf("post sweep n=%d err=%v, want 0,nil (already assigned)", n, err)
	}
	if assigneeOf(t, h, tid) != got {
		t.Fatalf("assignee changed after a no-op sweep, want stable %q", got)
	}
}

// the event-driven trigger path (project-scoped) assigns the same as the periodic
// sweep, and is a no-op for a project with no builtin pool / disabled switch.
func TestAutoAssign_TriggerForProject(t *testing.T) {
	h, dir, _ := autoAssignInject(t)
	pid, _, tid := poolTaskCaps(t, h, "org-1", "P", []string{"go"})
	addMember(t, h, pid, "agent:m1")
	dir.byOrg["org-1"] = []AutoAssignCandidate{cand("agent:m1", true, true, 1, "go")}

	n, err := h.svc.TriggerAutoAssignForProject(h.ctx, pid)
	if err != nil {
		t.Fatalf("trigger: %v", err)
	}
	if n != 1 || assigneeOf(t, h, tid) != "agent:m1" {
		t.Fatalf("trigger assigned n=%d assignee=%q, want 1/agent:m1", n, assigneeOf(t, h, tid))
	}
}

// ── unit: capability gate ──────────────────────────────────────────────────────

func TestCapabilityGatePasses(t *testing.T) {
	cases := []struct {
		name     string
		required []string
		tags     []string
		want     bool
	}{
		{"empty required ⇒ pass", nil, []string{"go"}, true},
		{"subset ⇒ pass", []string{"go"}, []string{"go", "backend"}, true},
		{"exact ⇒ pass", []string{"go", "backend"}, []string{"backend", "go"}, true},
		{"missing one ⇒ fail", []string{"go", "rust"}, []string{"go"}, false},
		{"none ⇒ fail", []string{"rust"}, []string{"go"}, false},
		{"required but no tags ⇒ fail", []string{"go"}, nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := capabilityGatePasses(c.required, c.tags); got != c.want {
				t.Fatalf("capabilityGatePasses(%v,%v)=%v, want %v", c.required, c.tags, got, c.want)
			}
		})
	}
}

// ── unit: directory adapter normalises agent capability_tags (case-insensitive) ──

// minimal agent.Repository: only ListByOrg is exercised; the rest panic if called.
type adapterAgentRepo struct {
	agentpkg.Repository
	agents []*agentpkg.Agent
}

func (r *adapterAgentRepo) ListByOrg(_ context.Context, _ string) ([]*agentpkg.Agent, error) {
	return r.agents, nil
}

// minimal worker repo: FindByID returns a fixed online worker.
type adapterWorkerRepo struct {
	workforce.WorkerRepository
	w *workforce.Worker
}

func (r *adapterWorkerRepo) FindByID(_ context.Context, _ workforce.WorkerID) (*workforce.Worker, error) {
	return r.w, nil
}

func TestAgentAutoAssignDirectory_NormalisesTagsAndOnline(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	w, err := workforce.RehydrateWorker(workforce.RehydrateWorkerInput{
		ID: "w1", Name: "w", Status: workforce.WorkerOnline, OrganizationID: "org-1",
		EnrolledAt: now, CreatedAt: now, UpdatedAt: now, Version: 1,
	})
	if err != nil {
		t.Fatalf("worker: %v", err)
	}
	ag, err := agentpkg.NewAgent(agentpkg.NewAgentInput{
		ID: "a1", OrganizationID: "org-1", WorkerID: "w1",
		Profile:          agentpkg.Profile{Name: "A", AutoAssignable: true},
		CapabilityTags:   []string{"Go", "  Backend ", "go"}, // mixed case + dup + ws
		IdentityMemberID: "agent-m1",
		CreatedBy:        "user:a", CreatedAt: now,
	})
	if err != nil {
		t.Fatalf("agent: %v", err)
	}
	dir := NewAgentAutoAssignDirectory(&adapterAgentRepo{agents: []*agentpkg.Agent{ag}}, &adapterWorkerRepo{w: w})
	cands, err := dir.ListAutoAssignCandidates(context.Background(), "org-1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(cands) != 1 {
		t.Fatalf("candidates=%d, want 1", len(cands))
	}
	c := cands[0]
	if c.AgentRef != "agent:agent-m1" {
		t.Fatalf("ref=%q, want agent:agent-m1 (identity-member form)", c.AgentRef)
	}
	if !c.Online {
		t.Fatal("want online (worker online)")
	}
	// canonical: trimmed, lowercased, deduped — "go" matches a task requiring "go".
	if !capabilityGatePasses([]string{"go"}, c.CapabilityTags) {
		t.Fatalf("tags=%v, want to satisfy required {go}", c.CapabilityTags)
	}
	if !capabilityGatePasses([]string{"backend"}, c.CapabilityTags) {
		t.Fatalf("tags=%v, want to satisfy required {backend}", c.CapabilityTags)
	}
}
