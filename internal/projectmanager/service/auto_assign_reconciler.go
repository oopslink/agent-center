package service

import (
	"context"
	"sort"
	"time"

	"github.com/oopslink/agent-center/internal/autoassign"
	"github.com/oopslink/agent-center/internal/clock"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// auto_assign_reconciler.go (v2.18.3 BE-2, issue-577a7b0e) — the auto-assign
// reconciler. It makes "drop a task into the assignment pool → an eligible idle agent
// is auto-assigned + woken to run it" true, closing the gap the issue names: before
// BE-2 an OWNERLESS pool task only ran if an agent ACTIVELY claim_task'd it (no
// background auto-claim); only an ASSIGNED dispatch minted the agent.work_available
// wake. This reconciler is the background matcher.
//
// SHAPE (mirrors lease_checker.go): a Service method that does one sweep
// (AutoAssignSweep) + an AutoAssignReconciler wrapper with a ticker Run + an explicit
// Tick for tests. Plus an event-driven entry (TriggerAutoAssignForProject) the
// outbox projector calls the instant a task enters the pool or an agent frees a slot
// — the "双轨" (dual-track) trigger: event-fast-path + periodic sweep backstop.
//
// MATCHING (all gates STRICT, decision-locked in issue-577a7b0e):
//   candidate task = open + unassigned + in a BUILTIN pool + dispatched (deps
//     trivially satisfied for a pool member) + the project's auto_assign master
//     switch ON.
//   candidate agent = project member ∩ worker online ∩ auto_assignable ∩ has a free
//     run slot (running < EffectiveConcurrencyCap, the W4c cap) ∩ STRICT capability
//     gate: required_capabilities(task) ⊆ capability_tags(agent) (both canonical,
//     case-insensitive). required empty ⇒ the gate passes for any otherwise-eligible
//     agent (unrestricted).
//   no eligible agent ⇒ the task STAYS IN THE POOL (strict — never a fallback to an
//     arbitrary idle agent). If it has required_capabilities but NO capable online
//     agent exists at all, it is STARVED (observable via the pool-node DTO, FE badge).
//
// SELECTION: among eligible agents pick the LEAST BUSY — running asc → current live
// (open+running) assigned load asc → AgentRef (stable) — so pool work spreads instead
// of piling on one agent. A per-sweep reservation makes a single sweep assign at most
// one task per free run slot (no same-tick dumping).
//
// RACE-SAFETY: the assign persists through ClaimIfUnassigned — the SAME open+unassigned
// CAS claim_task uses (claim_flow.go) — inside a tx. Two sweeps / two centers, or a
// concurrent agent claim_task, converge on that one CAS: exactly one wins, the rest
// are clean no-ops. No double-assign.

// AutoAssignDefaultTick is the cadence the periodic sweep backstop runs at (~20–30s,
// decision-locked). It is the completeness net behind the event-driven fast path: it
// catches a just-onlined agent, a freshly dependency-cleared task, or any event the
// projector missed. Short enough that an ownerless pool task is picked up promptly,
// long enough that the system-wide scan is cheap.
const AutoAssignDefaultTick = 25 * time.Second

// EvtTaskAutoAssigned is the BE-2 audit event emitted (in the assign tx) each time the
// reconciler auto-assigns a pool task to an agent — the observability trail "who got
// what and on which capabilities". It rides the same outbox as every other pm event;
// the assignee wake itself is the accompanying EvtTaskAssigned (DispatchWakeProjector).
const EvtTaskAutoAssigned = "pm.task.auto_assigned"

// taskAutoAssignedPayload is the JSON payload for EvtTaskAutoAssigned.
type taskAutoAssignedPayload struct {
	TaskID         string   `json:"task_id"`
	ProjectID      string   `json:"project_id"`
	OwnerRef       string   `json:"owner_ref"`
	Assignee       string   `json:"assignee"`
	MatchedCaps    []string `json:"matched_capabilities"`
	CandidateCount int      `json:"candidate_count"`
}

// capabilityGatePasses is the STRICT capability door: required ⊆ tags (set subset).
// BOTH sides are already canonical (NormalizeCapabilities: trimmed/lowercased/deduped)
// — the directory adapter re-canonicalises an agent's stored capability_tags and the
// domain canonicalises required_capabilities — so this is a pure case-insensitive
// subset test. An EMPTY required set ⇒ true (unrestricted: any agent satisfies it).
func capabilityGatePasses(required, tags []string) bool {
	if len(required) == 0 {
		return true
	}
	have := make(map[string]struct{}, len(tags))
	for _, t := range tags {
		have[t] = struct{}{}
	}
	for _, r := range required {
		if _, ok := have[r]; !ok {
			return false
		}
	}
	return true
}

// rankedCandidate pairs an eligible agent with the load metrics that order selection.
type rankedCandidate struct {
	ref     pm.IdentityRef
	running int // current running, unblocked tasks (W4c run-slot occupancy)
	load    int // current live (open+running, unblocked) assigned tasks — queue depth
}

// AutoAssignSweep runs ONE auto-assign pass over every claimable pool task in the
// system and assigns each to its least-busy eligible agent (BE-2). Returns the number
// auto-assigned. nil-safe: with no AutoAssignDirectory or no plans repo wired it is a
// no-op (returns 0). It is read-mostly — only a matched task takes a write (its own
// tx) — and every per-task assign re-validates inside the tx (CAS), so a task taken by
// a concurrent claim/sweep between the scan and the write is a clean skip.
func (s *Service) AutoAssignSweep(ctx context.Context) (int, error) {
	return s.autoAssignSweep(ctx, "")
}

// TriggerAutoAssignForProject is the event-driven fast path (the projector calls it
// the instant a task enters the pool or an agent frees a slot). It runs the same
// matching as the periodic sweep but SCOPED to one project, so it is cheap to fire per
// relevant event. Empty projectID ⇒ a full sweep (degrades to the periodic path).
func (s *Service) TriggerAutoAssignForProject(ctx context.Context, projectID pm.ProjectID) (int, error) {
	return s.autoAssignSweep(ctx, projectID)
}

// autoAssignSweep is the shared sweep core. scope=="" sweeps all projects (the periodic
// backstop, a global open-task scan); a non-empty scope restricts to ONE project's
// builtin pool (the event-driven fast path — cheap, no global scan). It keeps the
// builtin-pool dispatched, ownerless tasks whose project switch is ON, and assigns each
// to its least-busy eligible agent.
func (s *Service) autoAssignSweep(ctx context.Context, scope pm.ProjectID) (int, error) {
	if s.autoAssignDir == nil || s.plans == nil {
		return 0, nil // not wired ⇒ pool stays claim-only (pre-BE-2)
	}
	var tasks []*pm.Task
	var err error
	if scope == "" {
		// Periodic backstop: the system-wide open set (the matcher filters to pool tasks).
		tasks, err = s.tasks.ListByStatuses(ctx, []pm.TaskStatus{pm.TaskOpen})
	} else {
		// Event fast path: only THIS project's builtin pool, so a per-event trigger never
		// pays for a global scan. No pool / no project switch handled downstream.
		pool := s.builtinPoolOf(ctx, scope)
		if pool == nil {
			return 0, nil
		}
		tasks, err = s.tasks.ListByPlan(ctx, pool.ID())
	}
	if err != nil {
		return 0, err
	}
	sw := s.newAutoAssignSweepCtx()
	assigned := 0
	for _, snap := range tasks {
		if snap.Assignee() != "" || snap.Status() != pm.TaskOpen || snap.IsArchived() {
			continue // not an ownerless, open pool task
		}
		if scope != "" && snap.ProjectID() != scope {
			continue // defensive (ListByPlan is already scope-pure)
		}
		ok, err := s.autoAssignOne(ctx, sw, snap)
		if err != nil {
			return assigned, err
		}
		if ok {
			assigned++
		}
	}
	return assigned, nil
}

// autoAssignOne evaluates and (if a match wins) assigns a single candidate task. It
// returns true only when the task was actually auto-assigned. The passed task is the
// scan snapshot — used for the read-only gates; the authoritative re-validation +
// assign happens under CAS in autoAssignPoolTask. All cross-task caches (plans, node
// status, per-org candidates, project switch, reservations) live in sw so a sweep
// amortises the reads.
func (s *Service) autoAssignOne(ctx context.Context, sw *autoAssignSweepCtx, t *pm.Task) (bool, error) {
	planID := t.PlanID()
	if planID == "" {
		return false, nil // backlog — not in a pool
	}
	p, ns, err := sw.planNode(ctx, s, planID, t.ID())
	if err != nil {
		return false, err
	}
	if p == nil || !p.IsBuiltin() {
		return false, nil // structured-plan node: assignee-gated, not auto-claimable
	}
	if !pm.ClaimableInPool(t.IsArchived(), t.Status(), planID, ns) {
		return false, nil // not dispatched / not claimable
	}
	projectID := t.ProjectID()
	enabled, err := sw.projectEnabled(ctx, s, projectID)
	if err != nil {
		return false, err
	}
	if !enabled {
		return false, nil // project master switch OFF → no auto-assign
	}
	pick, _, err := s.selectAutoAssignee(ctx, sw, t, projectID)
	if err != nil {
		return false, err
	}
	if pick == "" {
		return false, nil // no eligible agent (strict) → stays in pool
	}
	won, matched, err := s.autoAssignPoolTask(ctx, t.ID(), pick, t.RequiredCapabilities(), sw.candidateCount)
	if err != nil {
		return false, err
	}
	if won {
		// Reserve the agent's just-filled run slot for the rest of this sweep so it is
		// not handed a second task the same tick (one task per free slot per sweep).
		sw.reserved[pick]++
		_ = matched
		return true, nil
	}
	return false, nil
}

// selectAutoAssignee returns the least-busy eligible agent for task t (or "" when none
// qualifies) plus whether t is STARVED. starved == true iff the task carries
// required_capabilities but NO online, auto_assignable, project-member agent's
// capability_tags satisfy them — i.e. no capable agent exists at all (independent of
// momentary slot load). A task with no eligible agent merely because the capable ones
// are all busy is NOT starved (it is picked up when a slot frees).
func (s *Service) selectAutoAssignee(ctx context.Context, sw *autoAssignSweepCtx, t *pm.Task, projectID pm.ProjectID) (pm.IdentityRef, bool, error) {
	required := t.RequiredCapabilities()
	cands, err := sw.candidatesForProject(ctx, s, projectID)
	if err != nil {
		return "", false, err
	}
	capableExists := false
	var eligible []rankedCandidate
	for _, c := range cands {
		if !c.Online || !c.AutoAssignable {
			continue
		}
		if !capabilityGatePasses(required, c.CapabilityTags) {
			continue
		}
		// Capable (online ∩ opt-in ∩ member ∩ capability-match), ignoring slot load —
		// this is the starvation supply test.
		capableExists = true
		running, err := s.tasks.CountRunningUnblockedByAssignee(ctx, c.AgentRef, "")
		if err != nil {
			return "", false, err
		}
		// Free run slot? running + this-sweep reservation must be under the cap.
		if running+sw.reserved[c.AgentRef] >= c.ConcurrencyCap {
			continue
		}
		load, err := s.liveAssignedLoad(ctx, c.AgentRef)
		if err != nil {
			return "", false, err
		}
		eligible = append(eligible, rankedCandidate{ref: c.AgentRef, running: running + sw.reserved[c.AgentRef], load: load + sw.reserved[c.AgentRef]})
	}
	starved := len(required) > 0 && !capableExists
	if len(eligible) == 0 {
		return "", starved, nil
	}
	// Least busy: running asc → live assigned load asc → AgentRef (stable).
	sort.Slice(eligible, func(i, j int) bool {
		a, b := eligible[i], eligible[j]
		if a.running != b.running {
			return a.running < b.running
		}
		if a.load != b.load {
			return a.load < b.load
		}
		return a.ref < b.ref
	})
	return eligible[0].ref, starved, nil
}

// liveAssignedLoad counts an agent's current live (open OR running, non-terminal)
// assigned tasks — its queue depth, the tie-break for "least busy" (总指派). Blocked
// tasks still count as held work. Bounded by the agent's assigned-task count.
func (s *Service) liveAssignedLoad(ctx context.Context, agentRef pm.IdentityRef) (int, error) {
	tasks, err := s.tasks.ListByAssignee(ctx, agentRef)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, t := range tasks {
		if t.Status() == pm.TaskOpen || t.Status() == pm.TaskRunning {
			n++
		}
	}
	return n, nil
}

// autoAssignPoolTask is the race-safe per-task assign tx: it assigns the ownerless
// pool task to agentRef through the SAME open+unassigned CAS (ClaimIfUnassigned) that
// claim_task uses, then — unlike claim_task (pull/no-wake) — emits EvtTaskAssigned so
// the DispatchWakeProjector mints the agent.work_available wake (the agent is idle and
// must be woken to start the task) and EvtTaskAutoAssigned for the audit trail. The
// assignee joins the pool conversation (additive). Returns won=false (a clean skip)
// when a concurrent claim/sweep took the task first. The task stays OPEN (assigned) —
// the W4c run-slot cap bites when the woken agent start_tasks it, exactly as a manual
// claim+start would.
func (s *Service) autoAssignPoolTask(ctx context.Context, taskID pm.TaskID, agentRef pm.IdentityRef, matchedCaps []string, candidateCount int) (won bool, matched []string, err error) {
	now := s.clock.Now()
	err = s.runInTx(ctx, func(txCtx context.Context) error {
		t, ferr := s.tasks.FindByID(txCtx, taskID)
		if ferr != nil {
			return ferr
		}
		// Re-validate inside the tx (authoritative): still an ownerless, open,
		// non-archived task. Anything else ⇒ a concurrent claim/assign won — clean skip.
		if t.Assignee() != "" || t.Status() != pm.TaskOpen || t.IsArchived() {
			won = false
			return nil
		}
		if aerr := t.Assign(agentRef, now); aerr != nil {
			return aerr
		}
		ok, cerr := s.tasks.ClaimIfUnassigned(txCtx, t)
		if cerr != nil {
			return cerr
		}
		if !ok {
			won = false // a concurrent claim took it first (converges with claim_task)
			return nil
		}
		won = true
		matched = matchedCaps
		// The dispatch wake: EvtTaskAssigned → DispatchWakeProjector wakes the assignee
		// (idle agent) so it start_tasks the freshly-assigned pool task. This is the ONE
		// difference from ClaimPoolTask (which suppresses it because the claimer is
		// already active). previousAssignee is "" — an auto-assign only ever fires on an
		// OWNERLESS pool task (first assignment), so it emits pm.task.assigned (not
		// .reassigned) with no prior assignee, exactly like the unblock re-dispatch path.
		if eerr := s.emitTaskAssignEvent(txCtx, t, EvtTaskAssigned, ""); eerr != nil {
			return eerr
		}
		// Additive: the assignee joins the pool conversation (mirrors claim).
		if perr := s.syncPlanParticipantOnAssign(txCtx, t, agentRef); perr != nil {
			return perr
		}
		// Audit trail (who/what/which caps).
		return s.emit(txCtx, EvtTaskAutoAssigned,
			refsJSON(map[string]string{"task_id": string(t.ID()), "project_id": string(t.ProjectID())}),
			taskAutoAssignedPayload{
				TaskID: string(t.ID()), ProjectID: string(t.ProjectID()),
				OwnerRef: "pm://tasks/" + string(t.ID()), Assignee: string(agentRef),
				MatchedCaps: matchedCaps, CandidateCount: candidateCount,
			})
	})
	return won, matched, err
}

// ─── per-sweep caches ────────────────────────────────────────────────────────────

// autoAssignSweepCtx amortises the cross-task reads of one sweep: resolved plans +
// node-status maps, per-org candidate lists, per-project membership-filtered
// candidates + master-switch verdicts, and the per-sweep run-slot reservations.
type autoAssignSweepCtx struct {
	plans          map[pm.PlanID]*pm.Plan
	nodeStatus     map[pm.PlanID]map[pm.TaskID]pm.NodeStatus
	orgCandidates  map[string][]AutoAssignCandidate
	projCandidates map[pm.ProjectID][]AutoAssignCandidate
	enabled        map[pm.ProjectID]bool
	reserved       map[pm.IdentityRef]int
	candidateCount int // candidates considered for the task currently being matched
}

func (s *Service) newAutoAssignSweepCtx() *autoAssignSweepCtx {
	return &autoAssignSweepCtx{
		plans:          map[pm.PlanID]*pm.Plan{},
		nodeStatus:     map[pm.PlanID]map[pm.TaskID]pm.NodeStatus{},
		orgCandidates:  map[string][]AutoAssignCandidate{},
		projCandidates: map[pm.ProjectID][]AutoAssignCandidate{},
		enabled:        map[pm.ProjectID]bool{},
		reserved:       map[pm.IdentityRef]int{},
	}
}

// planNode resolves (and caches) a plan + the derived node status of taskID within it.
func (sw *autoAssignSweepCtx) planNode(ctx context.Context, s *Service, planID pm.PlanID, taskID pm.TaskID) (*pm.Plan, pm.NodeStatus, error) {
	p, ok := sw.plans[planID]
	if !ok {
		var err error
		p, err = s.plans.FindByID(ctx, planID)
		if err != nil {
			return nil, "", err
		}
		sw.plans[planID] = p
	}
	if p == nil {
		return nil, "", nil
	}
	nsMap, ok := sw.nodeStatus[planID]
	if !ok {
		detail, err := s.planDetail(ctx, p)
		if err != nil {
			return nil, "", err
		}
		nsMap = make(map[pm.TaskID]pm.NodeStatus, len(detail.View.Nodes))
		for _, n := range detail.View.Nodes {
			nsMap[n.TaskID] = n.NodeStatus
		}
		sw.nodeStatus[planID] = nsMap
	}
	return p, nsMap[taskID], nil
}

// projectEnabled resolves (and caches) a project's auto_assign master switch.
func (sw *autoAssignSweepCtx) projectEnabled(ctx context.Context, s *Service, projectID pm.ProjectID) (bool, error) {
	if v, ok := sw.enabled[projectID]; ok {
		return v, nil
	}
	v, err := autoassign.Enabled(ctx, s.autoAssignSettings, string(projectID))
	if err != nil {
		return false, err
	}
	sw.enabled[projectID] = v
	return v, nil
}

// candidatesForProject returns the org's auto-assign candidates filtered to the
// project's members (cached per project). The reconciler applies the online/opt-out/
// capability/slot gates on top of this set. It also stamps sw.candidateCount with the
// project-member candidate count for the audit payload.
func (sw *autoAssignSweepCtx) candidatesForProject(ctx context.Context, s *Service, projectID pm.ProjectID) ([]AutoAssignCandidate, error) {
	if v, ok := sw.projCandidates[projectID]; ok {
		sw.candidateCount = len(v)
		return v, nil
	}
	proj, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		return nil, err
	}
	org := proj.OrganizationID()
	orgCands, ok := sw.orgCandidates[org]
	if !ok {
		orgCands, err = s.autoAssignDir.ListAutoAssignCandidates(ctx, org)
		if err != nil {
			return nil, err
		}
		sw.orgCandidates[org] = orgCands
	}
	members := make([]AutoAssignCandidate, 0, len(orgCands))
	for _, c := range orgCands {
		if _, merr := s.members.FindByProjectAndIdentity(ctx, projectID, c.AgentRef); merr != nil {
			continue // not a member of this project
		}
		members = append(members, c)
	}
	sw.projCandidates[projectID] = members
	sw.candidateCount = len(members)
	return members, nil
}

