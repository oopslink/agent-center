package airuntime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	ExportKind        = "ai_runtime_catalog"
	ExportVersion     = 1
	RedactedValue     = "[REDACTED]"
	ConflictReject    = "reject"
	ConflictSkip      = "skip"
	ConflictOverwrite = "overwrite"
)

const (
	ReasonImportMalformed          Reason = "runtime_import_malformed"
	ReasonImportVersionUnsupported Reason = "runtime_import_version_unsupported"
	ReasonImportInvalid            Reason = "runtime_import_invalid"
	ReasonImportConflict           Reason = "runtime_import_conflict"
)

type ExportDocument struct {
	Kind           string        `json:"kind"`
	Version        int           `json:"version"`
	SourceRevision int64         `json:"source_revision"`
	Catalog        ExportCatalog `json:"catalog"`
}

type ExportCatalog struct {
	DefaultProfileKey string          `json:"default_profile_key,omitempty"`
	CLIs              []ExportCLI     `json:"clis"`
	Models            []ExportModel   `json:"models"`
	Profiles          []ExportProfile `json:"profiles"`
}

type ExportCLI struct {
	Key               string          `json:"key"`
	DisplayName       string          `json:"display_name"`
	Executable        string          `json:"executable"`
	VersionConstraint string          `json:"version_constraint,omitempty"`
	RequiredFeatures  []string        `json:"required_features"`
	ParameterSchema   json.RawMessage `json:"parameter_schema"`
	Enabled           bool            `json:"enabled"`
}

type ExportModel struct {
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
}

