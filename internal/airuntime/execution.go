package airuntime

import "context"

// ExecutionFreezer adapts RuntimeResolver to the production execution-lifecycle
// boundary. Stage one resolves the organization default; later stages can replace
// the selection source without changing the immutable execution contract.
type ExecutionFreezer struct {
	repo     Repository
	resolver *RuntimeResolver
}

func NewExecutionFreezer(repo Repository) *ExecutionFreezer {
	return &ExecutionFreezer{repo: repo, resolver: NewRuntimeResolver(repo)}
}

func (f *ExecutionFreezer) EnsureExecution(ctx context.Context, orgID, executionID string) error {
	// A continuation must not consult mutable Catalog state before checking the
	// frozen execution. Besides preserving bytes, this keeps retry/resume working
	// if the selected profile is later disabled, removed, or temporarily unreadable.
	if _, ok, err := f.repo.GetExecutionSnapshot(ctx, orgID, executionID); err != nil {
		return err
	} else if ok {
		return nil
	}
	// Additive rollout compatibility: an organization that has not selected a
	// runtime default remains on the legacy model router. Once a default exists,
	// resolution is fail-closed and every continuation is pinned to the snapshot.
	catalog, err := f.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return err
	}
	if catalog.DefaultProfileID == "" {
		return nil
	}
	_, _, err = f.resolver.ResolveExecution(ctx, orgID, executionID, RuntimeSelection{Mode: SelectionInherit})
	return err
}
