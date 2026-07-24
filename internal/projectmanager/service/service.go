// Package service hosts the ProjectManager AppServices (v2.7 B2, ADR-0046 /
// ADR-0052). Every AppService writes ONLY ProjectManager state + an outbox
// event in ONE local transaction (OQ1 = outbox-now purity): creating the task
// Conversation, syncing ConversationParticipant, and enqueuing AgentWorkItems
// are CROSS-BC effects handled by idempotent outbox projectors (B2-b / B2-c),
// never inline in the producer transaction. PM is thus fully decoupled from
// Conversation and Agent.
package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"

	"github.com/oopslink/agent-center/internal/clock"
	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/outbox"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
	orch "github.com/oopslink/agent-center/internal/projectmanager/orchestration"
	orchsql "github.com/oopslink/agent-center/internal/projectmanager/orchestration/sqlite"
	"github.com/oopslink/agent-center/internal/settings"
)

// Outbox event types (the OQ1 cross-BC producer set, ADR-0052 §3).
const (
	EvtProjectCreated    = "pm.project.created"
	EvtMemberAdded       = "pm.member.added"
	EvtMemberRemoved     = "pm.member.removed"
	EvtIssueCreated      = "pm.issue.created"
	EvtIssueStateChanged = "pm.issue.state_changed"
	EvtIssueSubsChanged  = "pm.issue.subscribers_changed"
	EvtTaskCreated       = "pm.task.created"
	EvtTaskAssigned      = "pm.task.assigned"
	EvtTaskReassigned    = "pm.task.reassigned"
	EvtTaskStateChanged  = "pm.task.state_changed"
	EvtTaskSubsChanged   = "pm.task.subscribers_changed"
	// v2.9.1 P1 (task-aab863b3) auto-redispatch follow-up. EvtTaskAutoRedispatched
	// is an AUDIT marker emitted by the AutoRedispatchReconciler each time it
	// automatically re-dispatches a stuck (running + blocked_reason) task whose
	// assignee agent became available again — distinguishing the SYSTEM auto path
	// from a human/agent manual unblock_task. The functional re-dispatch is the
	// accompanying EvtTaskAssigned re-emit (consumed by the WorkItemProjector to
	// mint a fresh WorkItem); this event carries the attempt count for observability.
	EvtTaskAutoRedispatched = "pm.task.auto_redispatched"
	// EvtTaskAutoRedispatchExhausted is emitted ONCE when a stuck task hits the
	// auto-redispatch retry cap — the auto path gives up and leaves the task stuck
	// (blocked_reason intact) for manual unblock_task recovery (auto优先, 手动兜底).
	EvtTaskAutoRedispatchExhausted = "pm.task.auto_redispatch_exhausted"
	// v2.9 plan orchestration (#284). EvtPlanCreated drives the Plan↔Conversation
	// 1:1 create (owner_ref pm://plans/{id}); EvtPlanParticipantsChanged drives
	// the ADDITIVE participant sync (§9.5) when a task is selected into a Plan or
	// a plan-task's assignee changes.
	EvtPlanCreated             = "pm.plan.created"
	EvtPlanParticipantsChanged = "pm.plan.participants_changed"
	// v2.9 P2-1 auto-advance: EvtPlanStarted is emitted by StartPlan after the
	// draft→running transition. The PlanOrchestratorProjector consumes it to
	// dispatch the Plan's INITIAL ready nodes (those with no upstream) — no
	// manual Advance click. Payload mirrors planEventPayload (PlanID + ProjectID
	// + OrganizationID).
	EvtPlanStarted = "pm.plan.started"
	// v2.9 P3 (delete + archive). EvtPlanDeleted is emitted by DeletePlan AFTER the
	// plan row (+ its tasks unloaded to backlog + deps/dispatch-records) are gone; the
	// PlanParticipantProjector consumes it to HARD-DELETE the plan's 1:1 Conversation
	// (owner_ref pm://plans/{id}) — the cross-BC "删会话" cleanup, mirroring how
	// EvtPlanCreated CREATES that conversation (reverse direction). EvtPlanArchived is
	// emitted by ArchivePlan after the plan + cascade-task archive; the projector
	// ARCHIVES the plan's conversation (UpdateArchive) for consistency.
	EvtPlanDeleted  = "pm.plan.deleted"
	EvtPlanArchived = "pm.plan.archived"
	// reminder-event feature: plan lifecycle transition events. Historically the
	// plan running→done (complete), running→draft (stop) and task-failure paths
	// emitted NO dedicated outbox event (audit-only / creator-wake-only). These are
	// additive markers so the Cognition ReminderEventProjector can arm on_event
	// reminders watching a plan (entity_type=plan; event=completed|stopped|failed).
	// Each carries a planEventPayload (PlanID + ProjectID + OrganizationID).
	EvtPlanCompleted = "pm.plan.completed"
	EvtPlanStopped   = "pm.plan.stopped"
	EvtPlanFailed    = "pm.plan.failed"
	// v2.9 P3 (failure→agent-creator-wake, §9.1 / decision-1). Emitted by the
	// PlanOrchestratorProjector's notifyCreatorOnFailure — IN THE SAME TX as the
	// failure @mention PostMention — ONLY when the Plan creator is an AGENT
	// (CreatorRef has the "agent:" scheme). The (production-registered) WakeProjector
	// consumes it and enqueues an agent.converse control command for the
	// agent-creator pointing at the plan conversation, so the agent wakes to READ the
	// failure @mention and self-handle (adjust DAG / escalate via the Stage C MCP plan
	// tools). This is the SANCTIONED DIRECT system wake for a DETERMINED creator on a
	// DETERMINED failure event — NOT the human-only @mention wake path (#220 / v2.7
	// #185 only wakes agents on `user:` senders, so a system @mention can never wake an
	// agent creator). It does NOT widen #185: it is a one-shot system→agent wake on a
	// failure transition, not a chat agent→agent reply loop. For a HUMAN creator NO
	// event is emitted (the @mention in the conversation IS their notification).
	EvtPlanCreatorFailureWake = "pm.plan.creator_failure_wake"
	// T456 (issue-21ba5b78/I30 — 租约到期不 reclaim，只 nudge). Emitted by the
	// lease-checker (NudgeExpiredLeases) IN THE SAME TX as the lease renew + lease_nudge
	// log when a running task's execution lease lapsed. The (production-registered)
	// WakeProjector consumes it and (a) posts a visible @assignee nudge message into the
	// task's bound conversation and (b) enqueues an agent.converse for the assignee so
	// the SAME owner is woken to continue — the task NEVER leaves running and the
	// assignee NEVER changes (the anti-orphan fix). This is the SANCTIONED system→agent
	// wake (like EvtPlanCreatorFailureWake) — a system @mention can otherwise never wake
	// an agent (the #185 human-only loop-break).
	EvtTaskLeaseExpiredNudge = "pm.task.lease_expired_nudge"
	// T464 (issue-41aceddb — issue 派生 task 全完成 → 唤醒 owner 复核关闭). Emitted by the
	// terminal-task hook (maybeNotifyIssueDerivedTasksDone, in taskStateOp's tx) at the
	// MOMENT a task carrying a derived_from_issue link enters a terminal state AND that
	// makes ALL of the issue's derived tasks terminal — and only while the issue is still
	// actionable (not resolved/closed/discarded). The (production-registered) WakeProjector
	// consumes it and (a) posts a visible @owner message into the issue's bound conversation
	// and (b) — when the owner is an agent — enqueues an agent.converse so the owner is woken
	// to REVIEW and close it (close_issue). TRIGGER-ONLY: the issue's status is NEVER changed
	// programmatically (oopslink: close is owner-only via close_issue). A HUMAN owner is
	// notified by the @mention in the conversation (no converse). The all-terminal-on-this-
	// transition condition is the idempotency boundary: it fires once per "fill" of the
	// derived-task set, and again only after a NEW derived task is later added + concluded.
	EvtIssueDerivedTasksDone = "pm.issue.derived_tasks_done"
	// v2.10 Plan Shared Findings (ADR-0053 — DeLM shared verified context).
	// EvtPlanFindingRecorded is emitted (same tx) when an agent records a finding
	// back to a Plan; EvtPlanFindingRetracted when one is retracted. Both are pure
	// action events (no "why") so they carry no reason+message (§16). Observability
	// only — no projector consumes them in v2.10 (the finding's effect is the
	// dispatch injection + list_findings read, not a cross-BC projection).
	EvtPlanFindingRecorded  = "pm.plan_finding.recorded"
	EvtPlanFindingRetracted = "pm.plan_finding.retracted"
	// v2.14.0 I14/F6 (HTTP + Conversation 接线). When a running task is blocked
	// with reasonType=input_required (the agent needs a USER reply) BlockTask emits
	// EvtTaskInputRequested IN THE SAME TX; when the task is later unblocked from an
	// input_required block UnblockTask emits EvtTaskInputReplied. Both are consumed
	// by the (production-registered) TaskInputConversationProjector, which is the
	// ONLY writer of the input_request / input_reply Conversation messages — the pm
	// AppService NEVER writes a Conversation message inline (ADR-0052 outbox purity).
	// obstacle blocks (owner/PM action, no user reply) emit NEITHER event.
	EvtTaskInputRequested = "pm.task.input_requested"
	EvtTaskInputReplied   = "pm.task.input_replied"
)

