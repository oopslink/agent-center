package api

import (
	"net/http"
	"strings"

	agentbc "github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/airuntime"
	"github.com/oopslink/agent-center/internal/team"
)

type runtimePair struct {
	CLI   string
	Model string
}

func (s *Server) validateAgentRuntimeConfig(
	w http.ResponseWriter,
	r *http.Request,
	d HandlerDeps,
	orgID string,
	cli string,
	model string,
	allowedModels []string,
	allowedExecutors []agentbc.ExecutorProfile,
) (string, string, []string, []agentbc.ExecutorProfile, bool) {
	if d.RuntimeCatalog == nil {
		return cli, model, allowedModels, allowedExecutors, true
	}
	catalog, err := d.RuntimeCatalog.Catalog(r.Context(), orgID)
	if err != nil {
		writeRuntimeError(w, err)
		return "", "", nil, nil, false
	}
	main, err := runtimePairFromCatalog(catalog, cli, model)
	if err != nil {
		writeRuntimeError(w, err)
		return "", "", nil, nil, false
	}
	executors := allowedExecutors
	if len(executors) == 0 && len(allowedModels) > 0 {
		executors = agentbc.ExecutorsFromModels(allowedModels, main.CLI)
	}
	canonicalExecutors := make([]agentbc.ExecutorProfile, 0, len(executors))
	seen := make(map[agentbc.ExecutorProfile]struct{}, len(executors))
	for _, executor := range executors {
		pair, pairErr := runtimePairFromCatalog(catalog, executor.CLI, executor.Model)
		if pairErr != nil {
			writeRuntimeError(w, pairErr)
			return "", "", nil, nil, false
		}
		next := agentbc.ExecutorProfile{CLI: pair.CLI, Model: pair.Model}
		if _, ok := seen[next]; ok {
			continue
		}
		seen[next] = struct{}{}
		canonicalExecutors = append(canonicalExecutors, next)
	}
	return main.CLI, main.Model, nil, canonicalExecutors, true
}

func (s *Server) validateTeamRuntimeRoles(
	w http.ResponseWriter,
	r *http.Request,
	d HandlerDeps,
	orgID string,
	roles []team.RoleConfig,
) ([]team.RoleConfig, bool) {
	if d.RuntimeCatalog == nil {
		return roles, true
	}
	catalog, err := d.RuntimeCatalog.Catalog(r.Context(), orgID)
	if err != nil {
		writeRuntimeError(w, err)
		return nil, false
	}
	out := make([]team.RoleConfig, 0, len(roles))
	for _, role := range roles {
		pair, pairErr := runtimePairFromCatalog(catalog, role.CLI, role.Model)
		if pairErr != nil {
			writeRuntimeError(w, pairErr)
			return nil, false
		}
		role.CLI = pair.CLI
		role.Model = pair.Model
		out = append(out, role)
	}
	return out, true
}

func (s *Server) validateTeamRuntimeSlots(
	w http.ResponseWriter,
	r *http.Request,
	d HandlerDeps,
	orgID string,
	slots []team.RoleSlot,
) ([]team.RoleSlot, bool) {
	if d.RuntimeCatalog == nil {
		return slots, true
	}
	catalog, err := d.RuntimeCatalog.Catalog(r.Context(), orgID)
	if err != nil {
		writeRuntimeError(w, err)
		return nil, false
	}
	out := make([]team.RoleSlot, 0, len(slots))
	for _, slot := range slots {
		pair, pairErr := runtimePairFromCatalog(catalog, slot.Config.CLI, slot.Config.Model)
		if pairErr != nil {
			writeRuntimeError(w, pairErr)
			return nil, false
		}
		slot.Config.CLI = pair.CLI
		slot.Config.Model = pair.Model
		out = append(out, slot)
	}
	return out, true
}

func (s *Server) validateRuntimeModelValue(
	w http.ResponseWriter,
	r *http.Request,
	d HandlerDeps,
	orgID string,
	model string,
) (string, bool) {
	model = strings.TrimSpace(model)
	if d.RuntimeCatalog == nil || model == "" {
		return model, true
	}
	catalog, err := d.RuntimeCatalog.Catalog(r.Context(), orgID)
	if err != nil {
		writeRuntimeError(w, err)
		return "", false
	}
	pair, err := runtimePairForModel(catalog, model)
	if err != nil {
		writeRuntimeError(w, err)
		return "", false
	}
	return pair.Model, true
}

func runtimePairFromCatalog(catalog airuntime.Catalog, cliValue, modelValue string) (runtimePair, error) {
	cliValue = strings.TrimSpace(cliValue)
	modelValue = strings.TrimSpace(modelValue)
	if cliValue == "" && modelValue == "" {
		return runtimeDefaultPair(catalog)
	}
	if modelValue == "" {
		return runtimePairForCLI(catalog, cliValue)
	}
	if cliValue == "" {
		return runtimePairForModel(catalog, modelValue)
	}
	return runtimeExplicitPair(catalog, cliValue, modelValue)
}

func runtimeDefaultPair(catalog airuntime.Catalog) (runtimePair, error) {
	if strings.TrimSpace(catalog.DefaultProfileID) == "" {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonDefaultMissing,
			Message: "organization has no default runtime profile",
			Details: map[string]any{"org_id": catalog.OrgID},
		}
	}
	var profile *airuntime.RuntimeProfile
	for i := range catalog.Profiles {
		if catalog.Profiles[i].ID == catalog.DefaultProfileID || catalog.Profiles[i].Key == catalog.DefaultProfileID {
			profile = &catalog.Profiles[i]
			break
		}
	}
	if profile == nil {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonDefaultMissing,
			Message: "default runtime profile was not found",
			Details: map[string]any{"profile_id": catalog.DefaultProfileID},
		}
	}
	if !profile.Enabled {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonProfileDisabled,
			Message: "default runtime profile is disabled",
			Details: map[string]any{"profile_id": profile.ID, "profile_key": profile.Key},
		}
	}
	return runtimeExplicitPair(catalog, profile.CLIKey, profile.ModelKey)
}

