package agentruntime

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
)

const (
	rulePhasePlan     = "plan"
	rulePhaseExecute  = "execute"
	rulePhaseReview   = "review"
	rulePhaseRecovery = "recovery"
)

type teamRuleIndexToolResponse struct {
	TeamID           string                  `json:"team_id"`
	Phase            string                  `json:"phase"`
	Commit           string                  `json:"commit"`
	Rules            []teamRuleIndexToolRule `json:"rules"`
	Skipped          []string                `json:"skipped_nonstandard"`
	RefreshSemantics string                  `json:"refresh_semantics"`
}

type teamRuleIndexToolRule struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	AppliesTo   []string `json:"applies_to"`
	BodyBytes   int      `json:"body_bytes"`
	SourcePath  string   `json:"source_path"`
}

func (r *LocalRuntime) loadTeamRules(ctx context.Context, agentID, phase, executionID string) *executor.RuleSnapshot {
	caller := r.toolCaller()
	if caller == nil {
		return nil
	}
	phase = normalizeRulePhaseForRuntime(phase)
	var raw json.RawMessage
	body := map[string]any{"agent_id": agentID, "phase": phase}
	if executionID = strings.TrimSpace(executionID); executionID != "" {
		body["execution_id"] = executionID
	}
	if err := caller.CallAgentTool(ctx, "get_team_rule_index", body, &raw); err != nil {
		r.log("agent=%s team-rule-index phase=%s load failed: %v", agentID, phase, err)
		return &executor.RuleSnapshot{Phase: phase, LoadError: err.Error()}
	}
	if len(raw) == 0 {
		return nil
	}
	var resp teamRuleIndexToolResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		r.log("agent=%s team-rule-index phase=%s decode failed: %v", agentID, phase, err)
		return &executor.RuleSnapshot{Phase: phase, LoadError: "decode get_team_rule_index response: " + err.Error()}
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
		LoadError:        "",
		Skipped:          append([]string(nil), resp.Skipped...),
		RefreshSemantics: strings.TrimSpace(resp.RefreshSemantics),
	}
	for _, tr := range resp.Rules {
		snap.Rules = append(snap.Rules, executor.RuleContext{
			Slug:        tr.Slug,
			Title:       tr.Title,
			Description: tr.Description,
			AppliesTo:   append([]string(nil), tr.AppliesTo...),
			BodyBytes:   tr.BodyBytes,
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