// AgentDirectory resolves an agent's owning Organization (v2.7 D2 b2/d-i, #5a,
// ADR-0049/0052/OQ6). It is an OPTIONAL dependency of the pm Service: when wired
// (non-nil) AssignTask grants an assignee agent project membership so it can pass
// the project write-gate (OQ4 gives agents project-level write). The agentID is
// the bare id (the `agent:` prefix already stripped by the caller).
type AgentDirectory interface {
	OrgOfAgent(ctx context.Context, agentID string) (orgID string, err error)
	// ConcurrencyCapOfAgent returns the agent's effective run-slot cap — the
	// EffectiveConcurrencyCap of its profile (enabled ⇒ EffectiveMaxConcurrentTasks,
	// else 1). It is the SINGLE-SOURCE cap the center's ≤N start guard
	// (enforceConcurrencyCap) consults, computed adapter-side from the same
	// agent.Profile predicate the worker daemon's executor-pool gate uses, so the two
	// never drift (v2.18.0 W4c). agentID is the bare id (the `agent:` prefix stripped
	// by the Service). An unknown/unresolvable agent returns cap=1 (fail-safe to
	// single-active), never an error, so a directory hiccup can only ever be STRICTER,
	// never leak extra run-slots.
	ConcurrencyCapOfAgent(ctx context.Context, agentID string) (cap int, err error)
}

// CodeRepoResolver resolves a workspace coderepo.Repo by id (v2.18.4 BE-1). It is
// the narrow port the projectmanager BC uses to follow a CodeRepoRef.repo_id into
// the coderepo BC WITHOUT importing it:
//   - RepoURL backs the merge-check primaryRepoURL (an unknown repo returns "" so the
//     caller falls back to the ref's own url, never failing the merge on a miss);
//   - RepoOrg backs AddCodeRepoReference's existence + same-org guard (found=false
//     for an unknown repo; the org isolates cross-workspace references).
type CodeRepoResolver interface {
	RepoURL(ctx context.Context, repoID string) (string, error)
	RepoOrg(ctx context.Context, repoID string) (orgID string, found bool, err error)
}

// PausedTaskPort reports which of the given tasks currently have a PAUSED agent
// work item (T53). It is an OPTIONAL, nil-safe read-port of the pm Service: when
// wired (non-nil) the plan read model derives a `paused` node for a running task
// whose agent set its work item aside; when nil the read model behaves exactly as
// before (running stays running). The pm BC depends ONLY on this narrow port —
// never on the agent package — so the read-side join (agent execution state →
// plan node display) does not couple the two aggregates. Implemented over the
// agent WorkItem repo at composition (agent.WorkItemPausedProvider). taskIDs are
// the bare pm task ids; the returned map keys the paused ones (true). Like
// AgentDirectory it is intentionally STRING-typed (not pm.TaskID) so the agent-side
// adapter implements it WITHOUT importing the pm package. An empty input returns an
// empty map without a query.
type PausedTaskPort interface {
	PausedTasks(ctx context.Context, taskIDs []string) (map[string]bool, error)
}