func runtimePairForCLI(catalog airuntime.Catalog, cliValue string) (runtimePair, error) {
	cli := runtimeFindCLI(catalog.CLIs, cliValue)
	if cli == nil {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonCLINotFound,
			Message: "runtime CLI was not found",
			Details: map[string]any{"cli": cliValue},
		}
	}
	if !cli.Enabled {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonCLIDisabled,
			Message: "runtime CLI is disabled",
			Details: map[string]any{"cli_key": cli.Key},
		}
	}
	if profile := runtimeDefaultProfile(catalog); profile != nil && profile.Enabled && profile.CLIKey == cli.Key {
		if pair, err := runtimeExplicitPair(catalog, cli.Key, profile.ModelKey); err == nil {
			return pair, nil
		}
	}
	for _, model := range catalog.Models {
		if model.Enabled && runtimeModelAllowsCLI(model, cli.Key) {
			return runtimePair{CLI: cli.Key, Model: model.ModelKey}, nil
		}
	}
	return runtimePair{}, &airuntime.Error{
		Reason:  airuntime.ReasonModelNotFound,
		Message: "runtime CLI has no enabled compatible model",
		Details: map[string]any{"cli_key": cli.Key},
	}
}

func runtimePairForModel(catalog airuntime.Catalog, modelValue string) (runtimePair, error) {
	model := runtimeFindModel(catalog.Models, modelValue)
	if model == nil {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonModelNotFound,
			Message: "runtime model was not found",
			Details: map[string]any{"model": modelValue},
		}
	}
	if !model.Enabled {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonModelDisabled,
			Message: "runtime model is disabled",
			Details: map[string]any{"model_key": model.Key},
		}
	}
	if profile := runtimeDefaultProfile(catalog); profile != nil && profile.Enabled {
		if profileModel := runtimeFindModel(catalog.Models, profile.ModelKey); profileModel != nil && profileModel.Key == model.Key {
			if pair, err := runtimeExplicitPair(catalog, profile.CLIKey, model.Key); err == nil {
				return pair, nil
			}
		}
	}
	for _, cliKey := range model.CompatibleCLIKeys {
		if cli := runtimeFindCLI(catalog.CLIs, cliKey); cli != nil && cli.Enabled {
			return runtimePair{CLI: cli.Key, Model: model.ModelKey}, nil
		}
	}
	return runtimePair{}, &airuntime.Error{
		Reason:  airuntime.ReasonCLINotFound,
		Message: "runtime model has no enabled compatible CLI",
		Details: map[string]any{"model_key": model.Key},
	}
}

func runtimeExplicitPair(catalog airuntime.Catalog, cliValue, modelValue string) (runtimePair, error) {
	cli := runtimeFindCLI(catalog.CLIs, cliValue)
	if cli == nil {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonCLINotFound,
			Message: "runtime CLI was not found",
			Details: map[string]any{"cli": cliValue},
		}
	}
	if !cli.Enabled {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonCLIDisabled,
			Message: "runtime CLI is disabled",
			Details: map[string]any{"cli_key": cli.Key},
		}
	}
	model := runtimeFindModel(catalog.Models, modelValue)
	if model == nil {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonModelNotFound,
			Message: "runtime model was not found",
			Details: map[string]any{"model": modelValue},
		}
	}
	if !model.Enabled {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonModelDisabled,
			Message: "runtime model is disabled",
			Details: map[string]any{"model_key": model.Key},
		}
	}
	if !runtimeModelAllowsCLI(*model, cli.Key) {
		return runtimePair{}, &airuntime.Error{
			Reason:  airuntime.ReasonIncompatible,
			Message: "runtime model is not compatible with CLI",
			Details: map[string]any{"cli_key": cli.Key, "model_key": model.Key},
		}
	}
	return runtimePair{CLI: cli.Key, Model: model.ModelKey}, nil
}

func runtimeDefaultProfile(catalog airuntime.Catalog) *airuntime.RuntimeProfile {
	if strings.TrimSpace(catalog.DefaultProfileID) == "" {
		return nil
	}
	for i := range catalog.Profiles {
		if catalog.Profiles[i].ID == catalog.DefaultProfileID || catalog.Profiles[i].Key == catalog.DefaultProfileID {
			return &catalog.Profiles[i]
		}
	}
	return nil
}

func runtimeFindCLI(items []airuntime.CLIDefinition, value string) *airuntime.CLIDefinition {
	value = strings.TrimSpace(value)
	for i := range items {
		if items[i].ID == value || items[i].Key == value || items[i].Executable == value {
			return &items[i]
		}
	}
	return nil
}

func runtimeFindModel(items []airuntime.ModelDefinition, value string) *airuntime.ModelDefinition {
	value = strings.TrimSpace(value)
	for i := range items {
		if items[i].ID == value || items[i].Key == value || items[i].ModelKey == value {
			return &items[i]
		}
	}
	return nil
}

func runtimeModelAllowsCLI(model airuntime.ModelDefinition, cliKey string) bool {
	for _, compatible := range model.CompatibleCLIKeys {
		if compatible == cliKey {
			return true
		}
	}
	return false
}
