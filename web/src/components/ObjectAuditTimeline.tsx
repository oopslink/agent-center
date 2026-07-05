import type React from 'react';
import { useObjectAudit, type AuditEntry, type AuditObjectType } from '@/api/audit';

// ObjectAuditTimeline (变更记录 / audit-trail — change-log design §7). Renders an
// object's semantic change ledger as a human-readable timeline: a left time rail +
// a category-colored dot + a colored label + a "人话" sentence composed from the
// entry's STRUCTURED fields (the backend ships structure, not prose). Reuses the
// AgentActivityRow visual idiom (rail + dot + label) without its activity-event
// coupling. Newest-first, matching the API's ordering.

interface Cat {
  label: string;
  cls: string; // label text color
  dot: string; // rail bullet color
}

// category maps a change_type to its display label + color. Grouped by intent:
// creation (brand), status (blue), ownership (teal), dependency/structure (violet),
// gate/decision (orange), metadata (muted).
function category(changeType: string): Cat {
  switch (changeType) {
    case 'created':
      return { label: 'Created', cls: 'text-brand', dot: 'bg-brand' };
    case 'status_changed':
      return { label: 'Status', cls: 'text-status-blue-fg', dot: 'bg-status-blue-solid' };
    case 'assigned':
    case 'claimed':
    case 'auto_assigned':
      return { label: 'Assigned', cls: 'text-status-teal-fg', dot: 'bg-status-teal-solid' };
    case 'reassigned':
      return { label: 'Reassigned', cls: 'text-status-teal-fg', dot: 'bg-status-teal-solid' };
    case 'unassigned':
      return { label: 'Unassigned', cls: 'text-text-muted', dot: 'bg-text-muted' };
    case 'dependency_added':
    case 'dependency_removed':
    case 'node_added':
    case 'node_removed':
      return { label: 'Structure', cls: 'text-status-violet-fg', dot: 'bg-status-violet-solid' };
    case 'started':
    case 'stopped':
    case 'loopback':
    case 'decision_outcome':
      return { label: 'Plan', cls: 'text-status-blue-fg', dot: 'bg-status-blue-solid' };
    case 'review_verdict':
      return { label: 'Review', cls: 'text-status-orange-strong', dot: 'bg-status-orange-solid' };
    case 'metadata_edited':
      return { label: 'Edited', cls: 'text-text-muted', dot: 'bg-text-muted' };
    case 'auto_closed':
      return { label: 'Auto-closed', cls: 'text-status-orange-strong', dot: 'bg-status-orange-solid' };
    default:
      return { label: changeType, cls: 'text-text-secondary', dot: 'bg-text-secondary' };
  }
}

// actorName trims the ADR-0033 identity scheme prefix for display. A system actor
// ("system:<reconciler>") renders as "system (<reconciler>)"; a user/agent renders
// as its bare id.
function actorName(actor: string): string {
  if (actor === 'system' || actor.startsWith('system:')) {
    const r = actor.slice('system:'.length);
    return r ? `system (${r})` : 'system';
  }
  const i = actor.indexOf(':');
  return i >= 0 ? actor.slice(i + 1) : actor;
}

function detailStr(detail: Record<string, unknown>, key: string): string {
  const v = detail?.[key];
  return v == null ? '' : String(v);
}

