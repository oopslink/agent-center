package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
	"github.com/oopslink/agent-center/internal/workforce"
)

// CreateAgentCommand captures the create-agent inputs. The Worker binding is
// immutable after creation (ADR-0049 §5).
type CreateAgentCommand struct {
	OrganizationID string
	Name           string
	Description    string
	Model          string
	CLI            string
	EnvVars        map[string]string
	Skills         []string
	WorkerID       string
	CreatedBy      agent.IdentityRef
	// IdentityMemberID (optional, v2.7 #157) — the identity-member id this
	// execution Agent represents; set by the unified Members→Add Agent flow.
	IdentityMemberID string
}

// CreateAgent validates the chosen Worker belongs to the caller's org, then
// creates the Agent (stopped) + emits agent.created in one tx.
func (s *Service) CreateAgent(ctx context.Context, cmd CreateAgentCommand) (agent.AgentID, error) {
	// v2.7 #181 / FINDING-F: reject a cli the runtime can't execute (empty /
	// codex / opencode / unknown) — they are discovered (#147) + displayed
	// (#176) but their adapters are ErrNotImplemented stubs, so only
	// claude-code actually runs. Fail fast before touching the tx.
	if !agent.IsSupportedExecutionCLI(cmd.CLI) {
		return "", agent.ErrUnsupportedCLI
	}
	id := agent.AgentID(s.idgen.NewULID())
	now := s.clock.Now()
	a, err := agent.NewAgent(agent.NewAgentInput{
		ID:             id,
		OrganizationID: cmd.OrganizationID,
		Profile: agent.Profile{
			Name: cmd.Name, Description: cmd.Description, Model: cmd.Model,
			CLI: cmd.CLI, EnvVars: cmd.EnvVars,
		},
		Skills:           cmd.Skills,
		WorkerID:         cmd.WorkerID,
		CreatedBy:        cmd.CreatedBy,
		IdentityMemberID: cmd.IdentityMemberID,
		CreatedAt:        now,
	})
	if err != nil {
		return "", err
	}
	return id, s.runInTx(ctx, func(txCtx context.Context) error {
		// Worker must exist AND belong to the same org (cross-org binding hidden).
		w, werr := s.workers.FindByID(txCtx, workforce.WorkerID(cmd.WorkerID))
		if werr != nil || w == nil || w.OrganizationID() != cmd.OrganizationID {
			return ErrWorkerNotInOrg
		}
		if serr := s.agents.Save(txCtx, a); serr != nil {
			return serr
		}
		return s.emit(txCtx, EvtAgentCreated, a, "")
	})
}

// lifecycleOp is the shared load → AR-transition → persist → emit path for the
// lifecycle intent verbs. The transition function is an AR method, so illegal
// transitions are rejected by the aggregate (the AppService never bare-writes
// the lifecycle field).
func (s *Service) lifecycleOp(ctx context.Context, id agent.AgentID, mutate func(*agent.Agent) error, resetScope string) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		a, err := s.agents.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		if err := mutate(a); err != nil {
			return err
		}
		if err := s.agents.Update(txCtx, a); err != nil {
			return err
		}
		return s.emit(txCtx, EvtAgentLifecycleChanged, a, resetScope)
	})
}

// StartAgent moves stopped/error → running (intent; D2 reconciles the process).
func (s *Service) StartAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Start(now) }, "")
}

// StopAgent moves running → stopping (operational stop; does NOT touch WorkItems).
func (s *Service) StopAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Stop(now) }, "")
}

// RestartAgent requests a restart while keeping the running intent (version bump).
func (s *Service) RestartAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Restart(now) }, "")
}

// ResetAgent moves the Agent to resetting for the given scope. The destructive
// op requires explicit confirmation (ADR-0049 §5 second confirmation).
func (s *Service) ResetAgent(ctx context.Context, id agent.AgentID, scope agent.ResetScope, confirm bool) error {
	if !confirm {
		return ErrResetNotConfirmed
	}
	now := s.clock.Now()
	return s.lifecycleOp(ctx, id, func(a *agent.Agent) error { return a.Reset(scope, now) }, string(scope))
}

// --- D2-c-i controller→center lifecycle feedback (persist-only) -------------
//
// These are RESULT feedback from the (future) daemon AgentController reporting
// that a process settled to stopped or errored. They are NOT intent changes, so
// they MUST NOT emit agent.lifecycle_changed — that event is consumed by the
// Environment AgentControlProjector, which would enqueue a NEW reconcile command
// and create a feedback loop. We therefore PERSIST the AR state ONLY (no outbox
// emit). The lifecycle gating still lives in the AR (MarkStopped rejects an
// illegal precondition), so ErrIllegalLifecycle surfaces to the caller (→ 409).

