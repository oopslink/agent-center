package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

const (
	rulePhasePlan     = "plan"
	rulePhaseExecute  = "execute"
	rulePhaseReview   = "review"
	rulePhaseRecovery = "recovery"
)

type teamRulesToolResponse struct {
	TeamID           string              `json:"team_id"`
	Phase            string              `json:"phase"`
	Commit           string              `json:"commit"`
	Rules            []teamRulesToolRule `json:"rules"`
	Skipped          []string            `json:"skipped_nonstandard"`
	RefreshSemantics string              `json:"refresh_semantics"`
}

// planningPromptDescription freezes the Team Memory rules that govern plan
// authoring into a newly-created resident supervisor session.  The caller invokes
// it exactly once per new session generation; subsequent turns therefore keep the
// same repo commit even when Team Memory moves, while a crash/reset that creates a
// new generation reloads from the center.
func (r *LocalRuntime) planningPromptDescription(ctx context.Context, agentID, description string) string {
	snapshot := r.loadTeamRules(ctx, agentID, rulePhasePlan)
	section := renderPlanningRuleSnapshot(snapshot)
	if section == "" {
		return description
	}
	if strings.TrimSpace(description) == "" {
		return section
	}
	return strings.TrimSpace(description) + "\n\n" + section
}

func renderPlanningRuleSnapshot(snapshot *executor.RuleSnapshot) string {
	if snapshot == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("== Team Memory rules (frozen planning snapshot) ==\n")
	fmt.Fprintf(&b, "Team: %s\nPhase: %s\nCommit: %s\nRefresh: %s\n",
		snapshot.TeamID, snapshot.Phase, snapshot.Commit, snapshot.RefreshSemantics)
	if len(snapshot.Skipped) > 0 {
		fmt.Fprintf(&b, "Skipped nonstandard rules: %s\n", strings.Join(snapshot.Skipped, ", "))
	}
	for _, rule := range snapshot.Rules {
		heading := strings.TrimSpace(rule.Title)
		if heading == "" {
			heading = strings.TrimSpace(rule.Slug)
		}
		fmt.Fprintf(&b, "\n### %s", heading)
		fmt.Fprintf(&b, "\nSource: %s\nApplies to: %s\n",
			strings.TrimSpace(rule.SourcePath), strings.Join(rule.AppliesTo, ", "))
		if d := strings.TrimSpace(rule.Description); d != "" {
			b.WriteString(d)
			b.WriteByte('\n')
		}
		b.WriteString(strings.TrimSpace(rule.Body))
		b.WriteByte('\n')
	}
	return strings.TrimSpace(b.String())
}

type teamRulesToolRule struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Body        string   `json:"body"`
	AppliesTo   []string `json:"applies_to"`
	SourcePath  string   `json:"source_path"`
}

func (r *LocalRuntime) loadTeamRules(ctx context.Context, agentID, phase string) *executor.RuleSnapshot {
	caller := r.toolCaller()
	if caller == nil {
		return nil
	}
	phase = normalizeRulePhaseForRuntime(phase)
	var raw json.RawMessage
	body := map[string]any{"agent_id": agentID, "phase": phase}
	if err := caller.CallAgentTool(ctx, "get_team_rules", body, &raw); err != nil {
		r.log("agent=%s team-rules phase=%s load failed: %v — continuing without team rules", agentID, phase, err)
		return nil
	}
	if len(raw) == 0 {
		return nil
	}
	var resp teamRulesToolResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		r.log("agent=%s team-rules phase=%s decode failed: %v — continuing without team rules", agentID, phase, err)
		return nil
	}
	if strings.TrimSpace(resp.Phase) == "" {
		resp.Phase = phase
	}
	if strings.TrimSpace(resp.TeamID) == "" && strings.TrimSpace(resp.Commit) == "" && len(resp.Rules) == 0 {
		return nil
	}
	snap := &executor.RuleSnapshot{
		TeamID:           strings.TrimSpace(resp.TeamID),
		Phase:            normalizeRulePhaseForRuntime(resp.Phase),
		Commit:           strings.TrimSpace(resp.Commit),
		Skipped:          append([]string(nil), resp.Skipped...),
		RefreshSemantics: strings.TrimSpace(resp.RefreshSemantics),
	}
	for _, tr := range resp.Rules {
		snap.Rules = append(snap.Rules, executor.RuleContext{
			Slug:        tr.Slug,
			Title:       tr.Title,
			Description: tr.Description,
			Body:        tr.Body,
			AppliesTo:   append([]string(nil), tr.AppliesTo...),
			SourcePath:  tr.SourcePath,
		})
	}
	return snap
}

func rulePhaseForTask(task *centerTaskDetail) string {
	if task == nil {
		return rulePhaseExecute
	}
	if strings.TrimSpace(task.OriginVerdictID) != "" || strings.TrimSpace(task.FollowsTaskID) != "" {
		return rulePhaseRecovery
	}
	if strings.TrimSpace(task.DispatchMode) == executor.DispatchModeSupervisorInline ||
		strings.TrimSpace(task.DeliveryContract) == executor.DeliveryContractEvidenceOnly {
		return rulePhaseReview
	}
	title := strings.ToLower(task.Title + " " + task.Description)
	if strings.Contains(title, "review") || strings.Contains(title, "gate") || strings.Contains(title, "verdict") {
		return rulePhaseReview
	}
	return rulePhaseExecute
}

func normalizeRulePhaseForRuntime(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case rulePhasePlan:
		return rulePhasePlan
	case rulePhaseReview:
		return rulePhaseReview
	case rulePhaseRecovery, "recover":
		return rulePhaseRecovery
	default:
		return rulePhaseExecute
	}
}
