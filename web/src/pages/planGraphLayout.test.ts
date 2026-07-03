import { describe, expect, it } from 'vitest';
import { layoutGraph } from './PlanDetail';
import type { PlanGraphNode, PlanGraphEdge } from '@/api/plans';

// T800 layout algebra unit tests: buildPlanGraph now emits Start→root / sink→End
// edges (single source of truth in Go), and layoutGraph must (1) place Start as the
// inline source and End as the inline sink — robustly, even for older graphs whose
// End has no incoming edge — and (2) give control-only levels a slim column so a
// condition diamond / Start / End don't float in an over-wide card column.

function biz(id: string): PlanGraphNode {
  return { id, category: 'business', title: id, status: 'open', task_id: `task-${id}` };
}
function ctrl(id: string, kind: 'start' | 'end' | 'condition'): PlanGraphNode {
  return { id, category: 'control', control_kind: kind, title: kind, status: 'open' };
}
const seq = (from: string, to: string): PlanGraphEdge => ({ from, to, kind: 'seq' });

function posOf(nodes: PlanGraphNode[], edges: PlanGraphEdge[]) {
  const { positioned } = layoutGraph(nodes, edges);
  return new Map(positioned.map((p) => [p.node.id, p]));
}

describe('layoutGraph — T800 Start/End terminals + slim control columns', () => {
  it('places Start as the leftmost source and End as the rightmost sink, inline with the flow', () => {
    // start → A → B → end  (fully wired, the post-T800 shape).
    const nodes = [ctrl('s', 'start'), biz('A'), biz('B'), ctrl('e', 'end')];
    const edges = [seq('s', 'A'), seq('A', 'B'), seq('B', 'e')];
    const p = posOf(nodes, edges);
    // Monotonic left→right: Start < A < B < End.
    expect(p.get('s')!.x).toBeLessThan(p.get('A')!.x);
    expect(p.get('A')!.x).toBeLessThan(p.get('B')!.x);
    expect(p.get('B')!.x).toBeLessThan(p.get('e')!.x);
    // Start is the global min, End the global max.
    const xs = [...p.values()].map((v) => v.x);
    expect(p.get('s')!.x).toBe(Math.min(...xs));
    expect(p.get('e')!.x).toBe(Math.max(...xs));
    expect(p.get('s')!.level).toBe(0);
    expect(p.get('e')!.level).toBe(Math.max(...[...p.values()].map((v) => v.level)));
  });

  it('forces End to the rightmost column even when it has NO incoming edge (pre-T800 graph)', () => {
    // Old graph: End is an orphan (no sink→End edge). Longest-path would give it
    // level 0 (Start's column); the terminal-rank override must still push it right.
    const nodes = [ctrl('s', 'start'), biz('A'), biz('B'), ctrl('e', 'end')];
    const edges = [seq('s', 'A'), seq('A', 'B')]; // note: no B→end
    const p = posOf(nodes, edges);
    expect(p.get('e')!.level).toBeGreaterThan(p.get('B')!.level);
    expect(p.get('e')!.x).toBe(Math.max(...[...p.values()].map((v) => v.x)));
  });

  it('gives a condition (and Start/End) a slim column and keeps column gaps uniform', () => {
    // start → A → cond → B → end
    const nodes = [ctrl('s', 'start'), biz('A'), ctrl('c', 'condition'), biz('B'), ctrl('e', 'end')];
    const edges = [seq('s', 'A'), seq('A', 'c'), seq('c', 'B'), seq('B', 'e')];
    const p = posOf(nodes, edges);
    // Control markers are slimmer than business cards.
    expect(p.get('c')!.w).toBeLessThan(p.get('A')!.w);
    expect(p.get('s')!.w).toBeLessThan(p.get('A')!.w);
    expect(p.get('e')!.w).toBeLessThan(p.get('B')!.w);
    // No over-wide column around the condition: the gap Start→A equals the gap
    // A→cond equals cond→B (uniform COL_GAP), so the diamond doesn't reserve extra
    // horizontal space beside its neighbours.
    const gapStartA = p.get('A')!.x - (p.get('s')!.x + p.get('s')!.w);
    const gapAcond = p.get('c')!.x - (p.get('A')!.x + p.get('A')!.w);
    const gapCondB = p.get('B')!.x - (p.get('c')!.x + p.get('c')!.w);
    expect(gapAcond).toBe(gapStartA);
    expect(gapCondB).toBe(gapStartA);
  });
});