// ─── starvation read (FE DTO) ────────────────────────────────────────────────────

// PoolTaskStarved reports whether an ownerless pool task is STARVED — it carries
// required_capabilities but NO eligible online agent exists to ever take it (capability
// supply gap), so it sits in the pool until a capable agent appears or the requirement
// changes. It is the read backing the pool-node DTO `starved` bool (BE-2↔FE contract):
// FE renders a "waiting for an eligible agent" badge. A task with empty
// required_capabilities is NEVER starved (unrestricted). Slot load does NOT make a task
// starved (a capable-but-busy agent will pick it up when free). nil-safe: no directory
// wired ⇒ false. Reuses selectAutoAssignee's supply test through a fresh sweep ctx so
// the starved verdict and the assign decision share ONE eligibility definition.
func (s *Service) PoolTaskStarved(ctx context.Context, t *pm.Task) (bool, error) {
	if s.autoAssignDir == nil {
		return false, nil
	}
	if len(t.RequiredCapabilities()) == 0 {
		return false, nil
	}
	if t.Assignee() != "" || t.Status() != pm.TaskOpen {
		return false, nil // taken / not an ownerless pool task → not "waiting"
	}
	_, starved, err := s.selectAutoAssignee(ctx, s.newAutoAssignSweepCtx(), t, t.ProjectID())
	return starved, err
}