// MarkAgentStopped records that a stopping/resetting Agent has settled to
// stopped (Environment feedback). Persist-only: NO outbox emit (loop-avoidance).
// Returns agent.ErrIllegalLifecycle if the Agent is not stopping/resetting.
func (s *Service) MarkAgentStopped(ctx context.Context, id agent.AgentID, at time.Time) error {
	return s.feedbackPersist(ctx, id, func(a *agent.Agent) error { return a.MarkStopped(at) })
}

// MarkAgentError records an Environment-reported error state for the Agent.
// Persist-only: NO outbox emit (loop-avoidance). MarkError has no precondition.
func (s *Service) MarkAgentError(ctx context.Context, id agent.AgentID, msg string, at time.Time) error {
	return s.feedbackPersist(ctx, id, func(a *agent.Agent) error { a.MarkError(msg, at); return nil })
}

// MarkAgentFailed records the TERMINAL crash-loop circuit-breaker state (v2.7
// GATE-7 Mode-B self-heal cap exhausted) AND cascades: the agent's IN-FLIGHT
// WorkItems (active / waiting_input) can never continue (no auto-relaunch), so they
// are failed in the SAME transaction — so no observer ever reads the misleading
// intermediate state "agent failed but its WorkItem still active" (the user's task
// would look like it is still running). Atomic: agent→failed + in-flight WIs→failed
// commit together or not at all.
//
// Persist-only: NO outbox emit (result feedback, not an intent change). Returns
// agent.ErrIllegalLifecycle (→ 409) if the Agent is not running/error. Uses the
// dedicated AgentWorkItem.FailFromAgentDeath edge (the ONLY path that may move
// waiting_input→failed); the normal feedback path stays restricted. The WI failure
// cause is traceable via the agent's lifecycleError (msg).
func (s *Service) MarkAgentFailed(ctx context.Context, id agent.AgentID, msg string, at time.Time) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		a, err := s.agents.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		if err := a.MarkFailed(msg, at); err != nil {
			return err
		}
		if err := s.agents.Update(txCtx, a); err != nil {
			return err
		}
		// Cascade the agent-death to its in-flight WorkItems (active + waiting_input).
		wis, err := s.workItems.ListByAgent(txCtx, id)
		if err != nil {
			return err
		}
		for _, wi := range wis {
			if st := wi.Status(); st != agent.WorkItemActive && st != agent.WorkItemWaitingInput && st != agent.WorkItemPaused {
				// Only IN-FLIGHT WorkItems cascade (active / waiting_input / v2.8.1 #278
				// paused — a paused item on a terminally-dead agent can't resume). A
				// QUEUED WorkItem is deliberately
				// LEFT queued (DEFERRED-WITH-TRIGGER, PM): it is unstarted + not
				// session-bound, so its work is recoverable — failing it would wrongly
				// kill work that could still run, and the owning agent is itself visibly
				// `failed` (queryable), so the residual is non-silent and loses no done
				// work. Long-term a queued WI on a dead agent should be REASSIGNED to a
				// healthy agent (via Supersede + recreate — a cross-BC dispatch change
				// beyond Mode-B). Trigger: queued WIs stuck on dead agents become a
				// fleet-scale pain → wire agent-death→reassign then. (CHANGELOG + §A.)
				continue
			}
			if err := wi.FailFromAgentDeath(at); err != nil {
				return err
			}
			if err := s.workItems.Update(txCtx, wi); err != nil {
				return err
			}
		}
		return nil
	})
}

// feedbackPersist is the shared load → AR-transition → persist path for the
// controller feedback verbs. CRITICAL: unlike lifecycleOp it does NOT emit any
// outbox event — these are result feedback, not intent changes, and emitting
// agent.lifecycle_changed would re-trigger the reconcile projector.
func (s *Service) feedbackPersist(ctx context.Context, id agent.AgentID, mutate func(*agent.Agent) error) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		a, err := s.agents.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		if err := mutate(a); err != nil {
			return err
		}
		return s.agents.Update(txCtx, a)
	})
}

// WorkItemFeedbackState is the controller-reported terminal/active state for a
// WorkItem (D2-c-i work-item-state feedback).
type WorkItemFeedbackState string

