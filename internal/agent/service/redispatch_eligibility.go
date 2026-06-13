package service

import (
	"context"
	"errors"
	"strings"

	"github.com/oopslink/agent-center/internal/agent"
)

// RedispatchEligibility answers the agent-BC half of the v2.9.1 P1 auto-redispatch
// decision (task-aab863b3): GIVEN a stuck PM task assigned to `agentRef`, is its
// assignee agent ready to receive a fresh WorkItem right now?
//
// It is the adapter the ProjectManager's AutoRedispatchReconciler depends on
// through a port (the pm BC must not import agent repos directly — DDD BC seam,
// conventions §0). It satisfies that port STRUCTURALLY (method shape only), so the
// dependency edge stays pm/service → (interface) ← agent/service with no import
// cycle; the cli wiring layer is the only place that knows both concrete types.
type RedispatchEligibility struct {
	agents    agent.Repository
	workItems agent.WorkItemRepository
}

// NewRedispatchEligibility wires the adapter over the raw agent repositories.
func NewRedispatchEligibility(agents agent.Repository, workItems agent.WorkItemRepository) *RedispatchEligibility {
	return &RedispatchEligibility{agents: agents, workItems: workItems}
}

// Eligible reports whether a stuck task (taskRef = "pm://tasks/<id>") assigned to
// agentRef should be auto-redispatched NOW. It is true iff ALL hold:
//
//   - agentRef is an agent ("agent:<id>") — humans are never auto-redispatched
//     (false, nil; the human reassigns / the conversation @mention is their cue);
//   - the agent resolves AND its lifecycle is `running` — i.e. it is back online
//     and able to accept work. A stopped/failed/resetting agent is NOT redispatched
//     to (we wait for it to return); an unresolved agent is treated as ineligible
//     (false, nil) rather than an error so one bad ref never stalls the sweep;
//   - the task has NO live (non-terminal) WorkItem already in flight. After a stale
//     release the prior WorkItem is `failed` (terminal) → eligible; but a still-
//     `queued` item from a PRIOR auto-redispatch (awaiting pickup) or an `active`
//     one (the agent just took it) means a dispatch is already pending → NOT
//     eligible, so the reconciler never re-mints/­re-wakes every tick.
//
// A non-nil error is only returned for an infrastructure failure (repo read), which
// the caller logs-and-skips for that task — never fatal to the loop.
func (e *RedispatchEligibility) Eligible(ctx context.Context, agentRef, taskRef string) (bool, error) {
	rawID, ok := agentIDFromAssignee(agentRef)
	if !ok {
		return false, nil // not an agent assignee (human / empty)
	}
	a, err := e.resolveAgent(ctx, rawID)
	if err != nil {
		if errors.Is(err, agent.ErrAgentNotFound) {
			return false, nil // unresolved assignee → skip, not fatal
		}
		return false, err
	}
	if a.Lifecycle() != agent.LifecycleRunning {
		return false, nil // agent not back online yet → wait
	}
	live, err := e.taskHasLiveWorkItem(ctx, taskRef)
	if err != nil {
		return false, err
	}
	if live {
		return false, nil // a dispatch is already in flight (queued/active/waiting)
	}
	return true, nil
}

// taskHasLiveWorkItem reports whether any WorkItem for taskRef is non-terminal
// (queued / active / waiting_input / paused). The release that made the task stuck
// left the prior item `failed` (terminal), so this is false right after a stale
// release and true once a (re)dispatch has minted a fresh queued item.
func (e *RedispatchEligibility) taskHasLiveWorkItem(ctx context.Context, taskRef string) (bool, error) {
	items, err := e.workItems.ListByTask(ctx, taskRef)
	if err != nil {
		return false, err
	}
	for _, wi := range items {
		if !wi.Status().IsTerminal() {
			return true, nil
		}
	}
	return false, nil
}

// resolveAgent resolves rawID — which may be the execution-entity id OR the
// identity-member id ("agent-<ulid>", v2.7 #185, the id the assign path carries) —
// to the Agent. Entity-id first (cheap, no collision), then the member→entity
// bridge. Mirrors pm/service.resolveAgentByEither (kept BC-local to avoid a
// cross-BC import).
func (e *RedispatchEligibility) resolveAgent(ctx context.Context, rawID string) (*agent.Agent, error) {
	a, err := e.agents.FindByID(ctx, agent.AgentID(rawID))
	if err == nil {
		return a, nil
	}
	if !errors.Is(err, agent.ErrAgentNotFound) {
		return nil, err
	}
	return e.agents.FindByIdentityMemberID(ctx, rawID)
}

// agentIDFromAssignee extracts the agent id from an "agent:<id>" identity ref.
// ok=false for a human ("user:<id>") or empty ref.
func agentIDFromAssignee(ref string) (string, bool) {
	const p = "agent:"
	if strings.HasPrefix(ref, p) && len(ref) > len(p) {
		return strings.TrimPrefix(ref, p), true
	}
	return "", false
}
