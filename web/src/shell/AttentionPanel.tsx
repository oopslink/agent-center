import type React from 'react';
import { useEffect, useRef } from 'react';
import { useTranslation } from 'react-i18next';
import { Link } from 'react-router-dom';
import { useQueryClient } from '@tanstack/react-query';
import { useMarkSeen } from '@/api/readState';
import { qk } from '@/api/queryKeys';
import type { AttentionItem, AttentionSeverity } from '@/api/attention';

// ============================================================================
// AttentionPanel (v2.26.0 I61) — the "Needs your attention" popout, shared by the
// desktop col① rail and the mobile top bar. Renders the unified attention list
// (already deduped + severity-then-recency sorted server-side):
//   - kind=task    → deep-links to the stuck task (unblock it there). No dismiss
//                    (a task clears when the block is resolved — "沿用现状").
//   - kind=mention → deep-links to the source conversation AND offers a dismiss
//                    (mark_seen advances the read cursor past the item's
//                    message_id, so the escalation drops off the panel).
// Empty state is accurate now that the source is more than tasks.
// ============================================================================

const SEVERITY_ACCENT: Record<AttentionSeverity, string> = {
  urgent: 'border-l-danger',
  warning: 'border-l-warning',
  info: 'border-l-border-base',
};

const MAX_BADGE = 99;
function capCount(n: number): string {
  return n > MAX_BADGE ? `${MAX_BADGE}+` : String(n);
}

// itemBadge — the small type/severity chip. task items name the block flavour
// (input_required = awaiting your reply, obstacle = needs intervention); mention
// items name the directed-signal flavour (DM vs @mention).
function ItemBadge({ item }: { item: AttentionItem }): React.ReactElement {
  const { t } = useTranslation('common');
  let label: string;
  let cls: string;
  if (item.kind === 'task') {
    const inputRequired = item.reason_type === 'input_required';
    label = inputRequired ? t('shell.alerts.item.awaitingReply') : t('shell.alerts.item.needsIntervention');
    cls = inputRequired ? 'bg-danger/15 text-danger' : 'bg-warning/15 text-warning';
  } else {
    const isDM = item.conversation_kind === 'dm';
    label = isDM ? t('shell.alerts.item.dm') : t('shell.alerts.item.mention');
    cls = 'bg-brand/15 text-brand';
  }
  return (
    <span
      className={['inline-flex shrink-0 items-center rounded px-1.5 py-0.5 text-[0.625rem] font-semibold', cls].join(' ')}
    >
      {label}
    </span>
  );
}

function AttentionRow({
  item,
  orgBase,
  onClose,
  onDismiss,
}: {
  item: AttentionItem;
  orgBase: string;
  onClose: () => void;
  onDismiss: (item: AttentionItem) => void;
}): React.ReactElement {
  const canDismiss = item.kind === 'mention' && !!item.message_id;
  const secondary = item.kind === 'task' ? item.project_name : item.actor;
  return (
    <li className="relative">
      <Link
        to={`${orgBase}${item.route}`}
        data-testid="rail-alert-item"
        data-kind={item.kind}
        data-reason-type={item.reason_type ?? ''}
        onClick={onClose}
        className={[
          'block rounded-md border border-l-2 border-border-base px-2.5 py-2 hover:bg-bg-subtle motion-safe:transition-colors',
          canDismiss ? 'pr-7' : '',
          SEVERITY_ACCENT[item.severity] ?? SEVERITY_ACCENT.info,
        ].join(' ')}
      >
        <div className="flex items-center gap-1.5">
          <ItemBadge item={item} />
          {item.org_ref && (
            <span className="shrink-0 font-mono text-[0.6875rem] text-text-muted">{item.org_ref}</span>
          )}
          {item.kind === 'mention' && (item.mention_count ?? 0) > 0 && (
            <span
              data-testid="rail-alert-mention-count"
              className="shrink-0 rounded-full bg-brand px-1.5 text-[0.625rem] font-semibold leading-none text-white tabular-nums"
            >
              @{capCount(item.mention_count ?? 0)}
            </span>
          )}
          <span className="truncate text-sm font-medium text-text-primary">{item.title}</span>
        </div>
        {item.snippet && (
          <div className="mt-1 truncate text-xs text-text-secondary" title={item.snippet}>
            {item.snippet}
          </div>
        )}
        {secondary && <div className="mt-0.5 truncate text-[0.6875rem] text-text-muted">{secondary}</div>}
      </Link>
      {canDismiss && (
        <DismissButton item={item} onDismiss={onDismiss} />
      )}
    </li>
  );
}

