import type React from 'react';
import { useEffect, useState } from 'react';
import { useConversationByOwnerRef } from '@/api/conversations';
import { ConversationView } from './ConversationView';
import { SenderSidebarProvider } from './SenderSidebarContext';
import { FollowToggle } from './FollowToggle';

interface Props {
  // The expected pm owner_ref for the embedding page (pm://tasks|issues/{id}).
  ownerRef: string;
  // Short human label for the owner banner, e.g. the task/issue title.
  bannerLabel: string;
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
export function WorkItemConversation({ ownerRef, bannerLabel }: Props): React.ReactElement {
  const conv = useConversationByOwnerRef(ownerRef);
  const surface = ownerRef.includes('/issues/') ? 'issue-thread' : 'task-thread';

  // T206 maximize: on mobile the embedded thread sits at the very bottom of a
  // long scrolling detail page with only a few lines visible (you have to scroll
  // past title + description + attachments to reach it, and the on-screen height
  // is tiny). The maximize toggle promotes the whole conversation section to a
  // fixed full-viewport overlay (above the bottom tab bar) so the chat — message
  // history + composer — gets the entire screen; restore returns it inline. It is
  // available on every breakpoint but primarily fixes the cramped mobile case.
  const [maximized, setMaximized] = useState(false);

  // While maximized, lock body scroll (the overlay owns the viewport) and let
  // Escape restore — standard full-screen-overlay affordances.
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
    // #281 entry ②: the task/issue thread message surface needs a SenderSidebarProvider
    // so @mention tokens in messages are clickable (each message surface wraps its own).
    <SenderSidebarProvider>
    <section
      className={
        maximized
          ? 'fixed inset-0 z-50 m-0 flex min-h-0 flex-col bg-bg-base p-3'
          : 'mt-6 flex min-h-0 flex-1 flex-col'
      }
      data-testid="work-item-conversation"
      data-maximized={maximized ? 'true' : 'false'}
    >
      <div
        className="flex items-center gap-2 rounded-t border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-secondary"
        data-testid="conversation-owner-banner"
        data-owner-ref={ownerRef}
      >
        <span className="font-semibold uppercase tracking-wide text-text-muted">Conversation</span>
        <span>· linked</span>
        <span className="font-mono text-text-primary">{bannerLabel}</span>
        <span className="ml-auto flex items-center gap-1">
          {/* #264 P1 / #176 §4: follow this task/issue thread (threads default unfollowed). */}
          {conv.data && (
            <FollowToggle conversationId={conv.data.id} followed={conv.data.followed ?? false} />
          )}
          <button
            type="button"
            onClick={() => setMaximized((m) => !m)}
            className="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded text-text-muted hover:bg-border-base hover:text-text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-accent"
            data-testid="conversation-maximize-toggle"
            aria-pressed={maximized}
            aria-label={maximized ? 'Restore conversation' : 'Maximize conversation'}
            title={maximized ? 'Restore' : 'Maximize'}
          >
            {maximized ? <RestoreIcon /> : <MaximizeIcon />}
          </button>
        </span>
      </div>

      {conv.isLoading ? (
        <p className="rounded-b border border-t-0 border-border-base p-4 text-sm text-text-muted" data-testid="conversation-loading">
          Loading conversation…
        </p>
      ) : !conv.data ? (
        <p
          className="rounded-b border border-t-0 border-border-base p-4 text-sm italic text-text-muted"
          data-testid="conversation-empty"
        >
          No linked conversation yet.
        </p>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col rounded-b border border-t-0 border-border-base">
          {/* #264 P1: message body + read-cursor + SSE flow through the shared shell. */}
          <ConversationView surface={surface} conversationId={conv.data.id} />
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
