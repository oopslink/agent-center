package airuntime

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	jsonschema "github.com/santhosh-tekuri/jsonschema/v6"
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
	ReasonCLIDisabled       Reason = "runtime_cli_disabled"
	ReasonModelDisabled     Reason = "runtime_model_disabled"
	ReasonProfileNotFound   Reason = "runtime_profile_not_found"
	ReasonSelectionInvalid  Reason = "runtime_selection_invalid"
	ReasonLegacyUnmapped    Reason = "runtime_legacy_unmapped"
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

const (
	SelectionInherit  = "inherit"
	SelectionProfile  = "profile"
	SelectionOverride = "override"
)

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

type RuntimeSelectionValidation struct {
	Selection RuntimeSelection `json:"selection"`
	Snapshot  RuntimeSnapshot  `json:"snapshot"`
}

type RuntimeReferenceCounts struct {
	ProfileID                   string `json:"profile_id,omitempty"`
	DefaultProfile              int    `json:"default_profile"`
	AgentProfileSelections      int    `json:"agent_profile_selections"`
	ExecutorProfileSelections   int    `json:"executor_profile_selections"`
	TeamRoleProfileSelections   int    `json:"team_role_profile_selections"`
	TeamRoleInheritSelections   int    `json:"team_role_inherit_selections"`
	HistoricalExecutionSnapshot int    `json:"historical_execution_snapshot"`
}

type RuntimeImpactPreview struct {
	EntityType       string                 `json:"entity_type"`
	EntityID         string                 `json:"entity_id,omitempty"`
	Action           string                 `json:"action"`
	ReferenceCounts  RuntimeReferenceCounts `json:"reference_counts"`
	AffectedNewRuns  int                    `json:"affected_new_runs"`
	HistoricalNote   string                 `json:"historical_note"`
	GrayReleaseReady bool                   `json:"gray_release_ready"`
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
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return fmt.Errorf("parameter_schema: %w", err)
	}
	root, ok := document.(map[string]any)
	if !ok {
		return errors.New("parameter_schema root must be an object schema")
	}
	if typ, ok := root["type"].(string); ok && typ != "object" {
		return errors.New("parameter_schema root type must be object")
	}
	_, err := compileSchema(document)
	if err != nil {
		return fmt.Errorf("parameter_schema: %w", err)
	}
	return nil
}

func compileSchema(document any) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("urn:agent-center:ai-runtime-parameters", document); err != nil {
		return nil, err
	}
	return compiler.Compile("urn:agent-center:ai-runtime-parameters")
}