function DismissButton({
  item,
  onDismiss,
}: {
  item: AttentionItem;
  onDismiss: (item: AttentionItem) => void;
}): React.ReactElement {
  const { t } = useTranslation('common');
  return (
    <button
      type="button"
      data-testid="rail-alert-dismiss"
      aria-label={t('shell.alerts.item.dismiss')}
      title={t('shell.alerts.item.dismiss')}
      onClick={(e) => {
        // The button overlays a Link — don't navigate when dismissing.
        e.preventDefault();
        e.stopPropagation();
        onDismiss(item);
      }}
      className="absolute right-1 top-1.5 inline-flex h-5 w-5 items-center justify-center rounded text-text-muted hover:bg-bg-subtle hover:text-text-primary"
    >
      <svg viewBox="0 0 12 12" aria-hidden="true" className="h-3 w-3">
        <path d="M3 3l6 6M9 3l-6 6" fill="none" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    </button>
  );
}

export function AttentionPanel({
  items,
  orgBase,
  onClose,
  className,
  testId = 'rail-alerts-panel',
  toggleTestId = 'rail-alerts',
}: {
  items: ReadonlyArray<AttentionItem>;
  orgBase: string;
  onClose: () => void;
  /** positioning classes — the rail popout vs the mobile dropdown differ only here. */
  className?: string;
  testId?: string;
  /** the toggle button's testid, so an outside-click on it doesn't double-close. */
  toggleTestId?: string;
}): React.ReactElement {
  const { t } = useTranslation('common');
  const panelRef = useRef<HTMLDivElement>(null);
  const qc = useQueryClient();
  const markSeen = useMarkSeen();

  useEffect(() => {
    const handler = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onClose();
    };
    document.addEventListener('keydown', handler);
    return () => document.removeEventListener('keydown', handler);
  }, [onClose]);

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (panelRef.current && !panelRef.current.contains(e.target as Node)) {
        const target = e.target as HTMLElement;
        if (target.closest(`[data-testid="${toggleTestId}"]`)) return; // toggle manages its own state
        onClose();
      }
    };
    document.addEventListener('mousedown', handler);
    return () => document.removeEventListener('mousedown', handler);
  }, [onClose, toggleTestId]);

  const dismiss = (item: AttentionItem): void => {
    if (!item.message_id) return;
    markSeen.mutate(
      { conversationId: item.conversation_id, lastSeenMessageId: item.message_id },
      // The item drops off the panel once the read cursor advances; refresh now
      // (the SSE read_state event also invalidates attention, this is the local echo).
      { onSuccess: () => void qc.invalidateQueries({ queryKey: qk.attention() }) },
    );
  };

  return (
    <div
      ref={panelRef}
      data-testid={testId}
      className={
        className ??
        'absolute bottom-12 left-14 z-50 max-h-[70vh] w-80 overflow-y-auto rounded-lg border border-border-base bg-bg-elevated p-3 shadow-2'
      }
    >
      <div className="mb-2 flex items-center justify-between">
        <span className="text-sm font-medium text-text-primary">{t('shell.alerts.panelTitle')}</span>
        <span className="rounded-full bg-bg-subtle px-1.5 text-[0.6875rem] font-semibold tabular-nums text-text-secondary">
          {items.length}
        </span>
      </div>
      {items.length === 0 ? (
        <div data-testid="rail-alerts-empty" className="px-1 py-6 text-center text-sm text-text-muted">
          {t('shell.alerts.empty')}
        </div>
      ) : (
        <ul className="flex flex-col gap-1">
          {items.map((item) => (
            <AttentionRow key={`${item.kind}:${item.ref}`} item={item} orgBase={orgBase} onClose={onClose} onDismiss={dismiss} />
          ))}
        </ul>
      )}
    </div>
  );
}
