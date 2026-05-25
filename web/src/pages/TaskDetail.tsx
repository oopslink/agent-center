import type React from 'react';
import { useParams, Link } from 'react-router-dom';
import { useConversation, useMessages } from '@/api/conversations';
import { MessageList } from '@/components/MessageList';
import { MessageComposer } from '@/components/MessageComposer';
import { ParticipantsPanel } from '@/components/ParticipantsPanel';
import { ConversationDeriveControls } from '@/components/ConversationDeriveControls';
import { useSelection } from '@/components/useSelection';

// TaskDetail (/tasks/:id). Renders the task conversation + a link to
// the trace view; the trace itself lives at /tasks/:id/trace so users
// can deep-link directly to it without the composer noise.
export default function TaskDetail(): React.ReactElement {
  const { id = '' } = useParams<{ id: string }>();
  const conv = useConversation(id);
  const messages = useMessages(id);
  const selection = useSelection();

  if (conv.isLoading) {
    return (
      <section className="text-sm text-slate-500" data-testid="page-TaskDetail">
        Loading task…
      </section>
    );
  }
  if (conv.isError) {
    return (
      <section className="space-y-3" data-testid="page-TaskDetail">
        <p className="text-sm text-danger" data-testid="task-not-found">
          {(conv.error as Error).message}
        </p>
        <Link to="/tasks" className="text-blue-600 hover:underline">
          Back to tasks
        </Link>
      </section>
    );
  }
  if (!conv.data) {
    return (
      <section className="text-sm text-danger" data-testid="page-TaskDetail">
        Task lookup failed.
      </section>
    );
  }

  const participants = conv.data.participants ?? [];

  return (
    <section
      className="flex h-full flex-col"
      data-testid="page-TaskDetail"
      data-task-id={conv.data.id}
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
        <div className="flex items-center gap-2">
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
          <Link
            to={`/tasks/${encodeURIComponent(conv.data.id)}/trace`}
            className="rounded bg-slate-100 px-3 py-1.5 text-xs text-slate-700 hover:bg-slate-200"
            data-testid="task-view-trace"
          >
            View trace →
          </Link>
        </div>
      </header>

      <div className="flex flex-1 overflow-hidden">
        <div className="flex flex-1 flex-col overflow-hidden">
          {messages.isLoading && (
            <p className="p-4 text-sm text-slate-500" data-testid="task-messages-loading">
              Loading messages…
            </p>
          )}
          {messages.isError && (
            <p className="p-4 text-sm text-danger" data-testid="task-messages-error">
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
