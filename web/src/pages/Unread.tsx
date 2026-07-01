import type React from 'react';
import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';

import { OrgLink } from '@/OrgContext';
import { useUnreadConversations, useMarkAllConversationsRead } from '@/api/conversations';
import type { UnreadConversationRow } from '@/api/types';
import { EmptyState } from '@/components/EmptyState';
import { Skeleton } from '@/components/Skeleton';
import { SegmentedNav } from '@/shell/SegmentedNav';
import { RowBadge, SourceTag } from '@/shell/nav/UnreadConversationsSection';
import { formatChatTime } from '@/utils/time';
import { CONVERSATION_SEGMENTS } from './conversationSegments';

// T343 — mobile "Unread" page (@oopslink "移动端 chat 缺少 unread 消息"). Desktop
// surfaces the cross-source unread digest in col②; mobile had no way to reach it.
// This is the full-screen mobile equivalent: the same useUnreadConversations
// digest, an All / @me / Unread filter, mark-all-read, and tappable rows that
// jump straight to the buried plan/issue/task/channel/dm conversation. Row visuals
// (SourceTag + RowBadge) are shared with the desktop section so they stay in sync.

type Filter = 'all' | 'mentions' | 'unread';

function FilterChip({
  active,
  label,
  count,
  testId,
  onClick,
}: {
  active: boolean;
  label: string;
  count: number;
  testId: string;
  onClick: () => void;
}): React.ReactElement {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      data-testid={testId}
      className={[
        'inline-flex items-center gap-1 rounded-full px-3 py-1 text-sm font-medium motion-safe:transition-colors',
        active ? 'bg-brand text-white' : 'bg-bg-subtle text-text-muted hover:text-text-primary',
      ].join(' ')}
    >
      <span>{label}</span>
      <span className="tabular-nums opacity-80">{count}</span>
    </button>
  );
}

function UnreadCard({ row }: { row: UnreadConversationRow }): React.ReactElement {
  const { t } = useTranslation('chat');
  const isMention = row.mention_count > 0;
  const preview = row.last_message_sender
    ? `${row.last_message_sender}: ${row.last_message_preview}`
    : row.last_message_preview;
  return (
    <li>
      <OrgLink
        to={row.route}
        data-testid="unread-conv-row"
        data-source-type={row.source_type}
        data-mention={isMention ? 'true' : 'false'}
        className={[
          'flex min-w-0 items-start gap-2 px-3 py-3 motion-safe:transition-colors hover:bg-bg-subtle',
          isMention ? 'border-l-2 border-brand bg-brand/5' : 'border-l-2 border-transparent',
        ].join(' ')}
      >
        <span className="flex min-w-0 flex-1 flex-col gap-1">
          <span className="flex min-w-0 items-center gap-1.5">
            <SourceTag source={row.source_type} />
            <span className="min-w-0 flex-1 truncate text-sm font-semibold text-text-primary">
              {row.title}
            </span>
          </span>
          <span className="flex min-w-0 items-center gap-1">
            {isMention && (
              <span
                data-testid="unread-conv-mention-label"
                className="shrink-0 rounded bg-brand/10 px-1 text-[0.625rem] font-semibold uppercase tracking-wide text-brand"
              >
                {t('unread.mentionsYou')}
              </span>
            )}
            <span className="min-w-0 truncate text-xs text-text-muted">{preview}</span>
          </span>
        </span>
        <span className="flex shrink-0 flex-col items-end gap-1">
          <time className="text-[0.625rem] tabular-nums text-text-muted" dateTime={row.updated_at}>
            {formatChatTime(row.updated_at)}
          </time>
          <RowBadge unread={row.unread_count} mention={row.mention_count} />
        </span>
      </OrgLink>
    </li>
  );
}

export default function Unread(): React.ReactElement {
  const { t } = useTranslation('chat');
  const { data, isLoading, isError, error } = useUnreadConversations();
  const markAllRead = useMarkAllConversationsRead();
  const [filter, setFilter] = useState<Filter>('all');

  const rows = useMemo(() => data ?? [], [data]);
  const mentionRows = useMemo(() => rows.filter((r) => r.mention_count > 0), [rows]);
  const plainUnreadRows = useMemo(() => rows.filter((r) => r.mention_count === 0), [rows]);
  const shown =
    filter === 'mentions' ? mentionRows : filter === 'unread' ? plainUnreadRows : rows;

  return (
    <section className="space-y-4" data-testid="page-Unread">
      <SegmentedNav items={CONVERSATION_SEGMENTS} ariaLabel={t('unread.sectionsAriaLabel')} />
      <header className="flex items-center justify-between gap-2">
        <h1 className="text-xl font-semibold">{t('unread.title')}</h1>
        <button
          type="button"
          onClick={() => markAllRead.mutate()}
          disabled={markAllRead.isPending || rows.length === 0}
          data-testid="unread-mark-all-read"
          className="shrink-0 rounded border border-border-strong bg-bg-subtle px-3 py-1.5 text-sm font-medium text-text-secondary hover:bg-bg-base hover:text-text-primary disabled:opacity-50"
          title={t('unread.markAllReadTitle')}
        >
          {markAllRead.isPending ? t('unread.marking') : t('unread.markAllRead')}
        </button>
      </header>

      {isLoading && (
        <div className="space-y-2" data-testid="unread-loading">
          <Skeleton height="3rem" />
          <Skeleton height="3rem" />
          <Skeleton height="3rem" />
        </div>
      )}
      {isError && (
        <p className="text-sm text-danger" data-testid="unread-error">
          {(error as Error).message}
        </p>
      )}

      {!isLoading && !isError && rows.length === 0 && (
        <EmptyState
          testId="unread-empty"
          title={t('unread.emptyTitle')}
          body={t('unread.emptyBody')}
        />
      )}

      {!isLoading && !isError && rows.length > 0 && (
        <>
          <div className="flex flex-wrap gap-2" role="group" aria-label={t('unread.filtersAriaLabel')}>
            <FilterChip
              active={filter === 'all'}
              label={t('unread.filterAll')}
              count={rows.length}
              testId="unread-filter-all"
              onClick={() => setFilter('all')}
            />
            <FilterChip
              active={filter === 'mentions'}
              label={t('unread.filterMentions')}
              count={mentionRows.length}
              testId="unread-filter-mentions"
              onClick={() => setFilter('mentions')}
            />
            <FilterChip
              active={filter === 'unread'}
              label={t('unread.filterUnread')}
              count={plainUnreadRows.length}
              testId="unread-filter-unread"
              onClick={() => setFilter('unread')}
            />
          </div>
          <ul className="divide-y divide-border-base overflow-hidden rounded border border-border-base bg-bg-elevated text-text-primary">
            {shown.length === 0 ? (
              <li className="px-3 py-4 text-sm italic text-text-muted" data-testid="unread-no-match">
                {t('unread.noMatch')}
              </li>
            ) : (
              shown.map((row) => <UnreadCard key={row.conversation_id} row={row} />)
            )}
          </ul>
        </>
      )}
    </section>
  );
}
