package airuntime

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

const (
	ExportKind      = "agent-center-ai-runtime"
	ExportVersion   = 1
	RedactedValue   = "[REDACTED]"
	StrategyMerge   = "merge"
	StrategyCreate  = "create_only"
	StrategyReplace = "replace"
)

const (
	ReasonImportMalformed          Reason = "runtime_import_malformed"
	ReasonImportVersionUnsupported Reason = "runtime_import_version_unsupported"
	ReasonImportInvalid            Reason = "runtime_import_invalid"
	ReasonImportConflict           Reason = "runtime_import_conflict"
	ReasonImportTokenInvalid       Reason = "runtime_import_validation_token_invalid"
)

type ExportDocument struct {
	SchemaVersion int           `json:"schema_version" yaml:"schema_version"`
	Kind          string        `json:"kind" yaml:"kind"`
	ExportedAt    time.Time     `json:"exported_at" yaml:"exported_at"`
	Runtime       ExportCatalog `json:"runtime" yaml:"runtime"`
	Warnings      []string      `json:"warnings,omitempty" yaml:"warnings,omitempty"`
}

type ExportCatalog struct {
	DefaultProfileKey string          `json:"default_profile_key,omitempty" yaml:"default_profile_key,omitempty"`
	CLIs              []ExportCLI     `json:"clis" yaml:"clis"`
	Models            []ExportModel   `json:"models" yaml:"models"`
	Profiles          []ExportProfile `json:"profiles" yaml:"profiles"`
}

type ExportCLI struct {
	Key               string          `json:"key" yaml:"key"`
	DisplayName       string          `json:"display_name" yaml:"display_name"`
	Executable        string          `json:"executable" yaml:"executable"`
	VersionConstraint string          `json:"version_constraint,omitempty" yaml:"version_constraint,omitempty"`
	RequiredFeatures  []string        `json:"required_features" yaml:"required_features"`
	ParameterSchema   json.RawMessage `json:"parameter_schema" yaml:"parameter_schema"`
	Enabled           bool            `json:"enabled" yaml:"enabled"`
}

type ExportModel struct {
	Key               string         `json:"key" yaml:"key"`
	ModelKey          string         `json:"model_key" yaml:"model_key"`
	DisplayName       string         `json:"display_name" yaml:"display_name"`
	CompatibleCLIKeys []string       `json:"compatible_cli_keys" yaml:"compatible_cli_keys"`
	DefaultParameters map[string]any `json:"default_parameters" yaml:"default_parameters"`
	Enabled           bool           `json:"enabled" yaml:"enabled"`
	ContextWindow     int            `json:"context_window,omitempty" yaml:"context_window,omitempty"`
	InputCost         float64        `json:"input_cost_per_mtok,omitempty" yaml:"input_cost_per_mtok,omitempty"`
	OutputCost        float64        `json:"output_cost_per_mtok,omitempty" yaml:"output_cost_per_mtok,omitempty"`
	Tier              string         `json:"tier,omitempty" yaml:"tier,omitempty"`
}

type ExportProfile struct {
	Key         string         `json:"key" yaml:"key"`
	Name        string         `json:"name" yaml:"name"`
	Description string         `json:"description,omitempty" yaml:"description,omitempty"`
	CLIKey      string         `json:"cli_key" yaml:"cli_key"`
	ModelKey    string         `json:"model_key" yaml:"model_key"`
	Parameters  map[string]any `json:"parameters" yaml:"parameters"`
	Enabled     bool           `json:"enabled" yaml:"enabled"`
}

type ImportStrategy string

type ImportRequest struct {
	ExpectedRevision int64          `json:"expected_revision"`
	DryRun           bool           `json:"dry_run"`
	Strategy         ImportStrategy `json:"strategy"`
	Document         ExportDocument `json:"document"`
}

type ExportScope string

const (
	ExportScopeAll      ExportScope = "all"
	ExportScopeCLI      ExportScope = "cli"
	ExportScopeModel    ExportScope = "model"
	ExportScopeProfile  ExportScope = "profile"
	ExportScopeSelected ExportScope = "selected"
)

type ExportOptions struct {
	Scope               ExportScope
	CLIKeys             []string
	ModelKeys           []string
	ProfileKeys         []string
	IncludeDependencies bool
}

type PreviewRequest struct {
	Strategy ImportStrategy `json:"strategy" yaml:"strategy"`
	Document ExportDocument `json:"document" yaml:"document"`
	Warnings []Diagnostic   `json:"-" yaml:"-"`
}

type PreviewResponse struct {
	Report          ImportReport      `json:"report" yaml:"report"`
	Coverage        []RuntimeCoverage `json:"coverage" yaml:"coverage"`
	ValidationToken string            `json:"validation_token" yaml:"validation_token"`
	ExpiresAt       time.Time         `json:"expires_at" yaml:"expires_at"`
	DocumentSHA256  string            `json:"document_sha256" yaml:"document_sha256"`
}

