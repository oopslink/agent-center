import type React from 'react';
import { useState } from 'react';
import type { Participant } from '@/api/types';
import { useConversationThreads } from '@/api/conversations';
import { ParticipantsPanel } from './ParticipantsPanel';
import { ConversationThreadList } from './ConversationThreadList';
import { SharedFilesPanel, useSharedFiles } from './SharedFilesPanel';

// ============================================================================
// T184 — the SHARED conversation col④ sidebar (Participants / Threads / Files),
// hoisted out of the channel-only ChannelSidebarTabs so EVERY conversation type
// (channel / DM / task / issue / plan) gets the same panel. Generalized from
// v2.10.1 [T96]'s ChannelSidebarTabs (segmented 3-tab, one at a time).
//
//   - Participants : the participants list + invite/remove. HIDDEN for DMs
//     (showParticipants=false) — a DM is a fixed 1:1, nothing to manage (owner).
//   - Threads      : the conversation's thread list (embedded, no redundant header).
//   - Files        : the shared-files list.
//
// Mobile: the shell reflows col④ into a bottom sheet, and the page-level mobile
// chat/threads/files tab bar (T184) reuses these same panels, so this component
// is surface- and viewport-agnostic.
// ============================================================================
type Tab = 'participants' | 'threads' | 'files';

export interface ConversationSidebarProps {
  conversationId: string;
  /** participant list (from the conversation projection). Optional — omitted/[] is fine. */
  participants?: Participant[];
  /**
   * Whether to show the Participants tab. Default true. DMs pass false (fixed
   * 1:1 — no invite/remove), leaving Threads / Files only.
   */
  showParticipants?: boolean;
  /** optional toolbar rendered at the right of the tab row (e.g. the collapse toggle). */
  toolbar?: React.ReactNode;
}

export function ConversationSidebar({
  conversationId,
  participants = [],
  showParticipants = true,
  toolbar,
}: ConversationSidebarProps): React.ReactElement {
  const threads = useConversationThreads(conversationId);
  const files = useSharedFiles(conversationId);
  const threadCount = threads.data?.length ?? 0;
  const fileCount = files.length;

  const tabs: ReadonlyArray<{ id: Tab; label: string; count?: number }> = [
    ...(showParticipants ? [{ id: 'participants' as const, label: 'Participants' }] : []),
    { id: 'threads', label: 'Threads', count: threadCount },
    { id: 'files', label: 'Files', count: fileCount },
  ];
  // Default to the first available tab (Participants when shown, else Threads).
  const [tab, setTab] = useState<Tab>(showParticipants ? 'participants' : 'threads');

  return (
    <div className="flex min-h-0 flex-1 flex-col" data-testid="conversation-sidebar">
      <div
        role="tablist"
        aria-label="Conversation sidebar"
        className="flex items-center gap-1.5 border-b border-border-base p-2.5"
      >
        {tabs.map((t) => {
          const active = tab === t.id;
          return (
            <button
              key={t.id}
              type="button"
              role="tab"
              id={`conversation-tab-${t.id}`}
              aria-selected={active}
              aria-controls={`conversation-panel-${t.id}`}
              data-testid={`conversation-tab-${t.id}`}
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
                  data-testid={`conversation-tab-${t.id}-count`}
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
        {toolbar != null && <div className="ml-auto flex shrink-0 items-center">{toolbar}</div>}
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto">
        {showParticipants && (
          <div
            role="tabpanel"
            id="conversation-panel-participants"
            aria-labelledby="conversation-tab-participants"
            hidden={tab !== 'participants'}
            data-testid="conversation-panel-participants"
          >
            {tab === 'participants' && (
              <ParticipantsPanel
                conversationId={conversationId}
                participants={participants}
                showThreads={false}
              />
            )}
          </div>
        )}
        <div
          role="tabpanel"
          id="conversation-panel-threads"
          aria-labelledby="conversation-tab-threads"
          hidden={tab !== 'threads'}
          data-testid="conversation-panel-threads"
        >
          {tab === 'threads' && <ConversationThreadList conversationId={conversationId} embedded />}
        </div>
        <div
          role="tabpanel"
          id="conversation-panel-files"
          aria-labelledby="conversation-tab-files"
          hidden={tab !== 'files'}
          data-testid="conversation-panel-files"
        >
          {tab === 'files' &&
            (fileCount > 0 ? (
              <SharedFilesPanel conversationId={conversationId} />
            ) : (
              <p className="px-4 py-3 text-xs text-text-muted" data-testid="conversation-files-empty">
                No shared files yet.
              </p>
            ))}
        </div>
      </div>
    </div>
  );
}
