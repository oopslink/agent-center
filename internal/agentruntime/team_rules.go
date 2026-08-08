package agentruntime

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/oopslink/agent-center/internal/agentruntime/executor"
	"github.com/oopslink/agent-center/internal/agentruntime/orchestrator"
	"github.com/oopslink/agent-center/internal/conversation"
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

type teamRulesToolRule struct {
	Slug        string   `json:"slug"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Body        string   `json:"body"`
	Enabled     bool     `json:"enabled"`
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
			Enabled:     true,
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

func (r *LocalRuntime) planningRulesForConverse(ctx context.Context, req ConverseRequest) *executor.RuleSnapshot {
	if !converseLooksLikePlanning(req) {
		return nil
	}
	return r.loadTeamRules(ctx, req.AgentID, rulePhasePlan)
}

func (r *LocalRuntime) planningRulesForWorkAvailable(ctx context.Context, agentID, taskID string) *executor.RuleSnapshot {
	if r.toolCaller() == nil {
		return nil
	}
	task, err := r.fetchCenterTask(ctx, agentID, taskID)
	if err != nil {
		r.log("agent=%s task=%s team-rules phase=plan task probe failed: %v — continuing without planning team rules", agentID, taskID, err)
		return nil
	}
	if !taskLooksLikePlanning(task) {
		return nil
	}
	return r.loadTeamRules(ctx, agentID, rulePhasePlan)
}

func planningRulesForInjectedWork(ctx context.Context, rt *LocalRuntime, req WorkRequest) *executor.RuleSnapshot {
	if rt == nil || !routesSupervisorInline(req.DispatchMode) || !textLooksLikePlanning(req.Brief) {
		return nil
	}
	return rt.loadTeamRules(ctx, req.AgentID, rulePhasePlan)
}

func appendRuleSnapshotBrief(brief string, snap *executor.RuleSnapshot) string {
	rendered := orchestrator.RenderRuleSnapshot(snap)
	if strings.TrimSpace(rendered) == "" {
		return brief
	}
	if strings.TrimSpace(brief) == "" {
		return rendered
	}
	return brief + "\n\n" + rendered
}

func converseLooksLikePlanning(req ConverseRequest) bool {
	if oc, ok := conversation.ResolveOwnerContext(req.OwnerRef); ok && oc.Kind == conversation.OwnerKindPlan {
		return true
	}
	if strings.EqualFold(strings.TrimSpace(req.ConvKind), string(conversation.ConversationKindPlan)) {
		return true
	}
	return textLooksLikePlanning(req.MessageText)
}

func taskLooksLikePlanning(task *centerTaskDetail) bool {
	if task == nil || !routesSupervisorInline(task.DispatchMode) {
		return false
	}
	return textLooksLikePlanning(task.Title + "\n" + task.Description)
}

func textLooksLikePlanning(text string) bool {
	s := strings.ToLower(strings.TrimSpace(text))
	if s == "" {
		return false
	}
	for _, needle := range []string{
		"create_plan",
		"edit_plan_topology",
		"add_task_to_plan",
		"add_plan_dependency",
		"start_plan",
		"plan dag",
		"plan topology",
		"plan orchestration",
		"plan author",
		"create a plan",
		"create the plan",
		"build a plan",
		"build the plan",
		"topology",
		"dag",
		"编排",
		"拓扑",
		"创建计划",
		"创建 plan",
	} {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}
