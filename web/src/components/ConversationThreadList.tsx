import type React from 'react';
import { useTranslation } from 'react-i18next';
import { useConversationThreads } from '@/api/conversations';
import { isResolvedName, useDisplayNameResolver } from '@/api/members';
import type { ThreadSummary } from '@/api/types';
import { useThreadSidebar } from './ThreadSidebarContext';

// ConversationThreadList (v2.9.1 Threads P2) — the Participants-sidebar thread
// list: one row per thread in the conversation (root sender + content preview +
// reply-count chip + has-activity dot), sorted most-recent-activity first.
// Clicking a row opens the SAME ThreadSidebar (via the surface ThreadSidebar
// provider) with that thread's root — reusing the P1 component, no new sidebar.
//
// AA (day-0): solid theme tokens only, no alpha-tint; the count chip mirrors the
// ParticipantsPanel chip; the activity dot is a solid bg-accent disc. Each row is
// a real <button> (Tab + Enter/Space), so the list is reachable by keyboard.

interface Props {
  conversationId: string;
  // embedded = rendered as a standalone panel/tab (e.g. the channel Threads tab).
  // Drops the top divider + redundant "Threads" header that the stacked-in-
  // Participants layout needs — otherwise the divider shows as a stray empty
  // bordered bar at the top of its own tab (T96 follow-up).
  embedded?: boolean;
}

// activityKey — sort/compare key for a thread: most recent reply time, falling
// back to the root's own post time when there is no recorded activity.
function activityKey(t: ThreadSummary): string {
  return t.thread_last_activity_at ?? t.root.posted_at;
}

export function ConversationThreadList({ conversationId, embedded = false }: Props): React.ReactElement {
  // Aliased to `tr` because the thread .map() callback below binds `t` to a
  // ThreadSummary (and activityKey(t) does too) — using `t` for the translator
  // here would shadow/collide.
  const { t: tr } = useTranslation('chat');
  const threads = useConversationThreads(conversationId);
  const displayName = useDisplayNameResolver();
  // Opener for the shared ThreadSidebar (mounted by ConversationView). null when
  // there is no provider — then the rows are inert (the sidebar lives at the
  // surface level), which only happens outside a real conversation surface.
  const openThread = useThreadSidebar();

  return (
    <section
      className={embedded ? 'px-3 py-2' : 'mt-4 border-t border-border-base pt-4'}
      data-testid="thread-list"
    >
      {!embedded && (
        <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-text-muted">{tr('conversation.threadsHeader')}</h3>
      )}
      {threads.isLoading && (
        <p className="text-xs text-text-muted" data-testid="thread-list-loading">
          {tr('conversation.loadingThreads')}
        </p>
      )}
      {threads.isError && (
        <p className="text-xs text-danger" data-testid="thread-list-error">
          {tr('conversation.threadsLoadError')}
        </p>
      )}
      {threads.isSuccess && threads.data.length === 0 && (
        <p className="text-xs text-text-muted" data-testid="thread-list-empty">
          {tr('conversation.noThreads')}
        </p>
      )}
      {threads.isSuccess && threads.data.length > 0 && (
        <ul className="space-y-1" data-testid="thread-list-items">
          {[...threads.data]
            .sort((a, b) => activityKey(b).localeCompare(activityKey(a)))
            .map((t) => {
              const resolved = displayName(t.root.sender_identity_id);
              const senderResolved = isResolvedName(t.root.sender_identity_id, resolved);
              // v2.9.1 P3: dot = NEW activity since last viewed (server-derived).
              const hasActivity = !!t.has_new_activity;
              return (
                <li key={t.root.id}>
                  <button
                    type="button"
                    onClick={() => openThread?.(t.root)}
                    data-testid="thread-list-row"
                    data-root-id={t.root.id}
                    className="flex w-full items-center gap-2 rounded px-1.5 py-1 text-left text-xs hover:bg-bg-base focus-visible:ring-2 focus-visible:ring-accent"
                  >
                    <span className="min-w-0 flex-1">
                      <span
                        className={`block truncate font-medium ${senderResolved ? 'text-text-secondary' : 'italic text-text-muted'}`}
                        data-testid="thread-list-sender"
                      >
                        {senderResolved ? resolved : tr('conversation.deletedSender')}
                      </span>
                      {/* F3 (Tester2 run-real): preview uses text-text-secondary
                          (slate-600 ≈6.92), NOT text-text-muted (slate-500 on the
                          slate-100 panel = 4.34 < 4.5 AA FAIL in light). §5.5. */}
                      <span className="block truncate text-text-secondary" data-testid="thread-list-preview">
                        {t.root.content}
                      </span>
                    </span>
                    {t.reply_count > 0 && (
                      <span
                        className="inline-flex min-w-[1.25rem] shrink-0 items-center justify-center rounded-full bg-bg-subtle px-1.5 py-0.5 text-[0.625rem] font-semibold text-text-secondary"
                        data-testid="thread-list-reply-count"
                      >
                        {t.reply_count}
                      </span>
                    )}
                    {hasActivity && (
                      <span
                        className="h-2 w-2 shrink-0 rounded-full bg-accent"
                        data-testid="thread-list-activity-dot"
                        aria-hidden="true"
                      />
                    )}
                  </button>
                </li>
              );
            })}
        </ul>
      )}
    </section>
  );
}