const (
	WorkItemFeedbackActive WorkItemFeedbackState = "active"
	WorkItemFeedbackDone   WorkItemFeedbackState = "done"
	WorkItemFeedbackFailed WorkItemFeedbackState = "failed"
)

// ErrWorkItemNotForAgent is returned when a feedback call targets a WorkItem
// that does not belong to the asserted agent (ownership guardrail).
var ErrWorkItemNotForAgent = agent.ErrWorkItemNotFound

// MarkWorkItemState applies a controller-reported WorkItem transition
// (active/done/failed) and persists it (D2-c-i). The WorkItem must belong to
// agentID (ownership guardrail) — otherwise agent.ErrWorkItemNotFound. The
// transition is gated by the AR state machine (Activate/Done/Fail), so an
// illegal move surfaces agent.ErrWorkItemIllegalMove (→ 409). Persist-only: the
// WorkItem AR emits no outbox event.
func (s *Service) MarkWorkItemState(ctx context.Context, agentID agent.AgentID, workItemID string, state WorkItemFeedbackState, at time.Time) error {
	return s.runInTx(ctx, func(txCtx context.Context) error {
		wi, err := s.workItems.FindByID(txCtx, workItemID)
		if err != nil {
			return err
		}
		if wi.AgentID() != agentID {
			return ErrWorkItemNotForAgent
		}
		switch state {
		case WorkItemFeedbackActive:
			err = wi.Activate(at)
		case WorkItemFeedbackDone:
			err = wi.Done(at)
		case WorkItemFeedbackFailed:
			err = wi.Fail(at)
		default:
			return agent.ErrWorkItemBadStatus
		}
		if err != nil {
			return err
		}
		// v2.8.1 #278 PR1 (Tester finding): the controller-push report-active path
		// (the ACTUAL activation path until PR4's pull-loop) can race the single-
		// active UNIQUE index when concurrent assigns deliver multiple work
		// commands. Map the loser's UNIQUE violation to the clean
		// ErrAgentHasActiveWork (→ 409, not a raw 500) so the daemon's feedback
		// log is benign and the invariant (1 active) still holds.
		if uerr := s.workItems.Update(txCtx, wi); uerr != nil {
			if persistence.IsUniqueViolation(uerr) {
				return agent.ErrAgentHasActiveWork
			}
			return uerr
		}
		return nil
	})
}

// StartWork is the agent-PULL activation (v2.8.1 #278, @oopslink's pull model):
// the agent — via the MCP start_work tool — selects one of its OWN queued work
// items and marks it running (queued→active). It enforces the single-active
// invariant (the agent processes one work item at a time): if the agent already
// has an active/waiting_input item, this returns ErrAgentHasActiveWork and the
// selected item stays queued. The HasActiveWorkItem pre-check gives a clean error
// in the common case; the DB UNIQUE partial index (migration 0051) is the ATOMIC
// backstop — under concurrent start_work the second Update violates the unique
// constraint and rolls back, so at most one work item ever becomes active per
// agent (race closed). Ownership-guarded: the work item must belong to agentID.
func (s *Service) StartWork(ctx context.Context, agentID agent.AgentID, workItemID string) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		wi, err := s.workItems.FindByID(txCtx, workItemID)
		if err != nil {
			return err
		}
		if wi.AgentID() != agentID {
			return ErrWorkItemNotForAgent
		}
		active, err := s.workItems.HasActiveWorkItem(txCtx, agentID)
		if err != nil {
			return err
		}
		if active {
			return agent.ErrAgentHasActiveWork // already running one; pick after it settles
		}
		// T130: the open→running gate (sibling of the T83 claim guard). Activating
		// this work item flips its task open→running (via TaskStatusSyncProjector),
		// so a backlog task — neither a real-plan node nor a dispatched pool member —
		// must NOT be startable here. The pm Service (via the injected port) owns the
		// plan/pool decision; a nil gate (test fixtures) skips the check.
		if s.taskRunGate != nil {
			if err := s.taskRunGate.EnsureWorkItemRunnable(txCtx, wi.TaskRef()); err != nil {
				return err
			}
		}
		if err := wi.Activate(now); err != nil { // queued→active
			return err
		}
		// Update hits the UNIQUE partial index; a concurrent start_work that passed
		// the pre-check fails here (only one wins) → its tx rolls back, item stays
		// queued. Map that race-loss to the SAME clean error as the pre-check path
		// so the agent handles both consistently (benign "slot taken, try later"),
		// not a raw driver error (Dev2 #194 review).
		if err := s.workItems.Update(txCtx, wi); err != nil {
			if persistence.IsUniqueViolation(err) {
				return agent.ErrAgentHasActiveWork
			}
			return err
		}
		return nil
	})
}

