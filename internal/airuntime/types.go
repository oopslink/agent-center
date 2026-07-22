package airuntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type Reason string

const (
	ReasonCLINotFound       Reason = "runtime_cli_not_found"
	ReasonModelNotFound     Reason = "runtime_model_not_found"
	ReasonIncompatible      Reason = "runtime_model_cli_incompatible"
	ReasonParametersInvalid Reason = "runtime_parameters_invalid"
	ReasonProfileDisabled   Reason = "runtime_profile_disabled"
	ReasonDefaultMissing    Reason = "runtime_default_missing"
	ReasonRevisionConflict  Reason = "runtime_catalog_revision_conflict"
)

type Error struct {
	Reason  Reason         `json:"reason"`
	Message string         `json:"message"`
	Details map[string]any `json:"details"`
}

func (e *Error) Error() string { return e.Message }

type FeatureFlags struct {
	CatalogV2         bool `json:"ai_runtime_catalog_v2"`
	SchedulerMatching bool `json:"ai_runtime_scheduler_matching"`
}

func DefaultFeatureFlags() FeatureFlags { return FeatureFlags{} }

type CLIDefinition struct {
	ID                string          `json:"id"`
	OrgID             string          `json:"-"`
	Key               string          `json:"key"`
	DisplayName       string          `json:"display_name"`
	Executable        string          `json:"executable"`
	VersionConstraint string          `json:"version_constraint,omitempty"`
	RequiredFeatures  []string        `json:"required_features"`
	ParameterSchema   json.RawMessage `json:"parameter_schema"`
	Enabled           bool            `json:"enabled"`
	System            bool            `json:"system"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

type ModelDefinition struct {
	ID                string         `json:"id"`
	OrgID             string         `json:"-"`
	Key               string         `json:"key"`
	ModelKey          string         `json:"model_key"`
	DisplayName       string         `json:"display_name"`
	CompatibleCLIKeys []string       `json:"compatible_cli_keys"`
	DefaultParameters map[string]any `json:"default_parameters"`
	Enabled           bool           `json:"enabled"`
	ContextWindow     int            `json:"context_window,omitempty"`
	InputCost         float64        `json:"input_cost_per_mtok,omitempty"`
	OutputCost        float64        `json:"output_cost_per_mtok,omitempty"`
	Tier              string         `json:"tier,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
}

type RuntimeProfile struct {
	ID          string         `json:"id"`
	OrgID       string         `json:"-"`
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	CLIKey      string         `json:"cli_key"`
	ModelKey    string         `json:"model_key"`
	Parameters  map[string]any `json:"parameters"`
	Enabled     bool           `json:"enabled"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
}

type RuntimeSelection struct {
	Mode       string         `json:"mode"`
	ProfileID  string         `json:"profile_id,omitempty"`
	CLIID      string         `json:"cli_id,omitempty"`
	ModelID    string         `json:"model_id,omitempty"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type RuntimeSnapshot struct {
	SchemaVersion        int            `json:"schema_version"`
	CLIKey               string         `json:"cli_key"`
	CLIExecutable        string         `json:"cli_executable"`
	CLIVersionConstraint string         `json:"cli_version_constraint,omitempty"`
	RequiredFeatures     []string       `json:"required_features"`
	ModelKey             string         `json:"model_key"`
	Parameters           map[string]any `json:"parameters"`
	Source               string         `json:"source"`
	ProfileID            string         `json:"profile_id,omitempty"`
	ResolvedAt           time.Time      `json:"resolved_at"`
}

type Catalog struct {
	OrgID            string            `json:"org_id"`
	Revision         int64             `json:"revision"`
	DefaultProfileID string            `json:"default_runtime_profile_id,omitempty"`
	CLIs             []CLIDefinition   `json:"clis"`
	Models           []ModelDefinition `json:"models"`
	Profiles         []RuntimeProfile  `json:"profiles"`
}

type AuditEvent struct {
	ID, OrgID, Actor, EntityType, EntityKey, Action string
	Before, After                                   json.RawMessage
	Revision                                        int64
	OccurredAt                                      time.Time
}

var stableKeyRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)

func validateKey(field, value string) error {
	if !stableKeyRE.MatchString(value) {
		return fmt.Errorf("%s must be a stable lowercase key", field)
	}
	return nil
}

func normalizeStrings(in []string) ([]string, error) {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			return nil, errors.New("values must be non-empty and unique")
		}
		seen[v] = true
		out = append(out, v)
	}
	return out, nil
}

func validateSchema(raw json.RawMessage) error {
	if len(raw) == 0 {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return fmt.Errorf("parameter_schema: %w", err)
	}
	if typ, ok := schema["type"].(string); ok && typ != "object" {
		return errors.New("parameter_schema root type must be object")
	}
	return nil
}
