package airuntime

import (
	"context"
	"sort"
	"strings"
	"time"
)

const (
	CutoverStageShadowCompare = "shadow_compare"
	CutoverStageAgentScope    = "agent_scope"
	CutoverStageOrgDefault    = "org_default"
	CutoverStageRollback      = "rollback"
)

type CutoverRequest struct {
	Stage     string   `json:"stage"`
	Enabled   bool     `json:"enabled"`
	ObjectIDs []string `json:"object_ids,omitempty"`
	Note      string   `json:"note,omitempty"`
}

type CutoverFlagChange struct {
	Key    string `json:"key"`
	Before string `json:"before"`
	After  string `json:"after"`
}

type CutoverReport struct {
	OrgID           string              `json:"org_id"`
	Stage           string              `json:"stage"`
	Action          string              `json:"action"`
	Flags           []CutoverFlagChange `json:"flags"`
	RollbackFlags   []CutoverFlagChange `json:"rollback_flags"`
	RollbackSummary string              `json:"rollback_summary"`
	OccurredAt      time.Time           `json:"occurred_at"`
}

type CutoverMutation struct {
	ID         string
	OrgID      string
	Actor      string
	Stage      string
	Action     string
	Flags      []CutoverFlagChange
	Rollback   map[string]any
	Before     map[string]string
	After      map[string]string
	OccurredAt time.Time
}

