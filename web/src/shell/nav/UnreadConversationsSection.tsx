import type React from 'react';
import { useMemo, useState } from 'react';
import { NavLink } from 'react-router-dom';
import { useUnreadConversations, useMarkAllConversationsRead } from '@/api/conversations';
import type { UnreadConversationRow, UnreadConversationSource } from '@/api/types';
import { formatChatTime } from '@/utils/time';

// ============================================================================
// I23 (T332) — the cross-source "未读会话" digest at the top of the Conversations
// col② nav (mockup-conversations-reachability). It aggregates every unread
// conversation across plan / issue / task / channel / dm so the user can jump
// straight to a buried task/issue/plan conversation instead of hunting for it.
//
// Dynamic: renders nothing when there's no unread (the region only appears when
// it has content; the always-on Channels / DMs sections live below it). A
// top filter (全部 / @我 / 未读) instantly narrows the list. a11y: source tags
// are text-on-token chips (NOT emoji — the no-emoji-icons gate), and every
// badge spells its count in an aria-label (not color-only).
// ============================================================================

type Filter = 'all' | 'mentions' | 'unread';

// T334: the digest is collapsible; the fold state persists in localStorage so it
// stays folded across navigation (mirrors the DM subgroups / unmerged panel).
const UNREAD_COLLAPSE_KEY = 'ac.unreadconv.collapsed';
function readUnreadCollapsed(): boolean {
  try {
    return window.localStorage.getItem(UNREAD_COLLAPSE_KEY) === '1';
  } catch {
    return false;
  }
}

// Source-tag presentation per source family (mockup: 来源标签着色). Colors come
// from the design-token status palette so <html class="dark"> flips them; the
// numeric-palette raw-color lint never matches these `status-*` tokens.
const SOURCE_TAG: Record<UnreadConversationSource, { label: string; cls: string }> = {
  plan: { label: 'Plan', cls: 'bg-status-purple-bg text-status-purple-fg' },
  issue: { label: 'Issue', cls: 'bg-status-amber-bg text-status-amber-fg' },
  task: { label: 'Task', cls: 'bg-status-teal-bg text-status-teal-fg' },
  channel: { label: 'Channel', cls: 'bg-status-blue-bg text-status-blue-fg' },
  dm: { label: 'DM', cls: 'bg-status-slate-bg text-status-slate-fg' },
};

const MAX_BADGE = 99;
function cap(n: number): string {
  return n > MAX_BADGE ? `${MAX_BADGE}+` : String(n);
}

export function SourceTag({ source }: { source: UnreadConversationSource }): React.ReactElement {
  const t = SOURCE_TAG[source] ?? SOURCE_TAG.channel;
  return (
    <span
      data-testid="unread-conv-source-tag"
      className={`inline-flex shrink-0 items-center rounded px-1 text-[0.625rem] font-semibold uppercase leading-tight tracking-wide ${t.cls}`}
    >
      {t.label}
    </span>
  );
}

// RowBadge — the per-row unread indicator (mockup §badge rules):
//   - mention > 0 → brand "@N" pill (the high-signal @-me state).
//   - unread > 1 → neutral count pill.
//   - unread == 1 → small neutral dot (low count degrades to a dot).
export function RowBadge({ unread, mention }: { unread: number; mention: number }): React.ReactElement | null {
  if (mention > 0) {
    return (
      <span
        data-testid="unread-conv-mention-badge"
        data-mention-count={mention}
        aria-label={`${unread} unread, ${mention} ${mention === 1 ? 'mention' : 'mentions'}`}
        className="inline-flex min-w-[1.25rem] items-center justify-center rounded-full bg-brand px-1.5 text-[0.625rem] font-semibold leading-none text-white tabular-nums"
      >
        @{cap(mention)}
      </span>
    );
  }
  if (unread > 1) {
    return (
      <span
        data-testid="unread-conv-count-badge"
        data-unread-count={unread}
        aria-label={`${unread} unread`}
        className="inline-flex min-w-[1.25rem] items-center justify-center rounded-full bg-status-slate-bg px-1.5 text-[0.625rem] font-semibold leading-none text-status-slate-fg tabular-nums"
      >
        {cap(unread)}
      </span>
    );
  }
  if (unread === 1) {
    return (
      <span
        data-testid="unread-conv-dot"
        data-unread-count={unread}
        aria-label="1 unread"
        className="inline-flex items-center"
      >
        <span aria-hidden="true" className="h-1.5 w-1.5 rounded-full bg-status-slate-solid" />
      </span>
    );
  }
  return null;
}

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
        'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-[0.6875rem] font-medium motion-safe:transition-colors',
        active ? 'bg-brand text-white' : 'bg-bg-subtle text-text-muted hover:text-text-primary',
      ].join(' ')}
    >
      <span>{label}</span>
      <span className="tabular-nums opacity-80">{count}</span>
    </button>
  );
}

