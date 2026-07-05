import type React from 'react';
import { useTranslation } from 'react-i18next';
import type { TFunction } from 'i18next';
import { useObjectAudit, type AuditEntry, type AuditObjectType } from '@/api/audit';

// ObjectAuditTimeline (变更记录 / audit-trail — change-log design §7). Renders an
// object's semantic change ledger as a human-readable timeline: a left time rail +
// a category-colored dot + a colored label + a "人话" sentence composed from the
// entry's STRUCTURED fields (the backend ships structure, not prose). All copy is
// localized via the `work` namespace (H-1: no hardcoded English). Reuses the
// AgentActivityRow visual idiom (rail + dot + label). Newest-first, matching the API.

interface Cat {
  labelKey: string; // i18n key under audit.category
  cls: string; // label text color
  dot: string; // rail bullet color
}

// category maps a change_type to its display label key + color. Grouped by intent:
// creation (brand), status (blue), ownership (teal), dependency/structure (violet),
// gate/decision (orange), metadata (muted).
function category(changeType: string): Cat {
  switch (changeType) {
    case 'created':
      return { labelKey: 'created', cls: 'text-brand', dot: 'bg-brand' };
    case 'status_changed':
      return { labelKey: 'status', cls: 'text-status-blue-fg', dot: 'bg-status-blue-solid' };
    case 'assigned':
    case 'claimed':
    case 'auto_assigned':
      return { labelKey: 'assigned', cls: 'text-status-teal-fg', dot: 'bg-status-teal-solid' };
    case 'reassigned':
      return { labelKey: 'reassigned', cls: 'text-status-teal-fg', dot: 'bg-status-teal-solid' };
    case 'unassigned':
      return { labelKey: 'unassigned', cls: 'text-text-muted', dot: 'bg-text-muted' };
    case 'dependency_added':
    case 'dependency_removed':
    case 'node_added':
    case 'node_removed':
      return { labelKey: 'structure', cls: 'text-status-violet-fg', dot: 'bg-status-violet-solid' };
    case 'started':
    case 'stopped':
    case 'loopback':
    case 'decision_outcome':
      return { labelKey: 'plan', cls: 'text-status-blue-fg', dot: 'bg-status-blue-solid' };
    case 'review_verdict':
      return { labelKey: 'review', cls: 'text-status-orange-strong', dot: 'bg-status-orange-solid' };
    case 'metadata_edited':
      return { labelKey: 'edited', cls: 'text-text-muted', dot: 'bg-text-muted' };
    case 'auto_closed':
      return { labelKey: 'autoClosed', cls: 'text-status-orange-strong', dot: 'bg-status-orange-solid' };
    default:
      return { labelKey: '', cls: 'text-text-secondary', dot: 'bg-text-secondary' };
  }
}

// categoryLabel resolves the localized badge label; the default branch (unknown
// change_type) falls back to the raw change_type so nothing renders blank.
function categoryLabel(t: TFunction, changeType: string, cat: Cat): string {
  return cat.labelKey ? t(`audit.category.${cat.labelKey}`) : changeType;
}

// actorName trims the ADR-0033 identity scheme prefix for display. A system actor
// ("system:<reconciler>") renders via localized copy; a user/agent renders as its
// bare id.
function actorName(t: TFunction, actor: string): string {
  if (actor === 'system' || actor.startsWith('system:')) {
    const r = actor.slice('system:'.length);
    return r ? t('audit.actorSystemNamed', { reconciler: r }) : t('audit.actorSystem');
  }
  const i = actor.indexOf(':');
  return i >= 0 ? actor.slice(i + 1) : actor;
}

function detailStr(detail: Record<string, unknown>, key: string): string {
  const v = detail?.[key];
  return v == null ? '' : String(v);
}

// EMPTY is the em-dash placeholder shown where a from/to value is absent.
const EMPTY = '—';

