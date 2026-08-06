package airuntime

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

type RuntimeResolver struct {
	repo Repository
	now  func() time.Time
}

func NewRuntimeResolver(repo Repository) *RuntimeResolver {
	return &RuntimeResolver{repo: repo, now: func() time.Time { return time.Now().UTC() }}
}

func (r *RuntimeResolver) Resolve(ctx context.Context, orgID string, selection RuntimeSelection) (RuntimeSnapshot, error) {
	catalog, err := r.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	return r.ResolveCatalog(catalog, selection)
}

func (r *RuntimeResolver) ResolveCatalog(catalog Catalog, selection RuntimeSelection) (RuntimeSnapshot, error) {
	mode := strings.TrimSpace(selection.Mode)
	if mode == "" {
		mode = SelectionInherit
	}
	var cliID, modelID, profileID, source string
	parameters := map[string]any{}
	switch mode {
	case SelectionInherit:
		if catalog.DefaultProfileID == "" {
			return RuntimeSnapshot{}, runtimeError(ReasonDefaultMissing, "organization runtime default is not configured", map[string]any{"org_id": catalog.OrgID})
		}
		profileID, source = catalog.DefaultProfileID, "org_default"
	case SelectionProfile:
		if selection.ProfileID == "" {
			return RuntimeSnapshot{}, runtimeError(ReasonSelectionInvalid, "profile selection requires profile_id", nil)
		}
		profileID, source = selection.ProfileID, "profile"
	case SelectionOverride:
		if selection.CLIID == "" || selection.ModelID == "" {
			return RuntimeSnapshot{}, runtimeError(ReasonSelectionInvalid, "override selection requires cli_id and model_id", nil)
		}
		cliID, modelID, source = selection.CLIID, selection.ModelID, "override"
		parameters = cloneMap(selection.Parameters)
	default:
		return RuntimeSnapshot{}, runtimeError(ReasonSelectionInvalid, "runtime selection mode is invalid", map[string]any{"mode": mode})
	}

	if profileID != "" {
		profile := findProfile(catalog.Profiles, profileID)
		if profile == nil {
			return RuntimeSnapshot{}, runtimeError(ReasonProfileNotFound, "runtime profile not found", map[string]any{"profile_id": profileID})
		}
		if !profile.Enabled {
			return RuntimeSnapshot{}, runtimeError(ReasonProfileDisabled, "runtime profile is disabled", map[string]any{"profile_id": profileID})
		}
		cliID, modelID = profile.CLIKey, profile.ModelKey
		parameters = cloneMap(profile.Parameters)
	}
	cli := findCLI(catalog.CLIs, cliID)
	if cli == nil {
		return RuntimeSnapshot{}, runtimeError(ReasonCLINotFound, "runtime CLI not found", map[string]any{"cli": cliID})
	}
	if !cli.Enabled {
		return RuntimeSnapshot{}, runtimeError(ReasonCLIDisabled, "runtime CLI is disabled", map[string]any{"cli_key": cli.Key})
	}
	model := findModel(catalog.Models, modelID)
	if model == nil {
		return RuntimeSnapshot{}, runtimeError(ReasonModelNotFound, "runtime model not found", map[string]any{"model": modelID})
	}
	if !model.Enabled {
		return RuntimeSnapshot{}, runtimeError(ReasonModelDisabled, "runtime model is disabled", map[string]any{"model_key": model.Key})
	}
	if !containsString(model.CompatibleCLIKeys, cli.Key) {
		return RuntimeSnapshot{}, runtimeError(ReasonIncompatible, "model is not compatible with CLI", map[string]any{"cli_key": cli.Key, "model_key": model.Key})
	}
	merged, err := schemaDefaults(cli.ParameterSchema)
	if err != nil {
		return RuntimeSnapshot{}, parameterError("", err.Error())
	}
	mergeMap(merged, model.DefaultParameters)
	mergeMap(merged, parameters)
	if err := validateParameters(cli.ParameterSchema, merged); err != nil {
		return RuntimeSnapshot{}, err
	}
	return RuntimeSnapshot{
		SchemaVersion: 1, CLIKey: cli.Key, CLIExecutable: cli.Executable,
		CLIVersionConstraint: cli.VersionConstraint, RequiredFeatures: append([]string(nil), cli.RequiredFeatures...),
		ModelKey: model.ModelKey, Parameters: cloneMap(merged), Source: source,
		ProfileID: profileID, ResolvedAt: r.now().UTC(),
	}, nil
}

func findCLI(items []CLIDefinition, id string) *CLIDefinition {
	for i := range items {
		if items[i].ID == id || items[i].Key == id {
			return &items[i]
		}
	}
	return nil
}
func findModel(items []ModelDefinition, id string) *ModelDefinition {
	for i := range items {
		if items[i].ID == id || items[i].Key == id || items[i].ModelKey == id {
			return &items[i]
		}
	}
	return nil
}
func findProfile(items []RuntimeProfile, id string) *RuntimeProfile {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}
func containsString(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
func mergeMap(dst, src map[string]any) {
	for k, v := range src {
		dst[k] = deepCopy(v)
	}
}
func cloneMap(src map[string]any) map[string]any {
	dst := map[string]any{}
	mergeMap(dst, src)
	return dst
}
func deepCopy(v any) any {
	data, _ := json.Marshal(v)
	var out any
	_ = json.Unmarshal(data, &out)
	return out
}
func schemaDefaults(raw json.RawMessage) (map[string]any, error) {
	out := map[string]any{}
	if len(raw) == 0 {
		return out, nil
	}
	var schema map[string]any
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, err
	}
	properties, _ := schema["properties"].(map[string]any)
	for key, value := range properties {
		property, _ := value.(map[string]any)
		if def, ok := property["default"]; ok {
			out[key] = deepCopy(def)
		}
	}
	return out, nil
}
func runtimeError(reason Reason, message string, details map[string]any) error {
	if details == nil {
		details = map[string]any{}
	}
	return &Error{Reason: reason, Message: message, Details: details}
}