// NodeResumer resumes a plan node whose agent PAUSED its work item and re-engages
// the agent (T53), so an operator (PD/owner) can un-stick a node that ResumeWork —
// agent-ownership-guarded — left unrecoverable. It is an OPTIONAL, nil-safe port of
// the pm Service: when wired, ResumePausedNode authorizes the operator (pm project
// membership + plan running) then delegates the cross-BC effect to this port; nil ⇒
// ErrNodeResumerUnavailable (fail-loud, mirroring ErrDispatcherUnavailable). Like
// the other ports it is STRING-typed so the agent/environment-side adapter
// implements it WITHOUT importing pm. taskRef is the pm://tasks/{id} ref of the
// node. Implemented at composition over the agent service (resume) + env control
// (the agent.work_available wake).
type NodeResumer interface {
	ResumePausedNode(ctx context.Context, taskRef string) error
}

// Service is the ProjectManager AppService facade.
type Service struct {
	db           *sql.DB
	projects     pm.ProjectRepository
	members      pm.ProjectMemberRepository
	issues       pm.IssueRepository
	tasks        pm.TaskRepository
	taskSubs     pm.TaskSubscriberRepository
	issueSubs    pm.IssueSubscriberRepository
	codeRepoRefs pm.CodeRepoRefRepository
	// plans is OPTIONAL (nil-safe, v2.9 #284). nil ⇒ the Plan AppServices
	// (CreatePlan / SelectTaskIntoPlan / RemoveTaskFromPlan) are unavailable;
	// pre-#284 service constructions keep working unchanged.
	plans  pm.PlanRepository
	outbox outbox.Repository
	idgen  idgen.Generator
	clock  clock.Clock
	// agentDir is OPTIONAL (nil-safe). nil ⇒ AssignTask skips the
	// agent-membership step entirely (preserves pre-#5a behavior).
	agentDir AgentDirectory
	// codeRepoResolver is OPTIONAL (nil-safe, v2.18.4 BE-1). When wired it resolves a
	// CodeRepoRef's repo_id → the workspace coderepo.Repo URL for the merge-check
	// primaryRepoURL. nil ⇒ the resolver is skipped and a url-only ref's own url is
	// used (pre-0087 behaviour, fully compatible).
	codeRepoResolver CodeRepoResolver
	// orgSeq is OPTIONAL (nil-safe, v2.7.1 #245). nil ⇒ CreateTask/CreateIssue
	// skip org-number allocation (org_number stays 0, org_ref omitted) — keeps
	// pre-#245 service constructions (tests) working unchanged.
	orgSeq pm.OrgSequenceRepository
	// planDispatcher is OPTIONAL (nil-safe, v2.9 #285). nil ⇒ AdvancePlan returns
	// ErrDispatcherUnavailable (fail-loud — a missing dispatcher must not silently
	// no-op the @mention). Posts the node-ready @mention into the Plan conversation.
	planDispatcher PlanDispatcher
	// findings is OPTIONAL (nil-safe, v2.10 ADR-0053). nil ⇒ RecordFinding/
	// ListPlanFindings/RetractFinding return ErrFindingsUnavailable AND dispatch
	// injection is skipped (pre-v2.10 constructions keep working unchanged). The
	// plan-scoped shared-findings store (DeLM shared verified context).
	findings pm.PlanFindingRepository
	// pausedTasks is OPTIONAL (nil-safe, T53). nil ⇒ the plan read model derives no
	// `paused` nodes (running stays running). When wired, the read paths overlay the
	// live paused-work-item set onto the derived view.
	pausedTasks PausedTaskPort
	// nodeResumer is OPTIONAL (nil-safe, T53). nil ⇒ ResumePausedNode returns
	// ErrNodeResumerUnavailable. When wired, it resumes a paused node + wakes its
	// agent (cross-BC effect behind the port).
	nodeResumer NodeResumer
	// poolClaimLimit caps the concurrent claimed built-in-pool tasks per agent
	// (T83 §3.6, owner-set). 0 ⇒ DefaultPoolClaimLimit (3).
	poolClaimLimit int
	// actionLogs is OPTIONAL (nil-safe, v2.14.0 I14/F3 §7.3). nil ⇒ the append-only
	// Task lifecycle log (blocked/unblocked/lease_expired/reassigned) is not persisted
	// (the realtime annotation columns still are). When wired, the log-producing flows
	// flush the domain's freshly-appended TaskActionLog entries to pm_task_action_logs.
	actionLogs pm.TaskActionLogRepository
	// audit is OPTIONAL (nil-safe, change-log/audit design §4). nil ⇒ recordChange is
	// a no-op and no object-level change ledger is written (纯加法零回归 — pre-audit
	// constructions keep working unchanged). When wired, the semantic write points
	// append AuditEntry rows to pm_audit_log in the SAME tx (best-effort — an audit
	// failure NEVER rolls back the primary mutation, see recordChange).
	audit pm.AuditLogRepository
	// autoAssignDir is OPTIONAL (nil-safe, v2.18.3 BE-2). nil ⇒ the auto-assign
	// reconciler has no candidate source and AutoAssignSweep is a no-op (pool tasks
	// stay claim-only, pre-BE-2 behaviour). When wired, it lists each org's agents
	// with the online/opt-out/capability/cap snapshot the matcher consumes.
	autoAssignDir AutoAssignDirectory
	// autoAssignSettings is OPTIONAL (nil-safe, v2.18.3 BE-2). The center settings
	// store backing the per-project auto_assign master switch (autoassign.Enabled). nil
	// ⇒ the switch reads its default (ON) for every project.
	autoAssignSettings settings.Store
	// orch is OPTIONAL (nil-safe, T768). The orchestration engine application service.
	// When wired, StartPlan builds a graph mirroring the plan DAG (business node per
	// task + condition node per decision) and dispatch/advance switch to the graph as
	// the ready-source (via plan.GraphID). nil ⇒ graphs are never built and every plan
	// stays on the legacy plan-DAG path (DerivePlanView) — the zero-regression
	// fallback for pre-T768 constructions and in-flight (graphID=="") plans.
	orch *orch.Service
	// stages is OPTIONAL (nil-safe, 2026-07-03 plan-stage-model). nil ⇒ the Stage
	// AppServices (CreateStage / GetStage) are unavailable and buildPlanGraph lays down
	// NO stage structure (a plan is a pure-node DAG, §8 zero-regression). When wired,
	// create_stage / add_task_to_plan(stage) author stages and buildStages lays them
	// onto the graph as gate + barrier nodes/edges.
	stages pm.StageRepository

	// deadlinePolicy configures the I103 §2 deadline engine: per-wait_type deadline +
	// on_timeout action assigned during the reconcile materialize and consumed by the
	// router (routeTimeouts). The zero value is INERT — no deadline is ever assigned and
	// the engine is a no-op (pre-I103 behaviour, zero-regression). Set via
	// Deps.DeadlinePolicy; the composition root (cli app.go) wires
	// pm.DefaultDeadlinePolicy() in production.
	deadlinePolicy pm.DeadlinePolicy
	// timeoutSink is OPTIONAL (nil-safe, I103 §2). nil ⇒ the router still records each
	// timeout on the BlockedOn row (probe_count / last_probe_at) but emits no external
	// action. When wired, the sink enacts the routed on_timeout action PROPOSE-ONLY (it
	// never releases a gated node — the gates stay authoritative). The composition root
	// (cli app.go) wires the production HumanDecisionTimeoutSink via SetTimeoutSink
	// AFTER construction (its reminder adapter depends on this Service — a build cycle).
	timeoutSink TimeoutSink

	// stuckMu guards stuckTrackers — the per-node confirmed-dead accounting the periodic
	// lease sweep (NudgeExpiredLeases) carries across ticks to auto-reopen a structured
	// plan node wedged Running while its executor is dead (issue-6ff12523). In-memory:
	// a lost map on restart just restarts the count, safe (the node is re-observed next
	// sweep). See stuck_node_reconcile.go.
	stuckMu       sync.Mutex
	stuckTrackers map[pm.TaskID]*stuckNodeTracker
}

