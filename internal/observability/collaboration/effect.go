package collaboration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
)

const RuleVersionMVP = "collaboration-effect.mvp.v1"

type Effect struct {
	EffectID         string
	ProjectID        string
	TargetTaskID     string
	SourceAgentRef   string
	TargetAgentRef   string
	RelationType     string
	Polarity         string
	Magnitude        int
	Confidence       string
	OccurredAt       time.Time
	RuleVersion      string
	EvidenceEventIDs []string
	BeforeState      map[string]any
	AfterState       map[string]any
	ExplanationKey   string
}

type Projector struct {
	deps        map[dependencyKey][]dependencyFact
	effects     map[string]Effect
	effectOrder []string
}

func NewProjector() *Projector {
	return &Projector{
		deps:    map[dependencyKey][]dependencyFact{},
		effects: map[string]Effect{},
	}
}

func (p *Projector) Project(ctx context.Context, events []*observability.Event) error {
	if p == nil {
		return nil
	}
	for _, ev := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ev == nil {
			continue
		}
		switch string(ev.Type()) {
		case "pm.audit_recorded":
			p.projectAuditRecorded(ev)
		case "pm.task.state_changed":
			p.projectTaskStateChanged(eventFact{
				id:         string(ev.ID()),
				occurredAt: ev.OccurredAt(),
				eventType:  string(ev.Type()),
				refs:       ev.Refs(),
				actor:      string(ev.Actor()),
				payload:    ev.Payload(),
			})
		}
	}
	return nil
}

func (p *Projector) ProjectOutbox(ctx context.Context, events []outbox.Event) error {
	if p == nil {
		return nil
	}
	for _, ev := range events {
		if err := ctx.Err(); err != nil {
			return err
		}
		if ev.EventType != "pm.task.state_changed" {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(ev.Payload), &payload); err != nil {
			continue
		}
		p.projectTaskStateChanged(eventFact{
			id:         ev.ID,
			occurredAt: ev.CreatedAt,
			eventType:  ev.EventType,
			refs:       refsFromOutbox(ev.Refs),
			payload:    payload,
		})
	}
	return nil
}

func (p *Projector) Effects() []Effect {
	if p == nil {
		return nil
	}
	out := make([]Effect, 0, len(p.effectOrder))
	for _, id := range p.effectOrder {
		out = append(out, cloneEffect(p.effects[id]))
	}
	return out
}

func (p *Projector) projectAuditRecorded(ev *observability.Event) {
	pl := ev.Payload()
	if stringValue(pl["object_type"]) != "plan" || stringValue(pl["change_type"]) != "dependency_added" {
		return
	}
	detail, ok := pl["detail"].(map[string]any)
	if !ok {
		return
	}
	downstream := stringValue(detail["from"])
	upstream := stringValue(detail["to"])
	if downstream == "" || upstream == "" {
		return
	}
	refs := ev.Refs()
	projectID := refs.ProjectID
	planID := refs.PlanID
	if planID == "" {
		planID = stringValue(pl["object_id"])
	}
	if projectID == "" || planID == "" {
		return
	}
	key := dependencyKey{projectID: projectID, planID: planID, upstreamTaskID: upstream}
	p.deps[key] = append(p.deps[key], dependencyFact{
		eventID:          string(ev.ID()),
		occurredAt:       ev.OccurredAt(),
		downstreamTaskID: downstream,
		upstreamTaskID:   upstream,
	})
}

func (p *Projector) projectTaskStateChanged(ev eventFact) {
	pl := ev.payload
	if stringValue(pl["status"]) != "completed" {
		return
	}
	refs := ev.refs
	projectID := refs.ProjectID
	taskID := refs.TaskID
	if projectID == "" {
		projectID = stringValue(pl["project_id"])
	}
	if taskID == "" {
		taskID = stringValue(pl["task_id"])
	}
	if projectID == "" || taskID == "" {
		return
	}
	for _, fact := range p.depsForUpstream(projectID, taskID) {
		source := stringValue(pl["assignee"])
		if !strings.HasPrefix(source, "agent:") && strings.HasPrefix(ev.actor, "agent:") {
			source = ev.actor
		}
		effect := Effect{
			ProjectID:        projectID,
			TargetTaskID:     fact.downstreamTaskID,
			SourceAgentRef:   source,
			RelationType:     "dependency_release",
			Polarity:         "positive",
			Magnitude:        3,
			Confidence:       "high",
			OccurredAt:       ev.occurredAt,
			RuleVersion:      RuleVersionMVP,
			EvidenceEventIDs: []string{fact.eventID, ev.id},
			BeforeState:      map[string]any{"upstream_task_id": taskID, "task_status": stringValue(pl["prev_status"])},
			AfterState:       map[string]any{"released_task_id": fact.downstreamTaskID, "task_status": "completed"},
			ExplanationKey:   "collaboration.effect.dependency_release",
		}
		effect.EffectID = deterministicEffectID(effect)
		if _, exists := p.effects[effect.EffectID]; exists {
			continue
		}
		p.effects[effect.EffectID] = effect
		p.effectOrder = append(p.effectOrder, effect.EffectID)
	}
}

type eventFact struct {
	id         string
	occurredAt time.Time
	eventType  string
	refs       observability.EventRefs
	actor      string
	payload    map[string]any
}

func (p *Projector) depsForUpstream(projectID, upstreamTaskID string) []dependencyFact {
	var out []dependencyFact
	for key, facts := range p.deps {
		if key.projectID == projectID && key.upstreamTaskID == upstreamTaskID {
			out = append(out, facts...)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].occurredAt.Equal(out[j].occurredAt) {
			return out[i].eventID < out[j].eventID
		}
		return out[i].occurredAt.Before(out[j].occurredAt)
	})
	return out
}

type dependencyKey struct {
	projectID      string
	planID         string
	upstreamTaskID string
}

type dependencyFact struct {
	eventID          string
	occurredAt       time.Time
	downstreamTaskID string
	upstreamTaskID   string
}

func deterministicEffectID(e Effect) string {
	parts := []string{e.RuleVersion, e.ProjectID, e.TargetTaskID, e.RelationType}
	parts = append(parts, e.EvidenceEventIDs...)
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "ce_" + hex.EncodeToString(sum[:16])
}

func stringValue(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	default:
		return ""
	}
}

func refsFromOutbox(raw string) observability.EventRefs {
	if raw == "" || raw == "{}" {
		return observability.EventRefs{}
	}
	var refs observability.EventRefs
	_ = json.Unmarshal([]byte(raw), &refs)
	return refs
}

func cloneEffect(in Effect) Effect {
	out := in
	out.EvidenceEventIDs = append([]string(nil), in.EvidenceEventIDs...)
	out.BeforeState = cloneMap(in.BeforeState)
	out.AfterState = cloneMap(in.AfterState)
	return out
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
