package airuntime

import (
	"context"
	"fmt"
	"strings"
)

// ExecutionFreezer adapts RuntimeResolver to the production execution-lifecycle
// boundary. Stage one resolves the organization default; later stages can replace
// the selection source without changing the immutable execution contract.
type ExecutionFreezer struct {
	repo      Repository
	resolver  *RuntimeResolver
	freeze    bool
	shadowLog func(string, ...any)
}

func NewExecutionFreezer(repo Repository) *ExecutionFreezer {
	return &ExecutionFreezer{repo: repo, resolver: NewRuntimeResolver(repo), freeze: true}
}

func NewShadowExecutionFreezer(repo Repository, log func(string, ...any)) *ExecutionFreezer {
	return &ExecutionFreezer{repo: repo, resolver: NewRuntimeResolver(repo), shadowLog: log}
}

func (f *ExecutionFreezer) GetExecution(ctx context.Context, orgID, executionID string) (any, bool, error) {
	return f.repo.GetExecutionSnapshot(ctx, orgID, executionID)
}

func (f *ExecutionFreezer) EnsureExecution(ctx context.Context, orgID, executionID string, agentIDs ...string) error {
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
	selection := RuntimeSelection{Mode: SelectionInherit}
	agentID := ""
	if len(agentIDs) > 0 {
		agentID = agentIDs[0]
	}
	if selections, ok := f.repo.(AgentSelectionRepository); ok && agentID != "" {
		if saved, found, err := selections.GetAgentSelection(ctx, orgID, agentID); err != nil {
			return err
		} else if found {
			selection = saved
		}
	}
	catalog, err := f.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return err
	}
	if catalog.DefaultProfileID == "" {
		return nil
	}
	if !f.freeze {
		snapshot, err := f.resolver.ResolveCatalog(catalog, selection)
		if err != nil {
			if f.shadowLog != nil {
				f.shadowLog("ai-runtime shadow resolve execution=%s result=error error=%v", executionID, err)
			}
			return nil
		}
		if f.shadowLog != nil {
			f.shadowLog("ai-runtime shadow resolve execution=%s result=diff cli=%s model=%s source=%s", executionID, snapshot.CLIKey, snapshot.ModelKey, snapshot.Source)
		}
		return nil
	}
	_, _, err = f.resolver.ResolveExecution(ctx, orgID, executionID, selection)
	return err
}

// EnsureInlineCompatible is the production start gate for supervisor_inline work.
// The resident supervisor is configured from the Agent's current RuntimeSelection;
// it may consume an already-frozen execution only when the immutable CLI/model match.
func (f *ExecutionFreezer) EnsureInlineCompatible(ctx context.Context, orgID, executionID, agentID string) error {
	raw, ok, err := f.repo.GetExecutionSnapshot(ctx, orgID, executionID)
	if err != nil || !ok {
		return err
	}
	snapshot := raw
	selection := RuntimeSelection{Mode: SelectionInherit}
	if selections, supported := f.repo.(AgentSelectionRepository); supported && strings.TrimSpace(agentID) != "" {
		if saved, found, getErr := selections.GetAgentSelection(ctx, orgID, agentID); getErr != nil {
			return getErr
		} else if found {
			selection = saved
		}
	}
	catalog, err := f.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return err
	}
	resident, err := f.resolver.ResolveCatalog(catalog, selection)
	if err != nil {
		return err
	}
	var mismatches []string
	if !strings.EqualFold(strings.TrimSpace(resident.CLIKey), strings.TrimSpace(snapshot.CLIKey)) {
		mismatches = append(mismatches, fmt.Sprintf("cli resident=%q snapshot=%q", resident.CLIKey, snapshot.CLIKey))
	}
	if strings.TrimSpace(resident.ModelKey) != strings.TrimSpace(snapshot.ModelKey) {
		mismatches = append(mismatches, fmt.Sprintf("model resident=%q snapshot=%q", resident.ModelKey, snapshot.ModelKey))
	}
	if len(mismatches) > 0 {
		return fmt.Errorf("ai-runtime: supervisor_inline runtime mismatch (%s); use executor_fork or select a Profile matching the resident supervisor session",
			strings.Join(mismatches, ", "))
	}
	return nil
}
