import type React from 'react';
import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { Participant } from '@/api/types';
import { useConversationThreads } from '@/api/conversations';
import { ConversationView, type ConversationSurface } from './ConversationView';
import { ParticipantsPanel } from './ParticipantsPanel';
import { ConversationThreadList } from './ConversationThreadList';
import { ThreadSidebarProvider } from './ThreadSidebarContext';
import { SharedFilesPanel, useSharedFiles } from './SharedFilesPanel';

// ============================================================================
// Mobile (<768px) conversation surface — mobile-redesign-conversations.md §3.5.
// Replaces the pre-redesign ConversationMobileTabs (a compact <select> dropdown
// + a maximize toggle sharing one row).
//
// Per the spec mockup (frames ④/⑤), the panel switcher is a row of SEGMENT
// PILLS (`.segtabs`) directly under the detail-page header:
//   ChannelDetail : Chat | Threads | Files (n) | People (n)
//   DMDetail      : Chat | Threads | Files          (fixed 1:1 → no People)
//
// MAXIMIZE IS INTENTIONALLY GONE. The spec (§4 "Maximize/restore 切换", §7) flags
// it as an explicit implementation-stage decision — "不默默保留也不默默丢弃". The
// decision: DROP it. The old toggle only existed because the mobile chatbox was
// embedded in a long scrolling detail page and needed to escape it; under the
// nav framework (batch 1) the detail page IS the full-screen surface, so
// "maximize" would promote a full-screen view to a full-screen view. The
// useConversationMaximize hook is removed with this component's predecessor.
//
// The chat panel stays MOUNTED-but-hidden across switches so its SSE
// subscription + scroll position + composer draft survive; threads/files/people
// mount lazily when first activated.
// ============================================================================
type Tab = 'chat' | 'threads' | 'files' | 'people';

export interface ConversationSurfaceMobileProps {
  surface: ConversationSurface;
  conversationId: string;
  participants?: Participant[];
  /** Show the People segment (false for DMs — fixed 1:1). Default true. */
  showParticipants?: boolean;
}

export function ConversationSurfaceMobile({
  surface,
  conversationId,
  participants = [],
  showParticipants = true,
}: ConversationSurfaceMobileProps): React.ReactElement {
  const { t } = useTranslation('chat');
  const threads = useConversationThreads(conversationId);
  const files = useSharedFiles(conversationId);
  const threadCount = threads.data?.length ?? 0;
  const fileCount = files.length;
  const activeParticipants = participants.filter((p) => !p.left_at);
  const [tab, setTab] = useState<Tab>('chat');

  // Counts ride on the segment pills (mockup: `Files 3` / `People 12`). Chat and
  // Threads carry no count in the mockup's Channel frame; Threads gets one only
  // when non-zero so the pill row stays quiet on an empty conversation.
  const segments: ReadonlyArray<{ id: Tab; label: string; count?: number }> = [
    { id: 'chat', label: t('conversation.tabChat') },
    { id: 'threads', label: t('conversation.tabThreads'), count: threadCount },
    { id: 'files', label: t('conversation.tabFiles'), count: fileCount },
    ...(showParticipants
      ? [{ id: 'people' as const, label: t('conversation.tabParticipants'), count: activeParticipants.length }]
      : []),
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="conversation-surface-mobile">
      <div
        role="tablist"
        aria-label={t('conversation.panelLabel')}
        data-testid="conversation-segtabs"
        className="-mx-1 flex gap-1.5 overflow-x-auto border-b border-border-base px-1 py-1.5"
      >
        {segments.map((s) => {
          const active = tab === s.id;
          return (
            <button
              key={s.id}
              type="button"
              role="tab"
              id={`conversation-mseg-${s.id}`}
              aria-selected={active}
              aria-controls={`conversation-mpanel-${s.id}`}
              data-testid={`conversation-mseg-${s.id}`}
              data-active={active}
              onClick={() => setTab(s.id)}
              className={[
                // ≥44px touch target (v2.10.1 touch baseline).
                'inline-flex min-h-[44px] shrink-0 items-center gap-1 whitespace-nowrap rounded-full px-4 text-sm motion-safe:transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent',
                active
                  ? 'bg-brand font-semibold text-white'
                  : 'bg-bg-subtle text-text-secondary hover:text-text-primary',
              ].join(' ')}
            >
              <span>{s.label}</span>
              {s.count != null && s.count > 0 && (
                <span
                  data-testid={`conversation-mseg-${s.id}-count`}
                  className={[
                    'tabular-nums text-xs',
                    active ? 'text-white/80' : 'text-text-muted',
                  ].join(' ')}
                >
                  {s.count}
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Chat — mounted-but-hidden so SSE/scroll/draft survive switches. When
          active it must FILL the height so the stream scrolls inside the viewport. */}
      <div
        role="tabpanel"
        id="conversation-mpanel-chat"
        aria-labelledby="conversation-mseg-chat"
        hidden={tab !== 'chat'}
        data-testid="conversation-mpanel-chat"
        className={tab === 'chat' ? 'flex min-h-0 flex-1 flex-col' : undefined}
      >
        <ConversationView surface={surface} conversationId={conversationId} />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto" hidden={tab === 'chat'}>
        <div
          role="tabpanel"
          id="conversation-mpanel-threads"
          aria-labelledby="conversation-mseg-threads"
          hidden={tab !== 'threads'}
          data-testid="conversation-mpanel-threads"
        >
          {/* The Threads panel lives OUTSIDE ConversationView, so it has no
              ThreadSidebarProvider ancestor (ConversationView mounts its own for
              the chat panel). Without a provider, useThreadSidebar() is null and
              the thread rows are inert. Wrap it in its own provider so rows open
              the shared (overlay) ThreadSidebar. */}
          {tab === 'threads' && (
            <ThreadSidebarProvider>
              <ConversationThreadList conversationId={conversationId} embedded />
            </ThreadSidebarProvider>
          )}
        </div>
        <div
          role="tabpanel"
          id="conversation-mpanel-files"
          aria-labelledby="conversation-mseg-files"
          hidden={tab !== 'files'}
          data-testid="conversation-mpanel-files"
        >
          {tab === 'files' &&
            (fileCount > 0 ? (
              <SharedFilesPanel conversationId={conversationId} />
            ) : (
              <p className="px-4 py-3 text-xs text-text-muted" data-testid="conversation-mobile-files-empty">
                {t('conversation.noSharedFiles')}
              </p>
            ))}
        </div>
        {showParticipants && (
          <div
            role="tabpanel"
            id="conversation-mpanel-people"
            aria-labelledby="conversation-mseg-people"
            hidden={tab !== 'people'}
            data-testid="conversation-mpanel-people"
          >
            {tab === 'people' && (
              <ParticipantsPanel conversationId={conversationId} participants={participants} showThreads={false} />
            )}
          </div>
        )}
      </div>
    </div>
  );
}
