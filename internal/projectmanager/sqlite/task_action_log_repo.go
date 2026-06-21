package sqlite

import (
	"context"
	"database/sql"
	"strings"

	"github.com/oopslink/agent-center/internal/idgen"
	"github.com/oopslink/agent-center/internal/persistence"
	pm "github.com/oopslink/agent-center/internal/projectmanager"
)

// TaskActionLogRepo is the SQLite-backed pm.TaskActionLogRepository (v2.14.0 I14
// §7.3). It persists the append-only pm_task_action_logs entries that replace the
// deleted agent_work_item_transitions. IDs are minted here (via idgen) so the
// domain aggregate stays free of an infra id-generation dependency.
type TaskActionLogRepo struct {
	db  *sql.DB
	gen idgen.Generator
}

// NewTaskActionLogRepo constructs the repo. gen mints a ULID for any appended
// entry that arrives without an ID.
func NewTaskActionLogRepo(db *sql.DB, gen idgen.Generator) *TaskActionLogRepo {
	return &TaskActionLogRepo{db: db, gen: gen}
}

// Append inserts each entry for taskID, assigning a fresh ULID to any entry with
// an empty ID. It runs under the caller's ambient tx when one is set (so log
// inserts commit atomically with the owning Task.Update).
func (r *TaskActionLogRepo) Append(ctx context.Context, taskID pm.TaskID, logs []pm.TaskActionLog) error {
	if len(logs) == 0 {
		return nil
	}
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	for _, lg := range logs {
		id := strings.TrimSpace(lg.ID)
		if id == "" {
			id = r.gen.NewULID()
		}
		if _, err := exec.ExecContext(ctx,
			`INSERT INTO pm_task_action_logs (id, task_id, occurred_at, action, actor_ref, agent_ref, note)
			 VALUES (?,?,?,?,?,?,?)`,
			id, string(taskID), ts(lg.OccurredAt), string(lg.Action),
			string(lg.ActorRef), string(lg.AgentRef), lg.Note); err != nil {
			return err
		}
	}
	return nil
}

// ListByTask returns taskID's action log stable-ordered (occurred_at, id). Empty
// (not an error) when the task has no entries.
func (r *TaskActionLogRepo) ListByTask(ctx context.Context, taskID pm.TaskID) ([]pm.TaskActionLog, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx,
		`SELECT id, occurred_at, action, actor_ref, agent_ref, note
		 FROM pm_task_action_logs WHERE task_id = ? ORDER BY occurred_at, id`, string(taskID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []pm.TaskActionLog
	for rows.Next() {
		var id, occurredAt, action, actorRef, agentRef, note string
		if err := rows.Scan(&id, &occurredAt, &action, &actorRef, &agentRef, &note); err != nil {
			return nil, err
		}
		out = append(out, pm.TaskActionLog{
			ID:         id,
			OccurredAt: parseTime(occurredAt),
			Action:     pm.TaskAction(action),
			ActorRef:   pm.IdentityRef(actorRef),
			AgentRef:   pm.IdentityRef(agentRef),
			Note:       note,
		})
	}
	return out, rows.Err()
}

var _ pm.TaskActionLogRepository = (*TaskActionLogRepo)(nil)
