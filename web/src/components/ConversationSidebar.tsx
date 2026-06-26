import type React from 'react';
import { useState } from 'react';
import type { Participant } from '@/api/types';
import { useConversationThreads } from '@/api/conversations';
import { ParticipantsPanel } from './ParticipantsPanel';
import { ConversationThreadList } from './ConversationThreadList';
import { SharedFilesPanel, useSharedFiles } from './SharedFilesPanel';
import { useContextPanelCollapse } from '@/shell/contextPanel';

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
  /**
   * Whether to render the shell col④ collapse button (from useContextPanelCollapse).
   * Default true. The EMBEDDED variant (T325) passes false: it lives outside col④
   * but still inside the shell's ContextPanelProvider, so the shell collapse would
   * otherwise render a SECOND, no-op button (col④ isn't mounted on desktop). T326.
   */
  showShellCollapse?: boolean;
}

export function ConversationSidebar({
  conversationId,
  participants = [],
  showParticipants = true,
  toolbar,
  showShellCollapse = true,
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
  // T184: when rendered inside the shell col④, expose a fully-collapse button
  // (desktop only — mobile col④ is a bottom sheet). Null outside the shell.
  const collapse = useContextPanelCollapse();

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
                // min-w-0 lets the tabs shrink in a narrow (embedded) pane so the
                // row doesn't overflow and push the toolbar/collapse off-screen.
                'flex min-w-0 flex-1 items-center justify-center gap-1.5 rounded-full px-3 py-1.5 text-xs font-semibold motion-safe:transition-colors',
                active ? 'bg-brand text-white' : 'bg-bg-subtle text-text-secondary hover:bg-bg-base',
              ].join(' ')}
            >
              <span className="min-w-0 truncate">{t.label}</span>
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
        {(toolbar != null || (showShellCollapse && collapse != null)) && (
          <div className="ml-auto flex shrink-0 items-center gap-1">
            {toolbar}
            {showShellCollapse && collapse != null && (
              <button
                type="button"
                data-testid="conversation-sidebar-collapse"
                aria-label="Collapse sidebar"
                title="Collapse sidebar"
                onClick={() => collapse.setCollapsed(true)}
                className="hidden min-h-[44px] min-w-[44px] items-center justify-center rounded text-text-secondary hover:bg-bg-subtle hover:text-text-primary md:inline-flex md:h-7 md:w-7 md:min-h-0 md:min-w-0"
              >
                <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" aria-hidden="true" className="h-4 w-4">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M7.5 5l5 5-5 5" />
                </svg>
              </button>
            )}
          </div>
        )}
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

// ── T324/T325: embedded (in-chat-box) variant ──────────────────────────────
// On desktop the conversation's Participants/Threads/Files panel lives INSIDE
// the chat box (not the shell col④). Outside the shell, useContextPanelCollapse
// returns null, so the panel had no collapse affordance — this wrapper supplies
// its own: a collapse toggle (passed as ConversationSidebar's `toolbar`) and a
// thin collapsed strip with an expand button. State persists in localStorage.
const EMBEDDED_COLLAPSE_KEY = 'ac.convsidebar.embedded.collapsed';
function readEmbeddedCollapsed(): boolean {
  try {
    return window.localStorage.getItem(EMBEDDED_COLLAPSE_KEY) === '1';
  } catch {
    return false;
  }
}

export function EmbeddedConversationSidebar(props: ConversationSidebarProps): React.ReactElement {
  const [collapsed, setCollapsed] = useState(readEmbeddedCollapsed);
  const setAndPersist = (v: boolean): void => {
    setCollapsed(v);
    try {
      window.localStorage.setItem(EMBEDDED_COLLAPSE_KEY, v ? '1' : '0');
    } catch {
      /* storage disabled */
    }
  };
  if (collapsed) {
    return (
      <aside
        className="flex w-9 shrink-0 flex-col items-center border-l border-border-base py-2"
        data-testid="conv-embedded-sidebar"
        data-collapsed="true"
      >
        <button
          type="button"
          onClick={() => setAndPersist(false)}
          data-testid="conv-embedded-sidebar-toggle"
          aria-label="Show conversation details"
          aria-expanded={false}
          title="Show conversation details"
          className="inline-flex h-7 w-7 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
        >
          <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" className="h-4 w-4" aria-hidden="true">
            <path strokeLinecap="round" strokeLinejoin="round" d="M12.5 5l-5 5 5 5" />
          </svg>
        </button>
      </aside>
    );
  }
  return (
    <aside
      className="flex w-72 shrink-0 flex-col overflow-hidden border-l border-border-base"
      data-testid="conv-embedded-sidebar"
      data-collapsed="false"
    >
      <ConversationSidebar
        {...props}
        showShellCollapse={false}
        toolbar={
          <button
            type="button"
            onClick={() => setAndPersist(true)}
            data-testid="conv-embedded-sidebar-toggle"
            aria-label="Collapse conversation details"
            aria-expanded
            title="Collapse"
            className="inline-flex h-7 w-7 items-center justify-center rounded text-text-secondary hover:bg-bg-subtle hover:text-text-primary"
          >
            <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" strokeWidth="1.5" className="h-4 w-4" aria-hidden="true">
              <path strokeLinecap="round" strokeLinejoin="round" d="M7.5 5l5 5-5 5" />
            </svg>
          </button>
        }
      />
    </aside>
  );
}
