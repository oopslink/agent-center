import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useDisplayNameResolver } from '@/api/members';
import { type ReminderDetail, useReminder } from '@/api/reminders';
import { IconClose } from './icons';
import { MarkdownMessage } from './MarkdownMessage';
import { SenderSidebarProvider, useSenderSidebar } from './SenderSidebarContext';
import { formatLocalTime } from '@/utils/time';

// =============================================================================
// T207 [提醒-3] — row-click detail: the reminder + its 历史触发 (reminder_firings:
// 每次触发时间 / 是否投递 / 是否因重叠被跳过), per the mockup's list-row note.
//
// T286 [提醒] — linkify the ids in the detail TEXT: the reminder Content is rendered
// through MarkdownMessage (the SAME path chat uses) so embedded refs — task-<id>/
// T<n>, plan-<id>/P<n>, issue-<id>/I<n>, @handle — become ref links instead of dead
// text; the Target agent id becomes a clickable ref opening the sender sidebar. Both
// need a SenderSidebarProvider ancestor (MarkdownMessage only linkifies under one,
// and the Target link opens the provider's single SenderDetailSidebar).
// =============================================================================

const OUTCOME_LABEL_KEY: Record<string, string> = {
  delivered: 'reminders.detail.outcome.delivered',
  skipped_overlap: 'reminders.detail.outcome.skippedOverlap',
  failed: 'reminders.detail.outcome.failed',
};

interface Props {
  slug: string | undefined;
  reminderId: string;
  onClose: () => void;
}

export function ReminderDetailModal({ slug, reminderId, onClose }: Props): React.ReactElement {
  const { t } = useTranslation('insights');
  const { data, isLoading } = useReminder(slug, reminderId);

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-label={t('reminders.detail.ariaLabel')}
      data-testid="reminder-detail-modal"
    >
      <div className="flex max-h-[88vh] w-full max-w-md flex-col rounded-xl bg-bg-elevated shadow-xl">
        <div className="flex items-center justify-between border-b border-border-base px-5 py-3">
          <h4 className="text-base font-semibold text-text-primary">{t('reminders.detail.title')}</h4>
          <button type="button" onClick={onClose} className="text-text-muted hover:text-text-primary" aria-label={t('reminders.detail.close')}>
            <IconClose className="h-4 w-4" />
          </button>
        </div>
        {/* SenderSidebarProvider: enables MarkdownMessage linkification of the Content
            text AND owns the single SenderDetailSidebar the Target ref opens (T286). */}
        <SenderSidebarProvider>
          <div className="min-h-0 flex-1 space-y-3 overflow-y-auto px-5 py-4 text-sm">
            {isLoading && <p className="text-text-muted">{t('reminders.detail.loading')}</p>}
            {data && <ReminderDetailBody data={data} />}
          </div>
        </SenderSidebarProvider>
      </div>
    </div>
  );
}

// ReminderDetailBody renders the reminder fields UNDER the SenderSidebarProvider, so
// useSenderSidebar() resolves and the Content's MarkdownMessage linkifies refs (T286).
function ReminderDetailBody({ data }: { data: ReminderDetail }): React.ReactElement {
  const { t } = useTranslation('insights');
  const openSender = useSenderSidebar();
  const displayName = useDisplayNameResolver();
  const remindeeRef = `agent:${data.remindee_agent_id}`;

  return (
    <>
      <Row label={t('reminders.detail.target')}>
        {/* T286: the raw agent id becomes a ref link → opens the sender sidebar
            (mirrors the list page's displayName(agent:<id>) rendering). */}
        <button
          type="button"
          onClick={() => openSender?.(remindeeRef)}
          className="text-accent hover:underline"
          aria-label={t('reminders.detail.viewDetail', { name: displayName(remindeeRef) })}
          data-testid="reminder-target-link"
        >
          {displayName(remindeeRef)}
        </button>
      </Row>
      <Row label={t('reminders.detail.trigger')}>
        {data.schedule.kind === 'cron' ? (
          <span className="font-mono text-xs">
            {data.schedule.cron_expr} · {data.schedule.timezone}
          </span>
        ) : (
          <span>{data.schedule.once_at ? formatLocalTime(data.schedule.once_at) : '—'}</span>
        )}
      </Row>
      {/* T286: render Content through MarkdownMessage so task/plan/issue/@mention ids
          in the text are linkified (the same renderer chat messages use). */}
      <Row label={t('reminders.detail.content')}>
        <MarkdownMessage content={data.content} />
      </Row>
      {/* T719 defensive: was raw `data.status` enum (untranslated). */}
      <Row label={t('reminders.detail.status')}>{t(`reminders.statusChip.${data.status}`)}</Row>
      <Row label={t('reminders.detail.fired')}>{t('reminders.detail.firedCount', { count: data.fired_count })}</Row>

      <div className="pt-1">
        <div className="mb-1 text-xs font-semibold text-text-secondary">{t('reminders.detail.firingHistory')}</div>
        {data.firings.length === 0 ? (
          <p className="text-xs text-text-muted" data-testid="reminder-firings-empty">
            {t('reminders.detail.firingsEmpty')}
          </p>
        ) : (
          <ul className="space-y-1" data-testid="reminder-firings">
            {data.firings.map((f) => (
              <li
                key={f.id}
                className="flex items-center justify-between rounded border border-border-base/60 px-2 py-1 text-xs"
              >
                <span className="text-text-secondary">{formatLocalTime(f.fired_at)}</span>
                <span
                  className={
                    f.outcome === 'delivered'
                      ? 'text-success'
                      : f.outcome === 'skipped_overlap'
                        ? 'text-warning'
                        : 'text-danger'
                  }
                >
                  {OUTCOME_LABEL_KEY[f.outcome] ? t(OUTCOME_LABEL_KEY[f.outcome]) : f.outcome}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>
    </>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }): React.ReactElement {
  return (
    <div className="flex gap-3">
      <span className="w-14 shrink-0 text-xs text-text-muted">{label}</span>
      <span className="min-w-0 flex-1 text-text-primary">{children}</span>
    </div>
  );
}