// fillStarved populates detail.Starved for a BUILTIN POOL plan (the only plan kind
// whose ownerless tasks are auto-assigned and thus can starve — a structured-plan node
// is assignee-gated, never auto-assigned). It is a no-op for non-pool plans and when no
// AutoAssignDirectory is wired, so it is cheap to call on every FE-facing plan read. It
// is invoked by GetPlanDetail / planSummaries (the FE-facing reads), NOT the internal
// planDetail path that the reconciler / claim flow use.
func (s *Service) fillStarved(ctx context.Context, detail *PlanDetail) error {
	if s.autoAssignDir == nil || detail == nil || detail.Plan == nil || !detail.Plan.IsBuiltin() {
		return nil
	}
	starved, err := s.StarvedPoolTasks(ctx, detail.Plan.ProjectID(), detail.Tasks)
	if err != nil {
		return err
	}
	detail.Starved = starved
	return nil
}

// StarvedPoolTasks computes the starved verdict for a set of builtin-pool tasks in one
// project, sharing a single sweep ctx (one candidate list per org/project) so a plan
// view costs one directory read rather than one per node. Returns the subset that is
// starved as a TaskID→true map; non-starved tasks are absent. nil-safe (no directory ⇒
// empty map). Tasks not open/unassigned or with empty required_capabilities are skipped.
func (s *Service) StarvedPoolTasks(ctx context.Context, projectID pm.ProjectID, tasks []*pm.Task) (map[pm.TaskID]bool, error) {
	out := map[pm.TaskID]bool{}
	if s.autoAssignDir == nil {
		return out, nil
	}
	sw := s.newAutoAssignSweepCtx()
	for _, t := range tasks {
		if len(t.RequiredCapabilities()) == 0 || t.Assignee() != "" || t.Status() != pm.TaskOpen {
			continue
		}
		_, starved, err := s.selectAutoAssignee(ctx, sw, t, projectID)
		if err != nil {
			return out, err
		}
		if starved {
			out[t.ID()] = true
		}
	}
	return out, nil
}