type ApplyRequest struct {
	Strategy        ImportStrategy `json:"strategy" yaml:"strategy"`
	Document        ExportDocument `json:"document" yaml:"document"`
	ValidationToken string         `json:"validation_token" yaml:"validation_token"`
}

type validationClaims struct {
	OrgID          string         `json:"org_id"`
	DocumentDigest string         `json:"document_digest"`
	Strategy       ImportStrategy `json:"strategy"`
	Revision       int64          `json:"revision"`
	ExpiresAt      int64          `json:"expires_at"`
}

type Diagnostic struct {
	Code       Reason `json:"code"`
	Severity   string `json:"severity,omitempty"`
	Path       string `json:"path,omitempty"`
	EntityType string `json:"entity_type,omitempty"`
	Key        string `json:"key,omitempty"`
	Message    string `json:"message"`
}

const ReasonImportUnknownField Reason = "runtime_import_unknown_field"

type CoverageReason struct {
	Code    string `json:"code"`
	Count   int    `json:"count"`
	Message string `json:"message"`
}

type RuntimeCoverage struct {
	ProfileID           string           `json:"profile_id"`
	OnlineWorkerCount   int              `json:"online_worker_count"`
	EligibleWorkerCount int              `json:"eligible_worker_count"`
	Status              string           `json:"status"`
	Reasons             []CoverageReason `json:"reasons"`
	CalculatedAt        time.Time        `json:"calculated_at"`
}

type CoverageProvider interface {
	Coverage(context.Context, Catalog) ([]RuntimeCoverage, error)
}

type ImportItem struct {
	EntityType string `json:"entity_type"`
	Key        string `json:"key"`
	Action     string `json:"action"`
}

