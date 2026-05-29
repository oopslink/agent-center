package agent

import "context"

// Repository persists Agent ARs (C1, task #99). The implementation lives in the
// sqlite subpackage and honors persistence.ExecutorFromCtx so C2's
// outbox-driven flows can compose writes in one transaction.
type Repository interface {
	Save(ctx context.Context, a *Agent) error
	Update(ctx context.Context, a *Agent) error
	FindByID(ctx context.Context, id AgentID) (*Agent, error)
	// ListByOrg returns all agents in an Organization.
	ListByOrg(ctx context.Context, orgID string) ([]*Agent, error)
	// ListByWorker returns agents bound to a Worker (one Worker controls many
	// Agents — Environment availability derivation walks this).
	ListByWorker(ctx context.Context, workerID string) ([]*Agent, error)
}

// WorkItemRepository persists AgentWorkItem ARs (C2). ExecutorFromCtx-aware so
// the B2 outbox projector can supersede-old + create-new in one transaction.
type WorkItemRepository interface {
	Save(ctx context.Context, w *AgentWorkItem) error
	Update(ctx context.Context, w *AgentWorkItem) error
	FindByID(ctx context.Context, id string) (*AgentWorkItem, error)
	// ListByAgent returns an agent's work items (queue + history).
	ListByAgent(ctx context.Context, agentID AgentID) ([]*AgentWorkItem, error)
	// ListByTask returns the work items for a Task across reassignments
	// (the superseded chain).
	ListByTask(ctx context.Context, taskRef string) ([]*AgentWorkItem, error)
	// HasActiveWorkItem reports whether the agent has an active/waiting_input
	// item — the input to availability derivation (OQ2).
	HasActiveWorkItem(ctx context.Context, agentID AgentID) (bool, error)
}

// ActivityEventRepository persists the append-only AgentActivityEvent stream.
type ActivityEventRepository interface {
	Append(ctx context.Context, e *AgentActivityEvent) error
	// ListByAgent returns recent events for an agent, newest first, up to limit.
	ListByAgent(ctx context.Context, agentID AgentID, limit int) ([]*AgentActivityEvent, error)
	// ListByWorkItem returns events for one WorkItem segment, oldest first.
	ListByWorkItem(ctx context.Context, workItemRef string) ([]*AgentActivityEvent, error)
}