type ResolveObjectRequest struct {
	ObjectType string         `json:"object_type"`
	ObjectID   string         `json:"object_id"`
	Legacy     LegacyRuntime  `json:"legacy"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

func (s *Service) ResolveObject(ctx context.Context, orgID string, req ResolveObjectRequest) (RuntimeSnapshot, string, error) {
	repo, ok := s.repo.(MigrationRepository)
	if !ok {
		return RuntimeSnapshot{}, "legacy", runtimeError(ReasonMigrationUnavailable, "AI Runtime migration repository is not configured", nil)
	}
	objectType := strings.TrimSpace(req.ObjectType)
	if objectType == "" {
		objectType = "agent"
	}
	objectID := strings.TrimSpace(req.ObjectID)
	flags, err := repo.GetCutoverSettings(ctx, orgID)
	if err != nil {
		return RuntimeSnapshot{}, "legacy", err
	}
	if !useNewResolver(flags, orgID, objectType, objectID) {
		resolver := NewRuntimeResolver(s.repo)
		resolver.now = s.now
		snapshot, err := resolveLegacyObject(ctx, resolver, orgID, MigrationObject{
			OrgID: orgID, ObjectType: objectType, ObjectID: objectID,
			Legacy: req.Legacy, Parameters: req.Parameters,
		})
		return snapshot, "legacy", err
	}
	selections, err := repo.ListObjectSelections(ctx, orgID, objectType)
	if err != nil {
		return RuntimeSnapshot{}, "new", err
	}
	selection := RuntimeSelection{Mode: SelectionInherit}
	for _, candidate := range selections {
		if candidate.ObjectID == objectID {
			selection = candidate.Selection
			break
		}
	}
	resolver := NewRuntimeResolver(s.repo)
	resolver.now = s.now
	snapshot, err := resolver.Resolve(ctx, orgID, selection)
	return snapshot, "new", err
}

func (s *Service) ApplyCutover(ctx context.Context, orgID, actor string, req CutoverRequest) (CutoverReport, error) {
	repo, ok := s.repo.(MigrationRepository)
	if !ok {
		return CutoverReport{}, runtimeError(ReasonMigrationUnavailable, "AI Runtime migration repository is not configured", nil)
	}
	req.Stage = strings.TrimSpace(req.Stage)
	current, err := repo.GetCutoverSettings(ctx, orgID)
	if err != nil {
		return CutoverReport{}, err
	}
	after := cutoverAfterValues(orgID, req)
	if len(after) == 0 {
		return CutoverReport{}, runtimeError(ReasonSelectionInvalid, "cutover stage is invalid", map[string]any{"stage": req.Stage})
	}
	flags := make([]CutoverFlagChange, 0, len(after))
	for key, value := range after {
		flags = append(flags, CutoverFlagChange{Key: key, Before: current[key], After: value})
	}
	sort.Slice(flags, func(i, j int) bool { return flags[i].Key < flags[j].Key })
	rollbackFlags := make([]CutoverFlagChange, 0, len(flags))
	beforeMap, afterMap := map[string]string{}, map[string]string{}
	for _, flag := range flags {
		beforeMap[flag.Key] = flag.Before
		afterMap[flag.Key] = flag.After
		rollbackFlags = append(rollbackFlags, CutoverFlagChange{Key: flag.Key, Before: flag.After, After: flag.Before})
	}
	action := "disabled"
	if req.Enabled {
		action = "enabled"
	}
	if req.Stage == CutoverStageRollback {
		action = "rollback"
	}
	rollbackSummary := "restore listed flags to their recorded before values to return to the previous read path"
	mutation := CutoverMutation{
		ID: s.id(), OrgID: orgID, Actor: actor, Stage: req.Stage, Action: action,
		Flags: flags, Before: beforeMap, After: afterMap,
		Rollback: map[string]any{
			"summary": rollbackSummary,
			"flags":   rollbackFlags,
			"note":    req.Note,
		},
		OccurredAt: s.now(),
	}
	if err := repo.ApplyCutover(ctx, mutation); err != nil {
		return CutoverReport{}, err
	}
	return CutoverReport{
		OrgID: orgID, Stage: req.Stage, Action: action, Flags: flags,
		RollbackFlags: rollbackFlags, RollbackSummary: rollbackSummary, OccurredAt: mutation.OccurredAt,
	}, nil
}

func cutoverAfterValues(orgID string, req CutoverRequest) map[string]string {
	prefix := cutoverKeyPrefix(orgID)
	switch req.Stage {
	case CutoverStageShadowCompare:
		if req.Enabled {
			return map[string]string{prefix + "shadow_compare": "true"}
		}
		return map[string]string{prefix + "shadow_compare": "false"}
	case CutoverStageAgentScope:
		out := map[string]string{}
		if !req.Enabled {
			out[prefix+"agent_resolver"] = "legacy"
			out[prefix+"agent_resolver_allowlist"] = ""
			return out
		}
		ids := normalizedIDs(req.ObjectIDs)
		if len(ids) == 0 {
			out[prefix+"agent_resolver"] = "new"
			out[prefix+"agent_resolver_allowlist"] = ""
			return out
		}
		out[prefix+"agent_resolver"] = "allowlist"
		out[prefix+"agent_resolver_allowlist"] = strings.Join(ids, ",")
		return out
	case CutoverStageOrgDefault:
		if req.Enabled {
			return map[string]string{prefix + "org_default_resolver": "new"}
		}
		return map[string]string{prefix + "org_default_resolver": "legacy"}
	case CutoverStageRollback:
		return map[string]string{
			prefix + "agent_resolver":           "legacy",
			prefix + "agent_resolver_allowlist": "",
			prefix + "org_default_resolver":     "legacy",
		}
	default:
		return nil
	}
}

func useNewResolver(flags map[string]string, orgID, objectType, objectID string) bool {
	prefix := cutoverKeyPrefix(orgID)
	if objectType != "agent" {
		return flags[prefix+"org_default_resolver"] == "new"
	}
	switch flags[prefix+"agent_resolver"] {
	case "new":
		return true
	case "allowlist":
		for _, id := range strings.Split(flags[prefix+"agent_resolver_allowlist"], ",") {
			if strings.TrimSpace(id) == objectID {
				return true
			}
		}
	}
	return false
}

func cutoverKeyPrefix(orgID string) string {
	return "ai_runtime." + orgID + "."
}

func normalizedIDs(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range in {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
