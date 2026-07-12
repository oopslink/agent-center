import { describe, expect, it } from 'vitest';
import { layoutGraph, layoutStagedGraph } from './PlanDetail';
import type { PlanGraphNode, PlanGraphEdge, PlanStage } from '@/api/plans';

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

// ── T981 follow-up: layoutStagedGraph (outer stage DAG + inner sub-DAG) ─────

function stage(over: Partial<PlanStage> & { id: string }): PlanStage {
  return {
    name: over.id,
    status: 'open',
    rounds: 0,
    max_rounds: 3,
    depends_on_stages: [],
    gate_node_id: '',
    members: [],
    ...over,
  };
}
function member(taskId: string): PlanStage['members'][number] {
  return { task_id: taskId, title: taskId, task_status: 'open' };
}

describe('layoutStagedGraph — outer stage DAG + inner sub-DAG (T981 follow-up)', () => {
  it('degrades to the flat layout, unchanged, when the plan has no stages (§8 zero-regression)', () => {
    const nodes = [ctrl('s', 'start'), biz('A'), ctrl('e', 'end')];
    const edges = [seq('s', 'A'), seq('A', 'e')];
    const flat = layoutGraph(nodes, edges);
    const staged = layoutStagedGraph(nodes, edges, []);
    expect(staged.boxes).toEqual([]);
    expect(staged.width).toBe(flat.width);
    expect(staged.height).toBe(flat.height);
    expect(staged.positioned).toEqual(flat.positioned);
  });

  it('places independent stages (no depends_on_stages between them) side by side in the same row', () => {
    const nodes = [biz('A'), biz('B')];
    const stages = [stage({ id: 's1', members: [member('task-A')] }), stage({ id: 's2', members: [member('task-B')] })];
    const { boxes } = layoutStagedGraph(nodes, [], stages);
    const b1 = boxes.find((b) => b.stage.id === 's1')!;
    const b2 = boxes.find((b) => b.stage.id === 's2')!;
    expect(b1.y).toBe(b2.y); // same row
    // Side by side, not overlapping horizontally.
    expect(Math.abs(b1.x - b2.x)).toBeGreaterThanOrEqual(Math.min(b1.w, b2.w));
  });

  it('stacks a dependent stage into the row below both of its upstreams', () => {
    const nodes = [biz('A'), biz('B'), biz('C')];
    const stages = [
      stage({ id: 's1', members: [member('task-A')] }),
      stage({ id: 's2', members: [member('task-B')] }),
      stage({ id: 's3', members: [member('task-C')], depends_on_stages: ['s1', 's2'] }),
    ];
    const { boxes } = layoutStagedGraph(nodes, [], stages);
    const b1 = boxes.find((b) => b.stage.id === 's1')!;
    const b2 = boxes.find((b) => b.stage.id === 's2')!;
    const b3 = boxes.find((b) => b.stage.id === 's3')!;
    expect(b3.y).toBeGreaterThan(b1.y + b1.h);
    expect(b3.y).toBeGreaterThan(b2.y + b2.h);
  });

  it('positions a business node inside its own stage box bounds', () => {
    const nodes = [biz('A'), biz('B')];
    const stages = [stage({ id: 's1', members: [member('task-A'), member('task-B')] })];
    const { positioned, boxes } = layoutStagedGraph(nodes, [seq('A', 'B')], stages);
    const box = boxes[0];
    for (const id of ['A', 'B']) {
      const p = positioned.find((p) => p.node.id === id)!;
      expect(p.x).toBeGreaterThanOrEqual(box.x);
      expect(p.x + p.w).toBeLessThanOrEqual(box.x + box.w + 1); // +1 slack for rounding
      expect(p.y).toBeGreaterThanOrEqual(box.y);
      expect(p.y + NODE_H).toBeLessThanOrEqual(box.y + box.h + 1);
    }
  });

  it('places a stage gate node between its own box and the next row, not inside any box', () => {
    const nodes = [biz('A'), biz('B'), ctrl('gate1', 'condition')];
    const stages = [
      stage({ id: 's1', members: [member('task-A')], gate_node_id: 'gate1' }),
      stage({ id: 's2', members: [member('task-B')], depends_on_stages: ['s1'] }),
    ];
    const { positioned, boxes } = layoutStagedGraph(nodes, [], stages);
    const b1 = boxes.find((b) => b.stage.id === 's1')!;
    const b2 = boxes.find((b) => b.stage.id === 's2')!;
    const gate = positioned.find((p) => p.node.id === 'gate1')!;
    expect(gate.y).toBeGreaterThanOrEqual(b1.y + b1.h);
    expect(gate.y + NODE_H).toBeLessThanOrEqual(b2.y);
    // Roughly centered under its OWNING stage box, not the downstream one.
    expect(gate.x + gate.w / 2).toBeCloseTo(b1.x + b1.w / 2, 0);
  });

  it('keeps Start above every stage box and End below every stage box', () => {
    const nodes = [ctrl('s', 'start'), biz('A'), ctrl('e', 'end')];
    const stages = [stage({ id: 's1', members: [member('task-A')] })];
    const edges = [seq('s', 'A'), seq('A', 'e')];
    const { positioned, boxes } = layoutStagedGraph(nodes, edges, stages);
    const start = positioned.find((p) => p.node.id === 's')!;
    const end = positioned.find((p) => p.node.id === 'e')!;
    const box = boxes[0];
    expect(start.y).toBeLessThan(box.y);
    expect(end.y).toBeGreaterThan(box.y + box.h);
  });

  it('does not silently drop a business node with no matching stage member (defensive fallback)', () => {
    const nodes = [biz('A'), biz('Orphan')];
    const stages = [stage({ id: 's1', members: [member('task-A')] })];
    const { positioned } = layoutStagedGraph(nodes, [], stages);
    expect(positioned.some((p) => p.node.id === 'Orphan')).toBe(true);
  });
});
