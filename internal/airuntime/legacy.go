package airuntime

import (
	"context"
	"strings"
)

type LegacyRuntime struct {
	CLI   string `json:"cli,omitempty"`
	Model string `json:"model,omitempty"`
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
