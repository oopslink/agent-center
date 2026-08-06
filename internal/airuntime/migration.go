package airuntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	ReasonMigrationUnavailable Reason = "runtime_migration_unavailable"
	ReasonMigrationPlanChanged Reason = "runtime_migration_plan_changed"
)

type MigrationObject struct {
	OrgID      string         `json:"org_id,omitempty"`
	ObjectType string         `json:"object_type"`
	ObjectID   string         `json:"object_id"`
	Legacy     LegacyRuntime  `json:"legacy"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type ObjectSelection struct {
	OrgID           string           `json:"org_id,omitempty"`
	ObjectType      string           `json:"object_type"`
	ObjectID        string           `json:"object_id"`
	Selection       RuntimeSelection `json:"selection"`
	SelectionSource string           `json:"selection_source"`
	ContentHash     string           `json:"content_hash,omitempty"`
	MigratedAt      time.Time        `json:"migrated_at"`
	UpdatedAt       time.Time        `json:"updated_at"`
}

type MigrationMapping struct {
	ObjectType      string           `json:"object_type"`
	ObjectID        string           `json:"object_id"`
	ProfileID       string           `json:"profile_id,omitempty"`
	ProfileKey      string           `json:"profile_key,omitempty"`
	ContentHash     string           `json:"content_hash"`
	CLIKey          string           `json:"cli_key"`
	ModelKey        string           `json:"model_key"`
	Parameters      map[string]any   `json:"parameters,omitempty"`
	Selection       RuntimeSelection `json:"selection"`
	SelectionSource string           `json:"selection_source"`
}

type MigrationProfilePlan struct {
	ProfileKey  string             `json:"profile_key"`
	Name        string             `json:"name"`
	ContentHash string             `json:"content_hash"`
	CLIKey      string             `json:"cli_key"`
	ModelKey    string             `json:"model_key"`
	Parameters  map[string]any     `json:"parameters,omitempty"`
	ObjectIDs   []string           `json:"object_ids"`
	Objects     []MigrationMapping `json:"objects"`
}

type MigrationUnmapped struct {
	ObjectType string         `json:"object_type"`
	ObjectID   string         `json:"object_id"`
	Reason     Reason         `json:"reason"`
	Message    string         `json:"message"`
	Details    map[string]any `json:"details"`
	Original   LegacyRuntime  `json:"original"`
}

type MigrationSummary struct {
	Objects                 int `json:"objects"`
	ExactMappings           int `json:"exact_mappings"`
	DeduplicatedProfiles    int `json:"deduplicated_profiles"`
	DeduplicatedObjects     int `json:"deduplicated_objects"`
	ObjectOverrides         int `json:"object_overrides"`
	Unmapped                int `json:"unmapped"`
	ProfilesToCreate        int `json:"profiles_to_create"`
	ObjectSelectionsToWrite int `json:"object_selections_to_write"`
}

type MigrationReport struct {
	DryRun               bool                   `json:"dry_run"`
	Applied              bool                   `json:"applied"`
	Revision             int64                  `json:"revision"`
	PlanSHA256           string                 `json:"plan_sha256"`
	ExactMappings        []MigrationMapping     `json:"exact_mappings"`
	DeduplicatedProfiles []MigrationProfilePlan `json:"deduplicated_profiles"`
	ObjectOverrides      []MigrationMapping     `json:"object_overrides"`
	Unmapped             []MigrationUnmapped    `json:"unmapped"`
	Summary              MigrationSummary       `json:"summary"`
}

type ApplyMigrationRequest struct {
	ExpectedRevision int64  `json:"expected_revision"`
	PlanSHA256       string `json:"plan_sha256,omitempty"`
}

type MigrationMutation struct {
	OrgID             string
	Profiles          []RuntimeProfile
	ObjectSelections  []ObjectSelection
	PlannedReportHash string
	AppliedAt         time.Time
}

type migrationCandidate struct {
	object     MigrationObject
	cli        CLIDefinition
	model      ModelDefinition
	parameters map[string]any
	hash       string
}

type runtimeContent struct {
	CLIKey     string         `json:"cli_key"`
	ModelKey   string         `json:"model_key"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

func (s *Service) LegacyMigrationDryRun(ctx context.Context, orgID string) (MigrationReport, error) {
	repo, ok := s.repo.(MigrationRepository)
	if !ok {
		return MigrationReport{}, runtimeError(ReasonMigrationUnavailable, "AI Runtime migration repository is not configured", nil)
	}
	catalog, err := s.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return MigrationReport{}, err
	}
	objects, err := repo.ListLegacyRuntimeObjects(ctx, orgID)
	if err != nil {
		return MigrationReport{}, err
	}
	report := planLegacyMigration(catalog, objects)
	report.DryRun = true
	report.Revision = catalog.Revision
	report.PlanSHA256 = migrationReportDigest(report)
	return report, nil
}

