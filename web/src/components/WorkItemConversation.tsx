import type React from 'react';
import { useConversationByOwnerRef, useMessages } from '@/api/conversations';
import { MessageList } from './MessageList';
import { MessageComposer } from './MessageComposer';

interface Props {
  // The expected pm owner_ref for the embedding page (pm://tasks|issues/{id}).
  ownerRef: string;
  // Short human label for the owner banner, e.g. the task/issue title.
  bannerLabel: string;
}

// WorkItemConversation (#137) — embeds the task/issue conversation inside
// TaskDetail / IssueDetail. It fetches the conversation BY owner_ref (the
// list endpoint is org-scoped, so a cross-org owner_ref yields nothing —
// fail-closed). An owner banner names the bound task/issue, and the message
// list is split into work-item segments. v2.7 #186-4: a MessageComposer at
// the bottom makes the task/issue conversation interactive — a human can
// send a message into it (the agent replies via #185 wake + post_message).
export function WorkItemConversation({ ownerRef, bannerLabel }: Props): React.ReactElement {
  const conv = useConversationByOwnerRef(ownerRef);
  const messages = useMessages(conv.data?.id);

  return (
    <section className="mt-6 flex min-h-0 flex-1 flex-col" data-testid="work-item-conversation">
      <div
        className="flex items-center gap-2 rounded-t border border-border-base bg-bg-subtle px-3 py-2 text-xs text-text-secondary"
        data-testid="conversation-owner-banner"
        data-owner-ref={ownerRef}
      >
        <span className="font-semibold uppercase tracking-wide text-text-muted">Conversation</span>
        <span>· linked</span>
        <span className="font-mono text-text-primary">{bannerLabel}</span>
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
      ) : messages.isError ? (
        <p className="rounded-b border border-t-0 border-border-base p-4 text-sm text-danger" data-testid="conversation-messages-error">
          {(messages.error as Error).message}
        </p>
      ) : (
        <div className="flex min-h-0 flex-1 flex-col rounded-b border border-t-0 border-border-base">
          {messages.isLoading ? (
            <p className="p-4 text-sm text-text-muted" data-testid="conversation-messages-loading">
              Loading messages…
            </p>
          ) : (
            <MessageList messages={messages.data ?? []} segmentByWorkItem />
          )}
          {/* v2.7 #186-4: send a message into the task/issue conversation. */}
          <div className="border-t border-border-base p-2" data-testid="conversation-composer">
            <MessageComposer conversationId={conv.data.id} />
          </div>
        </div>
      )}
    </section>
  );
}
