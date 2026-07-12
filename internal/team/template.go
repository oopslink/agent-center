package team

import (
	"strings"
	"time"
)

// template.go implements the Team template artifact (design §6): an org-level,
// user-managed snapshot of a team's reusable shape — role composition + per-role
// config, a referenced workflow template, and the generalizable experience
// (skills/rules/principles). A template is a SNAPSHOT: instances are independent
// and are NOT retro-updated (design §9 "模版版本不 retro").
//
// Three first-class create/manage paths (design §6), all landing on this type:
//   - manual create / update / delete / list (see service layer);
//   - extract_from_team: snapshot a live team, keeping only the generalizable
//     (team/global-scope) layer and dropping project-specific facts, plus a
//     scrub pass that HIGHLIGHTS suspected proprietary tokens for manual
//     curation (ExtractFromTeam, below);
//   - import / export as JSON for backup and cross-org sharing (see
//     template_io.go).

// maxTemplateNameLen bounds a template name (mirrors team-name cap).
const maxTemplateNameLen = 80

// ExperienceScope tags a generalizable experience with the memory scope it came
// from (design §3). Only team/global-scoped experiences are portable; project
// scope is proprietary and must be dropped on extraction.
type ExperienceScope string

const (
	// ExpScopeTeam is team-shared, generalizable experience — portable.
	ExpScopeTeam ExperienceScope = "team"
	// ExpScopeGlobal is platform-level experience — portable.
	ExpScopeGlobal ExperienceScope = "global"
	// ExpScopeProject is project-specific fact — NOT portable, dropped on extract.
	ExpScopeProject ExperienceScope = "project"
)

// Portable reports whether experience of this scope may be carried into a
// template (design §3/§6: team/global yes, project no).
func (s ExperienceScope) Portable() bool {
	return s == ExpScopeTeam || s == ExpScopeGlobal
}

// Experience is one generalizable memory/skill/rule entry captured in a
// template. It mirrors the one-file-per-experience memory model (design §9).
type Experience struct {
	Slug        string
	Title       string
	Description string
	Body        string
	Scope       ExperienceScope
	Tags        []string
}

// RoleSlot is a template role: the per-role config (cli/model/tags/concurrency)
// plus Count — the role composition / 配比 (how many agents to instantiate for
// this role). Count defaults to 1 when non-positive.
type RoleSlot struct {
	Config RoleConfig
	Count  int
}

// TeamTemplate is the org-level snapshot artifact (design §6).
type TeamTemplate struct {
	ID          string
	OrgID       string
	Name        string
	Description string
	// Roles is the role composition + per-role config (design §6 block ①).
	Roles []RoleSlot
	// WorkflowTemplateRef references a workflow template (design §6 block ②).
	WorkflowTemplateRef string
	// Experiences are the portable skills/rules/principles (design §6 block ③).
	Experiences []Experience
	// Curated records that a human ran the mandatory manual curation pass
	// (design §9). export / cross-org share REQUIRE this (Validate enforces it
	// on the IO path; see template_io.go).
	Curated   bool
	Version   int
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewTemplateInput carries fields to construct a fresh template.
type NewTemplateInput struct {
	ID                  string
	OrgID               string
	Name                string
	Description         string
	Roles               []RoleSlot
	WorkflowTemplateRef string
	Experiences         []Experience
	Curated             bool
	CreatedAt           time.Time
}

// NewTemplate validates and returns a version-1 template.
func NewTemplate(in NewTemplateInput) (*TeamTemplate, error) {
	name := strings.TrimSpace(in.Name)
	if name == "" || len(name) > maxTemplateNameLen {
		return nil, ErrInvalidTemplate
	}
	if strings.TrimSpace(in.ID) == "" {
		return nil, ErrInvalidTemplate
	}
	roles, err := normalizeSlots(in.Roles)
	if err != nil {
		return nil, err
	}
	ts := in.CreatedAt
	if ts.IsZero() {
		ts = time.Now()
	}
	return &TeamTemplate{
		ID:                  strings.TrimSpace(in.ID),
		OrgID:               strings.TrimSpace(in.OrgID),
		Name:                name,
		Description:         in.Description,
		Roles:               roles,
		WorkflowTemplateRef: strings.TrimSpace(in.WorkflowTemplateRef),
		Experiences:         append([]Experience(nil), in.Experiences...),
		Curated:             in.Curated,
		Version:             1,
		CreatedAt:           ts.UTC(),
		UpdatedAt:           ts.UTC(),
	}, nil
}

// normalizeSlots validates each RoleSlot, defaults Count/Concurrency, and
// rejects duplicate role names.
func normalizeSlots(in []RoleSlot) ([]RoleSlot, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]RoleSlot, 0, len(in))
	for _, sl := range in {
		role := strings.TrimSpace(sl.Config.Role)
		if role == "" {
			return nil, ErrInvalidRole
		}
		if _, dup := seen[role]; dup {
			return nil, ErrInvalidRole
		}
		if sl.Config.MaxConcurrency < 0 {
			return nil, ErrInvalidRole
		}
		seen[role] = struct{}{}
		cfg := sl.Config
		cfg.Role = role
		if cfg.MaxConcurrency == 0 {
			cfg.MaxConcurrency = 1
		}
		cfg.CapabilityTags = append([]string(nil), sl.Config.CapabilityTags...)
		count := sl.Count
		if count <= 0 {
			count = 1
		}
		out = append(out, RoleSlot{Config: cfg, Count: count})
	}
	return out, nil
}

