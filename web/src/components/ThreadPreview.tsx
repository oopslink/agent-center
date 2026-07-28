import type React from 'react';
import { useTranslation } from 'react-i18next';
import type { Message } from '@/api/types';
import { useThreadReplies } from '@/api/conversations';
import { isResolvedName, normalizeIdentityRef, useDisplayNameResolver } from '@/api/members';
import { formatChatTime, formatLocalTime } from '@/utils/time';
import { Avatar } from './Avatar';

interface Props {
  rootMessage: Message;
  align?: 'left' | 'right';
  hasActivity?: boolean;
  onOpenThread: () => void;
}

function compactPreview(content: string): string {
  return content.replace(/\s+/g, ' ').trim();
}

export function ThreadPreview({
  rootMessage,
  align = 'left',
  hasActivity = false,
  onOpenThread,
}: Props): React.ReactElement | null {
  const { t } = useTranslation('chat');
  const displayName = useDisplayNameResolver();
  const replyCount = rootMessage.reply_count ?? 0;
  const replies = useThreadReplies(
    replyCount > 0 ? rootMessage.conversation_id : undefined,
    replyCount > 0 ? rootMessage.id : undefined,
  );

  if (replyCount <= 0) return null;

  const replyList = replies.data ?? [];
  const visibleReplies = replyList.slice(-3);
  const earlierCount = replies.isSuccess ? Math.max(0, replyCount - visibleReplies.length) : 0;
  const statusLabel = hasActivity
    ? t('threadPreview.unreadActivity')
    : t('threadPreview.seenActivity');

  return (
    <section
      className={`mt-0.5 w-full max-w-full md:max-w-[75%] ${align === 'right' ? 'self-end' : ''}`}
      data-testid="thread-preview"
      data-root-id={rootMessage.id}
      data-align={align}
    >
      <button
        type="button"
        onClick={onOpenThread}
        data-testid="thread-preview-panel"
        aria-label={t('threadPreview.openThread', { count: replyCount })}
        title={t('threadPreview.openThread', { count: replyCount })}
        className={`block w-full rounded-b-lg rounded-t-sm border border-border-base bg-bg-base px-3 py-2 text-left hover:border-border-strong hover:bg-bg-subtle focus-visible:ring-2 focus-visible:ring-accent ${
          align === 'right'
            ? 'border-r-2 border-r-border-strong text-right'
            : 'border-l-2 border-l-border-strong'
        }`}
      >
        <div
          className="mb-1.5 flex items-center gap-2 border-b border-border-base pb-1.5"
          data-testid="thread-preview-header"
        >
          <span
            className="inline-flex min-w-0 items-center gap-1.5 text-xs font-semibold text-text-primary"
            data-testid="thread-preview-chip"
          >
            <svg
              viewBox="0 0 20 20"
              fill="none"
              className="h-3.5 w-3.5 shrink-0 stroke-current"
              strokeWidth="1.7"
              aria-hidden="true"
            >
              <path d="M2.75 10s2.6-4.25 7.25-4.25S17.25 10 17.25 10s-2.6 4.25-7.25 4.25S2.75 10 2.75 10z" />
              <circle cx="10" cy="10" r="1.8" />
            </svg>
            <span className="truncate">
              {t('threadPreview.chip', { count: replyCount, id: rootMessage.id.slice(-4) })}
            </span>
            {hasActivity && (
              <span
                className="h-2 w-2 shrink-0 rounded-full bg-accent"
                data-testid="thread-preview-activity-dot"
                aria-hidden="true"
              />
            )}
          </span>
          <span className="ml-auto text-[0.625rem] font-medium uppercase text-text-muted">
            {statusLabel}
          </span>
          {earlierCount > 0 && (
            <span className="text-xs font-semibold text-text-secondary" data-testid="thread-preview-earlier">
              {t('threadPreview.earlierReplies', { count: earlierCount })}
            </span>
          )}
        </div>

        {replies.isLoading && (
          <p className="py-1 text-xs text-text-muted" data-testid="thread-preview-loading">
            {t('threadPreview.loading')}
          </p>
        )}
        {replies.isError && (
          <p className="py-1 text-xs text-danger" data-testid="thread-preview-error">
            {t('threadPreview.loadError')}
          </p>
        )}
        {!replies.isLoading && !replies.isError && visibleReplies.length === 0 && (
          <p className="py-1 text-xs text-text-muted" data-testid="thread-preview-empty">
            {t('threadPreview.noPreview')}
          </p>
        )}
        {visibleReplies.length > 0 && (
          <ul className="space-y-1" data-testid="thread-preview-replies">
            {visibleReplies.map((reply) => {
              const resolvedName = displayName(reply.sender_identity_id);
              const senderResolved = isResolvedName(reply.sender_identity_id, resolvedName);
              const senderHandle = normalizeIdentityRef(reply.sender_identity_id);
              const avatarName = senderResolved ? resolvedName : senderHandle;
              return (
                <li
                  key={reply.id}
                  className={`flex min-w-0 items-center gap-2 text-xs ${align === 'right' ? 'flex-row-reverse' : ''}`}
                  data-testid="thread-preview-reply"
                >
                  <Avatar
                    name={avatarName}
                    kind={reply.sender_identity_id.startsWith('agent:') ? 'agent' : 'human'}
                    size="sm"
                  />
                  <span className="min-w-0 flex-1 truncate text-text-secondary">
                    <span className="font-semibold text-text-primary">
                      {senderResolved ? resolvedName : t('threadPreview.deletedSender')}
                    </span>{' '}
                    <span>{compactPreview(reply.content)}</span>
                  </span>
                  <time
                    className="shrink-0 text-[0.625rem] text-text-muted"
                    dateTime={reply.posted_at}
                    title={formatLocalTime(reply.posted_at)}
                  >
                    {formatChatTime(reply.posted_at)}
                  </time>
                </li>
              );
            })}
          </ul>
        )}
      </button>
    </section>
  );
}