// sentence composes the human-readable description from the structured entry. It is
// deliberately terse and free of raw entity ids where a value already reads well.
function sentence(e: AuditEntry): string {
  const d = e.detail ?? {};
  switch (e.change_type) {
    case 'created':
      return e.object_type === 'plan' ? `created plan "${detailStr(d, 'name')}"` : 'created';
    case 'status_changed':
      return `changed status ${e.from || '—'} → ${e.to || '—'}`;
    case 'assigned':
      return `assigned to ${actorName(e.to)}`;
    case 'reassigned':
      return `reassigned ${actorName(e.from)} → ${actorName(e.to)}`;
    case 'unassigned':
      return `unassigned ${actorName(e.from)}`;
    case 'claimed':
      return `claimed the task`;
    case 'auto_assigned':
      return `auto-assigned to ${actorName(e.to)}`;
    case 'review_verdict':
      return `gate verdict ${e.to}${detailStr(d, 'round') ? ` (round ${detailStr(d, 'round')})` : ''}`;
    case 'metadata_edited': {
      const fields = Array.isArray(d.fields) ? (d.fields as string[]).join(', ') : '';
      return fields ? `edited ${fields}` : 'edited metadata';
    }
    case 'auto_closed':
      return `auto-closed ${e.from || ''} → ${e.to || 'closed'}`.trim();
    case 'started':
      return 'started the plan';
    case 'stopped':
      return 'stopped the plan';
    case 'dependency_added':
      return `added dependency ${detailStr(d, 'from')} → ${detailStr(d, 'to')}${detailStr(d, 'kind') && detailStr(d, 'kind') !== 'seq' ? ` (${detailStr(d, 'kind')})` : ''}`;
    case 'dependency_removed':
      return `removed dependency ${detailStr(d, 'from')} → ${detailStr(d, 'to')}`;
    case 'node_added':
      return `added task ${detailStr(d, 'task_title') || detailStr(d, 'task')} to the plan`;
    case 'node_removed':
      return `removed task ${detailStr(d, 'task_title') || detailStr(d, 'task')} from the plan`;
    case 'decision_outcome':
      return `decision outcome ${e.to || detailStr(d, 'outcome')}`;
    case 'loopback':
      return `loopback reopened ${detailStr(d, 'nodes') || 'upstream nodes'}`;
    default:
      return e.field ? `${e.field}: ${e.from || '—'} → ${e.to || '—'}` : e.change_type;
  }
}

function formatTime(iso: string): string {
  const dt = new Date(iso);
  if (Number.isNaN(dt.getTime())) return iso;
  return dt.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false });
}

function AuditRow({ entry }: { entry: AuditEntry }): React.ReactElement {
  const cat = category(entry.change_type);
  return (
    <li className="text-xs" data-testid="audit-row" data-change-type={entry.change_type}>
      <div className="flex items-start gap-2 py-1.5">
        <time
          className="w-24 shrink-0 pt-px tabular-nums text-text-muted"
          dateTime={entry.occurred_at}
          title={entry.occurred_at}
        >
          {formatTime(entry.occurred_at)}
        </time>
        <span className="relative flex w-3 shrink-0 justify-center pt-1" aria-hidden="true">
          <span className="absolute bottom-[-0.75rem] left-1/2 top-1.5 w-px -translate-x-1/2 bg-border-base" />
          <span className={`relative z-10 h-2 w-2 rounded-full ${cat.dot}`} />
        </span>
        <span className="flex min-w-0 flex-1 flex-wrap items-baseline gap-x-1.5">
          <span
            className={`shrink-0 text-[0.6875rem] font-semibold uppercase tracking-wide ${cat.cls}`}
            data-testid="audit-badge"
          >
            {cat.label}
          </span>
          <span className="min-w-0 text-text-secondary" data-testid="audit-sentence">
            <span className="font-medium text-text-primary">@{actorName(entry.actor)}</span> {sentence(entry)}
          </span>
        </span>
      </div>
    </li>
  );
}

interface Props {
  objectType: AuditObjectType;
  projectId: string | undefined;
  objectId: string | undefined;
  /** heading text; defaults to "Change history". */
  title?: string;
}

// ObjectAuditTimeline is the 变更记录 section rendered in the issue/task/plan detail
// views. Self-contained: it fetches the ledger and renders loading / empty / list.
export function ObjectAuditTimeline({ objectType, projectId, objectId, title = 'Change history' }: Props): React.ReactElement {
  const { data, isLoading, isError } = useObjectAudit(objectType, projectId, objectId);
  const entries = data?.entries ?? [];

  return (
    <section className="space-y-2" data-testid="object-audit-timeline">
      <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">{title}</p>
      {isLoading ? (
        <p className="text-xs text-text-muted" data-testid="audit-loading">
          Loading…
        </p>
      ) : isError ? (
        <p className="text-xs text-danger" data-testid="audit-error">
          Failed to load change history.
        </p>
      ) : entries.length === 0 ? (
        <p className="text-xs text-text-muted" data-testid="audit-empty">
          No changes recorded yet.
        </p>
      ) : (
        <ul data-testid="audit-list">
          {entries.map((e) => (
            <AuditRow key={e.id} entry={e} />
          ))}
        </ul>
      )}
    </section>
  );
}
