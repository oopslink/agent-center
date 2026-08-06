package airuntime

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("ai runtime entity not found")

type IDGenerator func() string

type Service struct {
	repo                Repository
	id                  IDGenerator
	now                 func() time.Time
	importTokenKey      []byte
	importTokenLifetime time.Duration
	coverage            CoverageProvider
}

var (
	defaultValidationKey     []byte
	defaultValidationKeyOnce sync.Once
)

// NewService creates a service whose validation tokens remain valid across
// service instances in this process. Production servers should use
// NewServiceWithValidationKey with restart-stable key material.
func NewService(repo Repository, id IDGenerator) *Service {
	defaultValidationKeyOnce.Do(func() {
		defaultValidationKey = make([]byte, 32)
		if _, err := rand.Read(defaultValidationKey); err != nil {
			panic("generate AI Runtime import validation key: " + err.Error())
		}
	})
	return newService(repo, id, defaultValidationKey)
}

// NewServiceWithValidationKey creates a service with a stable import-token key.
// Every constructor in an API process, and every replica serving the same API,
// must receive the same restart-stable secret.
func NewServiceWithValidationKey(repo Repository, id IDGenerator, key []byte) *Service {
	if len(key) < 32 {
		panic("AI Runtime import validation key must contain at least 32 bytes")
	}
	return newService(repo, id, append([]byte(nil), key...))
}

func (s *Service) SetCoverageProvider(provider CoverageProvider) {
	s.coverage = provider
}

func newService(repo Repository, id IDGenerator, key []byte) *Service {
	return &Service{
		repo:                repo,
		id:                  id,
		now:                 func() time.Time { return time.Now().UTC() },
		importTokenKey:      key,
		importTokenLifetime: 10 * time.Minute,
	}
}

