package service

import (
	"context"
	"errors"
	"time"

	"github.com/oopslink/agent-center/internal/agent"
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
			if st := wi.Status(); st != agent.WorkItemActive && st != agent.WorkItemWaitingInput {
				// Only IN-FLIGHT WorkItems cascade. A QUEUED WorkItem is deliberately
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
		return s.workItems.Update(txCtx, wi)
	})
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

// DeleteAgent hard-deletes a Stopped, idle agent (v2.7 #197). Guards: the agent
// must be Stopped (else ErrAgentNotStopped — operator stops it first) and have no
// active/waiting_input work item (else ErrAgentHasActiveWork). Deletes the agent
// row, which releases its worker binding (worker_id column). The webconsole
// wraps this in one tx with the identity-member delete for an atomic teardown
// (mirrors #157's atomic create — no orphan member left behind).
func (s *Service) DeleteAgent(ctx context.Context, id agent.AgentID) error {
	a, err := s.agents.FindByID(ctx, id)
	if err != nil {
		return err
	}
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
	return s.agents.Delete(ctx, id)
}

// ListWorkItems returns an Agent's work items (queue + history).
func (s *Service) ListWorkItems(ctx context.Context, id agent.AgentID) ([]*agent.AgentWorkItem, error) {
	return s.workItems.ListByAgent(ctx, id)
}

// ListActivity returns an Agent's recent activity events (newest first).
func (s *Service) ListActivity(ctx context.Context, id agent.AgentID, limit int) ([]*agent.AgentActivityEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	return s.activity.ListByAgent(ctx, id, limit)
}
