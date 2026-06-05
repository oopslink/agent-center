import type React from 'react';
import { useMessages } from '@/api/conversations';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';

// v2.8 #264 P1: the surface-agnostic conversation shell. channel / DM /
// task-thread / issue-thread all render through ONE <ConversationView> — the
// body (message fetch → loading/error/MessageList + Composer) is shared; only
// the per-surface header chrome + optional side panel are injected.
//
// This is the §5.1 "surface-agnostic conversation component" axis: upgrade the
// conversation surface once, every surface benefits. Replaces the duplicated
// message-body blocks previously inlined in ChannelDetail / DMDetail and wrapped
// by WorkItemConversation.
export type ConversationSurface = 'channel' | 'dm' | 'task-thread' | 'issue-thread';

interface Props {
  surface: ConversationSurface;
  conversationId: string;
  /** surface-specific header chrome (name/breadcrumb/TypeChip), rendered above the message body. */
  header?: React.ReactNode;
  /** optional right-side panel (e.g. channel ParticipantsPanel); rendered beside the message body. */
  sidePanel?: React.ReactNode;
}

export function ConversationView({
  surface,
  conversationId,
  header,
  sidePanel,
}: Props): React.ReactElement {
  const messages = useMessages(conversationId);

  const body = (
    <div className="flex flex-1 flex-col overflow-hidden">
      {messages.isLoading && (
        <p className="p-4 text-sm text-text-muted" data-testid="conversation-loading">
          Loading messages…
        </p>
      )}
      {messages.isError && (
        <p className="p-4 text-sm text-danger" data-testid="conversation-error">
          {(messages.error as Error).message}
        </p>
      )}
      {messages.isSuccess && <MessageList messages={messages.data} />}
      <MessageComposer conversationId={conversationId} />
    </div>
  );

  return (
    <div
      className="flex flex-1 flex-col overflow-hidden"
      data-testid="conversation-view"
      data-surface={surface}
    >
      {header}
      {sidePanel ? (
        <div className="flex flex-1 overflow-hidden">
          {body}
          {sidePanel}
        </div>
      ) : (
        body
      )}
    </div>
  );
}