func (s *Service) Catalog(ctx context.Context, orgID string) (Catalog, error) {
	catalog, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return Catalog{}, err
	}
	if s.coverage == nil {
		catalog.Coverage = []RuntimeCoverage{}
		return catalog, nil
	}
	coverage, err := s.coverage.Coverage(ctx, catalog)
	if err != nil {
		catalog.Coverage = []RuntimeCoverage{}
		return catalog, nil
	}
	catalog.Coverage = normalizeCoverageScopes(coverage)
	return catalog, nil
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
	oldIndex := -1
	for i := range cat.CLIs {
		if cat.CLIs[i].ID == in.ID {
			oldIndex = i
			break
		}
	}
	if oldIndex < 0 {
		return in, 0, ErrNotFound
	}
	old := cat.CLIs[oldIndex]
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
	candidate := cat
	candidate.CLIs[oldIndex] = in
	for _, model := range candidate.Models {
		if err := validateModel(candidate, model); err != nil {
			return in, 0, err
		}
	}
	for _, profile := range candidate.Profiles {
		if profile.Enabled {
			if err := validateProfile(candidate, profile); err != nil {
				return in, 0, err
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
	oldIndex := -1
	for i := range cat.Models {
		if cat.Models[i].ID == in.ID {
			oldIndex = i
			break
		}
	}
	if oldIndex < 0 {
		return in, 0, ErrNotFound
	}
	old := cat.Models[oldIndex]
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
	candidate := cat
	candidate.Models[oldIndex] = in
	for _, profile := range candidate.Profiles {
		if profile.Enabled {
			if err := validateProfile(candidate, profile); err != nil {
				return in, 0, err
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

func (s *Service) ImpactPreview(ctx context.Context, orgID string, req RuntimeImpactRequest) (RuntimeImpactPreview, error) {
	cat, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return RuntimeImpactPreview{}, err
	}
	req.EntityType = strings.TrimSpace(req.EntityType)
	req.EntityID = strings.TrimSpace(req.EntityID)
	req.Action = strings.TrimSpace(req.Action)
	if req.Action == "" {
		req.Action = "update"
	}
	req.Rollout = normalizeRollout(req.Rollout)
	coverage := []RuntimeCoverage{}
	if s.coverage != nil {
		if got, err := s.coverage.Coverage(ctx, cat); err == nil {
			coverage = normalizeCoverageScopes(got)
		}
	}
	return RuntimeImpactPreview{
		EntityType:               req.EntityType,
		EntityID:                 req.EntityID,
		Action:                   req.Action,
		ReferenceCounts:          catalogReferenceCounts(cat, req.EntityType, req.EntityID),
		BasicCapabilityCoverage:  coverage,
		ExecutionSchedulability:  []RuntimeCoverage{},
		SnapshotBackMutation:     false,
		HistoricalSnapshotPolicy: "historical runtime snapshots are append-only and never rewritten by catalog/profile/default changes",
		Rollout:                  req.Rollout,
		CalculatedAt:             s.now().UTC(),
	}, nil
}

func (s *Service) AuditLog(ctx context.Context, orgID string, limit int) ([]AuditEvent, error) {
	repo, ok := s.repo.(interface {
		ListAudit(context.Context, string, int) ([]AuditEvent, error)
	})
	if !ok {
		return []AuditEvent{}, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	return repo.ListAudit(ctx, orgID, limit)
}

func normalizeRollout(in RuntimeRolloutPlan) RuntimeRolloutPlan {
	if !in.Enabled {
		return RuntimeRolloutPlan{}
	}
	if in.Percent <= 0 {
		in.Percent = 10
	}
	if in.Percent > 100 {
		in.Percent = 100
	}
	return in
}

func catalogReferenceCounts(cat Catalog, entityType, entityID string) []RuntimeReferenceCount {
	counts := []RuntimeReferenceCount{}
	add := func(source string, count int) {
		counts = append(counts, RuntimeReferenceCount{
			Source: source, EntityType: entityType, EntityID: entityID, Count: count, Mutable: true,
		})
	}
	switch entityType {
	case "profile":
		n := 0
		if cat.DefaultProfileID == entityID {
			n = 1
		}
		add(ReferenceSourceCatalogDefault, n)
	case "cli":
		n := 0
		key := catalogCLIKey(cat, entityID)
		for _, p := range cat.Profiles {
			if p.CLIKey == key {
				n++
			}
		}
		add(ReferenceSourceProfile, n)
	case "model":
		n := 0
		key := catalogModelKey(cat, entityID)
		for _, p := range cat.Profiles {
			if p.ModelKey == key {
				n++
			}
		}
		add(ReferenceSourceProfile, n)
	}
	return counts
}

func catalogCLIKey(cat Catalog, id string) string {
	for _, cli := range cat.CLIs {
		if cli.ID == id || cli.Key == id {
			return cli.Key
		}
	}
	return id
}

func catalogModelKey(cat Catalog, id string) string {
	for _, model := range cat.Models {
		if model.ID == id || model.Key == id {
			return model.Key
		}
	}
	return id
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
	if !cli.Enabled {
		return &Error{Reason: ReasonProfileDisabled, Message: "profile CLI must be enabled", Details: map[string]any{"cli_key": p.CLIKey}}
	}
	if model == nil {
		return &Error{Reason: ReasonModelNotFound, Message: "model not found", Details: map[string]any{"model_key": p.ModelKey}}
	}
	if !model.Enabled {
		return &Error{Reason: ReasonProfileDisabled, Message: "profile model must be enabled", Details: map[string]any{"model_key": p.ModelKey}}
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
	var document any
	if err := json.Unmarshal(raw, &document); err != nil {
		return parameterError("", err.Error())
	}
	schema, err := compileSchema(document)
	if err != nil {
		return parameterError("", err.Error())
	}
	if err := schema.Validate(params); err != nil {
		return parameterError("", err.Error())
	}
	return nil
}

func parameterError(field, msg string) error {
	return &Error{Reason: ReasonParametersInvalid, Message: "runtime parameters are invalid", Details: map[string]any{"field": field, "error": msg}}
}

func (s *Service) audit(org, actor, entityType, entityKey, action string, before, after any) AuditEvent {
	b, _ := json.Marshal(before)
	a, _ := json.Marshal(after)
	return AuditEvent{ID: s.id(), OrgID: org, Actor: actor, EntityType: entityType, EntityKey: entityKey, Action: action, Before: b, After: a, OccurredAt: s.now()}
}