// FailWork is the agent-PULL failure report (v2.8.1 #278): the agent marks its
// own in-flight work item failed (active|waiting_input → failed) — the symmetric
// terminal to complete. Freeing the active slot lets the next queued item be
// drained (agent pulls next / reconciler advances). Ownership-guarded.
func (s *Service) FailWork(ctx context.Context, agentID agent.AgentID, workItemID string) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		wi, err := s.workItems.FindByID(txCtx, workItemID)
		if err != nil {
			return err
		}
		if wi.AgentID() != agentID {
			return ErrWorkItemNotForAgent
		}
		expectedV := wi.Version()
		if err := wi.Fail(now); err != nil {
			return err
		}
		// CAS race-guard (v2.8.1 #278 PR4): if the reconciler released this active
		// item concurrently (version moved), the agent's fail loses cleanly →
		// ErrWorkItemReassigned (agent pulls fresh) instead of a stale double-write.
		return s.workItems.UpdateCAS(txCtx, wi, expectedV)
	})
}

// PauseWork (v2.8.1 #278 D PR4 scheduling autonomy): the agent sets its active
// work item aside (active→paused) to switch to another, RELEASING the single-
// active slot so it can start_work/resume another. CAS-guarded: a concurrent
// reconciler-release of the (active) item moves the version → ErrWorkItemReassigned
// (agent pulls fresh). reason is recorded for observability. Ownership-guarded.
func (s *Service) PauseWork(ctx context.Context, agentID agent.AgentID, workItemID, reason string) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		wi, err := s.workItems.FindByID(txCtx, workItemID)
		if err != nil {
			return err
		}
		if wi.AgentID() != agentID {
			return ErrWorkItemNotForAgent
		}
		expectedV := wi.Version()
		if err := wi.Pause(now); err != nil { // active→paused
			return err
		}
		slog.Info("agent paused work item",
			"agent_id", string(agentID), "work_item_id", workItemID, "reason", reason)
		return s.workItems.UpdateCAS(txCtx, wi, expectedV)
	})
}

// ResumeWork (v2.8.1 #278 D PR4 scheduling autonomy): the agent resumes a paused
// work item (paused→active), RE-ACQUIRING the single-active slot — single-active-
// enforced like StartWork (HasActiveWorkItem pre-check → clean ErrAgentHasActiveWork
// + DB UNIQUE atomic backstop). The agent must pause/finish its current active item
// first. Ownership-guarded.
func (s *Service) ResumeWork(ctx context.Context, agentID agent.AgentID, workItemID string) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		wi, err := s.workItems.FindByID(txCtx, workItemID)
		if err != nil {
			return err
		}
		if wi.AgentID() != agentID {
			return ErrWorkItemNotForAgent
		}
		active, err := s.workItems.HasActiveWorkItem(txCtx, agentID)
		if err != nil {
			return err
		}
		if active {
			return agent.ErrAgentHasActiveWork // finish/pause the current one first
		}
		if err := wi.Resume(now); err != nil { // paused→active
			return err
		}
		if err := s.workItems.Update(txCtx, wi); err != nil {
			if persistence.IsUniqueViolation(err) {
				return agent.ErrAgentHasActiveWork
			}
			return err
		}
		return nil
	})
}

// ResumeWorkByOperator resumes a PAUSED work item on behalf of an OPERATOR
// (PD/owner) rather than the owning agent (T53). It is the recovery path for a
// plan node whose agent paused its work item and then went idle, leaving the node
// stuck: ResumeWork is agent-ownership-guarded, so no operator could un-stick it.
// This deliberately SKIPS the ownership guard (the CALLER authorizes the operator
// — e.g. pm project-membership) but keeps every OTHER invariant: the item must be
// paused (Resume enforces paused→active), and single-active is still enforced
// (HasActiveWorkItem pre-check + the DB UNIQUE backstop) so a busy agent is not
// double-activated. Returns the owning agent id so the caller can wake it.
func (s *Service) ResumeWorkByOperator(ctx context.Context, workItemID string) (agent.AgentID, error) {
	now := s.clock.Now()
	var agentID agent.AgentID
	err := s.runInTx(ctx, func(txCtx context.Context) error {
		wi, err := s.workItems.FindByID(txCtx, workItemID)
		if err != nil {
			return err
		}
		agentID = wi.AgentID()
		// Operator resume is strictly paused→active: a queued/active/terminal item is
		// not "stuck paused", so reject it (Resume itself would permit queued→active).
		if wi.Status() != agent.WorkItemPaused {
			return agent.ErrWorkItemIllegalMove
		}
		active, err := s.workItems.HasActiveWorkItem(txCtx, wi.AgentID())
		if err != nil {
			return err
		}
		if active {
			return agent.ErrAgentHasActiveWork // the agent is busy on another item
		}
		if err := wi.Resume(now); err != nil { // paused→active
			return err
		}
		if err := s.workItems.Update(txCtx, wi); err != nil {
			if persistence.IsUniqueViolation(err) {
				return agent.ErrAgentHasActiveWork
			}
			return err
		}
		return nil
	})
	return agentID, err
}