// DefaultPoolClaimLimit is the T83 §3.6 default cap on concurrently-claimed
// built-in-pool tasks per agent (owner-set 2026-06-15). Overridable via
// Deps.PoolClaimLimit.
const DefaultPoolClaimLimit = 3

// ErrDispatcherUnavailable is returned by AdvancePlan when no PlanDispatcher is
// wired (s.planDispatcher == nil) — fail-loud, mirroring ErrPlansUnavailable.
var ErrDispatcherUnavailable = errors.New("projectmanager: plan dispatcher unavailable — advance cannot post @mentions")

// ErrNodeResumerUnavailable is returned by ResumePausedNode when no NodeResumer is
// wired (s.nodeResumer == nil) — fail-loud, mirroring ErrDispatcherUnavailable.
var ErrNodeResumerUnavailable = errors.New("projectmanager: node resumer unavailable — paused-node resume is not wired")

// ErrTaskNotInPlan is returned by ResumePausedNode when the target task is not a
// node of the named plan (a mismatched/foreign task id).
var ErrTaskNotInPlan = errors.New("projectmanager: task is not a node of this plan")

// ErrNodeNotPaused is returned by ResumePausedNode when the target node has no
// paused work item to resume (the resumer reports nothing paused).
var ErrNodeNotPaused = errors.New("projectmanager: plan node has no paused work item to resume")

