import type React from 'react';
import { useMemo } from 'react';
import { useParams, Link } from 'react-router-dom';
import {
  useConversation,
  useConversationRefs,
  useMessages,
} from '@/api/conversations';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';
import { ParticipantsPanel } from '@/components/ParticipantsPanel';
import { CarryOverDivider } from '@/components/CarryOverDivider';
import { ConversationDeriveControls } from '@/components/ConversationDeriveControls';
import { useSelection } from '@/components/useSelection';
import { api } from '@/api/client';
import type { Message } from '@/api/types';
import { useQuery } from '@tanstack/react-query';

// IssueDetail page (/issues/:id). Renders the issue conversation + a
// carry-over section at the top showing the source messages that were
// referenced into this issue at creation.
//
// To render carry-over messages we need their content, which lives in
// the SOURCE conversation. We fetch refs first, then for each unique
// source_conversation_id we fetch its messages and pick the referenced
// ones. Source convs are typically small (channels), so this is cheap;
// react-query dedupes if the user has the channel page open elsewhere.
export default function IssueDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const conv = useConversation(id);
  const messages = useMessages(id);
  const refs = useConversationRefs(id);
  const selection = useSelection();

  // Unique source conversation ids from the refs.
  const sourceIds = useMemo(() => {
    const set = new Set<string>();
    (refs.data ?? []).forEach((r) => set.add(r.source_conversation_id));
    return Array.from(set);
  }, [refs.data]);

  // One query per source conv. Each one fetches /messages and we pluck
  // referenced ids on the render side.
  const sourceMessages = useSourceMessages(sourceIds);

  if (conv.isLoading) {
    return (
      <section className="text-sm text-slate-500" data-testid="page-IssueDetail">
        Loading issue…
      </section>
    );
  }
  if (conv.isError) {
    return (
      <section className="space-y-3" data-testid="page-IssueDetail">
        <p className="text-sm text-red-600" data-testid="issue-not-found">
          {(conv.error as Error).message}
        </p>
        <Link to="/issues" className="text-blue-600 hover:underline">
          Back to issues
        </Link>
      </section>
    );
  }
  if (!conv.data) {
    return (
      <section className="text-sm text-red-600" data-testid="page-IssueDetail">
        Issue lookup failed.
      </section>
    );
  }

  const participants = conv.data.participants ?? [];

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-IssueDetail"
      data-issue-id={conv.data.id}
    >
      <header className="flex items-start justify-between border-b border-slate-200 pb-3">
        <div>
          <h2 className="text-xl font-semibold">{conv.data.name || conv.data.id}</h2>
          {conv.data.description && (
            <p className="text-sm text-slate-500">{conv.data.description}</p>
          )}
          <p className="text-xs text-slate-500">
            state: <span className="font-mono">{conv.data.status}</span>
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

      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col overflow-hidden p-4">
          <CarryOverDivider refs={refs.data ?? []} messages={sourceMessages} />
          {messages.isLoading && (
            <p className="text-sm text-slate-500" data-testid="issue-messages-loading">
              Loading messages…
            </p>
          )}
          {messages.isError && (
            <p className="text-sm text-red-600" data-testid="issue-messages-error">
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
        <ParticipantsPanel conversationId={conv.data.id} participants={participants} />
      </div>
    </section>
  );
}

// useSourceMessages — fetches messages from each unique source conv id
// and returns a single flattened Message[]. Used by CarryOverDivider
// which filters by source_message_id.
function useSourceMessages(sourceIds: string[]): Message[] {
  // Single combined query keyed by the joined list keeps this hook
  // simple (avoids react-query's `useQueries` API churn).
  const key = sourceIds.slice().sort().join('|');
  const q = useQuery({
    queryKey: ['source-messages', key],
    queryFn: async () => {
      if (sourceIds.length === 0) return [];
      const lists = await Promise.all(
        sourceIds.map((id) =>
          api.get<Message[]>(`/conversations/${id}/messages`),
        ),
      );
      return lists.flat();
    },
    enabled: sourceIds.length > 0,
  });
  return q.data ?? [];
}
