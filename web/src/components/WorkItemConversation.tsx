import type React from 'react';
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

  return (
    // #281 entry ②: the task/issue thread message surface needs a SenderSidebarProvider
    // so @mention tokens in messages are clickable (each message surface wraps its own).
    <SenderSidebarProvider>
    <section className="mt-6 flex min-h-0 flex-1 flex-col" data-testid="work-item-conversation">
      <div
        className="flex items-center gap-2 rounded-t border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-secondary"
        data-testid="conversation-owner-banner"
        data-owner-ref={ownerRef}
      >
        <span className="font-semibold uppercase tracking-wide text-text-muted">Conversation</span>
        <span>· linked</span>
        <span className="font-mono text-text-primary">{bannerLabel}</span>
        {/* #264 P1 / #176 §4: follow this task/issue thread (threads default unfollowed). */}
        {conv.data && (
          <span className="ml-auto">
            <FollowToggle conversationId={conv.data.id} followed={conv.data.followed ?? false} />
          </span>
        )}
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
