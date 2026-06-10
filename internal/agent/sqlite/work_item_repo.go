package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"github.com/oopslink/agent-center/internal/agent"
	"github.com/oopslink/agent-center/internal/persistence"
)

// WorkItemRepo implements agent.WorkItemRepository.
//
// When a transition sink is wired (NewWorkItemRepoWithSink), Save/Update drain
// the AR's pending transitions and hand them to the sink IN THE SAME ctx/tx as
// the row write (v2.7 #111 locus B: structural no-miss — every status change
// persisted through this chokepoint emits, regardless of caller BC). A nil sink
// keeps the legacy behaviour (persist-only) for fixtures that don't exercise it.
type WorkItemRepo struct {
	db   *sql.DB
	sink agent.WorkItemTransitionSink // optional; nil → no emit
}

// NewWorkItemRepo constructs the repo with no transition sink (persist-only).
func NewWorkItemRepo(db *sql.DB) *WorkItemRepo { return &WorkItemRepo{db: db} }

// NewWorkItemRepoWithSink constructs the repo wired to emit drained transitions
// to sink within the persisting tx. sink may be nil (same as NewWorkItemRepo).
func NewWorkItemRepoWithSink(db *sql.DB, sink agent.WorkItemTransitionSink) *WorkItemRepo {
	return &WorkItemRepo{db: db, sink: sink}
}

// emitTransitions drains w's pending transitions and forwards them to the sink
// in the caller's ctx/tx. Drain ALWAYS runs (clears the AR buffer so a second
// Update never double-emits); the sink call is skipped when nil or empty.
func (r *WorkItemRepo) emitTransitions(ctx context.Context, w *agent.AgentWorkItem) error {
	ts := w.DrainTransitions()
	if r.sink == nil || len(ts) == 0 {
		return nil
	}
	return r.sink.AppendTransitions(ctx, ts)
}

func (r *WorkItemRepo) Save(ctx context.Context, w *agent.AgentWorkItem) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	_, err := exec.ExecContext(ctx,
		`INSERT INTO agent_work_items (id, agent_id, task_ref, status, interactions, created_at, updated_at, version)
		 VALUES (?,?,?,?,?,?,?,?)`,
		w.ID(), string(w.AgentID()), w.TaskRef(), string(w.Status()), w.Interactions(),
		ts(w.CreatedAt()), ts(w.UpdatedAt()), w.Version())
	if err != nil && isUnique(err) {
		return agent.ErrWorkItemExists
	}
	if err != nil {
		return err
	}
	return r.emitTransitions(ctx, w)
}

func (r *WorkItemRepo) Update(ctx context.Context, w *agent.AgentWorkItem) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE agent_work_items SET status=?, interactions=?, updated_at=?, version=? WHERE id=?`,
		string(w.Status()), w.Interactions(), ts(w.UpdatedAt()), w.Version(), w.ID())
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return agent.ErrWorkItemNotFound
	}
	return r.emitTransitions(ctx, w)
}

// UpdateCAS persists the work item with an optimistic-lock (compare-and-set)
// guard: the row is written ONLY if its current version still equals
// expectedVersion (the version the caller loaded before transitioning). v2.8.1
// #278 D PR4 — the agent-write-vs-reconciler-release race guard: if a concurrent
// writer (e.g. the reconciler releasing a stuck item) committed first, the
// version has moved → 0 rows match → ErrWorkItemReassigned (row exists, moved) or
// ErrWorkItemNotFound (row gone). Both the agent-facing race-prone ops and the
// reconciler use this so whichever commits second loses cleanly.
func (r *WorkItemRepo) UpdateCAS(ctx context.Context, w *agent.AgentWorkItem, expectedVersion int) error {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	res, err := exec.ExecContext(ctx,
		`UPDATE agent_work_items SET status=?, interactions=?, updated_at=?, version=? WHERE id=? AND version=?`,
		string(w.Status()), w.Interactions(), ts(w.UpdatedAt()), w.Version(), w.ID(), expectedVersion)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		// Distinguish gone vs moved: a still-present row means the version moved
		// (CAS lost the race) → reassigned; an absent row → not found.
		var one int
		if qerr := exec.QueryRowContext(ctx, `SELECT 1 FROM agent_work_items WHERE id=?`, w.ID()).Scan(&one); errors.Is(qerr, sql.ErrNoRows) {
			return agent.ErrWorkItemNotFound
		}
		return agent.ErrWorkItemReassigned
	}
	return r.emitTransitions(ctx, w)
}

func (r *WorkItemRepo) FindByID(ctx context.Context, id string) (*agent.AgentWorkItem, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx, workItemSelect+` WHERE id = ?`, id)
	w, err := scanWorkItem(row.Scan)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, agent.ErrWorkItemNotFound
	}
	return w, err
}

func (r *WorkItemRepo) ListByAgent(ctx context.Context, agentID agent.AgentID) ([]*agent.AgentWorkItem, error) {
	return r.list(ctx, workItemSelect+` WHERE agent_id = ? ORDER BY created_at, id`, string(agentID))
}

func (r *WorkItemRepo) ListByTask(ctx context.Context, taskRef string) ([]*agent.AgentWorkItem, error) {
	return r.list(ctx, workItemSelect+` WHERE task_ref = ? ORDER BY created_at, id`, taskRef)
}

// ListByStatus returns all work items in the given status, stable-ordered by
// created_at, id (a status index exists from migration 0043; plain WHERE is fine
// at v2.7 scale regardless). Backs the D2-e-iii poll-fallback sweep.
func (r *WorkItemRepo) ListByStatus(ctx context.Context, status agent.WorkItemStatus) ([]*agent.AgentWorkItem, error) {
	return r.list(ctx, workItemSelect+` WHERE status = ? ORDER BY created_at, id`, string(status))
}

func (r *WorkItemRepo) HasActiveWorkItem(ctx context.Context, agentID agent.AgentID) (bool, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	row := exec.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM agent_work_items WHERE agent_id = ? AND status IN ('active','waiting_input')`,
		string(agentID))
	var n int
	if err := row.Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

func (r *WorkItemRepo) list(ctx context.Context, q, arg string) ([]*agent.AgentWorkItem, error) {
	exec, _ := persistence.ExecutorFromCtx(ctx, r.db)
	rows, err := exec.QueryContext(ctx, q, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*agent.AgentWorkItem
	for rows.Next() {
		w, err := scanWorkItem(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

const workItemSelect = `SELECT id, agent_id, task_ref, status, interactions, created_at, updated_at, version FROM agent_work_items`

func scanWorkItem(scan func(...any) error) (*agent.AgentWorkItem, error) {
	var (
		id, agentID, taskRef, status, createdAt, updatedAt string
		interactions, version                              int
	)
	if err := scan(&id, &agentID, &taskRef, &status, &interactions, &createdAt, &updatedAt, &version); err != nil {
		return nil, err
	}
	return agent.RehydrateWorkItem(agent.RehydrateWorkItemInput{
		ID: id, AgentID: agent.AgentID(agentID), TaskRef: taskRef,
		Status: agent.WorkItemStatus(status), Interactions: interactions,
		CreatedAt: parseTime(createdAt), UpdatedAt: parseTime(updatedAt), Version: version,
	})
}

var _ agent.WorkItemRepository = (*WorkItemRepo)(nil)
