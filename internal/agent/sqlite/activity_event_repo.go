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

// ListByAgent returns an agent's activity events newest-first. v2.8 #274 adds
// cursor pagination:
//   - before == "": newest page. before != "": only events older than that
//     event id (the cursor) — `id < before`.
//   - limit > 0: cap at limit. limit <= 0: UNLIMITED (no LIMIT clause) — the
//     explicit admin/debug full-history path (handler resolves the default).
//
// Ordering is `id DESC` — ids are ULIDs minted at append on an append-only table,
// so id order is the stable, monotonic time order. This is what makes the cursor
// gap/dup/reorder-free across pages even when new events arrive mid-pagination
// (a new event has a larger id, lands on the newest page, never shifts an older
// `id < before` window) — the contract invariant the frontend cross-page
// re-group + concurrent-append gates rely on (#274 day-0 lock).
func (r *ActivityEventRepo) ListByAgent(ctx context.Context, agentID agent.AgentID, limit int, before string) ([]*agent.AgentActivityEvent, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	q := activitySelect + ` WHERE agent_id = ?`
	args := []any{string(agentID)}
	if before != "" {
		q += ` AND id < ?`
		args = append(args, before)
	}
	q += ` ORDER BY id DESC`
	if limit > 0 {
		q += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanActivityEvents(rows)
}

// LatestByAgents returns the single most-recent activity event per agent across
// the whole input set in ONE batch query (NO N+1) — the v2.8.1 agents-list enrich
// uses it to render last_activity_at/last_activity_content for the whole page in a
// single round-trip. It wraps `activitySelect` in a ROW_NUMBER() window partitioned
// by agent_id, ordered occurred_at DESC, id DESC (occurred_at is the canonical event
// timestamp + the (agent_id, occurred_at) index ordering; id is the stable ULID
// tiebreaker for same-instant events), and keeps rn = 1. Agents with no events have
// no map entry. Empty input → empty map.
func (r *ActivityEventRepo) LatestByAgents(ctx context.Context, agentIDs []agent.AgentID) (map[agent.AgentID]*agent.AgentActivityEvent, error) {
	out := make(map[agent.AgentID]*agent.AgentActivityEvent, len(agentIDs))
	if len(agentIDs) == 0 {
		return out, nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	placeholders := make([]byte, 0, len(agentIDs)*2)
	args := make([]any, 0, len(agentIDs))
	for i, id := range agentIDs {
		if i > 0 {
			placeholders = append(placeholders, ',')
		}
		placeholders = append(placeholders, '?')
		args = append(args, string(id))
	}
	q := `SELECT ` + activityCols + `
		FROM (SELECT ` + activityCols + `, ROW_NUMBER() OVER (
			PARTITION BY agent_id ORDER BY occurred_at DESC, id DESC
		) AS rn FROM agent_activity_events WHERE agent_id IN (` + string(placeholders) + `)) WHERE rn = 1`
	rows, err := exec.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events, err := scanActivityEvents(rows)
	if err != nil {
		return nil, err
	}
	for _, e := range events {
		out[e.AgentID()] = e
	}
	return out, nil
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

// activityCols is the shared column list (kept in sync with scanActivityEvents).
// activitySelect prepends SELECT + appends the FROM for the simple list queries;
// LatestByAgents reuses activityCols directly inside its window subquery.
const activityCols = `id, agent_id, work_item_ref, interaction_ref, event_type, payload, occurred_at`

const activitySelect = `SELECT ` + activityCols + ` FROM agent_activity_events`

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
