import { useQuery } from '@tanstack/react-query';
import { api } from './client';

// 变更记录 / audit-trail (change-log design §6/§7). The read side of the
// object-level semantic change ledger: GET the newest-first, structured change
// entries for an issue / task / plan and render them as a human-readable timeline
// in the detail views. The backend ships STRUCTURED fields — the human sentence is
// composed on the frontend (ObjectAuditTimeline).

export type AuditObjectType = 'issue' | 'task' | 'plan';

// AuditEntry mirrors the backend DTO (handlers_pm_audit.go pmAuditEntryMap). `detail`
// is an already-parsed JSON object (structured extras: dependency kind/when, gate
// round, edited-field list, …); `from`/`to` carry the X→Y diff when applicable.
export interface AuditEntry {
  id: string;
  object_type: AuditObjectType;
  object_id: string;
  change_type: string;
  field: string;
  from: string;
  to: string;
  actor: string;
  detail: Record<string, unknown>;
  occurred_at: string;
}

export interface AuditPage {
  entries: AuditEntry[];
  next_cursor: string;
}

// pathFor builds the project-scoped audit endpoint for an object kind. The three
// object kinds nest under their own detail path (issues/tasks/plans), so the plural
// segment is derived from the object type.
function pathFor(objType: AuditObjectType, projectId: string, objId: string): string {
  const plural = objType === 'issue' ? 'issues' : objType === 'task' ? 'tasks' : 'plans';
  return `/projects/${projectId}/${plural}/${objId}/audit`;
}

// useObjectAudit fetches an object's change ledger (first page). Disabled until both
// ids resolve. Read-only; the ledger only grows, so a modest staleTime avoids
// refetch churn while the detail view is open (mutations that add entries are rare
// relative to renders — the caller can invalidate on a known mutation if needed).
export function useObjectAudit(
  objType: AuditObjectType,
  projectId: string | undefined,
  objId: string | undefined,
) {
  return useQuery({
    queryKey: ['audit', objType, projectId ?? '', objId ?? ''],
    queryFn: () => api.get<AuditPage>(pathFor(objType, projectId as string, objId as string)),
    enabled: !!projectId && !!objId,
  });
}