// sentence composes the localized human-readable description from the structured
// entry. It is deliberately terse and free of raw entity ids where a value already
// reads well.
function sentence(t: TFunction, e: AuditEntry): string {
  const d = e.detail ?? {};
  switch (e.change_type) {
    case 'created':
      return e.object_type === 'plan'
        ? t('audit.sentence.createdPlan', { name: detailStr(d, 'name') })
        : t('audit.sentence.created');
    case 'status_changed':
      return t('audit.sentence.statusChanged', { from: e.from || EMPTY, to: e.to || EMPTY });
    case 'assigned':
      return t('audit.sentence.assigned', { to: actorName(t, e.to) });
    case 'reassigned':
      return t('audit.sentence.reassigned', { from: actorName(t, e.from), to: actorName(t, e.to) });
    case 'unassigned':
      return t('audit.sentence.unassigned', { from: actorName(t, e.from) });
    case 'claimed':
      return t('audit.sentence.claimed');
    case 'auto_assigned':
      return t('audit.sentence.autoAssigned', { to: actorName(t, e.to) });
    case 'review_verdict': {
      const round = detailStr(d, 'round');
      return round
        ? t('audit.sentence.reviewVerdictRound', { verdict: e.to, round })
        : t('audit.sentence.reviewVerdict', { verdict: e.to });
    }
    case 'metadata_edited': {
      const fields = Array.isArray(d.fields) ? (d.fields as string[]).join(', ') : '';
      return fields ? t('audit.sentence.editedFields', { fields }) : t('audit.sentence.editedMetadata');
    }
    case 'auto_closed':
      return t('audit.sentence.autoClosed', { from: e.from || EMPTY, to: e.to || 'closed' });
    case 'started':
      return t('audit.sentence.started');
    case 'stopped':
      return t('audit.sentence.stopped');
    case 'dependency_added': {
      const kind = detailStr(d, 'kind');
      const args = { from: detailStr(d, 'from') || EMPTY, to: detailStr(d, 'to') || EMPTY, kind };
      return kind && kind !== 'seq'
        ? t('audit.sentence.dependencyAddedKind', args)
        : t('audit.sentence.dependencyAdded', args);
    }
    case 'dependency_removed':
      return t('audit.sentence.dependencyRemoved', {
        from: detailStr(d, 'from') || EMPTY,
        to: detailStr(d, 'to') || EMPTY,
      });
    case 'node_added':
      return t('audit.sentence.nodeAdded', { task: detailStr(d, 'task_title') || detailStr(d, 'task') });
    case 'node_removed':
      return t('audit.sentence.nodeRemoved', { task: detailStr(d, 'task_title') || detailStr(d, 'task') });
    case 'decision_outcome': {
      const outcome = e.to || detailStr(d, 'outcome') || EMPTY;
      return d.exhausted
        ? t('audit.sentence.decisionOutcomeExhausted', { outcome })
        : t('audit.sentence.decisionOutcome', { outcome });
    }
    case 'loopback':
      return t('audit.sentence.loopback', { round: detailStr(d, 'round') || '—' });
    default:
      return e.field
        ? t('audit.sentence.fieldChange', { field: e.field, from: e.from || EMPTY, to: e.to || EMPTY })
        : e.change_type;
  }
}

function formatTime(iso: string): string {
  const dt = new Date(iso);
  if (Number.isNaN(dt.getTime())) return iso;
  return dt.toLocaleString([], { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false });
}

function AuditRow({ entry }: { entry: AuditEntry }): React.ReactElement {
  const { t } = useTranslation('work');
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
            {categoryLabel(t, entry.change_type, cat)}
          </span>
          <span className="min-w-0 text-text-secondary" data-testid="audit-sentence">
            <span className="font-medium text-text-primary">@{actorName(t, entry.actor)}</span> {sentence(t, entry)}
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
  /** heading text; defaults to the localized "Change history". */
  title?: string;
}

// ObjectAuditTimeline is the 变更记录 section rendered in the issue/task/plan detail
// views. Self-contained: it fetches the ledger and renders loading / empty / list.
export function ObjectAuditTimeline({ objectType, projectId, objectId, title }: Props): React.ReactElement {
  const { t } = useTranslation('work');
  const { data, isLoading, isError } = useObjectAudit(objectType, projectId, objectId);
  const entries = data?.entries ?? [];

  return (
    <section className="space-y-2" data-testid="object-audit-timeline">
      <p className="text-xs font-semibold uppercase tracking-wide text-text-muted">{title ?? t('audit.title')}</p>
      {isLoading ? (
        <p className="text-xs text-text-muted" data-testid="audit-loading">
          {t('audit.loading')}
        </p>
      ) : isError ? (
        <p className="text-xs text-danger" data-testid="audit-error">
          {t('audit.error')}
        </p>
      ) : entries.length === 0 ? (
        <p className="text-xs text-text-muted" data-testid="audit-empty">
          {t('audit.empty')}
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