func (s *Service) ApplyLegacyMigration(ctx context.Context, orgID, actor string, req ApplyMigrationRequest) (MigrationReport, error) {
	repo, ok := s.repo.(MigrationRepository)
	if !ok {
		return MigrationReport{}, runtimeError(ReasonMigrationUnavailable, "AI Runtime migration repository is not configured", nil)
	}
	report, err := s.LegacyMigrationDryRun(ctx, orgID)
	if err != nil {
		return report, err
	}
	if req.PlanSHA256 != "" && req.PlanSHA256 != report.PlanSHA256 {
		return report, &Error{
			Reason:  ReasonMigrationPlanChanged,
			Message: "AI Runtime migration plan changed after dry-run",
			Details: map[string]any{"expected_plan_sha256": req.PlanSHA256, "actual_plan_sha256": report.PlanSHA256},
		}
	}
	if req.ExpectedRevision != report.Revision {
		return report, &Error{
			Reason:  ReasonRevisionConflict,
			Message: "catalog revision changed after dry-run",
			Details: map[string]any{"expected_revision": req.ExpectedRevision, "actual_revision": report.Revision},
		}
	}
	mutation := s.migrationMutation(orgID, report)
	if len(mutation.Profiles) == 0 && len(mutation.ObjectSelections) == 0 {
		report.Applied = false
		return report, nil
	}
	audit := s.audit(orgID, actor, "migration", "legacy-runtime", "legacy_migration_applied", nil, report)
	rev, err := repo.ApplyLegacyMigration(ctx, mutation, req.ExpectedRevision, audit)
	if err != nil {
		return report, err
	}
	report.Applied = true
	report.DryRun = false
	report.Revision = rev
	return report, nil
}

func (s *Service) migrationMutation(orgID string, report MigrationReport) MigrationMutation {
	now := s.now()
	mutation := MigrationMutation{OrgID: orgID, PlannedReportHash: report.PlanSHA256, AppliedAt: now}
	for _, p := range report.DeduplicatedProfiles {
		mutation.Profiles = append(mutation.Profiles, RuntimeProfile{
			ID:          s.id(),
			OrgID:       orgID,
			Key:         p.ProfileKey,
			Name:        p.Name,
			Description: "Created by AI Runtime legacy migration content-hash dedupe.",
			CLIKey:      p.CLIKey,
			ModelKey:    p.ModelKey,
			Parameters:  cloneMap(p.Parameters),
			Enabled:     true,
			CreatedAt:   now,
			UpdatedAt:   now,
		})
		for _, obj := range p.Objects {
			mutation.ObjectSelections = append(mutation.ObjectSelections, selectionForMapping(orgID, obj, now))
		}
	}
	for _, obj := range report.ExactMappings {
		mutation.ObjectSelections = append(mutation.ObjectSelections, selectionForMapping(orgID, obj, now))
	}
	for _, obj := range report.ObjectOverrides {
		mutation.ObjectSelections = append(mutation.ObjectSelections, selectionForMapping(orgID, obj, now))
	}
	sort.Slice(mutation.Profiles, func(i, j int) bool { return mutation.Profiles[i].Key < mutation.Profiles[j].Key })
	sort.Slice(mutation.ObjectSelections, func(i, j int) bool {
		if mutation.ObjectSelections[i].ObjectType != mutation.ObjectSelections[j].ObjectType {
			return mutation.ObjectSelections[i].ObjectType < mutation.ObjectSelections[j].ObjectType
		}
		return mutation.ObjectSelections[i].ObjectID < mutation.ObjectSelections[j].ObjectID
	})
	return mutation
}

