import { describe, expect, it } from 'vitest';
import type { PlanGeneration, PlanGenerationRead, PlanGraphNode } from '@/api/plans';
import { withHistoricalEdges } from './planHistoricalEdges';
import { layoutGraphFlow, layoutLegacyFlow } from './planDagFlow';

const nodes: PlanGraphNode[] = ['dev', 'review', 'ship', 'publish', 'fix'].map((id) => ({
  id: `node-${id}`, task_id: id, title: id, category: 'business', status: 'open',
}));
const seq = (from: string, to: string) => ({ from: `node-${from}`, to: `node-${to}`, kind: 'seq' as const });
const dependency = (from: string, to: string) => ({ from_task_id: to, to_task_id: from, kind: 'seq' });
function generation(id: string, parent: string, edges: PlanGeneration['snapshot']['edges']): PlanGeneration {
  return { id, parent_generation_id: parent, snapshot: { edges } } as PlanGeneration;
}
const read = { generations: [
  generation('r1', '', [dependency('dev', 'review'), dependency('review', 'ship'), dependency('ship', 'publish')]),
  generation('r2', 'r1', [dependency('dev', 'review'), dependency('review', 'ship'), dependency('ship', 'publish'), dependency('dev', 'fix')]),
  generation('r3', 'r2', [dependency('dev', 'review'), dependency('dev', 'fix')]),
  generation('future', 'r3', [dependency('fix', 'publish')]),
] } as PlanGenerationRead;
const current = [seq('dev', 'review'), seq('dev', 'fix')];
const cancelled = new Set(['ship', 'publish']);

describe('historical dependency display', () => {
  it('restores both sides of cancelled nodes from ancestors, once, without future edges or input mutation', () => {
    const before = JSON.stringify({ current, read });
    const edges = withHistoricalEdges(nodes, current, read, 'r3', cancelled);
    expect(edges).toEqual([
      ...current.map((edge) => ({ ...edge, historical: false })),
      { ...seq('review', 'ship'), historical: true },
      { ...seq('ship', 'publish'), historical: true },
    ]);
    expect(JSON.stringify({ current, read })).toBe(before);
    expect(withHistoricalEdges(nodes, current, read, 'r1', new Set())).toEqual(current.map((edge) => ({ ...edge, historical: false })));
  });

  it('does not restore edges to hidden nodes or removed dependencies between effective nodes', () => {
    expect(withHistoricalEdges(nodes.filter((n) => !cancelled.has(n.task_id!)), current, read, 'r3', cancelled)).toHaveLength(2);
    expect(withHistoricalEdges(nodes, [], read, 'r3', new Set())).toEqual([]);
  });

  it('marks existing edges incident to cancelled nodes inactive without duplicates', () => {
    const edge = seq('review', 'ship');
    expect(withHistoricalEdges(nodes, [edge], read, 'r3', cancelled).filter((e) => e.from === edge.from && e.to === edge.to)).toEqual([{ ...edge, historical: true }]);
  });

  it('survives partial/cyclic history and retains edge kinds', () => {
    const partial = { generations: [generation('loop', 'loop', [{ ...dependency('review', 'ship'), kind: 'conditional' }])] } as PlanGenerationRead;
    expect(withHistoricalEdges(nodes, [], partial, 'loop', cancelled)).toEqual([{ ...seq('review', 'ship'), kind: 'conditional', historical: true }]);
    expect(withHistoricalEdges(nodes, current, undefined, undefined, cancelled)).toHaveLength(2);
  });

  it('restores legacy display edges without changing scheduling dependencies', async () => {
    const tasks = nodes.map((node) => ({ task_id: node.task_id!, title: node.title, assignee_ref: '',
      task_status: cancelled.has(node.task_id!) ? 'discarded' : 'open', node_status: 'blocked' as const,
      effective: !cancelled.has(node.task_id!), depends_on: [] as string[] }));
    const layout = await layoutLegacyFlow(tasks, [], read, 'r3');
    expect(layout.edges.filter((edge) => edge.data?.historical && edge.data?.fromTaskId)).toHaveLength(2);
    expect(tasks.every((task) => task.depends_on.length === 0)).toBe(true);
  });

  it('passes historical metadata and distinct styling through the real ELK adapter', async () => {
    const layout = await layoutGraphFlow(nodes, withHistoricalEdges(nodes, current, read, 'r3', cancelled));
    const historical = layout.edges.filter((edge) => edge.data?.historical);
    expect(historical).toHaveLength(2);
    expect(historical.every((edge) => edge.className?.includes('plan-flow-edge--historical') && !edge.animated)).toBe(true);
    expect(layout.nodes).toHaveLength(5);
  });
});
