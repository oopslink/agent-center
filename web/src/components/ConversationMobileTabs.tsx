import type React from 'react';
import { useState } from 'react';
import type { Participant } from '@/api/types';
import { useConversationThreads } from '@/api/conversations';
import { ConversationView, type ConversationSurface } from './ConversationView';
import { ParticipantsPanel } from './ParticipantsPanel';
import { ConversationThreadList } from './ConversationThreadList';
import { ThreadSidebarProvider } from './ThreadSidebarContext';
import { SharedFilesPanel, useSharedFiles } from './SharedFilesPanel';

// ============================================================================
// T184 — the MOBILE (<768px) conversation layout: a single tab bar
//   chat / [participants] / threads / files
// where "chat" is the message stream itself (col③ on desktop) and the rest are
// the same panels the desktop col④ sidebar shows. This replaces the desktop
// col③+col④ split on small screens (owner: "移动端改成 tab"). DMs drop the
// Participants tab (fixed 1:1), so a DM shows chat / threads / files.
//
// The chat panel stays MOUNTED-but-hidden across tab switches so its SSE
// subscription + scroll position + composer draft survive (same pattern as
// PlanDetail's chat tab); threads/files/participants mount lazily when active.
// ============================================================================
type Tab = 'chat' | 'participants' | 'threads' | 'files';

export interface ConversationMobileTabsProps {
  surface: ConversationSurface;
  conversationId: string;
  participants?: Participant[];
  /** show the Participants tab (false for DMs). Default true. */
  showParticipants?: boolean;
}

export function ConversationMobileTabs({
  surface,
  conversationId,
  participants = [],
  showParticipants = true,
}: ConversationMobileTabsProps): React.ReactElement {
  const threads = useConversationThreads(conversationId);
  const files = useSharedFiles(conversationId);
  const threadCount = threads.data?.length ?? 0;
  const fileCount = files.length;

  const tabs: ReadonlyArray<{ id: Tab; label: string; count?: number }> = [
    { id: 'chat', label: 'Chat' },
    ...(showParticipants ? [{ id: 'participants' as const, label: 'Participants' }] : []),
    { id: 'threads', label: 'Threads', count: threadCount },
    { id: 'files', label: 'Files', count: fileCount },
  ];
  const [tab, setTab] = useState<Tab>('chat');

  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="conversation-mobile-tabs">
      <div
        role="tablist"
        aria-label="Conversation"
        className="flex gap-1.5 border-b border-border-base px-2 py-2"
      >
        {tabs.map((t) => {
          const active = tab === t.id;
          return (
            <button
              key={t.id}
              type="button"
              role="tab"
              id={`conversation-mtab-${t.id}`}
              aria-selected={active}
              aria-controls={`conversation-mpanel-${t.id}`}
              data-testid={`conversation-mtab-${t.id}`}
              data-active={active}
              onClick={() => setTab(t.id)}
              className={[
                'flex flex-1 items-center justify-center gap-1.5 rounded-full px-2.5 py-1.5 min-h-[44px] text-[0.8125rem] font-semibold motion-safe:transition-colors',
                active ? 'bg-brand text-white' : 'bg-bg-subtle text-text-secondary hover:bg-bg-base',
              ].join(' ')}
            >
              {t.label}
              {t.count != null && t.count > 0 && (
                <span
                  data-testid={`conversation-mtab-${t.id}-count`}
                  className={`rounded-full px-1.5 text-[0.625rem] tabular-nums ${
                    active ? 'bg-white/25 text-white' : 'bg-bg-elevated text-text-muted'
                  }`}
                >
                  {t.count}
                </span>
              )}
            </button>
          );
        })}
      </div>

      {/* Chat — mounted-but-hidden so SSE/scroll/draft survive tab switches. When
          active it must FILL the height so the stream scrolls inside the viewport. */}
      <div
        role="tabpanel"
        id="conversation-mpanel-chat"
        aria-labelledby="conversation-mtab-chat"
        hidden={tab !== 'chat'}
        data-testid="conversation-mpanel-chat"
        className={tab === 'chat' ? 'flex min-h-0 flex-1 flex-col' : undefined}
      >
        <ConversationView surface={surface} conversationId={conversationId} />
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto" hidden={tab === 'chat'}>
        {showParticipants && (
          <div
            role="tabpanel"
            id="conversation-mpanel-participants"
            aria-labelledby="conversation-mtab-participants"
            hidden={tab !== 'participants'}
            data-testid="conversation-mpanel-participants"
          >
            {tab === 'participants' && (
              <ParticipantsPanel conversationId={conversationId} participants={participants} showThreads={false} />
            )}
          </div>
        )}
        <div
          role="tabpanel"
          id="conversation-mpanel-threads"
          aria-labelledby="conversation-mtab-threads"
          hidden={tab !== 'threads'}
          data-testid="conversation-mpanel-threads"
        >
          {/* The Threads tab lives OUTSIDE ConversationView, so it has no
              ThreadSidebarProvider ancestor (ConversationView mounts its own for
              the chat tab). Without a provider, useThreadSidebar() is null and
              the thread rows are inert — clicking a thread did nothing. Wrap the
              tab in its own provider so rows open the shared (overlay) ThreadSidebar. */}
          {tab === 'threads' && (
            <ThreadSidebarProvider>
              <ConversationThreadList conversationId={conversationId} embedded />
            </ThreadSidebarProvider>
          )}
        </div>
        <div
          role="tabpanel"
          id="conversation-mpanel-files"
          aria-labelledby="conversation-mtab-files"
          hidden={tab !== 'files'}
          data-testid="conversation-mpanel-files"
        >
          {tab === 'files' &&
            (fileCount > 0 ? (
              <SharedFilesPanel conversationId={conversationId} />
            ) : (
              <p className="px-4 py-3 text-xs text-text-muted" data-testid="conversation-mobile-files-empty">
                No shared files yet.
              </p>
            ))}
        </div>
      </div>
    </div>
  );
}