func selectionForMapping(orgID string, m MigrationMapping, now time.Time) ObjectSelection {
	return ObjectSelection{
		OrgID: orgID, ObjectType: m.ObjectType, ObjectID: m.ObjectID,
		Selection: m.Selection, SelectionSource: m.SelectionSource, ContentHash: m.ContentHash,
		MigratedAt: now, UpdatedAt: now,
	}
}

func planLegacyMigration(catalog Catalog, objects []MigrationObject) MigrationReport {
	report := MigrationReport{
		DryRun:               true,
		ExactMappings:        []MigrationMapping{},
		DeduplicatedProfiles: []MigrationProfilePlan{},
		ObjectOverrides:      []MigrationMapping{},
		Unmapped:             []MigrationUnmapped{},
	}
	profilesByHash := map[string]RuntimeProfile{}
	for _, p := range catalog.Profiles {
		if !p.Enabled {
			continue
		}
		hash := contentHash(runtimeContent{CLIKey: p.CLIKey, ModelKey: p.ModelKey, Parameters: cloneMap(p.Parameters)})
		if _, exists := profilesByHash[hash]; !exists {
			profilesByHash[hash] = p
		}
	}
	pending := map[string][]migrationCandidate{}
	for _, object := range objects {
		object.ObjectType = strings.TrimSpace(object.ObjectType)
		object.ObjectID = strings.TrimSpace(object.ObjectID)
		if object.ObjectType == "" || object.ObjectID == "" {
			continue
		}
		candidate, unmapped := migrationCandidateFor(catalog, object)
		if unmapped != nil {
			report.Unmapped = append(report.Unmapped, *unmapped)
			continue
		}
		if profile, ok := profilesByHash[candidate.hash]; ok {
			report.ExactMappings = append(report.ExactMappings, MigrationMapping{
				ObjectType: object.ObjectType, ObjectID: object.ObjectID,
				ProfileID: profile.ID, ProfileKey: profile.Key, ContentHash: candidate.hash,
				CLIKey: candidate.cli.Key, ModelKey: candidate.model.Key, Parameters: cloneMap(candidate.parameters),
				Selection:       RuntimeSelection{Mode: SelectionProfile, ProfileID: profile.Key},
				SelectionSource: "exact_profile",
			})
			continue
		}
		pending[candidate.hash] = append(pending[candidate.hash], candidate)
	}
	for hash, group := range pending {
		sort.Slice(group, func(i, j int) bool { return group[i].object.ObjectID < group[j].object.ObjectID })
		if len(group) > 1 {
			first := group[0]
			key := migratedProfileKey(hash)
			plan := MigrationProfilePlan{
				ProfileKey:  key,
				Name:        migratedProfileName(first.cli.Key, first.model.ModelKey, hash),
				ContentHash: hash,
				CLIKey:      first.cli.Key,
				ModelKey:    first.model.Key,
				Parameters:  cloneMap(first.parameters),
				ObjectIDs:   []string{},
				Objects:     []MigrationMapping{},
			}
			for _, c := range group {
				m := MigrationMapping{
					ObjectType: c.object.ObjectType, ObjectID: c.object.ObjectID,
					ProfileKey: key, ContentHash: hash, CLIKey: c.cli.Key, ModelKey: c.model.Key,
					Parameters:      cloneMap(c.parameters),
					Selection:       RuntimeSelection{Mode: SelectionProfile, ProfileID: key},
					SelectionSource: "content_hash_profile",
				}
				plan.ObjectIDs = append(plan.ObjectIDs, c.object.ObjectID)
				plan.Objects = append(plan.Objects, m)
			}
			report.DeduplicatedProfiles = append(report.DeduplicatedProfiles, plan)
			continue
		}
		c := group[0]
		report.ObjectOverrides = append(report.ObjectOverrides, MigrationMapping{
			ObjectType: c.object.ObjectType, ObjectID: c.object.ObjectID,
			ContentHash: hash, CLIKey: c.cli.Key, ModelKey: c.model.Key, Parameters: cloneMap(c.parameters),
			Selection:       RuntimeSelection{Mode: SelectionOverride, CLIID: c.cli.Key, ModelID: c.model.Key, Parameters: cloneMap(c.parameters)},
			SelectionSource: "object_override",
		})
	}
	sortMigrationReport(&report)
	report.Summary = MigrationSummary{
		Objects:                 len(objects),
		ExactMappings:           len(report.ExactMappings),
		DeduplicatedProfiles:    len(report.DeduplicatedProfiles),
		ObjectOverrides:         len(report.ObjectOverrides),
		Unmapped:                len(report.Unmapped),
		ProfilesToCreate:        len(report.DeduplicatedProfiles),
		ObjectSelectionsToWrite: len(report.ExactMappings) + len(report.ObjectOverrides),
	}
	for _, p := range report.DeduplicatedProfiles {
		report.Summary.DeduplicatedObjects += len(p.Objects)
		report.Summary.ObjectSelectionsToWrite += len(p.Objects)
	}
	return report
}

