package sqlite

import (
	"context"
	"database/sql"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
)

// ActivityEventRepo implements agent.ActivityEventRepository (append-only).
type ActivityEventRepo struct{ db *sql.DB }

// NewActivityEventRepo constructs the repo.
func NewActivityEventRepo(db *sql.DB) *ActivityEventRepo { return &ActivityEventRepo{db: db} }

func (r *ActivityEventRepo) Append(ctx context.Context, e *agent.AgentActivityEvent) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO agent_activity_events (id, agent_id, work_item_ref, interaction_ref, event_type, payload, occurred_at)
		 VALUES (?,?,?,?,?,?,?)`,
		e.ID(), string(e.AgentID()), nullString(e.WorkItemRef()), nullString(e.InteractionRef()),
		e.EventType(), e.Payload(), ts(e.OccurredAt()))
	return err
}

func (r *ActivityEventRepo) ListByAgent(ctx context.Context, agentID agent.AgentID, limit int) ([]*agent.AgentActivityEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		activitySelect+` WHERE agent_id = ? ORDER BY occurred_at DESC, id DESC LIMIT ?`, string(agentID), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivityEvents(rows)
}

func (r *ActivityEventRepo) ListByWorkItem(ctx context.Context, workItemRef string) ([]*agent.AgentActivityEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		activitySelect+` WHERE work_item_ref = ? ORDER BY occurred_at, id`, workItemRef)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivityEvents(rows)
}

const activitySelect = `SELECT id, agent_id, work_item_ref, interaction_ref, event_type, payload, occurred_at FROM agent_activity_events`

func scanActivityEvents(rows *sql.Rows) ([]*agent.AgentActivityEvent, error) {
	var out []*agent.AgentActivityEvent
	for rows.Next() {
		var (
			id, agentID, eventType, payload, occurredAt string
			workItemRef, interactionRef                 sql.NullString
		)
		if err := rows.Scan(&id, &agentID, &workItemRef, &interactionRef, &eventType, &payload, &occurredAt); err != nil {
			return nil, err
		}
		e, err := agent.NewActivityEvent(agent.NewActivityEventInput{
			ID: id, AgentID: agent.AgentID(agentID), WorkItemRef: workItemRef.String,
			InteractionRef: interactionRef.String, EventType: eventType, Payload: payload,
			OccurredAt: parseTime(occurredAt),
		})
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

var _ agent.ActivityEventRepository = (*ActivityEventRepo)(nil)
