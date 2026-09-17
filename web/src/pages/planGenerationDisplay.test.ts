import { layoutGraphFlow } from './planDagFlow';
import type { HistoricalGraphEdge } from './planHistoricalEdges';
import { describe, expect, it } from 'vitest';
import type { PlanGeneration } from '@/api/plans';
import { generationStyle, snapshotPlanNodes, snapshotPlanGraph } from './PlanDetail';

describe('historical generation display', () => {
  it('preserves terminal states ahead of dispatch history and readiness inference', () => {
    const statuses = ['completed', 'discarded', 'failed', 'running', 'paused', 'blocked', 'open'];
    const generation = { snapshot: {
      tasks: statuses.map((status) => ({ task_id: status, title: status, status })),
      edges: [],
      dispatch_records: statuses.map((task_id) => ({ task_id })),
    } } as unknown as PlanGeneration;
    const nodes = snapshotPlanNodes(generation);
    expect(nodes.map((node) => node.node_status)).toEqual(['done', 'discarded', 'failed', 'running', 'paused', 'blocked', 'dispatched']);
    expect(nodes.find((node) => node.task_id === 'discarded')?.effective).toBe(false);
    expect(nodes.find((node) => node.task_id === 'failed')?.task_status).toBe('failed');
  });

  it('keeps open tasks blocked on a failed predecessor and handles empty history', () => {
    const generation = { snapshot: {
      tasks: [{ task_id: 'a', status: 'failed' }, { task_id: 'b', status: 'open' }, { task_id: 'c', status: 'open' }],
      edges: [{ from_task_id: 'b', to_task_id: 'a' }], dispatch_records: [],
    } } as unknown as PlanGeneration;
    expect(snapshotPlanNodes(generation).map((node) => node.node_status)).toEqual(['failed', 'blocked', 'ready']);
    expect(snapshotPlanNodes(undefined)).toEqual([]);
  });

  it('assigns stable distinct generation hues independently of status and selection', () => {
    const palette = Array.from({ length: 9 }, (_, revision) => generationStyle(revision));
    expect(new Set(palette.map((style) => JSON.stringify(style))).size).toBe(9);
    expect(generationStyle(7)).toEqual(palette[7]);
    expect(generationStyle(undefined)).toBeUndefined();
    expect(generationStyle(Number.NaN)).toBeUndefined();
  });
});

describe('historical flow boundaries', () => {
  it('creates display-only anchors and connects active roots/leaves despite cancelled historical edges', async () => {
    const generation = { snapshot: { tasks: [
      { task_id: 'review', title: 'Review', status: 'failed' },
      { task_id: 'ship', title: 'Ship', status: 'discarded' },
      { task_id: 'fix', title: 'Fix', status: 'open' },
    ], edges: [], dispatch_records: [] } } as unknown as PlanGeneration;
    const original = JSON.stringify(generation);
    const graph = snapshotPlanGraph(generation)!;
    const layout = await layoutGraphFlow(graph.nodes, [{ from: 'snapshot:review', to: 'snapshot:ship', kind: 'seq', historical: true } as HistoricalGraphEdge]);
    const pairs = layout.edges.map((edge) => `${edge.source}->${edge.target}`);
    expect(pairs).toContain('__snapshot_start__->snapshot:review');
    expect(pairs).toContain('__snapshot_start__->snapshot:fix');
    expect(pairs).toContain('snapshot:review->__snapshot_end__');
    expect(pairs).toContain('snapshot:fix->__snapshot_end__');
    expect(pairs).not.toContain('snapshot:ship->__snapshot_end__');
    expect(pairs).not.toContain('__snapshot_start__->snapshot:ship');
    expect(layout.nodes.filter((node) => node.data.kind === 'business')).toHaveLength(3);
    expect(JSON.stringify(generation)).toBe(original);
  });

  it('handles empty and fully cancelled snapshots without creating execution tasks', async () => {
    const generation = { snapshot: { tasks: [], edges: [], dispatch_records: [] } } as unknown as PlanGeneration;
    expect(snapshotPlanGraph(generation)?.nodes).toEqual([]);
    expect(snapshotPlanGraph(undefined)).toBeUndefined();
    generation.snapshot.tasks = [{ task_id: 'cancelled', title: 'Cancelled', status: 'discarded' }];
    const graph = snapshotPlanGraph(generation)!;
    const layout = await layoutGraphFlow(graph.nodes, graph.edges);
    expect(layout.edges.map((edge) => [edge.source, edge.target])).toEqual([['__snapshot_start__', '__snapshot_end__']]);
    expect(snapshotPlanNodes(generation)).toHaveLength(1);
  });
});