func migrationCandidateFor(catalog Catalog, object MigrationObject) (migrationCandidate, *MigrationUnmapped) {
	legacy := LegacyRuntime{CLI: strings.TrimSpace(object.Legacy.CLI), Model: strings.TrimSpace(object.Legacy.Model)}
	if legacy.CLI == "" && legacy.Model == "" {
		return migrationCandidate{}, &MigrationUnmapped{
			ObjectType: object.ObjectType, ObjectID: object.ObjectID,
			Reason: ReasonDefaultMissing, Message: "legacy object has no explicit runtime to preserve",
			Details: map[string]any{"object_type": object.ObjectType, "object_id": object.ObjectID}, Original: legacy,
		}
	}
	cli := findLegacyCLI(catalog.CLIs, legacy.CLI)
	model := findLegacyModel(catalog.Models, legacy.Model)
	if cli == nil || model == nil {
		return migrationCandidate{}, &MigrationUnmapped{
			ObjectType: object.ObjectType, ObjectID: object.ObjectID,
			Reason: ReasonLegacyUnmapped, Message: "legacy runtime cannot be mapped exactly",
			Details:  map[string]any{"cli": legacy.CLI, "model": legacy.Model, "cli_found": cli != nil, "model_found": model != nil},
			Original: legacy,
		}
	}
	if !cli.Enabled {
		return migrationCandidate{}, &MigrationUnmapped{
			ObjectType: object.ObjectType, ObjectID: object.ObjectID,
			Reason: ReasonCLIDisabled, Message: "legacy runtime maps to a disabled CLI",
			Details: map[string]any{"cli_key": cli.Key}, Original: legacy,
		}
	}
	if !model.Enabled {
		return migrationCandidate{}, &MigrationUnmapped{
			ObjectType: object.ObjectType, ObjectID: object.ObjectID,
			Reason: ReasonModelDisabled, Message: "legacy runtime maps to a disabled model",
			Details: map[string]any{"model_key": model.Key}, Original: legacy,
		}
	}
	if !containsString(model.CompatibleCLIKeys, cli.Key) {
		return migrationCandidate{}, &MigrationUnmapped{
			ObjectType: object.ObjectType, ObjectID: object.ObjectID,
			Reason: ReasonIncompatible, Message: "legacy model is not compatible with legacy CLI",
			Details: map[string]any{"cli_key": cli.Key, "model_key": model.Key}, Original: legacy,
		}
	}
	params := cloneMap(object.Parameters)
	if params == nil {
		params = map[string]any{}
	}
	if err := validateParameters(cli.ParameterSchema, mergedMigrationParams(*model, params)); err != nil {
		var runtimeErr *Error
		reason := ReasonParametersInvalid
		message := err.Error()
		details := map[string]any{"cli_key": cli.Key, "model_key": model.Key}
		if errors.As(err, &runtimeErr) {
			reason = runtimeErr.Reason
			message = runtimeErr.Message
			details = runtimeErr.Details
		}
		return migrationCandidate{}, &MigrationUnmapped{
			ObjectType: object.ObjectType, ObjectID: object.ObjectID,
			Reason: reason, Message: message, Details: details, Original: legacy,
		}
	}
	hash := contentHash(runtimeContent{CLIKey: cli.Key, ModelKey: model.Key, Parameters: params})
	return migrationCandidate{object: object, cli: *cli, model: *model, parameters: params, hash: hash}, nil
}

