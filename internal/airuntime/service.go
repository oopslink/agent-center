package airuntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var ErrNotFound = errors.New("ai runtime entity not found")

type IDGenerator func() string

type Service struct {
	repo Repository
	id   IDGenerator
	now  func() time.Time
}

func NewService(repo Repository, id IDGenerator) *Service {
	return &Service{repo: repo, id: id, now: func() time.Time { return time.Now().UTC() }}
}

func (s *Service) Catalog(ctx context.Context, orgID string) (Catalog, error) {
	return s.repo.GetCatalog(ctx, orgID)
}

func (s *Service) CreateCLI(ctx context.Context, orgID, actor string, expected int64, in CLIDefinition) (CLIDefinition, int64, error) {
	in.ID, in.OrgID, in.Key = s.id(), orgID, strings.TrimSpace(in.Key)
	in.DisplayName, in.Executable = strings.TrimSpace(in.DisplayName), strings.TrimSpace(in.Executable)
	if err := validateKey("key", in.Key); err != nil {
		return in, 0, err
	}
	if in.DisplayName == "" || in.Executable == "" {
		return in, 0, errors.New("display_name and executable are required")
	}
	features, err := normalizeStrings(in.RequiredFeatures)
	if err != nil {
		return in, 0, err
	}
	in.RequiredFeatures = features
	if len(in.ParameterSchema) == 0 {
		in.ParameterSchema = json.RawMessage(`{"type":"object","additionalProperties":false}`)
	}
	if err := validateSchema(in.ParameterSchema); err != nil {
		return in, 0, err
	}
	in.CreatedAt, in.UpdatedAt = s.now(), s.now()
	rev, err := s.repo.CreateCLI(ctx, in, expected, s.audit(orgID, actor, "cli", in.Key, "created", nil, in))
	return in, rev, err
}

func (s *Service) UpdateCLI(ctx context.Context, orgID, actor string, expected int64, in CLIDefinition) (CLIDefinition, int64, error) {
	cat, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return in, 0, err
	}
	var old *CLIDefinition
	for i := range cat.CLIs {
		if cat.CLIs[i].ID == in.ID {
			old = &cat.CLIs[i]
			break
		}
	}
	if old == nil {
		return in, 0, ErrNotFound
	}
	if in.Key != "" && in.Key != old.Key {
		return in, 0, errors.New("cli key is immutable")
	}
	in.OrgID, in.Key, in.CreatedAt = orgID, old.Key, old.CreatedAt
	in.System = old.System
	in.UpdatedAt = s.now()
	if in.DisplayName == "" || in.Executable == "" {
		return in, 0, errors.New("display_name and executable are required")
	}
	features, err := normalizeStrings(in.RequiredFeatures)
	if err != nil {
		return in, 0, err
	}
	in.RequiredFeatures = features
	if err := validateSchema(in.ParameterSchema); err != nil {
		return in, 0, err
	}
	if !in.Enabled {
		for _, p := range cat.Profiles {
			if p.CLIKey == in.Key && p.Enabled {
				return in, 0, errors.New("cli is referenced by an enabled profile")
			}
		}
	}
	rev, err := s.repo.UpdateCLI(ctx, in, expected, s.audit(orgID, actor, "cli", in.Key, "updated", old, in))
	return in, rev, err
}

func (s *Service) CreateModel(ctx context.Context, orgID, actor string, expected int64, in ModelDefinition) (ModelDefinition, int64, error) {
	in.ID, in.OrgID, in.Key = s.id(), orgID, strings.TrimSpace(in.Key)
	if err := validateKey("key", in.Key); err != nil {
		return in, 0, err
	}
	if strings.TrimSpace(in.ModelKey) == "" {
		return in, 0, errors.New("model_key is required")
	}
	if in.DisplayName == "" {
		in.DisplayName = in.ModelKey
	}
	keys, err := normalizeStrings(in.CompatibleCLIKeys)
	if err != nil || len(keys) == 0 {
		return in, 0, errors.New("at least one compatible_cli_key is required")
	}
	in.CompatibleCLIKeys = keys
	if in.DefaultParameters == nil {
		in.DefaultParameters = map[string]any{}
	}
	cat, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return in, 0, err
	}
	if err := validateModel(cat, in); err != nil {
		return in, 0, err
	}
	in.CreatedAt, in.UpdatedAt = s.now(), s.now()
	rev, err := s.repo.CreateModel(ctx, in, expected, s.audit(orgID, actor, "model", in.Key, "created", nil, in))
	return in, rev, err
}