function UnreadRow({
  row,
  orgBase,
}: {
  row: UnreadConversationRow;
  orgBase: string;
}): React.ReactElement {
  const isMention = row.mention_count > 0;
  const preview = row.last_message_sender
    ? `${row.last_message_sender}: ${row.last_message_preview}`
    : row.last_message_preview;
  return (
    <li>
      <NavLink
        to={`${orgBase}${row.route}`}
        data-testid="unread-conv-row"
        data-source-type={row.source_type}
        data-mention={isMention ? 'true' : 'false'}
        className={({ isActive }) =>
          [
            'flex min-w-0 items-start gap-2 rounded px-2 py-1.5 motion-safe:transition-colors',
            isMention ? 'border-l-2 border-brand bg-brand/5 pl-1.5' : 'border-l-2 border-transparent',
            isActive ? 'bg-brand-hover text-white' : 'hover:bg-bg-subtle',
          ].join(' ')
        }
      >
        <span className="flex min-w-0 flex-1 flex-col gap-0.5">
          <span className="flex min-w-0 items-center gap-1.5">
            <SourceTag source={row.source_type} />
            <span className="min-w-0 flex-1 truncate text-sm font-semibold text-text-primary">
              {row.title}
            </span>
          </span>
          <span className="flex items-center gap-1">
            {isMention && (
              <span
                data-testid="unread-conv-mention-label"
                className="shrink-0 rounded bg-brand/10 px-1 text-[0.5625rem] font-semibold uppercase tracking-wide text-brand"
              >
                Mentions you
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
      </NavLink>
    </li>
  );
}

export function UnreadConversationsSection({ orgBase }: { orgBase: string }): React.ReactElement | null {
  const { data } = useUnreadConversations();
  const markAllRead = useMarkAllConversationsRead();
  const [filter, setFilter] = useState<Filter>('all');
  const [collapsed, setCollapsed] = useState(readUnreadCollapsed);
  const toggleCollapsed = (): void =>
    setCollapsed((v) => {
      const next = !v;
      try {
        window.localStorage.setItem(UNREAD_COLLAPSE_KEY, next ? '1' : '0');
      } catch {
        /* storage disabled */
      }
      return next;
    });

  const rows = useMemo(() => data ?? [], [data]);
  const mentionRows = useMemo(() => rows.filter((r) => r.mention_count > 0), [rows]);
  const plainUnreadRows = useMemo(() => rows.filter((r) => r.mention_count === 0), [rows]);

  // Dynamic: the region only appears when there IS unread (mockup §动态).
  if (rows.length === 0) return null;

  const shown =
    filter === 'mentions' ? mentionRows : filter === 'unread' ? plainUnreadRows : rows;

  return (
    <div data-testid="unread-conversations-section">
      <div className="flex items-center justify-between gap-2 px-1 pb-1">
        {/* T334: the header toggles the whole digest (chevron rotates when open);
            the total count stays visible even when collapsed. */}
        <button
          type="button"
          onClick={toggleCollapsed}
          aria-expanded={!collapsed}
          data-testid="unread-collapse-toggle"
          className="flex min-w-0 items-center gap-1 text-[0.6875rem] font-semibold uppercase tracking-wider text-text-muted hover:text-text-secondary"
        >
          <svg
            viewBox="0 0 12 12"
            aria-hidden="true"
            className={`h-2.5 w-2.5 shrink-0 transition-transform ${collapsed ? '' : 'rotate-90'}`}
          >
            <path d="M4 2l4 4-4 4" fill="none" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          <span>Unread</span>
          <span className="tabular-nums opacity-70">{rows.length}</span>
        </button>
        {/* T334: mark every conversation in the digest read in one click. */}
        <button
          type="button"
          onClick={() => markAllRead.mutate()}
          disabled={markAllRead.isPending}
          data-testid="unread-mark-all-read"
          className="shrink-0 rounded px-1 text-[0.625rem] font-medium text-text-muted hover:text-text-primary disabled:opacity-50"
          title="Mark all unread conversations read"
        >
          {markAllRead.isPending ? 'Marking…' : 'Mark all read'}
        </button>
      </div>
      {collapsed ? null : (
      <>
      <div className="mb-1 flex flex-wrap gap-1 px-1" role="group" aria-label="Unread filters">
        <FilterChip
          active={filter === 'all'}
          label="All"
          count={rows.length}
          testId="unread-filter-all"
          onClick={() => setFilter('all')}
        />
        <FilterChip
          active={filter === 'mentions'}
          label="@me"
          count={mentionRows.length}
          testId="unread-filter-mentions"
          onClick={() => setFilter('mentions')}
        />
        <FilterChip
          active={filter === 'unread'}
          label="Unread"
          count={plainUnreadRows.length}
          testId="unread-filter-unread"
          onClick={() => setFilter('unread')}
        />
      </div>
      <ul className="space-y-0.5">
        {shown.length === 0 ? (
          <li className="px-2 py-0.5 text-xs italic text-text-muted">No matching conversations</li>
        ) : (
          shown.map((row) => <UnreadRow key={row.conversation_id} row={row} orgBase={orgBase} />)
        )}
      </ul>
      </>
      )}
    </div>
  );
}