func resolveLegacyObject(ctx context.Context, resolver *RuntimeResolver, orgID string, object MigrationObject) (RuntimeSnapshot, error) {
	catalog, err := resolver.repo.GetCatalog(ctx, orgID)
	if err != nil {
		return RuntimeSnapshot{}, err
	}
	candidate, unmapped := migrationCandidateFor(catalog, object)
	if unmapped != nil {
		return RuntimeSnapshot{}, runtimeError(unmapped.Reason, unmapped.Message, unmapped.Details)
	}
	snapshot, err := resolver.ResolveCatalog(catalog, RuntimeSelection{
		Mode:       SelectionOverride,
		CLIID:      candidate.cli.Key,
		ModelID:    candidate.model.Key,
		Parameters: cloneMap(candidate.parameters),
	})
	if err == nil {
		snapshot.Source = "legacy"
	}
	return snapshot, err
}

func mergedMigrationParams(model ModelDefinition, params map[string]any) map[string]any {
	out := cloneMap(model.DefaultParameters)
	mergeMap(out, params)
	return out
}

func contentHash(content runtimeContent) string {
	data, _ := json.Marshal(content)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func migratedProfileKey(hash string) string {
	if len(hash) > 24 {
		hash = hash[:24]
	}
	return "migrated-shared-" + hash
}

func migratedProfileName(cliKey, modelKey, hash string) string {
	short := hash
	if len(short) > 8 {
		short = short[:8]
	}
	return fmt.Sprintf("Migrated shared %s %s %s", cliKey, modelKey, short)
}

func migrationReportDigest(report MigrationReport) string {
	copy := report
	copy.DryRun = true
	copy.Applied = false
	copy.Revision = 0
	copy.PlanSHA256 = ""
	data, _ := json.Marshal(copy)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sortMigrationReport(report *MigrationReport) {
	sort.Slice(report.ExactMappings, func(i, j int) bool {
		return mappingLess(report.ExactMappings[i], report.ExactMappings[j])
	})
	sort.Slice(report.ObjectOverrides, func(i, j int) bool {
		return mappingLess(report.ObjectOverrides[i], report.ObjectOverrides[j])
	})
	sort.Slice(report.Unmapped, func(i, j int) bool {
		if report.Unmapped[i].ObjectType != report.Unmapped[j].ObjectType {
			return report.Unmapped[i].ObjectType < report.Unmapped[j].ObjectType
		}
		return report.Unmapped[i].ObjectID < report.Unmapped[j].ObjectID
	})
	sort.Slice(report.DeduplicatedProfiles, func(i, j int) bool {
		return report.DeduplicatedProfiles[i].ProfileKey < report.DeduplicatedProfiles[j].ProfileKey
	})
	for i := range report.DeduplicatedProfiles {
		sort.Strings(report.DeduplicatedProfiles[i].ObjectIDs)
		sort.Slice(report.DeduplicatedProfiles[i].Objects, func(a, b int) bool {
			return mappingLess(report.DeduplicatedProfiles[i].Objects[a], report.DeduplicatedProfiles[i].Objects[b])
		})
	}
}

func mappingLess(a, b MigrationMapping) bool {
	if a.ObjectType != b.ObjectType {
		return a.ObjectType < b.ObjectType
	}
	return a.ObjectID < b.ObjectID
}
