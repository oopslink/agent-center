import { describe, expect, it } from 'vitest';
import { toStuckTasks } from './stuckTasks';
import type { OrgWorkItem } from './types';

function wi(over: Partial<OrgWorkItem> & { id: string }): OrgWorkItem {
  return {
    id: over.id,
    project: over.project ?? { id: 'p1', name: 'Proj One' },
    title: over.title ?? 'task title',
    status: over.status ?? 'running',
    assignee: over.assignee ?? null,
    updated_at: over.updated_at ?? '2026-06-30T00:00:00Z',
    created_at: over.created_at ?? '2026-06-29T00:00:00Z',
    org_ref: over.org_ref,
    blocked_reason: over.blocked_reason,
    blocked_reason_type: over.blocked_reason_type,
  };
}

describe('toStuckTasks', () => {
  it('keeps only running tasks blocked with input_required/obstacle', () => {
    const items: OrgWorkItem[] = [
      wi({ id: 'a', blocked_reason: 'need answer', blocked_reason_type: 'input_required' }),
      wi({ id: 'b', blocked_reason: 'needs a deploy key', blocked_reason_type: 'obstacle' }),
      // not blocked (no reason) — excluded
      wi({ id: 'c', blocked_reason: '', blocked_reason_type: '' }),
      // blocked but not running — excluded (stuck is a running annotation)
      wi({ id: 'd', status: 'open', blocked_reason: 'x', blocked_reason_type: 'input_required' }),
      // reason present but unknown/absent type — excluded (not actionable here)
      wi({ id: 'e', blocked_reason: 'x', blocked_reason_type: '' }),
      // whitespace-only reason — excluded
      wi({ id: 'f', blocked_reason: '   ', blocked_reason_type: 'obstacle' }),
    ];
    const got = toStuckTasks(items).map((t) => t.id);
    expect(got).toEqual(['a', 'b']);
  });

  it('orders input_required before obstacle, newest-updated first within a group', () => {
    const items: OrgWorkItem[] = [
      wi({ id: 'obs-old', blocked_reason: 'r', blocked_reason_type: 'obstacle', updated_at: '2026-06-01T00:00:00Z' }),
      wi({ id: 'inp-old', blocked_reason: 'r', blocked_reason_type: 'input_required', updated_at: '2026-06-02T00:00:00Z' }),
      wi({ id: 'inp-new', blocked_reason: 'r', blocked_reason_type: 'input_required', updated_at: '2026-06-10T00:00:00Z' }),
      wi({ id: 'obs-new', blocked_reason: 'r', blocked_reason_type: 'obstacle', updated_at: '2026-06-09T00:00:00Z' }),
    ];
    expect(toStuckTasks(items).map((t) => t.id)).toEqual(['inp-new', 'inp-old', 'obs-new', 'obs-old']);
  });

  it('projects the fields the rail panel renders (trimmed reason, project name, ref)', () => {
    const got = toStuckTasks([
      wi({
        id: 't9',
        org_ref: 'T9',
        title: 'Wire the thing',
        project: { id: 'p2', name: 'Proj Two' },
        blocked_reason: '  please confirm the schema  ',
        blocked_reason_type: 'input_required',
      }),
    ]);
    expect(got[0]).toEqual({
      id: 't9',
      project_id: 'p2',
      project_name: 'Proj Two',
      org_ref: 'T9',
      title: 'Wire the thing',
      reason: 'please confirm the schema',
      reason_type: 'input_required',
      updated_at: '2026-06-30T00:00:00Z',
    });
  });

  it('returns an empty list for empty input', () => {
    expect(toStuckTasks([])).toEqual([]);
  });
});
