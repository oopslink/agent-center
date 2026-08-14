import type React from 'react';
import { formatLocalTime } from '@/utils/time';

export type AccessStatus = 'allowed' | 'denied' | 'unauthorized' | 'not_applicable';
export type AccessRisk = 'low' | 'medium' | 'high';

export interface AccessResourceLike {
  kind: string;
  id?: string;
  org_id?: string;
  project_id?: string;
  label?: string;
  uri?: string;
}

export function accessResourceKey(resource: AccessResourceLike): string {
  return `${resource.kind}:${resource.uri || resource.id || resource.org_id || '*'}`;
}

export function accessResourceLabel(resource: AccessResourceLike): string {
  return resource.label || `${resource.kind}:${resource.uri || resource.id || resource.org_id || '*'}`;
}

export function accessStatusLabel(status: AccessStatus | undefined): string {
  if (status === 'not_applicable') return 'Not applicable';
  if (status === 'unauthorized') return 'No access';
  if (status === 'denied') return 'Denied';
  return 'Allowed';
}

export function accessRiskLabel(risk: AccessRisk | undefined): string {
  if (risk === 'high') return 'High risk';
  if (risk === 'medium') return 'Medium';
  return 'Low';
}

export function displayAccessDate(value?: string | null): string {
  if (!value) return '-';
  return formatLocalTime(value);
}

export function AccessStatusBadge({ status }: { status: AccessStatus }): React.ReactElement {
  const cls = {
    allowed: 'bg-status-emerald-bg text-status-emerald-fg border-status-emerald-border',
    denied: 'bg-status-rose-bg text-status-rose-fg border-status-rose-border',
    unauthorized: 'bg-status-amber-bg text-status-amber-fg border-status-amber-border',
    not_applicable: 'bg-status-slate-bg text-status-slate-fg border-status-slate-border',
  }[status];
  return (
    <span className={`inline-flex rounded border px-2 py-0.5 text-xs font-semibold ${cls}`} data-status={status}>
      {accessStatusLabel(status)}
    </span>
  );
}

export function AccessRiskBadge({ risk }: { risk: AccessRisk }): React.ReactElement {
  const cls = {
    high: 'bg-status-rose-bg text-status-rose-fg border-status-rose-border',
    medium: 'bg-status-amber-bg text-status-amber-fg border-status-amber-border',
    low: 'bg-status-slate-bg text-status-slate-fg border-status-slate-border',
  }[risk];
  return (
    <span className={`inline-flex rounded border px-2 py-0.5 text-xs font-semibold ${cls}`}>
      {accessRiskLabel(risk)}
    </span>
  );
}

export function AccessMetaPill({ children }: { children: React.ReactNode }): React.ReactElement {
  return (
    <span className="inline-flex rounded border border-border-base bg-bg-subtle px-2 py-0.5 text-xs font-semibold text-text-secondary">
      {children}
    </span>
  );
}
