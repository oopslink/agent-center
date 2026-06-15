import type React from 'react';
import { useState } from 'react';
import type { Participant } from '@/api/types';
import { useConversationThreads } from '@/api/conversations';
import { ParticipantsPanel } from './ParticipantsPanel';
import { ConversationThreadList } from './ConversationThreadList';
import { SharedFilesPanel, useSharedFiles } from './SharedFilesPanel';

// ============================================================================
// v2.10.1 [T96] ChannelSidebarTabs — the channel col④ sidebar reorganized into
// a segmented 3-tab panel (Chat / Threads / Files), showing one at a time
// (IA finalized = variant B, owner 2026-06-15; mockup
// `docs/design/v2.10.1/desk-channel-tabs.html`).
//
//   - Chat    = the participants sidebar (list + invite/remove). The main chat
//               stream stays in col③; per owner, participant management keeps
//               its current place — here, inside the default Chat tab.
//   - Threads = the conversation's thread list (previously stacked at the bottom
//               of the participants panel; now its own tab).
//   - Files   = the shared-files list.
//
// The tab header reuses the pill segment style. Threads/Files carry a count
// badge. On mobile the M1 shell reflows the whole col④ panel into a bottom
// sheet, so this tabbed panel works there too.
// ============================================================================
type Tab = 'chat' | 'threads' | 'files';

export function ChannelSidebarTabs({
  conversationId,
  participants,
}: {
  conversationId: string;
  participants: Participant[];
}): React.ReactElement {
  const [tab, setTab] = useState<Tab>('chat');
  const threads = useConversationThreads(conversationId);
  const files = useSharedFiles(conversationId);
  const threadCount = threads.data?.length ?? 0;
  const fileCount = files.length;

  const tabs: ReadonlyArray<{ id: Tab; label: string; count?: number }> = [
    { id: 'chat', label: 'Chat' },
    { id: 'threads', label: 'Threads', count: threadCount },
    { id: 'files', label: 'Files', count: fileCount },
  ];

  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="channel-sidebar-tabs">
      <div
        role="tablist"
        aria-label="Channel sidebar"
        className="flex gap-1.5 border-b border-border-base p-2.5"
      >
        {tabs.map((t) => {
          const active = tab === t.id;
          return (
            <button
              key={t.id}
              type="button"
              role="tab"
              id={`channel-tab-${t.id}`}
              aria-selected={active}
              aria-controls={`channel-panel-${t.id}`}
              data-testid={`channel-tab-${t.id}`}
              data-active={active}
              onClick={() => setTab(t.id)}
              className={[
                'flex flex-1 items-center justify-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-semibold motion-safe:transition-colors',
                active ? 'bg-brand text-white' : 'bg-bg-subtle text-text-secondary hover:bg-bg-base',
              ].join(' ')}
            >
              {t.label}
              {t.count != null && t.count > 0 && (
                <span
                  data-testid={`channel-tab-${t.id}-count`}
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

      <div className="min-h-0 flex-1 overflow-y-auto">
        <div
          role="tabpanel"
          id="channel-panel-chat"
          aria-labelledby="channel-tab-chat"
          hidden={tab !== 'chat'}
          data-testid="channel-panel-chat"
        >
          {tab === 'chat' && (
            <ParticipantsPanel
              conversationId={conversationId}
              participants={participants}
              showThreads={false}
            />
          )}
        </div>
        <div
          role="tabpanel"
          id="channel-panel-threads"
          aria-labelledby="channel-tab-threads"
          hidden={tab !== 'threads'}
          data-testid="channel-panel-threads"
        >
          {tab === 'threads' && <ConversationThreadList conversationId={conversationId} />}
        </div>
        <div
          role="tabpanel"
          id="channel-panel-files"
          aria-labelledby="channel-tab-files"
          hidden={tab !== 'files'}
          data-testid="channel-panel-files"
        >
          {tab === 'files' &&
            (fileCount > 0 ? (
              <SharedFilesPanel conversationId={conversationId} />
            ) : (
              <p className="px-4 py-3 text-xs text-text-muted" data-testid="channel-files-empty">
                No shared files yet.
              </p>
            ))}
        </div>
      </div>
    </div>
  );
}
