import type React from 'react';
import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { useConversationByOwnerRef } from '@/api/conversations';
import { ConversationView } from './ConversationView';
import { ConversationSurfaceMobile } from './ConversationSurfaceMobile';
import { EmbeddedConversationSidebar, EmbeddedSidebarToggle } from './ConversationSidebar';
import { SenderSidebarProvider } from './SenderSidebarContext';
import { FollowToggle } from './FollowToggle';
import { useIsMobile } from './WorkItemMobileMeta';

interface Props {
  // The expected pm owner_ref for the embedding page (pm://tasks|issues/{id}).
  ownerRef: string;
  // Short human label for the owner banner, e.g. the task/issue title.
  bannerLabel: string;
  // The owner's human-friendly short id ("T123" / "I233"). When present it
  // replaces the generic "Conversation" badge so the banner names the bound
  // task/issue by its concrete id (per @oopslink). Falls back to "Conversation".
  ownerCode?: string;
}

// WorkItemConversation (#137) — embeds the task/issue conversation inside
// TaskDetail / IssueDetail. It fetches the conversation BY owner_ref (the
// list endpoint is org-scoped, so a cross-org owner_ref yields nothing —
// fail-closed). An owner banner names the bound task/issue.
//
// #264 P1: once the conversation is resolved, the message body renders
// through the surface-agnostic <ConversationView> — so the task/issue thread
// gains the same read-cursor (markSeen) + SSE live-update behavior as
// channels/DMs, uniformly. The surface (task-thread vs issue-thread) is
// derived from the owner_ref. v2.7 #186-4: the shell's composer keeps the
// thread interactive (a human can send in; the agent replies via #185 wake).
// Embedded sidebar collapse state — lifted so the banner can render the toggle.
const EMBEDDED_COLLAPSE_KEY = 'ac.convsidebar.embedded.collapsed';
function readEmbeddedCollapsed(): boolean {
  try {
    return window.localStorage.getItem(EMBEDDED_COLLAPSE_KEY) === '1';
  } catch {
    return false;
  }
}

