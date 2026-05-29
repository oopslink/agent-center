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