func (s *Service) UpdateModel(ctx context.Context, orgID, actor string, expected int64, in ModelDefinition) (ModelDefinition, int64, error) {
	cat, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return in, 0, err
	}
	var old *ModelDefinition
	for i := range cat.Models {
		if cat.Models[i].ID == in.ID {
			old = &cat.Models[i]
			break
		}
	}
	if old == nil {
		return in, 0, ErrNotFound
	}
	if in.Key != "" && in.Key != old.Key {
		return in, 0, errors.New("model key is immutable")
	}
	in.OrgID, in.Key, in.CreatedAt, in.UpdatedAt = orgID, old.Key, old.CreatedAt, s.now()
	if strings.TrimSpace(in.ModelKey) == "" {
		return in, 0, errors.New("model_key is required")
	}
	keys, err := normalizeStrings(in.CompatibleCLIKeys)
	if err != nil || len(keys) == 0 {
		return in, 0, errors.New("at least one compatible_cli_key is required")
	}
	in.CompatibleCLIKeys = keys
	if in.DefaultParameters == nil {
		in.DefaultParameters = map[string]any{}
	}
	if err := validateModel(cat, in); err != nil {
		return in, 0, err
	}
	if !in.Enabled {
		for _, p := range cat.Profiles {
			if p.ModelKey == in.Key && p.Enabled {
				return in, 0, errors.New("model is referenced by an enabled profile")
			}
		}
	}
	rev, err := s.repo.UpdateModel(ctx, in, expected, s.audit(orgID, actor, "model", in.Key, "updated", old, in))
	return in, rev, err
}

func (s *Service) CreateProfile(ctx context.Context, orgID, actor string, expected int64, in RuntimeProfile) (RuntimeProfile, int64, error) {
	in.ID, in.OrgID, in.Key = s.id(), orgID, strings.TrimSpace(in.Key)
	if err := validateKey("key", in.Key); err != nil {
		return in, 0, err
	}
	if strings.TrimSpace(in.Name) == "" {
		return in, 0, errors.New("name is required")
	}
	if in.Parameters == nil {
		in.Parameters = map[string]any{}
	}
	cat, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return in, 0, err
	}
	if err := validateProfile(cat, in); err != nil {
		return in, 0, err
	}
	in.CreatedAt, in.UpdatedAt = s.now(), s.now()
	rev, err := s.repo.CreateProfile(ctx, in, expected, s.audit(orgID, actor, "profile", in.Key, "created", nil, in))
	return in, rev, err
}

func (s *Service) UpdateProfile(ctx context.Context, orgID, actor string, expected int64, in RuntimeProfile) (RuntimeProfile, int64, error) {
	cat, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return in, 0, err
	}
	var old *RuntimeProfile
	for i := range cat.Profiles {
		if cat.Profiles[i].ID == in.ID {
			old = &cat.Profiles[i]
			break
		}
	}
	if old == nil {
		return in, 0, ErrNotFound
	}
	if in.Key != "" && in.Key != old.Key {
		return in, 0, errors.New("profile key is immutable")
	}
	in.OrgID, in.Key, in.CreatedAt, in.UpdatedAt = orgID, old.Key, old.CreatedAt, s.now()
	if strings.TrimSpace(in.Name) == "" {
		return in, 0, errors.New("name is required")
	}
	if in.Parameters == nil {
		in.Parameters = map[string]any{}
	}
	if err := validateProfile(cat, in); err != nil {
		return in, 0, err
	}
	if !in.Enabled && cat.DefaultProfileID == in.ID {
		return in, 0, errors.New("default profile cannot be disabled")
	}
	rev, err := s.repo.UpdateProfile(ctx, in, expected, s.audit(orgID, actor, "profile", in.Key, "updated", old, in))
	return in, rev, err
}

func (s *Service) SetDefaultProfile(ctx context.Context, orgID, actor, profileID string, expected int64) (int64, error) {
	cat, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return 0, err
	}
	var profile *RuntimeProfile
	for i := range cat.Profiles {
		if cat.Profiles[i].ID == profileID {
			profile = &cat.Profiles[i]
			break
		}
	}
	if profile == nil {
		return 0, ErrNotFound
	}
	if !profile.Enabled {
		return 0, &Error{Reason: ReasonProfileDisabled, Message: "default profile must be enabled", Details: map[string]any{"profile_id": profileID}}
	}
	return s.repo.SetDefaultProfile(ctx, orgID, profileID, expected, s.audit(orgID, actor, "catalog", orgID, "default_profile_changed", cat.DefaultProfileID, profileID))
}

