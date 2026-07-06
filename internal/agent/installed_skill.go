package agent

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"
)

// InstalledSkill is one OBSERVED skill the agent-runtime resolved on disk
// (issue-4a45e9cc). It replaces the retired declared `skills` list: instead of a
// static wish, the runtime reports the REAL effective skill set it resolved, grouped
// into the four claude-code precedence LAYERS. Each skill carries its name +
// description (from the SKILL.md frontmatter) and whether it is SHADOWED — a
// higher-precedence layer defines the same name, so this copy is inert.
//
// It is a value object owned by the Agent BC (persisted in agent_installed_skills,
// migration 0101), keyed by the AgentRef the runtime reports it under. The center
// stores what the runtime computed; shadowing precedence is decided at collection
// time (the runtime is the only place that sees all four layers at once).
type InstalledSkill struct {
	AgentRef    AgentID
	Layer       SkillLayer
	Name        string
	Description string
	Shadowed    bool
	// CollectedAt is the batch collection time — every skill in one report shares it,
	// so an offline agent's panel can show "last collected X ago".
	CollectedAt time.Time
}

// SkillLayer is one of the four claude-code skill-resolution precedence layers.
// The string values are the wire + storage form and the UI grouping key.
type SkillLayer string

const (
	// SkillLayerBuiltin — skills shipped inside the claude-code CLI install.
	SkillLayerBuiltin SkillLayer = "built-in"
	// SkillLayerPlugin — skills contributed by installed plugins / marketplaces.
	SkillLayerPlugin SkillLayer = "plugin"
	// SkillLayerUser — skills under the user's ~/.claude/skills.
	SkillLayerUser SkillLayer = "user"
	// SkillLayerProject — skills under the project/workspace .claude/skills.
	SkillLayerProject SkillLayer = "project"
)

// skillLayerRank is the precedence, low→high: a skill NAME present in a
// higher-ranked layer SHADOWS the same name in every lower-ranked layer. "More
// local wins" — project overrides user overrides plugin overrides built-in.
var skillLayerRank = map[SkillLayer]int{
	SkillLayerBuiltin: 0,
	SkillLayerPlugin:  1,
	SkillLayerUser:    2,
	SkillLayerProject: 3,
}

// IsValid reports enum membership.
func (l SkillLayer) IsValid() bool {
	_, ok := skillLayerRank[l]
	return ok
}

// Rank returns the layer's precedence (higher = wins). Unknown layers rank -1 so a
// malformed report never silently out-ranks a valid layer.
func (l SkillLayer) Rank() int {
	if r, ok := skillLayerRank[l]; ok {
		return r
	}
	return -1
}

// ErrInvalidSkillLayer rejects a reported skill whose layer is not one of the four.
var ErrInvalidSkillLayer = errors.New("agent: invalid skill layer (want built-in|plugin|user|project)")

// NormalizeInstalledSkills validates + canonicalizes a reported skill set for one
// agent: it trims names/descriptions, drops entries with a blank name, rejects an
// unknown layer, and RECOMPUTES the shadowed flag from layer precedence so the stored
// truth is internally consistent regardless of what the reporter set (defense in
// depth — the runtime already computes it, but the center is the system of record).
// SAME-LAYER duplicate names COLLAPSE to the first seen (issue-4a45e9cc real-machine
// blocker): the store keys a row by (agent_ref, layer, name) and Claude Code resolves
// only ONE skill per name within a layer, so two same-layer copies of a name (a
// multi-plugin / multi-version install of e.g. skill-creator or frontend-design) are
// the SAME effective skill — keep one, drop the rest. Without this collapse the two
// copies mint the SAME store id and the whole report is rejected (UNIQUE constraint on
// agent_installed_skills.id) → the agent's panel stays empty. CROSS-layer duplicates
// are KEPT and expressed via the shadowed flag (highest-ranked layer effective, lower
// copies shadowed). The result is sorted by layer rank (built-in→project) then name for
// a stable read order. A nil/empty input returns nil.
func NormalizeInstalledSkills(in []InstalledSkill) ([]InstalledSkill, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make([]InstalledSkill, 0, len(in))
	seenInLayer := make(map[string]struct{}, len(in))
	for _, s := range in {
		s.Name = strings.TrimSpace(s.Name)
		s.Description = strings.TrimSpace(s.Description)
		if s.Name == "" {
			continue
		}
		if !s.Layer.IsValid() {
			return nil, ErrInvalidSkillLayer
		}
		// Collapse same-layer same-name to the first seen (one effective skill per name
		// per layer). Case-insensitive on name, matching the store id + shadow keys.
		lk := string(s.Layer) + "\x1f" + strings.ToLower(s.Name)
		if _, dup := seenInLayer[lk]; dup {
			continue
		}
		seenInLayer[lk] = struct{}{}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, nil
	}
	// Cross-layer shadowing: for each name the highest-ranked layer is effective; every
	// lower-layer copy is shadowed. After the same-layer collapse there is at most one
	// copy per (layer, name), so a name appears at most once per layer.
	bestRank := make(map[string]int, len(out))
	for _, s := range out {
		key := strings.ToLower(s.Name)
		if r, ok := bestRank[key]; !ok || s.Layer.Rank() > r {
			bestRank[key] = s.Layer.Rank()
		}
	}
	for i := range out {
		out[i].Shadowed = out[i].Layer.Rank() < bestRank[strings.ToLower(out[i].Name)]
	}
	sort.SliceStable(out, func(i, j int) bool {
		if ri, rj := out[i].Layer.Rank(), out[j].Layer.Rank(); ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// InstalledSkillRepository persists the OBSERVED installed-skill set per agent
// (agent_installed_skills, migration 0101). The set is REPLACED wholesale on each
// report — the runtime is the single source of truth for its current on-disk state.
type InstalledSkillRepository interface {
	// ReplaceForAgent atomically swaps the agent's whole installed-skill set
	// (delete-by-agent + insert-all) to mirror the latest report. An empty `skills`
	// clears the agent's rows (the runtime resolved nothing).
	ReplaceForAgent(ctx context.Context, agentRef AgentID, skills []InstalledSkill) error
	// ListByAgent returns the agent's installed skills ordered by layer precedence
	// (built-in→project) then name. Empty (no report yet) → empty slice, nil error.
	ListByAgent(ctx context.Context, agentRef AgentID) ([]InstalledSkill, error)
}
