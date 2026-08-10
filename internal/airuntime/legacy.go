package airuntime

import (
	"context"
	"strings"
)

type LegacyRuntime struct {
	CLI   string `json:"cli,omitempty"`
	Model string `json:"model,omitempty"`
}

type LegacyAgentRuntimeConfig struct {
	CLI              string
	Model            string
	AllowedExecutors []LegacyRuntime
}

type MigrationIssue struct {
	Reason   Reason         `json:"reason"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details"`
	Original LegacyRuntime  `json:"original"`
}

type LegacyFallbackCounter interface {
	IncrementLegacyFallback(objectType string)
}

type LegacyAdapter struct {
	resolver *RuntimeResolver
	counter  LegacyFallbackCounter
}

func NewLegacyAdapter(resolver *RuntimeResolver, counter LegacyFallbackCounter) *LegacyAdapter {
	return &LegacyAdapter{resolver: resolver, counter: counter}
}

// ValidateLegacyAgentRuntimeConfig is the server-side contract for migration-window
// Agent config writes that still persist legacy cli/model/allowed_executors fields.
// It uses the AI Runtime catalog as the selectable source of truth while preserving
// the legacy storage shape: Model is the runtime model string (ModelDefinition.model_key).
func (s *Service) ValidateLegacyAgentRuntimeConfig(ctx context.Context, orgID string, cfg LegacyAgentRuntimeConfig) error {
	catalog, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Model) != "" {
		if err := validateLegacyRuntimePair(catalog, cfg.CLI, cfg.Model); err != nil {
			return err
		}
	}
	for _, exec := range cfg.AllowedExecutors {
		if err := validateLegacyRuntimePair(catalog, exec.CLI, exec.Model); err != nil {
			return err
		}
	}
	return nil
}

// Resolve applies the migration-window precedence: explicit new selection, exact
// legacy mapping, then the organization default. Legacy values are never guessed.
func (a *LegacyAdapter) Resolve(ctx context.Context, orgID, objectType string, selection *RuntimeSelection, legacy LegacyRuntime) (RuntimeSnapshot, *MigrationIssue, error) {
	if selection != nil {
		snapshot, err := a.resolver.Resolve(ctx, orgID, *selection)
		return snapshot, nil, err
	}
	if strings.TrimSpace(legacy.CLI) == "" && strings.TrimSpace(legacy.Model) == "" {
		snapshot, err := a.resolver.Resolve(ctx, orgID, RuntimeSelection{Mode: SelectionInherit})
		return snapshot, nil, err
	}
	catalog, err := a.resolver.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return RuntimeSnapshot{}, nil, err
	}
	cli := findLegacyCLI(catalog.CLIs, legacy.CLI)
	model := findLegacyModel(catalog.Models, legacy.Model)
	if cli == nil || model == nil {
		details := map[string]any{"cli": legacy.CLI, "model": legacy.Model}
		issue := &MigrationIssue{Reason: ReasonLegacyUnmapped, Message: "legacy runtime cannot be mapped exactly", Details: details, Original: legacy}
		return RuntimeSnapshot{}, issue, runtimeError(ReasonLegacyUnmapped, issue.Message, details)
	}
	if a.counter != nil {
		a.counter.IncrementLegacyFallback(objectType)
	}
	snapshot, err := a.resolver.ResolveCatalog(catalog, RuntimeSelection{Mode: SelectionOverride, CLIID: cli.ID, ModelID: model.ID})
	if err == nil {
		snapshot.Source = "legacy"
	}
	return snapshot, nil, err
}

func findLegacyCLI(items []CLIDefinition, raw string) *CLIDefinition {
	raw = strings.TrimSpace(raw)
	for i := range items {
		if raw == items[i].Key || raw == items[i].Executable {
			return &items[i]
		}
	}
	return nil
}
func findLegacyModel(items []ModelDefinition, raw string) *ModelDefinition {
	raw = strings.TrimSpace(raw)
	for i := range items {
		if raw == items[i].Key || raw == items[i].ModelKey {
			return &items[i]
		}
	}
	return nil
}

func validateLegacyRuntimePair(catalog Catalog, cliRaw, modelRaw string) error {
	cli := findLegacyCLI(catalog.CLIs, cliRaw)
	if cli == nil {
		return runtimeError(ReasonCLINotFound, "runtime CLI not found", map[string]any{"cli_key": strings.TrimSpace(cliRaw)})
	}
	if !cli.Enabled {
		return runtimeError(ReasonCLIDisabled, "runtime CLI is disabled", map[string]any{"cli_key": cli.Key})
	}
	model := findLegacyModel(catalog.Models, modelRaw)
	if model == nil {
		return runtimeError(ReasonModelNotFound, "runtime model not found", map[string]any{"model": strings.TrimSpace(modelRaw)})
	}
	if !model.Enabled {
		return runtimeError(ReasonModelDisabled, "runtime model is disabled", map[string]any{"model_key": model.ModelKey})
	}
	if !containsString(model.CompatibleCLIKeys, cli.Key) {
		return runtimeError(ReasonIncompatible, "model is not compatible with CLI", map[string]any{"cli_key": cli.Key, "model_key": model.ModelKey})
	}
	return nil
}