// AppendActivity appends an observation event to the Agent's append-only
// activity stream (D2-c-i stdout→activity sink). It records an observation only
// — it does NOT post to any Conversation. Returns the new event id.
func (s *Service) AppendActivity(ctx context.Context, in agent.NewActivityEventInput) (string, error) {
	if in.ID == "" {
		in.ID = s.idgen.NewULID()
	}
	if in.OccurredAt.IsZero() {
		in.OccurredAt = s.clock.Now()
	}
	e, err := agent.NewActivityEvent(in)
	if err != nil {
		return "", err
	}
	if err := s.activity.Append(ctx, e); err != nil {
		return "", err
	}
	return e.ID(), nil
}

// --- reads ------------------------------------------------------------------

// ListAgents returns every Agent in an Organization.
func (s *Service) ListAgents(ctx context.Context, orgID string) ([]*agent.Agent, error) {
	return s.agents.ListByOrg(ctx, orgID)
}

// GetAgent returns one Agent by its execution-entity id.
func (s *Service) GetAgent(ctx context.Context, id agent.AgentID) (*agent.Agent, error) {
	return s.agents.FindByID(ctx, id)
}

// ResolveAgent resolves an Agent by either its execution-entity id (internal /
// back-compat) OR its identity-member id ("agent-<ulid>", the business-layer id
// — v2.7 #185). The webconsole addresses agents by member id, so {id} path
// values are member ids; this bridges to the entity. Entity-id is tried first
// (cheap, no collision risk — member ids are "agent-"-prefixed, entity ids are
// bare ULIDs), then the member→entity bridge.
func (s *Service) ResolveAgent(ctx context.Context, idOrMemberID string) (*agent.Agent, error) {
	a, err := s.agents.FindByID(ctx, agent.AgentID(idOrMemberID))
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, agent.ErrAgentNotFound) {
		return nil, err
	}
	return s.agents.FindByIdentityMemberID(ctx, idOrMemberID)
}

// ArchiveAgent soft-deletes an agent (v2.8 #272) — the SOLE user-facing delete
// path. Guard (b strict): only a settled non-running agent (stopped/error/failed)
// may be archived; running/transitioning → ErrAgentNotStoppedForArchive (409).
// Idempotent: re-archiving an already-archived agent is a 200 no-op (no
// re-persist, no double version bump). Archiving sets lifecycle=archived + clears
// the worker binding (worker_id="") via the dedicated repo.Archive, so the worker
// is freed to re-bind while the agent row is RETAINED (history — #215 chip /
// assignee / GET-by-id). Persist-only (no reconcile emit): the agent is already
// stopped, so there is no running process to reconcile — only the binding is
// released.
func (s *Service) ArchiveAgent(ctx context.Context, id agent.AgentID) error {
	now := s.clock.Now()
	return s.runInTx(ctx, func(txCtx context.Context) error {
		a, err := s.agents.FindByID(txCtx, id)
		if err != nil {
			return err
		}
		if err := a.Archive(now); err != nil {
			if errors.Is(err, agent.ErrAgentAlreadyArchived) {
				return nil // idempotent no-op
			}
			return err
		}
		return s.agents.Archive(txCtx, a)
	})
}

