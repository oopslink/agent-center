import type React from 'react';
import { useParams, Link } from 'react-router-dom';
import { useConversation, useMessages } from '@/api/conversations';
import { useAppStore } from '@/store/app';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';
import { ConversationDeriveControls } from '@/components/ConversationDeriveControls';
import { useSelection } from '@/components/useSelection';

// DMDetail page (/dms/:id). Mirrors ChannelDetail layout but skips the
// ParticipantsPanel — DM membership is fixed at create time (per
// ADR-0032 § 6) and not mutable from the UI.
//
// SSE live updates flow through F5's dispatchToQueryClient, same as
// channels — `conversation.message_added` invalidates the messages
// query.
export default function DMDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const me = useAppStore((s) => s.currentUserId);
  const conv = useConversation(id);
  const messages = useMessages(id);
  const selection = useSelection();

  if (conv.isLoading) {
    return (
      <section className="text-sm text-slate-500" data-testid="page-DMDetail">
        Loading DM…
      </section>
    );
  }
  if (conv.isError) {
    return (
      <section className="space-y-3 text-sm" data-testid="page-DMDetail">
        <p className="text-red-600" data-testid="dm-not-found">
          {(conv.error as Error).message}
        </p>
        <Link to="/dms" className="text-blue-600 hover:underline">
          Back to DMs
        </Link>
      </section>
    );
  }
  if (!conv.data) {
    return (
      <section className="text-sm text-red-600" data-testid="page-DMDetail">
        DM lookup failed.
      </section>
    );
  }

  // Peer label = active participants other than the current user, joined
  // by " · ". For group DMs this lists everyone.
  const peers = (conv.data.participants ?? [])
    .filter((p) => !p.left_at && p.identity_id !== me)
    .map((p) => p.identity_id);
  const heading = conv.data.name || peers.join(' · ') || conv.data.id;

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-DMDetail"
      data-dm-id={conv.data.id}
    >
      <header className="flex items-center justify-between border-b border-slate-200 pb-3">
        <div>
          <h2 className="text-xl font-semibold" data-testid="dm-heading">
            {heading}
          </h2>
          <p className="text-xs text-slate-500">
            {peers.length === 0
              ? 'You — solo DM'
              : `with ${peers.length} ${peers.length === 1 ? 'peer' : 'peers'}`}
          </p>
        </div>
        <button
          type="button"
          onClick={selection.toggleSelectMode}
          className={[
            'rounded px-2.5 py-1 text-xs font-medium',
            selection.selectMode
              ? 'bg-slate-900 text-white'
              : 'bg-slate-100 text-slate-700 hover:bg-slate-200',
          ].join(' ')}
          data-testid="select-mode-toggle"
          aria-pressed={selection.selectMode}
        >
          {selection.selectMode ? 'Cancel select' : 'Select messages'}
        </button>
      </header>

      <div className="flex flex-1 flex-col overflow-hidden">
        {messages.isLoading && (
          <p className="p-4 text-sm text-slate-500" data-testid="dm-messages-loading">
            Loading messages…
          </p>
        )}
        {messages.isError && (
          <p className="p-4 text-sm text-red-600" data-testid="dm-messages-error">
            {(messages.error as Error).message}
          </p>
        )}
        {messages.isSuccess && (
          <MessageList
            messages={messages.data}
            selectable={selection.selectMode}
            isSelected={selection.isSelected}
            onToggle={selection.toggle}
          />
        )}
        <ConversationDeriveControls
          conversationId={conv.data.id}
          selection={selection}
        />
        <MessageComposer conversationId={conv.data.id} />
      </div>
    </section>
  );
}
