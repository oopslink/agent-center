import type React from 'react';
import { useState } from 'react';
import type { Participant } from '@/api/types';
import { useConversationThreads } from '@/api/conversations';
import { ConversationView, type ConversationSurface } from './ConversationView';
import { ParticipantsPanel } from './ParticipantsPanel';
import { ConversationThreadList } from './ConversationThreadList';
import { ThreadSidebarProvider } from './ThreadSidebarContext';
import { SharedFilesPanel, useSharedFiles } from './SharedFilesPanel';
import { useConversationMaximize, MaximizeToggle } from './useConversationMaximize';

// ============================================================================
// T184 — the MOBILE (<768px) conversation layout. Originally a full-width tab
// BAR (chat / [participants] / threads / files). Since the chatbox lives inside
// a long detail page on mobile, the bar ate a whole row of vertical space, so
// the switcher is now a compact DROPDOWN (owner: "tab 放到下拉菜单里，节约空间")
// sharing one row with a MAXIMIZE toggle (owner: "chatbox 需要支持最大化").
//
// "chat" is the message stream itself (col③ on desktop) and the rest are the
// same panels the desktop col④ sidebar shows. DMs drop the Participants entry
// (fixed 1:1), so a DM shows chat / threads / files.
//
// The chat panel stays MOUNTED-but-hidden across switches so its SSE
// subscription + scroll position + composer draft survive (same pattern as
// PlanDetail's chat tab); threads/files/participants mount lazily when active.
// ============================================================================
type Tab = 'chat' | 'participants' | 'threads' | 'files';

export interface ConversationMobileTabsProps {
  surface: ConversationSurface;
  conversationId: string;
  participants?: Participant[];
  /** show the Participants entry (false for DMs). Default true. */
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
  const { maximized, toggle } = useConversationMaximize();

  const tabs: ReadonlyArray<{ id: Tab; label: string; count?: number }> = [
    { id: 'chat', label: 'Chat' },
    ...(showParticipants ? [{ id: 'participants' as const, label: 'Participants' }] : []),
    { id: 'threads', label: 'Threads', count: threadCount },
    { id: 'files', label: 'Files', count: fileCount },
  ];
  const [tab, setTab] = useState<Tab>('chat');

  return (
    <div
      className={
        maximized
          ? 'fixed inset-0 z-50 flex min-h-0 flex-col bg-bg-base'
          : 'flex min-h-0 flex-1 flex-col'
      }
      data-testid="conversation-mobile-tabs"
      data-maximized={maximized ? 'true' : 'false'}
    >
      {/* Compact switcher row: dropdown (left) + maximize toggle (right). One
          row instead of a full tab bar — saves vertical space on mobile. */}
      <div className="flex items-center gap-2 border-b border-border-base px-2 py-1.5">
        <label className="sr-only" htmlFor="conversation-mtab-select">
          Conversation panel
        </label>
        <div className="relative min-w-0 flex-1">
          <select
            id="conversation-mtab-select"
            data-testid="conversation-mtab-select"
            value={tab}
            onChange={(e) => setTab(e.target.value as Tab)}
            aria-label="Conversation panel"
            className="w-full appearance-none rounded-full border border-border-base bg-bg-subtle py-2 pl-3.5 pr-9 text-[0.8125rem] font-semibold text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
          >
            {tabs.map((t) => (
              <option key={t.id} value={t.id}>
                {t.label}
                {t.count != null && t.count > 0 ? ` · ${t.count}` : ''}
              </option>
            ))}
          </select>
          {/* Chevron — the native select arrow is hidden via appearance-none. */}
          <span
            aria-hidden="true"
            className="pointer-events-none absolute right-3 top-1/2 -translate-y-1/2 text-text-muted"
          >
            <ChevronDownIcon />
          </span>
        </div>
        <MaximizeToggle
          maximized={maximized}
          onToggle={toggle}
          testId="conversation-maximize-toggle-mobile"
          className="inline-flex h-9 w-9 shrink-0 items-center justify-center rounded-full border border-border-base bg-bg-elevated text-text-muted hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
        />
      </div>

      {/* Chat — mounted-but-hidden so SSE/scroll/draft survive switches. When
          active it must FILL the height so the stream scrolls inside the viewport. */}
      <div
        role="tabpanel"
        id="conversation-mpanel-chat"
        aria-labelledby="conversation-mtab-select"
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
            aria-labelledby="conversation-mtab-select"
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
          aria-labelledby="conversation-mtab-select"
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
          aria-labelledby="conversation-mtab-select"
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

function ChevronDownIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.6" aria-hidden="true">
      <path d="M6 8l4 4 4-4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
