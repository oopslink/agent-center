package collaborationeffect

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/oopslink/agent-center/internal/observability"
	"github.com/oopslink/agent-center/internal/outbox"
)

type Projector struct {
	repo   Repository
	engine Engine
}

func NewProjector(repo Repository, engine Engine) *Projector {
	return &Projector{repo: repo, engine: engine}
}
func (p *Projector) Name() string { return "observability-collaboration-effect" }
func (p *Projector) Project(ctx context.Context, e outbox.Event) error {
	// PM semantic effects use the canonical audit mirror. Its outbox row reuses
	// the exact events.id, so realtime and historical replay produce identical
	// effect ids. Legacy/direct PM outbox events remain for other projectors.
	if e.EventType != "pm.audit_recorded" {
		return nil
	}
	var refs, payload map[string]any
	if err := json.Unmarshal([]byte(e.Refs), &refs); err != nil {
		return p.diagnose(ctx, e.ID, e.CreatedAt, "invalid refs json")
	}
	if err := json.Unmarshal([]byte(e.Payload), &payload); err != nil {
		return p.diagnose(ctx, e.ID, e.CreatedAt, "invalid payload json")
	}
	return p.ProjectFact(ctx, Fact{EventID: e.ID, EventType: e.EventType, OccurredAt: e.CreatedAt, ProjectID: str(refs, "project_id"), TaskID: str(refs, "task_id"), Payload: payload})
}
func (p *Projector) ProjectEvent(ctx context.Context, e *observability.Event) error {
	if e == nil {
		return fmt.Errorf("collaboration effect: nil event")
	}
	r := e.Refs()
	return p.ProjectFact(ctx, Fact{EventID: e.ID().String(), EventType: e.Type().String(), OccurredAt: e.OccurredAt(), ActorRef: e.Actor().String(), ProjectID: r.ProjectID, TaskID: r.TaskID, CorrelationID: e.CorrelationID(), DecisionID: e.DecisionID(), Payload: e.Payload()})
}
func (p *Projector) ProjectFact(ctx context.Context, f Fact) error {
	deps, err := p.repo.DependenciesForUpstream(ctx, p.engine.Version, f.ProjectID, f.TaskID)
	if err != nil {
		return err
	}
	effects, learned, diag := p.engine.Evaluate(f, deps)
	var ds []Diagnostic
	if diag != nil {
		ds = append(ds, *diag)
	}
	return p.repo.Apply(ctx, f, p.engine.Version, effects, learned, ds)
}
func (p *Projector) diagnose(ctx context.Context, id string, at time.Time, reason string) error {
	return p.repo.Apply(ctx, Fact{EventID: id, OccurredAt: at}, p.engine.Version, nil, nil, []Diagnostic{{SourceEventID: id, RuleVersion: p.engine.Version, Reason: reason, OccurredAt: at}})
}

type EventSource interface {
	Find(context.Context, observability.EventQueryFilter) ([]*observability.Event, error)
}
type Rebuilder struct {
	source   EventSource
	repo     Repository
	pageSize int
}

func NewRebuilder(source EventSource, repo Repository) *Rebuilder {
	return &Rebuilder{source: source, repo: repo, pageSize: 500}
}
func (r *Rebuilder) Rebuild(ctx context.Context, version string) error {
	if version == "" {
		return fmt.Errorf("collaboration effect: rebuild version required")
	}
	if err := r.repo.DeleteVersion(ctx, version); err != nil {
		return err
	}
	p := NewProjector(r.repo, NewEngine(version))
	var cursor *observability.EventID
	for {
		events, err := r.source.Find(ctx, observability.EventQueryFilter{Cursor: cursor, Limit: r.pageSize})
		if err != nil {
			return err
		}
		for _, e := range events {
			if err := p.ProjectEvent(ctx, e); err != nil {
				return fmt.Errorf("project %s: %w", e.ID(), err)
			}
		}
		if len(events) < r.pageSize {
			break
		}
		c := events[len(events)-1].ID()
		cursor = &c
	}
	return r.repo.ReplaceVersion(ctx, "", version)
}

var _ outbox.Projector = (*Projector)(nil)
