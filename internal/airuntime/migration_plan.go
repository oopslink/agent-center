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
	MigrationCategoryExactProfile          = "exact_profile"
	MigrationCategoryDeduplicated          = "deduplicated_profile"
	MigrationCategoryObjectOverride        = "object_override"
	MigrationCategoryUnmapped              = "unmapped"
	ReasonMigrationMissingCLI       Reason = "runtime_migration_missing_cli"
)

type ResolverCutoverStage string

const (
	ResolverStageLegacyRead          ResolverCutoverStage = "legacy_read"
	ResolverStageShadowCompare       ResolverCutoverStage = "shadow_compare"
	ResolverStageNewResolverCanary   ResolverCutoverStage = "new_resolver_canary"
	ResolverStageOrganizationDefault ResolverCutoverStage = "organization_default"
)

type MigrationObjectRef struct {
	ObjectType string `json:"object_type"`
	ObjectID   string `json:"object_id"`
}

type MigrationObject struct {
	OrgID      string         `json:"org_id"`
	ObjectType string         `json:"object_type"`
	ObjectID   string         `json:"object_id"`
	Legacy     LegacyRuntime  `json:"legacy"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type RuntimeContent struct {
	CLIKey     string         `json:"cli_key"`
	ModelKey   string         `json:"model_key"`
	Parameters map[string]any `json:"parameters"`
}

type ExactProfileMapping struct {
	ProfileID   string               `json:"profile_id"`
	ProfileKey  string               `json:"profile_key"`
	ContentHash string               `json:"content_hash"`
	Objects     []MigrationObjectRef `json:"objects"`
}

type ProposedDedupProfile struct {
	ProposedKey string               `json:"proposed_key"`
	Name        string               `json:"name"`
	CLIKey      string               `json:"cli_key"`
	ModelKey    string               `json:"model_key"`
	Parameters  map[string]any       `json:"parameters"`
	ContentHash string               `json:"content_hash"`
	Objects     []MigrationObjectRef `json:"objects"`
}

type ObjectOverrideMapping struct {
	Object      MigrationObjectRef `json:"object"`
	Selection   RuntimeSelection   `json:"selection"`
	ContentHash string             `json:"content_hash"`
	CLIKey      string             `json:"cli_key"`
	ModelKey    string             `json:"model_key"`
	Parameters  map[string]any     `json:"parameters"`
}

type UnmappedMigrationObject struct {
	Object   MigrationObjectRef `json:"object"`
	Reason   Reason             `json:"reason"`
	Message  string             `json:"message"`
	Details  map[string]any     `json:"details"`
	Original LegacyRuntime      `json:"original"`
}

type ShadowCompareEntry struct {
	Object      MigrationObjectRef `json:"object"`
	Result      string             `json:"result"`
	ContentHash string             `json:"content_hash,omitempty"`
	Reason      Reason             `json:"reason,omitempty"`
	Message     string             `json:"message,omitempty"`
}

type CutoverEvidence struct {
	Stage       ResolverCutoverStage `json:"stage"`
	ReadPath    string               `json:"read_path"`
	FeatureFlag string               `json:"feature_flag"`
	Rollback    string               `json:"rollback"`
	Audit       []string             `json:"audit"`
}

type MigrationDryRunReport struct {
	DryRun                  bool                      `json:"dry_run"`
	OrgID                   string                    `json:"org_id"`
	CatalogRevision         int64                     `json:"catalog_revision"`
	GeneratedAt             time.Time                 `json:"generated_at"`
	TotalObjects            int                       `json:"total_objects"`
	Counts                  map[string]int            `json:"counts"`
	ExactProfiles           []ExactProfileMapping     `json:"exact_profiles"`
	DeduplicatedProfiles    []ProposedDedupProfile    `json:"deduplicated_profiles"`
	ObjectOverrides         []ObjectOverrideMapping   `json:"object_overrides"`
	Unmapped                []UnmappedMigrationObject `json:"unmapped"`
	ShadowCompare           []ShadowCompareEntry      `json:"shadow_compare"`
	CutoverEvidence         []CutoverEvidence         `json:"cutover_evidence"`
	IdempotencyDigestSHA256 string                    `json:"idempotency_digest_sha256"`
}

type MigrationPlanner struct {
	now func() time.Time
}

func NewMigrationPlanner() *MigrationPlanner {
	return &MigrationPlanner{now: func() time.Time { return time.Now().UTC() }}
}

func (p *MigrationPlanner) DryRun(catalog Catalog, objects []MigrationObject, stage ResolverCutoverStage) (MigrationDryRunReport, error) {
	if p == nil {
		p = NewMigrationPlanner()
	}
	if stage == "" {
		stage = ResolverStageShadowCompare
	}
	report := MigrationDryRunReport{
		DryRun:          true,
		OrgID:           catalog.OrgID,
		CatalogRevision: catalog.Revision,
		GeneratedAt:     p.now().UTC(),
		Counts: map[string]int{
			MigrationCategoryExactProfile:   0,
			MigrationCategoryDeduplicated:   0,
			MigrationCategoryObjectOverride: 0,
			MigrationCategoryUnmapped:       0,
		},
		CutoverEvidence: CutoverPlan(stage),
	}

	profileByHash, err := profileContentIndex(catalog)
	if err != nil {
		return report, err
	}
	type planned struct {
		object  MigrationObject
		ref     MigrationObjectRef
		content RuntimeContent
		hash    string
		cli     *CLIDefinition
		model   *ModelDefinition
	}
	groups := map[string][]planned{}
	exact := map[string]*ExactProfileMapping{}

	for _, object := range sortedMigrationObjects(objects) {
		report.TotalObjects++
		ref := MigrationObjectRef{ObjectType: object.ObjectType, ObjectID: object.ObjectID}
		content, hash, cli, model, issue := planObject(catalog, object)
		if issue != nil {
			report.Unmapped = append(report.Unmapped, *issue)
			report.Counts[MigrationCategoryUnmapped]++
			report.ShadowCompare = append(report.ShadowCompare, ShadowCompareEntry{
				Object: ref, Result: "unmapped", Reason: issue.Reason, Message: issue.Message,
			})
			continue
		}
		if prof, ok := profileByHash[hash]; ok {
			key := prof.ID
			if exact[key] == nil {
				exact[key] = &ExactProfileMapping{
					ProfileID: prof.ID, ProfileKey: prof.Key, ContentHash: hash, Objects: []MigrationObjectRef{},
				}
			}
			exact[key].Objects = append(exact[key].Objects, ref)
			report.Counts[MigrationCategoryExactProfile]++
			report.ShadowCompare = append(report.ShadowCompare, ShadowCompareEntry{Object: ref, Result: "match", ContentHash: hash})
			continue
		}
		groups[hash] = append(groups[hash], planned{object: object, ref: ref, content: content, hash: hash, cli: cli, model: model})
		report.ShadowCompare = append(report.ShadowCompare, ShadowCompareEntry{Object: ref, Result: "match", ContentHash: hash})
	}

	for _, mapping := range exact {
		sortRefs(mapping.Objects)
		report.ExactProfiles = append(report.ExactProfiles, *mapping)
	}
	sort.Slice(report.ExactProfiles, func(i, j int) bool {
		return report.ExactProfiles[i].ProfileKey < report.ExactProfiles[j].ProfileKey
	})

	hashes := make([]string, 0, len(groups))
	for hash := range groups {
		hashes = append(hashes, hash)
	}
	sort.Strings(hashes)
	for _, hash := range hashes {
		items := groups[hash]
		if len(items) > 1 {
			first := items[0]
			proposed := ProposedDedupProfile{
				ProposedKey: "profile-" + shortHash(hash),
				Name:        "Runtime " + shortHash(hash),
				CLIKey:      first.cli.Key,
				ModelKey:    first.model.Key,
				Parameters:  cloneMap(first.content.Parameters),
				ContentHash: hash,
				Objects:     make([]MigrationObjectRef, 0, len(items)),
			}
			for _, item := range items {
				proposed.Objects = append(proposed.Objects, item.ref)
			}
			sortRefs(proposed.Objects)
			report.DeduplicatedProfiles = append(report.DeduplicatedProfiles, proposed)
			report.Counts[MigrationCategoryDeduplicated] += len(items)
			continue
		}
		item := items[0]
		report.ObjectOverrides = append(report.ObjectOverrides, ObjectOverrideMapping{
			Object: item.ref,
			Selection: RuntimeSelection{
				Mode:       SelectionOverride,
				CLIID:      item.cli.ID,
				ModelID:    item.model.ID,
				Parameters: cloneMap(item.object.Parameters),
			},
			ContentHash: hash,
			CLIKey:      item.content.CLIKey,
			ModelKey:    item.content.ModelKey,
			Parameters:  cloneMap(item.content.Parameters),
		})
		report.Counts[MigrationCategoryObjectOverride]++
	}
	sort.Slice(report.ObjectOverrides, func(i, j int) bool {
		return refLess(report.ObjectOverrides[i].Object, report.ObjectOverrides[j].Object)
	})
	sort.Slice(report.ShadowCompare, func(i, j int) bool {
		return refLess(report.ShadowCompare[i].Object, report.ShadowCompare[j].Object)
	})
	sort.Slice(report.Unmapped, func(i, j int) bool {
		return refLess(report.Unmapped[i].Object, report.Unmapped[j].Object)
	})
	report.IdempotencyDigestSHA256 = reportDigest(report)
	return report, nil
}

func profileContentIndex(catalog Catalog) (map[string]RuntimeProfile, error) {
	resolver := NewRuntimeResolver(&staticCatalogRepo{catalog: catalog})
	resolver.now = func() time.Time { return time.Unix(0, 0).UTC() }
	out := map[string]RuntimeProfile{}
	for _, profile := range catalog.Profiles {
		if !profile.Enabled {
			continue
		}
		snapshot, err := resolver.ResolveCatalog(catalog, RuntimeSelection{Mode: SelectionProfile, ProfileID: profile.ID})
		if err != nil {
			return nil, err
		}
		hash := contentHash(contentFromSnapshot(snapshot))
		if _, exists := out[hash]; !exists {
			out[hash] = profile
		}
	}
	return out, nil
}

func planObject(catalog Catalog, object MigrationObject) (RuntimeContent, string, *CLIDefinition, *ModelDefinition, *UnmappedMigrationObject) {
	ref := MigrationObjectRef{ObjectType: object.ObjectType, ObjectID: object.ObjectID}
	if strings.TrimSpace(object.Legacy.CLI) == "" {
		return RuntimeContent{}, "", nil, nil, unmapped(ref, ReasonMigrationMissingCLI, "legacy runtime is missing CLI context", map[string]any{"model": object.Legacy.Model}, object.Legacy)
	}
	cli := findLegacyCLI(catalog.CLIs, object.Legacy.CLI)
	if cli == nil {
		return RuntimeContent{}, "", nil, nil, unmapped(ref, ReasonCLINotFound, "legacy CLI cannot be mapped exactly", map[string]any{"cli": object.Legacy.CLI}, object.Legacy)
	}
	model := findLegacyModel(catalog.Models, object.Legacy.Model)
	if model == nil {
		return RuntimeContent{}, "", nil, nil, unmapped(ref, ReasonModelNotFound, "legacy model cannot be mapped exactly", map[string]any{"model": object.Legacy.Model}, object.Legacy)
	}
	resolver := NewRuntimeResolver(&staticCatalogRepo{catalog: catalog})
	resolver.now = func() time.Time { return time.Unix(0, 0).UTC() }
	snapshot, err := resolver.ResolveCatalog(catalog, RuntimeSelection{
		Mode:       SelectionOverride,
		CLIID:      cli.ID,
		ModelID:    model.ID,
		Parameters: cloneMap(object.Parameters),
	})
	if err != nil {
		reason := ReasonLegacyUnmapped
		var rt *Error
		if errors.As(err, &rt) {
			reason = rt.Reason
		}
		return RuntimeContent{}, "", nil, nil, unmapped(ref, reason, "legacy runtime maps to invalid Runtime Catalog content", map[string]any{"error": err.Error()}, object.Legacy)
	}
	content := contentFromSnapshot(snapshot)
	return content, contentHash(content), cli, model, nil
}

func unmapped(ref MigrationObjectRef, reason Reason, msg string, details map[string]any, original LegacyRuntime) *UnmappedMigrationObject {
	if details == nil {
		details = map[string]any{}
	}
	return &UnmappedMigrationObject{Object: ref, Reason: reason, Message: msg, Details: details, Original: original}
}

func contentFromSnapshot(snapshot RuntimeSnapshot) RuntimeContent {
	return RuntimeContent{CLIKey: snapshot.CLIKey, ModelKey: snapshot.ModelKey, Parameters: cloneMap(snapshot.Parameters)}
}

func contentHash(content RuntimeContent) string {
	raw, _ := json.Marshal(content)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func reportDigest(report MigrationDryRunReport) string {
	stable := report
	stable.GeneratedAt = time.Time{}
	stable.IdempotencyDigestSHA256 = ""
	raw, _ := json.Marshal(stable)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func shortHash(hash string) string {
	if len(hash) < 12 {
		return hash
	}
	return hash[:12]
}

func sortedMigrationObjects(objects []MigrationObject) []MigrationObject {
	out := append([]MigrationObject(nil), objects...)
	sort.Slice(out, func(i, j int) bool {
		a := MigrationObjectRef{ObjectType: out[i].ObjectType, ObjectID: out[i].ObjectID}
		b := MigrationObjectRef{ObjectType: out[j].ObjectType, ObjectID: out[j].ObjectID}
		return refLess(a, b)
	})
	return out
}

func refLess(a, b MigrationObjectRef) bool {
	if a.ObjectType != b.ObjectType {
		return a.ObjectType < b.ObjectType
	}
	return a.ObjectID < b.ObjectID
}

func sortRefs(refs []MigrationObjectRef) {
	sort.Slice(refs, func(i, j int) bool { return refLess(refs[i], refs[j]) })
}

func CutoverPlan(current ResolverCutoverStage) []CutoverEvidence {
	steps := []CutoverEvidence{
		{
			Stage:       ResolverStageLegacyRead,
			ReadPath:    "legacy",
			FeatureFlag: "ai_runtime_resolver_stage=legacy_read",
			Rollback:    "keep or restore legacy_read; new resolver is not consulted",
			Audit:       []string{"migration dry-run report", "schema version", "git commit"},
		},
		{
			Stage:       ResolverStageShadowCompare,
			ReadPath:    "legacy + shadow",
			FeatureFlag: "ai_runtime_resolver_stage=shadow_compare",
			Rollback:    "set ai_runtime_resolver_stage=legacy_read",
			Audit:       []string{"runtime_shadow_diff_total", "shadow_compare report", "unmapped report"},
		},
		{
			Stage:       ResolverStageNewResolverCanary,
			ReadPath:    "new resolver for scoped objects",
			FeatureFlag: "ai_runtime_resolver_stage=new_resolver_canary",
			Rollback:    "set ai_runtime_resolver_stage=shadow_compare or legacy_read",
			Audit:       []string{"canary object list", "runtime_resolution_total", "rollback drill evidence"},
		},
		{
			Stage:       ResolverStageOrganizationDefault,
			ReadPath:    "new resolver + organization default",
			FeatureFlag: "ai_runtime_resolver_stage=organization_default",
			Rollback:    "set ai_runtime_resolver_stage=new_resolver_canary or shadow_compare",
			Audit:       []string{"default profile audit_log revision", "runtime_legacy_fallback_total=0", "rollback drill evidence"},
		},
	}
	for i, step := range steps {
		if step.Stage == current {
			return steps[:i+1]
		}
	}
	return steps[:2]
}

type staticCatalogRepo struct{ catalog Catalog }

func (r *staticCatalogRepo) GetCatalog(_ context.Context, _ string) (Catalog, error) {
	return r.catalog, nil
}
func (*staticCatalogRepo) CreateCLI(context.Context, CLIDefinition, int64, AuditEvent) (int64, error) {
	return 0, fmt.Errorf("static catalog is read-only")
}
func (*staticCatalogRepo) UpdateCLI(context.Context, CLIDefinition, int64, AuditEvent) (int64, error) {
	return 0, fmt.Errorf("static catalog is read-only")
}
func (*staticCatalogRepo) CreateModel(context.Context, ModelDefinition, int64, AuditEvent) (int64, error) {
	return 0, fmt.Errorf("static catalog is read-only")
}
func (*staticCatalogRepo) UpdateModel(context.Context, ModelDefinition, int64, AuditEvent) (int64, error) {
	return 0, fmt.Errorf("static catalog is read-only")
}
func (*staticCatalogRepo) CreateProfile(context.Context, RuntimeProfile, int64, AuditEvent) (int64, error) {
	return 0, fmt.Errorf("static catalog is read-only")
}
func (*staticCatalogRepo) UpdateProfile(context.Context, RuntimeProfile, int64, AuditEvent) (int64, error) {
	return 0, fmt.Errorf("static catalog is read-only")
}
func (*staticCatalogRepo) SetDefaultProfile(context.Context, string, string, int64, AuditEvent) (int64, error) {
	return 0, fmt.Errorf("static catalog is read-only")
}
