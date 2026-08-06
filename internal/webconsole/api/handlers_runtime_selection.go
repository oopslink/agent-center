package api

import (
	"context"
	"strings"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/airuntime"
	"github.com/oopslink/agent-center/internal/team"
)

func roleConfigFromInput(ri roleInputReq) team.RoleConfig {
	return team.RoleConfig{
		Role:             ri.Role,
		CLI:              ri.CLI,
		Model:            ri.Model,
		RuntimeSelection: teamRuntimeSelectionFromInput(ri.RuntimeSelection, ri.CLI, ri.Model),
		CapabilityTags:   splitTags(ri.Tags),
		MaxConcurrency:   ri.MaxConcurrency,
	}
}

func roleConfigFromTemplateInput(rr templateRoleReq) team.RoleConfig {
	return team.RoleConfig{
		Role:             rr.Role,
		CLI:              rr.CLI,
		Model:            rr.Model,
		RuntimeSelection: teamRuntimeSelectionFromInput(rr.RuntimeSelection, rr.CLI, rr.Model),
		CapabilityTags:   rr.CapabilityTags,
		MaxConcurrency:   rr.MaxConcurrency,
	}
}

func teamRuntimeSelectionFromInput(in team.RuntimeSelection, legacyCLI, legacyModel string) team.RuntimeSelection {
	sel, err := team.NormalizeRuntimeSelection(in, legacyCLI, legacyModel)
	if err != nil {
		return in
	}
	return sel
}

func teamRuntimeSelectionView(sel team.RuntimeSelection) map[string]any {
	return map[string]any{
		"mode":       sel.Mode,
		"profile_id": sel.ProfileID,
		"cli_id":     sel.CLIID,
		"model_id":   sel.ModelID,
		"parameters": sel.Parameters,
	}
}

func resolveTeamRoleRuntimeSelections(ctx context.Context, d HandlerDeps, orgID string, roles []team.RoleConfig) ([]team.RoleConfig, error) {
	if len(roles) == 0 || d.RuntimeCatalog == nil {
		return roles, nil
	}
	catalog, err := d.RuntimeCatalog.Catalog(ctx, orgID)
	if err != nil {
		return nil, err
	}
	resolver := airuntime.NewRuntimeResolver(nil)
	out := make([]team.RoleConfig, len(roles))
	copy(out, roles)
	for i := range out {
		sel, err := team.NormalizeRuntimeSelection(out[i].RuntimeSelection, out[i].CLI, out[i].Model)
		if err != nil {
			return nil, err
		}
		snapshot, err := resolver.ResolveCatalog(catalog, teamSelectionToRuntimeSelection(sel))
		if err != nil {
			return nil, err
		}
		out[i].RuntimeSelection = sel
		out[i].CLI = snapshot.CLIKey
		out[i].Model = snapshot.ModelKey
	}
	return out, nil
}

func resolveTemplateSlots(ctx context.Context, d HandlerDeps, orgID string, slots []team.RoleSlot) error {
	configs := make([]team.RoleConfig, 0, len(slots))
	for _, slot := range slots {
		configs = append(configs, slot.Config)
	}
	resolved, err := resolveTeamRoleRuntimeSelections(ctx, d, orgID, configs)
	if err != nil {
		return err
	}
	for i := range slots {
		slots[i].Config = resolved[i]
	}
	return nil
}

func teamSelectionToRuntimeSelection(sel team.RuntimeSelection) airuntime.RuntimeSelection {
	return airuntime.RuntimeSelection{
		Mode:       sel.Mode,
		ProfileID:  sel.ProfileID,
		CLIID:      sel.CLIID,
		ModelID:    sel.ModelID,
		Parameters: sel.Parameters,
	}
}

func resolveExecutorRuntimeSelections(ctx context.Context, d HandlerDeps, orgID string, execs []agentbc.ExecutorProfile) ([]agentbc.ExecutorProfile, error) {
	if len(execs) == 0 {
		return execs, nil
	}
	needsCatalog := false
	for _, exec := range execs {
		if exec.RuntimeSelection != nil && strings.TrimSpace(exec.RuntimeSelection.Mode) != "" {
			needsCatalog = true
			break
		}
	}
	if !needsCatalog {
		return execs, nil
	}
	if d.RuntimeCatalog == nil {
		return nil, &airuntime.Error{Reason: airuntime.ReasonSelectionInvalid, Message: "AI Runtime Catalog is not configured", Details: map[string]any{"field": "allowed_executors.runtime_selection"}}
	}
	catalog, err := d.RuntimeCatalog.Catalog(ctx, orgID)
	if err != nil {
		return nil, err
	}
	resolver := airuntime.NewRuntimeResolver(nil)
	out := make([]agentbc.ExecutorProfile, len(execs))
	copy(out, execs)
	for i := range out {
		if out[i].RuntimeSelection == nil || strings.TrimSpace(out[i].RuntimeSelection.Mode) == "" {
			continue
		}
		snapshot, err := resolver.ResolveCatalog(catalog, executorSelectionToRuntimeSelection(*out[i].RuntimeSelection))
		if err != nil {
			return nil, err
		}
		out[i].CLI = snapshot.CLIKey
		out[i].Model = snapshot.ModelKey
	}
	return out, nil
}

func executorSelectionToRuntimeSelection(sel agentbc.RuntimeSelection) airuntime.RuntimeSelection {
	return airuntime.RuntimeSelection{
		Mode:       sel.Mode,
		ProfileID:  sel.ProfileID,
		CLIID:      sel.CLIID,
		ModelID:    sel.ModelID,
		Parameters: sel.Parameters,
	}
}