// Deps bundles the Service dependencies.
type Deps struct {
	DB           *sql.DB
	Projects     pm.ProjectRepository
	Members      pm.ProjectMemberRepository
	Issues       pm.IssueRepository
	Tasks        pm.TaskRepository
	TaskSubs     pm.TaskSubscriberRepository
	IssueSubs    pm.IssueSubscriberRepository
	CodeRepoRefs pm.CodeRepoRefRepository
	// Plans is OPTIONAL (v2.9 #284): when set, the Plan AppServices are available.
	// nil ⇒ CreatePlan/SelectTaskIntoPlan/RemoveTaskFromPlan are unavailable.
	Plans  pm.PlanRepository
	Outbox outbox.Repository
	IDGen  idgen.Generator
	Clock  clock.Clock
	// AgentDir is OPTIONAL: when set, AssignTask grants an assignee agent
	// project membership (cross-org-guarded). When nil, that step is skipped.
	AgentDir AgentDirectory
	// CodeRepoResolver is OPTIONAL (v2.18.4 BE-1): when set, the merge-check
	// primaryRepoURL follows a ref's repo_id → workspace Repo url. nil ⇒ url-only refs.
	CodeRepoResolver CodeRepoResolver
	// OrgSeq is OPTIONAL (v2.7.1 #245): when set, CreateTask/CreateIssue allocate
	// a per-org T<n>/I<n> number. nil ⇒ allocation skipped (org_number 0).
	OrgSeq pm.OrgSequenceRepository
	// PlanDispatcher is OPTIONAL (v2.9 #285): when set, AdvancePlan posts the
	// node-ready @mention into the Plan conversation. nil ⇒ AdvancePlan unavailable.
	PlanDispatcher PlanDispatcher
	// Findings is OPTIONAL (v2.10 ADR-0053): when set, the PlanFinding AppServices
	// are available and dispatch injects the plan's findings into node @mentions.
	Findings pm.PlanFindingRepository
	// PausedTasks is OPTIONAL (T53): when set, the plan read model derives a
	// `paused` node for a running task whose agent paused its work item. nil ⇒ no
	// paused overlay.
	PausedTasks PausedTaskPort
	// NodeResumer is OPTIONAL (T53): when set, ResumePausedNode can resume a paused
	// node + wake its agent. nil ⇒ ResumePausedNode is unavailable.
	NodeResumer NodeResumer
	// PoolClaimLimit is OPTIONAL (T83 §3.6): max concurrent claimed built-in-pool
	// tasks per agent. 0 ⇒ DefaultPoolClaimLimit (3).
	PoolClaimLimit int
	// TaskActionLogs is OPTIONAL (v2.14.0 I14/F3 §7.3): when set, the log-producing
	// task flows (block/unblock/lease-expiry/reassign) flush the domain's appended
	// TaskActionLog entries to pm_task_action_logs. nil ⇒ no live log persistence.
	TaskActionLogs pm.TaskActionLogRepository
	// Audit is OPTIONAL (change-log/audit design §4): when set, the semantic write
	// points record object-level change-ledger entries into pm_audit_log (in-tx,
	// best-effort). nil ⇒ recordChange is a no-op (zero-regression).
	Audit pm.AuditLogRepository
	// AutoAssignDir is OPTIONAL (v2.18.3 BE-2): when set, the auto-assign reconciler
	// can list each org's candidate agents. nil ⇒ AutoAssignSweep is a no-op.
	AutoAssignDir AutoAssignDirectory
	// AutoAssignSettings is OPTIONAL (v2.18.3 BE-2): the center settings store backing
	// the per-project auto_assign master switch. nil ⇒ the switch is its default (ON).
	AutoAssignSettings settings.Store
	// Orch is OPTIONAL (T768): the orchestration engine application service. When set,
	// StartPlan builds a graph for the plan and dispatch/advance use it as the
	// ready-source (via plan.GraphID). nil ⇒ every plan stays on the legacy plan-DAG
	// path (zero-regression fallback).
	Orch *orch.Service
	// Stages is OPTIONAL (2026-07-03 plan-stage-model): when set, the Stage AppServices
	// are available and buildPlanGraph lays a plan's stages onto the graph. nil ⇒ Stage
	// is inert (pure-node DAG, §8 zero-regression).
	Stages pm.StageRepository
	// DeadlinePolicy is OPTIONAL (I103 §2): the deadline engine's per-wait_type deadline
	// + on_timeout policy. The zero value is INERT (no deadline ever assigned — engine
	// off). The composition root (cli app.go) wires pm.DefaultDeadlinePolicy() here.
	DeadlinePolicy pm.DeadlinePolicy
	// TimeoutSink is OPTIONAL (I103 §2): when set, the on_timeout router hands each
	// elapsed-deadline node to the sink to enact the routed action (PROPOSE-ONLY — never
	// releases a gated node). nil ⇒ timeouts are recorded on the BlockedOn row only. NOT
	// set here in production (the sink needs the reminder AppService, which depends on
	// this Service): the composition root wires it post-construction via SetTimeoutSink.
	TimeoutSink TimeoutSink
}

// New constructs the Service.
func New(d Deps) *Service {
	clk := d.Clock
	if clk == nil {
		clk = clock.SystemClock{}
	}
	// T810 ⑤: the orchestration engine is MANDATORY — it is the single dispatch/
	// decision/loopback path (the ComputePlanView fallback was deleted). Production
	// injects Deps.Orch explicitly; when a caller (e.g. a test harness) leaves it nil
	// but provides a DB, construct it from the DB so every Service has a live engine.
	orchSvc := d.Orch
	if orchSvc == nil && d.DB != nil {
		orchSvc = orch.NewService(orch.ServiceDeps{
			DB: d.DB, Graphs: orchsql.NewGraphRepo(d.DB), Nodes: orchsql.NewNodeRepo(d.DB),
			Edges: orchsql.NewEdgeRepo(d.DB), IDGen: d.IDGen, Clock: clk,
		})
	}
	return &Service{
		db: d.DB, projects: d.Projects, members: d.Members, issues: d.Issues,
		tasks: d.Tasks, taskSubs: d.TaskSubs, issueSubs: d.IssueSubs,
		codeRepoRefs: d.CodeRepoRefs, plans: d.Plans, outbox: d.Outbox, idgen: d.IDGen, clock: clk,
		agentDir: d.AgentDir, codeRepoResolver: d.CodeRepoResolver, orgSeq: d.OrgSeq, planDispatcher: d.PlanDispatcher, findings: d.Findings,
		pausedTasks: d.PausedTasks, nodeResumer: d.NodeResumer, poolClaimLimit: d.PoolClaimLimit,
		actionLogs:         d.TaskActionLogs,
		audit:              d.Audit,
		autoAssignDir:      d.AutoAssignDir,
		autoAssignSettings: d.AutoAssignSettings,
		orch:               orchSvc,
		stages:             d.Stages,
		deadlinePolicy:     d.DeadlinePolicy,
		timeoutSink:        d.TimeoutSink,
	}
}

// flushActionLogs persists the domain's freshly-appended TaskActionLog entries
// (v2.14.0 I14/F3 §7.3). It is nil-safe (no repo wired ⇒ no-op). A Task loaded via
// FindByID rehydrates with NO action-log history (scanTask does not read the log
// table), so after a single domain op t.ActionLogs() holds ONLY that op's new
// entries — appending them is duplicate-safe (Append assigns a ULID to each). Runs in
// the caller's tx so the log row commits atomically with the task state change.
func (s *Service) flushActionLogs(ctx context.Context, t *pm.Task) error {
	if s.actionLogs == nil {
		return nil
	}
	logs := t.ActionLogs()
	if len(logs) == 0 {
		return nil
	}
	return s.actionLogs.Append(ctx, t.ID(), logs)
}