export function WorkItemConversation({ ownerRef, bannerLabel, ownerCode }: Props): React.ReactElement {
  const { t } = useTranslation('chat');
  const conv = useConversationByOwnerRef(ownerRef);
  const surface = ownerRef.includes('/issues/') ? 'issue-thread' : 'task-thread';
  const isMobile = useIsMobile();

  const [maximized, setMaximized] = useState(false);
  // Embedded sidebar collapse — lifted here so the banner row can show the
  // expand toggle (no more w-9 strip between chat and metadata sidebar).
  const [embeddedCollapsed, setEmbeddedCollapsed] = useState(readEmbeddedCollapsed);
  const toggleEmbeddedCollapsed = (v: boolean): void => {
    setEmbeddedCollapsed(v);
    try {
      window.localStorage.setItem(EMBEDDED_COLLAPSE_KEY, v ? '1' : '0');
    } catch { /* storage disabled */ }
  };

  useEffect(() => {
    if (!maximized) return;
    const prevOverflow = document.body.style.overflow;
    document.body.style.overflow = 'hidden';
    const onKey = (e: KeyboardEvent): void => {
      if (e.key === 'Escape') setMaximized(false);
    };
    window.addEventListener('keydown', onKey);
    return () => {
      document.body.style.overflow = prevOverflow;
      window.removeEventListener('keydown', onKey);
    };
  }, [maximized]);

  return (
    <SenderSidebarProvider>
    <section
      className={
        maximized
          ? 'fixed inset-0 z-50 m-0 flex min-h-0 flex-col bg-bg-base p-3'
          : 'relative mt-0 flex min-h-0 flex-1 flex-col md:mt-6'
      }
      data-testid="work-item-conversation"
      data-maximized={maximized ? 'true' : 'false'}
    >
      {/* Desktop: full banner with ownerCode + linked title + controls. On
          mobile the chat renders through <ConversationSurfaceMobile> below,
          which carries its own segment-pill switcher (chat/threads/files/people).
          This desktop banner keeps its own maximize toggle — that one is
          unrelated to the dropped MOBILE maximize (the work-item chat really is
          embedded in a long desktop page). */}
      <div
        className="hidden items-center gap-2 rounded-t border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-secondary md:flex"
        data-testid="conversation-owner-banner"
        data-owner-ref={ownerRef}
      >
        <span className="flex items-center gap-2" data-testid="conversation-owner-label">
          <span className="font-semibold uppercase tracking-wide text-text-muted" data-testid="conversation-owner-code">
            {ownerCode || t('panels.workItem.conversationBadge')}
          </span>
          <span className="flex items-center gap-2" data-testid="conversation-owner-title">
            <span>{t('panels.workItem.linked')}</span>
            <span className="font-mono text-text-primary">{bannerLabel}</span>
          </span>
        </span>
        <span className="ml-auto flex items-center gap-1">
          {conv.data && (
            <FollowToggle conversationId={conv.data.id} followed={conv.data.followed ?? false} />
          )}
          {embeddedCollapsed && (
            <EmbeddedSidebarToggle collapsed={embeddedCollapsed} onExpand={() => toggleEmbeddedCollapsed(false)} />
          )}
          <button
            type="button"
            onClick={() => setMaximized((m) => !m)}
            className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded text-text-muted hover:bg-border-base hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            data-testid="conversation-maximize-toggle"
            aria-pressed={maximized}
            aria-label={maximized ? t('panels.workItem.restoreAriaLabel') : t('panels.workItem.maximizeAriaLabel')}
            title={maximized ? t('panels.workItem.restoreTitle') : t('panels.workItem.maximizeTitle')}
          >
            {maximized ? <RestoreIcon /> : <MaximizeIcon />}
          </button>
        </span>
      </div>

      {conv.isLoading ? (
        <p className="p-4 text-sm text-text-muted md:rounded-b md:border md:border-t-0 md:border-border-base" data-testid="conversation-loading">
          {t('panels.workItem.loading')}
        </p>
      ) : !conv.data ? (
        <p
          className="p-4 text-sm italic text-text-muted md:rounded-b md:border md:border-t-0 md:border-border-base"
          data-testid="conversation-empty"
        >
          {t('panels.workItem.empty')}
        </p>
      ) : isMobile ? (
        // Mobile: the same redesigned segment-pill surface (chat / threads /
        // files / people) used by DM/channel — so task/issue chat gains
        // threads/files access on small screens (owner: "task/issue/plan 的 chat
        // 也加上"). mobile-redesign-workspace-core.md §5 mandates this reuse
        // rather than a second design.
        <ConversationSurfaceMobile
          surface={surface}
          conversationId={conv.data.id}
          participants={conv.data.participants ?? []}
        />
      ) : (
        <div className="flex min-h-0 flex-1 overflow-hidden md:rounded-b md:border md:border-t-0 md:border-border-base">
          <div className="flex min-h-0 min-w-0 flex-1 flex-col">
            <ConversationView surface={surface} conversationId={conv.data.id} />
          </div>
          <EmbeddedConversationSidebar
            conversationId={conv.data.id}
            participants={conv.data.participants ?? []}
            collapsed={embeddedCollapsed}
            onToggleCollapsed={toggleEmbeddedCollapsed}
          />
        </div>
      )}
    </section>
    </SenderSidebarProvider>
  );
}

// Maximize / restore glyphs — single-stroke SVGs (no-emoji UX rule), matching the
// icon style used elsewhere in the app. Maximize = corner arrows pushing outward;
// restore = corner arrows pulling inward.
function MaximizeIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.6" aria-hidden="true">
      <path d="M8 4H4v4M16 8V4h-4M4 12v4h4M12 16h4v-4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function RestoreIcon(): React.ReactElement {
  return (
    <svg viewBox="0 0 20 20" fill="none" className="h-4 w-4 stroke-current" strokeWidth="1.6" aria-hidden="true">
      <path d="M4 8h4V4M12 4v4h4M8 16v-4H4M16 12h-4v4" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}
