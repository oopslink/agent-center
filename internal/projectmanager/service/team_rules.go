package service

import "strings"

// PlanRuleRefreshSemantics is the refresh boundary for plan-authoring tools. A
// resident supervisor may exchange many messages while building one plan; the
// MCP host freezes the plan-phase rule snapshot for that planning session so
// create_plan/edit_plan_topology do not refresh rules on every message. A fresh
// supervisor generation starts a fresh MCP host process and reloads from the
// current team repo HEAD.
const PlanRuleRefreshSemantics = "plan rules are snapshotted once per MCP planning session; create_plan/edit_plan_topology reuse that frozen snapshot for the session, and a new supervisor generation starts a fresh MCP host process that reloads from the current team repo"

const (
	PlanRuleSourceMCP         = "mcp_plan_tool"
	PlanRuleSourceAdmin       = "admin_plan_tool"
	PlanRuleSourceTeamMemory  = "team_memory"
	PlanRuleSourceUnavailable = "unavailable"
)

// RuleContext is the credential-free Team Memory rule material passed through
// plan-authoring commands for audit and tool responses.
type RuleContext struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Body        string   `json:"body,omitempty"`
	Enabled     bool     `json:"enabled"`
	AppliesTo   []string `json:"applies_to,omitempty"`
	BodyBytes   int      `json:"body_bytes,omitempty"`
	SourcePath  string   `json:"source_path,omitempty"`
}

// RuleSnapshot records the exact plan-phase Team Memory rules consumed by a
// plan-authoring command. It intentionally mirrors the executor input snapshot
// shape while adding Source/session metadata for the plan-tool freeze boundary.
type RuleSnapshot struct {
	TeamID             string        `json:"team_id,omitempty"`
	Phase              string        `json:"phase,omitempty"`
	Commit             string        `json:"commit,omitempty"`
	Source             string        `json:"source,omitempty"`
	PlanningSessionID  string        `json:"planning_session_id,omitempty"`
	PlanningGeneration int           `json:"planning_generation,omitempty"`
	LoadError          string        `json:"load_error,omitempty"`
	Rules              []RuleContext `json:"rules,omitempty"`
	Skipped            []string      `json:"skipped_nonstandard,omitempty"`
	RefreshSemantics   string        `json:"refresh_semantics,omitempty"`
}

func NormalizePlanRuleSnapshot(in *RuleSnapshot, fallbackSource string) *RuleSnapshot {
	if in == nil {
		return nil
	}
	out := &RuleSnapshot{
		TeamID:             strings.TrimSpace(in.TeamID),
		Phase:              strings.TrimSpace(in.Phase),
		Commit:             strings.TrimSpace(in.Commit),
		Source:             strings.TrimSpace(in.Source),
		PlanningSessionID:  strings.TrimSpace(in.PlanningSessionID),
		PlanningGeneration: in.PlanningGeneration,
		LoadError:          strings.TrimSpace(in.LoadError),
		Skipped:            append([]string(nil), in.Skipped...),
		RefreshSemantics:   strings.TrimSpace(in.RefreshSemantics),
	}
	if out.Phase == "" {
		out.Phase = "plan"
	}
	if out.Source == "" {
		out.Source = strings.TrimSpace(fallbackSource)
	}
	if out.Source == "" {
		out.Source = PlanRuleSourceMCP
	}
	if out.RefreshSemantics == "" {
		out.RefreshSemantics = PlanRuleRefreshSemantics
	}
	for _, r := range in.Rules {
		out.Rules = append(out.Rules, RuleContext{
			Slug:        strings.TrimSpace(r.Slug),
			Title:       strings.TrimSpace(r.Title),
			Description: strings.TrimSpace(r.Description),
			Body:        r.Body,
			Enabled:     r.Enabled,
			AppliesTo:   append([]string(nil), r.AppliesTo...),
			BodyBytes:   r.BodyBytes,
			SourcePath:  strings.TrimSpace(r.SourcePath),
		})
	}
	return out
}

func PlanRuleSnapshotAudit(in *RuleSnapshot) map[string]any {
	snap := NormalizePlanRuleSnapshot(in, "")
	if snap == nil {
		return nil
	}
	rules := make([]map[string]any, 0, len(snap.Rules))
	for _, r := range snap.Rules {
		row := map[string]any{
			"slug":        r.Slug,
			"title":       r.Title,
			"description": r.Description,
			"enabled":     r.Enabled,
			"applies_to":  r.AppliesTo,
			"body_bytes":  r.BodyBytes,
			"source_path": r.SourcePath,
		}
		if r.Body != "" {
			row["body"] = r.Body
		}
		rules = append(rules, row)
	}
	skipped := snap.Skipped
	if skipped == nil {
		skipped = []string{}
	}
	return map[string]any{
		"team_id":             snap.TeamID,
		"phase":               snap.Phase,
		"commit":              snap.Commit,
		"source":              snap.Source,
		"planning_session_id": snap.PlanningSessionID,
		"planning_generation": snap.PlanningGeneration,
		"load_error":          snap.LoadError,
		"rules":               rules,
		"skipped_nonstandard": skipped,
		"refresh_semantics":   snap.RefreshSemantics,
	}
}