// DeleteAgent hard-deletes an agent (v2.7 #197). When force is false: guards apply
// — the agent must be Stopped (else ErrAgentNotStopped) and have no active/
// waiting_input work item (else ErrAgentHasActiveWork). When force is true (v2.8.1
// force-delete, @oopslink): the process is assumed dead — the guards are skipped
// and the agent's non-terminal WorkItems are swept so none dangle on the deleted
// agent_id (orphan-sweep): in-flight (active/waiting_input) → FailFromAgentDeath
// (cause=agent_death) → Task.Block (reassignable, same path as the reconciler /
// #278 (b)); queued/paused → Cancel. Deletes the agent row, releasing its worker
// binding. The webconsole wraps this in one tx with the identity-member delete for
// an atomic teardown (mirrors #157 — no orphan member left behind).
func (s *Service) DeleteAgent(ctx context.Context, id agent.AgentID, force bool) error {
	a, err := s.agents.FindByID(ctx, id)
	if err != nil {
		return err
	}
	if !force {
		if a.Lifecycle() != agent.LifecycleStopped {
			return agent.ErrAgentNotStopped
		}
		active, err := s.workItems.HasActiveWorkItem(ctx, id)
		if err != nil {
			return err
		}
		if active {
			return agent.ErrAgentHasActiveWork
		}
	} else if err := s.sweepWorkItemsForForceDelete(ctx, id); err != nil {
		return err
	}
	return s.agents.Delete(ctx, id)
}

// sweepWorkItemsForForceDelete terminates every non-terminal WorkItem of an agent
// being force-deleted so nothing references the now-deleted agent_id. In-flight
// items fail (→ Task.Block via the (b) fix, reassignable); queued/paused items are
// canceled (no Task cascade). CAS-guarded: a concurrent agent complete/transition
// wins cleanly (skipped on ErrWorkItemReassigned).
func (s *Service) sweepWorkItemsForForceDelete(ctx context.Context, id agent.AgentID) error {
	items, err := s.workItems.ListByAgent(ctx, id)
	if err != nil {
		return err
	}
	now := s.clock.Now()
	for _, w := range items {
		if w.Status().IsTerminal() {
			continue
		}
		preV := w.Version()
		var terr error
		switch w.Status() {
		case agent.WorkItemActive, agent.WorkItemWaitingInput:
			terr = w.FailFromAgentDeath(now)
		default: // queued / paused
			terr = w.Cancel(now)
		}
		if terr != nil {
			continue
		}
		if err := s.workItems.UpdateCAS(ctx, w, preV); err != nil {
			if errors.Is(err, agent.ErrWorkItemReassigned) {
				continue
			}
			return err
		}
	}
	return nil
}

// AgentsByWorker returns the agents bound to a worker (worker_id binding). Used by
// the worker force-delete handler to detect bound agents (busy-guard / unbind).
func (s *Service) AgentsByWorker(ctx context.Context, workerID string) ([]*agent.Agent, error) {
	return s.agents.ListByWorker(ctx, workerID)
}

// UnbindAgentsFromWorker clears the worker binding of every agent bound to a
// force-deleted worker (v2.8.1), returning the count unbound. The agents become
// worker-less (retained, NOT archived) — re-bindable later. Called by the worker
// force-delete handler (composition layer) so the agent BC owns the binding write.
func (s *Service) UnbindAgentsFromWorker(ctx context.Context, workerID string) (int, error) {
	return s.agents.ClearWorkerBindings(ctx, workerID, s.clock.Now())
}

// ListWorkItems returns an Agent's work items (queue + history).
func (s *Service) ListWorkItems(ctx context.Context, id agent.AgentID) ([]*agent.AgentWorkItem, error) {
	return s.workItems.ListByAgent(ctx, id)
}

// ListActivity returns an Agent's activity events newest-first (id DESC). v2.8
// #274 cursor pagination: before="" = newest page, before=<event-id> = older
// than that cursor; limit>0 caps, limit<=0 = unlimited. The handler resolves the
// default (omitted → 50) and the next_cursor; the service/repo pass it through.
func (s *Service) ListActivity(ctx context.Context, id agent.AgentID, limit int, before string) ([]*agent.AgentActivityEvent, error) {
	return s.activity.ListByAgent(ctx, id, limit, before)
}

// LatestActivityByAgents returns the single most-recent activity event per agent
// across the whole input set in ONE batch query (NO N+1) — the v2.8.1 agents-list
// enrich uses it to render last_activity_at/last_activity_content for the whole
// page without a per-agent round-trip. Pass the execution-entity AgentIDs (the
// agent_activity_events partition key). Agents with no events have no map entry.
func (s *Service) LatestActivityByAgents(ctx context.Context, ids []agent.AgentID) (map[agent.AgentID]*agent.AgentActivityEvent, error) {
	return s.activity.LatestByAgents(ctx, ids)
}