// RoleConfigs projects the template's role slots to plain RoleConfigs (used when
// creating the instantiated team via the S1 team service).
func (t *TeamTemplate) RoleConfigs() []RoleConfig {
	out := make([]RoleConfig, 0, len(t.Roles))
	for _, sl := range t.Roles {
		out = append(out, sl.Config)
	}
	return out
}

// TeamSnapshot is the live-team view ExtractFromTeam consumes: the team's
// declared roles plus the memory experiences it has accumulated (each tagged
// with its scope). Callers assemble it from the S1 team service + the team's
// center-hosted memory repo.
type TeamSnapshot struct {
	Team                *Team
	WorkflowTemplateRef string
	Experiences         []Experience
	// Counts optionally overrides per-role instance counts (role → count). When
	// absent a role defaults to Count 1.
	Counts map[string]int
}

// ExtractResult is the output of ExtractFromTeam: a DRAFT template (never
// export-ready on its own) plus scrub findings that a human must review. Design
// §9: manual curation is load-bearing; extraction only produces a draft.
type ExtractResult struct {
	Draft         *TeamTemplate
	ScrubFindings []ScrubFinding
	// DroppedProject counts the project-scoped experiences filtered out.
	DroppedProject int
}

// ExtractFromTeam snapshots a live team into a DRAFT template (design §6):
//   - copies the role composition + per-role config;
//   - keeps ONLY portable (team/global-scope) experiences, dropping project
//     scope (design §3 scope boundary);
//   - runs a scrub pass over the surviving experiences to HIGHLIGHT suspected
//     proprietary tokens (repo names / code names like "T950" / paths) — it
//     does NOT auto-remove them (design §9: no auto-scrub pipeline, manual
//     curation required).
//
// The draft is returned with Curated=false; it must pass manual curation before
// export / cross-org share (enforced on the IO path).
func ExtractFromTeam(snap TeamSnapshot, newID string, gen func() string, now time.Time) (*ExtractResult, error) {
	if snap.Team == nil {
		return nil, ErrInvalidTemplate
	}
	if newID == "" && gen != nil {
		newID = gen()
	}
	roles := make([]RoleSlot, 0, len(snap.Team.Roles()))
	for _, rc := range snap.Team.Roles() {
		count := 1
		if snap.Counts != nil {
			if c, ok := snap.Counts[rc.Role]; ok && c > 0 {
				count = c
			}
		}
		roles = append(roles, RoleSlot{Config: rc, Count: count})
	}

	kept := make([]Experience, 0, len(snap.Experiences))
	dropped := 0
	var findings []ScrubFinding
	for _, e := range snap.Experiences {
		if !e.Scope.Portable() {
			dropped++
			continue
		}
		kept = append(kept, e)
		findings = append(findings, ScrubExperience(e)...)
	}

	draft, err := NewTemplate(NewTemplateInput{
		ID:                  newID,
		OrgID:               snap.Team.OrgID(),
		Name:                snap.Team.Name() + " (extracted)",
		Description:         snap.Team.Description(),
		Roles:               roles,
		WorkflowTemplateRef: snap.WorkflowTemplateRef,
		Experiences:         kept,
		Curated:             false,
		CreatedAt:           now,
	})
	if err != nil {
		return nil, err
	}
	return &ExtractResult{Draft: draft, ScrubFindings: findings, DroppedProject: dropped}, nil
}