// recordChange appends one object-level change-ledger entry (design §5) into
// pm_audit_log. It is the single in-tx write收口 the semantic write points call
// (紧挨 s.emit). Contract, honoring the two design constraints that pull opposite
// directions:
//
//   - 即时可见 / 无最终一致性延迟: it writes in the caller's AMBIENT tx, so on commit
//     the audit row lands atomically with the change it records — "刚改完就查变更记录"
//     is consistent, no async projector lag.
//   - 审计写不阻塞主 mutation: it is BEST-EFFORT — an audit failure must never fail or
//     roll back the primary business operation. The insert is wrapped in a SAVEPOINT
//     so a failed audit write rolls back ONLY the audit row (not the mutation), and any
//     error is swallowed (logged via observability, not returned). A missing tx / a
//     savepoint the backend rejects degrades to a plain best-effort append.
//
// nil audit repo ⇒ no-op (zero-regression). The whole-tx BUSY replay in RunInTx
// re-invokes the write points, so recordChange needs no per-tx buffer: each replay
// re-appends fresh entries (new ULIDs) — the losing attempt's rows never committed.
func (s *Service) recordChange(ctx context.Context, e pm.AuditEntry) {
	if s.audit == nil {
		return
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = s.clock.Now()
	}
	exec, err := persistence.ExecutorFromCtx(ctx, s.db)
	if err != nil {
		// No tx and no db — nothing we can do; audit is best-effort.
		return
	}
	// Wrap in a savepoint so a failed audit insert cannot poison the parent tx (some
	// SQLite errors abort the enclosing statement's changes; the savepoint bounds the
	// blast radius to the audit row). If the backend rejects SAVEPOINT, fall back to a
	// plain best-effort append.
	if _, sperr := exec.ExecContext(ctx, "SAVEPOINT pm_audit"); sperr != nil {
		if aerr := s.audit.Append(ctx, e); aerr != nil {
			slog.WarnContext(ctx, "pm: audit append failed (best-effort, mutation unaffected)", "err", aerr, "object_type", e.ObjectType, "object_id", e.ObjectID, "change_type", e.ChangeType)
		}
		return
	}
	if aerr := s.audit.Append(ctx, e); aerr != nil {
		slog.WarnContext(ctx, "pm: audit append failed (best-effort, mutation unaffected)", "err", aerr, "object_type", e.ObjectType, "object_id", e.ObjectID, "change_type", e.ChangeType)
		_, _ = exec.ExecContext(ctx, "ROLLBACK TO pm_audit")
	}
	_, _ = exec.ExecContext(ctx, "RELEASE pm_audit")
}

// ListObjectAudit returns an object's change ledger newest-first with cursor
// pagination (design §6), backing the read API. nil audit repo ⇒ empty page (a
// pre-audit construction has no ledger — not an error). It does NOT gate membership
// itself; the HTTP handler resolves the object-in-project (which enforces project
// membership) BEFORE calling this.
func (s *Service) ListObjectAudit(ctx context.Context, objType pm.AuditObjectType, objID, cursor string, limit int) ([]pm.AuditEntry, string, error) {
	if s.audit == nil {
		return nil, "", nil
	}
	return s.audit.ListByObject(ctx, objType, objID, cursor, limit)
}

// ListTaskActionLogs returns the persisted task lifecycle action history. GetTask
// intentionally does not hydrate pm_task_action_logs, so audit/read-model callers must
// use this service method instead of inspecting Task.ActionLogs().
func (s *Service) ListTaskActionLogs(ctx context.Context, taskID pm.TaskID, offset, limit int) ([]pm.TaskActionLog, int, error) {
	if s.actionLogs == nil {
		return nil, 0, nil
	}
	return s.actionLogs.ListByTaskPage(ctx, taskID, offset, limit)
}

// poolLimit resolves the configured per-agent pool-claim cap, defaulting to
// DefaultPoolClaimLimit when unset (T83 §3.6).
func (s *Service) poolLimit() int {
	if s.poolClaimLimit > 0 {
		return s.poolClaimLimit
	}
	return DefaultPoolClaimLimit
}

// SetPausedTaskProvider wires the optional T53 paused-task read-port AFTER
// construction — used by the composition root, where the agent WorkItem repo (the
// adapter's backing store) is built after the pm Service. nil is tolerated (clears
// the overlay). Returns the receiver for chaining.
func (s *Service) SetPausedTaskProvider(p PausedTaskPort) *Service {
	s.pausedTasks = p
	return s
}

// SetNodeResumer wires the optional T53 paused-node resume port AFTER construction
// (the adapter needs the agent service + env control, built after the pm Service).
// nil is tolerated. Returns the receiver for chaining.
func (s *Service) SetNodeResumer(r NodeResumer) *Service {
	s.nodeResumer = r
	return s
}

// SetTimeoutSink wires the optional I103 §2 on_timeout sink AFTER construction — the
// production sink's reminder adapter needs the cognition reminder AppService, whose
// Directory depends on this pm Service, so it cannot be built inside Deps{} (a
// construction cycle). nil is tolerated (the engine then records probes only). Returns
// the receiver for chaining. See decision_timeout_sink.go.
func (s *Service) SetTimeoutSink(sink TimeoutSink) *Service {
	s.timeoutSink = sink
	return s
}