func validateModel(cat Catalog, model ModelDefinition) error {
	clis := map[string]CLIDefinition{}
	for _, cli := range cat.CLIs {
		clis[cli.Key] = cli
	}
	for _, key := range model.CompatibleCLIKeys {
		cli, ok := clis[key]
		if !ok {
			return &Error{Reason: ReasonCLINotFound, Message: "compatible CLI not found", Details: map[string]any{"cli_key": key}}
		}
		if err := validateParameters(cli.ParameterSchema, model.DefaultParameters); err != nil {
			return err
		}
	}
	return nil
}

func validateProfile(cat Catalog, p RuntimeProfile) error {
	var cli *CLIDefinition
	var model *ModelDefinition
	for i := range cat.CLIs {
		if cat.CLIs[i].Key == p.CLIKey {
			cli = &cat.CLIs[i]
		}
	}
	for i := range cat.Models {
		if cat.Models[i].Key == p.ModelKey {
			model = &cat.Models[i]
		}
	}
	if cli == nil {
		return &Error{Reason: ReasonCLINotFound, Message: "CLI not found", Details: map[string]any{"cli_key": p.CLIKey}}
	}
	if model == nil {
		return &Error{Reason: ReasonModelNotFound, Message: "model not found", Details: map[string]any{"model_key": p.ModelKey}}
	}
	compatible := false
	for _, key := range model.CompatibleCLIKeys {
		compatible = compatible || key == cli.Key
	}
	if !compatible {
		return &Error{Reason: ReasonIncompatible, Message: "model is not compatible with CLI", Details: map[string]any{"cli_key": cli.Key, "model_key": model.Key}}
	}
	merged := map[string]any{}
	for k, v := range model.DefaultParameters {
		merged[k] = v
	}
	for k, v := range p.Parameters {
		merged[k] = v
	}
	return validateParameters(cli.ParameterSchema, merged)
}

func validateParameters(raw json.RawMessage, params map[string]any) error {
	if len(raw) == 0 {
		return nil
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return err
	}
	props, _ := schema["properties"].(map[string]any)
	if additional, ok := schema["additionalProperties"].(bool); ok && !additional {
		for k := range params {
			if _, ok := props[k]; !ok {
				return parameterError(k, "is not allowed")
			}
		}
	}
	if required, ok := schema["required"].([]any); ok {
		for _, v := range required {
			k, _ := v.(string)
			if _, ok := params[k]; !ok {
				return parameterError(k, "is required")
			}
		}
	}
	for k, v := range params {
		rule, _ := props[k].(map[string]any)
		typ, _ := rule["type"].(string)
		if typ != "" && !matchesType(typ, v) {
			return parameterError(k, fmt.Sprintf("must be %s", typ))
		}
		if enum, ok := rule["enum"].([]any); ok {
			found := false
			for _, x := range enum {
				found = found || fmt.Sprint(x) == fmt.Sprint(v)
			}
			if !found {
				return parameterError(k, "is not in enum")
			}
		}
	}
	return nil
}

func parameterError(field, msg string) error {
	return &Error{Reason: ReasonParametersInvalid, Message: "runtime parameters are invalid", Details: map[string]any{"field": field, "error": msg}}
}
func matchesType(t string, v any) bool {
	switch t {
	case "string":
		_, ok := v.(string)
		return ok
	case "boolean":
		_, ok := v.(bool)
		return ok
	case "number":
		_, ok := v.(float64)
		return ok
	case "integer":
		f, ok := v.(float64)
		return ok && f == float64(int64(f))
	case "object":
		_, ok := v.(map[string]any)
		return ok
	case "array":
		_, ok := v.([]any)
		return ok
	}
	return true
}

func (s *Service) audit(org, actor, entityType, entityKey, action string, before, after any) AuditEvent {
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	return AuditEvent{ID: s.id(), OrgID: org, Actor: actor, EntityType: entityType, EntityKey: entityKey, Action: action, Before: b, After: a, OccurredAt: s.now()}
}
