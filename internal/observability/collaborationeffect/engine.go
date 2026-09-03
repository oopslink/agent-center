package collaborationeffect

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

type Engine struct{ Version string }

func NewEngine(version string) Engine {
	if version == "" {
		version = RuleVersionV1
	}
	return Engine{Version: version}
}

func (e Engine) Evaluate(f Fact, deps []Dependency) ([]Effect, []Dependency, *Diagnostic) {
	if f.EventID == "" || f.ProjectID == "" || f.OccurredAt.IsZero() {
		return nil, nil, e.skip(f, "missing event_id, project_id, or occurred_at")
	}
	p := f.Payload
	actor := resolveActor(f)
	var effects []Effect
	var learned []Dependency
	switch f.EventType {
	case "pm.task.assigned":
		assignee := str(p, "assignee")
		if f.TaskID == "" || !isAgent(assignee) {
			return nil, nil, e.skip(f, "assign missing task_id or agent assignee")
		}
		effects = append(effects, e.effect(f, assignee, "", RelationAssign, PolarityNeutral, 1, state("assignee", ""), state("assignee", assignee)))
	case "pm.task.reassigned":
		prev, next := str(p, "previous_assignee"), str(p, "assignee")
		if f.TaskID == "" || !isAgent(prev) || !isAgent(next) {
			return nil, nil, e.skip(f, "reassign missing agent endpoints")
		}
		effects = append(effects, e.effect(f, actorOr(actor, next), next, RelationReassign, PolarityMixed, 2, state("assignee", prev), state("assignee", next)))
	case "pm.task.state_changed":
		from, to := str(p, "prev_status"), str(p, "status")
		if f.TaskID == "" || from == "" || to == "" {
			return nil, nil, e.skip(f, "state change missing task_id or status transition")
		}
		source := actorOr(actor, str(p, "assignee"))
		switch to {
		case "blocked":
			effects = append(effects, e.effect(f, source, "", RelationBlock, PolarityNegative, 3, state("task_status", from), state("task_status", to)))
		case "completed":
			effects = append(effects, e.effect(f, source, "", RelationComplete, PolarityPositive, 2, state("task_status", from), state("task_status", to)))
			effects = append(effects, e.dependencyReleaseEffects(f, deps, source, from, to)...)
		default:
			return nil, nil, nil
		}
	case "pm.audit_recorded":
		change := str(p, "change_type")
		objectID := str(p, "object_id")
		if objectID != "" && f.TaskID == "" && str(p, "object_type") == "task" {
			f.TaskID = objectID
		}
		switch change {
		case "assigned", "claimed", "auto_assigned":
			next := str(p, "to_value")
			if f.TaskID == "" || !isAgent(next) {
				return nil, nil, e.skip(f, "audit assign missing task or agent assignee")
			}
			effects = append(effects, e.effect(f, actorOr(actor, next), "", RelationAssign, PolarityNeutral, 1, state("assignee", str(p, "from_value")), state("assignee", next)))
		case "reassigned":
			prev, next := str(p, "from_value"), str(p, "to_value")
			if f.TaskID == "" || !isAgent(prev) || !isAgent(next) {
				return nil, nil, e.skip(f, "audit reassign missing agent endpoints")
			}
			effects = append(effects, e.effect(f, actorOr(actor, next), next, RelationReassign, PolarityMixed, 2, state("assignee", prev), state("assignee", next)))
		case "status_changed":
			from, to := str(p, "from_value"), str(p, "to_value")
			if f.TaskID == "" || from == "" || to == "" {
				return nil, nil, e.skip(f, "audit status missing transition")
			}
			if to == "running" && from == "blocked" {
				effects = append(effects, e.effect(f, actor, "", RelationUnblock, PolarityPositive, 3, state("task_status", from), state("task_status", to)))
			} else if to == "blocked" {
				effects = append(effects, e.effect(f, actor, "", RelationBlock, PolarityNegative, 3, state("task_status", from), state("task_status", to)))
			} else if to == "completed" {
				effects = append(effects, e.effect(f, actor, "", RelationComplete, PolarityPositive, 2, state("task_status", from), state("task_status", to)))
				effects = append(effects, e.dependencyReleaseEffects(f, deps, actor, from, to)...)
			}
		case "review_verdict":
			verdict, blocking := str(p, "to_value"), boolv(detail(p), "blocking")
			if f.TaskID == "" || (verdict != "pass" && verdict != "reject" && !blocking) {
				return nil, nil, e.skip(f, "review verdict missing or unknown")
			}
			if verdict == "pass" && !blocking {
				effects = append(effects, e.effect(f, actor, "", RelationReviewAccept, PolarityPositive, 2, state("review_verdict", ""), state("review_verdict", "pass")))
			} else {
				effects = append(effects, e.effect(f, actor, "", RelationReviewReject, PolarityMixed, 2, map[string]any{"review_verdict": "", "progress": "pending"}, map[string]any{"review_verdict": verdict, "progress": "delayed", "quality": "improved_or_unknown"}))
			}
		case "dependency_added":
			d := detail(p)
			from, to := str(d, "from"), str(d, "to")
			if from == "" || to == "" {
				return nil, nil, e.skip(f, "dependency missing endpoints")
			}
			// AddPlanDependency(from, to) means from depends_on to:
			// from is the downstream dependent and to is the upstream prerequisite.
			learned = append(learned, Dependency{ProjectID: f.ProjectID, PlanID: str(d, "plan_id"), UpstreamTaskID: to, DownstreamTaskID: from, SourceEventID: f.EventID, OccurredAt: f.OccurredAt})
		case "dependency_removed":
			return nil, nil, nil
		default:
			return nil, nil, nil
		}
	default:
		return nil, nil, nil
	}
	return effects, learned, nil
}

