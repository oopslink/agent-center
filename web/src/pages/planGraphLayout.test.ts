import { describe, expect, it } from 'vitest';
import { layoutGraph } from './PlanDetail';
import type { PlanGraphNode, PlanGraphEdge } from '@/api/plans';

// T800 layout algebra unit tests: buildPlanGraph now emits Start→root / sink→End
// edges (single source of truth in Go), and layoutGraph must (1) place Start as the
// inline source and End as the inline sink — robustly, even for older graphs whose
// End has no incoming edge — and (2) give control-only levels a slim marker so a
// condition diamond / Start / End don't float over-wide.
//
// The DAG flows TOP→BOTTOM: each dependency level is a horizontal row stacked
// downward (level → y), siblings spread left→right within a level (row → x).

function biz(id: string): PlanGraphNode {
  return { id, category: 'business', title: id, status: 'open', task_id: `task-${id}` };
}
function ctrl(id: string, kind: 'start' | 'end' | 'condition'): PlanGraphNode {
  return { id, category: 'control', control_kind: kind, title: kind, status: 'open' };
}
const seq = (from: string, to: string): PlanGraphEdge => ({ from, to, kind: 'seq' });

// NODE_H is the fixed card/marker height; the uniform vertical gap between levels is
// LEVEL_GAP (see PlanDetail). Kept in sync with those layout constants.
const NODE_H = 84;

function posOf(nodes: PlanGraphNode[], edges: PlanGraphEdge[]) {
  const { positioned } = layoutGraph(nodes, edges);
  return new Map(positioned.map((p) => [p.node.id, p]));
}

describe('layoutGraph — T800 Start/End terminals + slim control markers (top→bottom)', () => {
  it('places Start as the topmost source and End as the bottommost sink, inline with the flow', () => {
    // start → A → B → end  (fully wired, the post-T800 shape).
    const nodes = [ctrl('s', 'start'), biz('A'), biz('B'), ctrl('e', 'end')];
    const edges = [seq('s', 'A'), seq('A', 'B'), seq('B', 'e')];
    const p = posOf(nodes, edges);
    // Monotonic top→bottom: Start < A < B < End on the y-axis.
    expect(p.get('s')!.y).toBeLessThan(p.get('A')!.y);
    expect(p.get('A')!.y).toBeLessThan(p.get('B')!.y);
    expect(p.get('B')!.y).toBeLessThan(p.get('e')!.y);
    // Start is the global top (min y), End the global bottom (max y).
    const ys = [...p.values()].map((v) => v.y);
    expect(p.get('s')!.y).toBe(Math.min(...ys));
    expect(p.get('e')!.y).toBe(Math.max(...ys));
    expect(p.get('s')!.level).toBe(0);
    expect(p.get('e')!.level).toBe(Math.max(...[...p.values()].map((v) => v.level)));
  });

  it('forces End to the bottommost level even when it has NO incoming edge (pre-T800 graph)', () => {
    // Old graph: End is an orphan (no sink→End edge). Longest-path would give it
    // level 0 (Start's row); the terminal-rank override must still push it to the bottom.
    const nodes = [ctrl('s', 'start'), biz('A'), biz('B'), ctrl('e', 'end')];
    const edges = [seq('s', 'A'), seq('A', 'B')]; // note: no B→end
    const p = posOf(nodes, edges);
    expect(p.get('e')!.level).toBeGreaterThan(p.get('B')!.level);
    expect(p.get('e')!.y).toBe(Math.max(...[...p.values()].map((v) => v.y)));
  });

  it('gives a condition (and Start/End) a slim marker and keeps level gaps uniform', () => {
    // start → A → cond → B → end
    const nodes = [ctrl('s', 'start'), biz('A'), ctrl('c', 'condition'), biz('B'), ctrl('e', 'end')];
    const edges = [seq('s', 'A'), seq('A', 'c'), seq('c', 'B'), seq('B', 'e')];
    const p = posOf(nodes, edges);
    // Control markers are slimmer than business cards.
    expect(p.get('c')!.w).toBeLessThan(p.get('A')!.w);
    expect(p.get('s')!.w).toBeLessThan(p.get('A')!.w);
    expect(p.get('e')!.w).toBeLessThan(p.get('B')!.w);
    // Uniform vertical gap between successive levels (each is a full NODE_H tall, so
    // the inter-level gap is the constant LEVEL_GAP — no over-tall band around a diamond).
    const gapStartA = p.get('A')!.y - (p.get('s')!.y + NODE_H);
    const gapAcond = p.get('c')!.y - (p.get('A')!.y + NODE_H);
    const gapCondB = p.get('B')!.y - (p.get('c')!.y + NODE_H);
    expect(gapAcond).toBe(gapStartA);
    expect(gapCondB).toBe(gapStartA);
  });
});
