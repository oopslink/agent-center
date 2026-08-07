package airuntime

import "strings"

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
