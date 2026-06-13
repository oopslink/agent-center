// planDagEdit — pure validation for interactive DAG dependency editing
// (v2.9.1 task-ef41ca76, point 3). The drag-to-connect AND keyboard-connect paths
// both use these to REJECT illegal edges in the UI (PD merge-gate: cycle/self are
// blocked at the UI layer — invalid drop forbidden + highlighted — not just via
// the backend's friendly error). Edge semantics match the backend:
// AddPlanDependency(from, to) means "from depends_on to" (from waits for to).

import type { PlanNode } from '@/api/plans';

export type EdgeError = 'self' | 'exists' | 'cycle';

// reachable — can we get from `start` to `target` by following depends_on edges
// (i.e. does `start` transitively depend on `target`)? Cycle-guarded.
function reachable(
  depsById: Map<string, string[]>,
  start: string,
  target: string,
): boolean {
  const seen = new Set<string>();
  const stack = [start];
  while (stack.length > 0) {
    const cur = stack.pop()!;
    if (cur === target) return true;
    if (seen.has(cur)) continue;
    seen.add(cur);
    for (const dep of depsById.get(cur) ?? []) stack.push(dep);
  }
  return false;
}

function depsMap(nodes: PlanNode[]): Map<string, string[]> {
  const present = new Set(nodes.map((n) => n.task_id));
  return new Map(
    nodes.map((n) => [n.task_id, n.depends_on.filter((d) => present.has(d))]),
  );
}

// dependencyEdgeError — why adding "from depends_on to" is illegal, or null if OK.
//   self   — a task can't depend on itself.
//   exists — that dependency already exists.
//   cycle  — `to` already (transitively) depends on `from`, so the new edge would
//            close a cycle.
export function dependencyEdgeError(
  nodes: PlanNode[],
  from: string,
  to: string,
): EdgeError | null {
  if (from === to) return 'self';
  const deps = depsMap(nodes);
  if ((deps.get(from) ?? []).includes(to)) return 'exists';
  // Adding from→to creates a cycle iff `to` can already reach `from`.
  if (reachable(deps, to, from)) return 'cycle';
  return null;
}

// validDropTargets — the set of task_ids that are LEGAL targets for a new edge
// starting at `from` (used to highlight droppable nodes + reject illegal drops in
// the drag path, and to filter the keyboard target list). Excludes self,
// already-linked, and cycle-forming targets.
export function validDropTargets(nodes: PlanNode[], from: string): Set<string> {
  const out = new Set<string>();
  for (const n of nodes) {
    if (dependencyEdgeError(nodes, from, n.task_id) === null) out.add(n.task_id);
  }
  return out;
}