func (e Engine) dependencyReleaseEffects(f Fact, deps []Dependency, source, fromStatus, toStatus string) []Effect {
	var effects []Effect
	for _, d := range deps {
		ff := f
		ff.TaskID = d.DownstreamTaskID
		ff.EventID = f.EventID + "+" + d.SourceEventID
		effects = append(effects, e.effectWithEvidence(ff, source, "", RelationDependencyRelease, PolarityPositive, 3, map[string]any{"upstream_task_status": fromStatus, "downstream_task_id": d.DownstreamTaskID}, map[string]any{"upstream_task_status": toStatus, "downstream_task_id": d.DownstreamTaskID, "released": true}, []string{d.SourceEventID, f.EventID}))
	}
	return effects
}

func (e Engine) effect(f Fact, source, target string, rel RelationType, pol Polarity, mag int, before, after map[string]any) Effect {
	return e.effectWithEvidence(f, source, target, rel, pol, mag, before, after, []string{f.EventID})
}
func (e Engine) effectWithEvidence(f Fact, source, target string, rel RelationType, pol Polarity, mag int, before, after map[string]any, evidence []string) Effect {
	sort.Strings(evidence)
	h := sha256.Sum256([]byte(e.Version + "\x00" + strings.Join(evidence, "\x00") + "\x00" + f.TaskID + "\x00" + string(rel) + "\x00" + source + "\x00" + target))
	return Effect{EffectID: "ce_" + hex.EncodeToString(h[:16]), ProjectID: f.ProjectID, TargetTaskID: f.TaskID, SourceAgentRef: source, TargetAgentRef: target, RelationType: rel, Polarity: pol, Magnitude: mag, Confidence: "high", OccurredAt: f.OccurredAt.UTC(), RuleVersion: e.Version, EvidenceEventIDs: evidence, BeforeState: before, AfterState: after, ExplanationKey: "collaboration.effect." + string(rel)}
}
func (e Engine) skip(f Fact, reason string) *Diagnostic {
	return &Diagnostic{SourceEventID: f.EventID, RuleVersion: e.Version, Reason: reason, OccurredAt: f.OccurredAt}
}
func state(k, v string) map[string]any      { return map[string]any{k: v} }
func str(m map[string]any, k string) string { v, _ := m[k].(string); return v }
func boolv(m map[string]any, k string) bool { v, _ := m[k].(bool); return v }
func detail(p map[string]any) map[string]any {
	if v, ok := p["detail"].(map[string]any); ok {
		return v
	}
	return map[string]any{}
}
func isAgent(v string) bool { return strings.HasPrefix(v, "agent:") && len(v) > 6 }
func actorOr(actor, fallback string) string {
	if isAgent(actor) {
		return actor
	}
	if isAgent(fallback) {
		return fallback
	}
	return actor
}
func resolveActor(f Fact) string {
	if isAgent(f.ActorRef) {
		return f.ActorRef
	}
	if f.ActorRef != "system" && f.ActorRef != "" {
		return f.ActorRef
	}
	if v := str(f.Payload, "actor_ref"); isAgent(v) {
		return v
	}
	return "system/unknown"
}