// ─── reconciler wrapper (mirrors LeaseChecker) ───────────────────────────────────

// AutoAssignReconciler is the background loop that periodically runs AutoAssignSweep
// (BE-2). It mirrors the LeaseChecker wiring shape: a ticker-driven Run(ctx) that stops
// on ctx cancel, plus an explicit Tick for tests. It is the periodic backstop of the
// dual-track trigger (the event path is TriggerAutoAssignForProject via the projector).
type AutoAssignReconciler struct {
	svc  *Service
	clk  clock.Clock
	tick time.Duration
	log  func(string, ...any)
}

// NewAutoAssignReconciler wires the reconciler. Zero tick → AutoAssignDefaultTick; nil
// clk → system clock; nil log → no-op.
func NewAutoAssignReconciler(svc *Service, clk clock.Clock, tick time.Duration, log func(string, ...any)) *AutoAssignReconciler {
	if tick <= 0 {
		tick = AutoAssignDefaultTick
	}
	if clk == nil {
		clk = clock.SystemClock{}
	}
	if log == nil {
		log = func(string, ...any) {}
	}
	return &AutoAssignReconciler{svc: svc, clk: clk, tick: tick, log: log}
}

// Tick runs one sweep. Exposed for tests + the boot reconcile.
func (r *AutoAssignReconciler) Tick(ctx context.Context) (int, error) {
	return r.svc.AutoAssignSweep(ctx)
}

// Run sweeps every tick until ctx is canceled (the long-lived server goroutine).
func (r *AutoAssignReconciler) Run(ctx context.Context) error {
	t := time.NewTicker(r.tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			if n, err := r.Tick(ctx); err != nil {
				r.log("auto-assign: sweep failed: %v", err)
			} else if n > 0 {
				r.log("auto-assign: assigned %d pool task(s)", n)
			}
		}
	}
}