type ExportProfile struct {
	Key         string         `json:"key"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	CLIKey      string         `json:"cli_key"`
	ModelKey    string         `json:"model_key"`
	Parameters  map[string]any `json:"parameters"`
	Enabled     bool           `json:"enabled"`
}

type ConflictStrategy string

type ImportRequest struct {
	ExpectedRevision int64            `json:"expected_revision"`
	DryRun           bool             `json:"dry_run"`
	ConflictStrategy ConflictStrategy `json:"conflict_strategy"`
	Document         ExportDocument   `json:"document"`
}

type Diagnostic struct {
	Code       Reason `json:"code"`
	Path       string `json:"path,omitempty"`
	EntityType string `json:"entity_type,omitempty"`
	Key        string `json:"key,omitempty"`
	Message    string `json:"message"`
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
	CLIs              []CLIDefinition
	Models            []ModelDefinition
	Profiles          []RuntimeProfile
}

func (s *Service) Export(ctx context.Context, orgID string) (ExportDocument, error) {
	cat, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return ExportDocument{}, err
	}
	doc := exportCatalog(cat)
	return doc, nil
}

func (s *Service) Import(ctx context.Context, orgID, actor string, req ImportRequest) (ImportReport, error) {
	report := ImportReport{DryRun: req.DryRun, Revision: req.ExpectedRevision, Items: []ImportItem{}, Diagnostics: []Diagnostic{}}
	if req.Document.Kind != ExportKind {
		return report, importError(ReasonImportMalformed, "kind", "document kind must be "+ExportKind)
	}
	if req.Document.Version != ExportVersion {
		return report, importError(ReasonImportVersionUnsupported, "version", fmt.Sprintf("unsupported document version %d", req.Document.Version))
	}
	if req.ConflictStrategy != ConflictReject && req.ConflictStrategy != ConflictSkip && req.ConflictStrategy != ConflictOverwrite {
		return report, importError(ReasonImportMalformed, "conflict_strategy", "conflict_strategy must be reject, skip, or overwrite")
	}
	current, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return report, err
	}
	if current.Revision != req.ExpectedRevision {
		return report, &Error{Reason: ReasonRevisionConflict, Message: "catalog revision changed", Details: map[string]any{"expected_revision": req.ExpectedRevision, "actual_revision": current.Revision}}
	}

	mutation, items, diagnostics := s.planImport(orgID, current, req.Document.Catalog, req.ConflictStrategy)
	report.Items, report.Diagnostics = items, diagnostics
	if len(diagnostics) != 0 {
		return report, &Error{Reason: diagnostics[0].Code, Message: "AI Runtime import validation failed", Details: map[string]any{"diagnostics": diagnostics}}
	}
	if req.DryRun || len(items) == 0 || allSkipped(items) {
		return report, nil
	}

	assignImportIDs(&mutation, s.id)
	before := exportCatalog(current)
	after := candidateCatalog(current, mutation)
	audit := s.audit(orgID, actor, "catalog", orgID, "bulk_imported", before, exportCatalog(after))
	bulkRepo, ok := s.repo.(BulkRepository)
	if !ok {
		return report, errors.New("AI Runtime repository does not support bulk import")
	}
	rev, err := bulkRepo.ApplyBulkImport(ctx, mutation, req.ExpectedRevision, audit)
	if err != nil {
		return report, err
	}
	report.Applied, report.Revision = true, rev
	return report, nil
}

func (s *Service) planImport(orgID string, current Catalog, in ExportCatalog, strategy ConflictStrategy) (BulkMutation, []ImportItem, []Diagnostic) {
	m := BulkMutation{OrgID: orgID, DefaultProfileKey: strings.TrimSpace(in.DefaultProfileKey)}
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
			action := conflictAction(strategy)
			items = append(items, ImportItem{"cli", x.Key, action})
			if strategy == ConflictReject {
				diags = append(diags, diagnostic(ReasonImportConflict, path+".key", "cli", x.Key, "CLI key already exists"))
				continue
			}
			if strategy == ConflictSkip {
				continue
			}
			entity.ID, entity.System, entity.CreatedAt = old.ID, old.System, old.CreatedAt
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
			action := conflictAction(strategy)
			items = append(items, ImportItem{"model", x.Key, action})
			if strategy == ConflictReject {
				diags = append(diags, diagnostic(ReasonImportConflict, path+".key", "model", x.Key, "model key already exists"))
				continue
			}
			if strategy == ConflictSkip {
				continue
			}
			entity.ID, entity.CreatedAt = old.ID, old.CreatedAt
		} else {
			items = append(items, ImportItem{"model", x.Key, "create"})
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
			action := conflictAction(strategy)
			items = append(items, ImportItem{"profile", x.Key, action})
			if strategy == ConflictReject {
				diags = append(diags, diagnostic(ReasonImportConflict, path+".key", "profile", x.Key, "profile key already exists"))
				continue
			}
			if strategy == ConflictSkip {
				continue
			}
			entity.ID, entity.CreatedAt = old.ID, old.CreatedAt
		} else {
			items = append(items, ImportItem{"profile", x.Key, "create"})
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
	if m.DefaultProfileKey != "" {
		p, ok := profileByKey(candidate.Profiles)[m.DefaultProfileKey]
		if !ok || !p.Enabled {
			diags = append(diags, diagnostic(ReasonImportInvalid, "catalog.default_profile_key", "profile", m.DefaultProfileKey, "default profile must reference an enabled profile"))
		}
	}
	sortReport(items, diags)
	return m, items, dedupeDiagnostics(diags)
}

func exportCatalog(cat Catalog) ExportDocument {
	out := ExportDocument{Kind: ExportKind, Version: ExportVersion, SourceRevision: cat.Revision, Catalog: ExportCatalog{CLIs: []ExportCLI{}, Models: []ExportModel{}, Profiles: []ExportProfile{}}}
	for _, p := range cat.Profiles {
		if p.ID == cat.DefaultProfileID {
			out.Catalog.DefaultProfileKey = p.Key
		}
	}
	for _, x := range cat.CLIs {
		out.Catalog.CLIs = append(out.Catalog.CLIs, ExportCLI{Key: x.Key, DisplayName: x.DisplayName, Executable: x.Executable, VersionConstraint: x.VersionConstraint, RequiredFeatures: append([]string(nil), x.RequiredFeatures...), ParameterSchema: append(json.RawMessage(nil), x.ParameterSchema...), Enabled: x.Enabled})
	}
	for _, x := range cat.Models {
		out.Catalog.Models = append(out.Catalog.Models, ExportModel{Key: x.Key, ModelKey: x.ModelKey, DisplayName: x.DisplayName, CompatibleCLIKeys: append([]string(nil), x.CompatibleCLIKeys...), DefaultParameters: redactMap(x.DefaultParameters), Enabled: x.Enabled, ContextWindow: x.ContextWindow, InputCost: x.InputCost, OutputCost: x.OutputCost, Tier: x.Tier})
	}
	for _, x := range cat.Profiles {
		out.Catalog.Profiles = append(out.Catalog.Profiles, ExportProfile{Key: x.Key, Name: x.Name, Description: x.Description, CLIKey: x.CLIKey, ModelKey: x.ModelKey, Parameters: redactMap(x.Parameters), Enabled: x.Enabled})
	}
	sort.Slice(out.Catalog.CLIs, func(i, j int) bool { return out.Catalog.CLIs[i].Key < out.Catalog.CLIs[j].Key })
	sort.Slice(out.Catalog.Models, func(i, j int) bool { return out.Catalog.Models[i].Key < out.Catalog.Models[j].Key })
	sort.Slice(out.Catalog.Profiles, func(i, j int) bool { return out.Catalog.Profiles[i].Key < out.Catalog.Profiles[j].Key })
	return out
}

func candidateCatalog(current Catalog, m BulkMutation) Catalog {
	out := current
	out.CLIs = mergeCLIs(current.CLIs, m.CLIs)
	out.Models = mergeModels(current.Models, m.Models)
	out.Profiles = mergeProfiles(current.Profiles, m.Profiles)
	if m.DefaultProfileKey != "" {
		out.DefaultProfileID = profileByKey(out.Profiles)[m.DefaultProfileKey].ID
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
func conflictAction(s ConflictStrategy) string {
	if s == ConflictSkip {
		return "skip"
	}
	if s == ConflictOverwrite {
		return "overwrite"
	}
	return "reject"
}
func allSkipped(items []ImportItem) bool {
	for _, item := range items {
		if item.Action != "skip" {
			return false
		}
	}
	return true
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
