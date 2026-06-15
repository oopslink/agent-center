// boardDrop (v2.10.1 M5) — pure drop-decision logic shared by the Work Board's
// touch long-press drag. Given the dragged task's source (a plan id, or null for
// the Backlog) and the drop target column, decide which planning mutation to run.
// This mirrors the HTML5-DnD handlers already in ProjectPlans.tsx (BacklogColumn
// REMOVE, PlanColumn SELECT/MOVE, BuiltinPool SELECT) so the touch path and the
// pointer path stay consistent — kept pure so it's unit-testable without the DOM.

export type DropTarget =
  | { kind: 'backlog' }
  | { kind: 'plan' | 'pool'; planId: string };

export type DropDecision =
  | { op: 'noop' }
  | { op: 'remove'; fromPlanId: string }
  | { op: 'select'; toPlanId: string }
  | { op: 'move'; fromPlanId: string; toPlanId: string };

// decideDrop — what to do when `taskId` (from `fromPlanId`, null ⟺ Backlog) is
// dropped on `target`. Validity (draft-only plans, pool accepts backlog-only,
// backlog accepts plan-tasks-only) is enforced UPSTREAM by the columns'
// `data-droppable` attribute, which the touch hit-test already filters on — so
// here we only translate a *valid* (source, target) pair into a mutation.
export function decideDrop(fromPlanId: string | null, target: DropTarget): DropDecision {
  if (target.kind === 'backlog') {
    // Only a plan-task returns to the Backlog (REMOVE). A backlog card → no-op.
    return fromPlanId === null ? { op: 'noop' } : { op: 'remove', fromPlanId };
  }
  // target is a plan or the assignment pool.
  const toPlanId = target.planId;
  if (fromPlanId === toPlanId) return { op: 'noop' }; // dropped onto its own column.
  if (fromPlanId === null) return { op: 'select', toPlanId }; // Backlog → plan/pool.
  return { op: 'move', fromPlanId, toPlanId }; // plan → another plan.
}

// resolveTargetFromPoint — hit-test the column under a screen point during a touch
// drag. Reads the columns' existing data-attributes; only columns currently marked
// `data-droppable="true"` (computed from the in-flight dragSource) are accepted, so
// invalid targets (a running plan, the pool for a plan-task, …) are filtered for
// free. Returns null when there is no valid column under the finger.
export function resolveTargetFromPoint(x: number, y: number): DropTarget | null {
  if (typeof document === 'undefined' || typeof document.elementFromPoint !== 'function') return null;
  const el = document.elementFromPoint(x, y) as HTMLElement | null;
  if (!el) return null;
  const col = el.closest('[data-droppable="true"]') as HTMLElement | null;
  if (!col) return null;
  if (col.getAttribute('data-testid') === 'backlog-column') return { kind: 'backlog' };
  const planId = col.getAttribute('data-plan-id');
  if (!planId) return null;
  return { kind: col.getAttribute('data-builtin') === 'true' ? 'pool' : 'plan', planId };
}
