package api

import (
	"context"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/airuntime"
	"github.com/oopslink/agent-center/internal/team"
)

func resolveRuntimeSelectionMirror(ctx context.Context, svc *airuntime.Service, orgID string, selection *airuntime.RuntimeSelection) (*airuntime.RuntimeSelection, string, string, error) {
	if selection == nil || airuntime.SelectionIsZero(*selection) {
		return nil, "", "", nil
	}
	if svc == nil {
		return nil, "", "", &airuntime.Error{
			Reason:  airuntime.ReasonSelectionInvalid,
			Message: "AI Runtime Catalog is not configured",
		}
	}
	validated, err := svc.ValidateSelection(ctx, orgID, *selection)
	if err != nil {
		return nil, "", "", err
	}
	return &validated.Selection, validated.Snapshot.CLIKey, validated.Snapshot.ModelKey, nil
}

func roleConfigFromInput(ctx context.Context, svc *airuntime.Service, orgID string, in roleInputReq) (team.RoleConfig, error) {
	selection, cli, model, err := resolveRuntimeSelectionMirror(ctx, svc, orgID, in.RuntimeSelection)
	if err != nil {
		return team.RoleConfig{}, err
	}
	if selection == nil {
		cli, model = in.CLI, in.Model
	}
	return team.RoleConfig{
		Role: in.Role, CLI: cli, Model: model, RuntimeSelection: selection,
		CapabilityTags: splitTags(in.Tags), MaxConcurrency: in.MaxConcurrency,
	}, nil
}

func resolveExecutorRuntimeSelections(ctx context.Context, svc *airuntime.Service, orgID string, in []agentbc.ExecutorProfile) ([]agentbc.ExecutorProfile, error) {
	if len(in) == 0 {
		return in, nil
	}
	out := make([]agentbc.ExecutorProfile, 0, len(in))
	for _, candidate := range in {
		selection, cli, model, err := resolveRuntimeSelectionMirror(ctx, svc, orgID, candidate.RuntimeSelection)
		if err != nil {
			return nil, err
		}
		if selection != nil {
			candidate.RuntimeSelection = selection
			candidate.CLI = cli
			candidate.Model = model
		}
		out = append(out, candidate)
	}
	return out, nil
}
