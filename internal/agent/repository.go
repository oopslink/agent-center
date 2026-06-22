package agent

import (
	"context"
	"time"
)

// Repository persists Agent ARs (C1, task #99). The implementation lives in the
// sqlite subpackage and honors persistence.ExecutorFromCtx so C2's
// outbox-driven flows can compose writes in one transaction.
type Repository interface {
	Save(ctx context.Context, a *Agent) error
	Update(ctx context.Context, a *Agent) error
	// Archive persists the v2.8 #272 soft-delete (lifecycle→archived) AND clears
	// the worker_id binding — the one place worker_id changes (Update keeps it
	// immutable). The worker is freed to re-bind; the agent row is retained.
	Archive(ctx context.Context, a *Agent) error
	FindByID(ctx context.Context, id AgentID) (*Agent, error)
	// FindByIdentityMemberID resolves the execution Agent whose identity_member_id
	// equals the given id ("agent-<ulid>", v2.7 #157) — the identity-member ref a
	// conversation carries for an agent participant. Returns ErrAgentNotFound when
	// no agent carries that identity-member binding (or id is empty). The
	// conversational-wake projector (#185 FINDING-J) uses it to resolve a
	// participant referenced by its identity-member id back to the worker-bound
	// execution entity (FindByID keys on the entity id, a different value).
	FindByIdentityMemberID(ctx context.Context, identityMemberID string) (*Agent, error)
	// ListByOrg returns all agents in an Organization.
	ListByOrg(ctx context.Context, orgID string) ([]*Agent, error)
	// ListByWorker returns agents bound to a Worker (one Worker controls many
	// Agents — Environment availability derivation walks this).
	ListByWorker(ctx context.Context, workerID string) ([]*Agent, error)
	// ClearWorkerBindings unbinds every agent of a Worker (worker_id → "") WITHOUT
	// archiving them — the second legitimate place worker_id changes (v2.8.1
	// force-delete: a force-removed Worker's agents become worker-less, retained &
	// re-bindable). Bulk row update (admin destructive op); returns the count
	// unbound. `at` stamps updated_at.
	ClearWorkerBindings(ctx context.Context, workerID string, at time.Time) (int, error)
	// Delete hard-removes the agent row (v2.7 #197). The worker binding lives on
	// the agent row (worker_id column), so deleting the row releases it — the
	// worker is untouched and free to bind a new agent. Idempotent: absent id =
	// no-op. Lifecycle / active-work guards are the service's responsibility.
	Delete(ctx context.Context, id AgentID) error
}

// ActivityEventRepository persists the append-only AgentActivityEvent stream.
type ActivityEventRepository interface {
	Append(ctx context.Context, e *AgentActivityEvent) error
	// ListByAgent returns an agent's events newest-first (id DESC). v2.8 #274
	// cursor pagination: before="" = newest page, before=<event-id> = only events
	// with id < before; limit>0 caps, limit<=0 = unlimited (no cap).
	ListByAgent(ctx context.Context, agentID AgentID, limit int, before string) ([]*AgentActivityEvent, error)
	// ListByWorkItem returns events for one WorkItem segment, oldest first.
	ListByWorkItem(ctx context.Context, workItemRef string) ([]*AgentActivityEvent, error)
	// LatestByAgents batch-fetches the single most-recent activity event per agent
	// across the whole input set in ONE window-function query (NO N+1) — the v2.8.1
	// agents-list enrich uses it to render last_activity_at/last_activity_content for
	// the whole page in one round-trip. Agents with no events have no map entry; empty
	// input → empty map.
	LatestByAgents(ctx context.Context, agentIDs []AgentID) (map[AgentID]*AgentActivityEvent, error)
}
