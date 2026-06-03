package projection

import (
	"context"
)

// AgentWorkItemProjectionRepository is the Observability BC read-model
// repository over agent_work_item_projections (mig 0046) — the new-model
// equivalent of Repository. Owned by Observability; the agent BC must NOT
// import / write this table directly.
type AgentWorkItemProjectionRepository interface {
	// FindByID returns the projection row for the given work item.
	// Returns ErrProjectionNotFound if absent.
	FindByID(ctx context.Context, workItemID string) (*AgentWorkItemProjection, error)

	// FindByIDs returns projections for the given work items in any order.
	// IDs without a row are simply absent from the result map.
	FindByIDs(ctx context.Context, workItemIDs []string) (map[string]*AgentWorkItemProjection, error)

	// UpsertIfFresh tries to INSERT-or-UPDATE a row.
	// staleness rule: if a row already exists with a stored
	// last_activity_at >= update.LastActivityAt, the row is NOT modified and
	// the method returns (existing, false, ErrProjectionStale).
	// On a fresh write, returns (fresh, true, nil).
	UpsertIfFresh(ctx context.Context, workItemID string, update AgentWorkItemProjectionUpdate) (existing AgentWorkItemProjection, fresh bool, err error)

	// List returns projection rows matching filter, newest activity first
	// (ORDER BY last_activity_at DESC). Backs the fleet read path (v2.7 #107
	// Phase-2): the fleet view enumerates live work items. Empty filter = all.
	List(ctx context.Context, filter AgentWorkItemProjectionFilter) ([]*AgentWorkItemProjection, error)
}

// AgentWorkItemProjectionFilter narrows List. Zero value = no filter (all rows).
type AgentWorkItemProjectionFilter struct {
	// Statuses, if non-empty, restricts to rows whose status is in the set
	// (index-backed by idx_awip_status). Fleet passes the live set
	// (queued/active/waiting_input) to exclude terminal work items.
	Statuses []string
	// AgentID, if non-empty, restricts to one agent (index-backed by idx_awip_agent).
	AgentID string
}