// taskEventPayload is the JSON payload for task subscriber-affecting events.
// It carries the new EFFECTIVE subscriber set so the B2-b projector can
// overwrite the Conversation participants idempotently (set semantics) and
// (for created) create the Conversation by owner_ref.
type taskEventPayload struct {
	TaskID               string   `json:"task_id"`
	ProjectID            string   `json:"project_id"`
	OrganizationID       string   `json:"organization_id"` // the project's org — the participant projector stamps it onto the task Conversation so org-scoped endpoints (incl. human reply → agent wake) resolve it (v2.7 GATE-4 fix)
	OwnerRef             string   `json:"owner_ref"`       // pm://tasks/{id}
	EffectiveSubscribers []string `json:"effective_subscribers"`
	Assignee             string   `json:"assignee,omitempty"`
	PreviousAssignee     string   `json:"previous_assignee,omitempty"`
	Status               string   `json:"status,omitempty"`
	// PrevStatus is the task's status BEFORE the transition this event reports
	// (captured by the producer immediately before the AR transition method). It
	// lets a consumer distinguish a TRANSITION into a state from a re-emit of an
	// event whose task was ALREADY in that state — used by the P2-2 failure
	// handler to notify ONLY on the →failed transition (prev not-failed, now
	// failed), so re-discarding an already-failed task does NOT re-notify. Empty
	// on old events / non-transition emits ⇒ treated as "unknown / not-failed" so a
	// genuine first failure still notifies.
	PrevStatus string `json:"prev_status,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

// planEventPayload is the JSON payload for Plan participant-affecting events
// (v2.9 #284). It carries enough for the participant projector to (a) create the
// Plan's 1:1 Conversation by owner_ref on EvtPlanCreated and (b) ADDITIVELY add
// participants on EvtPlanCreated / EvtPlanParticipantsChanged.
//
// Unlike taskEventPayload (which carries the full EFFECTIVE set for overwrite
// semantics), Participants here is an ADD-ONLY delta: the projector unions it
// into the conversation's existing participants and NEVER removes anyone (§9.5 —
// preserve history access, don't yank mid-plan). The creator rides EvtPlanCreated
// (always a participant); each selected task's current assignee rides a
// subsequent EvtPlanParticipantsChanged.
type planEventPayload struct {
	PlanID         string   `json:"plan_id"`
	ProjectID      string   `json:"project_id"`
	OrganizationID string   `json:"organization_id"` // the project's org — stamped onto the Plan Conversation so org-scoped endpoints (incl. agent wake via @mention) resolve it
	OwnerRef       string   `json:"owner_ref"`       // pm://plans/{id}
	CreatorRef     string   `json:"creator_ref,omitempty"`
	Participants   []string `json:"participants"` // ADD-ONLY (additive §9.5); unioned into existing, never removed
}

// planCreatorFailureWakePayload is the JSON payload for EvtPlanCreatorFailureWake
// (v2.9 P3 failure→agent-creator-wake). It carries everything the WakeProjector
// needs to resolve the agent-creator → its worker binding and enqueue an
// agent.converse pointing at the plan conversation:
//   - CreatorRef is the agent ref ("agent:<id>") — the WakeProjector strips the
//     scheme and resolves the agent (tolerating the entity-id OR identity-member-id
//     form, like deliverConverse) → its worker binding.
//   - ConversationID is the plan's 1:1 conversation (where the failure @mention was
//     posted) — the converse target + cursor the agent reads.
//   - MessageID is the failure @mention's message id (from PostMention). It is the
//     idempotency anchor: the converse key embeds it so a redelivered wake on the
//     SAME failure transition never double-wakes the creator.
//   - PlanID / TaskID / OrganizationID are diagnostic context (the failure locus).
type planCreatorFailureWakePayload struct {
	CreatorRef     string `json:"creator_ref"`
	ConversationID string `json:"conversation_id"`
	MessageID      string `json:"message_id"`
	PlanID         string `json:"plan_id"`
	TaskID         string `json:"task_id"`
	OrganizationID string `json:"organization_id"`
}

// taskLeaseExpiredNudgePayload is the JSON payload for EvtTaskLeaseExpiredNudge
// (T456). It carries everything the WakeProjector needs to resolve the task's bound
// conversation and wake the SAME assignee — without re-reading the task:
//   - AssigneeRef is the agent ref ("agent:<member-id>") to nudge/wake.
//   - OwnerRef is pm://tasks/{id} (the projector resolves the bound conversation by
//     owner_ref, mirroring the task-input projector).
//   - TaskID / ProjectID are the locus (diagnostics + conversation resolution).
type taskLeaseExpiredNudgePayload struct {
	TaskID      string `json:"task_id"`
	ProjectID   string `json:"project_id"`
	OwnerRef    string `json:"owner_ref"` // pm://tasks/{id}
	AssigneeRef string `json:"assignee_ref"`
}

// issueDerivedTasksDonePayload is the JSON payload for EvtIssueDerivedTasksDone
// (T464). It carries everything the WakeProjector needs to resolve the issue's bound
// conversation and @-nudge/wake the owner — without re-reading the issue:
//   - OwnerRef is pm://issues/{id} (the projector resolves the bound conversation by
//     owner_ref, mirroring the task paths).
//   - OwnerIdentity is the issue's owner identity ref (created_by: "agent:<member>"
//     or "user:<id>") — the @mention target, and the converse target when it is an agent.
//   - Total/Completed/Discarded summarize the derived-task set for the message wording.
//   - IssueID/ProjectID are the locus.
type issueDerivedTasksDonePayload struct {
	IssueID       string `json:"issue_id"`
	ProjectID     string `json:"project_id"`
	OwnerRef      string `json:"owner_ref"` // pm://issues/{id}
	OwnerIdentity string `json:"owner_identity"`
	Total         int    `json:"total"`
	Completed     int    `json:"completed"`
	Discarded     int    `json:"discarded"`
}

// taskInputEventPayload is the JSON payload for the v2.14.0 I14/F6 task-input
// events (EvtTaskInputRequested / EvtTaskInputReplied). It carries everything the
// TaskInputConversationProjector needs to resolve the task's bound Conversation
// (by OwnerRef → NewTaskOwnerRef) and post the input_request / input_reply
// message — WITHOUT the pm AppService writing a Conversation message inline
// (ADR-0052 outbox purity).
//
//   - OwnerRef is pm://tasks/{id} (the projector derives the task id + resolves
//     the conversation by owner_ref, mirroring the participant projector).
//   - AgentRef is the assignee — the SENDER of the input_request (the agent asking
//     for input). Set on the request event.
//   - ActorRef is the user who unblocked — the SENDER of the input_reply. Set on
//     the reply event.
//   - Reason is the agent's block reason (the request body); Comment is the user's
//     reply (the reply body).
//   - InputRequestMessageID (reply only, optional) threads the reply under the
//     original input_request message (depth-1; empty ⇒ top-level reply).
type taskInputEventPayload struct {
	TaskID                string `json:"task_id"`
	ProjectID             string `json:"project_id"`
	OwnerRef              string `json:"owner_ref"` // pm://tasks/{id}
	AgentRef              string `json:"agent_ref,omitempty"`
	ActorRef              string `json:"actor_ref,omitempty"`
	Reason                string `json:"reason,omitempty"`
	Comment               string `json:"comment,omitempty"`
	InputRequestMessageID string `json:"input_request_message_id,omitempty"`
}

type issueEventPayload struct {
	IssueID              string   `json:"issue_id"`
	ProjectID            string   `json:"project_id"`
	OrganizationID       string   `json:"organization_id"` // the project's org — stamped onto the issue Conversation (same org-scoping fix as tasks)
	OwnerRef             string   `json:"owner_ref"`       // pm://issues/{id}
	EffectiveSubscribers []string `json:"effective_subscribers"`
	Status               string   `json:"status,omitempty"`
}

// emit appends an outbox event inside the current transaction. Producer
// AppServices call this within RunInTx so the PM state write + event commit
// atomically (OQ1).
func (s *Service) emit(ctx context.Context, eventType, refs string, payload any) error {
	pb, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return s.outbox.Append(ctx, outbox.Event{
		ID:        s.idgen.NewULID(),
		EventType: eventType,
		Refs:      refs,
		Payload:   string(pb),
		CreatedAt: s.clock.Now(),
	})
}

// emitPlanLifecycle appends an additive plan lifecycle marker event (completed /
// stopped / failed) inside the caller's tx (reminder-event feature). The org id is
// best-effort (looked up from the plan's project; absence is non-fatal — the
// Cognition ReminderEventProjector matches on plan_id + event, org is diagnostic).
func (s *Service) emitPlanLifecycle(txCtx context.Context, p *pm.Plan, eventType string) error {
	orgID := ""
	if proj, perr := s.projects.FindByID(txCtx, p.ProjectID()); perr == nil && proj != nil {
		orgID = proj.OrganizationID()
	}
	return s.emit(txCtx, eventType,
		refsJSON(map[string]string{"plan_id": string(p.ID()), "project_id": string(p.ProjectID())}),
		planEventPayload{
			PlanID:         string(p.ID()),
			ProjectID:      string(p.ProjectID()),
			OrganizationID: orgID,
			OwnerRef:       "pm://plans/" + string(p.ID()),
			CreatorRef:     string(p.CreatorRef()),
		})
}

func refsJSON(kv map[string]string) string {
	b, _ := json.Marshal(kv)
	return string(b)
}

// EffectiveTaskSubscribers computes the effective subscriber set for a Task
// (ADR-0052 §1): {creator} ∪ {current assignee} ∪ {manual subscriber rows}.
// creator/assignee are DERIVED here (not stored as rows), so they can never be
// unsubscribed while they hold that role.
func EffectiveTaskSubscribers(t *pm.Task, manual []*pm.TaskSubscriber) []string {
	set := map[string]struct{}{string(t.CreatedBy()): {}}
	if a := string(t.Assignee()); a != "" {
		set[a] = struct{}{}
	}
	for _, m := range manual {
		set[string(m.IdentityID())] = struct{}{}
	}
	return sortedKeys(set)
}

// EffectiveIssueSubscribers computes the effective subscriber set for an Issue:
// {creator} ∪ {manual subscriber rows} (issues have no assignee).
func EffectiveIssueSubscribers(i *pm.Issue, manual []*pm.IssueSubscriber) []string {
	set := map[string]struct{}{string(i.CreatedBy()): {}}
	for _, m := range manual {
		set[string(m.IdentityID())] = struct{}{}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]struct{}) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	// deterministic order (insertion-independent) for stable payloads/tests.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}

// requireProjectMember is the minimum write-gate (OQ6): the actor must be a
// member of the project to write in it. ErrNotMember on failure.
var ErrNotMember = errors.New("projectmanager: actor is not a member of this project")

// v2.7 #207/#208 (RemoveProjectMember): owner-only authz + owner-protection.
var (
	// ErrNotOwner is returned when a non-owner member attempts an owner-only
	// action (e.g. removing a project member). Maps to 403.
	ErrNotOwner = errors.New("projectmanager: actor must be a project owner")
	// ErrCannotRemoveOwner blocks removing an owner member so a project always
	// retains an owner. Maps to 409.
	ErrCannotRemoveOwner = errors.New("projectmanager: cannot remove an owner member")
)

func (s *Service) requireProjectMember(ctx context.Context, projectID pm.ProjectID, actor pm.IdentityRef) error {
	// v2.7.1 #239: distinguish "project does not exist" from "not a member" — a
	// membership miss alone can't tell them apart, so a bad/guessed project id
	// surfaced the misleading ErrNotMember (@oopslink screenshot pain). Check
	// existence FIRST: a missing project yields ErrProjectNotFound (404), and
	// ErrNotMember (403) is reserved for a project that exists but the actor isn't
	// a member of.
	if _, err := s.projects.FindByID(ctx, projectID); err != nil {
		return err // pm.ErrProjectNotFound when missing
	}
	if _, err := s.members.FindByProjectAndIdentity(ctx, projectID, actor); err != nil {
		if errors.Is(err, pm.ErrMemberNotFound) {
			return ErrNotMember
		}
		return err
	}
	return nil
}

// requireProjectMutable is the v2.9 #297 archived-project write-gate: an archived
// Project is PURE READ-ONLY (@oopslink: archive is IRREVERSIBLE, no restore), so
// every project-CHILD mutation must reject with pm.ErrProjectArchived (→ 409
// cross-surface) once the project is archived. It loads the project (projects.
// FindByID; a missing project surfaces pm.ErrProjectNotFound) and returns
// pm.ErrProjectArchived when status == archived, else nil. Callers invoke it INSIDE
// their tx, AFTER loading the mutated entity (so the projectID is resolved) and
// BEFORE the write. Reads (GetX/ListX) and the Archive op itself do NOT call it.
func (s *Service) requireProjectMutable(ctx context.Context, projectID pm.ProjectID) error {
	p, err := s.projects.FindByID(ctx, projectID)
	if err != nil {
		return err // pm.ErrProjectNotFound when missing
	}
	if p.Status() == pm.ProjectArchived {
		return pm.ErrProjectArchived
	}
	return nil
}

// runInTx is a thin wrapper so AppServices read clearly.
func (s *Service) runInTx(ctx context.Context, fn func(ctx context.Context) error) error {
	return persistence.RunInTx(ctx, s.db, fn)
}
