import { describe, expect, it } from 'vitest';
import type { PlanGeneration } from '@/api/plans';
import { generationStyle, snapshotPlanNodes } from './PlanDetail';

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
