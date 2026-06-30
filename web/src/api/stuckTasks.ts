import { useMemo } from 'react';
import { useOrgWorkItems } from './orgWorkItems';
import type { BlockReasonType, OrgWorkItem } from './types';

// Global "stuck" alerts (the rail Alerts item). A task is STUCK when it is
// RUNNING and carries a non-empty blocked_reason whose blocked_reason_type is one
// the user can act on: input_required (an agent needs the user's reply — most
// urgent) or obstacle (an external blocker needs owner/PM intervention). ADR-0046:
// "blocked" is no longer a status, it's an annotation on a running task — so the
// source list is the org-scoped running tasks, filtered client-side.

export type StuckReasonType = Exclude<BlockReasonType, ''>;

export interface StuckTask {
  id: string;
  project_id: string;
  project_name: string;
  org_ref?: string;
  title: string;
  reason: string;
  reason_type: StuckReasonType;
  updated_at: string;
}

// input_required ranks above obstacle (the user is the only one who can unblock it).
const REASON_RANK: Record<StuckReasonType, number> = { input_required: 0, obstacle: 1 };

function isActionableStuck(it: OrgWorkItem): boolean {
  return (
    it.status === 'running' &&
    typeof it.blocked_reason === 'string' &&
    it.blocked_reason.trim() !== '' &&
    (it.blocked_reason_type === 'input_required' || it.blocked_reason_type === 'obstacle')
  );
}

// toStuckTasks — pure projection (unit-tested): filter to actionable stuck tasks,
// then sort input_required first, newest-updated first within each group.
export function toStuckTasks(items: readonly OrgWorkItem[]): StuckTask[] {
  return items
    .filter(isActionableStuck)
    .map((it) => ({
      id: it.id,
      project_id: it.project.id,
      project_name: it.project.name,
      org_ref: it.org_ref,
      title: it.title,
      reason: (it.blocked_reason ?? '').trim(),
      reason_type: it.blocked_reason_type as StuckReasonType,
      updated_at: it.updated_at,
    }))
    .sort((a, b) => {
      const r = REASON_RANK[a.reason_type] - REASON_RANK[b.reason_type];
      if (r !== 0) return r;
      // newest first within a group (RFC3339Nano sorts lexicographically == chronologically)
      return a.updated_at < b.updated_at ? 1 : a.updated_at > b.updated_at ? -1 : 0;
    });
}

// useStuckTasks — the global Alerts data source. Scoped to the current org (the
// rail is org-scoped); disabled when no org is in context. Reuses the org tasks
// aggregation cache (qk.orgTasks), so the SSE invalidations on
// pm.task.state_changed / input_requested / input_replied / assigned keep it live.
export function useStuckTasks(slug: string | undefined) {
  const q = useOrgWorkItems('task', slug, { status: ['running'] });
  const tasks = useMemo(() => toStuckTasks(q.data?.items ?? []), [q.data]);
  return { ...q, tasks };
}
