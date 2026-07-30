package airuntime

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
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
	ReasonSchemaUnsupported Reason = "runtime_schema_unsupported"
	ReasonSecretInvalid     Reason = "runtime_secret_reference_invalid"
	ReasonImportUnsupported Reason = "runtime_import_schema_unsupported"
	ReasonImportInvalid     Reason = "runtime_import_validation_failed"
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
	AgentExecution    bool `json:"ai_runtime_agent_execution"`
	ShadowResolve     bool `json:"ai_runtime_shadow_resolve"`
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
	Version     int64          `json:"version"`
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
	CatalogRevision      int64          `json:"catalog_revision"`
	CLIKey               string         `json:"cli_key"`
	CLIExecutable        string         `json:"cli_executable"`
	CLIVersionConstraint string         `json:"cli_version_constraint,omitempty"`
	RequiredFeatures     []string       `json:"required_features"`
	ModelKey             string         `json:"model_key"`
	Parameters           map[string]any `json:"parameters"`
	ParametersDigest     string         `json:"parameters_digest"`
	Source               string         `json:"source"`
	ProfileID            string         `json:"profile_id,omitempty"`
	ProfileKey           string         `json:"profile_key,omitempty"`
	ProfileVersion       int64          `json:"profile_version,omitempty"`
	ResolvedAt           time.Time      `json:"resolved_at"`
}

type AgentRuntimeSelection struct {
	AgentID   string           `json:"agent_id"`
	Selection RuntimeSelection `json:"selection"`
	UpdatedAt time.Time        `json:"updated_at"`
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
	if err := validateSupportedSchema(root, "$"); err != nil {
		return err
	}
	_, err := compileSchema(document)
	if err != nil {
		return fmt.Errorf("parameter_schema: %w", err)
	}
	return nil
}

var supportedSchemaKeywords = map[string]bool{
	"type": true, "properties": true, "required": true, "enum": true, "const": true,
	"minimum": true, "maximum": true, "exclusiveMinimum": true, "exclusiveMaximum": true,
	"multipleOf": true, "minLength": true, "maxLength": true, "pattern": true,
	"items": true, "minItems": true, "maxItems": true, "uniqueItems": true,
	"additionalProperties": true, "default": true, "description": true, "title": true,
	"x-secret": true,
}

func validateSupportedSchema(schema map[string]any, path string) error {
	for keyword := range schema {
		if !supportedSchemaKeywords[keyword] {
			return &Error{Reason: ReasonSchemaUnsupported, Message: "parameter schema uses an unsupported keyword", Details: map[string]any{"path": path, "keyword": keyword}}
		}
	}
	if secret, ok := schema["x-secret"]; ok {
		flag, valid := secret.(bool)
		if !valid || !flag {
			return fmt.Errorf("%s.x-secret must be true when present", path)
		}
		if typ, _ := schema["type"].(string); typ != "object" {
			return fmt.Errorf("%s secret parameter must have type object", path)
		}
	}
	if properties, ok := schema["properties"].(map[string]any); ok {
		keys := make([]string, 0, len(properties))
		for key := range properties {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			child, ok := properties[key].(map[string]any)
			if !ok {
				return fmt.Errorf("%s.properties.%s must be an object schema", path, key)
			}
			if err := validateSupportedSchema(child, path+"."+key); err != nil {
				return err
			}
		}
	}
	if item, ok := schema["items"]; ok {
		child, ok := item.(map[string]any)
		if !ok {
			return fmt.Errorf("%s.items must be an object schema", path)
		}
		if err := validateSupportedSchema(child, path+"[]"); err != nil {
			return err
		}
	}
	if additional, ok := schema["additionalProperties"]; ok {
		if _, valid := additional.(bool); !valid {
			return &Error{Reason: ReasonSchemaUnsupported, Message: "schema-valued additionalProperties is unsupported", Details: map[string]any{"path": path, "keyword": "additionalProperties"}}
		}
	}
	return nil
}

// CanonicalJSON returns the stable JSON representation used by snapshots,
// digests, import tokens and round-trip comparison. encoding/json sorts map
// keys; the explicit deep copy also detaches the returned bytes from callers.
func CanonicalJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func parametersDigest(parameters map[string]any) (string, error) {
	redacted := RedactValue(parameters)
	data, err := CanonicalJSON(redacted)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("sha256:%x", sha256.Sum256(data)), nil
}

// RedactValue deep-copies arbitrary JSON-shaped data and removes likely
// plaintext credentials. Secret references remain useful but never expose a
// resolved value.
func RedactValue(v any) any {
	switch value := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(value))
		for key, child := range value {
			lower := strings.ToLower(key)
			if lower == "secret_ref" {
				out[key] = child
				continue
			}
			if strings.Contains(lower, "secret") || strings.Contains(lower, "password") ||
				strings.Contains(lower, "token") || strings.Contains(lower, "api_key") {
				if ref := secretReference(child); ref != "" {
					out[key] = map[string]any{"secret_ref": ref}
				} else {
					out[key] = "[REDACTED]"
				}
				continue
			}
			out[key] = RedactValue(child)
		}
		return out
	case []any:
		out := make([]any, len(value))
		for i := range value {
			out[i] = RedactValue(value[i])
		}
		return out
	default:
		return value
	}
}

func secretReference(v any) string {
	obj, ok := v.(map[string]any)
	if !ok || len(obj) != 1 {
		return ""
	}
	ref, _ := obj["secret_ref"].(string)
	return strings.TrimSpace(ref)
}

func compileSchema(document any) (*jsonschema.Schema, error) {
	compiler := jsonschema.NewCompiler()
	compiler.DefaultDraft(jsonschema.Draft2020)
	if err := compiler.AddResource("urn:agent-center:ai-runtime-parameters", document); err != nil {
		return nil, err
	}
	return compiler.Compile("urn:agent-center:ai-runtime-parameters")
}