type ImportReport struct {
	DryRun      bool         `json:"dry_run"`
	Applied     bool         `json:"applied"`
	Revision    int64        `json:"revision"`
	Items       []ImportItem `json:"items"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

type BulkMutation struct {
	OrgID             string
	DefaultProfileKey string
	SetDefaultProfile bool
	CLIs              []CLIDefinition
	Models            []ModelDefinition
	Profiles          []RuntimeProfile
}

func (s *Service) Export(ctx context.Context, orgID string) (ExportDocument, error) {
	return s.ExportWithOptions(ctx, orgID, ExportOptions{Scope: ExportScopeAll, IncludeDependencies: true})
}

func (s *Service) ExportWithOptions(ctx context.Context, orgID string, opts ExportOptions) (ExportDocument, error) {
	cat, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return ExportDocument{}, err
	}
	doc := exportCatalog(cat)
	doc.ExportedAt = s.now()
	if opts.Scope == "" {
		opts.Scope = ExportScopeAll
	}
	if opts.Scope != ExportScopeAll {
		filtered, warnings, err := filterExport(doc.Runtime, opts)
		if err != nil {
			return ExportDocument{}, err
		}
		doc.Runtime, doc.Warnings = filtered, warnings
	}
	return doc, nil
}

func (s *Service) PreviewImport(ctx context.Context, orgID string, req PreviewRequest) (PreviewResponse, error) {
	strategy, err := normalizeImportStrategy(req.Strategy)
	if err != nil {
		return PreviewResponse{}, err
	}
	current, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return PreviewResponse{}, err
	}
	report, err := s.validateImport(orgID, current, req.Document, strategy)
	if err != nil {
		return PreviewResponse{Report: report}, err
	}
	report.Diagnostics = append(report.Diagnostics, req.Warnings...)
	mutation, _, _ := s.planImport(orgID, current, req.Document.Runtime, strategy)
	candidate := candidateCatalog(current, mutation)
	var coverage []RuntimeCoverage
	if s.coverage == nil {
		report.Diagnostics = append(report.Diagnostics, Diagnostic{
			Code: Reason("runtime_coverage_unavailable"), Severity: "warning",
			Path: "coverage", Message: "scheduler coverage data is unavailable",
		})
		coverage = []RuntimeCoverage{}
	} else {
		coverage, err = s.coverage.Coverage(ctx, candidate)
		if err != nil {
			report.Diagnostics = append(report.Diagnostics, Diagnostic{
				Code: Reason("runtime_coverage_unavailable"), Severity: "warning",
				Path: "coverage", Message: "scheduler coverage data is unavailable",
			})
			coverage = []RuntimeCoverage{}
		}
	}
	sort.Slice(coverage, func(i, j int) bool { return coverage[i].ProfileID < coverage[j].ProfileID })
	digest, err := documentDigest(req.Document)
	if err != nil {
		return PreviewResponse{}, err
	}
	expires := s.now().Add(s.importTokenLifetime)
	claims := validationClaims{
		OrgID: orgID, DocumentDigest: digest, Strategy: strategy,
		Revision: current.Revision, ExpiresAt: expires.Unix(),
	}
	token, err := s.signValidationClaims(claims)
	if err != nil {
		return PreviewResponse{}, err
	}
	return PreviewResponse{
		Report: report, Coverage: coverage, ValidationToken: token, ExpiresAt: expires, DocumentSHA256: digest,
	}, nil
}

func (s *Service) ApplyImport(ctx context.Context, orgID, actor string, req ApplyRequest) (ImportReport, error) {
	strategy, err := normalizeImportStrategy(req.Strategy)
	if err != nil {
		return ImportReport{}, err
	}
	claims, err := s.verifyValidationToken(req.ValidationToken)
	if err != nil {
		return ImportReport{}, err
	}
	digest, err := documentDigest(req.Document)
	if err != nil {
		return ImportReport{}, err
	}
	if claims.OrgID != orgID || claims.DocumentDigest != digest || claims.Strategy != strategy {
		return ImportReport{}, importError(ReasonImportConflict, "validation_token", "validation token does not match organization, document, or strategy")
	}
	current, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return ImportReport{}, err
	}
	if current.Revision != claims.Revision {
		return ImportReport{}, &Error{Reason: ReasonRevisionConflict, Message: "catalog revision changed after preview", Details: map[string]any{"expected_revision": claims.Revision, "actual_revision": current.Revision}}
	}
	report, err := s.validateImport(orgID, current, req.Document, strategy)
	if err != nil {
		return report, err
	}
	if len(report.Items) == 0 || allUnchanged(report.Items) {
		return report, nil
	}
	mutation, _, _ := s.planImport(orgID, current, req.Document.Runtime, strategy)
	assignImportIDs(&mutation, s.id)
	before := exportCatalog(current)
	after := candidateCatalog(current, mutation)
	audit := s.audit(orgID, actor, "catalog", orgID, "bulk_imported", before, exportCatalog(after))
	bulkRepo, ok := s.repo.(BulkRepository)
	if !ok {
		return report, errors.New("AI Runtime repository does not support bulk import")
	}
	rev, err := bulkRepo.ApplyBulkImport(ctx, mutation, claims.Revision, audit)
	if err != nil {
		return report, err
	}
	report.Applied, report.Revision = true, rev
	return report, nil
}

func (s *Service) validateImport(orgID string, current Catalog, doc ExportDocument, strategy ImportStrategy) (ImportReport, error) {
	report := ImportReport{DryRun: true, Revision: current.Revision, Items: []ImportItem{}, Diagnostics: []Diagnostic{}}
	if doc.Kind != ExportKind {
		return report, importError(ReasonImportMalformed, "kind", "document kind must be "+ExportKind)
	}
	if doc.SchemaVersion != ExportVersion {
		return report, importError(ReasonImportVersionUnsupported, "schema_version", fmt.Sprintf("unsupported document schema_version %d", doc.SchemaVersion))
	}
	_, report.Items, report.Diagnostics = s.planImport(orgID, current, doc.Runtime, strategy)
	if len(report.Diagnostics) != 0 {
		return report, &Error{Reason: report.Diagnostics[0].Code, Message: "AI Runtime import validation failed", Details: map[string]any{"diagnostics": report.Diagnostics}}
	}
	return report, nil
}

func (s *Service) planImport(orgID string, current Catalog, in ExportCatalog, strategy ImportStrategy) (BulkMutation, []ImportItem, []Diagnostic) {
	m := BulkMutation{
		OrgID:             orgID,
		DefaultProfileKey: strings.TrimSpace(in.DefaultProfileKey),
		SetDefaultProfile: strategy == StrategyReplace || (strategy != StrategyCreate && strings.TrimSpace(in.DefaultProfileKey) != ""),
	}
	items := make([]ImportItem, 0, len(in.CLIs)+len(in.Models)+len(in.Profiles))
	diags := make([]Diagnostic, 0)
	existingCLI, existingModel, existingProfile := cliByKey(current.CLIs), modelByKey(current.Models), profileByKey(current.Profiles)
	seenCLI, seenModel, seenProfile := map[string]bool{}, map[string]bool{}, map[string]bool{}

	for i, x := range in.CLIs {
		path := fmt.Sprintf("catalog.clis[%d]", i)
		x.Key, x.DisplayName, x.Executable = strings.TrimSpace(x.Key), strings.TrimSpace(x.DisplayName), strings.TrimSpace(x.Executable)
		if seenCLI[x.Key] {
			diags = append(diags, diagnostic(ReasonImportInvalid, path+".key", "cli", x.Key, "duplicate CLI key"))
			continue
		}
		seenCLI[x.Key] = true
		features, featureErr := normalizeStrings(x.RequiredFeatures)
		if featureErr == nil {
			x.RequiredFeatures = features
		}
		if len(x.ParameterSchema) == 0 {
			x.ParameterSchema = json.RawMessage(`{"type":"object","additionalProperties":false}`)
		}
		entity := CLIDefinition{OrgID: orgID, Key: x.Key, DisplayName: x.DisplayName, Executable: x.Executable, VersionConstraint: x.VersionConstraint, RequiredFeatures: x.RequiredFeatures, ParameterSchema: x.ParameterSchema, Enabled: x.Enabled}
		if old, conflict := existingCLI[x.Key]; conflict {
			if strategy == StrategyCreate {
				items = append(items, ImportItem{"cli", x.Key, "unchanged"})
				continue
			}
			entity.ID, entity.System, entity.CreatedAt = old.ID, old.System, old.CreatedAt
			if equalCLIImport(old, entity) {
				items = append(items, ImportItem{"cli", x.Key, "unchanged"})
				continue
			}
			items = append(items, ImportItem{"cli", x.Key, "update"})
		} else {
			items = append(items, ImportItem{"cli", x.Key, "create"})
		}
		entity.UpdatedAt = s.now()
		if entity.CreatedAt.IsZero() {
			entity.CreatedAt = entity.UpdatedAt
		}
		if err := validateCLIImport(entity); err != nil {
			diags = append(diags, diagnostic(ReasonImportInvalid, path, "cli", x.Key, err.Error()))
		}
		m.CLIs = append(m.CLIs, entity)
	}
	for i, x := range in.Models {
		path := fmt.Sprintf("catalog.models[%d]", i)
		x.Key, x.ModelKey, x.DisplayName = strings.TrimSpace(x.Key), strings.TrimSpace(x.ModelKey), strings.TrimSpace(x.DisplayName)
		if seenModel[x.Key] {
			diags = append(diags, diagnostic(ReasonImportInvalid, path+".key", "model", x.Key, "duplicate model key"))
			continue
		}
		seenModel[x.Key] = true
		if x.DisplayName == "" {
			x.DisplayName = x.ModelKey
		}
		keys, keysErr := normalizeStrings(x.CompatibleCLIKeys)
		if keysErr == nil {
			x.CompatibleCLIKeys = keys
		}
		if x.DefaultParameters == nil {
			x.DefaultParameters = map[string]any{}
		}
		entity := ModelDefinition{OrgID: orgID, Key: x.Key, ModelKey: x.ModelKey, DisplayName: x.DisplayName, CompatibleCLIKeys: x.CompatibleCLIKeys, DefaultParameters: x.DefaultParameters, Enabled: x.Enabled, ContextWindow: x.ContextWindow, InputCost: x.InputCost, OutputCost: x.OutputCost, Tier: x.Tier}
		if old, conflict := existingModel[x.Key]; conflict {
			if strategy == StrategyCreate {
				items = append(items, ImportItem{"model", x.Key, "unchanged"})
				continue
			}
			var secretDiags []Diagnostic
			entity.DefaultParameters, secretDiags = preserveRedactedMap(entity.DefaultParameters, old.DefaultParameters, path+".default_parameters", "model", x.Key)
			diags = append(diags, secretDiags...)
			entity.ID, entity.CreatedAt = old.ID, old.CreatedAt
			if equalModelImport(old, entity) {
				items = append(items, ImportItem{"model", x.Key, "unchanged"})
				continue
			}
			items = append(items, ImportItem{"model", x.Key, "update"})
		} else {
			items = append(items, ImportItem{"model", x.Key, "create"})
			diags = append(diags, rejectUnresolvedRedacted(entity.DefaultParameters, path+".default_parameters", "model", x.Key)...)
		}
		entity.UpdatedAt = s.now()
		if entity.CreatedAt.IsZero() {
			entity.CreatedAt = entity.UpdatedAt
		}
		if err := validateModelImport(entity); err != nil {
			diags = append(diags, diagnostic(ReasonImportInvalid, path, "model", x.Key, err.Error()))
		}
		m.Models = append(m.Models, entity)
	}
	for i, x := range in.Profiles {
		path := fmt.Sprintf("catalog.profiles[%d]", i)
		x.Key, x.Name, x.CLIKey, x.ModelKey = strings.TrimSpace(x.Key), strings.TrimSpace(x.Name), strings.TrimSpace(x.CLIKey), strings.TrimSpace(x.ModelKey)
		if seenProfile[x.Key] {
			diags = append(diags, diagnostic(ReasonImportInvalid, path+".key", "profile", x.Key, "duplicate profile key"))
			continue
		}
		seenProfile[x.Key] = true
		if x.Parameters == nil {
			x.Parameters = map[string]any{}
		}
		entity := RuntimeProfile{OrgID: orgID, Key: x.Key, Name: x.Name, Description: x.Description, CLIKey: x.CLIKey, ModelKey: x.ModelKey, Parameters: x.Parameters, Enabled: x.Enabled}
		if old, conflict := existingProfile[x.Key]; conflict {
			if strategy == StrategyCreate {
				items = append(items, ImportItem{"profile", x.Key, "unchanged"})
				continue
			}
			var secretDiags []Diagnostic
			entity.Parameters, secretDiags = preserveRedactedMap(entity.Parameters, old.Parameters, path+".parameters", "profile", x.Key)
			diags = append(diags, secretDiags...)
			entity.ID, entity.CreatedAt = old.ID, old.CreatedAt
			if equalProfileImport(old, entity) {
				items = append(items, ImportItem{"profile", x.Key, "unchanged"})
				continue
			}
			items = append(items, ImportItem{"profile", x.Key, "update"})
		} else {
			items = append(items, ImportItem{"profile", x.Key, "create"})
			diags = append(diags, rejectUnresolvedRedacted(entity.Parameters, path+".parameters", "profile", x.Key)...)
		}
		entity.UpdatedAt = s.now()
		if entity.CreatedAt.IsZero() {
			entity.CreatedAt = entity.UpdatedAt
		}
		if err := validateProfileImport(entity); err != nil {
			diags = append(diags, diagnostic(ReasonImportInvalid, path, "profile", x.Key, err.Error()))
		}
		m.Profiles = append(m.Profiles, entity)
	}

	if strategy == StrategyReplace {
		now := s.now()
		for _, x := range current.Profiles {
			if !seenProfile[x.Key] && x.Enabled {
				x.Enabled, x.UpdatedAt = false, now
				m.Profiles = append(m.Profiles, x)
				items = append(items, ImportItem{"profile", x.Key, "disable"})
			}
		}
		for _, x := range current.Models {
			if !seenModel[x.Key] && x.Enabled {
				x.Enabled, x.UpdatedAt = false, now
				m.Models = append(m.Models, x)
				items = append(items, ImportItem{"model", x.Key, "disable"})
			}
		}
		for _, x := range current.CLIs {
			if !seenCLI[x.Key] && x.Enabled {
				x.Enabled, x.UpdatedAt = false, now
				m.CLIs = append(m.CLIs, x)
				items = append(items, ImportItem{"cli", x.Key, "disable"})
			}
		}
	}

	candidate := candidateCatalog(current, m)
	for _, model := range candidate.Models {
		if err := validateModel(candidate, model); err != nil {
			diags = append(diags, diagnostic(ReasonImportInvalid, "catalog.models", "model", model.Key, err.Error()))
		}
	}
	for _, profile := range candidate.Profiles {
		if profile.Enabled {
			if err := validateProfile(candidate, profile); err != nil {
				diags = append(diags, diagnostic(ReasonImportInvalid, "catalog.profiles", "profile", profile.Key, err.Error()))
			}
		}
	}
	if m.SetDefaultProfile && m.DefaultProfileKey != "" {
		p, ok := profileByKey(candidate.Profiles)[m.DefaultProfileKey]
		if !ok || !p.Enabled {
			diags = append(diags, diagnostic(ReasonImportInvalid, "catalog.default_profile_key", "profile", m.DefaultProfileKey, "default profile must reference an enabled profile"))
		}
	}
	sortReport(items, diags)
	return m, items, dedupeDiagnostics(diags)
}

func exportCatalog(cat Catalog) ExportDocument {
	out := ExportDocument{Kind: ExportKind, SchemaVersion: ExportVersion, Runtime: ExportCatalog{CLIs: []ExportCLI{}, Models: []ExportModel{}, Profiles: []ExportProfile{}}}
	for _, p := range cat.Profiles {
		if p.ID == cat.DefaultProfileID {
			out.Runtime.DefaultProfileKey = p.Key
		}
	}
	for _, x := range cat.CLIs {
		out.Runtime.CLIs = append(out.Runtime.CLIs, ExportCLI{Key: x.Key, DisplayName: x.DisplayName, Executable: x.Executable, VersionConstraint: x.VersionConstraint, RequiredFeatures: append([]string(nil), x.RequiredFeatures...), ParameterSchema: append(json.RawMessage(nil), x.ParameterSchema...), Enabled: x.Enabled})
	}
	for _, x := range cat.Models {
		out.Runtime.Models = append(out.Runtime.Models, ExportModel{Key: x.Key, ModelKey: x.ModelKey, DisplayName: x.DisplayName, CompatibleCLIKeys: append([]string(nil), x.CompatibleCLIKeys...), DefaultParameters: redactMap(x.DefaultParameters), Enabled: x.Enabled, ContextWindow: x.ContextWindow, InputCost: x.InputCost, OutputCost: x.OutputCost, Tier: x.Tier})
	}
	for _, x := range cat.Profiles {
		out.Runtime.Profiles = append(out.Runtime.Profiles, ExportProfile{Key: x.Key, Name: x.Name, Description: x.Description, CLIKey: x.CLIKey, ModelKey: x.ModelKey, Parameters: redactMap(x.Parameters), Enabled: x.Enabled})
	}
	sort.Slice(out.Runtime.CLIs, func(i, j int) bool { return out.Runtime.CLIs[i].Key < out.Runtime.CLIs[j].Key })
	sort.Slice(out.Runtime.Models, func(i, j int) bool { return out.Runtime.Models[i].Key < out.Runtime.Models[j].Key })
	sort.Slice(out.Runtime.Profiles, func(i, j int) bool { return out.Runtime.Profiles[i].Key < out.Runtime.Profiles[j].Key })
	return out
}

func candidateCatalog(current Catalog, m BulkMutation) Catalog {
	out := current
	out.CLIs = mergeCLIs(current.CLIs, m.CLIs)
	out.Models = mergeModels(current.Models, m.Models)
	out.Profiles = mergeProfiles(current.Profiles, m.Profiles)
	if m.SetDefaultProfile {
		out.DefaultProfileID = ""
		if m.DefaultProfileKey != "" {
			out.DefaultProfileID = profileByKey(out.Profiles)[m.DefaultProfileKey].ID
		}
	}
	return out
}

func validateCLIImport(x CLIDefinition) error {
	if err := validateKey("key", x.Key); err != nil {
		return err
	}
	if x.DisplayName == "" || x.Executable == "" {
		return errors.New("display_name and executable are required")
	}
	features, err := normalizeStrings(x.RequiredFeatures)
	if err != nil {
		return err
	}
	x.RequiredFeatures = features
	return validateSchema(x.ParameterSchema)
}
func validateModelImport(x ModelDefinition) error {
	if err := validateKey("key", x.Key); err != nil {
		return err
	}
	if x.ModelKey == "" {
		return errors.New("model_key is required")
	}
	keys, err := normalizeStrings(x.CompatibleCLIKeys)
	if err != nil || len(keys) == 0 {
		return errors.New("at least one compatible_cli_key is required")
	}
	return nil
}
func validateProfileImport(x RuntimeProfile) error {
	if err := validateKey("key", x.Key); err != nil {
		return err
	}
	if x.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

func importError(reason Reason, path, message string) error {
	d := diagnostic(reason, path, "", "", message)
	return &Error{Reason: reason, Message: message, Details: map[string]any{"diagnostics": []Diagnostic{d}}}
}
func diagnostic(code Reason, path, typ, key, message string) Diagnostic {
	return Diagnostic{Code: code, Path: path, EntityType: typ, Key: key, Message: message}
}
func allUnchanged(items []ImportItem) bool {
	for _, item := range items {
		if item.Action != "unchanged" {
			return false
		}
	}
	return true
}

func equalCLIImport(a, b CLIDefinition) bool {
	return a.DisplayName == b.DisplayName && a.Executable == b.Executable &&
		a.VersionConstraint == b.VersionConstraint && reflect.DeepEqual(a.RequiredFeatures, b.RequiredFeatures) &&
		reflect.DeepEqual(a.ParameterSchema, b.ParameterSchema) && a.Enabled == b.Enabled
}

func equalModelImport(a, b ModelDefinition) bool {
	return a.ModelKey == b.ModelKey && a.DisplayName == b.DisplayName &&
		reflect.DeepEqual(a.CompatibleCLIKeys, b.CompatibleCLIKeys) &&
		reflect.DeepEqual(a.DefaultParameters, b.DefaultParameters) && a.Enabled == b.Enabled &&
		a.ContextWindow == b.ContextWindow && a.InputCost == b.InputCost &&
		a.OutputCost == b.OutputCost && a.Tier == b.Tier
}

func equalProfileImport(a, b RuntimeProfile) bool {
	return a.Name == b.Name && a.Description == b.Description && a.CLIKey == b.CLIKey &&
		a.ModelKey == b.ModelKey && reflect.DeepEqual(a.Parameters, b.Parameters) && a.Enabled == b.Enabled
}

func preserveRedactedMap(in, existing map[string]any, path, entityType, key string) (map[string]any, []Diagnostic) {
	out := make(map[string]any, len(in))
	var diags []Diagnostic
	for name, value := range in {
		old, exists := existing[name]
		if sensitiveKey(name) && value == RedactedValue {
			if !exists || old == nil || old == RedactedValue {
				diags = append(diags, diagnostic(ReasonImportInvalid, path+"."+name, entityType, key, "redacted sensitive value has no existing value to preserve"))
				continue
			}
			out[name] = old
			continue
		}
		out[name], diags = preserveRedactedValue(value, old, path+"."+name, entityType, key, diags)
	}
	return out, diags
}

func preserveRedactedValue(value, existing any, path, entityType, key string, diags []Diagnostic) (any, []Diagnostic) {
	switch nested := value.(type) {
	case map[string]any:
		oldMap, _ := existing.(map[string]any)
		out, nestedDiags := preserveRedactedMap(nested, oldMap, path, entityType, key)
		return out, append(diags, nestedDiags...)
	case []any:
		oldSlice, _ := existing.([]any)
		out := make([]any, len(nested))
		for i, item := range nested {
			var old any
			if i < len(oldSlice) {
				old = oldSlice[i]
			}
			out[i], diags = preserveRedactedValue(item, old, fmt.Sprintf("%s[%d]", path, i), entityType, key, diags)
		}
		return out, diags
	default:
		if value == RedactedValue {
			if existing == nil || existing == RedactedValue {
				return nil, append(diags, diagnostic(ReasonImportInvalid, path, entityType, key, "redacted value has no existing value to preserve"))
			}
			return existing, diags
		}
		return value, diags
	}
}

func rejectUnresolvedRedacted(in map[string]any, path, entityType, key string) []Diagnostic {
	_, diags := preserveRedactedMap(in, nil, path, entityType, key)
	return diags
}
func assignImportIDs(m *BulkMutation, id IDGenerator) {
	for i := range m.CLIs {
		if m.CLIs[i].ID == "" {
			m.CLIs[i].ID = id()
		}
	}
	for i := range m.Models {
		if m.Models[i].ID == "" {
			m.Models[i].ID = id()
		}
	}
	for i := range m.Profiles {
		if m.Profiles[i].ID == "" {
			m.Profiles[i].ID = id()
		}
	}
}
func sortReport(items []ImportItem, diagnostics []Diagnostic) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].EntityType != items[j].EntityType {
			return items[i].EntityType < items[j].EntityType
		}
		return items[i].Key < items[j].Key
	})
	sort.Slice(diagnostics, func(i, j int) bool {
		if diagnostics[i].Path != diagnostics[j].Path {
			return diagnostics[i].Path < diagnostics[j].Path
		}
		if diagnostics[i].EntityType != diagnostics[j].EntityType {
			return diagnostics[i].EntityType < diagnostics[j].EntityType
		}
		if diagnostics[i].Key != diagnostics[j].Key {
			return diagnostics[i].Key < diagnostics[j].Key
		}
		return diagnostics[i].Message < diagnostics[j].Message
	})
}
func dedupeDiagnostics(in []Diagnostic) []Diagnostic {
	out := make([]Diagnostic, 0, len(in))
	for _, d := range in {
		if len(out) == 0 || out[len(out)-1] != d {
			out = append(out, d)
		}
	}
	return out
}
func cliByKey(xs []CLIDefinition) map[string]CLIDefinition {
	out := map[string]CLIDefinition{}
	for _, x := range xs {
		out[x.Key] = x
	}
	return out
}
func modelByKey(xs []ModelDefinition) map[string]ModelDefinition {
	out := map[string]ModelDefinition{}
	for _, x := range xs {
		out[x.Key] = x
	}
	return out
}
func profileByKey(xs []RuntimeProfile) map[string]RuntimeProfile {
	out := map[string]RuntimeProfile{}
	for _, x := range xs {
		out[x.Key] = x
	}
	return out
}
func mergeCLIs(base, changes []CLIDefinition) []CLIDefinition {
	m := cliByKey(base)
	for _, x := range changes {
		m[x.Key] = x
	}
	out := make([]CLIDefinition, 0, len(m))
	for _, x := range m {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
func mergeModels(base, changes []ModelDefinition) []ModelDefinition {
	m := modelByKey(base)
	for _, x := range changes {
		m[x.Key] = x
	}
	out := make([]ModelDefinition, 0, len(m))
	for _, x := range m {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
func mergeProfiles(base, changes []RuntimeProfile) []RuntimeProfile {
	m := profileByKey(base)
	for _, x := range changes {
		m[x.Key] = x
	}
	out := make([]RuntimeProfile, 0, len(m))
	for _, x := range m {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}

func redactMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		if sensitiveKey(k) {
			out[k] = RedactedValue
		} else {
			out[k] = redactValue(v)
		}
	}
	return out
}
func redactValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		return redactMap(x)
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = redactValue(item)
		}
		return out
	default:
		return v
	}
}
func sensitiveKey(k string) bool {
	k = strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(k, "-", "_"), ".", "_"))
	for _, token := range []string{"secret", "password", "passwd", "token", "api_key", "apikey", "credential", "private_key", "access_key"} {
		if k == token || strings.HasSuffix(k, "_"+token) || strings.HasPrefix(k, token+"_") {
			return true
		}
	}
	return false
}

func normalizeImportStrategy(strategy ImportStrategy) (ImportStrategy, error) {
	if strategy == "" {
		strategy = StrategyMerge
	}
	switch strategy {
	case StrategyMerge, StrategyCreate, StrategyReplace:
		return strategy, nil
	default:
		return "", importError(ReasonImportMalformed, "strategy", "strategy must be merge, create_only, or replace")
	}
}

func documentDigest(doc ExportDocument) (string, error) {
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return fmt.Sprintf("%x", sum[:]), nil
}

func (s *Service) signValidationClaims(claims validationClaims) (string, error) {
	raw, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, s.importTokenKey)
	_, _ = mac.Write([]byte(payload))
	signature := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return payload + "." + signature, nil
}

func (s *Service) verifyValidationToken(token string) (validationClaims, error) {
	var claims validationClaims
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return claims, importError(ReasonImportTokenInvalid, "validation_token", "validation token is malformed")
	}
	mac := hmac.New(sha256.New, s.importTokenKey)
	_, _ = mac.Write([]byte(parts[0]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || !hmac.Equal(signature, mac.Sum(nil)) {
		return claims, importError(ReasonImportTokenInvalid, "validation_token", "validation token signature is invalid")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil || json.Unmarshal(raw, &claims) != nil {
		return claims, importError(ReasonImportTokenInvalid, "validation_token", "validation token payload is invalid")
	}
	if claims.ExpiresAt <= s.now().Unix() {
		return claims, importError(ReasonImportTokenInvalid, "validation_token", "validation token has expired")
	}
	if claims.OrgID == "" || claims.DocumentDigest == "" || claims.Strategy == "" {
		return claims, importError(ReasonImportTokenInvalid, "validation_token", "validation token claims are incomplete")
	}
	return claims, nil
}

func filterExport(in ExportCatalog, opts ExportOptions) (ExportCatalog, []string, error) {
	if opts.Scope != ExportScopeCLI && opts.Scope != ExportScopeModel && opts.Scope != ExportScopeProfile && opts.Scope != ExportScopeSelected {
		return ExportCatalog{}, nil, importError(ReasonImportMalformed, "scope", "scope must be all, cli, model, profile, or selected")
	}
	cliWanted, modelWanted, profileWanted := map[string]bool{}, map[string]bool{}, map[string]bool{}
	add := func(dst map[string]bool, keys []string) {
		for _, key := range keys {
			if key = strings.TrimSpace(key); key != "" {
				dst[key] = true
			}
		}
	}
	switch opts.Scope {
	case ExportScopeCLI:
		add(cliWanted, opts.CLIKeys)
		if len(cliWanted) == 0 {
			for _, item := range in.CLIs {
				cliWanted[item.Key] = true
			}
		}
	case ExportScopeModel:
		add(modelWanted, opts.ModelKeys)
		if len(modelWanted) == 0 {
			for _, item := range in.Models {
				modelWanted[item.Key] = true
			}
		}
	case ExportScopeProfile:
		add(profileWanted, opts.ProfileKeys)
		if len(profileWanted) == 0 {
			for _, item := range in.Profiles {
				profileWanted[item.Key] = true
			}
		}
	case ExportScopeSelected:
		add(cliWanted, opts.CLIKeys)
		add(modelWanted, opts.ModelKeys)
		add(profileWanted, opts.ProfileKeys)
		if len(cliWanted)+len(modelWanted)+len(profileWanted) == 0 {
			return ExportCatalog{}, nil, importError(ReasonImportMalformed, "scope", "selected scope requires at least one key")
		}
	}

	if opts.IncludeDependencies {
		for _, profile := range in.Profiles {
			if profileWanted[profile.Key] {
				cliWanted[profile.CLIKey] = true
				modelWanted[profile.ModelKey] = true
			}
		}
		for _, model := range in.Models {
			if modelWanted[model.Key] {
				for _, cliKey := range model.CompatibleCLIKeys {
					cliWanted[cliKey] = true
				}
			}
		}
	}

	out := ExportCatalog{CLIs: []ExportCLI{}, Models: []ExportModel{}, Profiles: []ExportProfile{}}
	for _, item := range in.CLIs {
		if cliWanted[item.Key] {
			out.CLIs = append(out.CLIs, item)
			delete(cliWanted, item.Key)
		}
	}
	for _, item := range in.Models {
		if modelWanted[item.Key] {
			out.Models = append(out.Models, item)
			delete(modelWanted, item.Key)
		}
	}
	for _, item := range in.Profiles {
		if profileWanted[item.Key] {
			out.Profiles = append(out.Profiles, item)
			delete(profileWanted, item.Key)
			if item.Key == in.DefaultProfileKey {
				out.DefaultProfileKey = item.Key
			}
		}
	}
	if len(cliWanted)+len(modelWanted)+len(profileWanted) != 0 {
		return ExportCatalog{}, nil, importError(ReasonImportInvalid, "scope", "one or more selected stable keys do not exist")
	}
	if opts.IncludeDependencies {
		return out, nil, nil
	}
	return out, []string{"partial bundle: dependencies were not included and the document may not be independently importable"}, nil
}
